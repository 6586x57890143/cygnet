// The list's rules, its store run against SQLite (which D1 is) with D1's
// batches as transactions, the ledger's tamper checks, and DKIM against
// messages signed here.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";
import { test } from "node:test";

import { canonBody, canonHeader, canonHeaders, parse, signer, stripB, type Lookup } from "../src/dkim.ts";
import { esc, page, span } from "../src/page.ts";
import { area, created, digest, held, inviteCode, inviteLife, keyed, launches, mailbox, networks, normalEmail, same } from "../src/signup.ts";
import { Store, type DB, type Stmt } from "../src/store.ts";

const l = launches.silcord;

test("one inbox is one email", () => {
  assert.equal(normalEmail("Steve.Tester+launch@GoogleMail.com"), "stevetester@gmail.com");
  assert.equal(normalEmail("steve.tester+x@proton.me"), "steve.tester@proton.me");
  assert.equal(normalEmail("a+b@c+d.io"), "a@c+d.io");
});

test("a connection and the network around it", () => {
  assert.deepEqual(networks("203.0.113.7"), { ip: "203.0.113.7", net: "203.0.113" });
  assert.deepEqual(networks("::ffff:203.0.113.7"), { ip: "203.0.113.7", net: "203.0.113" });
  assert.deepEqual(networks("2001:db8:0a:1::5"), { ip: "2001:db8:a:1", net: "2001:db8:a" });
  assert.deepEqual(networks("2001:DB8::"), { ip: "2001:db8:0:0", net: "2001:db8:0" });
});

test("an area is a country and a half degree square", () => {
  assert.equal(area({ country: "DE", latitude: "52.52", longitude: "13.40" }), "DE:105:26");
  assert.equal(area({ country: "DE", latitude: "52.74", longitude: "13.49" }), "DE:105:26");
  assert.equal(area({ country: "DE", region: "Berlin" }), "DE:Berlin");
  assert.equal(area(undefined), "");
});

test("an account's age is in its id", () => {
  assert.equal(new Date(created("175928847299117063")).toISOString(), "2016-04-30T11:18:25.796Z");
});

test("codes, hashes and comparisons", async () => {
  const a = await keyed("k", "ip", "203.0.113.7");
  assert.equal(a.length, 22);
  assert.equal(a, await keyed("k", "ip", "203.0.113.7"));
  assert.notEqual(a, await keyed("k", "net", "203.0.113.7"));
  assert.notEqual(a, await keyed("other", "ip", "203.0.113.7"));
  assert.match(inviteCode(), /^[a-z2-7]{32}$/);
  assert.notEqual(inviteCode(), inviteCode());
  assert.match(mailbox(), /^v-[a-z2-7]{26}$/);
  assert.equal((await digest("abc")).slice(0, 8), "ba7816bf");
  assert.ok(same("abc", "abc"));
  assert.ok(!same("abc", "abd") && !same("abc", "ab"));
  assert.match(held(true, true) ?? "", /connection/);
  assert.match(held(false, true) ?? "", /network nearby/);
  assert.equal(held(false, false), null);
});

// d1 is D1's surface over SQLite: a batch is one transaction, rolled back
// whole if any statement in it fails.
function d1(db: DatabaseSync): DB {
  type S = Stmt & { q: string; a: unknown[] };
  const stmt = (q: string, a: unknown[] = []): S => ({
    q,
    a,
    bind: (...v: unknown[]) => stmt(q, v),
    first: async <T>() => (db.prepare(q).get(...(a as [])) as T) ?? null,
    all: async <T>() => ({ results: db.prepare(q).all(...(a as [])) as T[] }),
    run: async () => db.prepare(q).run(...(a as [])),
  });
  return {
    prepare: (q) => stmt(q),
    batch: async (ss) => {
      db.exec("BEGIN");
      try {
        const out = (ss as S[]).map((s) => db.prepare(s.q).all(...(s.a as [])));
        db.exec("COMMIT");
        return out;
      } catch (err) {
        db.exec("ROLLBACK");
        throw err;
      }
    },
  };
}

function fresh() {
  const sql = new DatabaseSync(":memory:");
  sql.exec(readFileSync(new URL("../migrations/0001_signup.sql", import.meta.url), "utf8"));
  return { sql, s: new Store(d1(sql), "test-key") };
}

const h = (n: string, geo: string | null = "g") => ({ email: `e${n}`, ip: `i${n}`, net: `n${n}`, geo });
const t0 = 1_800_000_000_000;

test("a line, released from the front", async () => {
  const { s } = fresh();
  assert.ok(await s.join(l, "1", h("1"), null, t0));
  assert.ok(await s.join(l, "2", h("2"), "held", t0 + 1));
  assert.ok(await s.join(l, "3", h("3"), null, t0 + 2));
  assert.ok(await s.join(l, "4", h("4"), null, t0 + 3));
  // The same account or email gets no second place.
  assert.ok(!(await s.join(l, "1", h("9"), null, t0 + 4)));
  assert.ok(!(await s.join(l, "5", h("1"), null, t0 + 5)));
  assert.deepEqual(await s.seen(l, { ip: "i1", net: "x", geo: "x" }), { ip: true, area: false });
  assert.deepEqual(await s.seen(l, { ip: "x", net: "n3", geo: "g" }), { ip: false, area: true });
  assert.deepEqual(await s.seen(l, { ip: "x", net: "n3", geo: null }), { ip: false, area: false });

  // Two released from the front skips the held one.
  assert.equal(await s.release(l, 2, 3, t0 + 10), 2);
  assert.equal((await s.view(l, "1", t0))?.left, 3);
  assert.equal((await s.view(l, "2", t0))?.released, 0);
  assert.equal((await s.view(l, "3", t0))?.left, 3);
  assert.equal((await s.view(l, "4", t0))?.released, 0);
  // A lifted hold is next in a later release.
  assert.ok(await s.lift(l, "2", t0 + 11));
  assert.equal(await s.release(l, 1, 2, t0 + 12), 1);
  assert.equal((await s.view(l, "2", t0))?.released, 2);
  assert.equal((await s.view(l, "3", t0))?.rank, 3);
  assert.deepEqual(await s.verify(), { entries: 8, broken: null, mismatches: [] });
});

test("invites: issued, taken back, spent once, and a tree", async () => {
  const { s } = fresh();
  await s.join(l, "1", h("1"), null, t0);
  await s.join(l, "2", h("2"), null, t0);
  await s.release(l, 1, 2, t0);
  // Nobody issues without a release.
  assert.equal(await s.issue(l, "2", t0), null);

  const a = await s.issue(l, "1", t0);
  const b = await s.issue(l, "1", t0 + 1);
  assert.ok(a && b);
  assert.equal(await s.issue(l, "1", t0 + 2), null, "two released, two made");
  const ha = await digest(a);
  const hb = await digest(b);

  // Taking one back gives it back; someone else can't.
  assert.ok(!(await s.revoke(l, "2", hb, t0 + 3)));
  assert.ok(await s.revoke(l, "1", hb, t0 + 3));
  assert.equal((await s.view(l, "1", t0 + 3))?.left, 1);
  assert.equal(await s.open(l, hb, t0 + 4), null);

  // Spent once: the second taker gets nothing and isn't let in.
  assert.equal(await s.redeem(l, ha, "7", h("7"), null, t0 + 5), "in");
  assert.equal(await s.redeem(l, ha, "8", h("8"), null, t0 + 6), "gone");
  assert.equal(await s.member(l, "8"), null);
  const m7 = await s.member(l, "7");
  assert.equal(m7?.sponsor, "1");
  assert.equal((await s.view(l, "7", t0))?.rank, null, "an invitee isn't in line, they come in with their sponsor");

  // An invite run out is back in its issuer's hands.
  const c = await s.issue(l, "1", t0 + 7);
  assert.equal((await s.view(l, "1", t0 + 8))?.left, 0);
  assert.equal((await s.view(l, "1", t0 + 7 + inviteLife + 1))?.left, 1);
  assert.equal(await s.redeem(l, await digest(c!), "9", h("9"), null, t0 + 7 + inviteLife + 1), "gone");

  // Taking an invite with an email already on the list fails whole: the
  // invite stays unspent for the right person.
  const d = await s.issue(l, "1", t0 + inviteLife + 10);
  const hd = await digest(d!);
  assert.equal(await s.redeem(l, hd, "10", h("2"), null, t0 + inviteLife + 11), "taken");
  assert.ok(await s.open(l, hd, t0 + inviteLife + 12));
  // An issuer can't take their own.
  assert.equal(await s.redeem(l, hd, "1", h("11"), null, t0 + inviteLife + 12), "gone");

  // Leaving takes back what's unspent and keeps the tree.
  await s.leave(l, "1", t0 + inviteLife + 13);
  assert.equal(await s.member(l, "1"), null);
  assert.equal(await s.open(l, hd, t0 + inviteLife + 14), null);
  assert.equal((await s.member(l, "7"))?.sponsor, "1");
  assert.deepEqual((await s.verify()).broken, null);
  assert.deepEqual((await s.verify()).mismatches, []);
  const tree = await s.tree(l);
  assert.deepEqual(
    tree.map((r) => [r.user, r.sponsor]),
    [
      ["2", null],
      ["7", "1"],
    ],
  );
});

test("the ledger can't be rewritten, and a rewrite shows", async () => {
  const { sql, s } = fresh();
  await s.join(l, "1", h("1"), null, t0);
  await s.release(l, 1, 3, t0);
  await s.issue(l, "1", t0);
  assert.throws(() => sql.exec("UPDATE ledger SET ref = '9' WHERE kind = 'release'"), /append only/);
  assert.throws(() => sql.exec("DELETE FROM ledger"), /append only/);

  // Someone with the database but not the key: they can drop the trigger,
  // but every mac after their edit stops matching.
  sql.exec("DROP TRIGGER ledger_no_update");
  sql.exec("UPDATE ledger SET ref = '9' WHERE kind = 'release'");
  assert.equal((await s.verify()).broken, 2);

  // And a grant made around the ledger doesn't add up.
  const b = fresh();
  await b.s.join(l, "1", h("1"), null, t0);
  b.sql.exec("UPDATE member SET released = 10");
  b.sql.exec("INSERT INTO invite (hash, launch, issuer, made, expires) VALUES ('x', 'silcord', '1', 0, 0)");
  assert.equal((await b.s.verify()).mismatches.length, 2);
});

test("a proof: started, rate limited, proven by the right email, used once", async () => {
  const { s } = fresh();
  const c = (i: number) => ({ id: `id${i}`, addr: `a${i}`, launch: "silcord", user: "1", purpose: "issue" as const, made: t0 + i, expires: t0 + 600_000, email: "E" });
  for (let i = 0; i < 5; i++) assert.ok(await s.challenge(c(i), 5));
  assert.ok(!(await s.challenge(c(5), 5)), "five an hour");
  assert.ok(await s.challenge({ ...c(6), made: t0 + 3_600_001, expires: t0 + 4_000_000 }, 5));

  assert.equal((await s.waiting("id0", t0))?.verified, null);
  assert.equal(await s.consume("id0", t0), null, "not proven yet");
  assert.ok(!(await s.prove("a0", "other", t0)), "the wrong email proves nothing");
  assert.ok(await s.prove("a0", "E", t0 + 1));
  assert.ok(!(await s.prove("a0", "E", t0 + 2)), "proven once");
  assert.equal((await s.consume("id0", t0 + 3))?.user, "1");
  assert.equal(await s.consume("id0", t0 + 4), null, "used once");
  assert.ok(!(await s.prove("a1", "E", t0 + 700_000)), "too late");
  await s.forget(t0 + 4_000_001, 1, [l], 1);
  assert.equal(await s.waiting("id6", t0), null);
});

// mail signs a message the way a provider does, with this test's key.
async function mail(o: { from: string; d: string; a: "rsa-sha256" | "ed25519-sha256"; c: string; key: CryptoKey; extra?: string; body?: string; tags?: string }) {
  const [hc, bc] = o.c.split("/");
  const body = o.body ?? "hi there  \r\n\r\n\r\n";
  const head = `From: Steve <${o.from}>\r\nTo: v-x@cygnet.lol\r\nSubject:  a\r\n\tfolded subject\r\n`;
  const bh = btoa(String.fromCharCode(...new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(canonBody(body, bc))))));
  const sig = `DKIM-Signature: v=1; a=${o.a}; c=${o.c}; d=${o.d}; s=sel;\r\n h=from:to:\r\n\tsubject; ${o.tags ?? ""}bh=${bh}; b=\r\n`;
  const data = canonHeaders(parse(head), ["from", "to", "subject"], hc) + canonHeader(stripB(sig), hc).replace(/\r\n$/, "");
  const bytes = new TextEncoder().encode(data);
  const raw =
    o.a === "rsa-sha256"
      ? await crypto.subtle.sign("RSASSA-PKCS1-v1_5", o.key, bytes)
      : await crypto.subtle.sign("Ed25519", o.key, await crypto.subtle.digest("SHA-256", bytes));
  const b = btoa(String.fromCharCode(...new Uint8Array(raw)));
  return sig.replace("b=\r\n", `b=${b.slice(0, 40)}\r\n ${b.slice(40)}\r\n`) + (o.extra ?? "") + head + "\r\n" + body;
}

test("dkim: a signature from the address's own domain, or nothing", async () => {
  const rsa = (await crypto.subtle.generateKey({ name: "RSASSA-PKCS1-v1_5", modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]), hash: "SHA-256" }, true, ["sign", "verify"])) as CryptoKeyPair;
  const ed = (await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"])) as CryptoKeyPair;
  const p = async (k: CryptoKey, f: "spki" | "raw") => btoa(String.fromCharCode(...new Uint8Array(await crypto.subtle.exportKey(f, k))));
  const keys: Record<string, string> = {
    "sel._domainkey.example.com": `v=DKIM1; k=rsa; p=${await p(rsa.publicKey, "spki")}`,
    "sel._domainkey.ed.example": `v=DKIM1; k=ed25519; p=${await p(ed.publicKey, "raw")}`,
  };
  const lookup: Lookup = async (n) => keys[n] ?? null;
  const now = Date.now();

  for (const c of ["relaxed/relaxed", "simple/simple", "relaxed/simple", "simple/relaxed"]) {
    assert.equal(await signer(await mail({ from: "Steve@Example.com", d: "example.com", a: "rsa-sha256", c, key: rsa.privateKey }), lookup, now), "steve@example.com", c);
  }
  assert.equal(await signer(await mail({ from: "s@mail.example.com", d: "example.com", a: "rsa-sha256", c: "relaxed/relaxed", key: rsa.privateKey }), lookup, now), "s@mail.example.com", "a subdomain of d");
  assert.equal(await signer(await mail({ from: "s@ed.example", d: "ed.example", a: "ed25519-sha256", c: "relaxed/relaxed", key: ed.privateKey }), lookup, now), "s@ed.example");

  const good = await mail({ from: "s@example.com", d: "example.com", a: "rsa-sha256", c: "relaxed/relaxed", key: rsa.privateKey });
  assert.equal(await signer(good.replace("hi there", "hi theRe"), lookup, now), null, "a changed body");
  assert.equal(await signer(good.replace("Steve <s@example.com>", "Steve <t@example.com>"), lookup, now), null, "a changed from");
  assert.equal(await signer(good.replace("\r\n\r\n", "\r\nFrom: x@evil.test\r\n\r\n"), lookup, now), null, "a second from");
  assert.equal(await signer(good.replace(/b=([A-Za-z0-9+/]{5})/, "b=AAAAA"), lookup, now), null, "a changed signature");
  assert.equal(await signer(await mail({ from: "s@victim.test", d: "example.com", a: "rsa-sha256", c: "relaxed/relaxed", key: rsa.privateKey }), lookup, now), null, "signed by someone else's domain");
  assert.equal(await signer(await mail({ from: "s@example.com", d: "example.com", a: "rsa-sha256", c: "relaxed/relaxed", key: rsa.privateKey, tags: "l=5; " }), lookup, now), null, "part of the body");
  assert.equal(await signer(await mail({ from: "s@example.com", d: "example.com", a: "rsa-sha256", c: "relaxed/relaxed", key: rsa.privateKey, tags: "x=1; " }), lookup, now), null, "expired");
  assert.equal(await signer(await mail({ from: "s@nokey.test", d: "nokey.test", a: "rsa-sha256", c: "relaxed/relaxed", key: rsa.privateKey }), lookup, now), null, "no key published");
  assert.equal(await signer("no headers here", lookup, now), null);
});

test("the page says what it should, escaped", () => {
  const open = page(l, 1204, { kind: "open" }, "key");
  assert.match(open, /1,204<\/b> people on the list/);
  assert.match(open, /data-sitekey="key"/);
  const now = t0;
  const mine = page(l, 1, { kind: "in", rank: 7, held: null, released: 3, left: 2, now, invites: [{ hash: "h1", made: now - 3_600_000, expires: now + 7_200_000, spent_at: null, revoked: null }, { hash: "h2", made: now - 60_000, expires: now, spent_at: now, revoked: null }] }, "key");
  assert.match(mine, /#7/);
  assert.match(mine, /▰▰▱<\/span>  2 of 3 left/);
  assert.match(mine, /▸ open · 2h 0m left/);
  assert.match(mine, /✓ used/);
  assert.match(mine, /make an invite/);
  const held = page(l, 1, { kind: "in", rank: 2, held: "a <b>", released: 3, left: 3, now, invites: [] }, "key");
  assert.match(held, /a &#60;b&#62;/);
  assert.doesNotMatch(held, /turnstile\/v0|make an invite/);
  assert.match(page(l, 1, { kind: "prove", address: "v-x@cygnet.lol", expires: now + 600_000, now, what: "take your invite" }, "k"), /http-equiv="refresh"[\s\S]*mailto:v-x@cygnet.lol/);
  assert.equal(span(45_000), "45s");
  assert.equal(span(3 * 3600_000 + 12 * 60_000 + 5_000), "3h 12m");
  assert.equal(span(2 * 86400_000 + 4 * 3600_000), "2d 4h");
  assert.equal(span(86400_000 + 46_000), "1d 0h");
  assert.equal(span(12 * 60_000), "12m 0s");
  assert.equal(span(0), "0s");
  assert.equal(esc(`"'&`), "&#34;&#39;&#38;");
});
