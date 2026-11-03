import test from "node:test";
import assert from "node:assert/strict";
import { centroid, contourRings, inradiusAt, insetPolygon, isConvex, polygonArea, rect, roundedPath, voronoiTreemap } from "./voronoi.ts";

const inside = (poly, w, h) => poly.every(([x, y]) => x >= -1e-6 && x <= w + 1e-6 && y >= -1e-6 && y <= h + 1e-6);

test("cells tile the rectangle and follow their targets", () => {
  const targets = [24.3, 16.5, 7.4, 1.4, 0.4];
  const cells = voronoiTreemap(targets, 900, 560, 11);
  assert.equal(cells.length, targets.length);
  const sum = targets.reduce((a, b) => a + b, 0);
  let covered = 0;
  cells.forEach((c, i) => {
    covered += c.area;
    assert.ok(c.poly.length >= 3, `cell ${i} exists`);
    assert.ok(isConvex(c.poly), `cell ${i} is convex`);
    assert.ok(inside(c.poly, 900, 560), `cell ${i} stays inside the bounds`);
    const want = (targets[i] / sum) * 900 * 560;
    assert.ok(Math.abs(c.area - want) / want < 0.12, `cell ${i} area ${c.area.toFixed(0)} vs ${want.toFixed(0)}`);
  });
  assert.ok(Math.abs(covered - 900 * 560) < 1, "the cells cover the whole rectangle");
});

test("equal targets give near-equal cells, and layout is deterministic", () => {
  const eq = new Array(14).fill(1);
  const a = voronoiTreemap(eq, 1000, 700, 3);
  const b = voronoiTreemap(eq, 1000, 700, 3);
  const c = voronoiTreemap(eq, 1000, 700, 4);
  const mean = (1000 * 700) / 14;
  a.forEach((cell) => assert.ok(Math.abs(cell.area - mean) / mean < 0.1));
  assert.deepEqual(a.map((x) => x.poly), b.map((x) => x.poly), "same seed, same layout");
  assert.notDeepEqual(a.map((x) => x.poly), c.map((x) => x.poly), "a different seed changes the layout");
});

test("layout holds on a tall phone-shaped canvas", () => {
  const cells = voronoiTreemap(new Array(14).fill(1), 358, 520, 3);
  cells.forEach((c) => {
    assert.ok(c.poly.length >= 3 && isConvex(c.poly) && inside(c.poly, 358, 520));
    assert.ok(Math.abs(c.area - (358 * 520) / 14) / ((358 * 520) / 14) < 0.12);
  });
});

test("inset, rings, rounding and inradius behave", () => {
  const sq = rect(100, 100);
  const inner = insetPolygon(sq, 10);
  assert.ok(Math.abs(polygonArea(inner) - 80 * 80) < 1e-6);
  assert.deepEqual(insetPolygon(sq, 60), [], "over-insetting leaves nothing");
  const rings = contourRings(sq, 10, 20, 4);
  assert.ok(rings.length >= 3 && rings.length <= 5);
  for (let i = 1; i < rings.length; i++) assert.ok(polygonArea(rings[i]) < polygonArea(rings[i - 1]));
  assert.match(roundedPath(sq, 8), /^M[\d. ]+Q.*Z$/);
  assert.equal(roundedPath([[0, 0], [1, 1]], 4), "");
  assert.deepEqual(centroid(sq).map((v) => Math.round(v)), [50, 50]);
  assert.ok(Math.abs(inradiusAt(sq, [50, 50]) - 50) < 1e-6);
});
