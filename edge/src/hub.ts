// cygnet's hub, as cygnet.lol reaches it to redeem a server's links: the
// bird protocol's POST /v1/call (SPEC.md, Protocol), as the bird edge,
// through the Cloudflare Tunnel and past Access (docs/edge.md). cygnet
// keeps the codes; the Worker only asks.

export interface Hub {
  url: string;
  token: string;
  // Access's service token, which the tunnel's hostname answers to alone.
  accessId?: string;
  accessSecret?: string;
}

// CodeInfo is cygnet.code's answer: whether a link still works, and the
// server it lets someone into, who it's from, and when it stops working.
export interface CodeInfo {
  open: boolean;
  guild?: string;
  name?: string;
  icon?: string;
  from?: string;
  expires?: number;
}

// Redeemed is cygnet.redeem's: they're in the server now, or Discord's
// invite made for them alone.
export interface Redeemed {
  guild: string;
  added?: boolean;
  invite?: string;
}

export type Reply<T> = { ok: true; body: T } | { ok: false; code: string; reason: string };

// within is how long a call has: cygnet drops it unanswered after that.
const within = 15_000;

// hubOf is the hub env names, if it names one.
export function hubOf(env: { CYGNET_HUB_URL?: string; CYGNET_HUB_TOKEN?: string; CF_ACCESS_CLIENT_ID?: string; CF_ACCESS_CLIENT_SECRET?: string }): Hub | null {
  if (!env.CYGNET_HUB_URL || !env.CYGNET_HUB_TOKEN) return null;
  return { url: env.CYGNET_HUB_URL, token: env.CYGNET_HUB_TOKEN, accessId: env.CF_ACCESS_CLIENT_ID, accessSecret: env.CF_ACCESS_CLIENT_SECRET };
}

// call asks cygnet method with body. A refusal comes back as cygnet's
// error; a hub that doesn't answer, or not as one, as offline.
export async function call<T>(hub: Hub, method: string, body: unknown, f: typeof fetch = fetch, now = Date.now()): Promise<Reply<T>> {
  const headers: Record<string, string> = { authorization: `Bearer ${hub.token}`, "content-type": "application/json" };
  if (hub.accessId && hub.accessSecret) {
    headers["cf-access-client-id"] = hub.accessId;
    headers["cf-access-client-secret"] = hub.accessSecret;
  }
  try {
    const r = await f(`${hub.url.replace(/\/+$/, "")}/v1/call`, {
      method: "POST",
      headers,
      body: JSON.stringify({ v: 1, kind: "call", method, body, deadline: now + within }),
      signal: AbortSignal.timeout(within + 1_000),
    });
    if (!r.ok) return { ok: false, code: "offline", reason: `hub answered ${r.status}` };
    const reply = (await r.json()) as { body?: T; err?: { code: string; reason?: string } };
    if (reply.err) return { ok: false, code: reply.err.code, reason: reply.err.reason ?? "" };
    return { ok: true, body: reply.body as T };
  } catch (err) {
    return { ok: false, code: "offline", reason: err instanceof Error ? err.message : "unknown" };
  }
}
