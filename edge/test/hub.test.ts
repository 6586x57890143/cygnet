// The hub as cygnet.lol calls it: the frame it sends, the headers that get
// it past Access, and every way an answer comes back.
import assert from "node:assert/strict";
import { test } from "node:test";

import { call, hubOf, type CodeInfo } from "../src/hub.ts";

const hub = { url: "https://hub.example/", token: "t".repeat(64), accessId: "id.access", accessSecret: "secret" };

// answering is a fetch that records the request and answers with status and
// body, or throws when body is an Error.
function answering(status: number, body: unknown) {
  const sent: { url: string; init: RequestInit }[] = [];
  const f = (async (url: string, init: RequestInit) => {
    sent.push({ url, init });
    if (body instanceof Error) throw body;
    return new Response(JSON.stringify(body), { status });
  }) as unknown as typeof fetch;
  return { f, sent };
}

test("a call is a frame from edge, with its deadline, past Access", async () => {
  const { f, sent } = answering(200, { v: 1, kind: "reply", body: { open: true, name: "pond" } });
  const r = await call<CodeInfo>(hub, "cygnet.code", { code: "ab" }, f, 1_000);
  assert.deepEqual(r, { ok: true, body: { open: true, name: "pond" } });
  assert.equal(sent[0].url, "https://hub.example/v1/call");
  const h = sent[0].init.headers as Record<string, string>;
  assert.equal(h.authorization, `Bearer ${hub.token}`);
  assert.equal(h["cf-access-client-id"], "id.access");
  assert.equal(h["cf-access-client-secret"], "secret");
  assert.deepEqual(JSON.parse(String(sent[0].init.body)), { v: 1, kind: "call", method: "cygnet.code", body: { code: "ab" }, deadline: 16_000 });
});

test("a refusal, and a hub that doesn't answer", async () => {
  assert.deepEqual(await call(hub, "cygnet.redeem", {}, answering(200, { err: { code: "denied", reason: "young" } }).f), { ok: false, code: "denied", reason: "young" });
  assert.deepEqual(await call(hub, "cygnet.redeem", {}, answering(200, { err: { code: "busy" } }).f), { ok: false, code: "busy", reason: "" });
  const down = await call(hub, "cygnet.redeem", {}, answering(502, "bad gateway").f);
  assert.equal(down.ok || down.code, "offline");
  const gone = await call(hub, "cygnet.redeem", {}, answering(0, new Error("connect refused")).f);
  assert.deepEqual(gone, { ok: false, code: "offline", reason: "connect refused" });
});

test("no Access token, no Access headers; no hub, none", async () => {
  const { f, sent } = answering(200, { body: {} });
  await call({ url: "http://cygnet:7700", token: "t" }, "cygnet.ping", {}, f);
  assert.equal((sent[0].init.headers as Record<string, string>)["cf-access-client-id"], undefined);
  assert.equal(hubOf({}), null);
  assert.equal(hubOf({ CYGNET_HUB_URL: "https://hub.example" }), null);
  assert.deepEqual(hubOf({ CYGNET_HUB_URL: "u", CYGNET_HUB_TOKEN: "t", CF_ACCESS_CLIENT_ID: "i", CF_ACCESS_CLIENT_SECRET: "s" }), { url: "u", token: "t", accessId: "i", accessSecret: "s" });
});
