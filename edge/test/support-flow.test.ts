import assert from "node:assert/strict";
import { test } from "node:test";
import worker from "../src/index.ts";

test("support journey: verification, OAuth, every payment option, payment status and logout", async (t) => {
  const origin = "https://support.cygnet.lol";
  const user = "175928847299117063";
  const rails = ["solana:USDC", "solana:USDT", "base:USDC", "polygon:USDC", "tron:USDT", "solana:SOL", "base:ETH", "ethereum:ETH", "monero:XMR"];
  const offer = { rates: [500], plans: [{ months: 1, prices: { 500: 500 } }], rails: rails.map(key => ({ key, label: key })), owner: { id: user } };
  const env = {
    CYGNET_HUB_URL: "https://hub.example", CYGNET_HUB_TOKEN: "fixture", HMAC_KEY: "fixture-cookie-key",
    DISCORD_CLIENT_ID: "fixture", DISCORD_CLIENT_SECRET: "fixture", TURNSTILE_SECRET: "fixture", TURNSTILE_SITEKEY: "fixture",
    DB: { prepare() { return { bind() { return this; }, async all() { return { results: [] }; }, async first() { return null; } }; } },
  } as unknown as Env;
  const pending: Promise<unknown>[] = [];
  const ctx = { waitUntil(promise: Promise<unknown>) { pending.push(promise); } } as ExecutionContext;
  let state = "open", currentRail = "base:ETH", unavailable = false, gone = false;
  let quotes = 0, tokenExchanges = 0, revocations = 0;
  const id = "abcdefghijklmnopqrstuvwxyz";
  const checkout = () => ({ id, state, rail: currentRail, label: currentRail, amount: "0.025", units: "25000000000", to: "fixture-immutable-destination", months: 1, cents: 500, price: 500, gold: false, expires: Date.now() + 1_200_000, paid: state === "paid" ? Date.now() : 0, until: Date.now() + 2_592_000_000 });
  t.mock.method(globalThis, "fetch", async (url: string, init: RequestInit) => {
    if (url === "https://challenges.cloudflare.com/turnstile/v0/siteverify") return Response.json({ success: true, hostname: "support.cygnet.lol", action: "support_signin" });
    if (url === "https://discord.com/api/v10/oauth2/token") {
      tokenExchanges++;
      return Response.json({ access_token: "fixture-disposable-grant" });
    }
    if (url === "https://discord.com/api/v10/users/@me") return Response.json({ id: user });
    if (url === "https://discord.com/api/v10/oauth2/token/revoke") {
      revocations++;
      return new Response(null, { status: 200 });
    }
    assert.equal(url, "https://hub.example/v1/call");
    const call = JSON.parse(String(init.body));
    if (call.method === "cygnet.offer") return Response.json({ body: offer });
    assert.equal(call.body.user, user, "account identity must come from the signed session");
    if (call.method === "cygnet.supporter") return Response.json({ body: { active: state === "paid", gold: false } });
    if (call.method === "cygnet.quote") {
      quotes++;
      assert.ok(rails.includes(call.body.rail));
      assert.equal(call.body.months, 1);
      assert.equal(call.body.cents, 500);
      currentRail = call.body.rail;
      return Response.json({ body: checkout() });
    }
    assert.equal(call.method, "cygnet.checkout");
    assert.equal(call.body.id, id);
    return Response.json(unavailable ? { err: { code: "offline" } } : gone ? { err: { code: "denied", reason: "not_yours" } } : { body: checkout() });
  });
  const request = (path: string, options: RequestInit = {}) => worker.fetch(new Request(origin + path, options), env, ctx);
  const login = await request("/in", { method: "POST", headers: { origin }, body: new URLSearchParams({ "cf-turnstile-response": "fixture-token" }) });
  assert.equal(login.status, 303);
  const oauth = new URL(login.headers.get("location")!);
  assert.equal(oauth.searchParams.get("scope"), "identify");
  const stateCookie = login.headers.get("set-cookie")!.split(";")[0];
  const invalid = await request("/auth/discord?code=fixture&state=wrong-state", { headers: { cookie: stateCookie } });
  assert.equal(invalid.status, 400);
  assert.equal(tokenExchanges, 0, "a bad state must never exchange a Discord code");
  const callback = await request("/auth/discord?code=fixture&state=" + oauth.searchParams.get("state"), { headers: { cookie: stateCookie } });
  assert.equal(callback.status, 303);
  const cookies = callback.headers.getSetCookie();
  assert.ok(cookies.some(cookie => cookie.startsWith("cy_state=; Max-Age=0")));
  const cookie = cookies.find(cookie => cookie.startsWith("cy_support="))!.split(";")[0];
  await Promise.all(pending);
  assert.equal(tokenExchanges, 1);
  assert.equal(revocations, 1, "the temporary Discord grant must be revoked");
  const home = await request("/", { headers: { cookie } });
  assert.equal(home.status, 200);
  const html = await home.text();
  for (const rail of rails) {
    assert.ok(html.includes(`value="${rail}"`));
    const quote = await request("/quote", { method: "POST", headers: { origin, cookie }, body: new URLSearchParams({ months: "1", cents: "500", rail }) });
    assert.equal(quote.status, 303, rail);
    assert.equal(quote.headers.get("location"), "/" + id);
    const payment = await request("/" + id, { headers: { cookie } });
    assert.equal(payment.status, 200, rail);
    const body = await payment.text();
    assert.ok(body.includes("fixture-immutable-destination"));
    assert.ok(body.includes("0.025"));
    assert.ok(body.includes("data-countdown"));
  }
  assert.equal(quotes, rails.length);
  for (const [next, expected] of [["open", 204], ["lapsed", 204], ["paid", 200], ["void", 410]] as const) {
    state = next;
    const poll = await request("/" + id + "?check", { headers: { cookie } });
    assert.equal(poll.status, expected, next);
    assert.equal(poll.headers.get("cache-control"), "no-store");
  }
  state = "paid";
  const paid = await request("/" + id, { headers: { cookie } });
  assert.match(await paid.text(), /it's in, thank you/);
  unavailable = true;
  assert.equal((await request("/" + id + "?check", { headers: { cookie } })).status, 503);
  unavailable = false; gone = true;
  assert.equal((await request("/" + id, { headers: { cookie } })).status, 404);
  assert.equal((await request("/" + id + "?check", { headers: { cookie } })).status, 410);
  const out = await request("/out", { method: "POST", headers: { origin, cookie } });
  assert.equal(out.status, 303);
  assert.match(out.headers.get("set-cookie")!, /cy_support=; Max-Age=0/);
  assert.equal((await request("/" + id + "?check")).status, 410);
  const unauthenticated = await request("/quote", { method: "POST", headers: { origin }, body: new URLSearchParams({ months: "1", cents: "500", rail: "base:ETH" }) });
  assert.equal(unauthenticated.status, 303);
  assert.equal(unauthenticated.headers.get("location"), "/");
  assert.equal(quotes, rails.length, "a signed-out request must not create a checkout");
});
