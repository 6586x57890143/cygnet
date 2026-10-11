// The rules of a launch's list, kept apart from the Worker so a test can
// run them: who may join, what counts as the same person, how long a code
// lasts, and what is forgotten when.

// A Launch is compiled in, as cygnet's layouts are.
export interface Launch {
  name: string;
  line: string;
  // minAgeDays turns away Discord accounts made just to sign up again.
  minAgeDays: number;
  // opens is when the server opens, in ms, once it's set. Email hashes are
  // kept until keepEmail after it.
  opens?: number;
}

export const launches: Record<string, Launch> = {
  silcord: {
    name: "silcord",
    line: "a new discord server, opening soon; get in line now, and when invites are released to the front of the line you can bring friends",
    minAgeDays: 30,
  },
};

export const minute = 60_000;
export const hour = 60 * minute;
export const day = 24 * hour;
// inviteLife is how long an invite link works before it's back in its
// issuer's hands; proofLife, how long someone has to send the email that
// proves their address. proofs is how many of those one Discord account
// can start in an hour.
export const inviteLife = 48 * hour;
export const proofLife = 30 * minute;
export const proofs = 5;
// keepNetwork is how long the network and area hashes are kept; keepEmail,
// how long after a launch opens its email hashes are.
export const keepNetwork = 30 * day;
export const keepEmail = 30 * day;

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

// mailbox is a one time address's local part: "v-" and 16 random bytes,
// base32, so it can't be guessed and fits any mail client.
export function mailbox(): string {
  return "v-" + base32(crypto.getRandomValues(new Uint8Array(16)));
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
