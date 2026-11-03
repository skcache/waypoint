import test from "node:test";
import assert from "node:assert/strict";
import { stripLayout } from "./treemap.ts";

const area = (r) => r.w * r.h;

for (const aspect of [0.7, 1, 1.6, 2.4]) {
  test(`strip layout fills the container exactly at aspect ${aspect}`, () => {
    const weights = [1, 1, 1, 1, 1, 1, 1, 4, 1, 1, 1, 1, 4, 4];
    const rects = stripLayout(weights, aspect);
    assert.equal(rects.length, weights.length);
    const total = weights.reduce((a, b) => a + b, 0);
    assert.ok(Math.abs(rects.reduce((s, r) => s + area(r), 0) - 1) < 1e-9, "areas sum to the container");
    rects.forEach((r, i) => assert.ok(Math.abs(area(r) - weights[i] / total) < 1e-9, "area is proportional to weight"));
    rects.forEach((r) => assert.ok(r.x >= -1e-9 && r.y >= -1e-9 && r.x + r.w <= 1 + 1e-9 && r.y + r.h <= 1 + 1e-9, "inside the container"));
    for (let i = 0; i < rects.length; i++) {
      for (let j = i + 1; j < rects.length; j++) {
        const a = rects[i];
        const b = rects[j];
        const overlap = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x) > 1e-9 && Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y) > 1e-9;
        assert.ok(!overlap, `tiles ${i} and ${j} do not overlap`);
      }
    }
  });
}

test("strip layout keeps reading order and degrades safely", () => {
  const rects = stripLayout(Array(14).fill(1), 1.6);
  for (let i = 1; i < rects.length; i++) {
    const a = rects[i - 1];
    const b = rects[i];
    assert.ok(b.y > a.y + 1e-9 || (Math.abs(b.y - a.y) < 1e-9 && b.x > a.x), "row-major order");
  }
  assert.deepEqual(stripLayout([], 1), []);
  assert.deepEqual(stripLayout([1, 2], 0), []);
});
