// cygnet.lol: the one public surface cygnet has (SPEC.md, Principles 6).
// A launch's page, its sign-in with Discord, and the list it keeps in D1.
// The bot token never comes here; the Discord app's OAuth secret does, to
// read who signed in, and their token is revoked as soon as it's read.

import { page, type View } from "./page.ts";
import { area, created, day, held, keepEmail, keepNetwork, keyed, launches, networks, normalEmail, sql, type Launch } from "./signup.ts";

interface Env {
  DB: D1Database;
  DISCORD_CLIENT_ID: string;
  DISCORD_CLIENT_SECRET: string;
  TURNSTILE_SITEKEY: string;
  TURNSTILE_SECRET: string;
  HMAC_KEY: string;
}

const api = "https://discord.com/api/v10";
// stay is how long the page remembers who you are.
const stay = 30 * day;

export default {
  async fetch(req, env, ctx): Promise<Response> {
    const url = new URL(req.url);
    const [, first, second] = url.pathname.split("/");
    try {
      if (first === "auth" && second === "discord" && req.method === "GET") return await callback(req, env, ctx, url);
      const l = launches[first];
      if (!l) return new Response("nothing here", { status: 404, headers: secure({ "content-type": "text/plain; charset=utf-8" }) });
      if (second === undefined && req.method === "GET") return await landing(req, env, l);
      if (second === "join" && req.method === "POST") return await join(req, env, l, url);
      if (second === "leave" && req.method === "POST") return await leave(req, env, l);
      return new Response("not allowed", { status: 405, headers: secure({ "content-type": "text/plain; charset=utf-8" }) });
    } catch (err) {
      console.error("signup", err instanceof Error ? err.message : "unknown");
      const l = launches[first] ?? launches.silcord;
      return html(env, l, { kind: "error", why: "something went wrong on cygnet's side; try again in a moment" }, 500);
    }
  },

  // Forgets what was only kept to stop double sign ups, once it's done
  // that job.
  async scheduled(_event, env): Promise<void> {
    const now = Date.now();
    await env.DB.prepare(sql.forgetNetwork).bind(now - keepNetwork).run();
    for (const l of Object.values(launches)) {
      if (l.opens && now > l.opens + keepEmail) await env.DB.prepare(sql.forgetEmail).bind(l.name).run();
    }
  },
} satisfies ExportedHandler<Env>;

async function landing(req: Request, env: Env, l: Launch): Promise<Response> {
  const user = await who(req, env, l);
  if (user) {
    const row = await env.DB.prepare(sql.mine).bind(l.name, user).first<{ invites: number; held: string | null; rank: number }>();
    if (row) return html(env, l, { kind: "in", rank: row.rank, invites: row.invites, held: row.held });
  }
  return html(env, l, { kind: "open" });
}

// join checks Turnstile, then sends the person to Discord with a state
// only this browser holds.
async function join(req: Request, env: Env, l: Launch, url: URL): Promise<Response> {
  const form = await req.formData();
  const ok = await fetch("https://challenges.cloudflare.com/turnstile/v0/siteverify", {
    method: "POST",
    body: new URLSearchParams({
      secret: env.TURNSTILE_SECRET,
      response: String(form.get("cf-turnstile-response") ?? ""),
      remoteip: req.headers.get("cf-connecting-ip") ?? "",
    }),
  }).then((r) => r.json<{ success: boolean }>());
  if (!ok.success) return html(env, l, { kind: "error", why: "the check that you're a person didn't go through; try again" }, 403);
  const state = `${l.name}.${crypto.randomUUID()}`;
  const to = new URL("https://discord.com/oauth2/authorize");
  to.search = new URLSearchParams({
    client_id: env.DISCORD_CLIENT_ID,
    response_type: "code",
    redirect_uri: `https://${url.host}/auth/discord`,
    scope: "identify email",
    state,
    prompt: "none",
  }).toString();
  return new Response(null, {
    status: 303,
    headers: secure({ location: to.toString(), "set-cookie": cookie("cy_state", await sign(env, state), 600) }),
  });
}

async function callback(req: Request, env: Env, ctx: ExecutionContext, url: URL): Promise<Response> {
  const state = url.searchParams.get("state") ?? "";
  const l = launches[state.split(".")[0]];
  if (!l) return new Response("nothing here", { status: 404, headers: secure({ "content-type": "text/plain; charset=utf-8" }) });
  if ((await unsign(env, read(req, "cy_state"))) !== state) {
    return html(env, l, { kind: "error", why: "that sign in started in another browser or took too long; start again here" }, 400);
  }
  const code = url.searchParams.get("code");
  if (!code) return back(l);

  const basic = `Basic ${btoa(`${env.DISCORD_CLIENT_ID}:${env.DISCORD_CLIENT_SECRET}`)}`;
  const grant = await fetch(`${api}/oauth2/token`, {
    method: "POST",
    headers: { authorization: basic, "content-type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({ grant_type: "authorization_code", code, redirect_uri: `https://${url.host}/auth/discord` }),
  });
  if (!grant.ok) return html(env, l, { kind: "error", why: "discord didn't confirm that sign in; try again" }, 502);
  const { access_token } = await grant.json<{ access_token: string }>();
  const me = await fetch(`${api}/users/@me`, { headers: { authorization: `Bearer ${access_token}` } });
  // Read once, then given back: cygnet keeps no Discord token for a list.
  ctx.waitUntil(
    fetch(`${api}/oauth2/token/revoke`, {
      method: "POST",
      headers: { authorization: basic, "content-type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ token: access_token, token_type_hint: "access_token" }),
    }),
  );
  if (!me.ok) return html(env, l, { kind: "error", why: "discord didn't say who you are; try again in a moment" }, 502);
  const user = await me.json<{ id: string; email?: string | null; verified?: boolean }>();

  const signedIn = async () =>
    new Response(null, { status: 303, headers: secure({ location: `/${l.name}`, "set-cookie": cookie(`cy_${l.name}`, await sign(env, `${l.name}.${user.id}.${Date.now() + stay}`), stay / 1000) }) });
  if (await env.DB.prepare(sql.mine).bind(l.name, user.id).first()) return signedIn();

  if (!user.email || !user.verified) {
    return html(env, l, { kind: "error", why: "your discord account needs a verified email first; verify it in discord's settings, then try again" }, 403);
  }
  if (Date.now() - created(user.id) < l.minAgeDays * day) {
    return html(env, l, { kind: "error", why: `discord accounts have to be at least ${l.minAgeDays} days old to get in line` }, 403);
  }

  const net = networks(req.headers.get("cf-connecting-ip") ?? "");
  const where = area(req.cf as { country?: string; region?: string; latitude?: string; longitude?: string } | undefined);
  const [email, ip, near, geo] = await Promise.all([
    keyed(env.HMAC_KEY, "email", normalEmail(user.email)),
    keyed(env.HMAC_KEY, "ip", net.ip),
    keyed(env.HMAC_KEY, "net", net.net),
    where ? keyed(env.HMAC_KEY, "geo", where) : Promise.resolve(null),
  ]);
  // ponytail: seen then join is two statements, so two sign ups from one
  // connection in the same instant can both keep their invites; a batch
  // if that's ever seen.
  const seen = await env.DB.prepare(sql.seen).bind(l.name, ip, near, geo).first<{ ip: number; area: number }>();
  const why = held(!!seen?.ip, !!seen?.area);
  const placed = await env.DB.prepare(sql.join).bind(l.name, user.id, email, ip, near, geo, l.invites, why, Date.now()).first();
  if (placed || (await env.DB.prepare(sql.mine).bind(l.name, user.id).first())) return signedIn();
  return html(env, l, { kind: "error", why: "this email already has a place in line; sign in with the discord account that holds it" }, 409);
}

async function leave(req: Request, env: Env, l: Launch): Promise<Response> {
  const user = await who(req, env, l);
  if (user) await env.DB.prepare(sql.leave).bind(l.name, user).run();
  return new Response(null, { status: 303, headers: secure({ location: `/${l.name}`, "set-cookie": cookie(`cy_${l.name}`, "", 0) }) });
}

const back = (l: Launch) => new Response(null, { status: 303, headers: secure({ location: `/${l.name}` }) });

async function html(env: Env, l: Launch, view: View, status = 200): Promise<Response> {
  const total = (await env.DB.prepare(sql.total).bind(l.name).first<{ n: number }>())?.n ?? 0;
  return new Response(page(l, total, view, env.TURNSTILE_SITEKEY), {
    status,
    headers: secure({ "content-type": "text/html; charset=utf-8", "cache-control": "no-store" }),
  });
}

// secure adds the headers every response carries: nothing framed, nothing
// referred, and scripts only from Turnstile.
function secure(h: Record<string, string>): Headers {
  return new Headers({
    ...h,
    "content-security-policy":
      "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; script-src https://challenges.cloudflare.com; frame-src https://challenges.cloudflare.com; connect-src https://challenges.cloudflare.com; form-action 'self' https://discord.com; base-uri 'none'; frame-ancestors 'none'",
    "referrer-policy": "no-referrer",
    "x-content-type-options": "nosniff",
    "strict-transport-security": "max-age=31536000",
  });
}

// who is the Discord ID this browser signed in as for l, if its cookie is
// ours and still good.
async function who(req: Request, env: Env, l: Launch): Promise<string | null> {
  const v = await unsign(env, read(req, `cy_${l.name}`));
  const [name, user, until] = v?.split(".") ?? [];
  return name === l.name && Number(until) > Date.now() ? user : null;
}

function cookie(name: string, value: string, maxAge: number): string {
  return `${name}=${value}; Max-Age=${maxAge}; Path=/; HttpOnly; Secure; SameSite=Lax`;
}

function read(req: Request, name: string): string {
  for (const part of (req.headers.get("cookie") ?? "").split(";")) {
    const [k, ...v] = part.trim().split("=");
    if (k === name) return v.join("=");
  }
  return "";
}

async function sign(env: Env, value: string): Promise<string> {
  return `${value}~${await keyed(env.HMAC_KEY, "cookie", value)}`;
}

async function unsign(env: Env, signed: string): Promise<string | null> {
  const at = signed.lastIndexOf("~");
  if (at < 0) return null;
  const value = signed.slice(0, at);
  return same(signed.slice(at + 1), await keyed(env.HMAC_KEY, "cookie", value)) ? value : null;
}

// same compares two MACs in time that doesn't depend on where they differ.
function same(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return d === 0;
}
