// The swan: a 3D model drawn in ASCII, the way donut.c draws a torus. Its
// surfaces (a boat of a body, an S of a neck swept as a tube, head, beak,
// raised wings, tail, eyes) are sampled densely into points with normals;
// each frame they're turned, projected, the nearest point kept for each
// character cell, and shaded by how much it faces the light. t grows it
// from a cygnet (0) to a swan (1). The Worker draws one still frame of it
// into the page and app.js brings it to life, from this one file.

const TAU = Math.PI * 2;
const lerp = (a, b, t) => a + (b - a) * t;

// ramp is the shading, dark to light.
export const ramp = " .,:-=+*#%@";

// shape is the swan's measurements at t, its neck held at pose: 0 upright,
// 1 arched back over its body to look behind it, and sway its side to side
// lean.
function shape(t, pose, sway) {
  const L = (a, b) => lerp(a, b, t);
  const P = (up, arch) => lerp(up, arch, pose);
  return {
    scale: L(0.66, 1),
    len: L(0.8, 1.05),
    ry: L(0.46, 0.34),
    rz: L(0.52, 0.46),
    lift: L(0.04, 0.22),
    neck: [
      [L(-0.52, -0.74), L(0.28, 0.34), 0],
      [L(-0.78, P(-1.16, -1.22)), L(0.5, P(0.82, 0.86)), sway * 0.3],
      [L(-0.4, P(-0.26, -0.36)), L(0.72, P(1.24, 1.3)), sway * 0.8],
      [L(-0.62, P(-0.7, -0.8)), L(0.86, P(1.52, 1.48)), sway],
    ],
    neckR: [L(0.21, 0.18), L(0.12, 0.068)],
    head: [L(0.16, 0.17), L(0.13, 0.1), L(0.12, 0.09)],
    beak: L(0.1, 0.27),
    wing: [L(0.4, 0.66), L(0.12, 0.2), L(0.1, 0.12)],
    wingLift: L(0.02, 0.13),
    tail: L(0.06, 0.24),
  };
}

function bez(p, s) {
  const k = 1 - s;
  const a = k * k * k, b = 3 * k * k * s, c = 3 * k * s * s, d = s * s * s;
  return [0, 1, 2].map((i) => a * p[0][i] + b * p[1][i] + c * p[2][i] + d * p[3][i]);
}

// model is the swan at t as points: x y z, then the normal, then what it
// is (0 plumage, 1 eye, 2 beak), seven numbers a point.
export function model(t, density = 1, pose = 0, sway = 0, wings = 0) {
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
  const cy = 0.12;

  // The body: an egg along x, fuller at the chest, rising to the tail,
  // cut at the water.
  const bu = n(190), bv = n(90);
  for (let j = 0; j < bv; j++) {
    const s = (j + jit()) / bv;
    const x0 = -Math.cos(Math.PI * s);
    const r = Math.pow(Math.sin(Math.PI * s), 0.72) * (1 - 0.16 * x0);
    const rise = m.lift * Math.max(0, x0) ** 2;
    for (let i = 0; i < bu; i++) {
      const th = ((i + jit()) / bu) * TAU;
      const y = cy + m.ry * r * Math.sin(th) + rise;
      if (y < -0.02) continue;
      const z = m.rz * r * Math.cos(th);
      add(x0 * m.len, y, z, Math.cos(Math.PI * s) * -0.3, Math.sin(th) / m.ry, Math.cos(th) / m.rz, 0);
    }
  }

  // The wings, folded and raised a little along the back, one a side: the
  // upper shell only, since a folded wing shows its top. lift raises them
  // a touch off the back, as a swan does when it settles its feathers.
  for (const side of [-1, 1]) {
    const wu = n(76), wv = n(40);
    const [rx, ry, rz] = m.wing;
    const raise = m.wingLift + 0.08 * wings;
    for (let j = 0; j < wv; j++) {
      const ph = ((j + jit()) / wv) * Math.PI;
      for (let i = 0; i < wu; i++) {
        const th = ((i + jit()) / wu) * Math.PI;
        const lx = rx * Math.cos(th) * Math.sin(ph), ly = ry * Math.sin(th) * Math.sin(ph), lz = rz * Math.cos(ph);
        const x = 0.16 * m.len + lx;
        const y = cy + m.ry * 0.5 + ly + raise * Math.max(0, lx / rx + 0.3) ** 2;
        const z = side * (m.rz * 0.5) + lz;
        add(x, y, z, lx / (rx * rx), ly / (ry * ry), lz / (rz * rz), 0);
      }
    }
  }

  // The neck: a tube along an S, thick at the chest and fine at the head.
  const nu = n(36), nv = n(190);
  for (let j = 0; j < nv; j++) {
    const s = (j + jit()) / nv;
    const [px, py, pz] = bez(m.neck, s);
    const [qx, qy] = bez(m.neck, Math.min(1, s + 0.01));
    const tl = Math.hypot(qx - px, qy - py) || 1;
    const nx = -(qy - py) / tl, ny = (qx - px) / tl;
    const r = lerp(m.neckR[0], m.neckR[1], Math.pow(s, 0.7));
    for (let i = 0; i < nu; i++) {
      const th = ((i + jit()) / nu) * TAU;
      const c = Math.cos(th), sn = Math.sin(th);
      add(px + r * c * nx, py + r * c * ny, pz + r * sn, c * nx, c * ny, sn, 0);
    }
  }

  // The head, carried on along the neck and tipped down toward the water,
  // the beak out in front of it, and an eye a side.
  const [hx, hy, hz] = bez(m.neck, 1);
  const [gx, gy] = bez(m.neck, 0.97);
  let ang = Math.atan2(hy - gy, hx - gx);
  // Tip the head 40 degrees toward straight down, whichever way is shorter.
  const down = -Math.PI / 2;
  let dlt = ((down - ang + 3 * Math.PI) % TAU) - Math.PI;
  ang += Math.sign(dlt) * Math.min(Math.abs(dlt), 0.45);
  const fx = Math.cos(ang), fy = Math.sin(ang);
  const ux = -fy, uy = fx; // up, across the head
  const [rx, ry, rz] = m.head;
  const hcx = hx + fx * rx * 0.45 + ux * ry * 0.2, hcy = hy + fy * rx * 0.45 + uy * ry * 0.2;
  const hu = n(44), hv = n(28);
  for (let j = 0; j < hv; j++) {
    const ph = ((j + jit()) / hv) * Math.PI;
    for (let i = 0; i < hu; i++) {
      const th = ((i + jit()) / hu) * TAU;
      const a = rx * Math.cos(th) * Math.sin(ph), b = ry * Math.sin(th) * Math.sin(ph), lz = rz * Math.cos(ph);
      const na = a / (rx * rx), nb = b / (ry * ry);
      add(hcx + a * fx + b * ux, hcy + a * fy + b * uy, hz + lz, na * fx + nb * ux, na * fy + nb * uy, lz / (rz * rz), 0);
    }
  }
  const bx = hcx + fx * rx * 0.85 - ux * ry * 0.25, by = hcy + fy * rx * 0.85 - uy * ry * 0.25;
  // The beak runs a little below the head's line.
  const kx = Math.cos(ang - 0.35 * Math.sign(fx || 1)), ky = Math.sin(ang - 0.35 * Math.sign(fx || 1));
  const ku = n(16), kv = n(30);
  for (let j = 0; j < kv; j++) {
    const s = (j + 0.5) / kv;
    const r = lerp(0.045, 0.006, s) * (1 + m.beak);
    const x = bx + kx * m.beak * s, y = by + ky * m.beak * s;
    for (let i = 0; i < ku; i++) {
      const th = ((i + 0.5) / ku) * TAU;
      add(x - ky * r * Math.sin(th), y + kx * r * Math.sin(th), hz + r * Math.cos(th), -ky * Math.sin(th), kx * Math.sin(th), Math.cos(th), 2);
    }
  }
  for (const side of [-1, 1]) {
    for (let i = 0; i < 6; i++) {
      const a = (i / 6) * TAU;
      const ex = hcx + fx * rx * 0.25 + ux * ry * 0.25, ey = hcy + fy * rx * 0.25 + uy * ry * 0.25;
      add(ex + 0.012 * Math.cos(a), ey + 0.012 * Math.sin(a), hz + side * rz * 0.98, 0, 0, side, 1);
    }
  }

  // The tail, a short point lifted at the back.
  const tx = m.len * 0.94, ty = cy + m.lift + 0.06;
  const tu = n(14), tv = n(20);
  for (let j = 0; j < tv; j++) {
    const s = (j + 0.5) / tv;
    const r = lerp(0.1, 0.01, s);
    for (let i = 0; i < tu; i++) {
      const th = ((i + 0.5) / tu) * TAU;
      add(tx + m.tail * s, ty + m.tail * 0.8 * s + r * Math.sin(th), r * Math.cos(th) * 1.6, 0.4, Math.sin(th), Math.cos(th), 0);
    }
  }
  // Turned to face right, as the swan in front of the card does.
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
  const k = Math.min(cols / 2.7, (horizon - 1) / (1.8 * aspect));
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
