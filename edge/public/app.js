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
  const notes = document.querySelector(".privacy");
  const x = c.getContext("2d");
  const to = Number(c.dataset.t);
  let t = to;
  try {
    t = Number(localStorage.getItem("swan") ?? 0);
    localStorage.setItem("swan", String(to));
  } catch {}
  if (still) t = to;
  const from = t;
  let w = 0, h = 0, cy = 0, bx = 0, by = 0, cw = 6, ch = 10, cols = 0, rows = 0;
  // The scene, blended over time: base is where it starts (night in dark
  // mode, day in light), cycle when the cycle started, sc the scene as
  // drawn, eased toward where the cycle is. ax and aw are the stretch of
  // the page the sun and the moon cross.
  let base = "", cycle = 0, sc = null, ink = "236,234,228", wink = "236,234,228", ax = 0, aw = 0, orb = 40;
  // dark is how far the swan's ink has gone dark (1) from light (0), wdark
  // the water's; page is the page's own colour under the sky.
  let dark = 0, wdark = 0, page = [10, 13, 20];
  let pale = [236, 234, 228], deep = [20, 24, 34], lit = [250, 250, 250];
  let mx = 0, my = 0, sunX = 0, sunY = 0, moonX = 0, moonY = 0, sunUp = 0, moonUp = 0;
  let lean = 0, aim = 0, float = 0, span = 400;
  function place() {
    const dpr = Math.min(devicePixelRatio || 1, 2);
    w = innerWidth;
    h = innerHeight;
    c.width = w * dpr;
    c.height = h * dpr;
    x.setTransform(dpr, 0, 0, dpr, 0, 0);
    const r = hero.getBoundingClientRect();
    // Measured without its bob, or the water would chase the card.
    const held = card.style.transform;
    card.style.transform = "none";
    const k = card.getBoundingClientRect();
    card.style.transform = held;
    float = k.left + k.width / 2;
    span = k.width;
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
    // The sun and the moon cross the swan's side of the page on a wide
    // screen, all of it on a phone.
    orb = Math.max(30, Math.min(72, w * 0.03));
    ax = wide ? r.left : 0;
    aw = wide ? r.width : w;
    const bg = getComputedStyle(document.body).backgroundColor.match(/\d+/g);
    if (bg) page = bg.slice(0, 3).map(Number);
    const was = base;
    base = getComputedStyle(document.documentElement).getPropertyValue("--scene").trim() === "day" ? "day" : "night";
    if (base !== was) cycle = performance.now() / 1000;
  }

  // The scenes, in the order the day goes. Colours are r, g, b (and a);
  // sun and moon are how strongly each shows, stars and clouds likewise;
  // hue tints the swan's ink, which is light or dark by what's behind it;
  // at is
  // where the sun is along its arc (0 rising on the left, 0.5 overhead, 1
  // setting on the right), the moon riding the opposite side of it.
  const scenes = {
    night: {
      top: [4, 7, 22, 0.9], low: [22, 34, 64, 0.72], sunC: [255, 170, 90], moonC: [196, 206, 224], sun: 0, moon: 1,
      stars: 1, clouds: 0, cloud: [180, 190, 214], glint: [214, 224, 244], deep: [3, 6, 14, 0.86], hue: [196, 212, 255], at: -0.1,
    },
    morning: {
      top: [92, 108, 168, 0.9], low: [252, 190, 158, 0.82], sunC: [255, 196, 120], moonC: [214, 214, 230], sun: 1, moon: 0.2,
      stars: 0.15, clouds: 0.75, cloud: [255, 214, 214], glint: [255, 200, 150], deep: [64, 66, 104, 0.82], hue: [132, 72, 120], at: 0.16,
    },
    day: {
      top: [128, 182, 236, 0.9], low: [255, 238, 208, 0.78], sunC: [255, 196, 82], moonC: [230, 234, 240], sun: 1, moon: 0,
      stars: 0, clouds: 1, cloud: [255, 255, 255], glint: [188, 118, 22], deep: [146, 174, 204, 0.8], hue: [40, 74, 128], at: 0.5,
    },
    evening: {
      top: [38, 36, 90, 0.92], low: [246, 138, 78, 0.82], sunC: [255, 138, 64], moonC: [214, 210, 226], sun: 1, moon: 0.3,
      stars: 0.4, clouds: 0.6, cloud: [255, 166, 136], glint: [255, 150, 70], deep: [36, 24, 44, 0.84], hue: [84, 56, 150], at: 0.84,
    },
  };
  const order = ["night", "morning", "day", "evening"];
  // hold is how long a scene stays, blend how long it takes to become the
  // next, in seconds.
  const hold = 75, blend = 25;
  const mixed = (a, b, t) => (Array.isArray(a) ? a.map((v, i) => v + (b[i] - v) * t) : a + (b - a) * t);
  // target is the scene the cycle is at, s seconds after it started.
  function target(s) {
    const i0 = order.indexOf(base);
    if (still) return scenes[base];
    const step = hold + blend;
    const k = Math.floor(s / step), into = s - k * step;
    const ia = (i0 + k) % 4, ib = (ia + 1) % 4;
    const a = scenes[order[ia]], b = scenes[order[ib]];
    const t = into < hold ? 0 : (into - hold) / blend;
    const e = t * t * (3 - 2 * t);
    const out = {};
    for (const key in a) out[key] = mixed(a[key], b[key], e);
    // The sun goes on past its setting into the night, and comes up from
    // below the left of the water: it never runs back across the sky.
    if (ia === 3 && ib === 0) out.at = mixed(a.at, 1.1, e);
    return out;
  }
  const rgb = (v) => v.slice(0, 3).map(Math.round).join(",");
  const rgba = (v, a = v[3]) => `rgba(${rgb(v)},${a.toFixed(3)})`;
  // arc is where a body at at along its arc is on the page, and how far
  // above the water it shows.
  function arc(at) {
    const p = Math.max(0, Math.min(1, at));
    const px = ax + aw * (0.04 + 0.92 * p);
    let py = cy - (cy - Math.max(orb * 2.2, cy * 0.2)) * Math.sin(Math.PI * p);
    if (at < 0) py += -at * cy * 1.4;
    if (at > 1) py += (at - 1) * cy * 1.4;
    return [px, py, Math.max(0, Math.min(1, (cy - py) / (orb * 2.4)))];
  }

  // wave is the water's height at a point along it.
  const wave = (gx, s) => Math.sin(gx * 0.012 + s * 1.1) * 4 + Math.sin(gx * 0.041 - s * 0.7) * 1.4;
  // hash is a steady random number for an integer pair.
  const hash = (a, b) => {
    const v = Math.sin(a * 12.9898 + b * 78.233) * 43758.5453;
    return v - Math.floor(v);
  };

  // A body in the sky: its glow, then its disc shaded glyph by glyph, the
  // moon lit from the side with its seas dark, the sun bright to its edge
  // with turning rays.
  function body(px, py, a, moon, color, s) {
    if (a < 0.02 || py - orb > cy) return;
    const halo = x.createRadialGradient(px, py, orb * 0.6, px, py, orb * (moon ? 3.6 : 7));
    halo.addColorStop(0, `rgba(${rgb(color)},${((moon ? 0.07 : 0.35) * a).toFixed(3)})`);
    halo.addColorStop(1, `rgba(${rgb(color)},0)`);
    x.fillStyle = halo;
    x.fillRect(px - orb * 7, py - orb * 7, orb * 14, orb * 14);
    const rc = Math.ceil(orb / cw), rr = Math.ceil(orb / ch);
    for (let j = -rr; j <= rr; j++) {
      for (let i = -rc; i <= rc; i++) {
        const dx = (i * cw) / orb, dy = (j * ch) / orb;
        const d2 = dx * dx + dy * dy;
        // Below the water line it's hidden: the sun setting goes down into it.
        if (d2 > 1 || py + j * ch > cy) continue;
        const nz = Math.sqrt(1 - d2);
        let v;
        if (moon) {
          v = 0.25 + 0.75 * Math.max(0, -dx * 0.35 - dy * 0.25 + nz * 0.9);
          const sea = Math.sin(dx * 5.1 + 1.3) * Math.cos(dy * 4.3 - 0.7) + Math.sin((dx + dy) * 7.7);
          if (sea > 0.85) v *= 0.62;
          if (hash(i + 99, j + 77) > 0.94) v *= 0.7;
        } else v = 0.7 + 0.3 * nz;
        x.fillStyle = `rgba(${rgb(color)},${(Math.min(1, moon ? 0.16 + v * 0.42 : 0.35 + v * 0.65) * a).toFixed(3)})`;
        x.fillText(ramp[Math.min(ramp.length - 1, 3 + Math.round(v * (ramp.length - 4)))], px + i * cw - cw / 2, py + j * ch - ch / 2);
      }
    }
    if (!moon) {
      for (let k = 0; k < 16; k++) {
        const ang = (k / 16) * Math.PI * 2 + s * 0.05;
        const glyph = "|/-\\"[Math.round((((ang % Math.PI) + Math.PI) % Math.PI) / (Math.PI / 4)) % 4];
        for (let q = 0; q < 4; q++) {
          const rad = orb * (1.35 + q * 0.32 + (k % 2) * 0.25);
          const ry = py + Math.sin(ang) * rad;
          if (ry > cy) continue;
          x.fillStyle = `rgba(${rgb(color)},${((0.55 - q * 0.12) * a).toFixed(3)})`;
          x.fillText(glyph, px + Math.cos(ang) * rad - cw / 2, ry - ch / 2);
        }
      }
    }
  }

  // The sky behind it all, in the same glyphs as everything else: its
  // gradient, the stars and the clouds as strongly as the scene has them,
  // and the sun and the moon where they are on their arcs.
  function sky(s) {
    const g = x.createLinearGradient(0, 0, 0, cy);
    g.addColorStop(0, rgba(sc.top));
    g.addColorStop(1, rgba(sc.low));
    x.fillStyle = g;
    x.fillRect(0, 0, w, cy);
    if (sc.stars > 0.02) {
      // Stars: fixed places, each twinkling in its own time.
      for (let i = 0; i < 190; i++) {
        const sx = hash(i, 3) * w, sy = Math.pow(hash(i, 4), 1.35) * (cy - 30);
        if (Math.hypot(sx - moonX, sy - moonY) < orb * 2) continue;
        const tw = 0.5 + 0.5 * Math.sin(s * (0.6 + hash(i, 5) * 1.8) + i);
        const big = hash(i, 6) > 0.86;
        x.fillStyle = `rgba(238,244,255,${((big ? 0.95 : 0.55) * (0.4 + 0.6 * tw) * sc.stars).toFixed(3)})`;
        if (big) {
          x.font = `${ch + 3}px ui-monospace, Menlo, Consolas, monospace`;
          x.fillText(tw > 0.75 ? "+" : tw > 0.35 ? "*" : "·", sx, sy);
          x.font = `${ch}px ui-monospace, Menlo, Consolas, monospace`;
        } else x.fillText(tw > 0.6 ? "." : "·", sx, sy);
      }
    }
    if (sc.clouds > 0.02) {
      // Clouds: soft clusters of glyphs drifting slowly across.
      for (let k = 0; k < 4; k++) {
        const across = w + 600;
        const cx0 = ((hash(k, 7) * across + s * (6 + k * 2.5)) % across) - 300;
        const cy0 = 40 + hash(k, 8) * (cy * 0.42);
        const cwid = 140 + hash(k, 9) * 160;
        for (let j = -3; j <= 3; j++) {
          for (let i = -Math.ceil(cwid / cw / 2); i <= Math.ceil(cwid / cw / 2); i++) {
            const dx = (i * cw) / (cwid / 2), dy = j / 3.2;
            const lump = 0.62 + 0.38 * Math.sin(i * 0.5 + k) * Math.cos(j * 0.9 + k * 2);
            const d = dx * dx + dy * dy * 2.2;
            if (d > lump) continue;
            const v = 1 - d / lump;
            x.fillStyle = `rgba(${rgb(sc.cloud)},${((0.18 + v * 0.42) * sc.clouds).toFixed(3)})`;
            x.fillText(v > 0.6 ? "@" : v > 0.35 ? "%" : v > 0.15 ? "=" : ":", cx0 + i * cw, cy0 + j * ch);
          }
        }
      }
    }
    body(moonX, moonY, sc.moon * moonUp, true, sc.moonC, s);
    body(sunX, sunY, sc.sun * sunUp, false, sc.sunC, s);
  }

  // The light on the water under whichever of them is up: a column of
  // glints widening toward the viewer, flickering with the ripples.
  function glitter(s, strength) {
    if (strength < 0.03) return;
    for (let r = 0; r <= 10; r++) {
      const ry = cy + r * ch * (0.7 + r * 0.12);
      const half = orb * (0.5 + r * 0.32);
      for (let gx = mx - half; gx < mx + half; gx += cw * 0.85) {
        const f = Math.sin(gx * 0.11 + s * 2.6 + r * 1.9) + Math.sin(gx * 0.047 - s * 1.7 + r) + Math.sin(gx * 0.31 + s * 4.1 - r);
        if (f < 0.1) continue;
        const edge = 1 - Math.abs(gx - mx) / half;
        x.fillStyle = `rgba(${rgb(sc.glint)},${Math.min(1, 0.95 * Math.sqrt(edge) * (1 - r / 14) * Math.min(1, f * 0.7 + 0.15) * strength).toFixed(3)})`;
        x.fillText(f > 1.5 ? "=" : f > 0.8 ? "~" : "-", gx, ry + wave(gx, s) * 0.5 - ch * 0.5);
      }
    }
  }

  function draw(now, pts) {
    const s = now / 1000;
    lean += (aim - lean) * 0.04;
    // Where the day is: the cycle's scene, eased into, so a change of theme
    // glides rather than cuts.
    const tg = target(performance.now() / 1000 - cycle);
    if (!sc) sc = tg;
    else for (const key in tg) sc[key] = mixed(sc[key], tg[key], 0.035);
    // From past its setting to before its rising the sun goes the long way
    // round, under the water: it jumps there, unseen, rather than sweep
    // back across the sky (the moon is in the same place either way).
    if (Math.abs(tg.at - sc.at) > 0.6) sc.at = tg.at;
    // The swan's ink: the scene's hue, light against a dark sky and dark
    // against a bright one, judged by the sky behind it as it shows over
    // the page, and turned quickly, so it's never the sky's own shade for
    // long. The water's strokes are judged by the water the same way.
    const lum = (c) => {
      const v = c.slice(0, 3).map((u, i) => u * c[3] + page[i] * (1 - c[3]));
      return (0.2126 * v[0] + 0.7152 * v[1] + 0.0722 * v[2]) / 255;
    };
    dark += ((lum(mixed(sc.top, sc.low, 0.8)) > 0.42 ? 1 : 0) - dark) * 0.08;
    wdark += ((lum(sc.deep) > 0.42 ? 1 : 0) - wdark) * 0.08;
    pale = mixed(sc.hue, [255, 255, 255], 0.8);
    deep = mixed(sc.hue, [0, 0, 0], 0.62);
    // By day the swan is drawn in tones, its shadows in the deep ink and
    // its sunlit planes nearly white: both stand off a bright sky, so the
    // whole form reads, as it does at night in light ink on the dark.
    lit = mixed(sc.hue, [255, 255, 255], 0.93);
    ink = rgb(mixed(pale, deep, dark));
    wink = rgb(mixed(pale, deep, wdark));
    [sunX, sunY, sunUp] = arc(sc.at);
    [moonX, moonY, moonUp] = arc(sc.at + 0.5 > 1.1 ? sc.at - 0.7 : sc.at + 0.5);
    // The light comes from whichever is up, weighted by how bright it is.
    const ws = sc.sun * sunUp, wm = sc.moon * moonUp * 0.7;
    mx = (sunX * ws + moonX * wm) / (ws + wm || 1);
    my = (sunY * ws + moonY * wm) / (ws + wm || 1);
    // Lit from there, a little toward the viewer: whichever side faces it
    // is bright and the far side falls into shadow.
    const sx = bx + (cols * cw) / 2, sy = cy - cols * cw * 0.2;
    const light = [mx - sx, sy - my, Math.hypot(mx - sx, sy - my) * 0.5];
    const f = frame(pts, cols, rows, -0.55 + Math.sin(s * 0.23) * 0.22 + lean, 0.13 + Math.sin(s * 0.41) * 0.025, cw / ch, light);
    x.clearRect(0, 0, w, h);
    // The card floats on the water line itself: its bottom corners sit on
    // the line where they are, so it rises by the water's height under its
    // middle and tips by the line between its corners.
    if (now) {
      const left = wave(float - span / 2, s) * 0.6, right = wave(float + span / 2, s) * 0.6;
      const lift = (left + right) / 2;
      const tip = (Math.atan2(right - left, span) * 180) / Math.PI;
      card.style.transform = `translateY(${lift.toFixed(2)}px) rotate(${tip.toFixed(3)}deg)`;
      // The privacy notes are its reflection, held to it on one line through
      // both that pivots at the water: they turn by the same angle about the
      // card's bottom edge, so they swing the other way beneath it, and they
      // sink as it rises.
      if (notes) notes.style.transform = `translateY(${(-lift).toFixed(2)}px) rotate(${tip.toFixed(3)}deg)`;
    }
    sky(s);
    // Below the surface the water is darker than the night above it, and
    // deeper toward the bottom of the page, so it reads as a body of water.
    const g = x.createLinearGradient(0, cy - 8, 0, h);
    g.addColorStop(0, rgba(sc.deep, 0));
    g.addColorStop(Math.min(0.2, 22 / Math.max(1, h - cy)), rgba(sc.deep, sc.deep[3] * 0.7));
    g.addColorStop(1, rgba(sc.deep));
    x.fillStyle = g;
    x.fillRect(0, cy - 8, w, h - cy + 8);
    // The water's surface across the whole page, all of it below the
    // water line: rows of ripples moving with the wave, brightest at the
    // line and fading with depth. Drawn first, so the swan and its
    // reflection sit in it.
    for (let r = 0; r <= 9; r++) {
      const gap = ch * (0.7 + r * 0.12);
      const ry = cy + r * gap;
      const fade = 0.32 * (1 - r / 10);
      for (let gx = (r * 13) % 11; gx < w; gx += cw * 1.25) {
        const v = Math.sin(gx * 0.021 + r * 1.7 + s * 0.9) + Math.sin(gx * 0.053 - s * 1.3 + r * 2.3) * 0.7;
        if (v < 0.55) continue;
        x.fillStyle = `rgba(${wink},${(fade * Math.min(1, v - 0.4)).toFixed(3)})`;
        x.fillText(v > 1.25 ? "~" : "-", gx, ry + wave(gx, s) * (0.6 - Math.min(0.4, Math.abs(r) * 0.05)) - ch * 0.5);
      }
    }
    glitter(s, Math.min(1, ws + wm));
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
        // The brighter a cell, the denser its glyph, night or day: what
        // changes with the light is the colour each tone is drawn in.
        else if (v >= 0) lanes[Math.min(bands - 1, Math.floor(v * bands))].push(i, j);
        else if (f.refl[q] > 0.03) glints.push(i, j, f.refl[q]);
      }
    }
    // The swan rides the water where it floats, as the card does: its
    // whole box rises and falls with the line under its middle.
    const ride = wave(bx + (cols * cw) / 2, s) * 0.6;
    const lift = by + f.horizon * ch - cy - ride; // so the bird's water is the card's
    for (let b = 0; b < bands; b++) {
      const k = b / (bands - 1);
      // At night a tone is the light ink, fainter in shadow; by day it's a
      // colour between the deep ink in shadow and near white in the sun.
      const col = mixed(pale, mixed(deep, lit, k), dark);
      const a = 0.3 + 0.7 * k + (0.9 - (0.3 + 0.7 * k)) * dark;
      x.fillStyle = `rgba(${rgb(col)},${a.toFixed(3)})`;
      const g = ramp[1 + Math.round((b / (bands - 1)) * (ramp.length - 2))];
      const lane = lanes[b];
      for (let n = 0; n < lane.length; n += 2) x.fillText(g, bx + lane[n] * cw, by - lift + lane[n + 1] * ch);
    }
    // The rings on the water round the hull, drawn as the rest of the
    // water is: the same strokes, centred on the same line, riding the same
    // wave, fainter outward.
    for (let n = 0; n < ripples.length; n += 3) {
      const i = ripples[n];
      // The rings' far side folds to the near side of the line: the line
      // is the water's edge, and nothing on the water sits above it.
      const j = f.horizon + Math.abs(ripples[n + 1] - f.horizon);
      // Each stroke drifts and flickers on its own, a little.
      const r1 = hash(i, j), r2 = hash(j, i + 31);
      const gx = bx + i * cw + Math.sin(s * (0.7 + r1) + r2 * 6) * cw * 0.6;
      const a = (0.06 + ripples[n + 2] * 0.42) * (0.6 + 0.4 * Math.sin(s * (1.1 + r2 * 1.7) + r1 * 9));
      if (a < 0.04) continue;
      x.fillStyle = `rgba(${wink},${a.toFixed(3)})`;
      // On the water line exactly: the bird's lift (which carries the swan's
      // own ride) taken back out, and the line's wave where the stroke is.
      x.fillText(r1 > 0.72 ? "~" : r1 > 0.12 ? "-" : ".", gx, by - lift - ride + j * ch - ch * 0.5 + wave(gx, s) * 0.6);
    }
    // The outline: every cell at the edge of the bird, bright and solid.
    x.fillStyle = `rgba(${ink},0.95)`;
    for (let n = 0; n < rims.length; n += 3) x.fillText(rims[n + 2] > 0.5 ? "#" : "*", bx + rims[n] * cw, by - lift + rims[n + 1] * ch);
    // The reflection, as moving water breaks it: each row slides on its own
    // and further the deeper it is, the rows fall apart into horizontal
    // streaks with gaps between them that drift, and the brightness comes
    // and goes along each streak.
    for (let n = 0; n < glints.length; n += 3) {
      const i = glints[n], j = glints[n + 1], v = glints[n + 2];
      const deep = j - f.horizon;
      const gx = bx + i * cw;
      const slide = Math.sin(s * (0.9 + hash(j, 2) * 0.8) + hash(j, 5) * 6.3) * Math.min(cw * 3.5, deep * cw * 0.45);
      const rx = gx + slide;
      const band = Math.sin(rx * 0.045 + s * 0.8 + j * 2.1) + Math.sin(rx * 0.12 - s * 1.5 + j * 0.7) * 0.6 + (hash(i, j) - 0.5) * 0.9;
      if (band < -0.3 + deep * 0.03) continue;
      const a = (0.04 + v * 0.42) * Math.min(1, 0.45 + band * 0.5) * (1 - Math.min(0.6, deep * 0.035));
      if (a < 0.03) continue;
      x.fillStyle = `rgba(${wink},${a.toFixed(3)})`;
      const r = hash(j, i * 3);
      x.fillText(band > 0.9 && r > 0.4 ? "=" : r > 0.8 ? "~" : r > 0.1 ? "-" : ".", rx, by - lift - ride + j * ch + wave(gx, s) * 0.6);
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
      x.fillStyle = `rgba(${wink},${(0.1 + 0.42 * near * near).toFixed(3)})`;
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

// The theme toggle: dark or light, whichever isn't showing, kept here for
// next time. The canvases read their colours again when it changes.
(() => {
  const b = document.querySelector("button.theme");
  if (!b) return;
  const root = document.documentElement;
  const dark = () => (root.dataset.theme ? root.dataset.theme === "dark" : !matchMedia("(prefers-color-scheme: light)").matches);
  const show = () => {
    b.textContent = dark() ? "☾" : "☼";
    b.setAttribute("aria-label", dark() ? "switch to light mode" : "switch to dark mode");
  };
  b.hidden = false;
  show();
  b.addEventListener("click", () => {
    root.dataset.theme = dark() ? "light" : "dark";
    try {
      localStorage.setItem("theme", root.dataset.theme);
    } catch {}
    show();
    dispatchEvent(new Event("resize"));
  });
  matchMedia("(prefers-color-scheme: light)").addEventListener("change", show);
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
