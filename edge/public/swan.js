// The swan: a 3D model drawn in ASCII, the way donut.c draws a torus. Its
// surfaces are sampled densely into points with normals; each frame they're
// turned, projected, the nearest point kept for each character cell, and
// shaded by how much it faces the light. t grows it from a cygnet (0) to a
// swan (1). The Worker draws one still frame of it into the page and
// app.js brings it to life, from this one file.
//
// The proportions are a mute swan's, measured off a side photograph with
// the body as 2.1 units from chest to tail and the water at y 0: a tall
// half egg of a body, low at the chest and highest toward the rear, wings
// folded over it with their long feathers raised at the back and a short
// tail turned up; a thick, even neck rising almost straight from the front
// of the chest at the water and arching forward at the top into the crown;
// the head tipped steeply down, the beak a long wedge with a knob at its
// base. Facing left while it's built, then turned to face right.

const TAU = Math.PI * 2;
const lerp = (a, b, t) => a + (b - a) * t;
const smooth = (a, b, x) => {
  const k = Math.min(1, Math.max(0, (x - a) / (b - a)));
  return k * k * (3 - 2 * k);
};

// ramp is the shading, dark to light.
export const ramp = " .,:-=+*#%@";

// profile is a smooth curve through [x, y] knots, read at x.
function profile(knots, x) {
  if (x <= knots[0][0]) return knots[0][1];
  for (let i = 1; i < knots.length; i++) {
    const [x1, y1] = knots[i];
    const [x0, y0] = knots[i - 1];
    if (x <= x1) {
      const k = (x - x0) / (x1 - x0);
      return lerp(y0, y1, k * k * (3 - 2 * k));
    }
  }
  return knots[knots.length - 1][1];
}

// shape is the swan's measurements at t, its neck nodding by pose (0 head
// up, 1 head lower) and swaying by sway.
function shape(t, pose, sway) {
  const L = (a, b) => lerp(a, b, t);
  return {
    scale: L(0.6, 1),
    len: L(0.86, 1.05),
    // How high the back stands above the water along the body, and how
    // wide it is, from the chest (-1) to the tail (1).
    // The back rises from the breast to its height two thirds of the way
    // back, then slopes down to a narrow stern, where the tail comes out.
    height: [[-1, L(0.2, 0.16)], [-0.75, L(0.42, 0.38)], [-0.35, L(0.58, 0.64)], [0.1, L(0.64, 0.78)], [0.5, L(0.6, 0.86)], [0.74, L(0.5, 0.8)], [0.9, L(0.38, 0.6)], [1, L(0.24, 0.34)]],
    width: [[-1, 0.1], [-0.75, 0.36], [-0.3, 0.52], [0.3, 0.54], [0.75, 0.42], [0.92, 0.22], [1, 0.08]],
    neck: [
      [L(-0.82, -0.93), 0.06, 0],
      [L(-0.86, -0.86), L(0.42, 0.92), sway * 0.25],
      [L(-0.72, -0.6 + pose * 0.04), L(0.86, 1.86 - pose * 0.08), sway * 0.75],
      [L(-0.96, -0.98 - pose * 0.04), L(0.84, 1.72 - pose * 0.1), sway],
    ],
    neckR: [L(0.17, 0.17), L(0.13, 0.125), L(0.13, 0.11)],
    head: [L(0.16, 0.19), L(0.12, 0.12), L(0.11, 0.105)],
    // How far down the head tips from the neck's line, in radians.
    tip: L(0.35, 0.68) + pose * 0.1,
    beak: L(0.16, 0.34),
    plumes: L(0.04, 0.2),
    tail: L(0.06, 0.2),
  };
}

function bez(p, s) {
  const k = 1 - s;
  const a = k * k * k, b = 3 * k * k * s, c = 3 * k * s * s, d = s * s * s;
  return [0, 1, 2].map((i) => a * p[0][i] + b * p[1][i] + c * p[2][i] + d * p[3][i]);
}

// model is the swan at t as points: x y z, then the normal, then what it
// is (0 plumage, 1 eye, 2 beak, 3 the edge of a feather, 4 to 6 the water's
// rings, nearest first), seven
// numbers a point.
export function model(t, density = 1, pose = 0, sway = 0) {
  const m = shape(t, pose, sway);
  const out = [];
  // Samples sit off the grid by a fixed random amount, so the surface reads
  // as a surface and not as rows of characters.
  let seed = 7;
  const jit = () => ((seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff) * 0.9 + 0.05;
  const add = (x, y, z, nx, ny, nz, kind) => {
    const l = Math.hypot(nx, ny, nz) || 1;
    out.push(x * m.scale, y * m.scale, z * m.scale, nx / l, ny / l, nz / l, kind);
  };
  const n = (a) => Math.max(4, Math.round(a * density));

  // The body: each slice across it a hull standing in the water, as high
  // and as wide as the profiles say there, fullest at the waterline. It
  // rides a little nose down, deeper at the chest and higher at the stern,
  // and the water cuts it where it meets y 0.
  const trim = (u) => -0.07 * Math.max(0, -u) + 0.04 * Math.max(0, u);
  const surface = (u, th) => {
    const hh = profile(m.height, u), ww = profile(m.width, u);
    return [u * m.len, hh * Math.pow(Math.sin(th), 0.85) + trim(u), ww * Math.cos(th), hh, ww];
  };
  const bu = n(120), bv = n(120);
  for (let j = 0; j < bv; j++) {
    const u = -1 + 2 * ((j + jit()) / bv);
    const dh = (profile(m.height, u + 0.01) - profile(m.height, u - 0.01)) / 0.02;
    for (let i = 0; i < bu; i++) {
      const th = ((i + jit()) / bu) * Math.PI;
      const [x, y, z, hh, ww] = surface(u, th);
      if (y < -0.005) continue;
      add(x, y, z, -dh * Math.sin(th) * 0.4, Math.sin(th) / hh, Math.cos(th) / ww, 0);
    }
  }

  // The folded wings: a panel a side lying over the body like a roof, the
  // two meeting in a ridge along the back. Each runs from the shoulder to
  // past the tail, its lower edge a clean line along the flank. Toward the
  // rear the long inner feathers (the tertials) lift off the back in a
  // soft hump and end in layered, scalloped tips that overhang the tail.
  // The feathers show as overlapping rows of scales.
  // wing is a point on a side's panel, at u along the body and v from the
  // ridge (0) down to the flank (1).
  const wing = (side, u, v) => {
    const back = Math.min(1, u);
    const edge = lerp(0.62, 0.9, Math.max(0, (u - 0.6) / 0.5)); // the flank line rises toward the rear
    const th = side > 0 ? lerp(Math.PI / 2 - 0.03, edge, v) : lerp(Math.PI / 2 + 0.03, Math.PI - edge, v);
    const [, y0, z0] = surface(back, th);
    // The long inner feathers lift at the rear in a few overlapping lobes,
    // highest along the ridge.
    const lobes = 0.8 + 0.2 * Math.cos(v * Math.PI * 3.2 + u * 4);
    const rise = m.plumes * smooth(0.42, 1.04, u) * (1 - 0.55 * v) * lobes + 0.025 * (1 - v);
    // The feathers lie in rows, each overlapping the one behind it: a
    // sawtooth of relief along the wing, so every row catches the light at
    // its edge and shades the one under it. Along the lower wing the long
    // flight feathers run back in shallow grooves.
    const across = u * 10 + 0.35 * Math.sin(v * Math.PI * 2);
    // Swan plumage is smooth: each row only a soft swell, not a step.
    const rows = 0.006 * Math.sin(Math.PI * (across - Math.floor(across))) ** 2 * (1 - 0.3 * v) * smooth(-0.3, -0.1, u);
    const grooves = 0.004 * Math.sin(v * Math.PI * 7) ** 2 * smooth(0.5, 0.75, v) * smooth(-0.1, 0.2, u);
    const out = 0.035 + rows + grooves;
    return [u * m.len, y0 + rise + out * Math.sin(th), z0 * 1.04 + out * Math.cos(th) * 0.8];
  };
  for (const side of [1, -1]) {
    const wu = n(150), wv = n(48);
    for (let j = 0; j < wu; j++) {
      const u = -0.38 + 1.52 * ((j + jit()) / wu);
      for (let i = 0; i < wv; i++) {
        const v = (i + jit()) / wv;
        // Past the body's end only the upper feathers go on, and their tips
        // are scalloped, longest at the ridge.
        if (u > 1 && (v > 0.55 || u - 1 > 0.14 * (1 - v) * (0.55 + 0.45 * Math.abs(Math.sin(v * Math.PI * 4))))) continue;
        const [x, y, z] = wing(side, u, v);
        // The normal from the panel's own shape, the lift included: the two
        // directions across it crossed, turned to face out from the body.
        const [ax, ay, az] = wing(side, u + 0.01, v), [bx2, by2, bz] = wing(side, u, v + 0.01);
        const e1 = [ax - x, ay - y, az - z], e2 = [bx2 - x, by2 - y, bz - z];
        let nx = e1[1] * e2[2] - e1[2] * e2[1], ny = e1[2] * e2[0] - e1[0] * e2[2], nz = e1[0] * e2[1] - e1[1] * e2[0];
        if (ny * y + nz * z < 0) (nx = -nx), (ny = -ny), (nz = -nz);
        // Scales: the edge of each feather, a short arc, row on row.
        const row = (u * 11 + Math.abs(Math.sin(v * Math.PI * 5)) * 0.45) % 1;
        const kind = row < 0.05 && u > -0.2 ? 3 : 0;
        add(x, y, z, nx, ny, nz, kind);
      }
    }
  }

  // The tail: short, flat and pointed, under the wing tips and turned a
  // little up, a thin slab with a face up and a face down.
  const tu = n(30), tv = n(16);
  for (let j = 0; j < tu; j++) {
    const s = (j + jit()) / tu;
    const half = 0.16 * (1 - s) * (0.4 + t * 0.6);
    for (let i = 0; i < tv; i++) {
      const v = -1 + 2 * ((i + jit()) / tv);
      const x = (0.9 + m.tail * s) * m.len, z = half * v;
      const y = profile(m.height, 0.9) * 0.62 + trim(0.9) + m.tail * 0.55 * s * s;
      for (const face of [1, -1]) add(x, y + face * 0.012, z, -0.3 * face, face, 0, 0);
    }
  }

  // The water where the swan is: the line it cuts round the hull, and
  // rings spreading from it, flat at y 0 (kinds 4 to 6, fading outward), so the bird hides the
  // far side of each and the near side crosses in front of it.
  for (const [k, kind] of [[1.0, 4], [1.24, 5], [1.58, 6]]) {
    const pts = n(300);
    for (let i = 0; i < pts; i++) {
      const g = (i / pts) * TAU;
      // Uneven gaps and a wandering radius, so a ring reads as ripples
      // pushed about by the water and not as a drawn ellipse.
      if (Math.sin(g * 7 + k * 5) + Math.sin(g * 17 + k * 11) * 0.6 + (jit() - 0.5) * 0.9 > 0.5) continue;
      const wob = 1 + Math.sin(g * 5 + k * 3) * 0.05 + Math.sin(g * 13 - k) * 0.03 + (jit() - 0.5) * 0.06;
      add(Math.cos(g) * m.len * 1.06 * k * wob, 0, Math.sin(g) * 0.58 * k * wob, 0, 1, 0, kind);
    }
  }

  // The neck: a tube along the curve, thickest where it meets the chest.
  const nu = n(40), nv = n(200);
  for (let j = 0; j < nv; j++) {
    const s = (j + jit()) / nv;
    const [px, py, pz] = bez(m.neck, s);
    const [qx, qy] = bez(m.neck, Math.min(1, s + 0.01));
    const tl = Math.hypot(qx - px, qy - py) || 1;
    const nx = -(qy - py) / tl, ny = (qx - px) / tl;
    const r = s < 0.5 ? lerp(m.neckR[0], m.neckR[1], s * 2) : lerp(m.neckR[1], m.neckR[2], (s - 0.5) * 2);
    for (let i = 0; i < nu; i++) {
      const th = ((i + jit()) / nu) * TAU;
      const c = Math.cos(th), sn = Math.sin(th);
      add(px + r * c * nx, py + r * c * ny, pz + r * sn, c * nx, c * ny, sn, 0);
    }
  }

  // The head: on from the neck's end, tipped down by tip, the beak a wedge
  // out in front with a knob where it meets the face, and an eye a side.
  const [hx, hy, hz] = bez(m.neck, 1);
  const [gx, gy] = bez(m.neck, 0.96);
  const ang = Math.atan2(hy - gy, hx - gx) + (Math.cos(Math.atan2(hy - gy, hx - gx)) < 0 ? m.tip : -m.tip);
  const fx = Math.cos(ang), fy = Math.sin(ang);
  const ux = -fy * Math.sign(fx || -1) * -1, uy = fx * Math.sign(fx || -1) * -1; // up across the head
  const [rx, ry, rz] = m.head;
  const hcx = hx + fx * rx * 0.55, hcy = hy + fy * rx * 0.55;
  const hu = n(50), hv = n(30);
  for (let j = 0; j < hv; j++) {
    const ph = ((j + jit()) / hv) * Math.PI;
    for (let i = 0; i < hu; i++) {
      const th = ((i + jit()) / hu) * TAU;
      const a = rx * Math.cos(th) * Math.sin(ph), b = ry * Math.sin(th) * Math.sin(ph), lz = rz * Math.cos(ph);
      const na = a / (rx * rx), nb = b / (ry * ry);
      add(hcx + a * fx + b * ux, hcy + a * fy + b * uy, hz + lz, na * fx + nb * ux, na * fy + nb * uy, lz / (rz * rz), 0);
    }
  }
  const bx = hcx + fx * rx * 0.8, by = hcy + fy * rx * 0.8;
  const ku = n(18), kv = n(36);
  for (let j = 0; j < kv; j++) {
    const s = (j + 0.5) / kv;
    // Deep at the face, flat and narrowing to the tip.
    const r = lerp(0.07, 0.015, s) * (0.6 + 0.4 * t);
    const x = bx + fx * m.beak * s, y = by + fy * m.beak * s;
    for (let i = 0; i < ku; i++) {
      const th = ((i + 0.5) / ku) * TAU;
      const c = Math.cos(th), sn = Math.sin(th);
      add(x + ux * r * c, y + uy * r * c, hz + r * 0.7 * sn, ux * c, uy * c, sn, 2);
    }
  }
  // The knob at the beak's base.
  for (let i = 0; i < n(60); i++) {
    const a = (i / n(60)) * TAU * 7.3, b = Math.acos(1 - (2 * i + 1) / n(60));
    const r = 0.04 * t;
    add(bx + ux * 0.05 + r * Math.cos(a) * Math.sin(b), by + uy * 0.05 + r * Math.sin(a) * Math.sin(b), hz + r * Math.cos(b), Math.cos(a), Math.sin(a), Math.cos(b), 1);
  }
  for (const side of [-1, 1]) {
    for (let i = 0; i < 8; i++) {
      const a = (i / 8) * TAU;
      const ex = hcx + fx * rx * 0.35 + ux * ry * 0.3, ey = hcy + fy * rx * 0.35 + uy * ry * 0.3;
      add(ex + 0.014 * Math.cos(a), ey + 0.014 * Math.sin(a), hz + side * rz * 0.97, 0, 0, side, 1);
    }
  }

  // Turned to face right.
  for (let p = 0; p < out.length; p += 7) {
    out[p] = -out[p];
    out[p + 3] = -out[p + 3];
  }
  return Float32Array.from(out);
}

// frame turns the model by yaw and pitch and draws it into a grid of cols
// by rows character cells, a cell aspect wide for each one tall. Each cell
// holds the shade of its nearest point (0 to 1), or -1; the reflection
// below the water is drawn into refl where nothing else is.
// light is where the light comes from, in the view: x right, y up, z out
// of the screen. By default, above, a little left and toward the viewer.
export function frame(pts, cols, rows, yaw, pitch, aspect = 0.55, light = [-0.45, 0.75, 0.5]) {
  const shade = new Float32Array(cols * rows).fill(-1);
  const refl = new Float32Array(cols * rows).fill(-1);
  const depth = new Float32Array(cols * rows).fill(-1e9);
  const rdepth = new Float32Array(cols * rows).fill(-1e9);
  // water marks the cells the water's rings won, which aren't the bird's.
  const water = new Uint8Array(cols * rows);
  // up is how far each cell's surface faces up (1) or down (-1), for light
  // from the sky above and the water below.
  const up = new Float32Array(cols * rows);
  const cyw = Math.cos(yaw), syw = Math.sin(yaw), cp = Math.cos(pitch), sp = Math.sin(pitch);
  const horizon = rows * 0.72;
  const k = Math.min(cols / 2.9, (horizon - 1) / (2.05 * aspect));
  const ox = cols / 2 - k * 0.06;
  const deep = rows - horizon;
  // The light: above, a little left and toward the viewer.
  const ll = Math.hypot(light[0], light[1], light[2]) || 1;
  const lx = light[0] / ll, ly = light[1] / ll, lz = light[2] / ll;
  for (let p = 0; p < pts.length; p += 7) {
    const x = pts[p], y = pts[p + 1], z = pts[p + 2];
    let nx = pts[p + 3], ny = pts[p + 4], nz = pts[p + 5];
    // Turn about y, then tip about x.
    const x1 = x * cyw + z * syw, z1 = -x * syw + z * cyw;
    const n1x = nx * cyw + nz * syw, n1z = -nx * syw + nz * cyw;
    const y2 = y * cp - z1 * sp, z2 = y * sp + z1 * cp;
    const n2y = ny * cp - n1z * sp, n2z = ny * sp + n1z * cp;
    const kind = pts[p + 6];
    let l = n1x * lx + n2y * ly + n2z * lz;
    // Light, a little ambient, and a rim where the surface turns away.
    const rim = Math.max(0, 1 - Math.abs(n2z)) ** 3 * 0.3;
    l = kind >= 4 ? [0.62, 0.42, 0.26][kind - 4] : kind === 1 ? 0.02 : kind === 2 ? 0.3 + 0.5 * Math.max(0, l) : kind === 3 ? (0.1 + 0.82 * Math.max(0, l) ** 0.85) * 0.9 : Math.min(1, 0.12 + 0.82 * Math.max(0, l) ** 0.85 + rim);
    const cx = Math.round(ox + x1 * k);
    const cyy = Math.round(horizon - y2 * k * aspect);
    if (cx >= 0 && cx < cols && cyy >= 0 && cyy < rows) {
      const i = cyy * cols + cx;
      if (z2 > depth[i]) {
        depth[i] = z2;
        shade[i] = l;
        water[i] = kind >= 4 ? 1 : 0;
        up[i] = n2y;
      }
    }
    // The reflection: the bird again below the line, squashed and dim.
    const ry = Math.round(horizon + y * k * aspect * 0.6);
    if (y > 0 && cx >= 0 && cx < cols && ry > horizon && ry < rows) {
      const i = ry * cols + cx;
      if (z2 > rdepth[i]) {
        rdepth[i] = z2;
        refl[i] = l * 0.55 * (1 - (ry - horizon) / deep);
      }
    }
  }
  // A cell the samples missed between drawn ones is filled from them, so
  // only the real silhouette counts as an edge.
  for (let pass = 0; pass < 2; pass++)
  for (let j = 1; j < rows - 1; j++) {
    for (let i = 1; i < cols - 1; i++) {
      const q = j * cols + i;
      if (shade[q] >= 0) continue;
      const around = [shade[q - 1], shade[q + 1], shade[q - cols], shade[q + cols]].filter((v) => v >= 0);
      if (around.length >= 3) shade[q] = around.reduce((a, b) => a + b, 0) / around.length;
    }
  }
  // Soften the shading as down softens light: each bird cell eased toward
  // its bird neighbours, so no lone sample turns into a dark speck.
  const soft = Float32Array.from(shade);
  for (let j = 1; j < rows - 1; j++) {
    for (let i = 1; i < cols - 1; i++) {
      const q = j * cols + i;
      if (shade[q] < 0 || water[q]) continue;
      let sum = shade[q] * 2, n = 2;
      for (const r of [q - 1, q + 1, q - cols, q + cols]) if (shade[r] >= 0 && !water[r]) (sum += shade[r]), n++;
      soft[q] = Math.max(shade[q], sum / n);
    }
  }
  shade.set(soft);
  // edge is each drawn cell beside an empty one: the outline, drawn bright.
  const edge = new Uint8Array(cols * rows);
  for (let j = 1; j < rows - 1; j++) {
    for (let i = 1; i < cols - 1; i++) {
      const q = j * cols + i;
      const bird = (r) => shade[r] >= 0 && !water[r];
      if (bird(q) && (!bird(q - 1) || !bird(q + 1) || !bird(q - cols) || !bird(q + cols))) edge[q] = 1;
    }
  }
  return { shade, refl, edge, water, up, horizon: Math.round(horizon) };
}

// text is a frame as lines of characters, for a page without script.
export function text(f, cols, rows) {
  const lines = [];
  for (let j = 0; j < rows; j++) {
    let s = "";
    for (let i = 0; i < cols; i++) {
      const v = f.shade[j * cols + i];
      const r = f.refl[j * cols + i];
      if (v >= 0 && f.water[j * cols + i]) s += (i + j) % 3 ? "-" : "~";
      else if (v >= 0) s += ramp[Math.min(ramp.length - 1, Math.round(v * (ramp.length - 1)))];
      else if (j === f.horizon) s += (i * 7) % 11 < 6 ? "~" : "-";
      else if (r > 0.04) s += ramp[Math.min(4, Math.round(r * 8))];
      else s += " ";
    }
    lines.push(s.replace(/\s+$/, ""));
  }
  return lines.join("\n");
}
