// The rules of a launch's list, kept apart from the Worker so a test can
// run them: who may join, what counts as the same person, and what is
// forgotten when.

// A Launch is compiled in, as cygnet's layouts are.
export interface Launch {
  name: string;
  line: string;
  // invites is how many invites each person on the list has reserved for
  // after the launch.
  invites: number;
  // minAgeDays turns away Discord accounts made just to sign up again.
  minAgeDays: number;
  // opens is when the server opens, in ms, once it's set. Email hashes are
  // kept until keepEmail after it.
  opens?: number;
}

export const launches: Record<string, Launch> = {
  silcord: {
    name: "silcord",
    line: "a new discord server, opening soon; get in line now and you'll have invites of your own to bring friends when it opens",
    invites: 3,
    minAgeDays: 30,
  },
};

export const day = 86_400_000;
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

// held is why a new person's invites wait for an admin: someone already in
// line signed up from the same connection, or from the same network in the
// same area. They still get their place.
export function held(sameIp: boolean, sameNetArea: boolean): string | null {
  if (sameIp) return "someone on your connection is already in line";
  if (sameNetArea) return "someone on your network nearby is already in line";
  return null;
}

// The statements, kept here so the test runs the same SQL as D1.
export const sql = {
  // join takes the next place, or nothing if this Discord account or this
  // email is already in line. One statement, so two at once can't take the
  // same place.
  join: `INSERT INTO signup (launch, place, user, email, ip, net, geo, invites, held, at)
    SELECT ?1, COALESCE(MAX(place), 0) + 1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9 FROM signup WHERE launch = ?1
    ON CONFLICT DO NOTHING
    RETURNING place`,
  // seen is whether anyone in line shares this connection, or this network
  // and area.
  seen: `SELECT
    EXISTS (SELECT 1 FROM signup WHERE launch = ?1 AND ip = ?2) AS ip,
    EXISTS (SELECT 1 FROM signup WHERE launch = ?1 AND net = ?3 AND geo = ?4) AS area`,
  // mine is a person's place as it is now, counting only who's still in
  // line ahead of them.
  mine: `SELECT s.invites, s.held,
    (SELECT COUNT(*) FROM signup t WHERE t.launch = s.launch AND t.place <= s.place) AS rank
    FROM signup s WHERE s.launch = ?1 AND s.user = ?2`,
  total: `SELECT COUNT(*) AS n FROM signup WHERE launch = ?1`,
  leave: `DELETE FROM signup WHERE launch = ?1 AND user = ?2`,
  forgetNetwork: `UPDATE signup SET ip = NULL, net = NULL, geo = NULL
    WHERE at < ?1 AND (ip IS NOT NULL OR net IS NOT NULL OR geo IS NOT NULL)`,
  forgetEmail: `UPDATE signup SET email = NULL WHERE launch = ?1 AND email IS NOT NULL`,
};
