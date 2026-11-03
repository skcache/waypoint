import type { DemoExport, DemoInvariant, DemoObservation } from "@/lib/demo";

export type StageId =
  | "incident"
  | "diagnosis"
  | "patch"
  | "verification"
  | "oracle"
  | "review";

export type CandidateMode = "verified" | "unsafe";

export const STAGES: {
  id: StageId;
  label: string;
  short: string;
}[] = [
  { id: "incident", label: "Incident", short: "Failure" },
  { id: "diagnosis", label: "Diagnosis", short: "Model" },
  { id: "patch", label: "Patch", short: "Source" },
  { id: "verification", label: "Execution", short: "Sandbox" },
  { id: "oracle", label: "Oracle", short: "Money" },
  { id: "review", label: "Review", short: "Human" },
];

export const FAULT_TITLES: Record<string, string> = {
  F1: "Ambiguous capture",
  F2: "Dropped capture response",
  F3: "Duplicate fulfillment",
  F4: "Currency mismatch",
  F5: "Pending capture",
  F6: "Early fulfillment",
};

const STATUS_LABELS: Record<string, string> = {
  VERIFIED_FOR_TESTED_SCENARIOS: "Verified for tested scenarios",
  CAPTURED_VERIFIED: "Captured and verified",
  ACCEPTED_FOR_HUMAN_REVIEW: "Accepted for human review",
  APPROVED: "Approved",
  CREATED: "Created",
  COMPLETED: "Completed",
  UNSAFE: "Unsafe",
};

export function humanStatus(value?: string): string {
  if (!value) return "not recorded";
  if (STATUS_LABELS[value]) return STATUS_LABELS[value];
  return value.toLowerCase().replace(/_/g, " ");
}

export function faultTitle(fault: string): string {
  return FAULT_TITLES[fault] ?? `Payment incident ${fault}`;
}

export function runKindLabel(kind?: string): string {
  if (kind === "recorded") return "recorded";
  if (kind === "replay") return "replay";
  if (kind === "live") return "live";
  return "unknown";
}

export function sourceLabel(doc: DemoExport, source: "recorded" | "local"): string {
  if (source === "recorded") return "recorded evidence";
  if (doc.run.kind === "live") return "local live run";
  if (doc.run.kind === "replay") return "local replay";
  return "local recorded run";
}

export function modelLabel(doc: DemoExport): string {
  if (doc.run.kind === "live") return "Hosted model";
  if (doc.run.kind === "replay") return "Replayed model";
  return "Recorded model";
}

export function modelNote(doc: DemoExport): string {
  if (doc.run.kind === "replay") {
    return "Replayed recorded model output. No hosted model is called for this page.";
  }
  if (doc.run.kind === "live") {
    return "Hosted-model output captured for this run. No model call is running for this page.";
  }
  return "Recorded hosted-model reply. No model call is running for this page.";
}

export function tierNote(doc: DemoExport): string {
  if (doc.run.evidence_tier === "F") {
    return "Tier F: deterministic fixture evidence. No live PayPal call.";
  }
  if (doc.run.evidence_tier === "R") {
    return "Tier R: real provider evidence recorded by this run.";
  }
  return `Tier ${doc.run.evidence_tier || "unknown"}: evidence tier is recorded per observation.`;
}

export function observationTierLabel(doc: DemoExport): string {
  const tiers = Array.from(new Set((doc.incident.observations ?? []).map((item) => item.tier).filter(Boolean)));
  return tiers.length ? tiers.join(" / ") : "unknown";
}

export function shortId(value?: string, head = 8, tail = 4): string {
  if (!value) return "not exported";
  if (value.length <= head + tail + 1) return value;
  return `${value.slice(0, head)}…${value.slice(-tail)}`;
}

export function money(minor: number, currency: string): string {
  return `${currency} ${(minor / 100).toFixed(2)}`;
}

export function utcTimestamp(value?: string): string {
  if (!value) return "not recorded";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("en-US", {
    timeZone: "UTC",
    month: "short",
    day: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }) + " UTC";
}

export function shortTime(value?: string): string {
  if (!value) return "not recorded";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString("en-US", {
    timeZone: "UTC",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

export function titleFor(doc: DemoExport): string {
  return faultTitle(doc.incident.fault);
}

export function verificationOrder(doc: DemoExport): string | undefined {
  return doc.incident.verification_order_id;
}

export function originalOrder(doc: DemoExport): string {
  return doc.incident.original_order_id ?? doc.incident.order_id;
}

export function captureObservation(doc: DemoExport): DemoObservation | undefined {
  return doc.incident.observations.find((o) => Boolean(o.capture_id));
}

export function failedInvariants(items?: DemoInvariant[]): DemoInvariant[] {
  return (items ?? []).filter((item) => item.status !== "PASS" && item.status !== "NOT_APPLICABLE");
}

export function stageMeta(
  doc: DemoExport,
  stage: StageId,
  candidate: CandidateMode = "verified",
): string {
  switch (stage) {
    case "incident":
      return `${doc.incident.observations.length} provider calls`;
    case "diagnosis":
      return runKindLabel(doc.run.kind);
    case "patch":
      return `${doc.patch.changed_files.length} file${doc.patch.changed_files.length === 1 ? "" : "s"}`;
    case "verification": {
      const passed = doc.verification.steps.filter((step) => step.passed).length;
      return `${passed}/${doc.verification.steps.length} checks`;
    }
    case "oracle": {
      if (candidate === "unsafe") {
        const failures = failedInvariants(doc.unsafe.invariants).length;
        return `${failures} counterexample${failures === 1 ? "" : "s"}`;
      }
      return `${doc.oracle.invariants.length} invariants`;
    }
    case "review":
      return "required";
  }
}
