// The list's rules, its store run against SQLite (which D1 is) with D1's
// batches as transactions, the ledger's tamper checks, the email budget,
// and every page.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";
import { test } from "node:test";

import { esc, page, span } from "../src/page.ts";
import { area, budget, created, digest, held, inviteCode, inviteLife, keyed, launches, masked, networks, normalEmail, same } from "../src/signup.ts";
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
  assert.equal(masked("Steve.Tester@GMail.com"), "st***@gm***.com");
  assert.equal(masked("a@b.io"), "a***@b***.io");
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

test("a link: sent, rate limited, clicked, used once", async () => {
  const { s } = fresh();
  const c = (i: number) => ({ id: `id${i}`, link: `k${i}`, launch: "silcord", user: "1", purpose: "issue" as const, made: t0 + i, expires: t0 + 600_000, email: "E" });
  for (let i = 0; i < 5; i++) assert.ok(await s.challenge(c(i), 5));
  assert.ok(!(await s.challenge(c(5), 5)), "five an hour");
  assert.ok(await s.challenge({ ...c(6), made: t0 + 3_600_001, expires: t0 + 4_000_000 }, 5));

  assert.equal((await s.waiting("id0", t0))?.verified, null);
  assert.equal(await s.consume("id0", t0), null, "not clicked yet");
  assert.ok(!(await s.prove("nope", t0)));
  assert.ok(await s.prove("k0", t0 + 1));
  assert.ok(!(await s.prove("k0", t0 + 2)), "clicked once");
  assert.equal((await s.consume("id0", t0 + 3))?.user, "1");
  assert.equal(await s.consume("id0", t0 + 4), null, "used once");
  assert.ok(!(await s.prove("k1", t0 + 700_000)), "too late");
  await s.forget(t0 + 4_000_001, 1, [l], 1);
  assert.equal(await s.waiting("id6", t0), null);
});

test("the email budget can't be overspent, in any window", async () => {
  const { s } = fresh();
  const d = 86_400_000;
  for (let i = 0; i < 3; i++) assert.ok(await s.spend(t0, 5, 31));
  assert.ok(await s.spend(t0 + 10 * d, 5, 31));
  assert.ok(await s.spend(t0 + 20 * d, 5, 31));
  assert.ok(!(await s.spend(t0 + 30 * d, 5, 31)), "five in 31 days");
  assert.ok(await s.spend(t0 + 31 * d, 5, 31), "the first day's three have left the window");
  assert.ok(budget.emails <= 3000 && budget.days >= 31, "inside what the plan includes in any billing month");
});

// visible is what a person reads on a page: no markup, styles or scripts,
// and nothing in bold, which is an address or a number.
const visible = (html: string) =>
  html
    .replace(/<(style|script|title)[\s\S]*?<\/\1>/g, "")
    .replace(/<meta[^>]*>/g, "")
    .replace(/<b>[\s\S]*?<\/b>/g, "")
    .replace(/<[^>]+>/g, " ")
    .replace(/&#\d+;/g, "");

test("every page, in the voice", () => {
  const now = t0;
  const views = {
    open: page(l, 1204, { kind: "open" }, "key"),
    mine: page(l, 1, { kind: "in", rank: 7, held: null, released: 3, left: 2, now, invites: [{ hash: "h1", made: now - 3_600_000, expires: now + 7_200_000, spent_at: null, revoked: null }, { hash: "h2", made: now - 60_000, expires: now, spent_at: now, revoked: null }, { hash: "h3", made: now - 60_000, expires: now + 1, spent_at: null, revoked: now }, { hash: "h4", made: now - 3 * 86400_000, expires: now - 1, spent_at: null, revoked: null }] }, "key"),
    invitee: page(l, 1, { kind: "in", rank: null, held: null, released: 0, left: 0, now, invites: [] }, "key"),
    held: page(l, 1, { kind: "in", rank: 2, held: "someone on your connection is already on the list", released: 3, left: 3, now, invites: [] }, "key"),
    invited: page(l, 1, { kind: "invited", code: "c", expires: now + 3_600_000, now }, "key"),
    sent: page(l, 1, { kind: "sent", to: "st***@gm***.com", expires: now + 600_000, now, what: "take your invite" }, "k"),
    confirm: page(l, 1, { kind: "confirm", token: "t" }, "k"),
    confirmed: page(l, 1, { kind: "confirmed" }, "k"),
    error: page(l, 1, { kind: "error", why: "that link ran out or was already used\nstart again from your page" }, "k"),
  };
  for (const [name, html] of Object.entries(views)) {
    const words = visible(html);
    assert.doesNotMatch(words, /[.,;:!?'"\u2014\u2026]/, `${name} has punctuation: ${words.match(/.{0,30}[.,;:!?'"].{0,30}/)?.[0]}`);
    assert.doesNotMatch(words, /[A-Z]/, `${name} has capitals`);
  }
  assert.match(views.open, /<b>1204<\/b> people on the list/);
  assert.match(views.open, /data-sitekey="key"/);
  assert.match(views.mine, /#7/);
  assert.match(views.mine, /▰▰▱<\/span>  2 of 3 left/);
  assert.match(views.mine, /open for 2h 0m/);
  assert.match(views.mine, />used</);
  assert.match(views.mine, /taken back/);
  assert.match(views.mine, /ran out and came back to you/);
  assert.match(views.mine, /make an invite/);
  assert.match(views.invitee, /youre in through a friend/);
  assert.doesNotMatch(views.held, /turnstile\/v0|make an invite/);
  assert.match(views.sent, /http-equiv="refresh"[\s\S]*<b>st\*\*\*@gm\*\*\*\.com<\/b>/);
  assert.match(views.confirm, /src="\/confirm\.js"[\s\S]*/);
  assert.match(views.confirm, /action="\/v\/t"/);
  assert.match(views.error, /ran out or was already used<br>start again/);
  assert.match(page(l, 1, { kind: "in", rank: 2, held: "a <b>", released: 3, left: 3, now, invites: [] }, "key"), /a &#60;b&#62;/);
  assert.match(page(l, 1, { kind: "minted", link: "https://cygnet.lol/i/x", expires: now + inviteLife, now }, "k"), /class="link"[^>]*>https:\/\/cygnet\.lol\/i\/x</);
  assert.equal(span(45_000), "45s");
  assert.equal(span(3 * 3600_000 + 12 * 60_000 + 5_000), "3h 12m");
  assert.equal(span(2 * 86400_000 + 4 * 3600_000), "2d 4h");
  assert.equal(span(86400_000 + 46_000), "1d 0h");
  assert.equal(span(12 * 60_000), "12m 0s");
  assert.equal(span(0), "0s");
  assert.equal(esc(`"'&`), "&#34;&#39;&#38;");
});
