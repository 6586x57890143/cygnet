// DKIM (RFC 6376, and RFC 8463 for Ed25519), checked here because
// Cloudflare's inbound check accepts a message that passes SPF or DKIM for
// any domain, which doesn't say the From address is real. This says it is:
// a signature from the From address's own domain, over the From header and
// the whole body, verified with the key that domain publishes.

// Lookup is a TXT record's text, its strings joined, or null.
export type Lookup = (name: string) => Promise<string | null>;

// signer is the From address of raw, lowercase, if a DKIM signature on it
// from that address's domain (or a domain above it) verifies; otherwise
// null. A message with more than one From is refused outright: which one a
// client shows is up to the client.
export async function signer(raw: string, lookup: Lookup, now: number): Promise<string | null> {
  raw = raw.replace(/\r?\n/g, "\r\n");
  const split = raw.indexOf("\r\n\r\n");
  if (split < 0) return null;
  const headers = parse(raw.slice(0, split + 2));
  const body = raw.slice(split + 4);
  const froms = headers.filter((h) => h.name === "from");
  if (froms.length !== 1) return null;
  const from = address(value(froms[0].raw));
  if (!from) return null;
  const domain = from.slice(from.lastIndexOf("@") + 1);
  for (const sig of headers.filter((h) => h.name === "dkim-signature")) {
    if (await verifies(sig.raw, headers, body, domain, lookup, now)) return from;
  }
  return null;
}

interface Header {
  name: string;
  raw: string; // as it came, continuation lines and final CRLF included
}

export function parse(block: string): Header[] {
  const out: Header[] = [];
  for (const line of block.split(/(?<=\r\n)/)) {
    if (!line) continue;
    if ((line[0] === " " || line[0] === "\t") && out.length) out[out.length - 1].raw += line;
    else out.push({ name: line.slice(0, line.indexOf(":")).trim().toLowerCase(), raw: line });
  }
  return out;
}

const value = (raw: string) => raw.slice(raw.indexOf(":") + 1);

// address is the one address in a From value, lowercase.
export function address(v: string): string | null {
  const angle = v.match(/<([^<>\s]+@[^<>\s]+)>/g);
  const found = angle ? angle.map((a) => a.slice(1, -1)) : (v.match(/[^\s<>,;:"]+@[^\s<>,;:"]+/g) ?? []);
  return found.length === 1 ? found[0].toLowerCase() : null;
}

function tags(v: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const part of v.split(";")) {
    const at = part.indexOf("=");
    if (at < 0) continue;
    out.set(part.slice(0, at).trim(), part.slice(at + 1).replace(/\s+/g, ""));
  }
  return out;
}

async function verifies(sigRaw: string, headers: Header[], body: string, domain: string, lookup: Lookup, now: number): Promise<boolean> {
  const t = tags(value(sigRaw));
  const a = t.get("a");
  const d = t.get("d")?.toLowerCase();
  const s = t.get("s");
  const h = t.get("h");
  const bh = t.get("bh");
  const b = t.get("b");
  if (t.get("v") !== "1" || (a !== "rsa-sha256" && a !== "ed25519-sha256") || !d || !s || !h || !bh || !b) return false;
  // A signature over part of the body lets anything be appended after it.
  if (t.has("l")) return false;
  if (t.has("x") && Number(t.get("x")) * 1000 < now) return false;
  if (!d.includes(".") || (domain !== d && !domain.endsWith("." + d))) return false;
  const signed = h.toLowerCase().split(":");
  if (!signed.includes("from")) return false;
  const [hc, bc = "simple"] = (t.get("c") ?? "simple/simple").toLowerCase().split("/");
  if (!["simple", "relaxed"].includes(hc) || !["simple", "relaxed"].includes(bc)) return false;

  if (b64(new Uint8Array(await sha256(canonBody(body, bc)))) !== bh) return false;

  const data = canonHeaders(headers, signed, hc) + canonHeader(stripB(sigRaw), hc).replace(/\r\n$/, "");
  const key = await lookup(`${s}._domainkey.${d}`);
  if (!key) return false;
  const kt = tags(key);
  const p = kt.get("p");
  if (!p) return false;
  const sig = unb64(b);
  try {
    if (a === "rsa-sha256") {
      if ((kt.get("k") ?? "rsa") !== "rsa") return false;
      const k = await crypto.subtle.importKey("spki", unb64(p), { name: "RSASSA-PKCS1-v1_5", hash: "SHA-256" }, false, ["verify"]);
      return await crypto.subtle.verify("RSASSA-PKCS1-v1_5", k, sig, new TextEncoder().encode(data));
    }
    if (kt.get("k") !== "ed25519") return false;
    const k = await crypto.subtle.importKey("raw", unb64(p), { name: "Ed25519" }, false, ["verify"]);
    return await crypto.subtle.verify("Ed25519", k, sig, await sha256(data));
  } catch {
    return false;
  }
}

// canonHeaders is the headers h names, each taken from the bottom up as
// RFC 6376 5.4.2 says, so a header added above a signed one isn't what
// was signed. A name with no instance left adds nothing.
export function canonHeaders(headers: Header[], names: string[], c: string): string {
  const used = new Map<string, number>();
  let out = "";
  for (const name of names) {
    const all = headers.filter((x) => x.name === name);
    const n = used.get(name) ?? 0;
    const pick = all[all.length - 1 - n];
    used.set(name, n + 1);
    if (pick) out += canonHeader(pick.raw, c);
  }
  return out;
}

export function canonHeader(raw: string, c: string): string {
  if (c === "simple") return raw.endsWith("\r\n") ? raw : raw + "\r\n";
  const at = raw.indexOf(":");
  const name = raw.slice(0, at).trim().toLowerCase();
  const v = raw
    .slice(at + 1)
    .replace(/\r\n(?=[ \t])/g, "")
    .replace(/\r\n$/, "")
    .replace(/[ \t]+/g, " ")
    .trim();
  return `${name}:${v}\r\n`;
}

export function canonBody(body: string, c: string): string {
  if (c === "relaxed") {
    const b = body
      .split("\r\n")
      .map((l) => l.replace(/[ \t]+/g, " ").replace(/ $/, ""))
      .join("\r\n")
      .replace(/(\r\n)+$/, "");
    return b ? b + "\r\n" : "";
  }
  return body.replace(/(\r\n)+$/, "") + "\r\n";
}

// stripB is the signature header with its b= value taken out, as it was
// when it was signed.
export function stripB(raw: string): string {
  return raw.replace(/([;\s:]b=)[^;]*/, "$1");
}

// doh looks a TXT record up over Cloudflare's DNS over HTTPS.
export const doh: Lookup = async (name) => {
  const r = await fetch(`https://cloudflare-dns.com/dns-query?name=${encodeURIComponent(name)}&type=TXT`, { headers: { accept: "application/dns-json" } });
  if (!r.ok) return null;
  const j = await r.json<{ Answer?: { type: number; data: string }[] }>();
  for (const a of j.Answer ?? []) {
    if (a.type !== 16) continue;
    const text = (a.data.match(/"((?:[^"\\]|\\.)*)"/g) ?? [a.data]).map((q) => q.replace(/^"|"$/g, "")).join("");
    if (/(^|;)\s*p=/.test(text)) return text;
  }
  return null;
};

const sha256 = (s: string) => crypto.subtle.digest("SHA-256", new TextEncoder().encode(s));

function b64(bytes: Uint8Array): string {
  return btoa(String.fromCharCode(...bytes));
}

function unb64(s: string): Uint8Array<ArrayBuffer> {
  return Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
}
