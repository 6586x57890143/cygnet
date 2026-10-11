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
    height: [[-1, L(0.2, 0.14)], [-0.75, L(0.42, 0.36)], [-0.35, L(0.58, 0.64)], [0.1, L(0.64, 0.8)], [0.5, L(0.6, 0.88)], [0.82, L(0.48, 0.8)], [1, L(0.3, 0.52)]],
    width: [[-1, 0.1], [-0.75, 0.36], [-0.3, 0.52], [0.3, 0.54], [0.8, 0.4], [1, 0.12]],
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
// is (0 plumage, 1 eye, 2 beak, 3 the line between feathers), seven
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

  // The body: each slice across it a half ellipse standing on the water,
  // as high and as wide as the profiles say there. The folded wings are
  // its top: rows of feathers run back along it, and toward the rear the
  // long ones lift.
  const bu = n(120), bv = n(120);
  for (let j = 0; j < bv; j++) {
    const u = -1 + 2 * ((j + jit()) / bv);
    const x = u * m.len;
    const hh = profile(m.height, u), ww = profile(m.width, u);
    const dh = (profile(m.height, u + 0.01) - profile(m.height, u - 0.01)) / 0.02;
    for (let i = 0; i < bu; i++) {
      const th = ((i + jit()) / bu) * Math.PI;
      const y = hh * Math.sin(th), z = ww * Math.cos(th);
      // Rows of feathers: a faint line every so often along the back.
      const row = (u * 9 + Math.cos(th) * 2.5) % 1;
      const kind = th > 0.5 && th < Math.PI - 0.5 && Math.abs(row) < 0.07 && u > -0.4 ? 3 : 0;
      add(x, y, z, -dh * Math.sin(th) * 0.4, Math.sin(th) / hh, Math.cos(th) / ww, kind);
    }
  }

  // The long feathers raised at the back of the folded wings, and the tail
  // below them: points turned up and back.
  const spike = (x0, y0, z0, len, ang, r0) => {
    const su = n(10), sv = n(22);
    const dx = Math.cos(ang), dy = Math.sin(ang);
    for (let j = 0; j < sv; j++) {
      const s = (j + jit()) / sv;
      const r = r0 * (1 - s);
      for (let i = 0; i < su; i++) {
        const th = ((i + jit()) / su) * TAU;
        const c = Math.cos(th), sn = Math.sin(th);
        add(x0 + dx * len * s - dy * r * c, y0 + dy * len * s + dx * r * c, z0 + r * sn * 1.8, -dy * c, dx * c, sn, 0);
      }
    }
  };
  for (const [k, z] of [[0, -0.18], [1, 0.18], [2, -0.06], [3, 0.06], [4, -0.3], [5, 0.3]]) {
    const u = 0.66 + k * 0.035;
    spike(u * m.len, profile(m.height, u) - 0.06, z, m.plumes * (1 - k * 0.08), 0.55 + k * 0.05, 0.05);
  }
  for (const z of [-0.08, 0, 0.08]) spike(0.94 * m.len, profile(m.height, 0.94) * 0.7, z, m.tail, 0.75, 0.05);

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
export function frame(pts, cols, rows, yaw, pitch, aspect = 0.55) {
  const shade = new Float32Array(cols * rows).fill(-1);
  const refl = new Float32Array(cols * rows).fill(-1);
  const depth = new Float32Array(cols * rows).fill(-1e9);
  const rdepth = new Float32Array(cols * rows).fill(-1e9);
  const cyw = Math.cos(yaw), syw = Math.sin(yaw), cp = Math.cos(pitch), sp = Math.sin(pitch);
  const horizon = rows * 0.72;
  const k = Math.min(cols / 2.9, (horizon - 1) / (2.05 * aspect));
  const ox = cols / 2 - k * 0.06;
  const deep = rows - horizon;
  // The light: above, a little left and toward the viewer.
  const lx = -0.45, ly = 0.75, lz = 0.5;
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
    l = kind === 1 ? 0.02 : kind === 2 ? 0.3 + 0.5 * Math.max(0, l) : kind === 3 ? 0.06 + 0.4 * Math.max(0, l) : Math.min(1, 0.12 + 0.82 * Math.max(0, l) ** 0.85 + rim);
    const cx = Math.round(ox + x1 * k);
    const cyy = Math.round(horizon - y2 * k * aspect);
    if (cx >= 0 && cx < cols && cyy >= 0 && cyy < rows) {
      const i = cyy * cols + cx;
      if (z2 > depth[i]) {
        depth[i] = z2;
        shade[i] = l;
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
  // edge is each drawn cell beside an empty one: the outline, drawn bright.
  const edge = new Uint8Array(cols * rows);
  for (let j = 1; j < rows - 1; j++) {
    for (let i = 1; i < cols - 1; i++) {
      const q = j * cols + i;
      if (shade[q] >= 0 && (shade[q - 1] < 0 || shade[q + 1] < 0 || shade[q - cols] < 0 || shade[q + cols] < 0)) edge[q] = 1;
    }
  }
  return { shade, refl, edge, horizon: Math.round(horizon) };
}

// text is a frame as lines of characters, for a page without script.
export function text(f, cols, rows) {
  const lines = [];
  for (let j = 0; j < rows; j++) {
    let s = "";
    for (let i = 0; i < cols; i++) {
      const v = f.shade[j * cols + i];
      const r = f.refl[j * cols + i];
      if (v >= 0) s += ramp[Math.min(ramp.length - 1, Math.round(v * (ramp.length - 1)))];
      else if (j === f.horizon) s += (i * 7) % 11 < 6 ? "~" : "-";
      else if (r > 0.04) s += ramp[Math.min(4, Math.round(r * 8))];
      else s += " ";
    }
    lines.push(s.replace(/\s+$/, ""));
  }
  return lines.join("\n");
}
