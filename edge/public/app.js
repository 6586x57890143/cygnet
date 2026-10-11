// cygnet.lol's moving parts. Each is an extra: every page works without
// this file, and with reduced motion nothing here moves.
import { model } from "./swan.js";

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

// The swan: the model in swan.js as a sparse, see-through cloud of points
// behind everything but the field. It faces you with its wings spread
// round the card, its neck rising over it: it turns a little, beats its
// wings slowly, nods and sways, leans toward the pointer, and every point
// shimmers and twinkles on its own. It grows from the stage last seen on
// this device to this page's. The water it stands in is a line of digits
// in a slow wave, level with the card's bottom edge on a wide screen and
// its top edge on a phone, and the reflection ripples with it. The still
// frame the page came with stays until the first live one is drawn.
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
  const density = 0.42;
  let w = 0, h = 0, cx = 0, cy = 0, size = 200, ink = "236,230,218";
  let lean = 0, aim = 0, tilt = 0, aimTilt = 0;
  function place() {
    const dpr = Math.min(devicePixelRatio || 1, 2);
    w = innerWidth;
    h = innerHeight;
    c.width = w * dpr;
    c.height = h * dpr;
    x.setTransform(dpr, 0, 0, dpr, 0, 0);
    const k = card.getBoundingClientRect();
    const wide = w >= 900;
    cx = k.left + k.width / 2;
    // The wings span about 4.6 sizes and the head stands 1.75 above the
    // water: as big as fits, with the head clear of the card's top.
    size = Math.min(w / 4.9, h * 0.34);
    cy = wide ? Math.min(k.bottom, k.top - 28 + 1.72 * size) : k.top - 8;
    size = Math.min(size, (cy - 14) / 1.8);
    ink = getComputedStyle(document.documentElement).getPropertyValue("--ink").trim() || ink;
  }
  // wave is the water's height at a point along it.
  const wave = (gx, s) => Math.sin(gx * 0.012 + s * 1.1) * 5 + Math.sin(gx * 0.041 - s * 0.7) * 1.6;
  // hash is a steady random number for an integer pair.
  const hash = (a, b) => {
    const v = Math.sin(a * 12.9898 + b * 78.233) * 43758.5453;
    return v - Math.floor(v);
  };
  function draw(now, pts) {
    const s = now / 1000;
    lean += (aim - lean) * 0.03;
    tilt += (aimTilt - tilt) * 0.03;
    const yaw = -Math.PI / 2 + Math.sin(s * 0.13) * 0.32 + lean;
    const pitch = 0.12 + Math.sin(s * 0.21) * 0.04 + tilt;
    const cyw = Math.cos(yaw), syw = Math.sin(yaw), cp = Math.cos(pitch), sp = Math.sin(pitch);
    const bob = Math.sin(s * 0.5) * size * 0.01;
    x.clearRect(0, 0, w, h);
    // The water: digits along a slow wave, brightest under the swan.
    x.font = "10px ui-monospace, Menlo, Consolas, monospace";
    x.textBaseline = "middle";
    // Each digit changes on its own every few seconds.
    const step = 9;
    for (let gx = 0, q = 0; gx < w; gx += step, q++) {
      const near = Math.max(0, 1 - Math.abs(gx - cx) / (size * 3.2));
      const tick = Math.floor(s * 0.5 + hash(q, 1) * 7);
      x.fillStyle = `rgba(${ink},${(0.1 + 0.42 * near * near).toFixed(3)})`;
      x.fillText(String(Math.floor(hash(q, tick) * 10)), gx, cy + wave(gx, s));
    }
    for (let p = 0, n = 0; p < pts.length; p += 7, n++) {
      const ph = n * 2.399;
      // Each point shimmers a little around where it belongs.
      const px = pts[p] + Math.sin(s * 0.9 + ph) * 0.01, py = pts[p + 1] + Math.cos(s * 0.7 + ph * 1.3) * 0.01, pz = pts[p + 2];
      const x1 = px * cyw + pz * syw, z1 = -px * syw + pz * cyw;
      const y2 = py * cp - z1 * sp, z2 = py * sp + z1 * cp;
      const nx = pts[p + 3] * cyw + pts[p + 5] * syw, nz0 = -pts[p + 3] * syw + pts[p + 5] * cyw;
      const ny = pts[p + 4] * cp - nz0 * sp, nz = pts[p + 4] * sp + nz0 * cp;
      const persp = 5 / (5 - z2);
      const sx = cx + x1 * size * persp, sy = cy + bob - y2 * size * persp;
      const light = Math.max(0, -0.35 * nx + 0.6 * ny + 0.7 * Math.abs(nz));
      const near = Math.min(1, Math.max(0, (z2 + 2) / 4));
      const twinkle = 0.72 + 0.28 * Math.sin(s * 2.1 + ph * 3.7);
      const kind = pts[p + 6];
      let a = (0.14 + 0.5 * light) * (0.5 + 0.5 * near) * twinkle;
      if (kind === 1) a = 0.75;
      else if (kind === 3) a *= 0.55;
      const d = (0.7 + near * 1.1) * Math.max(1, size / 240);
      x.fillStyle = `rgba(${ink},${a.toFixed(3)})`;
      x.fillRect(sx, sy, d, d);
      // The reflection: the point again below the water, fainter, carried
      // by the wave and broken up by ripples the deeper it goes.
      if (py > 0) {
        const depth = y2 * size * persp * 0.6;
        const rx = sx + Math.sin(depth * 0.16 + s * 2.2) * Math.min(6, depth * 0.04);
        x.fillStyle = `rgba(${ink},${(a * 0.2 * (1 - Math.min(1, depth / (size * 1.1)))).toFixed(3)})`;
        x.fillRect(rx, cy + wave(rx, s) + 6 + depth, d, d * 0.6);
      }
    }
  }
  place();
  addEventListener("resize", place);
  addEventListener("scroll", place, { passive: true });
  matchMedia("(prefers-color-scheme: light)").addEventListener("change", place);
  addEventListener(
    "pointermove",
    (e) => {
      aim = (e.clientX / innerWidth - 0.5) * 0.7;
      aimTilt = (e.clientY / innerHeight - 0.5) * 0.16;
    },
    { passive: true },
  );
  const start = performance.now();
  let last = 0;
  const loop = (now) => {
    if (!document.hidden && now - last > 30) {
      last = now;
      if (t !== to) {
        const k = Math.min(1, (now - start) / 2800);
        t = from + (to - from) * (1 - Math.pow(1 - k, 3));
        if (k === 1) t = to;
      }
      const s = still ? 0 : now / 1000;
      // The neck nods and sways, and the wings beat slowly.
      draw(still ? 0 : now, model(t, density, 0.25 + Math.sin(s * 0.17) * 0.25, Math.sin(s * 0.4) * 0.09, 0.9 + Math.sin(s * 0.55) * 0.1));
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
