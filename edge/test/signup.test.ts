// The list's rules, and its SQL run against SQLite, which D1 is.
import { readFileSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";
import { test } from "node:test";
import assert from "node:assert/strict";

import { area, created, held, keyed, networks, normalEmail, sql } from "../src/signup.ts";
import { esc, page } from "../src/page.ts";
import { launches } from "../src/signup.ts";

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

test("hashes are keyed and labelled", async () => {
  const a = await keyed("k", "ip", "203.0.113.7");
  assert.equal(a.length, 22);
  assert.equal(a, await keyed("k", "ip", "203.0.113.7"));
  assert.notEqual(a, await keyed("k", "net", "203.0.113.7"));
  assert.notEqual(a, await keyed("other", "ip", "203.0.113.7"));
});

test("held says why, or nothing", () => {
  assert.match(held(true, true) ?? "", /connection/);
  assert.match(held(false, true) ?? "", /network nearby/);
  assert.equal(held(false, false), null);
});

test("the list", () => {
  const db = new DatabaseSync(":memory:");
  db.exec(readFileSync(new URL("../migrations/0001_signup.sql", import.meta.url), "utf8"));
  const q = (s: string) => db.prepare(s);
  const join = (user: string, email: string, ip: string, net = "n-" + ip, geo: string | null = "g") =>
    q(sql.join).get("silcord", user, email, ip, net, geo, 3, null, Date.now()) as { place: number } | undefined;

  assert.equal(join("1", "e1", "i1")?.place, 1);
  assert.equal(join("2", "e2", "i2")?.place, 2);
  // The same account, or the same email, gets no second place.
  assert.equal(join("1", "e9", "i9"), undefined);
  assert.equal(join("3", "e1", "i3"), undefined);
  // Another launch counts from one.
  assert.equal((q(sql.join).get("other", "1", "e1", "i1", "n", "g", 3, null, 0) as { place: number }).place, 1);

  const seen = (ip: string, net: string, geo: string | null) => q(sql.seen).get("silcord", ip, net, geo) as { ip: number; area: number };
  assert.deepEqual({ ...seen("i1", "x", "x") }, { ip: 1, area: 0 });
  assert.deepEqual({ ...seen("x", "n-i2", "g") }, { ip: 0, area: 1 });
  assert.deepEqual({ ...seen("x", "n-i2", null) }, { ip: 0, area: 0 });

  // A place is counted from who's still in line.
  assert.equal(join("4", "e4", "i4")?.place, 3);
  q(sql.leave).run("silcord", "2");
  assert.equal((q(sql.mine).get("silcord", "4") as { rank: number }).rank, 2);
  assert.equal((q(sql.total).get("silcord") as { n: number }).n, 2);

  // Forgetting leaves the place and the account.
  q(sql.forgetNetwork).run(Date.now() + 1);
  q(sql.forgetEmail).run("silcord");
  const row = db.prepare("SELECT user, email, ip, net, geo FROM signup WHERE launch = 'silcord' AND user = '1'").get();
  assert.deepEqual({ ...row }, { user: "1", email: null, ip: null, net: null, geo: null });
  assert.equal((q(sql.mine).get("silcord", "1") as { rank: number }).rank, 1);
});

test("the page says what it should, escaped", () => {
  const l = launches.silcord;
  const open = page(l, 1204, { kind: "open" }, "key");
  assert.match(open, /1,204<\/b> people in line/);
  assert.match(open, /cf-turnstile" data-sitekey="key"/);
  const mine = page(l, 1, { kind: "in", rank: 7, invites: 3, held: "a <b>" }, "key");
  assert.match(mine, /#7/);
  assert.match(mine, /▰▰▰/);
  assert.match(mine, /a &#60;b&#62;/);
  assert.doesNotMatch(mine, /turnstile\/v0/);
  assert.equal(esc(`"'&`), "&#34;&#39;&#38;");
});
