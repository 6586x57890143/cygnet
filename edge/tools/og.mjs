// og draws public/art/og.gif, the picture a link to cygnet.lol unfurls
// into: the page's own swan on its water in one scene of its day, the
// card reduced to the name. It drives the page in headless Chromium on a
// clock of its own, so every frame is the one asked for.
//
//   node tools/og.mjs [scene] [url]   # night (default), morning, day or
//                                     # evening; https://cygnet.lol
//   node tools/og.mjs support [url] [scene] # the supporters club's merlin
//
// Needs Chrome or Edge, and ffmpeg on the PATH.
import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const support = process.argv[2] === "support";
const scene = (support ? process.argv[4] : process.argv[2]) ?? "night";
const url = process.argv[3] ?? (support ? "https://support.cygnet.lol" : "https://cygnet.lol");
// Where the scene is in the page's day, which starts at night and holds
// each scene 75 s of every 100.
const at = { night: 10, morning: 110, day: 210, evening: 310 }[scene];
if (at === undefined) throw new Error(`no scene ${scene}`);
const out = fileURLToPath(new URL(support ? "../public/art/support/og.gif" : "../public/art/og.gif", import.meta.url));
// The page is laid out a little over the width where it goes wide, and
// drawn at 4/3 so its glyphs are big enough to read in a chat: 1200 by
// 630, then 800 by 420, still more than a chat shows.
const W = 900, H = 473, dpr = 4 / 3;
// Frames a second and seconds of the loop, then the last fade seconds
// blended over the first so the swan's sway doesn't jump where it starts
// again.
const fps = 20, secs = 6, fade = 0.6;
const colors = 108, dither = "bayer:bayer_scale=4";

const browser = [
  process.env.CHROME,
  "C:/Program Files/Google/Chrome/Application/chrome.exe",
  "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
  "/usr/bin/google-chrome",
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
].find((p) => p && existsSync(p));
if (!browser) throw new Error("no Chrome or Edge found; set CHROME");

const dir = mkdtempSync(join(tmpdir(), "og-"));
const port = 9333;
const proc = spawn(browser, [`--headless=new`, `--remote-debugging-port=${port}`, `--user-data-dir=${join(dir, "profile")}`, `--hide-scrollbars`, `--mute-audio`, "about:blank"], { stdio: "ignore" });

try {
  let target;
  for (let i = 0; i < 50 && !target; i++) {
    await new Promise((r) => setTimeout(r, 200));
    target = await fetch(`http://127.0.0.1:${port}/json/list`).then((r) => r.json()).then((l) => l.find((t) => t.type === "page")).catch(() => null);
  }
  const ws = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((r) => ws.addEventListener("open", r, { once: true }));
  let seq = 0;
  const waiting = new Map();
  const events = [];
  ws.addEventListener("message", (e) => {
    const m = JSON.parse(e.data);
    if (m.id && waiting.has(m.id)) {
      const [ok, no] = waiting.get(m.id);
      waiting.delete(m.id);
      m.error ? no(new Error(m.error.message)) : ok(m.result);
    } else if (m.method) events.push(m.method);
  });
  const cdp = (method, params = {}) => new Promise((ok, no) => {
    waiting.set(++seq, [ok, no]);
    ws.send(JSON.stringify({ id: seq, method, params }));
  });

  await cdp("Page.enable");
  await cdp("Emulation.setDeviceMetricsOverride", { width: W, height: H, deviceScaleFactor: dpr, mobile: false });
  await cdp("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }, { name: "prefers-reduced-motion", value: "no-preference" }] });
  // The clock: animation frames run only when tick says, at its time. The
  // page's day reads performance.now, which jumps to the scene once the
  // page has started its day. The swan is grown, and the card holds only
  // the name and its line, large enough to read at the size a chat shows
  // it, which is about half of this.
  await cdp("Page.addScriptToEvaluateOnNewDocument", {
    source: `
      localStorage.setItem("swan", "1");
      let ms = 0;
      const queue = [];
      window.skip = 0;
      performance.now = () => ms + skip;
      requestAnimationFrame = (f) => queue.push(f);
      window.tick = (dt) => { ms += dt; for (const f of queue.splice(0)) f(ms); };
      // The page has measured itself by then: the resize has it measure again.
      addEventListener("DOMContentLoaded", () => (document.head.append(Object.assign(document.createElement("style"), { textContent: \`
        ${support ? `
        .sheet, footer, button.theme, .still { display: none !important; }
        main { position: static; display: block; width: 100%; max-width: none; min-height: 0; height: 100vh; margin: 0; padding: 0; }
        .hero { position: absolute; left: 0; top: 0; width: 480px; height: 440px; margin: 0; }
        .page { position: absolute; right: 26px; top: 124px; width: 400px; margin: 0; padding: 0; }
        .page header { display: block; margin: 0; text-align: left; color: #ecd9ae; text-shadow: 0 2px 12px #040716; }
        .page .kicker { font-size: 27px; letter-spacing: .04em; margin: 0 0 12px; color: #d9a62e; }
        .page h1 { display: block; font-size: 60px; line-height: 1.08; letter-spacing: -.035em; margin: 0; }
        .page .preview-domain { font-size: 18px; color: #c9ad85; margin: 22px 0 0; }
        ` : `
        .soon, .fine, footer, button.theme, .ring, canvas.field { display: none !important; }
        main { display: block; min-height: 0; height: 100vh; padding: 0; }
        .hero { position: absolute; left: -130px; width: 720px; top: 0; height: 100%; }
        .card { position: absolute; right: 22px; bottom: 84px; width: 392px; text-align: center; padding: 18px 10px 24px; }
        .card header { display: block; margin: 0 0 8px; }
        .card .kicker { display: none; }
        .card h1 { font-size: 92px; line-height: 1; letter-spacing: -0.02em; }
        .card .line { white-space: nowrap; font-size: 20px; line-height: 1.3; height: 1.3em; overflow: hidden; margin: 18px 0 0; color: var(--text); opacity: .8; }
        `}
      \` })), dispatchEvent(new Event("resize"))));
      ${support ? `addEventListener("DOMContentLoaded", () => {
        document.querySelector(".page header").innerHTML = '<p class="kicker">bird bot</p><h1>supporters<br>club</h1><p class="preview-domain">support.cygnet.lol</p>';
      });` : ""}
    `,
  });
  await cdp("Page.navigate", { url });
  while (!events.includes("Page.loadEventFired")) await new Promise((r) => setTimeout(r, 100));
  await cdp("Runtime.evaluate", { expression: "document.fonts.ready", awaitPromise: true });
  await cdp("Runtime.evaluate", { expression: `skip = ${at * 1000}` });

  const dt = 1000 / fps;
  // Six seconds first, so the scene has settled.
  for (let i = 0; i < fps * secs; i++) await cdp("Runtime.evaluate", { expression: `tick(${dt})` });
  const n = Math.round(fps * (secs + fade));
  for (let i = 0; i < n; i++) {
    await cdp("Runtime.evaluate", { expression: `tick(${dt})` });
    const { data } = await cdp("Page.captureScreenshot", { format: "png" });
    writeFileSync(join(dir, `${String(i).padStart(4, "0")}.png`), Buffer.from(data, "base64"));
  }
  ws.close();

  // The loop is secs long; its tail (the day come round again) fades in
  // over its head, then one palette for the whole thing, dithered gently.
  const f = Math.round(fps * fade), body = fps * secs;
  execFileSync("ffmpeg", [
    "-y", "-loglevel", "error", "-framerate", String(fps), "-i", join(dir, "%04d.png"),
    "-filter_complex",
    `[0]crop=1200:630:0:0,scale=800:420:flags=lanczos,split=3[a][b][c];[a]trim=start_frame=${f}:end_frame=${body},setpts=PTS-STARTPTS[mid];` +
      `[b]trim=start_frame=${body}:end_frame=${body + f},setpts=PTS-STARTPTS[tail];` +
      `[c]trim=end_frame=${f},setpts=PTS-STARTPTS[head];` +
      `[tail][head]blend=all_expr='A*(1-N/${f})+B*(N/${f})'[seam];` +
      `[seam][mid]concat=n=2,split[s0][s1];[s0]palettegen=max_colors=${colors}:stats_mode=diff[p];[s1][p]paletteuse=dither=${dither}:diff_mode=rectangle`,
    "-loop", "0", "-map_metadata", "-1", out,
  ]);
  console.log(out);
} finally {
  proc.kill();
  await new Promise((r) => setTimeout(r, 500));
  rmSync(dir, { recursive: true, force: true });
}
