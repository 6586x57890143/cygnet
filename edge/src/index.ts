// cygnet.lol: the one public surface cygnet has (SPEC.md, Principles 6).
// A launch's page, its sign in with Discord, the invites its members give,
// and the emailed links that prove an address. The bot token never comes
// here; the Discord app's OAuth secret does, to read who signed in, and
// their token is revoked as soon as it's read. A launch's line, its
// invites and their ledger are cygnet's: the Worker asks her over the hub
// (hub.ts) and keeps only what stops one person signing up twice.

import { page, type Invite, type Row, type View } from "./page.ts";
import {
  area,
  budget,
  created,
  day,
  digest,
  held,
  inviteCode,
  keepEmail,
  keepList,
  keepNetwork,
  keyed,
  launches,
  masked,
  networks,
  normalEmail,
  proofLife,
  proofs,
  same,
  stages,
  token,
  type Launch,
  type Stage,
  isPNG,
  reserved,
} from "./signup.ts";
import { Store, type DB, type Hashes } from "./store.ts";
import { call, hubOf, type CodeInfo, type ListRow, type Place, type Redeemed, type Reply } from "./hub.ts";
import { sealed } from "./seal.ts";
import { automatic, proof, type Letter } from "./mail.ts";
import { checkSays, supportDocument, supportPage, type Checkout, type Offer, type Status, type SupportView } from "./support.ts";

interface Env {
  DB: D1Database;
  // Scaleway's Transactional Email sends the links: the secret key's
  // token, and the project it bills.
  SCW_SECRET_KEY: string;
  SCW_PROJECT_ID: string;
  DISCORD_CLIENT_ID: string;
  DISCORD_CLIENT_SECRET: string;
  TURNSTILE_SITEKEY: string;
  TURNSTILE_SECRET: string;
  HMAC_KEY: string;
  ADMIN_TOKEN: string;
  // CONTROLLER is who's responsible for the data, and CONTACT how to reach
  // them, as the privacy notice names them. Set at deploy, not in the code.
  CONTROLLER?: string;
  CONTACT?: string;
  // cygnet's hub: its address through the tunnel, the Worker's token as
  // the bird edge, and Access's service token. All secrets; without the
  // first two there are no lists and no links here.
  CYGNET_HUB_URL?: string;
  CYGNET_HUB_TOKEN?: string;
  CF_ACCESS_CLIENT_ID?: string;
  CF_ACCESS_CLIENT_SECRET?: string;
  // cygnet's public key for sealed sign ins (tools/sealkey), base64url.
  // Unset, nobody is offered to be added when their launch opens.
  CYGNET_SEAL_PUBLIC?: string;
}

type Purpose = "join" | "issue" | "redeem" | "list" | "server" | "admit" | "forget" | "support";

const api = "https://discord.com/api/v10";
// stay is how long the page remembers who you are.
const stay = 30 * day;
const text = { "content-type": "text/plain; charset=utf-8" };
const from = { email: "cygnet@cygnet.lol", name: "cygnet" };

const store = (env: Env) => new Store(env.DB as unknown as DB);

// ask is cygnet answering method, or offline when the Worker has no hub.
function ask<T>(env: Env, method: string, body: unknown): Promise<Reply<T>> {
  const hub = hubOf(env);
  if (!hub) return Promise.resolve({ ok: false, code: "offline", reason: "no hub" });
  return call<T>(hub, method, body);
}

// refused is whether r is cygnet saying no, for one of why.
const refused = (r: Reply<unknown>, ...why: string[]) => !r.ok && r.code === "denied" && why.includes(r.reason);

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
  tooMany: "youve asked for too many links this hour\ntry again in an hour",
  noBudget: "weve sent all the emails we can for now\ntry again in a few days",
  sendFailed: "we couldnt send the email\ntry again in a minute",
  linkGone: "that link ran out or was already used\nstart again from your page",
  gotInMeanwhile: "this email or account got on the list another way in the meantime",
  inviteGoneLate: "this invite got used or taken back or ran out before you finished\nask for a new one",
  closed: "the list is closed for now",
  notYours: "only the server owner can see the list",
  tooNewHere: (days: number) => `discord accounts need to be at least ${days} days old to come in this way`,
  banned: "you cant join this server",
  full: "youre in as many servers as discord allows\nleave one and try again",
  unready: "this server isnt ready for new people yet\nwe told its admins so try again a bit later",
  notYet: "your turn hasnt come yet\nyour page says when it does",
  noCheckout: "that checkout isnt yours or its gone\nstart again from the support page",
  tooManyCheckouts: "you have several checkouts waiting already\nfinish one or wait for them to close",
  notOnOffer: "thats not something on offer\npick again",
  noPrice: "the price or the chain didnt answer\ntry again in a minute",
};

// denied is what a server's link refused for, in a person's words: the
// reason cygnet.redeem gives.
const denied: Record<string, string> = {
  gone: say.inviteGone,
  young: say.tooNewHere(30),
  banned: say.banned,
  full: say.full,
  unready: say.unready,
};

export default {
  async fetch(req, env, ctx): Promise<Response> {
    const url = new URL(req.url);
    const [, first, second, third] = url.pathname.split("/");
    const l = first ? await store(env).launch(first) : undefined;
    try {
      if (first === "auth" && second === "discord" && req.method === "GET") return await callback(req, env, ctx, url);
      if (first === "i" && second) return await invited(req, env, url, second);
      if (first === "v" && second) return await confirm(req, env, second);
      if (first === "admin") return await admin(req, env, second, third);
      if (first === "privacy" && !second) return await privacy(env);
      if (first === "privacy" && second === "forget" && req.method === "POST") return await human(req, env, privacyPage(env), () => oauth(env, url, "forget", launches.silcord));
      if (first === "bird" && second && !third && req.method === "GET") return await birdArt(env, second);
      // A launch registered under the name before /support existed keeps it.
      if (first === "support" && !l) return await support(req, env, url, second, third);
      if (!first && req.method === "GET") return await home(env);
      if (!l) return new Response("nothing here", { status: 404, headers: secure(text) });
      if (second === undefined && req.method === "GET") return await landing(req, env, l);
      if (second === "wait" && req.method === "GET") return await wait(req, env, l, url);
      if (second === "mine" && req.method === "GET") return await mine(req, env, l);
      if (second === "list" && req.method === "GET") return await listPage(req, env, l, url);
      if (req.method === "POST") {
        if (second === "join") {
          if ((l.stage ?? "gathering") !== "gathering") return html(env, l, { kind: "error", why: say.closed }, 403);
          return await human(req, env, l, (form) => oauth(env, url, "join", l, form.get("how") === "seal" && env.CYGNET_SEAL_PUBLIC ? "seal" : "-"));
        }
        if (second === "seal") return await sealStart(req, env, l, url);
        if (second === "in") return await inStart(req, env, l, url);
        if (second === "unseal") return await unseal(req, env, l);
        if (second === "issue") return await issueStart(req, env, l, url);
        if (second === "revoke") return await revoke(req, env, l);
        if (second === "leave") return await leave(req, env, l);
        if (second === "lift") return await liftPressed(req, env, l);
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
    const s = store(env);
    await s.forget(Date.now(), keepNetwork, await s.launches(), keepEmail, keepList);
  },
} satisfies ExportedHandler<Env>;

// invitesOf is cygnet's invites as the page lists them.
const invitesOf = (p: Place): Invite[] => p.invites.map((i) => ({ hash: i.code, made: i.made, expires: i.expires, spent_at: i.spent_at ?? null, revoked: i.revoked ?? null }));

async function landing(req: Request, env: Env, l: Launch): Promise<Response> {
  const user = await who(req, env, l);
  const now = Date.now();
  if (user) {
    const r = await ask<Place>(env, "cygnet.place", { launch: l.name, user });
    if (r.ok) return html(env, l, { kind: "in", rank: r.body.sponsored ? null : r.body.rank, held: r.body.held ?? null, released: r.body.released, left: r.body.left, invites: invitesOf(r.body), now, sealed: r.body.sealed, seal: !!env.CYGNET_SEAL_PUBLIC && !r.body.admitted && l.stage !== "open", admitted: !!r.body.admitted, here: !!r.body.here });
    if (!refused(r, "not_on_list")) return html(env, l, { kind: "error", why: say.broke }, 502);
  }
  // Signing in again still works when the list isn't taking anyone new.
  const stage = l.stage ?? "gathering";
  return html(env, l, stage === "gathering" ? { kind: "open", seal: !!env.CYGNET_SEAL_PUBLIC } : { kind: stage === "open" ? "opened" : "closed" });
}

// human runs next once Turnstile says a person sent the form, handing it
// the form.
async function human(req: Request, env: Env, l: Launch, next: (form: FormData) => Promise<Response>): Promise<Response> {
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
  return next(form);
}

// oauth sends the person to Discord with a state only this browser holds,
// which says what the sign in is for. A join that asks to be added when
// the launch opens ("seal") asks for guilds.join too.
async function oauth(env: Env, url: URL, purpose: Purpose, l: Launch, invite = "-"): Promise<Response> {
  const nonce = token();
  const to = new URL("https://discord.com/oauth2/authorize");
  to.search = new URLSearchParams({
    client_id: env.DISCORD_CLIENT_ID,
    response_type: "code",
    redirect_uri: `https://${url.host}/auth/discord`,
    // The list needs only who you are; everything else needs your email.
    scope: purpose === "list" || purpose === "forget" || purpose === "support" ? "identify" : purpose === "admit" ? (invite === "add" ? "identify guilds.join" : "identify") : invite === "seal" ? "identify email guilds.join" : "identify email",
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

// sealStart is someone on the list asking to be added when it opens: a
// fresh sign in that lets cygnet add them, sealed to her.
async function sealStart(req: Request, env: Env, l: Launch, url: URL): Promise<Response> {
  if (!(await who(req, env, l)) || !env.CYGNET_SEAL_PUBLIC) return back(l);
  return human(req, env, l, () => oauth(env, url, "join", l, "seal"));
}

// inStart is someone a wave let in coming for their way in: a sign in
// that lets cygnet add them ("add"), or only says who they are, for an
// invite of their own.
async function inStart(req: Request, env: Env, l: Launch, url: URL): Promise<Response> {
  if (!(await who(req, env, l))) return back(l);
  return human(req, env, l, (form) => oauth(env, url, "admit", l, form.get("how") === "invite" ? "invite" : "add"));
}

// unseal is never mind: cygnet forgets the sealed sign in, and they get
// their way in on this page when their wave opens.
async function unseal(req: Request, env: Env, l: Launch): Promise<Response> {
  const user = await who(req, env, l);
  if (user) {
    const r = await ask(env, "cygnet.seal", { launch: l.name, user, sealed: "" });
    if (!r.ok && !refused(r, "not_on_list")) return html(env, l, { kind: "error", why: say.broke }, 502);
  }
  return back(l);
}

// invited is an invite's page, and taking it: a launch's, which puts
// someone in line, or a server's, which lets them in. cygnet says which.
async function invited(req: Request, env: Env, url: URL, code: string): Promise<Response> {
  if (!hubOf(env)) return new Response("nothing here", { status: 404, headers: secure(text) });
  const hash = await digest(code);
  const r = await ask<CodeInfo>(env, "cygnet.code", { code: hash });
  if (!r.ok || !r.body.launch) return serverInvited(req, env, url, code, hash, r);
  const l = await store(env).launch(r.body.launch);
  if (!l) return new Response("nothing here", { status: 404, headers: secure(text) });
  if (!r.body.open) return html(env, l, { kind: "error", why: say.inviteGone }, 410);
  if (l.stage === "closed") return html(env, l, { kind: "error", why: say.closed }, 403);
  if (req.method === "POST") return human(req, env, l, () => oauth(env, url, "redeem", l, hash));
  return html(env, l, { kind: "invited", code, expires: r.body.expires ?? 0, now: Date.now() });
}

// serverInvited is a server's link (/invite): its page, as cygnet says the
// link stands, and the sign in that takes it. Two ways in: cygnet adds them
// with their own sign in, or makes an invite only they can take.
async function serverInvited(req: Request, env: Env, url: URL, code: string, hash: string, r: Reply<CodeInfo>): Promise<Response> {
  const s = serverPage(r.ok ? r.body : { open: false }, code);
  if (!r.ok) return html(env, s, { kind: "error", why: say.broke }, 502);
  if (!r.body.open) return html(env, s, { kind: "error", why: say.inviteGone }, 410);
  if (req.method === "POST") return human(req, env, s, (form) => serverOAuth(env, url, form.get("how") === "invite" ? "invite" : "add", hash));
  return html(env, s, { kind: "server", code, from: r.body.from ?? "someone", expires: r.body.expires ?? 0, now: Date.now() });
}

// serverPage is the page a server's link leads to: the server's name and
// picture, and back to the link from an error.
function serverPage(info: CodeInfo, code: string): Launch {
  return { name: `i/${code}`, title: info.name ?? "cygnet", icon: info.icon ?? "", line: "", minAgeDays: 30, server: true };
}

// serverOAuth sends the person to Discord: to be added (guilds.join, which
// lets cygnet add them with their own sign in), or only to say who they
// are, for an invite of their own.
async function serverOAuth(env: Env, url: URL, how: "add" | "invite", hash: string): Promise<Response> {
  const nonce = token();
  const to = new URL("https://discord.com/oauth2/authorize");
  to.search = new URLSearchParams({
    client_id: env.DISCORD_CLIENT_ID,
    response_type: "code",
    redirect_uri: `https://${url.host}/auth/discord`,
    scope: how === "add" ? "identify guilds.join" : "identify",
    state: nonce,
    prompt: "none",
  }).toString();
  const state = await sign(env, ["server", how, nonce, hash].join("."));
  return new Response(null, { status: 303, headers: secure({ location: to.toString(), "set-cookie": cookie("cy_state", state, 600) }) });
}

// serverBack is the page a refusal on a server's link shows: cygnet's, with
// the way back home, since only the link itself names the server.
const serverBack: Launch = { name: "", title: "cygnet", icon: "", line: "", minAgeDays: 30, server: true };

// discordToken trades an OAuth code for a token, at the redirect every
// sign-in comes back to.
function discordToken(env: Env, url: URL, code: string): Promise<Response> {
  return fetch(`${api}/oauth2/token`, {
    method: "POST",
    headers: { authorization: basicAuth(env), "content-type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({ grant_type: "authorization_code", code, redirect_uri: `https://${url.host}/auth/discord` }),
  });
}

// giveBack revokes a token once the response is on its way.
function giveBack(env: Env, ctx: ExecutionContext, token: string): void {
  ctx.waitUntil(
    fetch(`${api}/oauth2/token/revoke`, {
      method: "POST",
      headers: { authorization: basicAuth(env), "content-type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ token, token_type_hint: "access_token" }),
    }),
  );
}

const basicAuth = (env: Env) => `Basic ${btoa(`${env.DISCORD_CLIENT_ID}:${env.DISCORD_CLIENT_SECRET}`)}`;

// serverCallback is Discord sending the person back to take a server's
// link: cygnet spends it and lets them in. Their token goes to cygnet only
// for an add, and is revoked once she's done with it.
async function serverCallback(env: Env, ctx: ExecutionContext, url: URL, how: string, nonce: string, hash: string): Promise<Response> {
  const hub = hubOf(env);
  if (!hub) return new Response("nothing here", { status: 404, headers: secure(text) });
  if (!nonce || !same(nonce, url.searchParams.get("state") ?? "")) return html(env, serverBack, { kind: "error", why: say.elsewhere }, 400);
  const code = url.searchParams.get("code");
  if (!code) return new Response(null, { status: 303, headers: secure({ location: "/" }) });
  const grant = await discordToken(env, url, code);
  if (!grant.ok) return html(env, serverBack, { kind: "error", why: say.discordNo }, 502);
  const { access_token } = await grant.json<{ access_token: string }>();
  try {
    const me = await fetch(`${api}/users/@me`, { headers: { authorization: `Bearer ${access_token}` } });
    if (!me.ok) return html(env, serverBack, { kind: "error", why: say.discordWho }, 502);
    const user = await me.json<{ id: string }>();
    const r = await call<Redeemed>(hub, "cygnet.redeem", { code: hash, user: user.id, token: how === "add" ? access_token : undefined });
    if (!r.ok) return html(env, serverBack, { kind: "error", why: r.code === "denied" && Object.hasOwn(denied, r.reason) ? denied[r.reason] : say.broke }, r.code === "denied" ? 403 : 502);
    const to = r.body.invite ? `https://discord.gg/${encodeURIComponent(r.body.invite)}` : `https://discord.com/channels/${encodeURIComponent(r.body.guild)}`;
    return new Response(null, { status: 303, headers: secure({ location: to, "set-cookie": cookie("cy_state", "", 0) }) });
  } finally {
    giveBack(env, ctx, access_token);
  }
}

async function callback(req: Request, env: Env, ctx: ExecutionContext, url: URL): Promise<Response> {
  const state = (await unsign(env, read(req, "cy_state")))?.split(".") ?? [];
  const [purpose, launch, nonce, invite] = state as [Purpose, string, string, string];
  if (purpose === "server") return serverCallback(env, ctx, url, launch, nonce, invite);
  if (purpose === "support") return supportCallback(env, ctx, url, nonce);
  const l = launch ? await store(env).launch(launch) : undefined;
  if (!l) return new Response("nothing here", { status: 404, headers: secure(text) });
  if (!nonce || !same(nonce, url.searchParams.get("state") ?? "")) return html(env, l, { kind: "error", why: say.elsewhere }, 400);
  const code = url.searchParams.get("code");
  if (!code) return back(l);

  const grant = await discordToken(env, url, code);
  if (!grant.ok) return html(env, l, { kind: "error", why: say.discordNo }, 502);
  const { access_token, refresh_token, scope } = await grant.json<{ access_token: string; refresh_token?: string; scope?: string }>();
  const me = await fetch(`${api}/users/@me`, { headers: { authorization: `Bearer ${access_token}` } });
  // Read once, then given back: cygnet keeps no Discord token for a list,
  // except one sealed to her for someone who asked to be added when it
  // opens. Revoking either token ends the whole grant, so that one isn't.
  const sealing = purpose === "join" && invite === "seal" && !!refresh_token && !!env.CYGNET_SEAL_PUBLIC && (scope ?? "").split(" ").includes("guilds.join");
  const revoke = () => giveBack(env, ctx, access_token);
  // A way in's token goes to cygnet first, and is given back after.
  if (!sealing && purpose !== "admit") revoke();
  if (!me.ok) {
    if (sealing || purpose === "admit") revoke();
    return html(env, l, { kind: "error", why: say.discordWho }, 502);
  }
  const user = await me.json<{ id: string; email?: string | null; verified?: boolean }>();
  // Forget me everywhere: cygnet first, then what the Worker keeps.
  if (purpose === "forget") {
    const r = await ask(env, "cygnet.forget", { user: user.id });
    if (!r.ok) return html(env, privacyPage(env), { kind: "error", why: say.broke }, 502);
    await store(env).dropEverywhere(user.id);
    return html(env, privacyPage(env), { kind: "forgotten" });
  }
  if (purpose === "admit") {
    try {
      if ((await who(req, env, l)) !== user.id) return html(env, l, { kind: "error", why: say.otherAccount }, 403);
      const r = await ask<Redeemed>(env, "cygnet.admit", { launch: l.name, user: user.id, token: invite === "add" ? access_token : undefined });
      if (refused(r, "not_yet", "not_on_list")) return html(env, l, { kind: "error", why: say.notYet }, 403);
      if (!r.ok) return html(env, l, { kind: "error", why: r.code === "denied" && Object.hasOwn(denied, r.reason) ? denied[r.reason] : say.broke }, r.code === "denied" ? 403 : 502);
      const to = r.body.invite ? `https://discord.gg/${encodeURIComponent(r.body.invite)}` : `https://discord.com/channels/${encodeURIComponent(r.body.guild)}`;
      return new Response(null, { status: 303, headers: secure({ location: to, "set-cookie": cookie("cy_state", "", 0) }) });
    } finally {
      revoke();
    }
  }
  // The list: only for whoever may see it, for an hour.
  if (purpose === "list") {
    if (!(l.admins ?? []).includes(user.id)) return html(env, l, { kind: "error", why: say.notYours }, 403);
    const c = cookie(`cy_list_${l.name}`, await sign(env, `${l.name}.${user.id}.${Date.now() + 3_600_000}`), 3600);
    return new Response(null, { status: 303, headers: secure({ location: `/${l.name}/list`, "set-cookie": c }) });
  }
  // keepSeal hands cygnet the sealed sign in once they're on her list, and
  // gives the grant back if she won't keep it.
  const keepSeal = async () => {
    if (!sealing) return;
    const r = await ask(env, "cygnet.seal", { launch: l.name, user: user.id, sealed: await sealed(env.CYGNET_SEAL_PUBLIC!, refresh_token!) });
    if (!r.ok) revoke();
  };
  if (!user.email || !user.verified) {
    if (sealing) revoke();
    return html(env, l, { kind: "error", why: say.unverified }, 403);
  }
  const s = store(env);
  const now = Date.now();
  const email = await keyed(env.HMAC_KEY, "email", normalEmail(user.email));

  if (purpose === "issue") {
    if ((await who(req, env, l)) !== user.id) return html(env, l, { kind: "error", why: say.otherAccount }, 403);
    const p = await ask<Place>(env, "cygnet.place", { launch: l.name, user: user.id });
    if (!p.ok && !refused(p, "not_on_list")) return html(env, l, { kind: "error", why: say.broke }, 502);
    if (!p.ok || p.body.held || !p.body.left) return html(env, l, { kind: "error", why: say.noInvite }, 403);
    const kept = await s.signup(l, user.id);
    if (kept?.email && !same(kept.email, email)) return html(env, l, { kind: "error", why: say.emailChanged }, 403);
    return start(env, l, url, { purpose: "issue", user: user.id, address: user.email, email, now });
  }

  if (await s.signup(l, user.id)) {
    if (purpose !== "join") return html(env, l, { kind: "error", why: say.alreadyIn }, 409);
    // Signed up here: cygnet has them too, unless a join's answer was lost
    // on the way back, and then this puts them on.
    const r = await ask(env, "cygnet.join", { launch: l.name, user: user.id });
    if (!r.ok && !refused(r, "on_list", "closed")) {
      if (sealing) revoke();
      return html(env, l, { kind: "error", why: say.broke }, 502);
    }
    await keepSeal();
    return signedIn(env, l, user.id);
  }
  if (sealing && (now - created(user.id) < l.minAgeDays * day || (await s.emailTaken(l, email)))) revoke();
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
    if ((l.stage ?? "gathering") !== "gathering") return html(env, l, { kind: "error", why: say.closed }, 403);
    // cygnet first, then the hashes: a retry after a failed keep finds
    // them on her list, and keeps them then.
    const r = await ask<{ place: number }>(env, "cygnet.join", { launch: l.name, user: user.id, held: why ?? "" });
    if (!r.ok && !refused(r, "on_list")) {
      if (sealing) revoke();
      return html(env, l, { kind: "error", why: refused(r, "closed") ? say.closed : say.broke }, refused(r, "closed") ? 403 : 502);
    }
    await s.keep(l, user.id, h, now);
    await keepSeal();
    return signedIn(env, l, user.id);
  }

  const info = await ask<CodeInfo>(env, "cygnet.code", { code: invite });
  if (!info.ok) return html(env, l, { kind: "error", why: say.broke }, 502);
  if (!info.body.open || info.body.launch !== l.name) return html(env, l, { kind: "error", why: say.inviteGone }, 410);
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
  const href = `https://${url.host}/v/${link}`;
  try {
    await mail(env, c.address, proof(l.name, c.purpose, href, proofLife / 60_000, url.host), c.now + proofLife);
  } catch (err) {
    console.error("edge send", err instanceof Error ? err.message : "unknown");
    return html(env, l, { kind: "error", why: say.sendFailed }, 502);
  }
  const value = await sign(env, `${t}.${btoa(masked(c.address)).replaceAll("=", "")}`);
  return new Response(null, { status: 303, headers: secure({ location: `https://${url.host}/${l.name}/wait`, "set-cookie": cookie("cy_proof", value, proofLife / 1000) }) });
}

// mail sends one email through Scaleway's Transactional Email, in Paris,
// marked as automatic, and never delivered after until, when its link has
// run out anyway.
async function mail(env: Env, to: string, m: Letter, until: number): Promise<void> {
  const r = await fetch("https://api.scaleway.com/transactional-email/v1alpha1/regions/fr-par/emails", {
    method: "POST",
    headers: { "x-auth-token": env.SCW_SECRET_KEY, "content-type": "application/json" },
    body: JSON.stringify({
      from: { email: from.email, name: from.name },
      to: [{ email: to }],
      subject: m.subject,
      text: m.text,
      html: m.html,
      project_id: env.SCW_PROJECT_ID,
      send_before: new Date(until).toISOString(),
      additional_headers: automatic,
    }),
  });
  if (!r.ok) throw new Error(`scaleway ${r.status}`);
}

// confirm is an emailed link: a page whose one button marks it clicked,
// pressed for the person by confirm.js. If this browser started it, it goes
// straight on; on another device, the tab that started it does.
async function confirm(req: Request, env: Env, link: string): Promise<Response> {
  const s = store(env);
  const hash = await digest(link);
  const row = await env.DB.prepare("SELECT launch FROM challenge WHERE link = ?1").bind(hash).first<{ launch: string }>();
  const l = (row && (await s.launch(row.launch))) || launches.silcord;
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
  const fail = async (why: string, status: number) => withCookie(await html(env, l, { kind: "error", why }, status), done);
  if (c.purpose === "issue") {
    // The code is made here and shown once; cygnet keeps only its hash.
    const code = inviteCode();
    const r = await ask<{ expires: number }>(env, "cygnet.issue", { launch: l.name, user: c.user, code: await digest(code) });
    if (refused(r, "none_left", "not_on_list")) return fail(say.noInvite, 403);
    if (!r.ok) return fail(say.broke, 502);
    return withCookie(await html(env, l, { kind: "minted", link: `https://${url.host}/i/${code}`, expires: r.body.expires, now }), done);
  }
  const r = await ask<{ launch: string }>(env, "cygnet.vouch", { user: c.user, code: c.invite ?? "", held: c.held ?? "" });
  if (refused(r, "on_list")) return fail(say.gotInMeanwhile, 409);
  if (refused(r, "gone", "own")) return fail(say.inviteGoneLate, 410);
  if (refused(r, "closed")) return fail(say.closed, 403);
  if (!r.ok) return fail(say.broke, 502);
  await s.keep(l, c.user, { email: c.email, ip: c.ip, net: c.net, geo: c.geo }, now);
  return withCookie(await signedIn(env, l, c.user), done);
}

// mine is everything kept about the person signed in, as a file to keep:
// cygnet's record of them, and which hashes are kept here.
async function mine(req: Request, env: Env, l: Launch): Promise<Response> {
  const user = await who(req, env, l);
  if (!user) return back(l);
  const r = await ask<Record<string, unknown>>(env, "cygnet.mine", { launch: l.name, user });
  if (refused(r, "not_on_list")) return back(l);
  if (!r.ok) return html(env, l, { kind: "error", why: say.broke }, 502);
  const kept = await store(env).signup(l, user);
  const data = { ...r.body, stored_as_keyed_hashes: { email: !!kept?.email, connection: !!kept?.ip, network: !!kept?.net, area: !!kept?.geo } };
  return new Response(JSON.stringify(data, null, 2), {
    headers: secure({ "content-type": "application/json", "content-disposition": `attachment; filename="${l.name}-my-data.json"`, "cache-control": "no-store" }),
  });
}

// owner is who's signed in to see l's list, if they still may.
async function owner(req: Request, env: Env, l: Launch): Promise<string | null> {
  const [name, user, until] = (await unsign(env, read(req, `cy_list_${l.name}`)))?.split(".") ?? [];
  return name === l.name && Number(until) > Date.now() && (l.admins ?? []).includes(user) ? user : null;
}

// listPage is the list for the server's owner, after a Discord sign in
// that shows who they are. cygnet checks again that they may see it.
async function listPage(req: Request, env: Env, l: Launch, url: URL): Promise<Response> {
  const user = await owner(req, env, l);
  if (!user) return oauth(env, url, "list", l);
  const r = await ask<{ rows: ListRow[] }>(env, "cygnet.list", { launch: l.name, user });
  if (refused(r, "not_yours")) return html(env, l, { kind: "error", why: say.notYours }, 403);
  if (!r.ok) return html(env, l, { kind: "error", why: say.broke }, 502);
  const rows: Row[] = r.body.rows.map((x) => ({ user: x.user, sponsor: x.sponsor ?? null, place: x.place, released: x.released, held: x.held ?? null }));
  return html(env, l, { kind: "list", rows });
}

async function liftPressed(req: Request, env: Env, l: Launch): Promise<Response> {
  const user = await owner(req, env, l);
  if (!user) return new Response(null, { status: 303, headers: secure({ location: `/${l.name}/list` }) });
  const member = String((await req.formData()).get("user") ?? "");
  if (/^\d{17,20}$/.test(member)) await ask(env, "cygnet.lift", { launch: l.name, user, member });
  return new Response(null, { status: 303, headers: secure({ location: `/${l.name}/list` }) });
}

// home is cygnet.lol itself: the swan, the name, a line, and later the
// launches people can get in line for. Nothing to fill in, nothing to
// scrape, and no search engine is asked in.
async function home(env: Env): Promise<Response> {
  return html(env, { name: "cygnet", title: "cygnet", line: "we keep the quiet places\nlit from below like water at dusk\nand let you in by name", minAgeDays: 30, icon: "/art/cygnet.png" }, { kind: "home" });
}

// privacyPage is the page the notice and forgetting are on.
const privacyPage = (_env: Env): Launch => ({ ...launches.silcord, title: "privacy" });

// privacy is the full privacy notice, for every launch, and the way to be
// forgotten everywhere.
async function privacy(env: Env): Promise<Response> {
  return html(env, privacyPage(env), { kind: "privacy", controller: env.CONTROLLER || "the people running cygnet.lol", contact: env.CONTACT || "privacy@cygnet.lol", forget: hubOf(env) !== null });
}

async function revoke(req: Request, env: Env, l: Launch): Promise<Response> {
  const user = await who(req, env, l);
  const form = await req.formData();
  if (user) await ask(env, "cygnet.revoke", { launch: l.name, user, code: String(form.get("invite") ?? "") });
  return back(l);
}

// leave takes them off cygnet's list first, then forgets their sign up
// here: if cygnet can't be reached, nothing is half done.
async function leave(req: Request, env: Env, l: Launch): Promise<Response> {
  const user = await who(req, env, l);
  if (user) {
    const r = await ask(env, "cygnet.leave", { launch: l.name, user });
    if (!r.ok) return html(env, l, { kind: "error", why: say.broke }, 502);
    await store(env).drop(l, user);
  }
  return new Response(null, { status: 303, headers: secure({ location: `/${l.name}`, "set-cookie": cookie(`cy_${l.name}`, "", 0) }) });
}

// admin is what cygnet sends the Worker, with ADMIN_TOKEN as a bearer: a
// launch's page, and the flock's pictures. Without the token set, none of
// it exists. Everything else about a launch, cygnet answers herself.
async function admin(req: Request, env: Env, launch?: string, action?: string): Promise<Response> {
  const given = (req.headers.get("authorization") ?? "").replace(/^Bearer /, "");
  if (!env.ADMIN_TOKEN || !given || !same(await digest(given), await digest(env.ADMIN_TOKEN))) {
    return new Response("nothing here", { status: 404, headers: secure(text) });
  }
  const s = store(env);
  const json = (v: unknown) => new Response(JSON.stringify(v, null, 2), { headers: secure({ "content-type": "application/json", "cache-control": "no-store" }) });
  // cygnet sends a linked bird's picture: a small PNG, base64.
  if (launch === "bird" && action && req.method === "PUT") {
    const b = await req.json<{ png?: string }>();
    if (!/^[a-z0-9-]{1,32}$/.test(action) || typeof b.png !== "string" || b.png.length > 90_000 || !isPNG(b.png)) {
      return new Response("bad bird", { status: 400, headers: secure(text) });
    }
    await s.bird(action, b.png, (await digest(b.png)).slice(0, 8), Date.now());
    return json({ bird: action });
  }
  // cygnet registers a launch, or refreshes it: its server's id, name and
  // picture, who may see its list, its stage and when it first opened.
  if (launch && !action && req.method === "PUT") {
    const b = await req.json<{ guild?: string; title?: string; icon?: string; admins?: string[]; stage?: string; opens?: number }>();
    // A reserved name is refused only to a new launch: one registered under
    // it before the name was reserved still takes its refreshes.
    const ok =
      /^[a-z0-9-]{2,32}$/.test(launch) &&
      (!reserved.includes(launch) || !!(await s.launch(launch))) &&
      /^\d{17,20}$/.test(b.guild ?? "") &&
      typeof b.title === "string" && b.title.length > 0 && b.title.length <= 100 &&
      typeof b.icon === "string" && (b.icon === "" || b.icon.startsWith("https://cdn.discordapp.com/icons/")) &&
      (b.admins === undefined || (Array.isArray(b.admins) && b.admins.length <= 5 && b.admins.every((a) => /^\d{17,20}$/.test(a)))) &&
      (b.stage === undefined || stages.includes(b.stage as Stage)) &&
      (b.opens === undefined || Number.isInteger(b.opens));
    if (!ok) return new Response("bad launch", { status: 400, headers: secure(text) });
    const kept = await s.register(launch, b.guild!, b.title!, b.icon!, Date.now(), b.admins ?? [], (b.stage as Stage) ?? "gathering", b.opens ?? null);
    return kept ? json({ launch, page: `/${launch}` }) : new Response("that name is another server's", { status: 409, headers: secure(text) });
  }
  return new Response("not allowed", { status: 405, headers: secure(text) });
}

// support is backing the birds (src/support.ts): the owner's letter and
// its sign in at /support, a checkout's choices once signed in, and the
// checkout at /support/<id>, which checks itself until it's paid. cygnet
// keeps all of it; this only asks her.
async function support(req: Request, env: Env, url: URL, second?: string, third?: string): Promise<Response> {
  if (!hubOf(env) || third !== undefined) return new Response("nothing here", { status: 404, headers: secure(text) });
  if (second === undefined && req.method === "GET") return supportHome(req, env);
  if (req.method === "POST") {
    if (second === "in") return human(req, env, supportPage, () => oauth(env, url, "support", supportPage));
    if (second === "quote") return supportQuote(req, env);
    if (second === "out") return new Response(null, { status: 303, headers: secure({ location: "/support", "set-cookie": cookie("cy_support", "", 0) }) });
  }
  if (second && /^[a-z2-7]{26}$/.test(second) && req.method === "GET") return checkoutPage(req, env, url, second);
  return new Response("not allowed", { status: 405, headers: secure(text) });
}

async function supportHome(req: Request, env: Env): Promise<Response> {
  const user = await backer(req, env);
  const o = await ask<Offer>(env, "cygnet.offer", {});
  // Nothing on offer (no wallet set yet) is a page that says so, not an
  // error.
  if (!o.ok && !refused(o, "no_rails")) return html(env, supportPage, { kind: "error", why: say.broke }, 502);
  let status: Status | null = null;
  if (o.ok && user) {
    const s = await ask<Status>(env, "cygnet.supporter", { user });
    if (s.ok) status = s.body;
  }
  return supportHtml(env, { kind: "support", offer: o.ok ? o.body : null, status, signedIn: o.ok && !!user });
}

// supportQuote opens a checkout for whoever is signed in. Their cookie is
// SameSite=Lax, so another site's form can't open one for them.
async function supportQuote(req: Request, env: Env): Promise<Response> {
  const user = await backer(req, env);
  if (!user) return new Response(null, { status: 303, headers: secure({ location: "/support" }) });
  const form = await req.formData();
  const months = Number(form.get("months"));
  const cents = Number(form.get("cents"));
  const rail = String(form.get("rail") ?? "");
  if (!Number.isInteger(months) || !Number.isInteger(cents) || !/^[a-z]{2,16}:[A-Z]{2,8}$/.test(rail)) return html(env, supportPage, { kind: "error", why: say.notOnOffer }, 400);
  const r = await ask<Checkout>(env, "cygnet.quote", { user, months, cents, rail });
  if (refused(r, "too_many")) return html(env, supportPage, { kind: "error", why: say.tooManyCheckouts }, 429);
  if (refused(r, "not_offered")) return html(env, supportPage, { kind: "error", why: say.notOnOffer }, 400);
  if (!r.ok) return html(env, supportPage, { kind: "error", why: r.code === "busy" ? say.noPrice : say.broke }, r.code === "busy" ? 503 : 502);
  return new Response(null, { status: 303, headers: secure({ location: `/support/${r.body.id}` }) });
}

// checkoutPage is a checkout, for the person who opened it. ?check is
// app.js asking quietly whether it's paid yet (support.ts, checkSays).
async function checkoutPage(req: Request, env: Env, url: URL, id: string): Promise<Response> {
  const user = await backer(req, env);
  const check = url.searchParams.has("check");
  const quiet = (status: number) => new Response(null, { status, headers: secure({ "cache-control": "no-store" }) });
  if (!user) return check ? quiet(410) : new Response(null, { status: 303, headers: secure({ location: "/support" }) });
  const r = await ask<Checkout>(env, "cygnet.checkout", { user, id });
  const gone = refused(r, "no_checkout", "not_yours");
  if (check) return quiet(r.ok ? checkSays(r.body) : gone ? 410 : 503);
  if (gone) return html(env, supportPage, { kind: "error", why: say.noCheckout }, 404);
  if (!r.ok) return html(env, supportPage, { kind: "error", why: say.broke }, 502);
  return supportHtml(env, { kind: "checkout", c: r.body, now: Date.now() });
}

// supportCallback is Discord sending someone back from signing in to back
// the birds: who they are is read, their token given back at once, and a
// cookie remembers them for a week.
async function supportCallback(env: Env, ctx: ExecutionContext, url: URL, nonce: string): Promise<Response> {
  if (!nonce || !same(nonce, url.searchParams.get("state") ?? "")) return html(env, supportPage, { kind: "error", why: say.elsewhere }, 400);
  const code = url.searchParams.get("code");
  if (!code) return new Response(null, { status: 303, headers: secure({ location: "/support" }) });
  const grant = await discordToken(env, url, code);
  if (!grant.ok) return html(env, supportPage, { kind: "error", why: say.discordNo }, 502);
  const { access_token } = await grant.json<{ access_token: string }>();
  try {
    const me = await fetch(`${api}/users/@me`, { headers: { authorization: `Bearer ${access_token}` } });
    if (!me.ok) return html(env, supportPage, { kind: "error", why: say.discordWho }, 502);
    const user = await me.json<{ id: string }>();
    if (!/^\d{17,20}$/.test(user.id)) return html(env, supportPage, { kind: "error", why: say.discordWho }, 502);
    const headers = secure({ location: "/support" });
    headers.append("set-cookie", cookie("cy_support", await sign(env, `support.${user.id}.${Date.now() + backerStay}`), backerStay / 1000));
    headers.append("set-cookie", cookie("cy_state", "", 0));
    return new Response(null, { status: 303, headers });
  } finally {
    giveBack(env, ctx, access_token);
  }
}

// backerStay is how long /support remembers who signed in.
const backerStay = 7 * day;

// backer is who signed in to back the birds, if this browser's cookie is
// ours and still good.
async function backer(req: Request, env: Env): Promise<string | null> {
  const [what, user, until] = (await unsign(env, read(req, "cy_support")))?.split(".") ?? [];
  return what === "support" && /^\d{17,20}$/.test(user ?? "") && Number(until) > Date.now() ? user : null;
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
  // /support is drawn in its own art, its refusals included.
  if (l === supportPage && view.kind === "error") return supportHtml(env, view, status);
  const s = store(env);
  return new Response(page(l, await s.total(l), view, env.TURNSTILE_SITEKEY, l.icon ?? "", await s.flock(Date.now())), {
    status,
    headers: secure({ "content-type": "text/html; charset=utf-8", "cache-control": "no-store" }),
  });
}

// supportHtml is a /support page (src/support.ts).
async function supportHtml(env: Env, view: SupportView, status = 200): Promise<Response> {
  return new Response(supportDocument(view, env.TURNSTILE_SITEKEY, await store(env).flock(Date.now())), {
    status,
    headers: secure({ "content-type": "text/html; charset=utf-8", "cache-control": "no-store" }),
  });
}

// birdArt is a bird's picture for the footer. Its URL carries the
// picture's version, so it can be kept a long time.
async function birdArt(env: Env, file: string): Promise<Response> {
  const name = file.replace(/\.png$/, "");
  const png = /^[a-z0-9-]{1,32}$/.test(name) ? await store(env).art(name) : null;
  if (!png) return new Response("nothing here", { status: 404, headers: secure(text) });
  return new Response(Uint8Array.from(atob(png), (c) => c.charCodeAt(0)), {
    headers: secure({ "content-type": "image/png", "cache-control": "public, max-age=604800, immutable" }),
  });
}

// secure adds the headers every response carries: nothing framed, nothing
// referred, scripts only from this site and Turnstile, pictures only from
// here and Discord's CDN, and fonts only from here.
function secure(h: Record<string, string>): Headers {
  return new Headers({
    ...h,
    "content-security-policy":
      "default-src 'none'; img-src 'self' https://cdn.discordapp.com; font-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' https://challenges.cloudflare.com; frame-src https://challenges.cloudflare.com; connect-src 'self' https://challenges.cloudflare.com; form-action 'self' https://discord.com; base-uri 'none'; frame-ancestors 'none'",
    "referrer-policy": "no-referrer",
    "x-content-type-options": "nosniff",
    // Shared by people, never found by search or taken for training.
    "x-robots-tag": "noindex, nofollow, noarchive, noai, noimageai",
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
