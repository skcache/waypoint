// Evidence-derivation tests. They run against the committed recorded export, so
// they fail if the export changes in a way the UI would misread.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  callTimeline, compareInvariants, diffNotes, effectCount, epochNanos, failedIds, firstSentence,
  formatMicros, gateSteps, humanGate, orderIdentities, verdictWord,
} from "./evidence.ts";
import { diffStats, parseDiff, tokenizeLine } from "./diff.ts";

const doc = JSON.parse(readFileSync(new URL("../../public/demo/recorded.json", import.meta.url), "utf8"));

test("three candidates share one invariant set and the differing rows are I8, I13, I14", () => {
  const rows = compareInvariants(doc);
  assert.equal(rows.length, 14);
  assert.deepEqual(rows.map((r) => r.id), Array.from({ length: 14 }, (_, i) => `I${i + 1}`));
  assert.deepEqual(rows.filter((r) => r.differs).map((r) => r.id), ["I8", "I13", "I14"]);
  const i13 = rows.find((r) => r.id === "I13");
  assert.deepEqual([i13.before.status, i13.repair.status, i13.unsafe.status], ["FAIL", "PASS", "FAIL"]);
  const i8 = rows.find((r) => r.id === "I8");
  assert.deepEqual([i8.before.status, i8.repair.status, i8.unsafe.status], ["FAIL", "PASS", "PASS"]);
});

test("effect counts come only from the oracle's own record", () => {
  assert.equal(effectCount(doc.pre_repair), 2);
  assert.equal(effectCount(doc.unsafe), 2);
  assert.equal(effectCount(doc.oracle), 1);
  const noReason = structuredClone(doc.oracle);
  noReason.reason = "verified";
  assert.equal(effectCount(noReason), null, "a pass with no stated count is not guessed");
  assert.deepEqual(failedIds(doc.pre_repair), ["I8", "I13", "I14"]);
  assert.deepEqual(failedIds(doc.unsafe), ["I13"]);
});

test("original, verification and unsafe-run orders stay three distinct identities", () => {
  const ids = orderIdentities(doc);
  assert.ok(ids.incident && ids.verification && ids.unsafeRun);
  assert.equal(new Set([ids.incident, ids.verification, ids.unsafeRun]).size, 3);
  assert.equal(ids.incident, doc.incident.original_order_id);
  assert.equal(ids.verification, doc.incident.verification_order_id);
});

test("call timeline keeps sub-millisecond gaps and never reorders", () => {
  const nodes = callTimeline(doc);
  assert.equal(nodes.length, doc.incident.observations.length);
  assert.equal(nodes[0].offsetUs, 0);
  for (let i = 1; i < nodes.length; i++) assert.ok(nodes[i].offsetUs >= nodes[i - 1].offsetUs);
  assert.equal(epochNanos("2026-10-22T02:23:20.038029833Z") - epochNanos("2026-10-22T02:23:20.038Z"), 29833n);
  assert.equal(formatMicros(3563), "3.6 ms");
  assert.equal(formatMicros(null), "not recorded");
  assert.equal(callTimeline({ incident: { observations: [{ seq: 1, method: "x" }] } })[0].offsetUs, null);
});

test("gate steps scale to the longest real duration", () => {
  const steps = gateSteps(doc.verification.steps);
  assert.equal(Math.max(...steps.map((s) => s.share)), 1);
  assert.ok(steps.every((s) => s.share > 0 && s.share <= 1));
});

test("human gate reflects the export, not a constant", () => {
  assert.deepEqual(humanGate(doc), { worktreeOnly: true, merged: false, deployed: false, approvalRequired: true });
  const merged = structuredClone(doc);
  merged.human_review.deployed = true;
  merged.human_review.developer_tree_applied = true;
  const g = humanGate(merged);
  assert.equal(g.deployed, true);
  assert.equal(g.worktreeOnly, false);
});

test("diff annotations bind to digests and every note matches a real diff line", () => {
  for (const [label, patch, digest] of [
    ["accepted", doc.patch.diff, doc.patch.digest],
    ["unsafe", doc.unsafe.diff, doc.unsafe.diff_digest],
  ]) {
    const notes = diffNotes(digest);
    assert.ok(notes.length > 0, `${label} has notes`);
    const { lines } = parseDiff(patch);
    for (const n of notes) {
      assert.ok(lines.some((l) => l.text.includes(n.match)), `${label}: no line matches ${n.match}`);
    }
  }
  assert.deepEqual(diffNotes("sha256:other"), [], "unknown patches get no annotations");
  assert.deepEqual(diffNotes(undefined), []);
});

test("diff parser numbers lines and counts changes", () => {
  const { lines, hunks } = parseDiff(doc.patch.diff);
  assert.ok(hunks.length >= 2);
  assert.ok(hunks[0].context.includes("legacyApplyWebhookEvent"));
  const stats = diffStats(doc.patch.diff);
  assert.equal(stats.added, lines.filter((l) => l.kind === "add").length);
  assert.equal(stats.removed, lines.filter((l) => l.kind === "del").length);
  assert.ok(lines.filter((l) => l.kind === "add").every((l) => typeof l.newNo === "number"));
  assert.ok(lines.filter((l) => l.kind === "del").every((l) => typeof l.oldNo === "number"));
});

test("tokenizer highlights keywords, strings, comments and SQL", () => {
  const kinds = (s) => tokenizeLine(s).filter((t) => t.kind !== "plain").map((t) => `${t.kind}:${t.text}`);
  assert.deepEqual(kinds("if x != nil { // gate"), ["keyword:if", "keyword:nil", "comment:// gate"]);
  assert.ok(kinds('return fmt.Errorf("bad")').includes('string:"bad"'));
  assert.ok(kinds("VALUES ($1::uuid, 'FULFILL', $2, $3)").includes("sql:VALUES"));
  assert.equal(tokenizeLine("a := 1").map((t) => t.text).join(""), "a := 1", "tokens round-trip the source");
});

test("prose helpers and verdict wording", () => {
  assert.equal(firstSentence("First one. Second one."), "First one.");
  assert.ok(firstSentence("x".repeat(400), 100).endsWith("…"));
  assert.equal(verdictWord("VERIFIED_FOR_TESTED_SCENARIOS"), "VERIFIED");
  assert.equal(verdictWord("UNSAFE"), "UNSAFE");
  assert.equal(verdictWord("INCONCLUSIVE"), "INCONCLUSIVE");
});

test("ledger autopsy lists what the oracle read, with no invented timing", async () => {
  const { ledgerAutopsy } = await import("./evidence.ts");
  const rows = ledgerAutopsy(doc.pre_repair);
  assert.deepEqual(rows.map((r) => r.key), ["capture", "status", "effect-1", "effect-2"]);
  assert.equal(rows[0].state, "ok");
  assert.equal(rows[1].state, "fail");
  assert.ok(rows[1].facts.some((f) => f.value === "#5"));
  assert.deepEqual(rows[2].invariantIds, ["I13", "I14"], "evt-cap-1 is the I14 counterexample");
  assert.deepEqual(rows[3].invariantIds, ["I13"]);
  assert.ok(rows.every((r) => !("at" in r)), "rows carry no timestamps");
  const repaired = ledgerAutopsy(doc.oracle);
  assert.ok(repaired.every((r) => r.state === "ok"));
  assert.equal(repaired.filter((r) => r.key.startsWith("effect")).length, 0, "no effects are invented for a passing run");
});

test("short ids keep a head only when the tail is zero", async () => {
  const { shortId } = await import("./showcase.ts");
  assert.equal(shortId("7cd25ae2c689685f28c6f395ab55aecc9d19521c", 10, 0), "7cd25ae2c6…");
  assert.equal(shortId("b602689d-a9bb-455c-8b3e-0124df8e613d", 8, 4), "b602689d…613d");
  assert.equal(shortId("short", 8, 4), "short");
  assert.equal(shortId(undefined), "not exported");
});

test("effect keys are shown once when key and event match", async () => {
  const { cleanEffect } = await import("./evidence.ts");
  assert.equal(cleanEffect("evt-cap-1 (evt-cap-1)"), "evt-cap-1");
  assert.equal(cleanEffect("FULFILL (evt-cap-9)"), "FULFILL (evt-cap-9)");
  assert.equal(cleanEffect("plain"), "plain");
});

test("every invariant has the oracle's title, and totals come from the export", async () => {
  const { totalStepMs, formatMs } = await import("./evidence.ts");
  for (const e of [doc.pre_repair, doc.oracle, doc.unsafe]) {
    for (const inv of e.invariants) assert.ok(inv.title && inv.title.length > 8, `${inv.id} has a title`);
  }
  assert.equal(totalStepMs(doc.verification.steps), doc.verification.steps.reduce((a, s) => a + s.duration_ms, 0));
  assert.equal(formatMs(363), "363 ms");
  assert.equal(formatMs(24310), "24.3 s");
});

test("looks fixed, is not: the repair and the unsafe patch share a surface and differ in the ledger", async () => {
  const { receiptModel, surfaceOf } = await import("./evidence.ts");
  const before = receiptModel(doc, "before");
  const repair = receiptModel(doc, "repair");
  const unsafe = receiptModel(doc, "unsafe");
  assert.deepEqual(surfaceOf(repair), surfaceOf(unsafe), "a glance cannot tell the repair from the unsafe patch");
  assert.equal(surfaceOf(repair).status, "CAPTURED_VERIFIED");
  assert.notEqual(surfaceOf(before).status, "CAPTURED_VERIFIED", "the original's status visibly rolled back");
  assert.deepEqual([before.fulfilled, repair.fulfilled, unsafe.fulfilled], [2, 1, 2]);
  assert.deepEqual([before.balanced, repair.balanced, unsafe.balanced], [false, true, false]);
  assert.deepEqual(before.failed, ["I8", "I13", "I14"]);
  assert.deepEqual(unsafe.failed, ["I13"]);
  assert.deepEqual(repair.failed, []);
  assert.deepEqual(before.effects.map((e) => e.key), ["evt-cap-1", "evt-cap-2"]);
  assert.deepEqual(repair.effects, [{ key: undefined, flagged: false }], "no effect key is invented for a passing run");
  assert.deepEqual([before.captured, repair.captured, unsafe.captured], [1, 1, 1], "one payment in every candidate");
  assert.equal(before.statusFlagged, true);
  assert.equal(unsafe.statusFlagged, false);
  assert.equal(new Set([before.orderId, repair.orderId, unsafe.orderId]).size, 3, "three distinct order identities");
  assert.equal(before.ticks.length, 14);
});
