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
// server is the page a server's link leads to, as index.ts makes it.
const server = { name: "i/c0de", title: "pond", icon: "", line: "", minAgeDays: 30, server: true };

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

function fresh(upTo = "") {
  const sql = new DatabaseSync(":memory:");
  // Every migration, in order, as D1 applies them, or those before upTo.
  const dir = new URL("../migrations/", import.meta.url);
  for (const f of readdirSync(dir).filter((n) => n.endsWith(".sql") && (!upTo || n < upTo)).sort()) sql.exec(readFileSync(new URL(f, dir), "utf8"));
  return { sql, s: new Store(d1(sql)) };
}

const h = (n: string, geo: string | null = "g") => ({ email: `e${n}`, ip: `i${n}`, net: `n${n}`, geo });
const t0 = 1_800_000_000_000;

test("a sign up: one per account and email, and who looks like someone already on", async () => {
  const { s } = fresh();
  await s.keep(l, "1", h("1"), t0);
  await s.keep(l, "3", h("3"), t0);
  // Kept again changes nothing: cygnet already said they're on.
  await s.keep(l, "1", h("9"), t0 + 1);
  assert.equal((await s.signup(l, "1"))?.email, "e1");
  assert.equal(await s.signup(l, "2"), null);
  assert.ok(await s.emailTaken(l, "e1"));
  assert.ok(!(await s.emailTaken(l, "e2")));
  assert.ok(!(await s.emailTaken({ ...l, name: "wren" }, "e1")), "each launch on its own");
  assert.deepEqual(await s.seen(l, { ip: "i1", net: "x", geo: "x" }), { ip: true, area: false });
  assert.deepEqual(await s.seen(l, { ip: "x", net: "n3", geo: "g" }), { ip: false, area: true });
  assert.deepEqual(await s.seen(l, { ip: "x", net: "n3", geo: null }), { ip: false, area: false });
  assert.equal(await s.total(l), 2);
  await s.drop(l, "1");
  assert.equal(await s.signup(l, "1"), null);
  assert.equal(await s.total(l), 1);
});

test("the move to cygnet keeps every sign up's hashes and drops the rest", async () => {
  const { sql } = fresh("0004");
  sql.exec("INSERT INTO member (launch, user, place, email, ip, net, geo, released, at) VALUES ('silcord', '1', 1, 'e1', 'i1', 'n1', 'g', 3, 5)");
  sql.exec("INSERT INTO campaign (name, stage, at) VALUES ('silcord', 'closed', 5)");
  const dir = new URL("../migrations/", import.meta.url);
  for (const f of readdirSync(dir).filter((n) => n >= "0004").sort()) sql.exec(readFileSync(new URL(f, dir), "utf8"));
  assert.deepEqual({ ...sql.prepare("SELECT * FROM signup").get() }, { launch: "silcord", user: "1", email: "e1", ip: "i1", net: "n1", geo: "g", at: 5 });
  for (const table of ["member", "invite", "ledger", "gone", "campaign"]) {
    assert.throws(() => sql.prepare(`SELECT 1 FROM ${table}`).get(), /no such table/, table);
  }
});

test("gdpr: nothing left after you go, and every hash gone in time", async () => {
  const { sql, s } = fresh();
  await s.keep(l, "1", h("1"), t0);
  await s.challenge({ id: "c", link: "k", launch: "silcord", user: "1", purpose: "issue", made: t0, expires: t0 + 1_000_000_000, email: "E" }, 5);
  await s.drop(l, "1");
  for (const table of ["signup", "challenge"]) {
    const hit = sql.prepare(`SELECT * FROM ${table}`).all().some((r) => Object.values(r as object).includes("1"));
    assert.ok(!hit, `${table} still names them`);
  }

  // The network and area go after keepNetwork, the email keepEmail after
  // the launch opens, and the rest keepList after it; another launch's stay.
  const other = { ...l, name: "wren" };
  await s.keep(l, "2", h("2"), t0);
  await s.keep(other, "5", h("5"), t0);
  const open = { ...l, opens: t0 };
  await s.forget(t0 + 10, 5, [open, other], 50, 100);
  assert.deepEqual({ ...(await s.signup(l, "2")) }, { email: "e2", ip: null, net: null, geo: null });
  await s.forget(t0 + 60, 5, [open, other], 50, 100);
  assert.equal((await s.signup(l, "2"))?.email, null);
  await s.forget(t0 + 200, 5, [open, other], 50, 100);
  assert.equal(await s.signup(l, "2"), null);
  assert.ok(await s.signup(other, "5"));
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
    sealOffered: page(l, 1, { kind: "open", seal: true }, "key"),
    sealable: page(l, 1, { kind: "in", rank: 3, held: null, released: 0, left: 0, now, invites: [], seal: true }, "key"),
    sealed: page(l, 1, { kind: "in", rank: 3, held: null, released: 0, left: 0, now, invites: [], seal: true, sealed: true }, "key"),
    turn: page(l, 1, { kind: "in", rank: 3, held: null, released: 0, left: 0, now, invites: [], admitted: true }, "key"),
    adding: page(l, 1, { kind: "in", rank: 3, held: null, released: 0, left: 0, now, invites: [], admitted: true, sealed: true }, "key"),
    held: page(l, 1, { kind: "in", rank: 2, held: "someone on your connection is already on the list", released: 3, left: 3, now, invites: [] }, "key"),
    invited: page(l, 1, { kind: "invited", code: "c", expires: now + 3_600_000, now }, "key"),
    sent: page(l, 1, { kind: "sent", to: "st***@gm***.com", expires: now + 600_000, now, what: "take your invite" }, "k"),
    confirm: page(l, 1, { kind: "confirm", token: "t" }, "k"),
    confirmed: page(l, 1, { kind: "confirmed" }, "k"),
    error: page(l, 1, { kind: "error", why: "that link ran out or was already used\nstart again from your page" }, "k"),
    privacy: page(l, 1, { kind: "privacy", controller: "the people running cygnet lol", contact: "privacy@cygnet.lol" }, "k"),
    server: page(server, 0, { kind: "server", code: "c0de", from: "steve", expires: now + 3_600_000, now }, "key"),
    serverWaiting: page(server, 0, { kind: "server", code: "c0de", from: "steve", expires: now - 1, now }, "key"),
    serverError: page(server, 0, { kind: "error", why: "you cant join this server" }, "k"),
  };
  for (const [name, html] of Object.entries(views)) {
    const words = visible(html);
    assert.doesNotMatch(words, /[.,;:!?'"\u2014\u2026]/, `${name} has punctuation: ${words.match(/.{0,30}[.,;:!?'"].{0,30}/)?.[0]}`);
    assert.doesNotMatch(words, /[A-Z]/, `${name} has capitals`);
  }
  assert.match(views.open, /<b data-count="1204">1204<\/b> people on the list/);
  assert.match(views.open, /data-sitekey="key"/);
  assert.doesNotMatch(views.open, /add me when it opens/, "no seal without cygnet's key");
  assert.match(views.sealOffered, /name="how" value="seal">count me in and add me when it opens/);
  assert.match(views.sealable, /action="\/silcord\/seal">[\s\S]*add me when it opens/);
  assert.match(views.sealable, /cf-turnstile/);
  assert.match(views.sealed, /cygnet adds you[\s\S]*action="\/silcord\/unseal"/);
  assert.doesNotMatch(views.sealed, /action="\/silcord\/seal"/);
  assert.match(views.turn, /your way in is ready[\s\S]*action="\/silcord\/in"[\s\S]*value="add">take me in[\s\S]*value="invite">send me an invite instead/);
  assert.match(views.turn, /cf-turnstile/);
  assert.ok(views.turn.indexOf("your way in is ready") < views.turn.indexOf("invites"), "the way in first");
  assert.doesNotMatch(views.turn, /invites go out from the front/);
  assert.match(views.adding, /cygnet is adding you now/);
  assert.doesNotMatch(views.adding, /action="\/silcord\/in"/);
  assert.match(views.mine, /#7/);
  assert.match(views.mine, /aria-label="2 of 3 left"/);
  assert.equal(views.mine.match(/<i class="on">/g)?.length, 2);
  assert.match(views.mine, /open for 2h 0m/);
  assert.match(views.mine, />used</);
  assert.match(views.mine, /taken back/);
  assert.match(views.mine, /ran out and came back to you/);
  assert.match(views.mine, /make an invite/);
  assert.match(views.invitee, /youre in through a friend/);
  assert.match(views.server, /steve invited you<br>this link is for one person and works for another 1h 0m/);
  assert.match(views.server, /<form method="post" action="\/i\/c0de">[\s\S]*name="how" value="add">take me in[\s\S]*name="how" value="invite">send me an invite instead/);
  assert.match(views.server, /data-sitekey="key"/);
  assert.match(views.server, /<span class="kicker">an invite<\/span><h1>pond<\/h1>/);
  assert.match(views.serverWaiting, /this link is for one person<\/p>/, "no time left to count down");
  for (const v of [views.server, views.serverError]) assert.doesNotMatch(v, /on the list|class="line"/, "a server has no line and no list");
  assert.match(views.serverError, /href="\/i\/c0de">go back/);
  assert.match(views.privacy, /links into a server/);
  assert.match(views.server, /<span class="icon none" aria-hidden="true">p<\/span>/, "the server's letter, not the link's");
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

test("a campaign's stage and opening are what cygnet last sent", async () => {
  const { s } = fresh();
  assert.equal((await s.launch("silcord"))?.stage, undefined, "the code's launch before cygnet sends it: gathering");
  await s.register("silcord", "1558585698712293378", "silcord", "", t0, [], "closed");
  assert.equal((await s.launch("silcord"))?.stage, "closed");
  assert.equal((await s.launch("silcord"))?.opens, undefined, "closing starts no clock");
  await s.register("silcord", "1558585698712293378", "silcord", "", t0, [], "open", t0 + 4);
  assert.equal((await s.launch("silcord"))?.opens, t0 + 4);
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
  // As cygnet.list answers it.
  const rows: Row[] = [
    { user: "11111111111111111", sponsor: null, place: 1, released: 2, held: null },
    { user: "22222222222222222", sponsor: null, place: 2, released: 0, held: "someone on your connection is already on the list" },
    { user: "33333333333333333", sponsor: "11111111111111111", place: 3, released: 0, held: null },
  ];
  const html = page({ ...l, admins: [] }, 3, { kind: "list", rows }, "k");
  assert.match(html, /3 on the list/);
  assert.match(html, /#1<\/span><a href="https:\/\/discord\.com\/users\/11111111111111111"/);
  assert.match(html, /--d:1"><span class="mark">└<\/span><a href="https:\/\/discord\.com\/users\/33333333333333333"/, "the invitee under its sponsor");
  assert.match(html, /on hold<\/span><form method="post" action="\/silcord\/lift"><input type="hidden" name="user" value="22222222222222222">/);
  const after = page(l, 2, { kind: "list", rows: [rows[1], { ...rows[2], sponsor: "gone" }] }, "k");
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
