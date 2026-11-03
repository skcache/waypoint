// A strip treemap: it keeps the input order (so the board reads I1 to I14) and
// still fills the container exactly. Area is proportional to weight.

export interface Rect {
  /** Fractions of the container, 0 to 1. */
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Lay weights out in reading order as full-width rows of a container with width/height = aspect. */
export function stripLayout(weights: number[], aspect: number): Rect[] {
  const n = weights.length;
  const total = weights.reduce((a, b) => a + b, 0);
  if (!n || total <= 0 || !(aspect > 0)) return [];
  const rects: Rect[] = [];
  let y = 0;
  let i = 0;
  while (i < n) {
    const remainingWeight = weights.slice(i).reduce((a, b) => a + b, 0);
    const scale = (aspect * (1 - y)) / remainingWeight; // area per unit of weight
    const worst = (sum: number, upto: number) => {
      const h = (sum * scale) / aspect;
      let r = 0;
      for (let k = i; k <= upto; k++) {
        const w = (weights[k] * scale) / h;
        r = Math.max(r, w / h, h / w);
      }
      return r;
    };
    let j = i + 1;
    let rowSum = weights[i];
    let best = worst(rowSum, i);
    while (j < n) {
      const next = worst(rowSum + weights[j], j);
      if (next > best) break;
      rowSum += weights[j];
      best = next;
      j++;
    }
    const h = (rowSum * scale) / aspect;
    let x = 0;
    for (let k = i; k < j; k++) {
      const w = weights[k] / rowSum;
      rects.push({ x, y, w, h });
      x += w;
    }
    y += h;
    i = j;
  }
  return rects;
}
