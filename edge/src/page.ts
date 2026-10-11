// The pages a launch has, drawn on the server: no script of ours, and
// Turnstile's only on a page that asks for it.

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
  | { kind: "prove"; address: string; expires: number; now: number; what: string }
  | { kind: "minted"; link: string; expires: number; now: number }
  | { kind: "error"; why: string };

export function esc(s: string): string {
  return s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

const n = (x: number) => x.toLocaleString("en-US");

// span is a duration in its two largest units, as UX.md has them: 45s,
// 12m, 3h 12m, 2d 4h.
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
  if (!v.released) return `<pre class="grid">invites   not released to you yet\nopens     when the server does</pre>`;
  const rows = v.invites
    .map((i) => {
      const made = `<span class="dim">made ${span(v.now - i.made)} ago</span>`;
      if (i.spent_at) return `<li><span class="ok">✓ used</span>${made}</li>`;
      if (i.revoked) return `<li><span class="dim">taken back</span>${made}</li>`;
      if (i.expires <= v.now) return `<li><span class="dim">expired, back with you</span>${made}</li>`;
      return `<li><span>▸ open · ${span(i.expires - v.now)} left</span><form method="post" action="/${esc(l.name)}/revoke"><input type="hidden" name="invite" value="${esc(i.hash)}"><button class="quiet" type="submit">take back</button></form></li>`;
    })
    .join("");
  const make = v.left
    ? `<form method="post" action="/${esc(l.name)}/issue">${widget(sitekey)}<button type="submit">make an invite</button></form>
      <p class="fine">an invite is a link for one person, good for 48 hours. making one takes a sign in with discord and an email from your address, so only you can, and whoever you bring in is linked to you</p>`
    : "";
  return `<pre class="grid">invites   <span class="bar">${"▰".repeat(v.left)}${"▱".repeat(Math.max(0, v.released - v.left))}</span>  ${v.left} of ${v.released} left</pre>
    ${rows ? `<ul class="invites">${rows}</ul>` : ""}
    ${make}`;
}

export function page(l: Launch, total: number, view: View, sitekey: string): string {
  let body = "";
  let turnstile = false;
  let refresh = "";
  switch (view.kind) {
    case "open":
      turnstile = true;
      body = `
      <form method="post" action="/${esc(l.name)}/join">${widget(sitekey)}<button type="submit">count me in</button></form>
      <p class="fine">you sign in with discord, which shares your account id and your verified email; nothing else is asked for</p>`;
      break;
    case "in":
      turnstile = view.left > 0 && !view.held;
      body = `
      <div class="place">
        <span class="label">${view.rank === null ? "you're in through an invite" : "your place"}</span>
        <span class="rank">${view.rank === null ? "✓" : `#${n(view.rank)}`}</span>
      </div>
      ${view.held ? "" : invites(l, view, sitekey)}
      ${
        view.held
          ? `<p class="note warn">! your invites wait for a quick check, because ${esc(view.held)}; your place doesn't</p>`
          : view.rank === null
            ? `<p class="note ok">✓ you get in with the person who invited you</p>`
            : `<p class="note ok">✓ you're in line; invites are released from the front of it</p>`
      }
      <form method="post" action="/${esc(l.name)}/leave" class="leave"><button type="submit" class="quiet">leave the list</button></form>`;
      break;
    case "invited":
      turnstile = true;
      body = `
      <p class="note ok">▸ you've been invited; this link works for one person, for ${span(view.expires - view.now)} more</p>
      <form method="post" action="/i/${esc(view.code)}">${widget(sitekey)}<button type="submit">take the invite</button></form>
      <p class="fine">you sign in with discord, then send one email from your address to show it's yours. nothing is ever emailed to you</p>`;
      break;
    case "prove":
      refresh = `<meta http-equiv="refresh" content="5">`;
      body = `
      <p class="note">▸ to ${esc(view.what)}, send any email from the address on your discord account to</p>
      <a class="mailto" href="mailto:${esc(view.address)}?subject=${encodeURIComponent(l.name)}">${esc(view.address)}</a>
      <pre class="grid">from      your discord email
says      anything, or nothing
works     ${span(view.expires - view.now)} more</pre>
      <p class="fine">this page moves on by itself once it arrives. the email is checked for your provider's signature, then dropped: nothing in it is kept</p>`;
      break;
    case "minted":
      body = `
      <p class="note ok">✓ here's your invite, for one person, good for ${span(view.expires - view.now)}</p>
      <p class="link" aria-label="invite link">${esc(view.link)}</p>
      <p class="note warn">! this is the only time it's shown; copy it now and send it to the one person it's for</p>
      <a class="again" href="/${esc(l.name)}">back to your place</a>`;
      break;
    case "error":
      body = `
      <p class="note err">✗ ${esc(view.why)}</p>
      <a class="again" href="/${esc(l.name)}">back</a>`;
      break;
  }
  const script = turnstile ? `<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>` : "";
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark light">
<meta name="theme-color" content="#1e2228" media="(prefers-color-scheme: dark)">
<meta name="theme-color" content="#f1ece2" media="(prefers-color-scheme: light)">
${refresh}
<title>${esc(l.name)} · get in line</title>
<meta name="description" content="${esc(l.line)}">
<meta property="og:title" content="${esc(l.name)}">
<meta property="og:description" content="${esc(l.line)}">
<meta property="og:image" content="/art/banner.png">
<link rel="icon" href="/art/cygnet.png">
<style>${style}</style>
</head>
<body>
<div class="sky" aria-hidden="true"></div>
<main>
  <section class="card">
    <p class="kicker">▸ launch</p>
    <h1>${esc(l.name)}</h1>
    <p class="line">${esc(l.line)}</p>
    <p class="count"><b>${n(total)}</b> ${total === 1 ? "person" : "people"} on the list</p>
    ${body}
  </section>
  <footer>
    <p class="birds"><img src="/art/cygnet.png" alt="" width="20" height="20"><img src="/art/skua.png" alt="" width="20" height="20">kept by cygnet, with skua</p>
    <p class="fine">we keep your discord id, your place and who invited you. your email, connection and rough area are kept only as keyed hashes that can't be turned back into them, to stop one person getting in twice, and the connection and area are forgotten after 30 days. we never send you email</p>
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
