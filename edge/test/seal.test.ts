// The seal against RFC 9180's own test vectors (A.1.1: DHKEM(X25519,
// HKDF-SHA256), HKDF-SHA256, AES-128-GCM, base mode), and the sealed form
// cygnet opens (internal/modules/launch, TestOpensWhatTheWorkerSealed).
import assert from "node:assert/strict";
import { test } from "node:test";

import { schedule, seal, sealed } from "../src/seal.ts";

const hex = (s: string) => Uint8Array.from(s.match(/../g)!.map((b) => parseInt(b, 16)));
const toHex = (b: Uint8Array) => [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
const b64url = (b: Uint8Array) => btoa(String.fromCharCode(...b)).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");

const v = {
  info: hex("4f6465206f6e2061204772656369616e2055726e"),
  skEm: hex("52c4a758a802cd8b936eceea314432798d5baf2d7e9235dc084ab1b9cfa2f736"),
  pkEm: hex("37fda3567bdbd628e88668c3c8d7e97d1d1253b6d4ea6d44c150f741f1bf4431"),
  pkRm: hex("3948cfe0ad1ddb695d780e59077195da6c56506b027329794ab02bca80815c4d"),
  shared: hex("fe0e18c9f024ce43799ae393c7e8fe8fce9d218875e8227b0187c04e7d2ea1fc"),
  key: "4531685d41d65f03dc48f6b8302c05b0", // gitleaks:allow: RFC 9180 A.1.1 test vector
  nonce: "56d890e5accaaf011cff4b7d",
  pt: hex("4265617574792069732074727574682c20747275746820626561757479"),
  aad: hex("436f756e742d30"),
  ct: "f938558b5d72f1a23810b4be2ab4f84331acc02fc97babc53a52ae8218a355a96d8770ac83d07bea87e13c512a",
};

test("the key schedule is RFC 9180's", async () => {
  const { key, nonce } = await schedule(v.shared, v.info);
  assert.equal(toHex(key), v.key);
  assert.equal(toHex(nonce), v.nonce);
});

test("a seal with the RFC's ephemeral key is the RFC's ciphertext", async () => {
  const eph = {
    privateKey: await crypto.subtle.importKey("jwk", { kty: "OKP", crv: "X25519", d: b64url(v.skEm), x: b64url(v.pkEm) }, { name: "X25519" }, true, ["deriveBits"]),
    publicKey: await crypto.subtle.importKey("raw", v.pkEm, { name: "X25519" }, true, []),
  };
  const out = await seal(v.pkRm, v.pt, v.info, v.aad, eph);
  assert.equal(toHex(out.slice(0, 32)), toHex(v.pkEm), "the encapsulated key first");
  assert.equal(toHex(out.slice(32)), v.ct);
});

test("a sealed token is new each time, and as long as it should be", async () => {
  const pk = b64url(v.pkRm);
  const a = await sealed(pk, "a refresh token");
  const b = await sealed(pk, "a refresh token");
  assert.notEqual(a, b, "a fresh ephemeral key each time");
  assert.match(a, /^[A-Za-z0-9_-]+$/);
  assert.equal(Math.ceil(((32 + 15 + 16) * 4) / 3), a.length, "enc, the token, and the tag");
});
