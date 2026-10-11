// cygnet.lol: the one public surface cygnet has (SPEC.md, Principles 6).
// A launch's page, its sign in with Discord, the invites its members give,
// and the emailed links that prove an address. The bot token never comes
// here; the Discord app's OAuth secret does, to read who signed in, and
// their token is revoked as soon as it's read.

import { page, type View } from "./page.ts";
import {
  area,
  budget,
  created,
  day,
  digest,
  held,
  inviteLife,
  keepEmail,
  keepNetwork,
  keyed,
  launches,
  masked,
  networks,
  normalEmail,
  proofLife,
  proofs,
  same,
  token,
  type Launch,
} from "./signup.ts";
import { Store, type DB, type Hashes } from "./store.ts";

interface Env {
  DB: D1Database;
  EMAIL: SendEmail;
  DISCORD_CLIENT_ID: string;
  DISCORD_CLIENT_SECRET: string;
  TURNSTILE_SITEKEY: string;
  TURNSTILE_SECRET: string;
  // ICON is the launch server's picture, a cdn.discordapp.com URL, or "".
  ICON: string;
  HMAC_KEY: string;
  ADMIN_TOKEN: string;
}

type Purpose = "join" | "issue" | "redeem";

const api = "https://discord.com/api/v10";
// stay is how long the page remembers who you are.
const stay = 30 * day;
const text = { "content-type": "text/plain; charset=utf-8" };
const from = { email: "cygnet@cygnet.lol", name: "cygnet" };

const store = (env: Env) => new Store(env.DB as unknown as DB, env.HMAC_KEY);

// What a person reads when something stops them. Plain and human: what
// happened, then what to do, one line each.
const say = {
  broke: "something broke on our end\ngive it a minute and try again",
  robot: "we couldnt tell youre a person\ntry again",
  elsewhere: "that sign in started somewhere else or took too long\nstart again from here",
  discordNo: "discord didnt confirm the sign in\ntry again",
  discordWho: "discord didnt tell us who you are\ntry again in a minute",
  unverified: "your discord account needs a verified email first\nverify it in discord settings and come back",
  otherAccount: "thats a different discord account from the one on the list",
  noInvite: "you dont have an invite to give right now",
  emailChanged: "the email on your discord changed since you joined\nask an admin to sort it out",
  alreadyIn: "youre already on the list\nso this invite is still free for someone else",
  tooNew: (days: number) => `discord accounts need to be at least ${days} days old to get on the list`,
  emailTaken: "that email is already on the list\nsign in with the discord account that has it",
  inviteGone: "this invite was used or taken back or ran out\nask whoever sent it for a new one",
  ownInvite: "thats your own invite\nsend it to the person its for",
  tooMany: "youve asked for too many links this hour\ntry again in an hour",
  noBudget: "weve sent all the emails we can for now\ntry again in a few days",
  sendFailed: "we couldnt send the email\ntry again in a minute",
  linkGone: "that link ran out or was already used\nstart again from your page",
  gotInMeanwhile: "this email or account got on the list another way in the meantime",
  inviteGoneLate: "this invite got used or taken back or ran out before you finished\nask for a new one",
};

export default {
  async fetch(req, env, ctx): Promise<Response> {
    const url = new URL(req.url);
    const [, first, second, third] = url.pathname.split("/");
    const l = launches[first];
    try {
      if (first === "auth" && second === "discord" && req.method === "GET") return await callback(req, env, ctx, url);
      if (first === "i" && second) return await invited(req, env, url, second);
      if (first === "v" && second) return await confirm(req, env, second);
      if (first === "admin") return await admin(req, env, second, third);
      if (!l) return new Response("nothing here", { status: 404, headers: secure(text) });
      if (second === undefined && req.method === "GET") return await landing(req, env, l);
      if (second === "wait" && req.method === "GET") return await wait(req, env, l, url);
      if (req.method === "POST") {
        if (second === "join") return await human(req, env, l, () => oauth(env, url, "join", l));
        if (second === "issue") return await issueStart(req, env, l, url);
        if (second === "revoke") return await revoke(req, env, l);
        if (second === "leave") return await leave(req, env, l);
      }
      return new Response("not allowed", { status: 405, headers: secure(text) });
    } catch (err) {
      console.error("edge", err instanceof Error ? err.message : "unknown");
      return html(env, l ?? launches.silcord, { kind: "error", why: say.broke }, 500);
    }
  },

  // Forgets what was only kept to stop double sign ups once it's done that
  // job, links nobody clicked, and old send counts.
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
  if (!ok.success) return html(env, l, { kind: "error", why: say.robot }, 403);
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
  if (!open) return html(env, l, { kind: "error", why: say.inviteGone }, 410);
  if (req.method === "POST") return human(req, env, l, () => oauth(env, url, "redeem", l, hash));
  return html(env, l, { kind: "invited", code, expires: open.expires, now });
}

async function callback(req: Request, env: Env, ctx: ExecutionContext, url: URL): Promise<Response> {
  const state = (await unsign(env, read(req, "cy_state")))?.split(".") ?? [];
  const [purpose, launch, nonce, invite] = state as [Purpose, string, string, string];
  const l = launches[launch];
  if (!l) return new Response("nothing here", { status: 404, headers: secure(text) });
  if (!nonce || !same(nonce, url.searchParams.get("state") ?? "")) return html(env, l, { kind: "error", why: say.elsewhere }, 400);
  const code = url.searchParams.get("code");
  if (!code) return back(l);

  const basic = `Basic ${btoa(`${env.DISCORD_CLIENT_ID}:${env.DISCORD_CLIENT_SECRET}`)}`;
  const grant = await fetch(`${api}/oauth2/token`, {
    method: "POST",
    headers: { authorization: basic, "content-type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({ grant_type: "authorization_code", code, redirect_uri: `https://${url.host}/auth/discord` }),
  });
  if (!grant.ok) return html(env, l, { kind: "error", why: say.discordNo }, 502);
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
  if (!me.ok) return html(env, l, { kind: "error", why: say.discordWho }, 502);
  const user = await me.json<{ id: string; email?: string | null; verified?: boolean }>();
  if (!user.email || !user.verified) return html(env, l, { kind: "error", why: say.unverified }, 403);
  const s = store(env);
  const now = Date.now();
  const email = await keyed(env.HMAC_KEY, "email", normalEmail(user.email));

  if (purpose === "issue") {
    if ((await who(req, env, l)) !== user.id) return html(env, l, { kind: "error", why: say.otherAccount }, 403);
    const m = await s.member(l, user.id);
    const v = await s.view(l, user.id, now);
    if (!m || !v || v.held || !v.left) return html(env, l, { kind: "error", why: say.noInvite }, 403);
    if (m.email && !same(m.email, email)) return html(env, l, { kind: "error", why: say.emailChanged }, 403);
    return start(env, l, url, { purpose: "issue", user: user.id, address: user.email, email, now });
  }

  if (await s.member(l, user.id)) {
    if (purpose === "join") return signedIn(env, l, user.id);
    return html(env, l, { kind: "error", why: say.alreadyIn }, 409);
  }
  if (now - created(user.id) < l.minAgeDays * day) return html(env, l, { kind: "error", why: say.tooNew(l.minAgeDays) }, 403);
  if (await s.emailTaken(l, email)) return html(env, l, { kind: "error", why: say.emailTaken }, 409);
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
    return html(env, l, { kind: "error", why: say.emailTaken }, 409);
  }

  const open = await s.open(l, invite, now);
  if (!open) return html(env, l, { kind: "error", why: say.inviteGone }, 410);
  if (open.issuer === user.id) return html(env, l, { kind: "error", why: say.ownInvite }, 403);
  return start(env, l, url, { purpose: "redeem", user: user.id, address: user.email, email, now, invite, hashes: h, held: why });
}

// start emails the person a link that proves the address is theirs, and
// shows them the page that waits for it. The address is used for this send
// and never stored.
async function start(env: Env, l: Launch, url: URL, c: { purpose: "issue" | "redeem"; user: string; address: string; email: string; now: number; invite?: string; hashes?: Hashes; held?: string | null }): Promise<Response> {
  const s = store(env);
  const t = token();
  const link = token();
  const ok = await s.challenge(
    { id: await digest(t), link: await digest(link), launch: l.name, user: c.user, purpose: c.purpose, made: c.now, expires: c.now + proofLife, email: c.email, invite: c.invite, hashes: c.hashes, held: c.held },
    proofs,
  );
  if (!ok) return html(env, l, { kind: "error", why: say.tooMany }, 429);
  if (!(await s.spend(c.now, budget.emails, budget.days))) return html(env, l, { kind: "error", why: say.noBudget }, 503);
  const what = c.purpose === "issue" ? "make your invite" : "take your invite";
  const href = `https://${url.host}/v/${link}`;
  try {
    await env.EMAIL.send({
      to: c.address,
      from,
      subject: `your ${l.name} link`,
      text: `hey\n\nclick this to ${what} on ${l.name}\n${href}\n\nit works once and only for 30 minutes\nif you didnt ask for this you can ignore it and nothing happens\n\ncygnet`,
      html: `<p>hey</p><p>click this to ${what} on ${l.name}<br><a href="${href}">${href}</a></p><p>it works once and only for 30 minutes<br>if you didnt ask for this you can ignore it and nothing happens</p><p>cygnet</p>`,
    });
  } catch (err) {
    console.error("edge send", err instanceof Error ? err.message : "unknown");
    return html(env, l, { kind: "error", why: say.sendFailed }, 502);
  }
  const value = await sign(env, `${t}.${btoa(masked(c.address)).replaceAll("=", "")}`);
  return new Response(null, { status: 303, headers: secure({ location: `https://${url.host}/${l.name}/wait`, "set-cookie": cookie("cy_proof", value, proofLife / 1000) }) });
}

// confirm is an emailed link: a page whose one button marks it clicked,
// pressed for the person by confirm.js. If this browser started it, it goes
// straight on; on another device, the tab that started it does.
async function confirm(req: Request, env: Env, link: string): Promise<Response> {
  const s = store(env);
  const hash = await digest(link);
  const row = await env.DB.prepare("SELECT launch FROM challenge WHERE link = ?1").bind(hash).first<{ launch: string }>();
  const l = (row && launches[row.launch]) || launches.silcord;
  if (req.method === "GET") return html(env, l, row ? { kind: "confirm", token: link } : { kind: "error", why: say.linkGone }, row ? 200 : 410);
  if (req.method !== "POST") return new Response("not allowed", { status: 405, headers: secure(text) });
  if (!(await s.prove(hash, Date.now()))) return html(env, l, { kind: "error", why: say.linkGone }, 410);
  if (read(req, "cy_proof")) return new Response(null, { status: 303, headers: secure({ location: `/${l.name}/wait` }) });
  return html(env, l, { kind: "confirmed" });
}

// wait shows where the link went until it's clicked, then does what it was
// for, once.
async function wait(req: Request, env: Env, l: Launch, url: URL): Promise<Response> {
  const [t, to] = (await unsign(env, read(req, "cy_proof")))?.split(".") ?? [];
  const now = Date.now();
  const s = store(env);
  const id = t ? await digest(t) : "";
  const w = id ? await s.waiting(id, now) : null;
  // ?check is app.js asking quietly: 204 while it waits, 200 once the
  // link is clicked, 410 once it can't be. The page itself does the rest.
  if (url.searchParams.has("check")) return new Response(null, { status: !w || w.launch !== l.name ? 410 : w.verified ? 200 : 204, headers: secure({ "cache-control": "no-store" }) });
  if (!w || w.launch !== l.name) return html(env, l, { kind: "error", why: say.linkGone }, 410);
  const what = w.purpose === "issue" ? "make your invite" : "take your invite";
  if (!w.verified) return html(env, l, { kind: "sent", to: atob(to ?? ""), expires: w.expires, now, what });
  const c = await s.consume(id, now);
  if (!c) return back(l);
  const done = cookie("cy_proof", "", 0);
  if (c.purpose === "issue") {
    const code = await s.issue(l, c.user, now);
    if (!code) return withCookie(await html(env, l, { kind: "error", why: say.noInvite }, 403), done);
    return withCookie(await html(env, l, { kind: "minted", link: `https://${url.host}/i/${code}`, expires: now + inviteLife, now }), done);
  }
  const r = await s.redeem(l, c.invite ?? "", c.user, { email: c.email, ip: c.ip, net: c.net, geo: c.geo }, c.held, now);
  if (r === "taken") return withCookie(await html(env, l, { kind: "error", why: say.gotInMeanwhile }, 409), done);
  if (r === "gone") return withCookie(await html(env, l, { kind: "error", why: say.inviteGoneLate }, 410), done);
  return withCookie(await signedIn(env, l, c.user), done);
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
  return new Response(page(l, await store(env).total(l), view, env.TURNSTILE_SITEKEY, env.ICON), {
    status,
    headers: secure({ "content-type": "text/html; charset=utf-8", "cache-control": "no-store" }),
  });
}

// secure adds the headers every response carries: nothing framed, nothing
// referred, scripts only from this site and Turnstile, and pictures only
// from here and Discord's CDN.
function secure(h: Record<string, string>): Headers {
  return new Headers({
    ...h,
    "content-security-policy":
      "default-src 'none'; img-src 'self' https://cdn.discordapp.com; style-src 'self' 'unsafe-inline'; script-src 'self' https://challenges.cloudflare.com; frame-src https://challenges.cloudflare.com; connect-src 'self' https://challenges.cloudflare.com; form-action 'self' https://discord.com; base-uri 'none'; frame-ancestors 'none'",
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
