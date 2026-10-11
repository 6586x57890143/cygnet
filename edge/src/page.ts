// The pages a launch has, drawn on the server so each works without
// script. app.js adds what moves: the ASCII field behind, the swan growing
// in front, the server's picture coming in, numbers counting up, the copy
// button, the wait page checking quietly, and the button an emailed link's
// page presses for the person, so a mail scanner that only fetches the
// link confirms nothing.
//
// The voice is a person texting: lowercase, no punctuation, one thought a
// line. A line break does what a full stop would.

import type { Launch } from "./signup.ts";
import { frame, model, text } from "../public/swan.js";

export interface Invite {
  hash: string;
  made: number;
  expires: number;
  spent_at: number | null;
  revoked: number | null;
}

export type View =
  | { kind: "open" }
  | { kind: "in"; rank: number | null; held: string | null; released: number; left: number; invites: Invite[]; now: number }
  | { kind: "invited"; code: string; expires: number; now: number }
  | { kind: "sent"; to: string; expires: number; now: number; what: string }
  | { kind: "confirm"; token: string }
  | { kind: "confirmed" }
  | { kind: "minted"; link: string; expires: number; now: number }
  | { kind: "error"; why: string }
  | { kind: "privacy"; controller: string; contact: string };

// esc escapes text for HTML; lines also turns each line into its own.
export function esc(s: string): string {
  return s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}
const lines = (s: string) => esc(s).replaceAll("\n", "<br>");

// span is a duration in its two largest units, as UX.md has them: 45s,
// 12m 0s, 3h 12m, 2d 4h.
export function span(ms: number): string {
  const units = [[86400, "d"], [3600, "h"], [60, "m"], [1, "s"]] as const;
  const s = Math.max(0, Math.round(ms / 1000));
  const i = units.findIndex(([size]) => s >= size);
  if (i < 0 || i === units.length - 1) return `${s}s`;
  const [big, u] = units[i];
  const [small, v] = units[i + 1];
  return `${Math.floor(s / big)}${u} ${Math.floor((s % big) / small)}${v}`;
}

// grown is how grown the swan is for a view: a cygnet on arrival, half
// grown on the way in, a swan once someone's in or has invites to give.
export function grown(view: View): number {
  return [0, 0.55, 1][stage(view)];
}

function stage(view: View): number {
  switch (view.kind) {
    case "in":
      return view.rank === null || view.released > 0 ? 2 : 1;
    case "minted":
      return 2;
    case "sent":
    case "confirm":
    case "confirmed":
      return 1;
    default:
      return 0;
  }
}

// still is the swan at t as text, the frame a page without script shows
// and app.js draws over. One for each stage, so it's drawn once.
const stills = new Map<number, string>();
export function still(t: number): string {
  let s = stills.get(t);
  if (!s) {
    s = text(frame(model(t), 96, 40, -0.55, 0.14), 96, 40);
    stills.set(t, s);
  }
  return s;
}

const widget = (sitekey: string) =>
  `<div class="cf-turnstile" data-sitekey="${esc(sitekey)}" data-theme="auto" data-size="flexible" data-appearance="interaction-only"></div>`;

function invites(l: Launch, v: Extract<View, { kind: "in" }>, sitekey: string): string {
  if (!v.released) return `<div class="row"><span class="k">invites</span><span>not yet<br><span class="dim">they go to the front of the line first</span></span></div>`;
  const rows = v.invites
    .map((i) => {
      const ago = `<span class="dim">${span(v.now - i.made)} ago</span>`;
      if (i.spent_at) return `<li><span class="ok">used</span>${ago}</li>`;
      if (i.revoked) return `<li><span class="dim">taken back</span>${ago}</li>`;
      if (i.expires <= v.now) return `<li><span class="dim">ran out and came back to you</span>${ago}</li>`;
      return `<li><span>open for ${span(i.expires - v.now)}</span><form method="post" action="/${esc(l.name)}/revoke"><input type="hidden" name="invite" value="${esc(i.hash)}"><button class="quiet" type="submit">take it back</button></form></li>`;
    })
    .join("");
  const make = v.left
    ? `<form method="post" action="/${esc(l.name)}/issue">${widget(sitekey)}<button type="submit">make an invite</button></form>
      <p class="fine">each invite is a link for one person and lasts 2 days<br>we email you a link first to make sure its really you<br>whoever you bring in is tied to you</p>`
    : "";
  const pips = Array.from({ length: v.released }, (_, i) => `<i class="${i < v.left ? "on" : ""}"></i>`).join("");
  return `<div class="row"><span class="k">invites</span><span class="pips" aria-label="${v.left} of ${v.released} left">${pips}</span><span class="dim">${v.left} of ${v.released} left</span></div>
    ${rows ? `<ul class="invites">${rows}</ul>` : ""}
    ${make}`;
}

export function page(l: Launch, total: number, view: View, sitekey: string, icon = ""): string {
  let body = "";
  let turnstile = false;
  let refresh = "";
  let wait = "";
  switch (view.kind) {
    case "open":
      turnstile = true;
      body = `
      <form method="post" action="/${esc(l.name)}/join">${widget(sitekey)}<button type="submit">count me in</button></form>
      <p class="fine">you sign in with discord<br>we only see your account id and your email</p>`;
      break;
    case "in":
      turnstile = view.left > 0 && !view.held;
      body = `
      <div class="place">
        <span class="k">${view.rank === null ? "youre in through a friend" : "your spot"}</span>
        <span class="rank" data-count="${view.rank ?? ""}">${view.rank === null ? "in" : `#${view.rank}`}</span>
      </div>
      ${view.held ? "" : invites(l, view, sitekey)}
      ${
        view.held
          ? `<p class="note warn">your invites are on hold while we take a quick look<br>${esc(view.held)}<br>your spot in line stays the same</p>`
          : view.rank === null
            ? `<p class="note ok">you get in with the friend who invited you</p>`
            : `<p class="note ok">youre on the list<br>invites go out from the front of the line</p>`
      }
      <div class="leave"><a class="quiet" href="/${esc(l.name)}/mine">download my data</a><form method="post" action="/${esc(l.name)}/leave"><button type="submit" class="quiet">leave the list and delete my data</button></form></div>`;
      break;
    case "invited":
      turnstile = true;
      body = `
      <p class="note ok">someone invited you<br>this link is for one person and works for another ${span(view.expires - view.now)}</p>
      <form method="post" action="/i/${esc(view.code)}">${widget(sitekey)}<button type="submit">take the invite</button></form>
      <p class="fine">you sign in with discord<br>then we email you a link to make sure its you</p>`;
      break;
    case "sent":
      refresh = `<noscript><meta http-equiv="refresh" content="5"></noscript>`;
      wait = ` data-wait="/${esc(l.name)}/wait?check"`;
      body = `
      <p class="note">check your email<br>we sent a link to <b>${esc(view.to)}</b> to ${esc(view.what)}</p>
      <div class="row"><span class="k">link works</span><span>${span(view.expires - view.now)}</span></div>
      <div class="row"><span class="k">open it on</span><span>any device</span></div>
      <p class="fine"><span class="pulse"></span>this page carries on by itself once you click it<br>nothing there yet so check your spam folder too</p>`;
      break;
    case "confirm":
      body = `
      <form method="post" action="/v/${esc(view.token)}" class="auto"><button type="submit">thats me</button></form>
      <p class="fine">one sec</p>`;
      break;
    case "confirmed":
      body = `
      <p class="note ok">all set<br>go back to the tab where you started and it carries on from there</p>`;
      break;
    case "minted":
      body = `
      <p class="note ok">heres your invite<br>its for one person and works for ${span(view.expires - view.now)}</p>
      <div class="link"><code>${esc(view.link)}</code><button type="button" class="copy" data-copy="${esc(view.link)}" hidden>copy</button></div>
      <p class="note warn">this is the only time youll see it<br>copy it now and send it to the one person its for</p>
      <a class="again" href="/${esc(l.name)}">back to your spot</a>`;
      break;
    case "privacy":
      body = notice(view.controller, view.contact);
      break;
    case "error":
      body = `
      <p class="note err">${lines(view.why)}</p>
      <a class="again" href="/${esc(l.name)}">go back</a>`;
      break;
  }
  const scripts = `<script type="module" src="/app.js"></script>${turnstile ? `<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>` : ""}`;
  const mark = icon
    ? `<img class="icon" src="${esc(icon)}" alt="" width="56" height="56" decoding="async">`
    : `<span class="icon none" aria-hidden="true">${esc(l.name.slice(0, 1))}</span>`;
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark light">
<meta name="theme-color" content="#0c0f13" media="(prefers-color-scheme: dark)">
<meta name="theme-color" content="#f2efe8" media="(prefers-color-scheme: light)">
${refresh}
<title>${esc(l.title ?? l.name)}</title>
<meta name="description" content="${esc(l.line.replaceAll("\n", " "))}">
<meta property="og:title" content="${esc(l.title ?? l.name)}">
<meta property="og:description" content="${esc(l.line.replaceAll("\n", " "))}">
<link rel="icon" href="/art/cygnet.png">
<link rel="stylesheet" href="/app.css">
<script src="/theme.js"></script>
</head>
<body${wait}${view.kind === "privacy" ? ' class="doc"' : ""}>
<canvas class="field" aria-hidden="true"></canvas>
<button type="button" class="theme" hidden aria-label="switch theme"></button>
<main>
  <figure class="hero" role="img" aria-label="a swan on the water">
    <pre class="ascii" aria-hidden="true">${still(grown(view))}</pre>
    <canvas class="swan" aria-hidden="true" data-t="${grown(view)}"></canvas>
  </figure>
  <section class="card">
    <header>
      <span class="ring">${mark}</span>
      <span><span class="kicker">${view.kind === "privacy" ? "cygnet lol" : "coming soon"}</span><h1>${esc(l.title ?? l.name)}</h1></span>
    </header>
    ${
      view.kind === "privacy"
        ? ""
        : `<p class="line">${lines(l.line)}</p>
    <p class="count"><b data-count="${total}">${total}</b> ${total === 1 ? "person" : "people"} on the list</p>`
    }
    ${body}
  </section>
  <footer class="privacy">
    <p>privacy</p>
    <p>kept<br>your discord id and your place in line and who invited you<br>your email and ip and rough location only as keyed hashes so nobody signs up twice</p>
    <p>deleted<br>ip and location hashes after 30 days<br>email hashes 30 days after launch<br>everything 12 months after launch or when you leave</p>
    <p>shared with<br>cloudflare who hosts this<br>discord for sign in<br>scaleway who sends the link emails<br>nobody else and never sold</p>
    <p>no tracking no ads no profiling<br>cookies only to keep you signed in</p>
    <p>you can see download or delete your data any time<br><a href="/privacy">full privacy notice</a></p>
    <p class="by"><img src="/art/cygnet.png" alt="" width="14" height="14"><img src="/art/skua.png" alt="" width="14" height="14">kept by cygnet with skua</p>
  </footer>
</main>
${scripts}
</body>
</html>`;
}

// notice is the full privacy notice: what the GDPR asks a notice to say
// (who, what, why and on what basis, with whom, for how long, your rights),
// in the page's voice.
function notice(controller: string, contact: string): string {
  const row = (k: string, v: string) => `<div class="term"><span class="k">${k}</span><p>${v}</p></div>`;
  return `<div class="notice">
    ${row("who", `<b>${esc(controller)}</b> decide what happens with this data<br>reach us at <b>${esc(contact)}</b>`)}
    ${row("what we keep", "your discord id and your place in line<br>who invited you and who you invited<br>the invites you made and when<br>a keyed hash of your email and of your ip and rough location<br>a tamper proof record of each step keyed to a hash of your id")}
    ${row("why", "to run the list and its invites because you asked to join<br>the hashes and the record to stop people signing up twice and to keep invites fair<br>that is our legitimate interest and we keep it as small as we can")}
    ${row("what we never keep", "your ip or email in plain form<br>your discord token which we drop right after reading your id and email<br>anything from the email we send you")}
    ${row("hashes", "a keyed hash cant be read back without our key but it is still personal data and we treat it like that")}
    ${row("shared with", "cloudflare hosts the site and the data and checks you are not a bot<br>discord signs you in under its own privacy policy<br>scaleway in france sends the link emails<br>cloudflare may process data in the us under the eu us data privacy framework and standard contractual clauses<br>nothing is sold or shared for ads")}
    ${row("how long", "ip and location hashes 30 days<br>email hashes until 30 days after launch<br>everything else until 12 months after launch<br>or until you leave the list whichever comes first")}
    ${row("when you leave", "your entry is deleted at once<br>where you invited someone your id is replaced by a keyed hash so the invite tree still adds up<br>the step record keeps that hash until the launch data is deleted")}
    ${row("cookies", "only ones that keep you signed in and in the middle of a step<br>your browser also remembers your theme and how grown the swan is<br>no analytics no tracking so there is nothing to consent to")}
    ${row("decisions", "nothing is decided about you by profiling<br>a second sign up from the same connection only holds invites until a person looks")}
    ${row("your rights", "see and download your data from your page<br>delete it by leaving the list<br>object to or ask to limit anything here or correct it by writing to us<br>complain to the data protection authority where you live")}
  </div>`;
}
