// cygnet.lol's moving parts. Each is an extra: every page works without
// this file, and with reduced motion nothing here moves.
import { frame, model, ramp } from "./swan.js";

const still = matchMedia("(prefers-reduced-motion: reduce)").matches;

// The field: dim ASCII drifting like water, a little brighter where the
// pointer is. Drawn about 20 times a second, in a few alpha bands so the
// canvas changes colour only a handful of times a frame.
(() => {
  const c = document.querySelector("canvas.field");
  if (!c) return;
  const x = c.getContext("2d");
  const glyphs = " .:-=+*<>/{}01#";
  const bands = 6;
  let w = 0, h = 0, cell = 14, cols = 0, rows = 0, rgb = "96,118,150";
  let px = -1e4, py = -1e4;
  function size() {
    const dpr = Math.min(devicePixelRatio || 1, 2);
    w = innerWidth;
    h = innerHeight;
    c.width = w * dpr;
    c.height = h * dpr;
    x.setTransform(dpr, 0, 0, dpr, 0, 0);
    cell = w < 600 ? 12 : 14;
    cols = Math.ceil(w / (cell * 0.9));
    rows = Math.ceil(h / cell);
    x.font = `${cell - 2}px ui-monospace, Menlo, Consolas, monospace`;
    x.textBaseline = "top";
    rgb = getComputedStyle(document.documentElement).getPropertyValue("--field").trim() || rgb;
  }
  function draw(t) {
    x.clearRect(0, 0, w, h);
    const lanes = Array.from({ length: bands }, () => []);
    for (let j = 0; j < rows; j++) {
      const deep = j / rows;
      for (let i = 0; i < cols; i++) {
        let v = Math.sin(i * 0.17 + t * 0.55 + Math.sin(j * 0.11 + t * 0.2) * 2) * Math.cos(j * 0.21 - t * 0.35) + Math.sin((i + j) * 0.05 + t * 0.25);
        v = (v + 2) / 4;
        v *= 0.45 + deep * 0.75;
        const dx = i * cell * 0.9 - px, dy = j * cell - py;
        v += Math.max(0, 1 - Math.sqrt(dx * dx + dy * dy) / 160) * 0.55;
        if (v < 0.42) continue;
        const b = Math.min(bands - 1, Math.floor((v - 0.42) * bands * 1.6));
        lanes[b].push(i, j, Math.floor(v * 97 + i * 7 + j * 3) % glyphs.length);
      }
    }
    for (let b = 0; b < bands; b++) {
      x.fillStyle = `rgba(${rgb},${0.05 + b * 0.05})`;
      const lane = lanes[b];
      for (let k = 0; k < lane.length; k += 3) x.fillText(glyphs[lane[k + 2]], lane[k] * cell * 0.9, lane[k + 1] * cell);
    }
  }
  size();
  addEventListener("resize", size);
  matchMedia("(prefers-color-scheme: light)").addEventListener("change", size);
  addEventListener("pointermove", (e) => ((px = e.clientX), (py = e.clientY)), { passive: true });
  addEventListener("pointerleave", () => ((px = py = -1e4)));
  if (still) return draw(0);
  let last = 0;
  const loop = (now) => {
    if (!document.hidden && now - last > 50) {
      last = now;
      draw(now / 1000);
    }
    requestAnimationFrame(loop);
  };
  requestAnimationFrame(loop);
})();

// The swan: the model in swan.js drawn as shaded ASCII beside the card,
// its outline bright. It turns a little on the water, nods and sways its
// neck, leans toward the pointer, and grows from the stage last seen on
// this device to this page's. The water it sits on is a fine dashed line
// on a slow wave across the whole screen, level with the card's bottom edge
// on a wide screen and its top edge on a phone, and the reflection
// ripples with it. The still frame the page came with stays until the
// first live one is drawn.
(() => {
  const c = document.querySelector("canvas.swan");
  if (!c) return;
  const hero = c.parentElement;
  const card = document.querySelector(".card");
  const x = c.getContext("2d");
  const to = Number(c.dataset.t);
  let t = to;
  try {
    t = Number(localStorage.getItem("swan") ?? 0);
    localStorage.setItem("swan", String(to));
  } catch {}
  if (still) t = to;
  const from = t;
  let w = 0, h = 0, cy = 0, bx = 0, by = 0, cw = 6, ch = 10, cols = 0, rows = 0, ink = "236,230,218", deep = "3,5,8", depth = 0.55;
  let lean = 0, aim = 0;
  function place() {
    const dpr = Math.min(devicePixelRatio || 1, 2);
    w = innerWidth;
    h = innerHeight;
    c.width = w * dpr;
    c.height = h * dpr;
    x.setTransform(dpr, 0, 0, dpr, 0, 0);
    const r = hero.getBoundingClientRect();
    const k = card.getBoundingClientRect();
    const wide = w >= 900;
    cy = wide ? k.bottom : k.top - 10;
    ch = wide ? 8 : 6;
    cw = ch * 0.6;
    // The swan's box: a little over half the hero wide, its water line
    // (at 72% of its height) on the card's.
    const bw = Math.min(wide ? r.width * 0.86 : w * 0.92, 760);
    const bh = bw * 0.8;
    bx = (wide ? r.left + r.width / 2 : w / 2) - bw / 2;
    by = cy - bh * 0.72;
    cols = Math.floor(bw / cw);
    rows = Math.floor(bh / ch);
    x.font = `${ch}px ui-monospace, Menlo, Consolas, monospace`;
    x.textBaseline = "top";
    const css = getComputedStyle(document.documentElement);
    ink = css.getPropertyValue("--ink").trim() || ink;
    deep = css.getPropertyValue("--deep").trim() || deep;
    depth = Number(css.getPropertyValue("--deep-a").trim()) || depth;
  }
  // wave is the water's height at a point along it.
  const wave = (gx, s) => Math.sin(gx * 0.012 + s * 1.1) * 4 + Math.sin(gx * 0.041 - s * 0.7) * 1.4;
  // hash is a steady random number for an integer pair.
  const hash = (a, b) => {
    const v = Math.sin(a * 12.9898 + b * 78.233) * 43758.5453;
    return v - Math.floor(v);
  };
  function draw(now, pts) {
    const s = now / 1000;
    lean += (aim - lean) * 0.04;
    const f = frame(pts, cols, rows, -0.55 + Math.sin(s * 0.23) * 0.22 + lean, 0.13 + Math.sin(s * 0.41) * 0.025, cw / ch);
    x.clearRect(0, 0, w, h);
    // Below the surface the water is darker than the night above it, and
    // deeper toward the bottom of the page, so it reads as a body of water.
    const g = x.createLinearGradient(0, cy - 8, 0, h);
    g.addColorStop(0, `rgba(${deep},0)`);
    g.addColorStop(Math.min(0.2, 22 / Math.max(1, h - cy)), `rgba(${deep},${depth * 0.7})`);
    g.addColorStop(1, `rgba(${deep},${depth})`);
    x.fillStyle = g;
    x.fillRect(0, cy - 8, w, h - cy + 8);
    // The water's surface across the whole page: rows of ripples moving
    // with the wave, brightest at the waterline and fading with depth, a
    // few faint rows above it for the far water behind the bird. Drawn
    // first, so the swan and its reflection sit in it.
    for (let r = -3; r <= 9; r++) {
      const gap = r < 0 ? ch * 0.55 : ch * (0.7 + r * 0.12);
      const ry = cy + Math.sign(r) * Math.abs(r) * gap;
      const fade = r < 0 ? 0.1 + 0.03 * (3 + r) : 0.32 * (1 - r / 10);
      for (let gx = (r * 13) % 11; gx < w; gx += cw * 1.25) {
        const v = Math.sin(gx * 0.021 + r * 1.7 + s * 0.9) + Math.sin(gx * 0.053 - s * 1.3 + r * 2.3) * 0.7;
        if (v < 0.55) continue;
        x.fillStyle = `rgba(${ink},${(fade * Math.min(1, v - 0.4)).toFixed(3)})`;
        x.fillText(v > 1.25 ? "~" : "-", gx, ry + wave(gx, s) * (0.6 - Math.min(0.4, Math.abs(r) * 0.05)) - ch * 0.5);
      }
    }
    const bands = 10;
    const lanes = Array.from({ length: bands }, () => []);
    const rims = [];
    const glints = [];
    const ripples = [];
    for (let j = 0; j < rows; j++) {
      for (let i = 0; i < cols; i++) {
        const q = j * cols + i;
        const v = f.shade[q];
        if (v >= 0 && f.water[q]) ripples.push(i, j, v);
        else if (v >= 0 && f.edge[q]) rims.push(i, j, v);
        else if (v >= 0) lanes[Math.min(bands - 1, Math.floor(v * bands))].push(i, j);
        else if (f.refl[q] > 0.03) glints.push(i, j, f.refl[q]);
      }
    }
    const lift = (by + f.horizon * ch) - cy; // so the bird's water is the card's
    for (let b = 0; b < bands; b++) {
      x.fillStyle = `rgba(${ink},${0.3 + (b / (bands - 1)) * 0.7})`;
      const g = ramp[1 + Math.round((b / (bands - 1)) * (ramp.length - 2))];
      const lane = lanes[b];
      for (let n = 0; n < lane.length; n += 2) x.fillText(g, bx + lane[n] * cw, by - lift + lane[n + 1] * ch);
    }
    // The rings on the water round the hull, drawn as the rest of the
    // water is: the same strokes, centred on the same line, riding the same
    // wave, fainter outward.
    for (let n = 0; n < ripples.length; n += 3) {
      const gx = bx + ripples[n] * cw;
      const j = ripples[n + 1];
      x.fillStyle = `rgba(${ink},${(0.08 + ripples[n + 2] * 0.42).toFixed(3)})`;
      x.fillText((ripples[n] + j) % 3 ? "-" : "~", gx, by - lift + j * ch - ch * 0.5 + wave(gx, s) * (0.6 - Math.min(0.4, Math.abs(j - f.horizon) * 0.05)));
    }
    // The outline: every cell at the edge of the bird, bright and solid.
    x.fillStyle = `rgba(${ink},0.95)`;
    for (let n = 0; n < rims.length; n += 3) x.fillText(rims[n + 2] > 0.5 ? "#" : "*", bx + rims[n] * cw, by - lift + rims[n + 1] * ch);
    // The reflection, carried by the wave and broken up by ripples the
    // deeper it goes.
    for (let n = 0; n < glints.length; n += 3) {
      const i = glints[n], j = glints[n + 1], v = glints[n + 2];
      const gx = bx + i * cw;
      const deep = j - f.horizon;
      const rx = gx + Math.sin(deep * 0.9 + s * 2.2) * Math.min(5, deep * 0.5);
      x.fillStyle = `rgba(${ink},${(0.05 + v * 0.35).toFixed(3)})`;
      x.fillText(ramp[1 + Math.min(3, Math.floor(v * 8))], rx, by - lift + j * ch + wave(gx, s));
    }
    // The water: a fine dashed line across the screen on a slow wave,
    // brightest under the swan.
    const mid = bx + (cols * cw) / 2;
    const drift = (s * 9) % 7;
    // It stops where the bird sits, rather than running through it.
    const hull = (gx) => {
      const i = Math.floor((gx - bx) / cw);
      if (i < 0 || i >= cols) return false;
      for (const j of [f.horizon - 1, f.horizon]) {
        const q = j * cols + i;
        if (f.shade[q] >= 0 && !f.water[q]) return true;
      }
      return false;
    };
    for (let gx = -drift; gx < w; gx += 7) {
      if (hull(gx) || hull(gx + 4)) continue;
      const near = Math.max(0, 1 - Math.abs(gx - mid) / (cols * cw * 0.9));
      x.fillStyle = `rgba(${ink},${(0.1 + 0.42 * near * near).toFixed(3)})`;
      x.fillRect(gx, cy + wave(gx, s) * 0.6, 4, 1);
    }
    x.font = `${ch}px ui-monospace, Menlo, Consolas, monospace`;
  }
  place();
  addEventListener("resize", place);
  addEventListener("scroll", place, { passive: true });
  matchMedia("(prefers-color-scheme: light)").addEventListener("change", place);
  addEventListener("pointermove", (e) => (aim = (e.clientX / innerWidth - 0.5) * 0.6), { passive: true });
  const start = performance.now();
  let last = 0;
  let pts = model(t);
  const loop = (now) => {
    if (!document.hidden && now - last > 33) {
      last = now;
      if (t !== to) {
        const k = Math.min(1, (now - start) / 2600);
        t = from + (to - from) * (1 - Math.pow(1 - k, 3));
        if (k === 1) t = to;
      }
      const s = still ? 0 : now / 1000;
      // The neck nods and sways; the model is drawn fresh for it.
      pts = model(t, 1, 0.12 + Math.sin(s * 0.19) * 0.12, Math.sin(s * 0.37) * 0.07);
      draw(still ? 0 : now, pts);
      hero.classList.add("live");
    }
    if (!still) requestAnimationFrame(loop);
  };
  requestAnimationFrame(loop);
})();

// The server's picture comes in once it has loaded, and goes if it can't.
for (const img of document.querySelectorAll("img.icon")) {
  const on = () => img.classList.add("in");
  if (img.complete && img.naturalWidth) on();
  else img.addEventListener("load", on);
  img.addEventListener("error", () => img.replaceWith(Object.assign(document.createElement("span"), { className: "icon none", textContent: (document.title || "s")[0] })));
}

// Numbers count up to themselves.
if (!still) {
  for (const el of document.querySelectorAll("[data-count]")) {
    const n = Number(el.dataset.count);
    if (!n) continue;
    const pre = el.textContent.startsWith("#") ? "#" : "";
    const start = performance.now();
    const frame = (now) => {
      const k = Math.min(1, (now - start) / 1100);
      el.textContent = pre + Math.round(n * (1 - Math.pow(1 - k, 3)));
      if (k < 1) requestAnimationFrame(frame);
    };
    requestAnimationFrame(frame);
  }
}

// The invite link copies in one tap.
for (const b of document.querySelectorAll("button.copy")) {
  b.hidden = false;
  b.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(b.dataset.copy);
      b.textContent = "copied";
    } catch {
      b.textContent = "select it and copy";
    }
  });
}

// An emailed link's page presses its own button, so a person clicks once
// and a scanner that only fetches the link confirms nothing.
document.querySelector("form.auto")?.submit();

// The wait page checks quietly and moves on once the link is clicked.
const check = document.body.dataset.wait;
if (check) {
  const tick = async () => {
    try {
      const r = await fetch(check, { cache: "no-store" });
      if (r.status === 200 || r.status === 410) return location.reload();
    } catch {}
    setTimeout(tick, 2500);
  };
  setTimeout(tick, 2500);
}
