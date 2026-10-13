// Sealing a sign in to cygnet (SPEC.md, Sealed until launch): someone who
// asks to be added when their launch opens leaves a Discord refresh token,
// which the Worker encrypts to cygnet's public key and can't open again.
// HPKE (RFC 9180) in its base mode, single shot, with DHKEM(X25519,
// HKDF-SHA256), HKDF-SHA256 and AES-128-GCM: what cygnet opens with Go's
// crypto/hpke. The output is the encapsulated key and then the ciphertext.

const te = new TextEncoder();
const kem = Uint8Array.of(0x4b, 0x45, 0x4d, 0x00, 0x20); // "KEM" || 0x0020
const suite = Uint8Array.of(0x48, 0x50, 0x4b, 0x45, 0x00, 0x20, 0x00, 0x01, 0x00, 0x01); // "HPKE" || kem || kdf || aead
const v1 = te.encode("HPKE-v1");

// info binds a sealed token to what it's for.
export const info = te.encode("cygnet.lol sealed sign in v1");

const cat = (...parts: Uint8Array[]) => {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
};

async function hmac(key: Uint8Array, data: Uint8Array): Promise<Uint8Array> {
  // An empty salt is HashLen zeros (RFC 5869), which HMAC pads to the same.
  const k = await crypto.subtle.importKey("raw", key.length ? key : new Uint8Array(32), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return new Uint8Array(await crypto.subtle.sign("HMAC", k, data));
}

const extract = (id: Uint8Array, salt: Uint8Array, label: string, ikm: Uint8Array) => hmac(salt, cat(v1, id, te.encode(label), ikm));

// expand is LabeledExpand for at most one hash's length, all this needs.
async function expand(id: Uint8Array, prk: Uint8Array, label: string, ctx: Uint8Array, n: number): Promise<Uint8Array> {
  const t = await hmac(prk, cat(Uint8Array.of(0, n), v1, id, te.encode(label), ctx, Uint8Array.of(1)));
  return t.slice(0, n);
}

// schedule is the key and nonce a shared secret gives, in base mode.
export async function schedule(shared: Uint8Array, inf: Uint8Array): Promise<{ key: Uint8Array; nonce: Uint8Array }> {
  const none = new Uint8Array(0);
  const ctx = cat(Uint8Array.of(0), await extract(suite, none, "psk_id_hash", none), await extract(suite, none, "info_hash", inf));
  const secret = await extract(suite, shared, "secret", none);
  return { key: await expand(suite, secret, "key", ctx, 16), nonce: await expand(suite, secret, "base_nonce", ctx, 12) };
}

// seal encrypts plaintext to the X25519 public key pkR (32 raw bytes). A
// test may hand it the ephemeral key pair.
export async function seal(pkR: Uint8Array, plaintext: Uint8Array, inf = info, aad = new Uint8Array(0), eph?: CryptoKeyPair): Promise<Uint8Array> {
  const e = eph ?? ((await crypto.subtle.generateKey({ name: "X25519" }, true, ["deriveBits"])) as CryptoKeyPair);
  const recipient = await crypto.subtle.importKey("raw", pkR, { name: "X25519" }, false, []);
  // Workers' types spell the standard `public` as `$public`; the runtime
  // takes `public`, as the spec has it.
  const ecdh = { name: "X25519", public: recipient } as unknown as SubtleCryptoDeriveKeyAlgorithm;
  const dh = new Uint8Array(await crypto.subtle.deriveBits(ecdh, e.privateKey, 256));
  const enc = new Uint8Array((await crypto.subtle.exportKey("raw", e.publicKey)) as ArrayBuffer);
  const shared = await expand(kem, await extract(kem, new Uint8Array(0), "eae_prk", dh), "shared_secret", cat(enc, pkR), 32);
  const { key, nonce } = await schedule(shared, inf);
  const k = await crypto.subtle.importKey("raw", key, { name: "AES-GCM" }, false, ["encrypt"]);
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce, additionalData: aad }, k, plaintext));
  return cat(enc, ct);
}

// sealed is a refresh token sealed to cygnet's public key (base64url, as
// CYGNET_SEAL_PUBLIC holds it), in base64url.
export async function sealed(publicKey: string, token: string): Promise<string> {
  const pk = Uint8Array.from(atob(publicKey.replaceAll("-", "+").replaceAll("_", "/")), (c) => c.charCodeAt(0));
  const out = await seal(pk, te.encode(token));
  return btoa(String.fromCharCode(...out)).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}
