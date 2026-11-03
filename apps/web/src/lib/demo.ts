export interface DemoModelCall {
  purpose: string;
  model: string;
  prompt_tokens?: number;
  completion_tokens?: number;
  latency_ms?: number;
}

export interface DemoRunMeta {
  kind: "recorded" | "replay" | "live";
  mode: string;
  model_mode: string;
  model: string;
  model_calls: DemoModelCall[];
  prompt_tokens: number;
  completion_tokens: number;
  usage_source: string;
  run_id: string;
  base_revision: string;
  revision_source: string;
  evidence_tier: string;
  evidence_layout: "attempt" | "legacy_recorded_root" | string;
  finished_at: string;
  model_recorded_at?: string;
  source: string;
  source_label: string;
}

export interface DemoObservation {
  seq: number;
  at?: string;
  method: string;
  order_ref?: string;
  request_id?: string;
  request_id_durable_before_call?: boolean;
  sent_amount?: string;
  sent_currency?: string;
  mutation_applied?: boolean;
  idempotent_replay?: boolean;
  response_dropped?: boolean;
  definite_refusal?: boolean;
  order_status?: string;
  capture_id?: string;
  capture_status?: string;
  capture_amount?: string;
  capture_currency?: string;
  stale_read?: boolean;
  source?: string;
  tier?: string;
}

export interface DemoIncident {
  id: string;
  fault: string;
  order_id: string;
  original_order_id?: string;
  verification_order_id?: string;
  order_status: string;
  evidence_tier: string;
  total_minor: number;
  currency: string;
  checkout_error?: string;
  capture_error?: string;
  observations: DemoObservation[];
}

export interface DemoCounterexample {
  order_id?: string;
  final_status?: string;
  confirm_event_id?: number;
  effect_count?: number;
  effects?: string[];
  event_id?: string;
}

export interface DemoInvariant {
  id: string;
  /** The oracle's own name for the invariant. */
  title?: string;
  status: string;
  summary: string;
  counterexample?: DemoCounterexample;
}

export interface DemoEval {
  accepted: boolean;
  reason: string;
  order_status: string;
  claimed_capture_id?: string;
  provider_capture_id?: string;
  verdict: string;
  evaluated_at?: string;
  invariants: DemoInvariant[];
}

export interface DemoDiagnosis {
  root_cause: string;
  mechanism: string;
  proposed_fix: string;
  suspected_files: string[];
  supporting_evidence: string[];
  invariants_at_risk: string[];
  uncertainties: string[];
}

export interface DemoStep {
  name: string;
  exit_code: number;
  timed_out: boolean;
  duration_ms: number;
  passed: boolean;
}

export interface DemoExport {
  schema_version: number;
  run: DemoRunMeta;
  incident: DemoIncident;
  pre_repair: DemoEval;
  diagnosis?: DemoDiagnosis;
  patch: { summary: string; changed_files: string[]; digest: string; diff: string };
  verification: {
    ladder: {
      diagnosis_produced: boolean;
      patch_generated: boolean;
      patch_applied: boolean;
      build_passed: boolean;
      tests_passed: boolean;
      oracle_verdict: string;
      accepted: boolean;
    };
    steps: DemoStep[];
    sandbox?: string;
  };
  oracle: DemoEval;
  unsafe: DemoEval & {
    kind: string;
    source: string;
    provenance: string;
    fresh_trusted_gate: { executed: boolean; result: string; detail?: string };
    /** The seeded candidate's own patch, when the recorded evaluation has one beside it. */
    diff?: string;
    diff_digest?: string;
    changed_files?: string[];
  };
  human_review: {
    status: string;
    candidate_patch_applied_in_disposable_worktree: boolean;
    developer_tree_applied: boolean;
    deployed: boolean;
    human_approval_required: boolean;
  };
}
