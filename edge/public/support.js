// support.cygnet.lol's moving parts. The scene is the swan page's sky
// (app.js) without its water: the day going round from night to morning
// to day to evening, the sun and the moon on their arcs, the stars, the
// clouds, a shooting star now and then, and other birds crossing far off,
// all in glyphs across the whole page. A bare branch runs the width of it,
// and on it sits the merlin of merlin.js, a real merlin measured off
// photographs and drawn as shaded ASCII in her own colours. She breathes,
// blinks, flicks her tail, looks about and, when somebody points near her,
// looks at that. The sheet with the words stands still. Every page works
// without this file, and with reduced motion nothing here moves.

import { bark, branch as perchPoints, frame, model, part, perch, pose, ramp } from "./merlin.js";
import { startCountdown } from "./countdown.js";

for (const timer of document.querySelectorAll("[data-countdown]")) {
  const stop = startCountdown(timer, { onExpire: () => {
    document.querySelector("[data-active-quote]")?.setAttribute("hidden", "");
    document.querySelector("[data-expired-quote]")?.removeAttribute("hidden");
    document.querySelector("a.pay")?.remove();
  } });
  addEventListener("pagehide", stop, { once: true });
}

const still = matchMedia("(prefers-reduced-motion: reduce)").matches;

// hash is a steady random number for an integer pair.
const hash = (a, b) => {
  const v = Math.sin(a * 12.9898 + b * 78.233) * 43758.5453;
  return v - Math.floor(v);
};
const mixed = (a, b, t) => (Array.isArray(a) ? a.map((v, i) => v + (b[i] - v) * t) : a + (b - a) * t);
const rgb = (v) => v.slice(0, 3).map((u) => Math.max(0, Math.min(255, Math.round(u)))).join(",");
const rgba = (v, a = v[3]) => `rgba(${rgb(v)},${a.toFixed(3)})`;
const smooth = (t) => t * t * (3 - 2 * t);

// How she stands to the page: turned three quarters toward it, seen from a
// touch below, which is where both photographs she was measured from have
// the most to say about her.
const YAW = 0.45, PITCH = 0.08;

(() => {
  const c = document.querySelector("canvas.sky");
  const hero = document.querySelector(".hero");
  if (!c || !hero) return;
  const x = c.getContext("2d", { alpha: false });

  // fit sizes the canvas to the window, only when it has to (app.js).
  let w = 0, h = 0, fd = 0;
  function fit() {
    const dpr = Math.min(devicePixelRatio || 1, 2);
    const nw = innerWidth, nh = nw === w && dpr === fd ? Math.max(h, innerHeight) : innerHeight;
    if (nw !== w || nh !== h || dpr !== fd) {
      [w, h, fd] = [nw, nh, dpr];
      c.width = nw * dpr;
      c.height = nh * dpr;
      x.setTransform(dpr, 0, 0, dpr, 0, 0);
    }
  }

  // The scenes, from app.js: colours are r, g, b (and a); sun and moon how
  // strongly each shows, stars and clouds likewise; at is where the sun is
  // along its arc, the moon riding opposite; front is how much she is lit
  // from in front of her rather than from where the sun is (see light).
  const scenes = {
    night: { front: 0, top: [4, 7, 22, 1], low: [22, 34, 64, 1], sunC: [255, 170, 90], moonC: [196, 206, 224], sun: 0, moon: 1, stars: 1, clouds: 0.25, cloud: [120, 130, 160], hue: [196, 212, 255], at: -0.1 },
    morning: { front: 0, top: [92, 108, 168, 1], low: [252, 190, 158, 1], sunC: [255, 196, 120], moonC: [214, 214, 230], sun: 1, moon: 0.2, stars: 0.15, clouds: 0.75, cloud: [255, 214, 214], hue: [132, 72, 120], at: 0.16 },
    day: { front: 1, top: [128, 182, 236, 1], low: [255, 238, 208, 1], sunC: [255, 196, 82], moonC: [230, 234, 240], sun: 1, moon: 0, stars: 0, clouds: 1, cloud: [255, 255, 255], hue: [40, 74, 128], at: 0.5 },
    evening: { front: 0, top: [38, 36, 90, 1], low: [246, 138, 78, 1], sunC: [255, 138, 64], moonC: [214, 210, 226], sun: 1, moon: 0.3, stars: 0.4, clouds: 0.6, cloud: [255, 166, 136], hue: [84, 56, 150], at: 0.84 },
  };
  const order = ["night", "morning", "day", "evening"];
  const hold = 75, blend = 25;
  let base = "", cycle = 0, sc = null;
  function target(s) {
    if (still) return scenes[base];
    const step = hold + blend;
    const k = Math.floor(s / step), into = s - k * step;
    const ia = (order.indexOf(base) + k) % 4, ib = (ia + 1) % 4;
    const a = scenes[order[ia]], b = scenes[order[ib]];
    const e = smooth(into < hold ? 0 : (into - hold) / blend);
    const out = {};
    for (const key in a) out[key] = mixed(a[key], b[key], e);
    if (ia === 3 && ib === 0) out.at = mixed(a.at, 1.1, e);
    return out;
  }

  // She is built once, at rest, with the perch under her; each frame poses
  // a copy. A small screen gets fewer points, which it can't show anyway.
  let rest = null, posed = null;
  function build(density) {
    const bird = model({ density });
    const wood = perchPoints(0.7, density);
    rest = new Float32Array(bird.length + wood.length);
    rest.set(bird);
    rest.set(wood, bird.length);
    posed = new Float32Array(rest.length);
  }

  // The layout: cw and ch a character cell, orb the sun and the moon's
  // size, her box (bx, by, cols, rows) over the hero, its perch row where
  // her y 0 falls.
  let arcLeft = 0, arcWidth = 0, cw = 6, ch = 10, orb = 40, bx = 0, by = 0, cols = 0, rows = 0, perchRow = 0, font = "";
  function place() {
    fit();
    const r = hero.getBoundingClientRect();
    ch = w >= 900 ? 9 : 7;
    cw = ch * 0.6;
    font = `700 ${ch}px "Cygnet Mono", ui-monospace, Menlo, Consolas, monospace`;
    // Her box is a little wider than tall: on a phone the column's width
    // is what bounds it.
    const bh = Math.max(160, Math.min(r.height, w >= 900 ? 640 : 380, r.width / 1.05));
    const bw = bh * 1.05;
    rows = Math.floor(bh / ch);
    cols = Math.floor(bw / cw);
    bx = Math.round(r.left + r.width / 2 - bw * 0.46);
    by = Math.round(r.top + (r.height - bh) * 0.4);
    perchRow = Math.round(rows * 0.84);
    orb = Math.max(26, Math.min(64, w * 0.028));
    [arcLeft, arcWidth] = w >= 1040 ? [r.left, r.width] : [0, w];
    const density = w < 600 ? 0.55 : w < 900 ? 0.75 : 1;
    if (!rest || rest.density !== density) {
      build(density);
      rest.density = density;
    }
    const was = base;
    const root = document.documentElement;
    const light = root.dataset.theme ? root.dataset.theme === "light" : matchMedia("(prefers-color-scheme: light)").matches;
    base = light ? "day" : "night";
    if (base !== was) {
      cycle = performance.now() / 1000;
      sc = null;
    }
  }

  // arc is where a body at at along its arc is on the page. On a wide
  // screen it crosses her side of the page only, as the swan page keeps
  // its sun and moon over the swan, so neither ever sets behind the sheet
  // and its title.
  function arc(at) {
    const p = Math.max(0, Math.min(1, at));
    const ax = arcLeft + arcWidth * (0.04 + 0.92 * p);
    let ay = h * 0.86 - h * 0.74 * Math.sin(Math.PI * p);
    if (at < 0) ay += -at * h * 1.4;
    if (at > 1) ay += (at - 1) * h * 1.4;
    return [ax, ay, Math.max(0, Math.min(1, (h - ay) / (orb * 2.4)))];
  }

  // A body in the sky, from app.js: its glow, its disc shaded glyph by
  // glyph, the moon lit from the side with its seas, the sun with rays.
  function body(px, py, a, moon, color, s) {
    if (a < 0.02) return;
    const halo = x.createRadialGradient(px, py, orb * 0.6, px, py, orb * (moon ? 3.6 : 7));
    halo.addColorStop(0, `rgba(${rgb(color)},${((moon ? 0.07 : 0.3) * a).toFixed(3)})`);
    halo.addColorStop(1, `rgba(${rgb(color)},0)`);
    x.fillStyle = halo;
    x.fillRect(px - orb * 7, py - orb * 7, orb * 14, orb * 14);
    const gw = 7.2, gh = 12;
    const rc = Math.ceil(orb / gw), rr = Math.ceil(orb / gh);
    x.font = `${gh}px ui-monospace, Menlo, Consolas, monospace`;
    for (let j = -rr; j <= rr; j++) {
      for (let i = -rc; i <= rc; i++) {
        const dx = (i * gw) / orb, dy = (j * gh) / orb;
        const d2 = dx * dx + dy * dy;
        if (d2 > 1) continue;
        const nz = Math.sqrt(1 - d2);
        let v;
        if (moon) {
          v = 0.25 + 0.75 * Math.max(0, -dx * 0.35 - dy * 0.25 + nz * 0.9);
          const sea = Math.sin(dx * 5.1 + 1.3) * Math.cos(dy * 4.3 - 0.7) + Math.sin((dx + dy) * 7.7);
          if (sea > 0.85) v *= 0.62;
        } else v = 0.7 + 0.3 * nz;
        x.fillStyle = `rgba(${rgb(color)},${(Math.min(1, moon ? 0.16 + v * 0.42 : 0.35 + v * 0.65) * a).toFixed(3)})`;
        x.fillText(ramp[Math.min(ramp.length - 1, 3 + Math.round(v * (ramp.length - 4)))], px + i * gw - gw / 2, py + j * gh - gh / 2);
      }
    }
    if (!moon) {
      for (let k = 0; k < 16; k++) {
        const ang = (k / 16) * Math.PI * 2 + s * 0.05;
        const glyph = "|/-\\"[Math.round((((ang % Math.PI) + Math.PI) % Math.PI) / (Math.PI / 4)) % 4];
        for (let q = 0; q < 4; q++) {
          const rad = orb * (1.35 + q * 0.32 + (k % 2) * 0.25);
          x.fillStyle = `rgba(${rgb(color)},${((0.5 - q * 0.11) * a).toFixed(3)})`;
          x.fillText(glyph, px + Math.cos(ang) * rad - gw / 2, py + Math.sin(ang) * rad - gh / 2);
        }
      }
    }
  }

  let sunX = 0, sunY = 0, moonX = 0, moonY = 0, sunUp = 0, moonUp = 0;

  // The sky over the whole page: its gradient, the stars and the clouds as
  // strongly as the scene has them, a shooting star at night, other birds
  // crossing by day, the sun and the moon.
  function sky(s) {
    const g = x.createLinearGradient(0, 0, 0, h);
    g.addColorStop(0, rgba(sc.top));
    g.addColorStop(1, rgba(sc.low));
    x.fillStyle = g;
    x.fillRect(0, 0, w, h);
    x.font = "12px ui-monospace, Menlo, Consolas, monospace";
    x.textBaseline = "top";
    if (sc.stars > 0.02) {
      for (let i = 0; i < 240; i++) {
        const sx = hash(i, 3) * w, sy = Math.pow(hash(i, 4), 1.2) * h * 0.9;
        if (Math.hypot(sx - moonX, sy - moonY) < orb * 2) continue;
        const tw = 0.5 + 0.5 * Math.sin(s * (0.6 + hash(i, 5) * 1.8) + i);
        const big = hash(i, 6) > 0.86;
        x.fillStyle = `rgba(238,244,255,${((big ? 0.95 : 0.55) * (0.4 + 0.6 * tw) * sc.stars).toFixed(3)})`;
        x.fillText(big ? (tw > 0.75 ? "+" : tw > 0.35 ? "*" : "·") : tw > 0.6 ? "." : "·", sx, sy);
      }
      // A shooting star: one every twenty seconds or so, a second long.
      const k = Math.floor(s / 19), into = s - k * 19;
      if (!still && into < 1.1 && hash(k, 9) > 0.35) {
        const x0 = hash(k, 10) * w * 0.8 + w * 0.1, y0 = hash(k, 11) * h * 0.35;
        const len = 18;
        for (let q = 0; q < len; q++) {
          const t = into / 1.1 - q * 0.012;
          if (t < 0) break;
          x.fillStyle = `rgba(255,250,235,${((1 - q / len) * (1 - into / 1.1) * sc.stars).toFixed(3)})`;
          x.fillText(q === 0 ? "*" : "-", x0 + t * w * 0.3, y0 + t * h * 0.12);
        }
      }
    }
    if (sc.clouds > 0.02) {
      for (let k = 0; k < 5; k++) {
        const across = w + 600;
        const cx0 = ((hash(k, 7) * across + s * (6 + k * 2.5)) % across) - 300;
        const cy0 = 30 + hash(k, 8) * h * 0.55;
        const cwid = 140 + hash(k, 9) * 200;
        for (let j = -3; j <= 3; j++) {
          for (let i = -Math.ceil(cwid / 14); i <= Math.ceil(cwid / 14); i++) {
            const dx = (i * 7) / (cwid / 2), dy = j / 3.2;
            const lump = 0.62 + 0.38 * Math.sin(i * 0.5 + k) * Math.cos(j * 0.9 + k * 2);
            const d = dx * dx + dy * dy * 2.2;
            if (d > lump) continue;
            const v = 1 - d / lump;
            x.fillStyle = `rgba(${rgb(sc.cloud)},${((0.16 + v * 0.4) * sc.clouds).toFixed(3)})`;
            x.fillText(v > 0.6 ? "@" : v > 0.35 ? "%" : v > 0.15 ? "=" : ":", cx0 + i * 7, cy0 + j * 11);
          }
        }
      }
    }
    // Far off, now and then, a few other birds cross, flapping.
    const day = 1 - sc.stars;
    if (!still && day > 0.3) {
      const k = Math.floor(s / 37), into = (s - k * 37) / 37;
      if (hash(k, 12) > 0.4) {
        const fy = h * (0.12 + hash(k, 13) * 0.25), dir = hash(k, 14) > 0.5 ? 1 : -1;
        for (let b = 0; b < 3 + Math.floor(hash(k, 15) * 4); b++) {
          const bxx = dir > 0 ? -60 + into * (w + 120) - b * 22 : w + 60 - into * (w + 120) + b * 22;
          const flap = Math.sin(s * 9 + b * 1.7) > 0;
          x.fillStyle = `rgba(40,36,48,${(0.55 * day).toFixed(3)})`;
          x.fillText(flap ? "v" : "-", bxx, fy + b * 9 + Math.sin(s * 2 + b) * 3);
        }
      }
    }
    body(moonX, moonY, sc.moon * moonUp, true, sc.moonC, s);
    body(sunX, sunY, sc.sun * sunUp, false, sc.sunC, s);
  }

  // light is where the light comes from in the view (x right, y up, z out
  // of the page): whichever of the sun and the moon is up, from where it
  // stands relative to her.
  function light() {
    const ws = sc.sun * sunUp, wm = sc.moon * moonUp * 0.7;
    const mx = (sunX * ws + moonX * wm) / (ws + wm || 1);
    const my = (sunY * ws + moonY * wm) / (ws + wm || 1);
    const sx = bx + (cols * cw) / 2, sy = by + rows * ch * 0.4;
    lighting(ws, wm);
    // From where the sun or the moon is, raking in from the side, as a
    // painter would set it, so she has a shadow side to model her; softer
    // and more frontal at night. At midday that puts the sun high behind
    // her and everything you see of her in shade, so by day the light is
    // set where a painter would put it instead, in front of her and to
    // the left, raking, and the sun itself catches her outline (rimAt).
    const at = [mx - sx, sy - my, Math.hypot(mx - sx, sy - my) * (0.35 + 0.3 * sc.stars) + 1];
    const l = Math.hypot(...at) || 1;
    return mixed(at.map((u) => u / l), [-0.42, 0.5, 0.76], sc.front);
  }

  // The scene's light, as the swan page paints with it (app.js, paint and
  // hue), worked out once a frame by lighting(): key is the light from
  // whichever of the sun and the moon is up, warmed toward white; fill is
  // the sky's own light in the shadows, cool and tinted by the scene's
  // hue; rim is what an edge facing the light catches.
  let key = [1, 1, 1], fill = [1, 1, 1], rim = [255, 255, 255];
  function lighting(ws, wm) {
    const sun = mixed(sc.sunC, [255, 255, 255], 0.42), moon = mixed(sc.moonC, [200, 214, 255], 0.3);
    const k = mixed(sun, moon, wm / (ws + wm || 1));
    key = k.map((u) => (u / 255) * (1.3 - 0.25 * sc.stars));
    // By day the shadows take the warm light off the ground and the low
    // sky as well as the blue above, or a midday bird goes slate.
    fill = mixed(mixed(mixed(sc.top, sc.hue, 0.45).slice(0, 3), [255, 255, 255], 0.3), sc.low.slice(0, 3), 0.4 * sc.front).map((u) => (u / 255) * (0.62 + 0.25 * sc.stars));
    rim = mixed(k, [255, 255, 255], 0.35);
  }
  // paint is a surface's colour in that light, as a painter sets a bird
  // in low sun: two families with a clean edge between them, the lit one
  // her own colour in the warm key and raised, the shadow one her colour
  // in the cool fill and clearly lower, darkest just past the edge where
  // the form turns away; a lit outline catches the light; and all of it a
  // little toward the sky, so she sits in its air. Her colours are the
  // photographs', and the light only tints them: the painted colour is
  // mixed back with her own simply lit, or a sunset repaints her into a
  // bird she isn't. Her face keeps some sky light even with the sun
  // behind her, as a painter keeps a face readable.
  const paint = (c, v, edge = false, face = false) => {
    // Her streaks and bars drawn a little firmer than the photographs'
    // averaged cells hold them, about their middle.
    c = c.map((u) => Math.max(0, Math.min(255, 118 + (u - 118) * 1.22)));
    const lightness = Math.min(1, Math.max(0, (v - 0.38) / 0.12));
    const inLight = 0.85 + 0.45 * Math.max(0, (v - 0.45) / 0.55);
    // By day the shadow side sits clearly lower, so the form models.
    const inShade = (face ? 0.78 : 0.66) - 0.05 * sc.front - 0.14 * Math.max(0, 1 - Math.abs(v - 0.34) / 0.07);
    let col = c.map((u, n) => u * (fill[n] * inShade + (key[n] * inLight - fill[n] * inShade) * lightness));
    col = mixed(col, c.map((u) => u * (0.55 + 0.65 * Math.max(v, face ? 0.45 : 0))), 0.4);
    if (edge && lightness > 0.5) col = mixed(col, rim, 0.4);
    return mixed(col, sc.low, 0.08);
  };
  // cell draws one point of the cloud as the swan's are drawn: a dense
  // glyph, its tone by how light the colour is, straight on the sky, the
  // glyphs' own spacing the even gap between every point.
  const cell = (i, j, col, glyph) => {
    x.fillStyle = `rgba(${rgb(col)},0.95)`;
    x.fillText(glyph ?? toneGlyph(col), bx + i * cw, by + j * ch);
  };
  const toneGlyph = (c) => {
    const l = 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
    return l > 190 ? "@" : l > 110 ? "%" : "#";
  };

  // The branch across the page: the same perch as under her, its axis
  // carried on both ways in the direction her turn and the slight look up
  // give it. Under her and for a little way either side it is exactly the
  // perch, the same line and the same thickness, so the two meet without
  // a seam; further out it wavers a little and thickens toward the left,
  // where it comes from, and a few twigs leave it. Each cell is shaded as
  // a round branch in that light, its bark furrowed along its length.
  function wood(f, s, L) {
    const k = f.scale, unit = k * cw;
    const cyw = Math.cos(YAW), syw = Math.sin(YAW), sp = Math.sin(PITCH), cp = Math.cos(PITCH);
    // Where the perch's axis crosses her middle, and its direction.
    const z1 = -perch.x * syw;
    const ax0 = bx + (f.left + perch.x * cyw * k) * cw;
    const ay0 = by + (perchRow - (perch.y * cp - z1 * sp) * k * (cw / ch)) * ch;
    const dl = Math.hypot(syw, cyw * sp);
    const ux = syw / dl, uy = (cyw * sp) / dl;
    const R = perch.r * unit;
    const ll = Math.hypot(...L);
    const lx = L[0] / ll, ly = L[1] / ll, lz = L[2] / ll;
    // free is how far a point t along the branch may stray from the
    // perch: nothing near her, all of it two perch-widths on.
    const free = (t) => smooth(Math.min(1, Math.max(0, (Math.abs(t) - unit * 0.8) / (unit * 1.5))));
    // A limb: cells near the line from (ox, oy) along (dx, dy), from to
    // to px, radius rAt(t) and its middle bent off the line by bendAt(t).
    const limb = (ox, oy, dx, dy, from, to, rAt, bendAt) => {
      const nx = -dy, ny = dx;
      const reach = Math.max(rAt(from), rAt(to), rAt((from + to) / 2)) * 1.5 + ch;
      const xs = [ox + dx * from, ox + dx * to], ys = [oy + dy * from, oy + dy * to];
      const i0 = Math.floor((Math.min(...xs) - reach - bx) / cw), i1 = Math.ceil((Math.max(...xs) + reach - bx) / cw);
      const j0 = Math.floor((Math.min(...ys) - reach - by) / ch), j1 = Math.ceil((Math.max(...ys) + reach - by) / ch);
      for (let j = j0; j <= j1; j++) {
        const py = by + j * ch + ch / 2;
        if (py < -ch || py > h + ch) continue;
        for (let i = i0; i <= i1; i++) {
          const px = bx + i * cw + cw / 2;
          if (px < -cw || px > w + cw) continue;
          // Leave her box's own cells to her and the perch under her.
          if (i >= 0 && i < cols && j >= 0 && j < rows && f.shade[j * cols + i] >= 0) continue;
          const t = (px - ox) * dx + (py - oy) * dy;
          if (t < from || t > to) continue;
          const r = rAt(t);
          const d = (px - ox) * nx + (py - oy) * ny - bendAt(t);
          if (Math.abs(d) > r) continue;
          const nd = d / r, nz = Math.sqrt(1 - nd * nd);
          // The surface normal in the view: across the limb (down the page
          // is -y in the view) and out of the page.
          const v = Math.min(1, 0.22 + 0.78 * Math.max(0, nx * nd * lx - ny * nd * ly + nz * lz) ** 0.85);
          const furrow = 0.82 + 0.36 * hash(Math.round(t / (cw * 3)) + Math.round(ox), Math.round(d / (ch * 0.7)));
          cell(i, j, paint(bark.map((u) => u * furrow), v));
        }
      }
    };
    // The branch, from beyond the left edge to beyond the right.
    const left = (-ax0 - 40) / ux, right = (w - ax0 + 40) / ux;
    const span = Math.max(1, right - left) / 2;
    const thick = (t) => R * (1 - 0.32 * free(t) * Math.tanh(t / span * 1.6));
    limb(ax0, ay0, ux, uy, left, right, thick, (t) => free(t) * Math.sin(t * 0.006) * R * 0.35);
    // Twigs: fixed places along it, leaving it up and out and tapering to
    // nothing, each in two lengths that kink where they meet, the outer
    // one swaying a little, and a side shoot off some.
    for (let q = 0; q < 7; q++) {
      const t = left + (right - left) * (0.06 + 0.88 * hash(q, 21));
      if (Math.abs(t) < unit * 0.6) continue;
      const len = unit * (0.45 + 0.8 * hash(q, 22));
      const side = hash(q, 23) > 0.5 ? 1 : -1;
      const sway = still ? 0 : Math.sin(s * 0.7 + q * 1.3) * 0.05;
      const r = thick(t);
      const tilt = 0.5 + 0.55 * hash(q, 24);
      const d1 = [side * Math.sin(tilt), -Math.cos(tilt)];
      const o1 = [ax0 + ux * t, ay0 + uy * t - r * 0.5];
      const taper = (r0, r1, l) => (t) => r0 + (r1 - r0) * Math.min(1, Math.max(0, t / l));
      const wobble = (seed) => (t) => Math.sin(t * 0.05 + seed) * r * 0.06;
      limb(o1[0], o1[1], d1[0], d1[1], 0, len * 0.55, taper(r * 0.26, r * 0.13, len * 0.55), wobble(q));
      const kink = tilt + side * (0.25 - 0.5 * hash(q, 25)) + sway;
      const d2 = [side * Math.sin(kink), -Math.cos(kink)];
      const o2 = [o1[0] + d1[0] * len * 0.55, o1[1] + d1[1] * len * 0.55];
      limb(o2[0], o2[1], d2[0], d2[1], 0, len * 0.6, taper(r * 0.13, r * 0.02, len * 0.6), wobble(q + 11));
      if (hash(q, 26) > 0.4) {
        const shoot = tilt - side * (0.7 + 0.3 * hash(q, 27)) + sway;
        limb(o2[0], o2[1], side * Math.sin(shoot), -Math.cos(shoot), 0, len * 0.35, taper(r * 0.09, r * 0.015, len * 0.35), wobble(q + 21));
      }
    }
  }

  // Her life, from the time and from where somebody points: she breathes
  // all the time, blinks every few seconds, flicks her tail now and then,
  // and turns her head in quick moves with holds between, as falcons do,
  // toward a pointer that has moved near her lately.
  let pointer = null;
  addEventListener("pointermove", (e) => (pointer = { x: e.clientX, y: e.clientY, at: performance.now() }), { passive: true });
  let look = 0.15, lookFrom = 0.15, lookTo = 0.15, lookAt = 0;
  function life(s) {
    if (still) return { look: 0.15 };
    // Where to look next: the pointer if it moved in the last four seconds,
    // else somewhere new every few seconds.
    let want = lookTo;
    if (pointer && performance.now() - pointer.at < 4000) {
      const hx = bx + cols * cw * 0.32;
      want = Math.max(-0.35, Math.min(0.6, -0.1 + ((pointer.x - hx) / Math.max(200, w * 0.35)) * 0.7));
    } else if (s - lookAt > 2.2 + hash(Math.floor(s), 41) * 3) want = -0.3 + hash(Math.floor(s * 3), 42) * 0.85;
    if (Math.abs(want - lookTo) > 0.12) [lookFrom, lookTo, lookAt] = [look, want, s];
    look = lookFrom + (lookTo - lookFrom) * smooth(Math.min(1, (s - lookAt) / 0.28));
    // A tail flick: down quickly and back, every so often.
    const fk = Math.floor(s / 6.3), fi = s - fk * 6.3;
    const pump = hash(fk, 43) > 0.45 && fi < 0.5 ? 0.14 * Math.sin((fi / 0.5) * Math.PI) : 0;
    const b = s % 5.3;
    return { look, pump, breathe: (Math.sin(s * 1.6) + 1) / 2, blink: b < 0.18 ? Math.sin((b / 0.18) * Math.PI) : 0 };
  }

  function draw(now) {
    const s = now / 1000;
    const tg = target(performance.now() / 1000 - cycle);
    if (!sc) sc = structuredClone(tg);
    else for (const key in tg) sc[key] = mixed(sc[key], tg[key], 0.035);
    if (Math.abs(tg.at - sc.at) > 0.6) sc.at = tg.at;
    [sunX, sunY, sunUp] = arc(sc.at);
    [moonX, moonY, moonUp] = arc(sc.at + 0.5 > 1.1 ? sc.at - 0.7 : sc.at + 0.5);
    sky(s);
    const L = light();
    const f = frame(pose(rest, life(s), posed), cols, rows, YAW, PITCH, cw / ch, L, perchRow);
    const sunSide = Math.sign(sunX - (bx + cols * cw * 0.42)) || 1;
    const rimAt = sc.front * sc.sun * sunUp;
    x.font = font;
    x.textBaseline = "top";
    wood(f, s, L);
    for (let j = 0; j < rows; j++) {
      for (let i = 0; i < cols; i++) {
        const q = j * cols + i, v = f.shade[q];
        if (v < 0) continue;
        const p = f.parts[q];
        // The catchlight is a pinpoint: one cell, the first of it in
        // reading order; any cell beside it that also caught it is pupil,
        // or two bright cells side by side read as googly eyes.
        const glint = p === part.glint && f.parts[q - 1] !== part.glint && f.parts[q - cols] !== part.glint && f.parts[q - cols - 1] !== part.glint && f.parts[q - cols + 1] !== part.glint;
        if (p === part.eye || (p === part.glint && !glint)) cell(i, j, [12, 10, 10], "@");
        else if (glint) cell(i, j, [255, 252, 240], "*");
        else {
          const c = [f.colours[q * 3], f.colours[q * 3 + 1], f.colours[q * 3 + 2]];
          // The ring round her eye keeps its own olive-yellow, half lit.
          if (p === part.ring) cell(i, j, mixed(paint(c, 0.7, false, true), c, 0.5), "#");
          else {
            let col = paint(c, v, f.edge[q] === 1 && p !== part.branch, p === part.head);
            // By day the sun, high on one side, rims her outline there.
            if (f.edge[q] && p !== part.branch && (i - cols * 0.42) * sunSide > 0) col = mixed(col, rim, 0.55 * rimAt);
            cell(i, j, col);
          }
        }
      }
    }
  }

  place();
  addEventListener("resize", place);
  addEventListener("scroll", place, { passive: true });
  matchMedia("(prefers-color-scheme: light)").addEventListener("change", place);
  new MutationObserver(place).observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  let last = 0;
  const loop = (now) => {
    if (!document.hidden && now - last > 33) {
      last = now;
      draw(still ? 0 : now);
      document.body.classList.add("live");
    }
    if (!still) requestAnimationFrame(loop);
  };
  document.fonts.ready.then(() => requestAnimationFrame(loop));
})();

// The theme toggle: dark or light, whichever isn't showing, kept for next
// time. The scene follows it.
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
  });
  matchMedia("(prefers-color-scheme: light)").addEventListener("change", show);
})();

// Currency first, then its supported networks. Before script runs every
// group is visible and every payment choice is an ordinary radio.
(() => {
  const form = document.querySelector("form.checkout");
  if (!form) return;
  const picker = form.querySelector(".currencies");
  const groups = [...form.querySelectorAll(".asset-networks")];
  if (!picker || !groups.length) return;
  const previous = new Map();
  for (const group of groups) {
    const label = document.createElement("label");
    label.className = "opt";
    const input = document.createElement("input");
    input.type = "radio";
    input.name = "asset";
    input.value = group.dataset.asset;
    input.setAttribute("aria-label", group.dataset.asset);
    input.checked = !!group.querySelector("input:checked");
    const name = document.createElement("span");
    name.className = "name";
    for (const child of group.querySelector("legend").childNodes) name.append(child.cloneNode(true));
    label.append(input, name);
    picker.append(label);
  }
  const show = () => {
    const asset = picker.querySelector("input:checked")?.value;
    for (const group of groups) {
      const selected = group.querySelector('input[name="rail"]:checked');
      if (selected) previous.set(group.dataset.asset, selected.value);
      group.hidden = group.disabled = group.dataset.asset !== asset;
    }
    const active = groups.find((g) => !g.disabled);
    if (!active) return;
    const inputs = [...active.querySelectorAll('input[name="rail"]')];
    const chosen = inputs.find((input) => input.value === previous.get(asset)) ?? inputs[0];
    if (chosen) chosen.checked = true;
  };
  form.addEventListener("change", show);
  show();
  picker.hidden = false;
})();

// The checkout's choices, summed up as they change: what you picked, in
// the page's own words (each option's label), and the total on the
// button, so nobody reaches the next page surprised by the amount.
(() => {
  const form = document.querySelector("form.checkout[data-prices]");
  if (!form) return;
  const prices = JSON.parse(form.dataset.prices);
  const out = form.querySelector(".summary");
  const go = form.querySelector('button[type="submit"]');
  const dollars = (c) => (c % 100 === 0 ? `$${c / 100}` : `$${(c / 100).toFixed(2)}`);
  const picked = (name) => form.querySelector(`input[name="${name}"]:checked`);
  // replay restarts an element's one-off animation (support.css, motion).
  const replay = (el) => {
    el.classList.remove("fresh");
    void el.offsetWidth;
    el.classList.add("fresh");
  };
  let was = null;
  const show = () => {
    const months = picked("months"), cents = picked("cents"), rail = picked("rail");
    const price = prices[months?.value]?.[cents?.value];
    if (price == null || !rail) return;
    const long = months.closest(".opt").querySelector(".name").textContent;
    out.textContent = `${long} at ${cents.getAttribute("aria-label")}, paid in ${rail.getAttribute("aria-label")}. `;
    out.append(Object.assign(document.createElement("b"), { textContent: `${dollars(price)} in all.` }));
    out.hidden = false;
    go.textContent = `go to checkout, ${dollars(price)}`;
    if (was !== null) replay(out);
    if (was !== null && was !== price) replay(go);
    was = price;
  };
  form.addEventListener("change", show);
  show();
})();

// Copy an address or an amount exactly as it is.
for (const b of document.querySelectorAll("button[data-copy]")) {
  b.hidden = false;
  b.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(b.dataset.copy);
      b.textContent = "copied";
      b.classList.add("done");
    } catch {
      b.textContent = "select it";
    }
  });
}

// A checkout checks quietly whether it's paid: 204 while it waits, 200 once
// paid, 410 once it can't be; the last two redraw the page, which says
// which.
const check = document.body.dataset.wait;
if (check) {
  const tick = async () => {
    try {
      const r = await fetch(check, { cache: "no-store" });
      if (r.status === 200 || r.status === 410) return location.reload();
    } catch {}
    setTimeout(tick, 5000);
  };
  setTimeout(tick, 5000);
}
