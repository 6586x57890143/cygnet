// The pages a launch has, drawn on the server. The only scripts are
// Turnstile's, on a page that asks for it, and confirm.js, which presses
// the button on an emailed link's page so a person clicks once and a mail
// scanner that only fetches the link confirms nothing.
//
// The voice is a person texting: lowercase, no punctuation, one thought a
// line. A line break does what a full stop would.

import type { Launch } from "./signup.ts";

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
  | { kind: "error"; why: string };

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

const widget = (sitekey: string) =>
  `<div class="cf-turnstile" data-sitekey="${esc(sitekey)}" data-theme="auto" data-size="flexible" data-appearance="interaction-only"></div>`;

function invites(l: Launch, v: Extract<View, { kind: "in" }>, sitekey: string): string {
  if (!v.released) return `<pre class="grid">invites   not yet\n          they go to the front of the line first</pre>`;
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
  return `<pre class="grid">invites   <span class="bar">${"▰".repeat(v.left)}${"▱".repeat(Math.max(0, v.released - v.left))}</span>  ${v.left} of ${v.released} left</pre>
    ${rows ? `<ul class="invites">${rows}</ul>` : ""}
    ${make}`;
}

export function page(l: Launch, total: number, view: View, sitekey: string): string {
  let body = "";
  let turnstile = false;
  let script = "";
  let refresh = "";
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
        <span class="label">${view.rank === null ? "youre in through a friend" : "your spot"}</span>
        <span class="rank">${view.rank === null ? "in" : `#${view.rank}`}</span>
      </div>
      ${view.held ? "" : invites(l, view, sitekey)}
      ${
        view.held
          ? `<p class="note warn">your invites are on hold while we take a quick look<br>${esc(view.held)}<br>your spot in line stays the same</p>`
          : view.rank === null
            ? `<p class="note ok">you get in with the friend who invited you</p>`
            : `<p class="note ok">youre on the list<br>invites go out from the front of the line</p>`
      }
      <form method="post" action="/${esc(l.name)}/leave" class="leave"><button type="submit" class="quiet">leave the list</button></form>`;
      break;
    case "invited":
      turnstile = true;
      body = `
      <p class="note ok">someone invited you<br>this link is for one person and works for another ${span(view.expires - view.now)}</p>
      <form method="post" action="/i/${esc(view.code)}">${widget(sitekey)}<button type="submit">take the invite</button></form>
      <p class="fine">you sign in with discord<br>then we email you a link to make sure its you</p>`;
      break;
    case "sent":
      refresh = `<meta http-equiv="refresh" content="5">`;
      body = `
      <p class="note">check your email<br>we sent a link to <b>${esc(view.to)}</b> to ${esc(view.what)}</p>
      <pre class="grid">link works   ${span(view.expires - view.now)}\nopen it on   any device</pre>
      <p class="fine">this page carries on by itself once you click it<br>nothing there yet so check your spam folder too</p>`;
      break;
    case "confirm":
      script = `<script src="/confirm.js" defer></script>`;
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
      <p class="link" aria-label="invite link">${esc(view.link)}</p>
      <p class="note warn">this is the only time youll see it<br>copy it now and send it to the one person its for</p>
      <a class="again" href="/${esc(l.name)}">back to your spot</a>`;
      break;
    case "error":
      body = `
      <p class="note err">${lines(view.why)}</p>
      <a class="again" href="/${esc(l.name)}">go back</a>`;
      break;
  }
  if (turnstile) script = `<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>`;
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark light">
<meta name="theme-color" content="#1e2228" media="(prefers-color-scheme: dark)">
<meta name="theme-color" content="#f1ece2" media="(prefers-color-scheme: light)">
${refresh}
<title>${esc(l.name)}</title>
<meta name="description" content="${esc(l.line.replaceAll("\n", " "))}">
<meta property="og:title" content="${esc(l.name)}">
<meta property="og:description" content="${esc(l.line.replaceAll("\n", " "))}">
<meta property="og:image" content="/art/banner.png">
<link rel="icon" href="/art/cygnet.png">
<style>${style}</style>
</head>
<body>
<div class="sky" aria-hidden="true"></div>
<main>
  <section class="card">
    <p class="kicker">coming soon</p>
    <h1>${esc(l.name)}</h1>
    <p class="line">${lines(l.line)}</p>
    <p class="count"><b>${total}</b> ${total === 1 ? "person" : "people"} on the list</p>
    ${body}
  </section>
  <footer>
    <p class="birds"><img src="/art/cygnet.png" alt="" width="20" height="20"><img src="/art/skua.png" alt="" width="20" height="20">kept by cygnet with skua</p>
    <p class="fine">we keep your discord id and your spot and who invited you<br>your email and connection and rough location are only kept as scrambled hashes so nobody gets in twice<br>the connection and location ones are deleted after 30 days</p>
  </footer>
</main>
${script}
</body>
</html>`;
}

// Dark first, skua's night with cygnet's water; light is cygnet's down
// with skua's brown. Every colour is one of the two birds'.
const style = `
:root {
  --bg: #1e2228; --card: #262c35; --line: #38425f; --text: #ece5d8; --muted: #8a9cb2;
  --accent: #d89b45; --on-accent: #1e2228; --shadow: #12151a;
  --ok: #7fa08c; --warn: #d1a06a; --err: #c0626a;
  --mono: ui-monospace, "Cascadia Mono", "JetBrains Mono", Menlo, Consolas, monospace;
  color-scheme: dark;
}
@media (prefers-color-scheme: light) {
  :root {
    --bg: #f1ece2; --card: #fbf8f2; --line: #7c889e; --text: #1e2228; --muted: #5d6779;
    --accent: #38425f; --on-accent: #f1ece2; --shadow: #a8744e;
    --ok: #4f7c5a; --warn: #a8744e; --err: #a8424a;
    color-scheme: light;
  }
}
* { box-sizing: border-box; }
html { background: var(--bg); }
body { margin: 0; min-height: 100dvh; background: var(--bg); color: var(--text);
  font: 16px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; text-transform: lowercase; }
main { max-width: 480px; margin: 0 auto; padding: 0 16px 48px; }
.sky { height: clamp(180px, 38vh, 380px); background: url(/art/banner.png) 74% center / cover no-repeat;
  image-rendering: pixelated; border-bottom: 4px solid var(--line); }
.card { position: relative; margin-top: clamp(-120px, -12vh, -64px); padding: 28px 24px 24px; background: var(--card);
  border: 2px solid var(--line); box-shadow: 6px 6px 0 var(--shadow); }
.kicker, .label, .count, .grid, .note, .fine, footer { font-family: var(--mono); }
.kicker { margin: 0; color: var(--accent); font-size: 13px; letter-spacing: .08em; }
.auto button { margin-bottom: 4px; }
h1 { margin: 4px 0 8px; font: 700 clamp(40px, 9vw, 52px)/1.05 var(--mono); letter-spacing: -.02em; }
.line { margin: 0 0 16px; color: var(--muted); }
.count { margin: 0 0 20px; padding: 8px 0; border-block: 2px dashed var(--line); font-size: 14px; color: var(--muted); }
.count b { color: var(--text); font-size: 18px; }
form { margin: 0; }
.cf-turnstile { margin-bottom: 12px; }
button { width: 100%; padding: 14px 16px; border: 2px solid var(--shadow); background: var(--accent); color: var(--on-accent);
  font: 700 16px var(--mono); text-transform: lowercase; cursor: pointer; box-shadow: 4px 4px 0 var(--shadow);
  transition: transform 60ms steps(2), box-shadow 60ms steps(2); }
button:hover { filter: brightness(1.06); }
button:active { transform: translate(3px, 3px); box-shadow: 1px 1px 0 var(--shadow); }
button:focus-visible, a:focus-visible { outline: 2px dashed var(--accent); outline-offset: 3px; }
button.quiet { width: auto; padding: 6px 0; border: 0; background: none; color: var(--muted); box-shadow: none;
  font-weight: 400; font-size: 13px; text-decoration: underline dotted; }
.place { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.label { color: var(--muted); font-size: 13px; }
.rank { font: 700 44px/1 var(--mono); color: var(--accent); font-variant-numeric: tabular-nums; }
.grid { margin: 0 0 16px; padding: 12px; background: var(--bg); border: 2px solid var(--line);
  font-size: 13px; color: var(--muted); white-space: pre-wrap; }
.bar { color: var(--accent); font-size: 16px; line-height: 1; letter-spacing: .12em; }
.note { margin: 0 0 12px; font-size: 14px; }
.ok { color: var(--ok); } .warn { color: var(--warn); } .err { color: var(--err); }
.dim { color: var(--muted); }
.again { color: var(--accent); font-family: var(--mono); }
.leave { text-align: right; }
.invites { list-style: none; margin: 0 0 16px; padding: 0; font: 13px var(--mono); }
.invites li { display: flex; justify-content: space-between; align-items: center; gap: 8px; padding: 6px 0; border-bottom: 1px dashed var(--line); }
.mailto { display: block; margin: 0 0 16px; padding: 12px; border: 2px dashed var(--accent); color: var(--accent);
  font: 700 15px var(--mono); text-transform: none; word-break: break-all; text-decoration: none; }
.link { margin: 0 0 12px; padding: 12px; background: var(--bg); color: var(--text); border: 2px solid var(--accent);
  font: 14px/1.4 var(--mono); text-transform: none; word-break: break-all; user-select: all; }
.fine { margin: 12px 0 0; color: var(--muted); font-size: 12px; }
.card .fine + .note { margin-top: 16px; }
footer { margin-top: 28px; color: var(--muted); font-size: 12px; }
.birds { display: flex; align-items: center; gap: 6px; margin: 0; }
.birds img { image-rendering: pixelated; border: 1px solid var(--line); }
.birds img + img { margin-right: 4px; }
`;
