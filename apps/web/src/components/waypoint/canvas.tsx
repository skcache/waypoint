"use client";

import { centroid, contourRings, inradiusAt, insetPolygon, roundedPath, voronoiTreemap, type Cell, type Pt } from "@/lib/voronoi";

export interface Shape {
  index: number;
  path: string;
  rings: string[];
  anchor: Pt;
  radius: number;
  area: number;
}

/** Turn layout cells into soft-cornered blobs with concentric contour rings. */
export function shapesFor(cells: Cell[], gap = 4, corner = 26, ringStep = 9): Shape[] {
  return cells.map((c) => {
    const inner = insetPolygon(c.poly, gap);
    const anchor = centroid(inner.length ? inner : c.poly);
    return {
      index: c.index,
      path: roundedPath(inner, corner),
      rings: contourRings(inner, ringStep, 8).map((r) => roundedPath(r, Math.max(8, corner - 6))),
      anchor,
      radius: inradiusAt(inner.length ? inner : c.poly, anchor),
      area: c.area,
    };
  });
}

export function layoutShapes(targets: number[], w: number, h: number, seed: number): Shape[] {
  if (w < 40 || h < 40) return [];
  return shapesFor(voronoiTreemap(targets, w, h, seed));
}

/** Wrap text into lines of at most `max` characters, up to `lines` lines. */
export function wrapLabel(text: string, max: number, lines: number): string[] {
  const words = text.split(/\s+/);
  const out: string[] = [];
  let cur = "";
  for (const word of words) {
    if (!cur) cur = word;
    else if ((cur + " " + word).length <= max) cur += " " + word;
    else {
      out.push(cur);
      cur = word;
    }
  }
  if (cur) out.push(cur);
  if (out.length > lines) {
    const kept = out.slice(0, lines);
    kept[lines - 1] = kept[lines - 1].replace(/[\s,.;:]+$/, "") + "…";
    return kept;
  }
  return out;
}
