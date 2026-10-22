import raw from "@/data/m5.json";

export type RunStatus =
  | "REPAIRED"
  | "SAFE_REFUSAL"
  | "REJECTED"
  | "ERROR"
  | "DIAGNOSIS_ONLY"
  | "NOT_CONFIGURED";

export interface Invariant {
  id: string;
  title?: string;
  status: string;
  summary: string;
  counterexample?: unknown;
}

export interface EvalDoc {
  accepted?: boolean;
  reason?: string;
  order_status?: string;
  claimed_capture_id?: string;
  provider_capture_id?: string;
  oracle?: { verdict?: string; invariants?: Invariant[] };
}

export interface Observation {
  Seq?: number;
  At?: string;
  Method?: string;
  OrderRef?: string;
  RequestID?: string;
  DurableBeforeCall?: boolean;
  Applied?: boolean;
  Replayed?: boolean;
  ResponseDropped?: boolean;
  DefiniteRefusal?: boolean;
  OrderStatus?: string;
  CaptureID?: string;
  CaptureStatus?: string;
  SentAmount?: string;
  SentCurrency?: string;
  Tier?: string;
}

export interface Diagnosis {
  root_cause?: string;
  mechanism?: string;
  suspected_files?: string[];
  supporting_evidence?: string[];
  invariants_at_risk?: string[];
  proposed_fix?: string;
  confidence_reasons?: string[];
  uncertainties?: string[];
}

export interface Attempt {
  number: number;
  ladder?: Record<string, boolean | string>;
  diff?: string;
  eval?: {
    accepted?: boolean;
    reason?: string;
    order_status?: string;
    oracle_verdict?: string;
  };
  error?: string;
  model_calls?: { purpose: string; model: string; latency_ms: number; prompt_tokens?: number; completion_tokens?: number }[];
}

export interface Report {
  status?: string;
  mode?: string;
  case_id?: string;
  base_revision?: string;
  diagnosis?: Diagnosis;
  attempts?: Attempt[];
  final_ladder?: Record<string, boolean | string>;
  prompt_tokens?: number;
  completion_tokens?: number;
  limitations?: string[];
}

export interface Run {
  case_id: string;
  condition: string;
  status: RunStatus;
  accepted: boolean;
  diagnosis_category: string;
  diagnosis_correct: boolean;
  patch_applied: boolean;
  build_passed: boolean;
  tests_passed: boolean;
  oracle_verdict: string;
  attempts: number;
  latency_ms: number;
  prompt_tokens: number;
  completion_tokens: number;
  estimated_usd?: number;
  error?: string;
  artifacts: string;
  report?: Report;
  diff?: string;
  pre_eval?: EvalDoc;
  eval?: EvalDoc;
  diagnosis_reply?: string;
  patch_reply?: string;
}

export interface CaseMeta {
  ID: string;
  Family: string;
  Title: string;
  Mode: string;
  Exposed: boolean;
}

export interface CaseRow {
  case: CaseMeta;
  mutation_sha: string;
  pre_repair: {
    accepted: boolean;
    reason: string;
    order_status: string;
    verdict: string;
    failing?: string[];
  };
  runs: Record<string, Run>;
}

export interface Totals {
  held_out_cases: number;
  safe_accepted: number;
  verified_accepted: number;
  safe_refusals: number;
  rejected: number;
  errors: number;
  false_acceptances: number;
  prompt_tokens: number;
  completion_tokens: number;
  estimated_usd?: number;
  by_condition: Record<string, {
    safe_accepted: number;
    verified_accepted: number;
    safe_refusals: number;
    rejected: number;
    errors: number;
    not_run: number;
    false_acceptances: number;
    prompt_tokens: number;
    completion_tokens: number;
    estimated_usd?: number;
  }>;
}

export interface Dataset {
  benchmark_id: string;
  digest: string;
  frozen_at: string;
  base_sha: string;
  model: { id?: string; base_url?: string; max_output_tokens?: number; rate_table?: string };
  conditions: { id: string; description: string; max_attempts: number; oracle_feedback?: boolean }[];
  cases: CaseRow[];
  totals: Totals;
  notes: string[];
  views: Record<string, Record<string, Run & { observations?: { order_id?: string; observations?: Observation[] } }>>;
}

export const data = raw as unknown as Dataset;

export function caseById(id: string): CaseRow | undefined {
  return data.cases.find((c) => c.case.ID === id);
}

export function runFor(id: string, condition: string): Run | undefined {
  const view = data.views[id]?.[condition];
  if (view) return view;
  return caseById(id)?.runs[condition];
}

export function allRuns(c: CaseRow): Run[] {
  return Object.values(c.runs);
}

export function statusTone(status: string): string {
  switch (status) {
    case "REPAIRED":
      return "text-ok";
    case "SAFE_REFUSAL":
      return "text-accent";
    case "REJECTED":
      return "text-bad";
    case "ERROR":
      return "text-bad";
    default:
      return "text-muted";
  }
}

export function statusLabel(status: string): string {
  switch (status) {
    case "REPAIRED":
      return "Repaired";
    case "SAFE_REFUSAL":
      return "Safe refusal";
    case "REJECTED":
      return "Rejected";
    case "ERROR":
      return "Error";
    case "DIAGNOSIS_ONLY":
      return "Diagnosis only";
    case "NOT_CONFIGURED":
      return "Not configured";
    default:
      return status;
  }
}

export function shortHash(v?: string): string {
  if (!v) return "—";
  return v.length > 12 ? v.slice(0, 12) : v;
}

export function fmtMs(ms: number): string {
  if (!ms) return "—";
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

export function fmtTokens(n: number): string {
  return n > 0 ? n.toLocaleString("en-US") : "—";
}

export function usd(n?: number): string {
  if (n === undefined || n === null) return "not reported";
  return `$${n.toFixed(4)}`;
}

export function observations(id: string, condition: string): Observation[] {
  const view = data.views[id]?.[condition];
  return view?.observations?.observations ?? [];
}
