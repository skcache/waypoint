// A small Voronoi treemap: cells whose areas follow target weights, laid out as
// a power diagram inside a rectangle. Pure functions, no dependencies, so the
// geometry is testable and deterministic for a given seed.
export type Pt = [number, number];

export function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export function signedArea(p: Pt[]): number {
  let s = 0;
  for (let i = 0; i < p.length; i++) {
    const [x1, y1] = p[i];
    const [x2, y2] = p[(i + 1) % p.length];
    s += x1 * y2 - x2 * y1;
  }
  return s / 2;
}

export const polygonArea = (p: Pt[]): number => Math.abs(signedArea(p));

export function centroid(p: Pt[]): Pt {
  const a = signedArea(p);
  if (Math.abs(a) < 1e-9) {
    const n = Math.max(1, p.length);
    return [p.reduce((s, q) => s + q[0], 0) / n, p.reduce((s, q) => s + q[1], 0) / n];
  }
  let cx = 0;
  let cy = 0;
  for (let i = 0; i < p.length; i++) {
    const [x1, y1] = p[i];
    const [x2, y2] = p[(i + 1) % p.length];
    const f = x1 * y2 - x2 * y1;
    cx += (x1 + x2) * f;
    cy += (y1 + y2) * f;
  }
  return [cx / (6 * a), cy / (6 * a)];
}

/** Keep the part of a convex polygon where a*x + b*y <= c. */
export function clipHalfPlane(poly: Pt[], a: number, b: number, c: number): Pt[] {
  const out: Pt[] = [];
  for (let i = 0; i < poly.length; i++) {
    const p = poly[i];
    const q = poly[(i + 1) % poly.length];
    const dp = a * p[0] + b * p[1] - c;
    const dq = a * q[0] + b * q[1] - c;
    if (dp <= 0) out.push(p);
    if ((dp < 0 && dq > 0) || (dp > 0 && dq < 0)) {
      const t = dp / (dp - dq);
      out.push([p[0] + (q[0] - p[0]) * t, p[1] + (q[1] - p[1]) * t]);
    }
  }
  return out;
}

export function rect(w: number, h: number): Pt[] {
  return [[0, 0], [w, 0], [w, h], [0, h]];
}

/** Power diagram: |x-si|^2 - wi <= |x-sj|^2 - wj is a half-plane per pair. */
export function powerCells(sites: Pt[], weights: number[], bounds: Pt[]): Pt[][] {
  return sites.map((si, i) => {
    let poly = bounds;
    for (let j = 0; j < sites.length && poly.length > 2; j++) {
      if (j === i) continue;
      const sj = sites[j];
      const a = 2 * (sj[0] - si[0]);
      const b = 2 * (sj[1] - si[1]);
      const c = sj[0] ** 2 + sj[1] ** 2 - si[0] ** 2 - si[1] ** 2 - weights[j] + weights[i];
      poly = clipHalfPlane(poly, a, b, c);
    }
    return poly;
  });
}

export interface Cell {
  index: number;
  poly: Pt[];
  site: Pt;
  area: number;
}

/**
 * Lay out one cell per target weight inside width x height. Phase one moves the
 * sites toward cell centroids while adjusting weights; phase two keeps the
 * sites fixed and only refines weights, so shapes stay organic and areas land
 * on their targets.
 */
export function voronoiTreemap(targets: number[], width: number, height: number, seed = 7): Cell[] {
  const n = targets.length;
  const total = width * height;
  const sum = targets.reduce((s, t) => s + t, 0) || 1;
  const want = targets.map((t) => (t / sum) * total);
  const rand = mulberry32(seed);
  const bounds = rect(width, height);
  const cols = Math.ceil(Math.sqrt((n * width) / height));
  const rows = Math.ceil(n / cols);
  const sites: Pt[] = Array.from({ length: n }, (_, i) => {
    const cx = ((i % cols) + 0.5) / cols;
    const cy = (Math.floor(i / cols) + 0.5) / rows;
    return [(cx + (rand() - 0.5) * 0.5 / cols) * width, (cy + (rand() - 0.5) * 0.5 / rows) * height] as Pt;
  });
  const weights = new Array(n).fill(0);
  const avg = total / n;

  function step(moveSites: boolean, gain: number) {
    const cells = powerCells(sites, weights, bounds);
    cells.forEach((poly, i) => {
      const area = poly.length > 2 ? polygonArea(poly) : 0;
      if (area < 1e-6) {
        weights[i] += avg * 0.6;
        return;
      }
      weights[i] += gain * ((2 * (want[i] - area)) / Math.PI);
      if (moveSites) {
        const [cx, cy] = centroid(poly);
        sites[i] = [sites[i][0] + (cx - sites[i][0]) * 0.55, sites[i][1] + (cy - sites[i][1]) * 0.55];
      }
    });
  }
  for (let k = 0; k < 14; k++) step(true, 0.7);
  for (let k = 0; k < 90; k++) step(false, 0.8);
  const cells = powerCells(sites, weights, bounds);
  return cells.map((poly, index) => ({ index, poly, site: sites[index], area: poly.length > 2 ? polygonArea(poly) : 0 }));
}

function ccw(poly: Pt[]): Pt[] {
  return signedArea(poly) < 0 ? [...poly].reverse() : poly;
}

/** Shrink a convex polygon by d on every side. */
export function insetPolygon(poly: Pt[], d: number): Pt[] {
  const p = ccw(poly);
  let out = p;
  for (let i = 0; i < p.length && out.length > 2; i++) {
    const [x1, y1] = p[i];
    const [x2, y2] = p[(i + 1) % p.length];
    const ex = x2 - x1;
    const ey = y2 - y1;
    const len = Math.hypot(ex, ey) || 1;
    // For a counter-clockwise polygon the inward normal is (-ey, ex)/len.
    const nx = -ey / len;
    const ny = ex / len;
    // Keep points with n.(x - p1) >= d  <=>  -n.x <= -(n.p1 + d)
    out = clipHalfPlane(out, -nx, -ny, -(nx * x1 + ny * y1 + d));
  }
  return out.length > 2 ? out : [];
}

/** A closed SVG path through a convex polygon with every corner rounded. */
export function roundedPath(poly: Pt[], radius: number): string {
  const n = poly.length;
  if (n < 3) return "";
  const parts: string[] = [];
  for (let i = 0; i < n; i++) {
    const prev = poly[(i + n - 1) % n];
    const cur = poly[i];
    const next = poly[(i + 1) % n];
    const toPrev = Math.hypot(prev[0] - cur[0], prev[1] - cur[1]) || 1;
    const toNext = Math.hypot(next[0] - cur[0], next[1] - cur[1]) || 1;
    const r = Math.min(radius, toPrev * 0.48, toNext * 0.48);
    const a: Pt = [cur[0] + ((prev[0] - cur[0]) / toPrev) * r, cur[1] + ((prev[1] - cur[1]) / toPrev) * r];
    const b: Pt = [cur[0] + ((next[0] - cur[0]) / toNext) * r, cur[1] + ((next[1] - cur[1]) / toNext) * r];
    parts.push(`${i === 0 ? "M" : "L"}${a[0].toFixed(1)} ${a[1].toFixed(1)}Q${cur[0].toFixed(1)} ${cur[1].toFixed(1)} ${b[0].toFixed(1)} ${b[1].toFixed(1)}`);
  }
  return `${parts.join("")}Z`;
}

/** Concentric contour rings, outermost first, until the cell is too small. */
export function contourRings(poly: Pt[], step: number, maxRings = 8, minArea = 40): Pt[][] {
  const rings: Pt[][] = [];
  for (let k = 1; k <= maxRings; k++) {
    const ring = insetPolygon(poly, step * k);
    if (ring.length < 3 || polygonArea(ring) < minArea) break;
    rings.push(ring);
  }
  return rings;
}

/** Radius of the largest circle centred at c that fits inside a convex polygon. */
export function inradiusAt(poly: Pt[], c: Pt): number {
  const p = ccw(poly);
  let best = Infinity;
  for (let i = 0; i < p.length; i++) {
    const [x1, y1] = p[i];
    const [x2, y2] = p[(i + 1) % p.length];
    const ex = x2 - x1;
    const ey = y2 - y1;
    const len = Math.hypot(ex, ey) || 1;
    best = Math.min(best, Math.abs((c[0] - x1) * ey - (c[1] - y1) * ex) / len);
  }
  return best === Infinity ? 0 : best;
}

export function isConvex(poly: Pt[]): boolean {
  let sign = 0;
  for (let i = 0; i < poly.length; i++) {
    const a = poly[i];
    const b = poly[(i + 1) % poly.length];
    const c = poly[(i + 2) % poly.length];
    const cross = (b[0] - a[0]) * (c[1] - b[1]) - (b[1] - a[1]) * (c[0] - b[0]);
    if (Math.abs(cross) < 1e-7) continue;
    const s = cross > 0 ? 1 : -1;
    if (sign === 0) sign = s;
    else if (s !== sign) return false;
  }
  return true;
}
