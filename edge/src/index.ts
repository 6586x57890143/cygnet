// cygnet.lol: the one public surface cygnet has (SPEC.md, Principles 6).
// A launch's page, its sign in with Discord, the invites its members give,
// and the email that proves an address, which comes to us: we never send
// one. The bot token never comes here; the Discord app's OAuth secret does,
// to read who signed in, and their token is revoked as soon as it's read.

import { doh, signer } from "./dkim.ts";
import { page, type View } from "./page.ts";
import { area, created, day, digest, held, inviteLife, keepEmail, keepNetwork, keyed, launches, mailbox, networks, normalEmail, proofLife, proofs, same, token, type Launch } from "./signup.ts";
import { Store, type DB, type Hashes } from "./store.ts";

interface Env {
  DB: D1Database;
  DISCORD_CLIENT_ID: string;
  DISCORD_CLIENT_SECRET: string;
  TURNSTILE_SITEKEY: string;
  TURNSTILE_SECRET: string;
  HMAC_KEY: string;
  ADMIN_TOKEN: string;
}

type Purpose = "join" | "issue" | "redeem";

const api = "https://discord.com/api/v10";
// stay is how long the page remembers who you are.
const stay = 30 * day;
const text = { "content-type": "text/plain; charset=utf-8" };

const store = (env: Env) => new Store(env.DB as unknown as DB, env.HMAC_KEY);

export default {
  async fetch(req, env, ctx): Promise<Response> {
    const url = new URL(req.url);
    const [, first, second, third] = url.pathname.split("/");
    const l = launches[first];
    try {
      if (first === "auth" && second === "discord" && req.method === "GET") return await callback(req, env, ctx, url);
      if (first === "i" && second) return await invited(req, env, url, second);
      if (first === "admin") return await admin(req, env, second, third);
      if (!l) return new Response("nothing here", { status: 404, headers: secure(text) });
      if (second === undefined && req.method === "GET") return await landing(req, env, l);
      if (second === "prove" && req.method === "GET") return await prove(req, env, l, url);
      if (req.method === "POST") {
        if (second === "join") return await human(req, env, l, () => oauth(env, url, "join", l));
        if (second === "issue") return await issueStart(req, env, l, url);
        if (second === "revoke") return await revoke(req, env, l);
        if (second === "leave") return await leave(req, env, l);
      }
      return new Response("not allowed", { status: 405, headers: secure(text) });
    } catch (err) {
      console.error("edge", err instanceof Error ? err.message : "unknown");
      return html(env, l ?? launches.silcord, { kind: "error", why: "something went wrong on cygnet's side; try again in a moment" }, 500);
    }
  },

  // An email to a one time address: if it's signed by the domain of the
  // address it's from, and that address is the one the challenge waits
  // for, the challenge is proven. The email itself is read only for that
  // and never kept.
  async email(message, env): Promise<void> {
    const local = message.to.toLowerCase().split("@")[0];
    if (!/^v-[a-z2-7]{26}$/.test(local) || message.rawSize > 512 * 1024) {
      message.setReject("there's no one at this address");
      return;
    }
    const now = Date.now();
    const from = await signer(await new Response(message.raw).text(), doh, now);
    if (!from) {
      message.setReject("this email isn't signed by your address's own provider, so it can't show the address is yours");
      return;
    }
    const proven = await store(env).prove(await digest(local), await keyed(env.HMAC_KEY, "email", normalEmail(from)), now);
    if (!proven) message.setReject("this isn't the email on your discord account, or the address ran out; start again on the page");
  },

  // Forgets what was only kept to stop double sign ups once it's done that
  // job, and proofs nobody finished.
  async scheduled(_event, env): Promise<void> {
    await store(env).forget(Date.now(), keepNetwork, Object.values(launches), keepEmail);
  },
} satisfies ExportedHandler<Env>;

async function landing(req: Request, env: Env, l: Launch): Promise<Response> {
  const user = await who(req, env, l);
  const now = Date.now();
  const v = user ? await store(env).view(l, user, now) : null;
  if (v) return html(env, l, { kind: "in", rank: v.rank, held: v.held, released: v.released, left: v.left, invites: v.invites, now });
  return html(env, l, { kind: "open" });
}

// human runs next once Turnstile says a person sent the form.
async function human(req: Request, env: Env, l: Launch, next: () => Promise<Response>): Promise<Response> {
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
  return next();
}

// oauth sends the person to Discord with a state only this browser holds,
// which says what the sign in is for.
async function oauth(env: Env, url: URL, purpose: Purpose, l: Launch, invite = "-"): Promise<Response> {
  const nonce = token();
  const to = new URL("https://discord.com/oauth2/authorize");
  to.search = new URLSearchParams({
    client_id: env.DISCORD_CLIENT_ID,
    response_type: "code",
    redirect_uri: `https://${url.host}/auth/discord`,
    scope: "identify email",
    state: nonce,
    prompt: "none",
  }).toString();
  const state = await sign(env, [purpose, l.name, nonce, invite].join("."));
  return new Response(null, { status: 303, headers: secure({ location: to.toString(), "set-cookie": cookie("cy_state", state, 600) }) });
}

async function issueStart(req: Request, env: Env, l: Launch, url: URL): Promise<Response> {
  if (!(await who(req, env, l))) return back(l);
  return human(req, env, l, () => oauth(env, url, "issue", l));
}

// invited is an invite's page, and taking it.
async function invited(req: Request, env: Env, url: URL, code: string): Promise<Response> {
  const hash = await digest(code);
  const row = await env.DB.prepare("SELECT launch FROM invite WHERE hash = ?1").bind(hash).first<{ launch: string }>();
  const l = row ? launches[row.launch] : undefined;
  if (!l) return new Response("nothing here", { status: 404, headers: secure(text) });
  const now = Date.now();
  const open = await store(env).open(l, hash, now);
  if (!open) return html(env, l, { kind: "error", why: "this invite was used, taken back or ran out; ask whoever sent it for a new one" }, 410);
  if (req.method === "POST") return human(req, env, l, () => oauth(env, url, "redeem", l, hash));
  return html(env, l, { kind: "invited", code, expires: open.expires, now });
}

async function callback(req: Request, env: Env, ctx: ExecutionContext, url: URL): Promise<Response> {
  const state = (await unsign(env, read(req, "cy_state")))?.split(".") ?? [];
  const [purpose, launch, nonce, invite] = state as [Purpose, string, string, string];
  const l = launches[launch];
  if (!l) return new Response("nothing here", { status: 404, headers: secure(text) });
  if (!nonce || !same(nonce, url.searchParams.get("state") ?? "")) {
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
  if (!user.email || !user.verified) {
    return html(env, l, { kind: "error", why: "your discord account needs a verified email first; verify it in discord's settings, then try again" }, 403);
  }
  const s = store(env);
  const now = Date.now();
  const email = await keyed(env.HMAC_KEY, "email", normalEmail(user.email));

  if (purpose === "issue") {
    if ((await who(req, env, l)) !== user.id) return html(env, l, { kind: "error", why: "that's a different discord account than the one on the list here" }, 403);
    const m = await s.member(l, user.id);
    const v = await s.view(l, user.id, now);
    if (!m || !v || v.held || !v.left) return html(env, l, { kind: "error", why: "you don't have an invite to give right now" }, 403);
    if (m.email && !same(m.email, email)) {
      return html(env, l, { kind: "error", why: "the email on your discord account changed since you joined, so it can't vouch for you; ask an admin" }, 403);
    }
    return start(env, l, url, { purpose: "issue", user: user.id, email, now });
  }

  if (await s.member(l, user.id)) {
    if (purpose === "join") return signedIn(env, l, user.id);
    return html(env, l, { kind: "error", why: "you're already on the list, so this invite is still free for someone else" }, 409);
  }
  if (now - created(user.id) < l.minAgeDays * day) {
    return html(env, l, { kind: "error", why: `discord accounts have to be at least ${l.minAgeDays} days old to get on the list` }, 403);
  }
  if (await s.emailTaken(l, email)) {
    return html(env, l, { kind: "error", why: "this email is already on the list; sign in with the discord account that holds it" }, 409);
  }
  const net = networks(req.headers.get("cf-connecting-ip") ?? "");
  const where = area(req.cf as { country?: string; region?: string; latitude?: string; longitude?: string } | undefined);
  const h: Hashes = {
    email,
    ip: await keyed(env.HMAC_KEY, "ip", net.ip),
    net: await keyed(env.HMAC_KEY, "net", net.net),
    geo: where ? await keyed(env.HMAC_KEY, "geo", where) : null,
  };
  const seen = await s.seen(l, h);
  const why = held(seen.ip, seen.area);

  if (purpose === "join") {
    if (await s.join(l, user.id, h, why, now)) return signedIn(env, l, user.id);
    return html(env, l, { kind: "error", why: "this email is already on the list; sign in with the discord account that holds it" }, 409);
  }

  const open = await s.open(l, invite, now);
  if (!open) return html(env, l, { kind: "error", why: "this invite was used, taken back or ran out; ask whoever sent it for a new one" }, 410);
  if (open.issuer === user.id) return html(env, l, { kind: "error", why: "that's your own invite; send it to the person it's for" }, 403);
  return start(env, l, url, { purpose: "redeem", user: user.id, email, now, invite, hashes: h, held: why });
}

// start begins a proof of the person's email and shows them where to
// send it.
async function start(env: Env, l: Launch, url: URL, c: { purpose: "issue" | "redeem"; user: string; email: string; now: number; invite?: string; hashes?: Hashes; held?: string | null }): Promise<Response> {
  const t = token();
  const box = mailbox();
  const ok = await store(env).challenge(
    { id: await digest(t), addr: await digest(box), launch: l.name, user: c.user, purpose: c.purpose, made: c.now, expires: c.now + proofLife, email: c.email, invite: c.invite, hashes: c.hashes, held: c.held },
    proofs,
  );
  if (!ok) return html(env, l, { kind: "error", why: "you've started this too many times this hour; try again in an hour" }, 429);
  const proof = await sign(env, `${t}.${box}`);
  return new Response(null, { status: 303, headers: secure({ location: `https://${url.host}/${l.name}/prove`, "set-cookie": cookie("cy_proof", proof, proofLife / 1000) }) });
}

// prove waits for the email, then does what it was for, once.
async function prove(req: Request, env: Env, l: Launch, url: URL): Promise<Response> {
  const [t, box] = (await unsign(env, read(req, "cy_proof")))?.split(".") ?? [];
  const now = Date.now();
  const s = store(env);
  const id = t ? await digest(t) : "";
  const w = id ? await s.waiting(id, now) : null;
  if (!w || w.launch !== l.name) return html(env, l, { kind: "error", why: "that was already used, or ran out before the email came in; start again from your page" }, 410);
  const what = w.purpose === "issue" ? "make your invite" : "take your invite";
  if (!w.verified) return html(env, l, { kind: "prove", address: `${box}@${url.host}`, expires: w.expires, now, what });
  const c = await s.consume(id, now);
  if (!c) return back(l);
  const done = cookie("cy_proof", "", 0);
  if (c.purpose === "issue") {
    const code = await s.issue(l, c.user, now);
    if (!code) return html(env, l, { kind: "error", why: "you don't have an invite to give right now" }, 403);
    const res = html(env, l, { kind: "minted", link: `https://${url.host}/i/${code}`, expires: now + inviteLife, now });
    return withCookie(await res, done);
  }
  const r = await s.redeem(l, c.invite ?? "", c.user, { email: c.email, ip: c.ip, net: c.net, geo: c.geo }, c.held, now);
  if (r === "taken") return withCookie(await html(env, l, { kind: "error", why: "this email or account got on the list another way meanwhile" }, 409), done);
  if (r === "gone") return withCookie(await html(env, l, { kind: "error", why: "this invite was used, taken back or ran out before you finished; ask for a new one" }, 410), done);
  const res = await signedIn(env, l, c.user);
  res.headers.append("set-cookie", done);
  return res;
}

async function revoke(req: Request, env: Env, l: Launch): Promise<Response> {
  const user = await who(req, env, l);
  const form = await req.formData();
  if (user) await store(env).revoke(l, user, String(form.get("invite") ?? ""), Date.now());
  return back(l);
}

async function leave(req: Request, env: Env, l: Launch): Promise<Response> {
  const user = await who(req, env, l);
  if (user) await store(env).leave(l, user, Date.now());
  return new Response(null, { status: 303, headers: secure({ location: `/${l.name}`, "set-cookie": cookie(`cy_${l.name}`, "", 0) }) });
}

// admin is what an admin does, with ADMIN_TOKEN as a bearer: release
// invites to the front of a line, lift a hold, read the tree, and check
// the ledger. Without the token set, none of it exists.
async function admin(req: Request, env: Env, launch?: string, action?: string): Promise<Response> {
  const given = (req.headers.get("authorization") ?? "").replace(/^Bearer /, "");
  if (!env.ADMIN_TOKEN || !given || !same(await digest(given), await digest(env.ADMIN_TOKEN))) {
    return new Response("nothing here", { status: 404, headers: secure(text) });
  }
  const s = store(env);
  const json = (v: unknown) => new Response(JSON.stringify(v, null, 2), { headers: secure({ "content-type": "application/json", "cache-control": "no-store" }) });
  if (launch === "verify" && req.method === "GET") return json(await s.verify());
  const l = launch ? launches[launch] : undefined;
  if (!l) return new Response("no such launch", { status: 404, headers: secure(text) });
  if (action === "tree" && req.method === "GET") return json(await s.tree(l));
  if (req.method === "POST") {
    const body = await req.json<{ count?: number; invites?: number; user?: string }>();
    if (action === "release" && Number.isInteger(body.count) && Number.isInteger(body.invites) && body.count! > 0 && body.invites! > 0 && body.invites! <= 10) {
      return json({ released: await s.release(l, body.count!, body.invites!, Date.now()) });
    }
    if (action === "lift" && body.user && /^\d{17,20}$/.test(body.user)) return json({ lifted: await s.lift(l, body.user, Date.now()) });
  }
  return new Response("not allowed", { status: 405, headers: secure(text) });
}

const back = (l: Launch) => new Response(null, { status: 303, headers: secure({ location: `/${l.name}` }) });

async function signedIn(env: Env, l: Launch, user: string): Promise<Response> {
  const c = cookie(`cy_${l.name}`, await sign(env, `${l.name}.${user}.${Date.now() + stay}`), stay / 1000);
  return new Response(null, { status: 303, headers: secure({ location: `/${l.name}`, "set-cookie": c }) });
}

function withCookie(res: Response, c: string): Response {
  res.headers.append("set-cookie", c);
  return res;
}

async function html(env: Env, l: Launch, view: View, status = 200): Promise<Response> {
  return new Response(page(l, await store(env).total(l), view, env.TURNSTILE_SITEKEY), {
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
  const [name, user, until] = (await unsign(env, read(req, `cy_${l.name}`)))?.split(".") ?? [];
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

