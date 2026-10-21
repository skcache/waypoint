// Package repair implements M3's incident → diagnosis → scoped patch →
// independent verification pipeline.
//
// Non-negotiable boundaries:
//
//   - The model never decides financial correctness. It proposes a patch; the
//     independent oracle and the fixture acceptance rules decide.
//   - Candidate code is applied only inside a disposable git worktree and only
//     to an allowlisted slice of the merchant fixture. Tests, migrations, the
//     replay harness, the oracle and the verifier are outside that scope.
//   - Verification runs the CANDIDATE's compiled code (the worktree is built
//     and executed), never the reference implementation on its behalf.
//   - Nothing is applied to the developer's tree, deployed, pushed or merged.
package repair

import (
	"encoding/json"
	"time"
)

// Diagnosis is the investigator's structured, falsifiable hypothesis. Every
// field is required to be non-empty where it carries a claim; the model is not
// asked for an invented probability.
type Diagnosis struct {
	RootCause          string   `json:"root_cause"`
	Mechanism          string   `json:"mechanism"`
	SuspectedFiles     []string `json:"suspected_files"`
	SupportingEvidence []string `json:"supporting_evidence"`
	InvariantsAtRisk   []string `json:"invariants_at_risk"`
	ProposedFix        string   `json:"proposed_fix"`
	ConfidenceReasons  []string `json:"confidence_reasons"`
	Uncertainties      []string `json:"uncertainties"`
}

// Edit is one exact-substring replacement. Exact replacements are used instead
// of model-authored unified diffs because they are deterministically
// verifiable: the old text must occur exactly once, or the patch is refused.
type Edit struct {
	Path      string `json:"path"`
	OldText   string `json:"old_text"`
	NewText   string `json:"new_text"`
	Rationale string `json:"rationale,omitempty"`
}

// Patch is the model's scoped repair proposal.
type Patch struct {
	Summary string `json:"summary"`
	Edits   []Edit `json:"edits"`
}

// CallMeta records one model interaction. Tokens are only set when the
// provider reported them; cost is never invented.
type CallMeta struct {
	Purpose          string `json:"purpose"`
	Model            string `json:"model"`
	LatencyMS        int64  `json:"latency_ms"`
	PromptBytes      int    `json:"prompt_bytes"`
	ResponseBytes    int    `json:"response_bytes"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	UsageReported    bool   `json:"usage_reported"`
}

// Ladder keeps the distinct facts of a repair attempt separate. These are
// deliberately NOT collapsed into one "success" boolean.
type Ladder struct {
	DiagnosisProduced bool   `json:"diagnosis_produced"`
	PatchGenerated    bool   `json:"patch_generated"`
	PatchApplied      bool   `json:"patch_applied"`
	BuildPassed       bool   `json:"build_passed"`
	TestsPassed       bool   `json:"tests_passed"`
	OracleVerdict     string `json:"oracle_verdict,omitempty"`
	Accepted          bool   `json:"accepted_for_human_review"`
}

// Stage names the highest rung actually reached, for display.
func (l Ladder) Stage() string {
	switch {
	case l.Accepted:
		return "ACCEPTED_FOR_HUMAN_REVIEW"
	case l.OracleVerdict != "":
		return "ORACLE_VERDICT"
	case l.TestsPassed:
		return "TESTS_PASSED"
	case l.BuildPassed:
		return "BUILD_PASSED"
	case l.PatchApplied:
		return "PATCH_APPLIED"
	case l.PatchGenerated:
		return "PATCH_GENERATED"
	case l.DiagnosisProduced:
		return "DIAGNOSED"
	default:
		return "NO_OUTPUT"
	}
}

// StepResult is one bounded subprocess execution.
type StepResult struct {
	Step            string   `json:"step"`
	Command         []string `json:"command"`
	ExitCode        int      `json:"exit_code"`
	TimedOut        bool     `json:"timed_out"`
	DurationMS      int64    `json:"duration_ms"`
	Output          string   `json:"output"`
	OutputTruncated bool     `json:"output_truncated"`
}

// Passed reports a clean, untimed-out exit.
func (s StepResult) Passed() bool { return s.ExitCode == 0 && !s.TimedOut }

// EvalSummary is the trusted verifier's decision, parsed from the workspace's
// own repaircheck run. The full oracle result stays in the bundle.
type EvalSummary struct {
	Accepted          bool     `json:"accepted"`
	Reason            string   `json:"reason"`
	OrderStatus       string   `json:"order_status"`
	ClaimedCaptureID  string   `json:"claimed_capture_id"`
	ProviderCaptureID string   `json:"provider_capture_id"`
	OracleVerdict     string   `json:"-"`
	FailingInvariants []string `json:"-"`
}

// Attempt is one bounded repair try with everything needed to review it.
type Attempt struct {
	Number     int          `json:"number"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
	Ladder     Ladder       `json:"ladder"`
	Patch      *Patch       `json:"patch,omitempty"`
	Diff       string       `json:"diff,omitempty"`
	Changed    []string     `json:"changed_files,omitempty"`
	Steps      []StepResult `json:"steps,omitempty"`
	Eval       *EvalSummary `json:"eval,omitempty"`
	Feedback   string       `json:"feedback_to_model,omitempty"`
	Error      string       `json:"error,omitempty"`
	Calls      []CallMeta   `json:"model_calls,omitempty"`
}

// Report is the reviewable repair artifact. It states plainly that nothing was
// applied or deployed and that a human must approve any integration.
type Report struct {
	SchemaVersion  int       `json:"schema_version"`
	IncidentID     string    `json:"incident_id"`
	Fault          string    `json:"fault_family"`
	Mode           string    `json:"mode"`
	EvidenceTier   string    `json:"evidence_tier"`
	BaseRevision   string    `json:"base_revision"`
	RevisionSource string    `json:"revision_source"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	Status         string    `json:"status"`

	Diagnosis *Diagnosis `json:"diagnosis,omitempty"`
	Attempts  []Attempt  `json:"attempts"`
	Final     Ladder     `json:"final_ladder"`

	ModelCalls       []CallMeta `json:"model_calls,omitempty"`
	PromptTokens     int        `json:"prompt_tokens,omitempty"`
	CompletionTokens int        `json:"completion_tokens,omitempty"`
	UsageReported    bool       `json:"usage_reported"`
	CostUSD          *float64   `json:"cost_usd,omitempty"`

	Applied               bool      `json:"applied_to_developer_tree"`
	Deployed              bool      `json:"deployed"`
	HumanApprovalRequired bool      `json:"human_approval_required"`
	Limitations           []string  `json:"limitations"`
	Baseline              *Baseline `json:"baseline_comparison,omitempty"`
}

// Baseline records the comparison run's outcome without conflating it with the
// oracle-fed run.
type Baseline struct {
	Mode      string `json:"mode"`
	Attempts  int    `json:"attempts"`
	Accepted  bool   `json:"accepted"`
	Stage     string `json:"stage_reached"`
	LatencyMS int64  `json:"latency_ms"`
}

// Statuses are exact strings, never booleans with invented meaning.
const (
	StatusNotConfigured = "NOT_CONFIGURED"
	StatusError         = "ERROR"
	StatusNotVerified   = "NOT_VERIFIED"
	StatusAccepted      = "ACCEPTED_FOR_HUMAN_REVIEW"
)

// MarshalReport renders the report deterministically.
func MarshalReport(r *Report) ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
