// Backing the birds (docs/support.md): cygnet.lol/support. cygnet keeps the
// checkouts and the payments and watches the chains; the Worker asks her
// what is on offer, opens a checkout for whoever signed in, and shows it
// until it is paid. It keeps nothing of its own about anyone.
//
// The page is the birds' and the owner's, not cygnet's: the swan page's
// sky in glyphs across the whole page, but with a branch in it instead of
// water and a real merlin on the branch (public/merlin.js, measured off
// photographs; public/support.js brings her to life), and a sheet in her
// browns and cream that stands still. Its words are the owner's voice, lowercase with full punctuation
// and no oxford comma (the test holds it to that), where the rest of the
// site is cygnet's, with none. Addresses and amounts sit in code, which
// nothing lowercases: a Solana or TRON address read in lowercase sends
// money nowhere.

import type { Launch } from "./signup.ts";
import type { Bird } from "./store.ts";
import { frame, model, text } from "../public/merlin.js";

// Offer is cygnet.offer's answer.
export interface Offer {
  plans: { months: number; prices: Record<string, number> }[];
  // rates is the monthly amounts in US cents, the first the plain one.
  rates: number[];
  rails: { key: string; label: string }[];
  owner: { id: string; name?: string; avatar?: string };
}

// Checkout is cygnet.checkout's answer. Times are Unix ms, 0 for not yet.
export interface Checkout {
  id: string;
  state: "open" | "lapsed" | "paid" | "void";
  months: number;
  cents: number;
  price: number;
  gold: boolean;
  rail: string;
  label: string;
  amount: string;
  units: string;
  to: string;
  pay?: string;
  quote?: string;
  expires: number;
  paid?: number;
  until?: number;
  // stale is a SOL checkout whose price has run out: nothing sent now pays it.
  stale?: boolean;
}

// Status is cygnet.supporter's answer.
export interface Status {
  active: boolean;
  gold: boolean;
  since?: number;
  until?: number;
}

// SupportView is what a /support page shows.
export type SupportView =
  | { kind: "support"; offer: Offer | null; status: Status | null; signedIn: boolean }
  | { kind: "checkout"; c: Checkout; now: number }
  | { kind: "error"; why: string };

// supportPage stands for /support where the Worker wants a Launch: its
// sign in's state, and its errors, which html() draws here.
export const supportPage: Launch = { name: "support", title: "back the birds", line: "", minAgeDays: 0, server: true };

const esc = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);

// dollars is cents as a price: "$5", "$27.50".
export function dollars(cents: number): string {
  return cents % 100 === 0 ? `$${cents / 100}` : `$${(cents / 100).toFixed(2)}`;
}

const monthNames = ["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"];

// date is a day as the page writes one: "12 jan 2027".
export function date(ms: number): string {
  const d = new Date(ms);
  return `${d.getUTCDate()} ${monthNames[d.getUTCMonth()]} ${d.getUTCFullYear()}`;
}

// wait is a wait in its two largest units, as the rest of the site has it.
export function wait(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`;
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
}

// list joins words the way the owner does: commas, and "or" before the
// last, with no comma before it.
export function list(words: string[], last = "or"): string {
  if (words.length < 2) return words.join("");
  return `${words.slice(0, -1).join(", ")} ${last} ${words.at(-1)}`;
}

const lengthName = (months: number) => (months === 12 ? "a year" : months === 1 ? "a month" : `${months} months`);

// saving is what a plan saves on paying monthly: "2 months free", "save
// 7%", or nothing.
export function saving(months: number, price: number, rate: number): string {
  const full = months * rate;
  if (price >= full) return "";
  const free = (full - price) / rate;
  if (Number.isInteger(free)) return `${free} ${free === 1 ? "month" : "months"} free`;
  return `save ${Math.round((100 * (full - price)) / full)}%`;
}

// length is how long a plan runs, and what it saves on paying monthly.
export function length(months: number, price: number, rate: number): string {
  const save = saving(months, price, rate);
  return save ? `${lengthName(months)}, ${save}` : lengthName(months);
}

// duo is a mark in both its drawings, dusk and paper, for the page to show
// whichever fits the theme.
const duo = (name: string, cls: string, size: number) =>
  `<span class="duo ${cls}"><img class="dark" src="/art/support/${name}_dusk.png" alt="" width="${size}" height="${size}"><img class="light" src="/art/support/${name}_paper.png" alt="" width="${size}" height="${size}"></span>`;

const tile = (name: string) => duo(name, "tile", 32);

// marks is the coins and chains that have a mark, by the word the page
// writes for them; solana the chain wears sol's, as solana does.
const marks: Record<string, string> = { usdc: "usdc", usdt: "usdt", sol: "sol", solana: "sol", base: "base", polygon: "polygon", tron: "tron" };

// marked is a coin's or chain's name with its mark in front, at 16 px: an
// eighth of the tile, so each pixel of the mark is one of the screen's.
// A word without a mark is just the word.
export function marked(word: string): string {
  const m = marks[word.toLowerCase()];
  return m ? `<span class="marked">${duo(m, "mark", 16)}${esc(word)}</span>` : esc(word);
}

// markedText is a line from cygnet ("usdc on solana") with every coin and
// chain in it marked.
const markedText = (s: string) => s.split(" ").map(marked).join(" ");

// paying is the assets and chains a checkout takes, in words, each with
// its mark: "usdc, usdt or sol, on solana, base, polygon or tron".
function paying(rails: Offer["rails"]): string {
  const assets: string[] = [];
  const chains: string[] = [];
  for (const r of rails) {
    const [chain, asset] = r.key.split(":");
    if (!assets.includes(asset.toLowerCase())) assets.push(asset.toLowerCase());
    if (!chains.includes(chain)) chains.push(chain);
  }
  return `${list(assets.map(marked))}, on ${list(chains.map(marked))}`;
}

// flock is the birds on the roster, each with its picture, joined the
// owner's way: merlin, peregrine, cygnet and skua. With no roster to hand
// it names them without pictures.
function flock(birds: Bird[]): string {
  if (!birds.length) return list(["merlin", "peregrine", "cygnet", "skua"], "and");
  return list(
    birds.map((b) => `<span class="marked"><img class="mark bird" src="/bird/${esc(b.name)}.png?v=${esc(b.v)}" alt="" width="16" height="16">${esc(b.name)}</span>`),
    "and",
  );
}

// letter is the owner's word: who they are, what it costs and what it buys.
function letter(o: Offer | null, birds: Bird[], signedIn = false): string {
  const plain = o ? o.rates[0] : 500;
  const prices = o ? list(o.plans.map((p, i) => `${lengthName(p.months)}${i === 0 ? " is" : ""} ${dollars(p.prices[String(plain)])}`), "and") : "";
  const more = o && o.rates.length > 1 ? `<li>pay more if you like: ${list(o.rates.slice(1).map(dollars))} a month gets you the gold badge. the perks stay the same.</li>` : "";
  return `<section class="letter">
      <p>i build and run ${flock(birds)} on my own, in my own time and out of my own pocket, the ai moderation in the melting pot included.</p>
      <p>if they make your servers better, you can back them. it's ${dollars(plain)} a month, paid in crypto. it goes to me directly rather than to a company. it keeps the bots running and pays for what i build next.</p>
    </section>
    <section>
      <h2>what you get</h2>
      <ul class="perks">
        <li>${tile("badge")}<span>the supporter role and its badge in the servers i run</span></li>
        <li>${tile("previews")}<span>preview builds of the bots before they ship</span></li>
        <li>${tile("early")}<span>early access to my alpha projects</span></li>
        <li>${tile("say")}<span>a say in what gets built next</span></li>
      </ul>
    </section>
    <${signedIn ? "details" : "section"} class="works">
      ${signedIn ? "<summary><h2>how it works</h2></summary>" : "<h2>how it works</h2>"}
      <ul class="how">
        <li>you sign in with discord so i know who to thank. it tells me your account id and nothing else.</li>
        ${o ? `<li>you pay up front for as long as you pick: ${prices}.</li>${more}<li>you pay in ${paying(o.rails)}, from any wallet.</li>` : ""}
        <li>nothing renews by itself, so it never takes more than you sent.</li>
      </ul>
    </${signedIn ? "details" : "section"}>`;
}

const widget = (sitekey: string) =>
  `<div class="cf-turnstile" data-sitekey="${esc(sitekey)}" data-theme="auto" data-size="flexible" data-appearance="interaction-only"></div>`;

// supportBody is /support: the letter, then the way in. Signed out, a sign
// in behind Turnstile; signed in, their backing and the checkout's choices.
function supportBody(o: Offer | null, status: Status | null, signedIn: boolean, sitekey: string, birds: Bird[]): string {
  const body = letter(o, birds, signedIn);
  if (!o) return body + `<p class="note">backing opens soon. come back in a bit.</p>`;
  if (!signedIn) {
    return body + `<form class="act" method="post" action="/support/in">${widget(sitekey)}<button type="submit">sign in with discord to start</button></form>`;
  }
  let you = "";
  if (status?.active && status.until) {
    you = `<p class="note ok">you're backing the birds${status.gold ? " with the gold badge" : ""} until ${date(status.until)}. thank you.</p>`;
  } else if (status?.until) {
    you = `<p class="note">you backed the birds until ${date(status.until)}. pick it back up whenever you like.</p>`;
  }
  // The choices, as the good checkouts lay them out: the length as one
  // segmented row with each saving under it, the amounts as cards side by
  // side, and the ways to pay as a grid of tiles, each a coin on a chain.
  // Each choice is a radio inside its label, so it all works without
  // script; with it, a summary says what you're about to pay, and the
  // button says the total.
  const plain = o.rates[0];
  const option = (name: string, value: string | number, first: boolean, inner: string, label: string) =>
    `<label class="opt"><input type="radio" name="${name}" value="${esc(String(value))}"${first ? " checked" : ""} aria-label="${esc(label)}">${inner}</label>`;
  const plans = o.plans
    .map((p, i) => {
      const save = saving(p.months, p.prices[String(plain)], plain);
      return option("months", p.months, i === 0, `<span class="name">${lengthName(p.months)}</span><span class="sub">${save || "&#160;"}</span>`, length(p.months, p.prices[String(plain)], plain));
    })
    .join("");
  const rates = o.rates
    .map((c, i) =>
      option("cents", c, i === 0, `${i > 0 ? tile("badge_gold") : tile("badge")}<span class="name">${dollars(c)}</span><span class="sub">a month${i > 0 ? ", gold badge" : ""}</span>`, `${dollars(c)} a month${i > 0 ? ", gold badge" : ""}`),
    )
    .join("");
  const rails = o.rails
    .map((r, i) => {
      const [chain, asset] = r.key.split(":");
      const coin = asset.toLowerCase();
      return option("rail", r.key, i === 0, `<span class="name">${marked(coin)}</span><span class="sub">on ${marked(chain)}</span>`, r.label);
    })
    .join("");
  // What each length costs at each amount, for the summary to read.
  const prices = Object.fromEntries(o.plans.map((p) => [p.months, p.prices]));
  return (
    body +
    `${you}<form class="checkout" method="post" action="/support/quote" data-prices="${esc(JSON.stringify(prices))}">
      <fieldset class="seg"><legend>${status?.active ? "add on" : "how long"}</legend><div class="row">${plans}</div></fieldset>
      <fieldset class="cards"><legend>how much a month</legend><div class="row">${rates}</div></fieldset>
      <fieldset class="methods"><legend>pay with</legend><div class="row">${rails}</div></fieldset>
      <p class="summary" aria-live="polite" hidden></p>
      <button type="submit">go to checkout</button>
    </form>
    <p class="fine">the next page shows the exact amount and where to send it. an amount holds for an hour. in sol it holds for 20 minutes.</p>
    <form method="post" action="/support/out" class="out"><button class="quiet" type="submit">not you? sign out</button></form>`
  );
}

// checkoutBody is a checkout: what to send and where while it waits, then
// thanks once it's paid.
function checkoutBody(c: Checkout, now: number): string {
  const what = `${lengthName(c.months)}${c.gold ? ", gold badge" : ""}`;
  if (c.state === "paid") {
    return `<p class="note ok">it's in, thank you.${c.until ? ` you're backing the birds until ${date(c.until)}.` : ""}</p>
      <dl class="facts"><dt>for</dt><dd>${what}</dd></dl>
      <a class="again" href="/support">back to the start</a>`;
  }
  if (c.stale) {
    return `<p class="note">this checkout's price ran out, so nothing sent now pays it. start a new one for a fresh price.</p>
      <a class="again" href="/support">start again</a>`;
  }
  if (c.state === "void") {
    return `<p class="note">this checkout closed without a payment. start a new one whenever you like.</p>
      <a class="again" href="/support">start again</a>`;
  }
  const asset = c.rail.split(":")[1]?.toLowerCase() ?? "";
  const head =
    c.state === "open"
      ? `<p class="note">send exactly this within <b>${wait(c.expires - now)}</b>.</p>`
      : `<p class="note warn">this checkout's time is up. if you already sent it, it still counts, so keep this page open. if not, start a new one.</p>`;
  return `${head}
      <div class="copy"><span class="what">send</span><code>${esc(c.amount)}</code><span class="unit">${marked(asset)}</span><button type="button" data-copy="${esc(c.amount)}" hidden>copy</button></div>
      <div class="copy"><span class="what">to</span><code>${esc(c.to)}</code><button type="button" data-copy="${esc(c.to)}" hidden>copy</button></div>
      <dl class="facts">
        <dt>on</dt><dd>${markedText(c.label)}</dd>
        <dt>for</dt><dd>${what}</dd>
        <dt>price</dt><dd>${dollars(c.price)}${c.quote ? ` <span class="dim">at $${esc(c.quote)} a ${esc(asset)}</span>` : ""}</dd>
      </dl>
      ${c.pay ? `<a class="pay" href="${esc(c.pay)}">open it in your wallet</a>` : ""}
      <p class="fine"><span class="pulse"></span>this page checks for it by itself. it can take a few minutes after you send.</p>
      <p class="fine">the amount is how i know it's you, so send exactly this, with the fee on top. from an exchange, check that what arrives is this amount.</p>
      <a class="again" href="/support">start over</a>`;
}

// checkSays is what the checkout's quiet check answers: 204 while it
// waits, 200 once paid, 410 once it can't be.
export function checkSays(c: Checkout | null): number {
  if (!c || c.state === "void" || c.stale) return 410;
  return c.state === "paid" ? 200 : 204;
}

const lines = (s: string) => esc(s).replaceAll("\n", "<br>");

// stillFrame is merlin as one frame of text, for a page without script,
// drawn sparsely and once: it never changes.
let stillFrame = "";
function still(): string {
  if (!stillFrame) {
    const cols = 84, rows = 60;
    stillFrame = text(frame(model({ density: 0.4 }), cols, rows, 0.45, 0.08, 0.6, [-0.45, 0.75, 0.6], Math.round(rows * 0.84)), cols, rows);
  }
  return stillFrame;
}

// supportDocument is a /support page, whole: the sky, merlin on her
// branch, the title, the letter or the checkout on its sheet, and the
// footer every page of the site has.
export function supportDocument(view: SupportView, sitekey: string, birds: Bird[] = []): string {
  let body = "";
  let watching = "";
  let turnstile = false;
  let gold = false;
  let owner: Offer["owner"] | null = null;
  switch (view.kind) {
    case "support":
      turnstile = !!view.offer && !view.signedIn;
      body = supportBody(view.offer, view.status, view.signedIn, sitekey, birds);
      owner = view.offer?.owner ?? null;
      gold = !!view.status?.active && view.status.gold;
      break;
    case "checkout":
      body = checkoutBody(view.c, view.now);
      gold = view.c.gold;
      if (view.c.state === "open" || view.c.state === "lapsed") watching = ` data-wait="/support/${esc(view.c.id)}?check"`;
      break;
    case "error":
      body = `<p class="note err">${lines(view.why)}</p><a class="again" href="/support">go back</a>`;
      break;
  }
  const badge = gold ? "badge_gold" : "badge";
  // What a shared link says: the birds on the roster and, where the offer
  // is to hand, its plain rate.
  const rate = view.kind === "support" && view.offer ? view.offer.rates[0] : 0;
  const names = birds.length ? birds.map((b) => b.name) : ["merlin", "peregrine", "cygnet", "skua"];
  const description = esc(`back ${list(names, "and")}${rate ? ` for ${dollars(rate)} a month` : ""}, paid in crypto`);
  const from = owner?.name
    ? `<p class="from">${owner.avatar ? `<img src="${esc(owner.avatar)}" alt="" width="28" height="28">` : ""}<span>hey, i'm <b>${esc(owner.name)}</b>.</span></p>`
    : "";
  const flock = (birds.length ? birds : [{ name: "cygnet", v: "" }])
    .map((b) => `<img src="${b.v ? `/bird/${esc(b.name)}.png?v=${esc(b.v)}` : "/art/cygnet.png"}" alt="" width="14" height="14">`)
    .join("");
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="robots" content="noindex, nofollow, noarchive, noai, noimageai">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="light dark">
<meta name="theme-color" content="#80b6ec" media="(prefers-color-scheme: light)">
<meta name="theme-color" content="#04071a" media="(prefers-color-scheme: dark)">
${watching ? `<noscript><meta http-equiv="refresh" content="15"></noscript>\n` : ""}<title>back the birds</title>
<meta name="description" content="${description}">
<meta property="og:title" content="back the birds">
<meta property="og:description" content="${description}">
<meta property="og:type" content="website">
<meta property="og:site_name" content="cygnet.lol">
<link rel="icon" href="/art/support/badge_dusk.png">
<link rel="preload" href="/fonts/cascadia-mono.woff2" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="/support.css">
<script src="/theme.js"></script>
</head>
<body${watching}>
<canvas class="sky" aria-hidden="true"></canvas>
<button type="button" class="theme" hidden aria-label="switch theme"></button>
<main>
  <figure class="hero" role="img" aria-label="a merlin perched on a branch">
    <pre class="still" aria-hidden="true">${esc(still())}</pre>
  </figure>
  <div class="page">
  <header>
    <p class="kicker">supporter</p>
    <h1>${duo(badge, "badge", 40)}back the birds</h1>
  </header>
  <article class="sheet">
    ${from}
    ${body}
    <img class="rule" src="/art/support/rule.png" alt="" width="1040" height="32">
  </article>
  <footer><span class="by">${flock}kept by cygnet</span><a href="/privacy">privacy</a></footer>
  </div>
</main>
<script type="module" src="/support.js"></script>${turnstile ? `\n<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>` : ""}
</body>
</html>`;
}
