// The merlin: a 3D model drawn in ASCII, the way swan.js draws the swan.
// Its surface is sampled densely into points with normals and colours;
// each frame they're turned, projected, the nearest point kept for each
// character cell and lit by how much it faces the light. The Worker draws
// one still frame into /support and support.js brings it to life.
//
// Nothing here is a guessed shape. Every dimension is measured off
// photographs of a real merlin (public domain, US Fish and Wildlife Service
// and the National Park Service), in units of her height from the perch to
// her crown, x from the perch point toward her tail, y up, z across:
//
//   the side profile, height by height, from "Merlin" 54309288128: where
//   her front and her back are (side);
//   her width at each height from a bird facing the camera, "Merlin,
//   January 27 2021" 50953149268 (width);
//   landmarks off the side photo: the eye, the cere and the beak's tip, the
//   tail's base and tip and its thickness, the legs and the feet.
//
// Her colours are those photos', sampled where each point falls: the side
// photo's by x and y, the front photo's by z and y, mixed by which way the
// point faces (merlin-skin.js, written by tools/supportart from
// art/source). She is
// rigged as the photos measure her: her head turns about her neck, her
// tail pumps about its base and her breast rises and falls.

import { front, side as fromSide } from "./merlin-skin.js";

const TAU = Math.PI * 2;
const lerp = (a, b, t) => a + (b - a) * t;

// ramp is the shading, dark to light.
export const ramp = " .,:-=+*#%@";

// part is what a point belongs to, which is what the rig moves.
export const part = { body: 0, head: 1, tail: 2, eye: 3, glint: 4, branch: 5, ring: 6 };

// The side profile, measured: [height, front, back]. Below 0.141 the
// photo's rows are her feet and the branch, not feathers.
const side = [
  [0.141, 0.018, 0.53],
  [0.183, -0.014, 0.51],
  [0.225, -0.067, 0.49],
  [0.268, -0.113, 0.475],
  [0.31, -0.144, 0.454],
  [0.352, -0.18, 0.419],
  [0.394, -0.215, 0.394],
  [0.437, -0.243, 0.368],
  [0.479, -0.268, 0.354],
  [0.521, -0.292, 0.336],
  [0.563, -0.303, 0.336],
  [0.606, -0.31, 0.315],
  [0.648, -0.313, 0.28],
  [0.69, -0.306, 0.252],
  [0.732, -0.296, 0.217],
  [0.775, -0.278, 0.167],
  [0.817, -0.275, 0.086],
  [0.859, -0.278, 0.051],
  [0.901, -0.289, 0.03],
  [0.944, -0.261, 0.005],
  [0.986, -0.204, -0.03],
];

// Her width, measured off the front view: [height, width].
const width = [
  [0.065, 0.4],
  [0.12, 0.45],
  [0.23, 0.45],
  [0.285, 0.46],
  [0.34, 0.505],
  [0.395, 0.55],
  [0.45, 0.58],
  [0.533, 0.64],
  [0.588, 0.57],
  [0.67, 0.53],
  [0.725, 0.47],
  [0.78, 0.39],
  [0.835, 0.31],
  [0.89, 0.28],
  [0.945, 0.265],
  [1, 0.175],
];

// Landmarks off the side photo.
// Her eye, off the photo at eight times: a dark oval this wide and tall
// in profile, with a thin olive-yellow ring round it, brightest below,
// and a small sharp catchlight up and forward of its middle.
const eye = [-0.132, 0.879];
const eyeR = [0.0275, 0.0236], ringW = 0.006;
const pupil = [13, 13, 13], ringTop = [98, 85, 32], ringLow = [121, 108, 30];
const catchlight = [-0.0044, 0.0044], catchR = 0.0045;
const cere = [-0.225, 0.849];
const tip = [-0.255, 0.789];
const tailBase = [0.405, 0.07], tailTip = [0.757, -0.194];
// The tail, measured along the line from its base to its tip (s, 0 to 1):
// in profile, how far its upper edge (toward her back) and its lower edge
// lie from that line; it comes out from under her wings at s 0.16, and is
// taken as it is there under them. Its width across, from the front view,
// where it hangs below the perch (rows 396 to 524 there, which is where
// the front skin is read for it).
const tailSide = [
  [0.16, 0.112, 0.054],
  [0.24, 0.098, 0.054],
  [0.28, 0.094, 0.04],
  [0.36, 0.086, 0.04],
  [0.48, 0.08, 0.036],
  [0.6, 0.072, 0.036],
  [0.72, 0.066, 0.036],
  [0.84, 0.058, 0.032],
  [0.92, 0.054, 0.026],
  [0.96, 0.048, 0.022],
  [1, 0.042, 0.01],
];
const tailWidth = [
  [0.16, 0.286],
  [0.24, 0.261],
  [0.29, 0.245],
  [0.34, 0.22],
  [0.4, 0.187],
  [0.45, 0.162],
  [0.55, 0.156],
  [0.71, 0.154],
  [0.92, 0.129],
  [0.97, 0.087],
  [1, 0.046],
];
const tailFront = [-0.21, -0.74];
// Where the tail's lower edge, carried forward under her, meets her belly
// (at y 0.11): s -0.3 puts it at (0.28, 0.08).
const tailRoot = -0.3;
// The legs and feet, off the side photo's feet at four times: the branch
// under her (it runs across her, side to side, as a perch does) is 0.19
// thick with its top at 0.088; each tarsus runs from her feathers at
// (0.147, 0.207) down to its foot's pad on top of the branch, 0.029
// thick; three toes wrap forward and down round it, 0.022 thick, fanning
// apart, the middle one longest (it runs 0.11 down the branch's face in
// the photo after crossing its top), and the hallux wraps back; each ends
// in a talon 0.05 long that carries the curve on. Half the distance
// between her legs is from the front view; the colours are the photo's
// yellow and the talon's black, each a mean over its pixels.
export const perch = { x: 0.03, y: -0.007, r: 0.095 };
const thigh = [0.147, 0.207];
const legApart = 0.05;
const toeR = 0.011;
// [where it starts across, how far across it ends, length round the
// branch, which way]: forward is -1; across is outward from her middle.
const toes = [
  [-0.012, -0.05, 0.12, -1],
  [0, 0, 0.15, -1],
  [0.012, 0.05, 0.12, -1],
  [0, -0.012, 0.09, 1],
];
const talon = 0.05;
const yellowFoot = [224, 154, 29], talonBlack = [24, 18, 11];
// The bark, the mean of the photo's branch where it is clear of her.
export const bark = [105, 84, 58];
// The neck, which the head turns about, and the crown's top.
const neck = [-0.07, 0.8];
const crown = 1;
// Under her lowest measured row the branch hides her; her belly closes
// over down to just above the pads of her feet.
const belly = 0.11;

// knot reads a smooth curve through [x, y] knots at x, as swan.js does.
function knot(knots, x, i = 1) {
  if (x <= knots[0][0]) return knots[0][i];
  for (let k = 1; k < knots.length; k++) {
    const [x0, x1] = [knots[k - 1][0], knots[k][0]];
    if (x <= x1) {
      const t = (x - x0) / (x1 - x0);
      return lerp(knots[k - 1][i], knots[k][i], t * t * (3 - 2 * t));
    }
  }
  return knots[knots.length - 1][i];
}

// section is her cross section at height h: its centre front to back, and
// its half depth and half width.
function section(h) {
  const top = side[side.length - 1][0], low = side[0][0];
  // Above the last measured row the crown closes over in a quarter round,
  // and under the first her belly does.
  const close =
    h > top ? Math.sqrt(Math.max(0, 1 - ((h - top) / (crown - top)) ** 2)) : h < low ? Math.sqrt(Math.max(0, 1 - ((low - h) / (low - belly)) ** 2)) : 1;
  const f = knot(side, Math.min(h, top), 1), b = knot(side, Math.min(h, top), 2);
  return [(f + b) / 2, ((b - f) / 2) * close, (knot(width, h) / 2) * close];
}

// read is a skin's colour at u, v.
const bytes = (g) => Uint8Array.from(atob(g.rgb), (c) => c.charCodeAt(0));
const sideRGB = bytes(fromSide), frontRGB = bytes(front);
function read(g, rgb, u, v) {
  const i = Math.max(0, Math.min(g.cols - 1, Math.floor((u - g.u0) / g.step)));
  const j = Math.max(0, Math.min(g.rows - 1, Math.floor((g.v1 - v) / g.step)));
  const q = (j * g.cols + i) * 3;
  return [rgb[q], rgb[q + 1], rgb[q + 2]];
}

// colour is her colour at a point: the side photo's where it faces
// sideways or back, the front photo's where it faces forward (its normal
// nx toward her front, which is -x), mixed between. fy is where the front
// photo has the point, if not at y. The front photo's head is turned
// away, so above her neck the side photo's colour is hers all round.
function colour(x, y, z, nx, fy = y) {
  const s = read(fromSide, sideRGB, x, y);
  const below = Math.max(0, Math.min(1, (neck[1] - y) / 0.08));
  const w = Math.max(0, Math.min(1, (-nx - 0.25) / 0.5)) * below;
  if (w === 0) return s;
  const f = read(front, frontRGB, z, fy);
  return s.map((v, i) => v + (f[i] - v) * w);
}

// model is the merlin at rest as points, ten numbers each: x y z, the
// normal, the colour, and the part. pose moves her from there.
export function model({ density = 1 } = {}) {
  const out = [];
  let seed = 11;
  const jit = () => ((seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff) * 0.9 + 0.05;
  const n = (a) => Math.max(4, Math.round(a * density));
  const add = (x, y, z, nx, ny, nz, p, c = colour(x, y, z, nx / (Math.hypot(nx, ny, nz) || 1))) => {
    const l = Math.hypot(nx, ny, nz) || 1;
    out.push(x, y, z, nx / l, ny / l, nz / l, c[0], c[1], c[2], p);
  };

  // The body and the head, one surface lofted through the measured
  // sections from under the belly to the crown.
  const bottom = belly;
  const hv = n(170), hu = n(150);
  for (let j = 0; j < hv; j++) {
    const h = bottom + ((crown - bottom) * (j + jit())) / hv;
    const [c, a, b] = section(h);
    const dh = 0.004;
    const [c1, a1, b1] = section(Math.min(crown, h + dh));
    const [c0, a0, b0] = section(Math.max(bottom, h - dh));
    const dc = (c1 - c0) / (2 * dh), da = (a1 - a0) / (2 * dh), db = (b1 - b0) / (2 * dh);
    for (let i = 0; i < hu; i++) {
      const th = ((i + jit()) / hu) * TAU;
      const co = Math.cos(th), si = Math.sin(th);
      const x = c + a * co, z = b * si;
      // The normal: across the section, crossed with up the surface,
      // turned to face out.
      const tu = [-a * si, 0, b * co];
      const tv = [dc + da * co, 1, db * si];
      const nx = tu[1] * tv[2] - tu[2] * tv[1], ny = tu[2] * tv[0] - tu[0] * tv[2], nz = tu[0] * tv[1] - tu[1] * tv[0];
      const s = nx * (x - c) + nz * z >= 0 ? 1 : -1;
      add(x, h, z, nx * s, ny * s, nz * s, h > neck[1] && x < 0.12 ? part.head : part.body);
    }
  }

  // The beak: from the cere to its tip, deep at the face and narrow, hooked
  // down as it measures.
  const ku = n(24), kv = n(30);
  for (let j = 0; j < kv; j++) {
    const s = (j + 0.5) / kv;
    const x = lerp(cere[0], tip[0], s), y = lerp(cere[1], tip[1], s) - 0.012 * Math.sin(s * Math.PI);
    const r = lerp(0.034, 0.006, s);
    for (let i = 0; i < ku; i++) {
      const th = ((i + 0.5) / ku) * TAU;
      const bx = x - 0.01 * Math.cos(th), by = y + r * Math.cos(th);
      add(bx, by, r * 0.62 * Math.sin(th), -0.4, Math.cos(th), Math.sin(th), part.head, read(fromSide, sideRGB, bx, by));
    }
  }

  // The eyes, where the eye measures, on the head's surface either side:
  // the dark oval, its catchlight, and the ring round it. pose shuts them.
  const [ec, ea, eb] = section(eye[1]);
  const ez = eb * Math.sqrt(Math.max(0, 1 - ((eye[0] - ec) / ea) ** 2)) + 0.004;
  for (const sideZ of [-1, 1]) {
    const m = n(260);
    for (let i = 0; i < m; i++) {
      // Spread evenly over the oval and its ring (a sunflower spiral).
      const a = i * 2.39996, rr = Math.sqrt((i + 0.5) / m) * (1 + ringW / eyeR[1]);
      const ox = Math.cos(a) * rr * eyeR[0], oy = Math.sin(a) * rr * eyeR[1];
      const ring = rr > 1;
      const glint = !ring && Math.hypot(ox - catchlight[0], oy - catchlight[1]) < catchR;
      const p = ring ? part.ring : glint ? part.glint : part.eye;
      const c = ring ? (oy < 0 ? ringLow : ringTop) : glint ? [255, 255, 255] : pupil;
      add(eye[0] + ox, eye[1] + oy, sideZ * ez, -0.2, 0.1, sideZ, p, c);
    }
  }

  // The tail: from its base under the folded wings to its tip, its section
  // at each step as deep in profile and as wide across as it measures,
  // centred between its measured edges, each point the photo's colour
  // where it falls in profile. Behind the photo's branch it carries on
  // forward under her belly as it is where it comes out, to tailRoot,
  // where its lower edge meets her belly: the undertail coverts that join
  // the two.
  const tdx = tailTip[0] - tailBase[0], tdy = tailTip[1] - tailBase[1];
  const tl = Math.hypot(tdx, tdy);
  const vx = -tdy / tl, vy = tdx / tl;
  const tu = n(60), tv = n(140);
  for (let j = 0; j < tv; j++) {
    const s = tailRoot + ((1 - tailRoot) * (j + jit())) / tv;
    const up = knot(tailSide, s, 1), down = knot(tailSide, s, 2);
    const off = (up - down) / 2, half = (up + down) / 2, wide = knot(tailWidth, s) / 2;
    const cx = tailBase[0] + tdx * s + vx * off, cy = tailBase[1] + tdy * s + vy * off;
    for (let i = 0; i < tu; i++) {
      const th = ((i + jit()) / tu) * TAU;
      const co = Math.cos(th), si = Math.sin(th);
      const x = cx + vx * half * co, y = cy + vy * half * co;
      const nx = (vx * co) / half, ny = (vy * co) / half, nz = si / wide;
      const c = colour(x, y, wide * si, nx / Math.hypot(nx, ny, nz), lerp(tailFront[0], tailFront[1], Math.max(0, (s - 0.16) / 0.84)));
      add(x, y, wide * si, nx, ny, nz, part.tail, c);
    }
  }

  // The legs and the feet: a tube along a path ([x, y, z] points),
  // tapering from r0 to r1.
  const tube = (path, r0, r1, c) => {
    let total = 0;
    for (let k = 1; k < path.length; k++) total += Math.hypot(path[k][0] - path[k - 1][0], path[k][1] - path[k - 1][1]);
    let done = 0;
    for (let k = 1; k < path.length; k++) {
      const [a, b] = [path[k - 1], path[k]];
      const len = Math.hypot(b[0] - a[0], b[1] - a[1]);
      const steps = Math.max(2, Math.ceil(len * 300 * density));
      const px = -(b[1] - a[1]) / len, py = (b[0] - a[0]) / len;
      for (let j = 0; j < steps; j++) {
        const s = (j + 0.5) / steps;
        const r = lerp(r0, r1, (done + len * s) / total);
        const x = lerp(a[0], b[0], s), y = lerp(a[1], b[1], s);
        for (let i = 0; i < n(12); i++) {
          const th = (i / n(12)) * TAU;
          const co = Math.cos(th), si = Math.sin(th);
          add(x + px * r * co, y + py * r * co, lerp(a[2], b[2], s) + r * si, px * co, py * co, si, part.body, c);
        }
      }
      done += len;
    }
  };
  // round is a path round the branch from its top, length long, forward
  // (-1) or back (1), a little off its surface, across from z0 to z1.
  const round = (from, length, dir, lift, z0, z1) => {
    const R = perch.r + lift, pts = [];
    for (let k = 0; k <= 12; k++) {
      const a = Math.PI / 2 + from - (dir * (length / R) * k) / 12;
      pts.push([perch.x + R * Math.cos(a), perch.y + R * Math.sin(a), lerp(z0, z1, k / 12)]);
    }
    return pts;
  };
  const pad = [perch.x, perch.y + perch.r + toeR];
  for (const sideZ of [-1, 1]) {
    const z = sideZ * legApart;
    tube([[...thigh, z], [...pad, z]], 0.0145, 0.0135, yellowFoot);
    for (const [z0, z1, length, dir] of toes) {
      const [a0, a1] = [z + sideZ * z0, z + sideZ * z1];
      tube(round(0, length, dir, toeR, a0, a1), toeR, 0.0085, yellowFoot);
      // The talon carries on round from where the toe ends, curving in
      // and on across the way the toe was going.
      const a = length / (perch.r + toeR);
      const on = a1 + ((a1 - a0) * talon) / length;
      tube(round(-dir * a, talon, dir, 0.004, a1, on), 0.0065, 0.001, talonBlack);
    }
  }
  return Float32Array.from(out);
}

let lidColour;

// pose is the model moved into a pose, written into into (a copy of rest
// the same length): look turns her head about her neck (radians, toward
// the viewer positive), pump swings her tail down about its base
// (radians), breathe swells her chest along its normals (0 to 1) and
// blink shuts her eyes from the top (0 open to 1 shut), in the colour of
// the feathers over them.
export function pose(rest, { look = 0, pump = 0, breathe = 0, blink = 0 } = {}, into = new Float32Array(rest.length)) {
  into.set(rest);
  lidColour ??= colour(eye[0], eye[1] + 0.04, 0, 0);
  const cl = Math.cos(look), sl = Math.sin(look);
  const cp = Math.cos(pump), sp = Math.sin(pump);
  for (let q = 0; q < into.length; q += 10) {
    const p = into[q + 9];
    if (p === part.head || p === part.eye || p === part.glint || p === part.ring) {
      if (blink > 0 && (p === part.eye || p === part.glint) && into[q + 1] - eye[1] > eyeR[1] * (1 - 2 * blink)) {
        into[q + 6] = lidColour[0];
        into[q + 7] = lidColour[1];
        into[q + 8] = lidColour[2];
        into[q + 9] = part.head;
      }
      if (look === 0) continue;
      const dx = into[q] - neck[0], z = into[q + 2], nx = into[q + 3], nz = into[q + 5];
      into[q] = neck[0] + dx * cl - z * sl;
      into[q + 2] = dx * sl + z * cl;
      into[q + 3] = nx * cl - nz * sl;
      into[q + 5] = nx * sl + nz * cl;
    } else if (p === part.tail) {
      if (pump === 0) continue;
      const dx = into[q] - tailBase[0], dy = into[q + 1] - tailBase[1], nx = into[q + 3], ny = into[q + 4];
      into[q] = tailBase[0] + dx * cp + dy * sp;
      into[q + 1] = tailBase[1] - dx * sp + dy * cp;
      into[q + 3] = nx * cp + ny * sp;
      into[q + 4] = -nx * sp + ny * cp;
    } else if (p === part.body && breathe > 0) {
      const d = 0.008 * breathe * Math.max(0, 1 - Math.abs(into[q + 1] - 0.55) / 0.3);
      into[q] += into[q + 3] * d;
      into[q + 2] += into[q + 5] * d;
    }
  }
  return into;
}

// branch is the perch she stands on, as points: across her from -half to
// half, in the bark's colour.
export function branch(half = 0.6, density = 1) {
  const out = [];
  const along = Math.round(half * 320 * density), round = Math.round(48 * density);
  for (let j = 0; j < along; j++) {
    const z = -half + (2 * half * (j + 0.5)) / along;
    for (let i = 0; i < round; i++) {
      const a = (i / round) * TAU;
      const c = Math.cos(a), s = Math.sin(a);
      out.push(perch.x + perch.r * c, perch.y + perch.r * s, z, c, s, 0, bark[0], bark[1], bark[2], part.branch);
    }
  }
  return Float32Array.from(out);
}

// frame turns the model by yaw and pitch and draws it into a grid of cols
// by rows character cells, a cell aspect wide for each one tall, the perch
// at row perch. Each cell holds the shade of its nearest point (0 to 1,
// or -1 for none), its colour and its part; edge marks the outline. light
// is where the light comes from in the view: x right, y up, z toward the
// viewer.
export function frame(pts, cols, rows, yaw, pitch, aspect = 0.55, light = [-0.45, 0.75, 0.5], perch = rows * 0.8) {
  const shade = new Float32Array(cols * rows).fill(-1);
  const colours = new Uint8Array(cols * rows * 3);
  const parts = new Uint8Array(cols * rows);
  const depth = new Float32Array(cols * rows).fill(-1e9);
  const cyw = Math.cos(yaw), syw = Math.sin(yaw), cp = Math.cos(pitch), sp = Math.sin(pitch);
  // Scale so her height and her tail fit the box.
  const k = Math.min(cols / 1.35, (perch - 1) / (1.08 * aspect));
  const ox = cols * 0.42;
  const ll = Math.hypot(light[0], light[1], light[2]) || 1;
  const lx = light[0] / ll, ly = light[1] / ll, lz = light[2] / ll;
  // The surface band a cell averages over: a cell and a half deep.
  const band = 1.5 / k;
  const sum = new Float32Array(cols * rows * 4), count = new Uint16Array(cols * rows);
  for (let p = 0; p < pts.length; p += 10) {
    const x = pts[p], y = pts[p + 1], z = pts[p + 2];
    const nx = pts[p + 3], ny = pts[p + 4], nz = pts[p + 5];
    const x1 = x * cyw + z * syw, z1 = -x * syw + z * cyw;
    const n1x = nx * cyw + nz * syw, n1z = -nx * syw + nz * cyw;
    const y2 = y * cp - z1 * sp, z2 = y * sp + z1 * cp;
    const n2y = ny * cp - n1z * sp, n2z = ny * sp + n1z * cp;
    const pt = pts[p + 9];
    let l = n1x * lx + n2y * ly + n2z * lz;
    const rim = Math.max(0, 1 - Math.abs(n2z)) ** 3 * 0.2;
    l = pt === part.eye ? 0.08 : pt === part.glint ? 1 : pt === part.ring ? 0.7 : Math.min(1, 0.22 + 0.78 * Math.max(0, l) ** 0.85 + rim);
    const cx = Math.round(ox + x1 * k);
    const cy = Math.round(perch - y2 * k * aspect);
    if (cx < 0 || cx >= cols || cy < 0 || cy >= rows) continue;
    const i = cy * cols + cx;
    // Each cell is the average of the points on the surface nearest the
    // viewer there (within band of the nearest), not the one nearest
    // point: neighbouring points carry neighbouring colours and normals,
    // and picking one of them made single cells speckle and shimmer as
    // she moved. The eyes sit clear of the band so they stay their own.
    const d = z2 + (pt === part.glint ? 0.08 : pt === part.eye ? 0.06 : pt === part.ring ? 0.03 : 0);
    if (d > depth[i] + band) {
      depth[i] = d;
      parts[i] = pt;
      sum[i * 4] = pts[p + 6];
      sum[i * 4 + 1] = pts[p + 7];
      sum[i * 4 + 2] = pts[p + 8];
      sum[i * 4 + 3] = l;
      count[i] = 1;
    } else if (d > depth[i] - band) {
      if (d > depth[i]) {
        depth[i] = d;
        parts[i] = pt;
      }
      sum[i * 4] += pts[p + 6];
      sum[i * 4 + 1] += pts[p + 7];
      sum[i * 4 + 2] += pts[p + 8];
      sum[i * 4 + 3] += l;
      count[i]++;
    }
  }
  for (let i = 0; i < cols * rows; i++) {
    if (!count[i]) continue;
    colours[i * 3] = sum[i * 4] / count[i];
    colours[i * 3 + 1] = sum[i * 4 + 1] / count[i];
    colours[i * 3 + 2] = sum[i * 4 + 2] / count[i];
    shade[i] = sum[i * 4 + 3] / count[i];
  }
  // Cells the samples missed between drawn ones are filled from them.
  for (let pass = 0; pass < 2; pass++)
    for (let j = 1; j < rows - 1; j++) {
      for (let i = 1; i < cols - 1; i++) {
        const q = j * cols + i;
        if (shade[q] >= 0) continue;
        const around = [q - 1, q + 1, q - cols, q + cols].filter((r) => shade[r] >= 0);
        if (around.length < 3) continue;
        shade[q] = around.reduce((a, r) => a + shade[r], 0) / around.length;
        const r0 = around[0];
        parts[q] = parts[r0];
        colours.copyWithin(q * 3, r0 * 3, r0 * 3 + 3);
      }
    }
  const edge = new Uint8Array(cols * rows);
  for (let j = 1; j < rows - 1; j++) {
    for (let i = 1; i < cols - 1; i++) {
      const q = j * cols + i;
      if (shade[q] >= 0 && (shade[q - 1] < 0 || shade[q + 1] < 0 || shade[q - cols] < 0 || shade[q + cols] < 0)) edge[q] = 1;
    }
  }
  // Her plumage, smoothed a touch once: each feather cell almost all
  // itself and a little the mean of its neighbours of the same part,
  // which takes the edge off a lone cell that disagrees with all around
  // it and keeps the photographs' texture. Her head, where her look is (brow,
  // moustache, eye), and the outline are left exactly as they are.
  const was = colours.slice(), wasShade = shade.slice();
  for (let j = 1; j < rows - 1; j++) {
    for (let i = 1; i < cols - 1; i++) {
      const q = j * cols + i, p = parts[q];
      if (wasShade[q] < 0 || edge[q] || p !== part.body && p !== part.tail) continue;
      let r = 0, g = 0, b = 0, v = 0, n = 0;
      for (const o of [q - 1, q + 1, q - cols, q + cols]) {
        if (wasShade[o] < 0 || parts[o] !== p) continue;
        r += was[o * 3];
        g += was[o * 3 + 1];
        b += was[o * 3 + 2];
        v += wasShade[o];
        n++;
      }
      if (!n) continue;
      colours[q * 3] = was[q * 3] * 0.85 + (r / n) * 0.15;
      colours[q * 3 + 1] = was[q * 3 + 1] * 0.85 + (g / n) * 0.15;
      colours[q * 3 + 2] = was[q * 3 + 2] * 0.85 + (b / n) * 0.15;
      shade[q] = wasShade[q] * 0.85 + (v / n) * 0.15;
    }
  }
  return { shade, colours, parts, edge, scale: k, left: ox };
}

// text is a frame as lines of characters, for a page without script.
export function text(f, cols, rows) {
  const lines = [];
  for (let j = 0; j < rows; j++) {
    let s = "";
    for (let i = 0; i < cols; i++) {
      const v = f.shade[j * cols + i];
      s += v < 0 ? " " : f.parts[j * cols + i] === part.eye ? "@" : ramp[Math.min(ramp.length - 1, 1 + Math.round(v * (ramp.length - 2)))];
    }
    lines.push(s.replace(/\s+$/, ""));
  }
  return lines.join("\n");
}
