// Pure derivations over the sanitized export. Nothing here invents evidence:
// every value is read from, or computed directly from, a field of DemoExport.
// This file is imported by node tests, so it uses relative type-only imports.
import type { DemoEval, DemoExport, DemoInvariant, DemoObservation, DemoStep } from "./demo.ts";

export type Column = "before" | "repair" | "unsafe";

export interface CompareRow {
  id: string;
  summary: string;
  before?: DemoInvariant;
  repair?: DemoInvariant;
  unsafe?: DemoInvariant;
  /** True when the three candidates do not all share one status. */
  differs: boolean;
}

export function compareInvariants(doc: DemoExport): CompareRow[] {
  const pick = (e: DemoEval, id: string) => e.invariants.find((i) => i.id === id);
  const ids: string[] = [];
  for (const e of [doc.pre_repair, doc.oracle, doc.unsafe]) {
    for (const i of e.invariants) if (!ids.includes(i.id)) ids.push(i.id);
  }
  ids.sort((a, b) => Number(a.slice(1)) - Number(b.slice(1)));
  return ids.map((id) => {
    const before = pick(doc.pre_repair, id);
    const repair = pick(doc.oracle, id);
    const unsafe = pick(doc.unsafe, id);
    const statuses = new Set([before?.status, repair?.status, unsafe?.status]);
    return {
      id,
      summary: (repair ?? before ?? unsafe)?.summary ?? "",
      before,
      repair,
      unsafe,
      differs: statuses.size > 1,
    };
  });
}

/**
 * How many fulfillment effects the oracle's own record shows for one payment.
 * Failing I13 carries the exact count; a passing run only states it in the
 * oracle's reason ("exactly one fulfillment effect ..."). Anything else is null
 * rather than a guess.
 */
export function effectCount(e: DemoEval): number | null {
  const i13 = e.invariants.find((i) => i.id === "I13");
  if (i13?.status === "FAIL") return i13.counterexample?.effect_count ?? null;
  if (i13?.status === "PASS" && /exactly one fulfillment effect/i.test(e.reason)) return 1;
  return null;
}

/** The oracle prints effects as "key (event)"; show the key once when both match. */
export function cleanEffect(raw: string): string {
  const m = raw.match(/^(\S+) \((\S+)\)$/);
  return m && m[1] === m[2] ? m[1] : raw;
}

export function failedIds(e: DemoEval): string[] {
  return e.invariants.filter((i) => i.status === "FAIL").map((i) => i.id);
}

export function firstSentence(text: string, max = 220): string {
  const clean = text.replace(/\s+/g, " ").trim();
  const match = clean.match(/^.*?[.!?](?=\s|$)/);
  const sentence = match ? match[0] : clean;
  if (sentence.length <= max) return sentence;
  const cut = sentence.slice(0, max);
  return `${cut.slice(0, Math.max(cut.lastIndexOf(" "), 40))}…`;
}

export interface OrderIdentities {
  incident: string;
  verification?: string;
  /** Only known from the unsafe oracle's own counterexample; never assumed. */
  unsafeRun?: string;
}

export function orderIdentities(doc: DemoExport): OrderIdentities {
  const unsafeRun = doc.unsafe.invariants.find((i) => i.counterexample?.order_id)?.counterexample?.order_id;
  return {
    incident: doc.incident.original_order_id ?? doc.incident.order_id,
    verification: doc.incident.verification_order_id,
    unsafeRun,
  };
}

/** Epoch nanoseconds from an RFC3339 string without losing sub-millisecond digits. */
export function epochNanos(at?: string): bigint | null {
  if (!at) return null;
  const match = at.match(/^(.*?)(?:\.(\d+))?Z$/);
  if (!match) return null;
  const seconds = Date.parse(`${match[1]}Z`);
  if (Number.isNaN(seconds)) return null;
  const frac = (match[2] ?? "").padEnd(9, "0").slice(0, 9);
  return BigInt(seconds) * BigInt(1000000) + BigInt(frac);
}

export interface CallNode {
  obs: DemoObservation;
  /** Microseconds since the first recorded call; null when timestamps are absent. */
  offsetUs: number | null;
  /** Microseconds since the previous call. */
  gapUs: number | null;
}

export function callTimeline(doc: DemoExport): CallNode[] {
  const list = doc.incident.observations;
  const base = epochNanos(list[0]?.at);
  let prev: bigint | null = base;
  return list.map((obs) => {
    const t = epochNanos(obs.at);
    const offsetUs = t !== null && base !== null ? Number((t - base) / BigInt(1000)) : null;
    const gapUs = t !== null && prev !== null ? Number((t - prev) / BigInt(1000)) : null;
    if (t !== null) prev = t;
    return { obs, offsetUs, gapUs };
  });
}

export function formatMicros(us: number | null): string {
  if (us === null) return "not recorded";
  if (us < 1000) return `${us} µs`;
  if (us < 1_000_000) return `${(us / 1000).toFixed(1)} ms`;
  return `${(us / 1_000_000).toFixed(2)} s`;
}

export interface GateStep extends DemoStep {
  /** Share of the longest step, 0..1, for a bar. */
  share: number;
}

export function gateSteps(steps: DemoStep[]): GateStep[] {
  const longest = Math.max(1, ...steps.map((s) => s.duration_ms));
  return steps.map((s) => ({ ...s, share: s.duration_ms / longest }));
}

export function humanGate(doc: DemoExport) {
  const h = doc.human_review;
  return {
    worktreeOnly: h.candidate_patch_applied_in_disposable_worktree && !h.developer_tree_applied,
    merged: h.developer_tree_applied,
    deployed: h.deployed,
    approvalRequired: h.human_approval_required,
  };
}

/** Annotation bound to an exact patch digest; ignored for any other patch bytes. */
export interface DiffNote {
  /** Substring of a diff line; the first line containing it is annotated. */
  match: string;
  note: string;
}

const ACCEPTED_DIGEST = "sha256:5625ca9426f661ea7c381cc9c9edcabaa66316d6f962b1522f545d91cc0534fe";
const UNSAFE_DIGEST = "sha256:27f7fde907eff7009132f2f9a7e43b3afbb460b1f91416bc970f8814bbcaa291";

// These notes only describe what the lines of the frozen recorded patches do.
// They make no claim about which line fixed which invariant; the oracle scored
// each candidate as a whole.
const NOTES: Record<string, DiffNote[]> = {
  [ACCEPTED_DIGEST]: [
    {
      match: "orderID, status, err := s.webhookOrder",
      note: "Keeps the order's current local status instead of discarding it.",
    },
    {
      match: 'case "ORDER.APPROVED":',
      note: "The ORDER.APPROVED case is removed, so this handler no longer writes an order status from a webhook event.",
    },
    {
      match: "if status != StatusCapturedVerified",
      note: "Fulfillment is skipped unless the local order is already CAPTURED_VERIFIED.",
    },
    {
      match: "ev.CaptureID != verifiedCapture",
      note: "Fulfillment is skipped unless the event names the order's verified provider capture.",
    },
    {
      match: "VALUES ($1::uuid, 'FULFILL', $2, $3)",
      note: "The effect key is the constant 'FULFILL' (it was the event id). The existing ON CONFLICT (order_id, effect_key) DO NOTHING now covers every event for the order.",
    },
  ],
  [UNSAFE_DIGEST]: [
    {
      match: "Seeded unsafe candidate",
      note: "The patch's own comment states what it does: it removes the status write and keeps a per-event fulfillment key.",
    },
    {
      match: "_ = orderID",
      note: "The status write is gone, but nothing in this diff changes the fulfillment key or adds a gate on capture.",
    },
  ],
};

export function diffNotes(digest?: string): DiffNote[] {
  return (digest && NOTES[digest]) || [];
}

export function verdictWord(verdict: string): "VERIFIED" | "UNSAFE" | "INCONCLUSIVE" {
  if (verdict === "VERIFIED_FOR_TESTED_SCENARIOS") return "VERIFIED";
  if (verdict === "UNSAFE") return "UNSAFE";
  return "INCONCLUSIVE";
}

export interface LedgerRow {
  key: string;
  title: string;
  value: string;
  state: "ok" | "fail";
  invariantIds: string[];
  facts: { label: string; value: string }[];
}

/**
 * The merchant ledger as the oracle read it after the run. Order and timing of
 * these rows are NOT recorded, so they are listed, never placed on a time axis.
 */
export function ledgerAutopsy(e: DemoEval): LedgerRow[] {
  const rows: LedgerRow[] = [];
  const inv = (id: string) => e.invariants.find((i) => i.id === id);
  if (e.provider_capture_id) {
    rows.push({
      key: "capture",
      title: "Provider capture",
      value: e.provider_capture_id,
      state: e.claimed_capture_id === e.provider_capture_id ? "ok" : "fail",
      invariantIds: inv("I6") ? ["I6"] : [],
      facts: [
        { label: "Merchant claimed", value: e.claimed_capture_id ?? "not recorded" },
        { label: "Provider reported", value: e.provider_capture_id },
      ],
    });
  }
  const i8 = inv("I8");
  rows.push({
    key: "status",
    title: "Final order status",
    value: e.order_status,
    state: i8?.status === "FAIL" || e.order_status !== "CAPTURED_VERIFIED" ? "fail" : "ok",
    invariantIds: i8 ? ["I8"] : [],
    facts: [
      { label: "Final status", value: e.order_status },
      ...(i8?.counterexample?.confirm_event_id !== undefined
        ? [{ label: "Journal confirmation event", value: `#${i8.counterexample.confirm_event_id}` }]
        : []),
    ],
  });
  const i13 = inv("I13");
  const i14 = inv("I14");
  (i13?.counterexample?.effects ?? []).forEach((raw, index) => {
    const id = raw.split(" ")[0];
    const flagged14 = i14?.status === "FAIL" && i14.counterexample?.event_id === id;
    rows.push({
      key: `effect-${index + 1}`,
      title: `Fulfillment effect ${index + 1}`,
      value: id,
      state: i13?.status === "FAIL" || flagged14 ? "fail" : "ok",
      invariantIds: ["I13", ...(flagged14 ? ["I14"] : [])],
      facts: [
        { label: "Effect key", value: cleanEffect(raw) },
        { label: "Effects for this payment", value: String(i13?.counterexample?.effect_count ?? "not recorded") },
      ],
    });
  });
  return rows;
}

// Short names for the map cells. The oracle's full title is always shown in the
// detail; these are presentation text keyed by the oracle's stable invariant id.
const SHORT_LABELS: Record<string, string> = {
  I1: "Integer minor units",
  I2: "Amount bound to order",
  I3: "Approval is not capture",
  I4: "Durable before the call",
  I5: "Ambiguity is not failure",
  I6: "Capture needs provider proof",
  I7: "One order, one effect",
  I8: "No silent rollback",
  I9: "Divergence is explicit",
  I10: "Refunds need proof",
  I11: "Unsafe never applies",
  I12: "Evidence provenance",
  I13: "One fulfillment per payment",
  I14: "Fulfillment needs capture proof",
};

export function shortLabel(inv: { id: string; title?: string; summary?: string }): string {
  return SHORT_LABELS[inv.id] ?? inv.title ?? inv.summary ?? inv.id;
}

export function totalStepMs(steps: DemoStep[]): number {
  return steps.reduce((sum, s) => sum + s.duration_ms, 0);
}

export function formatMs(ms: number): string {
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`;
}
