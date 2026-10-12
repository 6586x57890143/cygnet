// The list's rules, its store run against SQLite (which D1 is) with D1's
// batches as transactions, the ledger's tamper checks, the email budget,
// and every page.
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";
import { test } from "node:test";

import { byline, esc, page, span, type Row } from "../src/page.ts";
import { area, budget, isPNG, created, digest, held, inviteCode, inviteLife, keyed, launches, masked, networks, normalEmail, same } from "../src/signup.ts";
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
  // Every migration, in order, as D1 applies them.
  const dir = new URL("../migrations/", import.meta.url);
  for (const f of readdirSync(dir).filter((n) => n.endsWith(".sql")).sort()) sql.exec(readFileSync(new URL(f, dir), "utf8"));
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

  // Leaving takes back what's unspent, keeps the tree, and leaves nothing
  // that names them: their ID is the ledger's keyed hash wherever it stood.
  await s.leave(l, "1", t0 + inviteLife + 13);
  assert.equal(await s.member(l, "1"), null);
  assert.equal(await s.open(l, hd, t0 + inviteLife + 14), null);
  const pseudo = "h:" + (await s.who("1"));
  assert.equal((await s.member(l, "7"))?.sponsor, pseudo);
  assert.deepEqual((await s.verify()).broken, null);
  assert.deepEqual((await s.verify()).mismatches, []);
  const tree = await s.tree(l);
  assert.deepEqual(
    tree.map((r) => [r.user, r.sponsor]),
    [
      ["2", null],
      ["7", pseudo],
    ],
  );
});

test("gdpr: everything about you to download, nothing left after you go, all of it gone in time", async () => {
  const { sql, s } = fresh();
  await s.join(l, "1", h("1"), null, t0);
  await s.release(l, 1, 2, t0);
  const code = await s.issue(l, "1", t0 + 1);
  await s.redeem(l, await digest(code!), "2", h("2"), null, t0 + 2);

  const mine = await s.mine(l, "1");
  assert.equal(mine?.discord_id, "1");
  assert.equal(mine?.invites_released, 2);
  assert.equal(mine?.invites_made.length, 1);
  assert.deepEqual(mine?.stored_as_keyed_hashes, { email: true, connection: true, network: true, area: true });
  assert.deepEqual(
    mine?.ledger.map((e) => [e.what, e.by_you]),
    [
      ["join", true],
      ["release", false],
      ["issue", true],
      ["redeem", false],
    ],
  );
  assert.equal(await s.mine(l, "9"), null);

  // After leaving, the raw ID is in no table.
  await s.leave(l, "1", t0 + 3);
  for (const table of ["member", "invite", "challenge"]) {
    const hit = sql.prepare(`SELECT * FROM ${table}`).all().some((r) => Object.values(r as object).includes("1"));
    assert.ok(!hit, `${table} still names them`);
  }

  // A launch's list, invites and ledger go once its time is up, and only
  // then; another launch's ledger still verifies.
  const other = { ...l, name: "wren" };
  await s.join(other, "5", h("5"), null, t0);
  await s.forget(t0 + 10, 1e15, [{ ...l, opens: t0 }], 1e15, 100);
  assert.ok(sql.prepare("SELECT 1 FROM ledger WHERE launch = 'silcord'").get(), "not yet");
  assert.throws(() => sql.exec("DELETE FROM ledger WHERE launch = 'silcord'"), /append only/);
  await s.forget(t0 + 200, 1e15, [{ ...l, opens: t0 }], 1e15, 100);
  for (const table of ["member", "invite", "ledger"]) {
    assert.equal(sql.prepare(`SELECT COUNT(*) AS n FROM ${table} WHERE launch = 'silcord'`).get()?.n, 0, table);
  }
  assert.equal((await s.member(other, "5"))?.user, "5");
  assert.deepEqual(await s.verify(), { entries: 1, broken: null, mismatches: [] });
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

// visible is what a person reads on a page: no markup, styles, scripts or
// the swan's ASCII, and nothing in bold, which is an address or a number.
const visible = (html: string) =>
  html
    .replace(/<(style|script|title|pre)[\s\S]*?<\/\1>/g, "")
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
    privacy: page(l, 1, { kind: "privacy", controller: "the people running cygnet lol", contact: "privacy@cygnet.lol" }, "k"),
  };
  for (const [name, html] of Object.entries(views)) {
    const words = visible(html);
    assert.doesNotMatch(words, /[.,;:!?'"\u2014\u2026]/, `${name} has punctuation: ${words.match(/.{0,30}[.,;:!?'"].{0,30}/)?.[0]}`);
    assert.doesNotMatch(words, /[A-Z]/, `${name} has capitals`);
  }
  assert.match(views.open, /<b data-count="1204">1204<\/b> people on the list/);
  assert.match(views.open, /data-sitekey="key"/);
  assert.match(views.mine, /#7/);
  assert.match(views.mine, /aria-label="2 of 3 left"/);
  assert.equal(views.mine.match(/<i class="on">/g)?.length, 2);
  assert.match(views.mine, /open for 2h 0m/);
  assert.match(views.mine, />used</);
  assert.match(views.mine, /taken back/);
  assert.match(views.mine, /ran out and came back to you/);
  assert.match(views.mine, /make an invite/);
  assert.match(views.invitee, /youre in through a friend/);
  for (const right of ["download", "delete", "complain", "cloudflare", "scaleway", "12 months"]) assert.match(views.privacy, new RegExp(right));
  assert.match(views.mine, /href="\/silcord\/mine">download my data/);
  assert.match(views.open, /href="\/privacy">privacy<\/a>/);
  assert.doesNotMatch(views.open, /your discord id and your place in line/, "the notice is linked, not repeated");
  assert.doesNotMatch(views.held, /turnstile\/v0|make an invite/);
  assert.match(views.sent, /<b>st\*\*\*@gm\*\*\*\.com<\/b>/);
  assert.match(views.confirm, /action="\/v\/t" class="auto"/);
  assert.match(views.confirm, /src="\/app\.js"/);
  assert.match(views.open, /<canvas class="swan" aria-hidden="true" data-t="0">/, "a cygnet on arrival");
  assert.match(views.mine, /data-t="1"/, "a swan with invites to give");
  assert.match(views.sent, /data-t="0\.55"/, "half grown on the way in");
  assert.match(views.mine, /<pre class="ascii" aria-hidden="true">[\s\S]*@[\s\S]*~/, "the still frame, swan and water");
  assert.match(views.mine, /<script type="module" src="\/app\.js">/);
  assert.match(views.sent, /<noscript><meta http-equiv="refresh"/);
  assert.match(views.error, /ran out or was already used<br>start again/);
  assert.match(page(l, 1, { kind: "in", rank: 2, held: "a <b>", released: 3, left: 3, now, invites: [] }, "key"), /a &#60;b&#62;/);
  assert.match(page(l, 1, { kind: "minted", link: "https://cygnet.lol/i/x", expires: now + inviteLife, now }, "k"), /<code>https:\/\/cygnet\.lol\/i\/x<\/code><button type="button" class="copy" data-copy="https:\/\/cygnet\.lol\/i\/x"/);
  assert.match(page(l, 1, { kind: "open" }, "k", "https://cdn.discordapp.com/icons/1/a.webp"), /<img class="icon" src="https:\/\/cdn\.discordapp\.com\/icons\/1\/a\.webp"/);
  assert.match(page(l, 1, { kind: "open" }, "k"), /<span class="icon none" aria-hidden="true">s<\/span>/);
  assert.equal(span(45_000), "45s");
  assert.equal(span(3 * 3600_000 + 12 * 60_000 + 5_000), "3h 12m");
  assert.equal(span(2 * 86400_000 + 4 * 3600_000), "2d 4h");
  assert.equal(span(86400_000 + 46_000), "1d 0h");
  assert.equal(span(12 * 60_000), "12m 0s");
  assert.equal(span(0), "0s");
  assert.equal(esc(`"'&`), "&#34;&#39;&#38;");
});

test("the swan model grows, and draws inside its frame", async () => {
  const { model, frame, text } = await import("../public/swan.js");
  const cygnet = model(0);
  const swan = model(1);
  assert.ok(swan.length / 7 > 15_000, "dense enough to read as a surface");
  const top = (p: Float32Array) => Math.max(...p.filter((_, i) => i % 7 === 1));
  assert.ok(top(swan) > top(cygnet) * 1.8, "the swan stands taller than the cygnet");
  for (let i = 3; i < swan.length; i += 7) assert.ok(Math.abs(Math.hypot(swan[i], swan[i + 1], swan[i + 2]) - 1) < 1e-3, "unit normals");
  const f = frame(swan, 96, 40, -0.55, 0.14);
  const rows = text(f, 96, 40).split("\n");
  assert.equal(rows.length, 40);
  assert.ok(rows.every((r: string) => r.length <= 96));
  assert.ok(!rows[0].trim(), "the head clears the top of the frame");
  assert.match(rows[f.horizon], /^[~-]{10}/, "the water line");
});

test("cygnet registers a launch from its server, and can't take another's name", async () => {
  const { s } = fresh();
  assert.equal(await s.launch("nope"), undefined);
  // silcord is the code's: only its own server can refresh it.
  assert.ok(!(await s.register("silcord", "175928847299117063", "x", "", t0)));
  assert.ok(await s.register("silcord", "1558585698712293378", "silcord", "https://cdn.discordapp.com/icons/1/a.webp", t0));
  const sil = await s.launch("silcord");
  assert.equal(sil?.icon, "https://cdn.discordapp.com/icons/1/a.webp");
  assert.match(sil?.line ?? "", /the front of the line/);
  // A new one needs no code, and keeps its name to its server.
  assert.ok(await s.register("wren", "175928847299117063", "Wren Club", "", t0));
  assert.equal((await s.launch("wren"))?.title, "Wren Club");
  assert.equal((await s.launch("wren"))?.minAgeDays, 30);
  assert.ok(!(await s.register("wren", "175928847299117064", "other", "", t0)));
  assert.deepEqual((await s.launches()).map((l) => l.name).sort(), ["silcord", "wren"]);
});

test("a campaign moves through its stages, and counts itself", async () => {
  const { s } = fresh();
  assert.equal((await s.launch("silcord"))?.stage, "gathering");
  assert.equal((await s.byGuild("1558585698712293378"))?.name, "silcord");
  assert.equal(await s.byGuild("1"), undefined);
  await s.join(l, "1", h("1"), null, t0);
  await s.join(l, "2", h("2"), "held", t0);
  await s.release(l, 1, 3, t0);
  const code = await s.issue(l, "1", t0);
  await s.redeem(l, await digest(code!), "3", h("3"), null, t0 + 1);
  const st = await s.stats((await s.launch("silcord"))!, t0 + 2);
  assert.deepEqual(
    { total: st.total, in_line: st.in_line, through: st.through_invites, held: st.held, released: st.released_to, made: st.invites_made, used: st.invites_used },
    { total: 3, in_line: 2, through: 1, held: 1, released: 1, made: 1, used: 1 },
  );
  await s.stage("silcord", "closed", t0 + 3);
  assert.equal((await s.launch("silcord"))?.stage, "closed");
  assert.equal((await s.launch("silcord"))?.opens, undefined, "closing starts no clock");
  await s.stage("silcord", "open", t0 + 4);
  await s.stage("silcord", "gathering", t0 + 5);
  await s.stage("silcord", "open", t0 + 6);
  assert.equal((await s.launch("silcord"))?.opens, t0 + 4, "the first opening is kept");
  const p1 = page({ ...l, stage: "closed" }, 1, { kind: "closed" }, "k");
  const p2 = page({ ...l, stage: "open" }, 1, { kind: "opened" }, "k");
  assert.match(p1, /the list is closed/);
  assert.match(p2, /open now[\s\S]*is open/);
  for (const html of [p1, p2]) assert.doesNotMatch(visible(html), /[.,;:!?'"A-Z]/);
});

test("the list: only its owner, as a tree, with holds to lift", async () => {
  const { s } = fresh();
  assert.ok(await s.register("silcord", "1558585698712293378", "silcord", "", t0, ["175928847299117063"]));
  assert.deepEqual((await s.launch("silcord"))?.admins, ["175928847299117063"]);
  await s.join(l, "11111111111111111", h("1"), null, t0);
  await s.join(l, "22222222222222222", h("2"), "someone on your connection is already on the list", t0);
  await s.release(l, 1, 2, t0);
  const code = await s.issue(l, "11111111111111111", t0);
  await s.redeem(l, await digest(code!), "33333333333333333", h("3"), null, t0 + 1);
  const rows = (await s.tree(l)) as unknown as Row[];
  const html = page({ ...l, admins: [] }, 3, { kind: "list", rows }, "k");
  assert.match(html, /3 on the list/);
  assert.match(html, /#1<\/span><a href="https:\/\/discord\.com\/users\/11111111111111111"/);
  assert.match(html, /--d:1"><span class="mark">└<\/span><a href="https:\/\/discord\.com\/users\/33333333333333333"/, "the invitee under its sponsor");
  assert.match(html, /on hold<\/span><form method="post" action="\/silcord\/lift"><input type="hidden" name="user" value="22222222222222222">/);
  await s.leave(l, "11111111111111111", t0 + 2);
  const after = page(l, 2, { kind: "list", rows: (await s.tree(l)) as unknown as Row[] }, "k");
  assert.match(after, /invited by someone who left/);
  assert.doesNotMatch(visible(after), /[,;:!?'"A-Z]/);
});

test("the home page: the name, a line, nothing to fill in, nothing for robots", () => {
  const home = page({ name: "cygnet", title: "cygnet", line: "we keep the quiet places\nlit from below like water at dusk\nand let you in by name", minAgeDays: 30, icon: "/art/cygnet.png" }, 0, { kind: "home" }, "k");
  assert.match(home, /<h1>cygnet<\/h1>/);
  assert.match(home, /nothing is open tonight/);
  assert.match(home, /<meta name="robots" content="noindex, nofollow, noarchive, noai, noimageai">/);
  assert.doesNotMatch(home, /<form|turnstile\/v0|on the list/);
  assert.doesNotMatch(visible(home), /[.,;:!?'"A-Z]/);
  assert.equal(readFileSync(new URL("../public/robots.txt", import.meta.url), "utf8"), "User-agent: *\nDisallow: /\n");
});

test("the flock in the footer: whoever linked in the last day, cygnet first", async () => {
  const { s } = fresh();
  const png = "iVBORw0KGgoAAAANSUhEUg";
  await s.bird("skua", png, "aaaa", t0);
  await s.bird("cygnet", png, "bbbb", t0 + 1);
  await s.bird("merlin", png, "cccc", t0 + 2);
  await s.bird("kite", png, "dddd", t0 - 2 * 86_400_000);
  assert.deepEqual((await s.flock(t0 + 3)).map((b) => b.name), ["cygnet", "skua", "merlin"]);
  // A bird sending again keeps its place and takes its new picture.
  await s.bird("skua", png + "A", "eeee", t0 + 4);
  assert.deepEqual((await s.flock(t0 + 5)).map((b) => `${b.name}:${b.v}`), ["cygnet:bbbb", "skua:eeee", "merlin:cccc"]);
  assert.equal(await s.art("skua"), png + "A");
  assert.equal(await s.art("wren"), null);

  assert.equal(byline([]), '<img src="/art/cygnet.png" alt="" width="14" height="14">kept by cygnet');
  assert.match(byline([{ name: "skua", v: "e" }]), /^<img src="\/art\/cygnet\.png"[^>]*><img src="\/bird\/skua\.png\?v=e"[^>]*>kept by cygnet with skua$/);
  assert.match(byline(await s.flock(t0 + 5)), /kept by cygnet with skua and merlin$/);
  assert.match(byline([{ name: "cygnet", v: "b" }, { name: "a", v: "1" }, { name: "b", v: "2" }, { name: "c", v: "3" }]), /with a b and c$/);
  assert.match(page(l, 0, { kind: "open" }, "k", "", await s.flock(t0 + 5)), /kept by cygnet with skua and merlin<a class="plink" href="\/privacy">privacy<\/a>/);

  assert.ok(isPNG(png));
  assert.ok(!isPNG("R0lGODlh") && !isPNG(""));
});
