// Backing the birds: the owner's letter and its voice, the choices, the
// checkout as it waits and once it's paid, and the words a price is said in.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { keyed, reserved } from "../src/signup.ts";
import { checkSays, date, dollars, length, list, paymentGroups, supportDocument, type Checkout, type Offer, type SupportView } from "../src/support.ts";

const offer: Offer = {
  plans: [
    { months: 1, prices: { "500": 500, "1000": 1000, "2000": 2000 } },
    { months: 3, prices: { "500": 1400, "1000": 2800, "2000": 5600 } },
    { months: 6, prices: { "500": 2700, "1000": 5400, "2000": 10800 } },
    { months: 12, prices: { "500": 5000, "1000": 10000, "2000": 20000 } },
  ],
  rates: [500, 1000, 2000],
  rails: [
    { key: "solana:USDC", label: "usdc on solana" },
    { key: "solana:USDT", label: "usdt on solana" },
    { key: "base:USDC", label: "usdc on base" },
    { key: "polygon:USDC", label: "usdc on polygon" },
    { key: "tron:USDT", label: "usdt on tron" },
    { key: "solana:SOL", label: "sol" },
  ],
  owner: { id: "175928847299117063", name: "steve", avatar: "https://cdn.discordapp.com/embed/avatars/0.png" },
};

const now = Date.UTC(2026, 9, 25, 12);
const address = "mvines9iiHiQTysrwkJjGf2gb9Ex9jXJX8ns3qwf2kN";
const checkout: Checkout = {
  id: "abcdefghijklmnopqrstuvwxyz",
  state: "open",
  months: 3,
  cents: 1000,
  price: 2800,
  gold: true,
  rail: "solana:USDC",
  label: "usdc on solana",
  amount: "28.004217",
  units: "28004217",
  to: address,
  pay: `solana:${address}?amount=28.004217&spl-token=EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`,
  expires: now + 59 * 60_000,
};

const render = (view: SupportView) => supportDocument(view, "key");

test("shared support and private checkout links use the same public club preview", () => {
  const previews = [
    render({ kind: "support", offer, status: null, signedIn: true }),
    render({ kind: "support", offer: null, status: null, signedIn: false }),
    render({ kind: "checkout", c: checkout, now }),
    render({ kind: "error", why: "private checkout unavailable" }),
  ];
  for (const html of previews) {
    const head = html.slice(0, html.indexOf("</head>"));
    assert.match(head, /property="og:title" content="bird bot supporters club"/);
    assert.match(head, /property="og:url" content="https:\/\/support\.cygnet\.lol\/"/);
    assert.match(head, /property="og:image" content="https:\/\/support\.cygnet\.lol\/art\/support\/og\.gif\?v=1"/);
    assert.match(head, /property="og:image:type" content="image\/gif"/);
    assert.match(head, /name="twitter:card" content="summary_large_image"/);
    for (const privateValue of [checkout.id, checkout.to, checkout.amount, offer.owner.id, offer.owner.avatar!]) {
      assert.ok(!head.includes(privateValue), "preview metadata must not include checkout or account data");
    }
  }
  const gif = readFileSync(new URL("../public/art/support/og.gif", import.meta.url));
  assert.equal(gif.subarray(0, 6).toString("ascii"), "GIF89a");
  assert.equal(gif.readUInt16LE(6), 800);
  assert.equal(gif.readUInt16LE(8), 420);
  assert.ok(gif.includes(Buffer.from("NETSCAPE2.0")), "the preview must loop");
  assert.ok(gif.length < 4 * 1024 * 1024, "keep the chat preview within its asset budget");
});

// words is what a person reads in the owner's letter and the choices: no
// markup, no code blocks (addresses and amounts), no names in bold.
const words = (html: string) =>
  html
    .replace(/<(style|script|title|code)[\s\S]*?<\/\1>/g, "")
    .replace(/<meta[^>]*>/g, "")
    .replace(/<b>[\s\S]*?<\/b>/g, "")
    .replace(/<[^>]+>/g, " ")
    .replace(/&#\d+;/g, "'")
    .replace(/\s+/g, " ");

// text is the page with its apostrophes read back.
const text = (html: string) => html.replace(/&#39;/g, "'");

// unmarked is text with each mark in front of a name taken out, leaving
// the name, so a sentence can be checked as it reads.
const unmarked = (html: string) =>
  text(html).replace(/<span class="marked">(?:<span class="duo mark">(?:<img [^>]*>)+<\/span>|<img [^>]*>)([^<]*)<\/span>/g, "$1");

// duo is a mark exactly as the page draws one: its dusk drawing and its
// paper one, for the theme to choose between.
const duo = (name: string, cls: string, size: number) =>
  `<span class="duo ${cls}"><img class="dark" src="/art/support/${name}_dusk.png" alt="" width="${size}" height="${size}"><img class="light" src="/art/support/${name}_paper.png" alt="" width="${size}" height="${size}"></span>`;

test("currencies retain only offered networks and unknown names remain escaped", () => {
  const rails = [...offer.rails,
    { key: "base:ETH", label: "eth on base" },
    { key: "ethereum:ETH", label: "eth on ethereum" },
    { key: "monero:XMR", label: "xmr on monero" },
    { key: "base:ETH", label: "duplicate" },
    { key: '<img src=x>:XMR', label: "invalid" }];
  const groups = paymentGroups(rails);
  assert.deepEqual(groups.map((g) => g.asset), ["usdc", "usdt", "sol", "eth", "xmr"]);
  assert.deepEqual(groups.find((g) => g.asset === "eth")!.rails.map((r) => r.key), ["base:ETH", "ethereum:ETH"]);
  assert.equal(groups.flatMap((g) => g.rails).length, 9);
  const html = text(render({ kind: "support", offer: { ...offer, rails }, status: null, signedIn: true }));
  assert.equal(html.match(/name="rail" value=/g)?.length, 9);
  assert.equal(html.match(/name="rail" value="[^"]+" checked/g)?.length, 1);
  assert.equal(html.match(/class="asset-networks"/g)?.length, 5);
  assert.doesNotMatch(html, /value="<img|aria-label="duplicate"/);
  assert.match(html, /eth_dusk\.png/);
  assert.match(html, /xmr_dusk\.png/);
  assert.deepEqual(paymentGroups([]), []);
});

test("support mutations reject missing, foreign and sibling origins before touching services", async () => {
  const { default: worker } = await import("../src/index.ts");
  for (const path of ["quote", "in", "out"]) {
    for (const origin of [null, "null", "https://cygnet.lol", "https://evil.example", "http://support.cygnet.lol"]) {
      const headers = origin === null ? {} : { origin };
      const response = await worker.fetch(new Request(`https://support.cygnet.lol/${path}`, { method: "POST", headers }), {} as Env, {} as ExecutionContext);
      assert.equal(response.status, 403, `${path}: ${origin}`);
      assert.equal(response.headers.get("referrer-policy"), "no-referrer");
    }
  }
  const response = await worker.fetch(new Request("https://support.cygnet.lol/out", { method: "POST", headers: { origin: "https://support.cygnet.lol" } }), {} as Env, {} as ExecutionContext);
  assert.equal(response.status, 404, "same origin reaches the unconfigured service check");
});

test("support form pages preserve browser origins through sign-in, quote and sign-out", async (t) => {
  const { default: worker } = await import("../src/index.ts");
  const calls: string[] = [];
  t.mock.method(globalThis, "fetch", async (url: string, init: RequestInit) => {
    if (url === "https://challenges.cloudflare.com/turnstile/v0/siteverify") {
      calls.push("turnstile");
      return Response.json({ success: true, hostname: "support.cygnet.lol", action: "support_signin" });
    }
    assert.equal(url, "https://hub.example/v1/call");
    const request = JSON.parse(String(init.body));
    calls.push(request.method);
    return Response.json({ body: request.method === "cygnet.offer" ? offer : request.method === "cygnet.supporter" ? { active: false, gold: false } : checkout });
  });
  const env = {
    CYGNET_HUB_URL: "https://hub.example", CYGNET_HUB_TOKEN: "fixture",
    HMAC_KEY: "fixture-only-cookie-key", DISCORD_CLIENT_ID: String(123456789012345678n),
    TURNSTILE_SECRET: "fixture", TURNSTILE_SITEKEY: "fixture",
    DB: { prepare() { return { bind() { return this; }, async all() { return { results: [] }; } }; } },
  } as unknown as Env;
  const origin = "https://support.cygnet.lol";
  const get = await worker.fetch(new Request(origin), env, {} as ExecutionContext);
  assert.equal(get.status, 200);
  assert.equal(get.headers.get("referrer-policy"), "same-origin", "no-referrer would null real form Origins");
  assert.match(await get.text(), /method="post" action="\/in"/);
  const login = await worker.fetch(new Request(origin + "/in", { method: "POST", headers: { origin }, body: new URLSearchParams({ "cf-turnstile-response": "fixture" }) }), env, {} as ExecutionContext);
  assert.equal(login.status, 303);
  const oauth = new URL(login.headers.get("location")!);
  assert.equal(oauth.origin, "https://discord.com");
  assert.equal(oauth.searchParams.get("redirect_uri"), origin + "/auth/discord");
  assert.equal(oauth.searchParams.get("scope"), "identify");
  assert.match(login.headers.get("set-cookie")!, /^cy_state=.+; Max-Age=600; Path=\/; HttpOnly; Secure; SameSite=Lax$/);
  assert.ok(calls.includes("turnstile"));
  const value = `support.${offer.owner.id}.${Date.now() + 600_000}`;
  const cookie = `cy_support=${value}~${await keyed(env.HMAC_KEY, "cookie", value)}`;
  const signed = await worker.fetch(new Request(origin, { headers: { cookie } }), env, {} as ExecutionContext);
  assert.equal(signed.headers.get("referrer-policy"), "same-origin");
  const quote = await worker.fetch(new Request(origin + "/quote", { method: "POST", headers: { origin, cookie }, body: new URLSearchParams({ months: "1", cents: "500", rail: "base:ETH" }) }), env, {} as ExecutionContext);
  assert.equal(quote.status, 303);
  assert.equal(quote.headers.get("location"), "/" + checkout.id);
  assert.ok(calls.includes("cygnet.quote"));
  const out = await worker.fetch(new Request(origin + "/out", { method: "POST", headers: { origin, cookie } }), env, {} as ExecutionContext);
  assert.equal(out.status, 303);
  assert.equal(out.headers.get("location"), "/");
  assert.equal(out.headers.get("set-cookie"), "cy_support=; Max-Age=0; Path=/; HttpOnly; Secure; SameSite=Lax");
});

test("sign-in refuses missing, oversized, failed, replayed and wrong-context verification", async (t) => {
  const { default: worker } = await import("../src/index.ts");
  const env = {
    CYGNET_HUB_URL: "https://hub.example", CYGNET_HUB_TOKEN: "fixture",
    TURNSTILE_SECRET: "fixture", TURNSTILE_SITEKEY: "fixture",
    DB: { prepare() { return { bind() { return this; }, async all() { return { results: [] }; } }; } },
  } as unknown as Env;
  let response: unknown, calls = 0;
  t.mock.method(globalThis, "fetch", async () => {
    calls++;
    if (response instanceof Error) throw response;
    return response instanceof Response ? response : Response.json(response);
  });
  for (const token of ["", "x".repeat(2049)]) {
    const result = await worker.fetch(new Request("https://support.cygnet.lol/in", { method: "POST", headers: { origin: "https://support.cygnet.lol" }, body: new URLSearchParams({ "cf-turnstile-response": token }) }), env, {} as ExecutionContext);
    assert.equal(result.status, 403);
  }
  assert.equal(calls, 0, "invalid input should not call Siteverify");
  for (response of [
    { success: false, "error-codes": ["timeout-or-duplicate"] },
    { success: true, hostname: "cygnet.lol", action: "support_signin" },
    { success: true, hostname: "support.cygnet.lol", action: "other_action" },
    { success: true }, { success: "true", hostname: "support.cygnet.lol", action: "support_signin" },
    new Response("unavailable", { status: 503 }), new Response("not JSON"), new Error("network unavailable"),
  ]) {
    const result = await worker.fetch(new Request("https://support.cygnet.lol/in", { method: "POST", headers: { origin: "https://support.cygnet.lol" }, body: new URLSearchParams({ "cf-turnstile-response": "fixture-token" }) }), env, {} as ExecutionContext);
    assert.equal(result.status, 403);
    assert.equal(result.headers.has("set-cookie"), false, "failed verification must not start OAuth");
  }
  const page = render({ kind: "support", offer, status: null, signedIn: false });
  assert.match(page, /data-appearance="always"/);
  assert.match(page, /data-action="support_signin"/);
  assert.match(page, /button type="submit" disabled/);
  assert.ok(page.indexOf('src="/support-verification.js"') < page.indexOf('src="https://challenges.cloudflare.com/turnstile/v0/api.js"'), "callbacks must exist before a cached Turnstile API executes");
});

test("the owner's pages are in the owner's voice", () => {
  const pages = {
    signedOut: render({ kind: "support", offer, status: null, signedIn: false }),
    signedIn: render({ kind: "support", offer, status: null, signedIn: true }),
    backing: render({ kind: "support", offer, status: { active: true, gold: true, since: now, until: now + 90 * 86_400_000 }, signedIn: true }),
    lapsedBacker: render({ kind: "support", offer, status: { active: false, gold: false, since: now - 90 * 86_400_000, until: now - 86_400_000 }, signedIn: true }),
    nothingYet: render({ kind: "support", offer: null, status: null, signedIn: false }),
    open: render({ kind: "checkout", c: checkout, now }),
    lapsed: render({ kind: "checkout", c: { ...checkout, state: "lapsed" }, now }),
    paid: render({ kind: "checkout", c: { ...checkout, state: "paid", paid: now, until: Date.UTC(2027, 0, 25) }, now }),
    void: render({ kind: "checkout", c: { ...checkout, state: "void" }, now }),
  };
  const marks = new RegExp(`[!${String.fromCharCode(0x2014, 0x2026, 0x201c, 0x201d)}]`);
  for (const [name, html] of Object.entries(pages)) {
    const w = words(html);
    assert.doesNotMatch(w, /[A-Z]/, `${name} has capitals: ${w.match(/.{0,30}[A-Z].{0,30}/)?.[0]}`);
    assert.doesNotMatch(w, /, (and|or) /, `${name} has an oxford comma: ${w.match(/.{0,30}, (and|or) .{0,30}/)?.[0]}`);
    assert.doesNotMatch(w, marks, `${name} has a mark the owner doesn't use`);
    for (const ai of ["unlock", "elevate", "seamless", "journey", "empower", "delve", "exclusive"]) {
      assert.doesNotMatch(w, new RegExp(`\\b${ai}`), `${name} says ${ai}`);
    }
    // The page draws its own scene, nothing of the swan's.
    assert.doesNotMatch(html, /app\.js|app\.css|class="swan"|class="ascii"|class="field"/, `${name} borrows the swan's page`);
    assert.match(html, /<link rel="stylesheet" href="\/support\.css">/);
    assert.match(html, /href="https:\/\/cygnet\.lol\/privacy">privacy<\/a>/, `${name} has no privacy link`);
  }
});

test("the letter says who, what it costs, what it buys and how", () => {
  const html = text(render({ kind: "support", offer, status: null, signedIn: false }));
  const says = unmarked(html);
  for (const want of [
    "hey, i'm <b>steve</b>.",
    "it's $5 a month, paid in crypto. it goes to me directly rather than to a company.",
    "a month is $5, 3 months $14, 6 months $27 and a year $50.",
    "$10 or $20 a month gets you the gold badge.",
    "you pay in usdc on solana, base or polygon, usdt on solana or tron or sol on solana, from any wallet.",
    "nothing renews by itself",
    "preview builds of the bots before they ship",
    "early access to my alpha projects",
  ]) {
    assert.ok(says.includes(want), `the letter lacks ${want}`);
  }
  // Every coin and chain it names wears its mark; solana wears sol's.
  for (const [mark, times] of [["usdc", 1], ["usdt", 1], ["sol", 4], ["base", 1], ["polygon", 1], ["tron", 1]] as const) {
    assert.equal(html.split(duo(mark, "mark", 16)).length - 1, times, mark);
  }
  assert.ok(html.includes(`<p class="kicker">supporter</p>\n    <h1>${duo("badge", "badge", 40)}back the birds</h1>`), "the title, its badge in both drawings");
  // Every perk wears its mark.
  for (const mark of ["badge", "previews", "early", "say"]) assert.ok(html.includes(`<li>${duo(mark, "tile", 32)}<span>`), mark);
  // The scene: the sky for the script to draw in, and merlin as text for a
  // page without it, her eye in it.
  assert.match(html, /<canvas class="sky" aria-hidden="true"><\/canvas>/);
  assert.match(html, /<pre class="still" aria-hidden="true">[^<]*@[^<]*<\/pre>/);
  assert.match(html, /<img class="dark" src="\/art\/support\/badge_dusk\.png"[^>]*><img class="light" src="\/art\/support\/badge_paper\.png"/);
  assert.match(html, /<img class="rule" src="\/art\/support\/rule\.png"/);
  assert.match(html, /<form class="act" method="post" action="\/in"><div class="cf-turnstile" data-sitekey="key"[\s\S]*sign in with discord to start/);
  assert.match(html, /turnstile\/v0\/api\.js/);
  assert.doesNotMatch(html, /175928847299117063/, "the owner's id isn't printed");
  const none = render({ kind: "support", offer: null, status: null, signedIn: false });
  assert.match(none, /backing opens soon/);
  assert.doesNotMatch(none, /cf-turnstile|\/in"|turnstile\/v0/, "nothing to sign in for");
});

test("signed in, the choices are every plan, rate and rail on offer", () => {
  const html = text(render({ kind: "support", offer, status: null, signedIn: true }));
  assert.match(html, /<form class="checkout" method="post" action="\/quote" data-prices="/);
  // The prices the summary reads are the plans' own, by length and amount.
  const prices = JSON.parse(html.match(/data-prices="([^"]*)"/)![1].replace(/&#34;/g, '"'));
  assert.deepEqual(prices, Object.fromEntries(offer.plans.map((p) => [p.months, p.prices])));
  // The length: one segmented row, the saving under each, the first chosen.
  assert.equal(html.match(/name="months"/g)?.length, 4);
  assert.match(html, /<fieldset class="seg"><legend>how long<\/legend>/);
  assert.match(html, /value="1" checked aria-label="a month"><span class="name">a month<\/span><span class="sub">&#160;<\/span>/);
  assert.match(html, /value="3" aria-label="3 months, save 7%"><span class="name">3 months<\/span><span class="sub">save 7%<\/span>/);
  assert.match(html, /value="6" aria-label="6 months, save 10%">/);
  assert.match(html, /value="12" aria-label="a year, 2 months free"><span class="name">a year<\/span><span class="sub">2 months free<\/span>/);
  // The amounts: cards, the gold ones wearing the gold badge.
  assert.match(html, /value="500" checked aria-label="\$5 a month">.*?badge_dusk\.png.*?<span class="name">\$5<\/span><span class="sub">a month<\/span>/);
  assert.match(html, /value="1000" aria-label="\$10 a month, gold badge">.*?badge_gold_dusk\.png.*?<span class="sub">a month, gold badge<\/span>/);
  // The ways to pay: a tile for each rail, its coin and its chain marked.
  assert.equal(html.match(/name="rail" value=/g)?.length, 6);
  assert.ok(
    html.includes(`name="rail" value="solana:USDC" checked aria-label="usdc on solana"><span class="name"><span class="marked">${duo("sol", "mark", 16)}solana</span></span>`),
    "usdc's network is marked",
  );
  assert.match(html, /name="rail" value="tron:USDT" aria-label="usdt on tron">.*?tron_dusk/);
  // The summary is the script's to fill; without it, it stays out of the way.
  assert.match(html, /<p class="summary" aria-live="polite" hidden><\/p>/);
  // Signed in, how it works is folded away.
  assert.match(html, /<details class="works">\s*<summary><h2>how it works<\/h2><\/summary>/);
  assert.match(html, /action="\/out"/);
  assert.doesNotMatch(html, /cf-turnstile|turnstile\/v0/, "signed in, no check");
  const backing = text(render({ kind: "support", offer, status: { active: true, gold: true, since: now, until: Date.UTC(2027, 0, 25) }, signedIn: true }));
  assert.match(backing, /you're backing the birds with the gold badge until 25 jan 2027\. thank you\./);
  assert.match(backing, /<legend>add on<\/legend>/);
  assert.match(backing, /badge_gold_dusk\.png" alt="" width="40"/, "a gold backer's title wears the gold badge");
});

test("a checkout shows exactly what to send, where, and checks itself", () => {
  const html = text(render({ kind: "checkout", c: checkout, now }));
  // Addresses are case sensitive: they go in code, which the page never
  // lowercases, and their copy button copies them as they are.
  assert.match(html, new RegExp(`<code>${address}</code><button type="button" data-copy="${address}" hidden>copy</button>`));
  assert.ok(html.includes(`<code>28.004217</code><span class="unit"><span class="marked">${duo("usdc", "mark", 16)}usdc</span></span><button type="button" data-copy="28.004217"`), "the amount's unit, marked");
  assert.match(unmarked(html), /<dt>on<\/dt><dd>usdc on solana<\/dd>/);
  assert.ok(html.includes(`<dt>on</dt><dd><span class="marked">${duo("usdc", "mark", 16)}usdc</span> on <span class="marked">${duo("sol", "mark", 16)}solana</span></dd>`), "the rail, marked");
  assert.doesNotMatch(readFileSync(new URL("../public/support.css", import.meta.url), "utf8"), /text-transform/, "nothing on the page is lowercased by style");
  assert.match(html, /send exactly this within <b><time class="countdown" data-countdown data-remaining="3540000">59:00<\/time><\/b>/);
  assert.match(html, /<a class="pay" href="solana:mvines9iiHiQTysrwkJjGf2gb9Ex9jXJX8ns3qwf2kN\?amount=28\.004217&#38;spl-token=EPjF/);
  assert.match(html, /<dt>for<\/dt><dd>3 months, gold badge<\/dd>/);
  assert.match(html, /<dt>price<\/dt><dd>\$28<\/dd>/);
  assert.match(html, /<body data-wait="\/abcdefghijklmnopqrstuvwxyz\?check">/);
  assert.match(html, /<noscript><meta http-equiv="refresh"/);
  assert.match(html, /badge_gold_dusk\.png/, "a gold checkout wears the gold badge");
  const base = render({ kind: "checkout", c: { ...checkout, rail: "base:USDC", label: "usdc on base", to: "0x1111111111111111111111111111111111111111", pay: undefined }, now });
  assert.doesNotMatch(base, /class="pay"/, "no wallet link where there's no standard one");
  const sol = render({ kind: "checkout", c: { ...checkout, rail: "solana:SOL", label: "sol", quote: "150.00" }, now });
  assert.match(sol, /\$28 <span class="dim">at \$150\.00 a sol<\/span>/);
  const lapsed = text(render({ kind: "checkout", c: { ...checkout, state: "lapsed" }, now }));
  assert.match(lapsed, /time is up\. if you already sent it, keep this page open/);
  assert.doesNotMatch(lapsed, /data-countdown|class="pay"/, "expired quotes don't invite a new payment");
  const expired = render({ kind: "checkout", c: { ...checkout, expires: now - 1 }, now });
  assert.doesNotMatch(expired, /data-countdown|class="pay"/, "expiry doesn't wait for the background sweep");
  assert.match(lapsed, /data-wait=/, "a lapsed checkout still takes a payment, so it still checks");
  const paid = text(render({ kind: "checkout", c: { ...checkout, state: "paid", paid: now, until: Date.UTC(2027, 0, 25) }, now }));
  assert.match(paid, /it's in, thank you\. you're backing the birds until 25 jan 2027\./);
  assert.doesNotMatch(paid, /data-wait|<code>/, "nothing left to send or check");
  const gone = render({ kind: "checkout", c: { ...checkout, state: "void" }, now });
  assert.match(gone, /closed without a payment/);
  assert.doesNotMatch(gone, /data-wait|<code>/);
  const err = render({ kind: "error", why: "that checkout isnt yours or its gone\nstart again" });
  assert.match(err, /<p class="note err">that checkout isnt yours or its gone<br>start again<\/p><a class="again" href="\/">go back<\/a>/);
  assert.match(render({ kind: "checkout", c: { ...checkout, to: "<script>" }, now }), /&#60;script&#62;/, "what cygnet sends is escaped");
});

test("the letter names every bird on the roster, each with its picture", () => {
  const birds = [
    { name: "merlin", v: "1" },
    { name: "peregrine", v: "2" },
    { name: "cygnet", v: "3" },
    { name: "skua", v: "4" },
  ];
  const html = text(supportDocument({ kind: "support", offer, status: null, signedIn: false }, "key", birds));
  assert.ok(unmarked(html).includes("i build and run merlin, peregrine, cygnet and skua on my own"), "the roster, the owner's way");
  for (const b of birds) assert.match(html, new RegExp(`<img class="mark bird" src="/bird/${b.name}\\.png\\?v=${b.v}"[^>]*>${b.name}</span>`), b.name);
  // With no roster to hand, the four by name.
  assert.ok(unmarked(render({ kind: "support", offer, status: null, signedIn: false })).includes("i build and run merlin, peregrine, cygnet and skua on my own"));
});

test("the quiet check says when to stop", () => {
  assert.equal(checkSays({ ...checkout, state: "open" }), 204);
  assert.equal(checkSays({ ...checkout, state: "lapsed" }), 204);
  assert.equal(checkSays({ ...checkout, state: "paid" }), 200);
  assert.equal(checkSays({ ...checkout, state: "void" }), 410);
  assert.equal(checkSays({ ...checkout, state: "lapsed", stale: true }), 204);
  assert.equal(checkSays(null), 410);
});

test("a SOL checkout whose price ran out offers nothing to pay", () => {
  const html = render({ kind: "checkout", c: { ...checkout, state: "lapsed", stale: true, pay: undefined }, now });
  assert.match(html, /price ran out/);
  assert.doesNotMatch(html, /still counts/);
  assert.doesNotMatch(html, new RegExp(checkout.to));
});

test("XMR explains its own address, partial payments and confirmation delay", () => {
  const html = text(render({ kind: "checkout", c: { ...checkout, rail: "monero:XMR", label: "xmr on monero", amount: "0.025", to: "monero-checkout-address", pay: "monero:monero-checkout-address?tx_amount=0.025" }, now }));
  assert.match(html, /this address belongs only to this checkout/);
  assert.match(html, /smaller payments sent before its price runs out add up/);
  assert.match(html, /at least 10 confirmations/);
  assert.doesNotMatch(html, /the amount is how i know it's you/);
  assert.match(html, /href="monero:monero-checkout-address\?tx_amount=0.025"/);
});

test("prices, dates and lists in the owner's words", () => {
  assert.equal(dollars(500), "$5");
  assert.equal(dollars(2750), "$27.50");
  assert.equal(date(Date.UTC(2027, 0, 2)), "2 jan 2027");
  assert.equal(list(["a"]), "a");
  assert.equal(list(["a", "b"]), "a or b");
  assert.equal(list(["a", "b", "c"], "and"), "a, b and c");
  assert.equal(length(1, 500, 500), "a month");
  assert.equal(length(12, 5000, 500), "a year, 2 months free");
  assert.equal(length(2, 500, 500), "2 months, 1 month free");
  assert.equal(length(2, 900, 500), "2 months, save 10%");
  assert.equal(length(3, 1400, 500), "3 months, save 7%");
});

test("the site's own pages are names no launch can take", () => {
  for (const name of ["support", "privacy", "admin", "auth", "bird"]) assert.ok(reserved.includes(name), name);
});
