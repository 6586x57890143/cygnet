// The rules of a launch's list, kept apart from the Worker so a test can
// run them: who may join, what counts as the same person, how long a code
// lasts, and what is forgotten when.

// A Launch is compiled in, as cygnet's layouts are.
export interface Launch {
  name: string;
  line: string;
  // minAgeDays turns away Discord accounts made just to sign up again.
  minAgeDays: number;
  // guild is the server's id; title and icon its name and picture, as
  // cygnet last sent them (store.ts, launch).
  guild?: string;
  title?: string;
  icon?: string;
  // opens is when the server opens, in ms, once it's set. Email hashes are
  // kept until keepEmail after it.
  opens?: number;
}

export const launches: Record<string, Launch> = {
  silcord: {
    name: "silcord",
    guild: "1558585698712293378",
    line: "a new discord server is on the way\nget on the list and when invites go out to the front of the line you can bring your friends in",
    minAgeDays: 30,
  },
};

// fallback is what a launch cygnet registered but the code doesn't name
// says, and how old an account has to be.
export const fallback = { line: "a new discord server is on the way\nget on the list and when invites go out to the front of the line you can bring your friends in", minAgeDays: 30 };

export const minute = 60_000;
export const hour = 60 * minute;
export const day = 24 * hour;
// inviteLife is how long an invite link works before it's back in its
// issuer's hands; proofLife, how long an emailed link does. proofs is how
// many links one Discord account can be sent in an hour.
export const inviteLife = 48 * hour;
export const proofLife = 30 * minute;
export const proofs = 5;

// budget is the most emails sent in any window days long. Scaleway sends
// 300 a month free and then charges per 1,000, so this caps the worst
// month at well under a euro, whatever anyone tries.
export const budget = { emails: 2_900, days: 31 };
// keepNetwork is how long the network and area hashes are kept; keepEmail,
// how long after a launch opens its email hashes are.
export const keepNetwork = 30 * day;
export const keepEmail = 30 * day;
// keepList is how long after a launch opens its whole list, invites and
// ledger are kept before they're deleted.
export const keepList = 365 * day;

// discordEpoch is the first ms of 2015, where Discord's IDs count from.
const discordEpoch = 1_420_070_400_000n;

// created is when a Discord account was made, read from its ID.
export function created(id: string): number {
  return Number((BigInt(id) >> 22n) + discordEpoch);
}

// normalEmail is an address as its mailbox: lowercase, without a +tag, and
// for Gmail without the dots it ignores, so one inbox is one person.
export function normalEmail(email: string): string {
  const at = email.lastIndexOf("@");
  let local = email.slice(0, at).toLowerCase().split("+")[0];
  let domain = email.slice(at + 1).toLowerCase();
  if (domain === "googlemail.com") domain = "gmail.com";
  if (domain === "gmail.com") local = local.replaceAll(".", "");
  return `${local}@${domain}`;
}

// networks is the connection a person signs up from (an IPv4 address, or
// an IPv6 /64, which is one household's) and the network around it (its
// /24, or /48).
export function networks(addr: string): { ip: string; net: string } {
  const mapped = addr.toLowerCase().match(/^::ffff:(\d+\.\d+\.\d+\.\d+)$/);
  if (mapped) addr = mapped[1];
  if (!addr.includes(":")) {
    return { ip: addr, net: addr.split(".").slice(0, 3).join(".") };
  }
  const groups = expand(addr);
  return { ip: groups.slice(0, 4).join(":"), net: groups.slice(0, 3).join(":") };
}

// expand is an IPv6 address as its eight groups, unpadded and lowercase.
function expand(addr: string): string[] {
  const [head, tail] = addr.toLowerCase().split("::");
  const left = head ? head.split(":") : [];
  const right = tail ? tail.split(":") : [];
  const fill = tail === undefined ? [] : Array(8 - left.length - right.length).fill("0");
  return [...left, ...fill, ...right].map((g) => g.replace(/^0+(?=.)/, ""));
}

// area is where a person is, roughly: their country and a half degree grid
// square (about 50 km), from what Cloudflare reads off the address. "" is
// nowhere known.
export function area(cf: { country?: string; region?: string; latitude?: string; longitude?: string } | undefined): string {
  if (!cf?.country) return "";
  const lat = Number(cf.latitude);
  const lon = Number(cf.longitude);
  if (cf.latitude && cf.longitude && Number.isFinite(lat) && Number.isFinite(lon)) {
    return `${cf.country}:${Math.floor(lat * 2)}:${Math.floor(lon * 2)}`;
  }
  return `${cf.country}:${cf.region ?? ""}`;
}

const keys = new Map<string, Promise<CryptoKey>>();

// keyed is value's HMAC under the Worker's secret, labelled so one kind of
// value never matches another: 16 bytes, base64url. Without the key the
// hash of an address can't be found by trying every address.
export async function keyed(secret: string, label: string, value: string): Promise<string> {
  let key = keys.get(secret);
  if (!key) {
    key = crypto.subtle.importKey("raw", new TextEncoder().encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
    keys.set(secret, key);
  }
  const mac = await crypto.subtle.sign("HMAC", await key, new TextEncoder().encode(`${label}\0${value}`));
  return base64url(new Uint8Array(mac).slice(0, 16));
}

export function base64url(bytes: Uint8Array): string {
  return btoa(String.fromCharCode(...bytes)).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

// held is why a new person's invites wait for an admin: someone already on
// the list signed up from the same connection, or from the same network in
// the same area. They still get their place.
export function held(sameIp: boolean, sameNetArea: boolean): string | null {
  if (sameIp) return "someone on your connection is already on the list";
  if (sameNetArea) return "someone on your network nearby is already on the list";
  return null;
}

// inviteCode is a new invite's code: 20 random bytes, base32, 32
// characters. Only its digest is stored.
export function inviteCode(): string {
  return base32(crypto.getRandomValues(new Uint8Array(20)));
}

// token is a random value for a cookie or a state: 32 bytes, base64url.
export function token(): string {
  return base64url(crypto.getRandomValues(new Uint8Array(32)));
}

// masked is an address with most of it hidden, so a page can say which
// inbox to look in without showing the address: st***@gm***.com.
export function masked(email: string): string {
  const at = email.lastIndexOf("@");
  const domain = email.slice(at + 1);
  const dot = domain.lastIndexOf(".");
  const hide = (s: string) => s.slice(0, Math.min(2, Math.max(1, s.length - 1))) + "***";
  return `${hide(email.slice(0, at))}@${hide(domain.slice(0, dot))}${domain.slice(dot)}`.toLowerCase();
}

// digest is value's SHA-256, hex: for a code with entropy of its own, which
// needs no key to stay unguessable.
export async function digest(value: string): Promise<string> {
  const h = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return [...new Uint8Array(h)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

// same compares two strings in time that doesn't depend on where they
// differ.
export function same(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return d === 0;
}

const alphabet = "abcdefghijklmnopqrstuvwxyz234567";

function base32(bytes: Uint8Array): string {
  let out = "";
  let bits = 0;
  let acc = 0;
  for (const b of bytes) {
    acc = (acc << 8) | b;
    bits += 8;
    while (bits >= 5) {
      out += alphabet[(acc >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  if (bits > 0) out += alphabet[(acc << (5 - bits)) & 31];
  return out;
}
