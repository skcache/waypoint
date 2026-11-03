// Command demoexport projects one real repair artifact directory into the
// typed, sanitized JSON consumed by the Next.js demo page.
//
// It is deliberately a whitelist: only reviewable fields are copied. Candidate
// step commands/output, source files, evaluator-only fixes, host paths and
// credentials never enter the export.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/repair"
)

type export struct {
	SchemaVersion int          `json:"schema_version"`
	Run           runMeta      `json:"run"`
	Incident      incident     `json:"incident"`
	PreRepair     eval         `json:"pre_repair"`
	Diagnosis     *diagnosis   `json:"diagnosis,omitempty"`
	Patch         patch        `json:"patch"`
	Verification  verification `json:"verification"`
	Oracle        eval         `json:"oracle"`
	Unsafe        unsafe       `json:"unsafe"`
	HumanReview   humanReview  `json:"human_review"`
}

type runMeta struct {
	Kind             string      `json:"kind"` // recorded | replay | live
	Mode             string      `json:"mode"`
	ModelMode        string      `json:"model_mode"`
	Model            string      `json:"model"`
	ModelCalls       []modelCall `json:"model_calls"`
	PromptTokens     int         `json:"prompt_tokens"`
	CompletionTokens int         `json:"completion_tokens"`
	UsageSource      string      `json:"usage_source"`
	RunID            string      `json:"run_id"`
	BaseRevision     string      `json:"base_revision"`
	RevisionSource   string      `json:"revision_source"`
	EvidenceTier     string      `json:"evidence_tier"`
	FinishedAt       string      `json:"finished_at"`
	ModelRecordedAt  string      `json:"model_recorded_at,omitempty"`
	Source           string      `json:"source"`
	SourceLabel      string      `json:"source_label"`
}

type modelCall struct {
	Purpose          string `json:"purpose"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	LatencyMS        int64  `json:"latency_ms,omitempty"`
}

type incident struct {
	ID            string        `json:"id"`
	Fault         string        `json:"fault"`
	OrderID       string        `json:"order_id"`
	OrderStatus   string        `json:"order_status"`
	EvidenceTier  string        `json:"evidence_tier"`
	TotalMinor    int64         `json:"total_minor"`
	Currency      string        `json:"currency"`
	CheckoutError string        `json:"checkout_error,omitempty"`
	CaptureError  string        `json:"capture_error,omitempty"`
	Observations  []observation `json:"observations"`
}

type incidentFile struct {
	IncidentID    string `json:"incident_id"`
	FaultFamily   string `json:"fault_family"`
	OrderID       string `json:"order_id"`
	OrderStatus   string `json:"order_status"`
	EvidenceTier  string `json:"evidence_tier"`
	TotalMinor    int64  `json:"total_minor"`
	Currency      string `json:"currency"`
	CheckoutError string `json:"checkout_error"`
	CaptureError  string `json:"capture_error"`
}

type observation struct {
	Seq               int    `json:"seq"`
	At                string `json:"at,omitempty"`
	Method            string `json:"method"`
	OrderRef          string `json:"order_ref,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
	DurableBeforeCall bool   `json:"request_id_durable_before_call"`
	SentAmount        string `json:"sent_amount,omitempty"`
	SentCurrency      string `json:"sent_currency,omitempty"`
	Applied           bool   `json:"mutation_applied"`
	Replayed          bool   `json:"idempotent_replay"`
	ResponseDropped   bool   `json:"response_dropped"`
	DefiniteRefusal   bool   `json:"definite_refusal"`
	OrderStatus       string `json:"order_status,omitempty"`
	CaptureID         string `json:"capture_id,omitempty"`
	CaptureStatus     string `json:"capture_status,omitempty"`
	Tier              string `json:"tier,omitempty"`
}

type diagnosis struct {
	RootCause          string   `json:"root_cause"`
	Mechanism          string   `json:"mechanism"`
	ProposedFix        string   `json:"proposed_fix"`
	SuspectedFiles     []string `json:"suspected_files"`
	SupportingEvidence []string `json:"supporting_evidence"`
	InvariantsAtRisk   []string `json:"invariants_at_risk"`
	Uncertainties      []string `json:"uncertainties"`
}

type patch struct {
	Summary      string   `json:"summary"`
	ChangedFiles []string `json:"changed_files"`
	Digest       string   `json:"digest"`
	Diff         string   `json:"diff"`
}

type verification struct {
	Ladder  ladder `json:"ladder"`
	Steps   []step `json:"steps"`
	Sandbox string `json:"sandbox,omitempty"`
}

type ladder struct {
	DiagnosisProduced bool   `json:"diagnosis_produced"`
	PatchGenerated    bool   `json:"patch_generated"`
	PatchApplied      bool   `json:"patch_applied"`
	BuildPassed       bool   `json:"build_passed"`
	TestsPassed       bool   `json:"tests_passed"`
	OracleVerdict     string `json:"oracle_verdict"`
	Accepted          bool   `json:"accepted"`
}

type step struct {
	Name       string `json:"name"`
	ExitCode   int    `json:"exit_code"`
	TimedOut   bool   `json:"timed_out"`
	DurationMS int64  `json:"duration_ms"`
	Passed     bool   `json:"passed"`
}

type invariant struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type eval struct {
	Accepted    bool        `json:"accepted"`
	Reason      string      `json:"reason"`
	OrderStatus string      `json:"order_status"`
	Verdict     string      `json:"verdict"`
	EvaluatedAt string      `json:"evaluated_at,omitempty"`
	Invariants  []invariant `json:"invariants"`
}

type unsafe struct {
	Kind        string      `json:"kind"`
	Source      string      `json:"source"`
	Provenance  string      `json:"provenance"`
	Verdict     string      `json:"verdict"`
	Reason      string      `json:"reason"`
	EvaluatedAt string      `json:"evaluated_at,omitempty"`
	Invariants  []invariant `json:"invariants"`
	FreshGate   freshGate   `json:"fresh_trusted_gate"`
}

type freshGate struct {
	Executed bool   `json:"executed"`
	Result   string `json:"result"`
	Detail   string `json:"detail,omitempty"`
}

type humanReview struct {
	Status                                    string `json:"status"`
	CandidatePatchAppliedInDisposableWorktree bool   `json:"candidate_patch_applied_in_disposable_worktree"`
	DeveloperTreeApplied                      bool   `json:"developer_tree_applied"`
	Deployed                                  bool   `json:"deployed"`
	HumanApprovalRequired                     bool   `json:"human_approval_required"`
}

func main() {
	from := flag.String("from", "", "repair artifact directory (required)")
	out := flag.String("out", "", "export JSON path (required)")
	mode := flag.String("mode", "replay", "recorded | replay | live")
	source := flag.String("source", "", "repo-relative source artifact directory")
	sourceLabel := flag.String("source-label", "", "human-readable provenance label")
	unsafePath := flag.String("unsafe", "evidence/m4-unsafe-f3/eval.json", "seeded unsafe eval")
	unsafeSource := flag.String("unsafe-source", "evidence/m4-unsafe-f3/eval.json", "repo-relative unsafe eval provenance label")
	unsafeLog := flag.String("unsafe-test-log", "", "fresh trusted-gate test log (optional)")
	sourceReport := flag.String("source-report", "", "report of the original recorded run (optional)")
	flag.Parse()

	if *from == "" || *out == "" {
		fatal("-from and -out are required")
	}
	if *mode != "recorded" && *mode != "replay" && *mode != "live" {
		fatal("-mode must be recorded, replay or live")
	}
	if err := run(*from, *out, *mode, *source, *sourceLabel, *unsafePath, *unsafeSource, *unsafeLog, *sourceReport); err != nil {
		fatal("%v", err)
	}
}

func run(from, out, mode, source, sourceLabel, unsafePath, unsafeSource, unsafeLog, sourceReport string) error {
	rep, err := readReport(filepath.Join(from, "report.json"))
	if err != nil {
		return err
	}
	if rep.Status != repair.StatusAccepted {
		return fmt.Errorf("report status is %s, want %s", rep.Status, repair.StatusAccepted)
	}
	if !rep.Final.Accepted || !rep.Final.PatchApplied || !rep.Final.BuildPassed || !rep.Final.TestsPassed {
		return fmt.Errorf("accepted report has an incomplete verification ladder: %+v", rep.Final)
	}
	if rep.Applied || rep.Deployed || !rep.HumanApprovalRequired {
		return fmt.Errorf("report human boundary is inconsistent: applied=%v deployed=%v approval_required=%v",
			rep.Applied, rep.Deployed, rep.HumanApprovalRequired)
	}
	if len(rep.ModelCalls) == 0 {
		return fmt.Errorf("report has no model provenance")
	}
	if rep.EvidenceTier != "F" {
		return fmt.Errorf("unexpected evidence tier %q", rep.EvidenceTier)
	}

	var rawIncident incidentFile
	if err := readJSON(filepath.Join(from, "incident.json"), &rawIncident); err != nil {
		return err
	}
	inc := incident{
		ID: rawIncident.IncidentID, Fault: rawIncident.FaultFamily, OrderID: rawIncident.OrderID,
		OrderStatus: rawIncident.OrderStatus, EvidenceTier: rawIncident.EvidenceTier,
		TotalMinor: rawIncident.TotalMinor, Currency: rawIncident.Currency,
		CheckoutError: rawIncident.CheckoutError, CaptureError: rawIncident.CaptureError,
	}
	obsPath := filepath.Join(from, "observations.json")
	var obsFile struct {
		Observations []observation `json:"observations"`
	}
	if err := readJSON(obsPath, &obsFile); err == nil {
		inc.Observations = obsFile.Observations
	} else {
		// Fresh cmd/repair artifacts write a bare observation array; the
		// committed M4 evidence wraps it with order metadata.
		var obsArray []observation
		if err := readJSON(obsPath, &obsArray); err == nil {
			inc.Observations = obsArray
		}
	}
	pre, err := readEval(filepath.Join(from, "pre-repair-eval.json"))
	if err != nil {
		return err
	}
	oraclePath := filepath.Join(from, "eval.json")
	if _, err := os.Stat(oraclePath); err != nil {
		oraclePath = filepath.Join(from, "attempts", "attempt-1", "eval.json")
	}
	oracle, err := readEval(oraclePath)
	if err != nil {
		return err
	}
	unsafeEval, err := readEval(unsafePath)
	if err != nil {
		return err
	}
	fresh, err := readFreshGate(unsafeLog)
	if err != nil {
		return err
	}
	diff := rep.Attempts[0].Diff
	if data, err := os.ReadFile(filepath.Join(from, "patch.diff")); err == nil {
		diff = string(data)
	}
	if strings.TrimSpace(diff) == "" {
		return fmt.Errorf("no reviewable diff in the artifact directory")
	}
	if rep.PatchDigest == "" {
		return fmt.Errorf("no patch digest in the report")
	}

	recordedAt := ""
	if sourceReport != "" {
		src, err := readReport(sourceReport)
		if err != nil {
			return fmt.Errorf("read source report: %w", err)
		}
		recordedAt = src.FinishedAt.UTC().Format(time.RFC3339)
	}

	calls := make([]modelCall, 0, len(rep.ModelCalls))
	for _, c := range rep.ModelCalls {
		calls = append(calls, modelCall{
			Purpose: c.Purpose, Model: c.Model,
			PromptTokens: c.PromptTokens, CompletionTokens: c.CompletionTokens, LatencyMS: c.LatencyMS,
		})
	}
	model := calls[0].Model
	if mode == "live" && strings.HasPrefix(model, "recorded:") {
		return fmt.Errorf("live export cannot use a recorded model call")
	}
	if mode == "replay" && !strings.HasPrefix(model, "recorded:") {
		return fmt.Errorf("replay export must use recorded model calls")
	}
	modelMode := "hosted model"
	switch mode {
	case "recorded":
		modelMode = "recorded hosted model"
	case "replay":
		modelMode = "recorded model replay"
	}
	usageSource := "provider-reported"
	if mode == "replay" {
		usageSource = "recorded provider-reported"
	}
	if !rep.UsageReported {
		usageSource = "not reported"
	}
	if source == "" {
		source = sourceLabel
	}
	if source != "" && (filepath.IsAbs(source) || strings.Contains(source, "..")) {
		return fmt.Errorf("source must be a repo-relative path without traversal")
	}
	if unsafeSource != "" && (filepath.IsAbs(unsafeSource) || strings.Contains(unsafeSource, "..")) {
		return fmt.Errorf("unsafe-source must be a repo-relative path without traversal")
	}
	if sourceLabel == "" {
		sourceLabel = source
	}

	doc := export{
		SchemaVersion: 1,
		Run: runMeta{
			Kind: mode, Mode: rep.Mode, ModelMode: modelMode, Model: model,
			ModelCalls: calls, PromptTokens: rep.PromptTokens, CompletionTokens: rep.CompletionTokens,
			UsageSource: usageSource, RunID: rep.IncidentID, BaseRevision: rep.BaseRevision,
			RevisionSource: rep.RevisionSource, EvidenceTier: rep.EvidenceTier,
			FinishedAt: rep.FinishedAt.UTC().Format(time.RFC3339), ModelRecordedAt: recordedAt,
			Source: source, SourceLabel: sourceLabel,
		},
		Incident:  inc,
		PreRepair: pre,
		Diagnosis: diag(rep.Diagnosis),
		Patch: patch{
			Summary: patchSummary(rep), ChangedFiles: changedFiles(rep),
			Digest: rep.PatchDigest, Diff: diff,
		},
		Verification: verification{
			Ladder: ladder{
				DiagnosisProduced: rep.Final.DiagnosisProduced,
				PatchGenerated:    rep.Final.PatchGenerated,
				PatchApplied:      rep.Final.PatchApplied,
				BuildPassed:       rep.Final.BuildPassed,
				TestsPassed:       rep.Final.TestsPassed,
				OracleVerdict:     rep.Final.OracleVerdict,
				Accepted:          rep.Final.Accepted,
			},
			Steps: steps(rep), Sandbox: rep.Sandbox,
		},
		Oracle: oracle,
		Unsafe: unsafe{
			Kind: "seeded", Source: unsafeSource, Provenance: "Recorded M4 independent oracle evaluation; this is not a fresh judge run.",
			Verdict: unsafeEval.Verdict, Reason: unsafeEval.Reason, EvaluatedAt: unsafeEval.EvaluatedAt,
			Invariants: unsafeEval.Invariants, FreshGate: fresh,
		},
		HumanReview: humanReview{
			Status: rep.Status,
			CandidatePatchAppliedInDisposableWorktree: rep.Final.PatchApplied,
			DeveloperTreeApplied:                      rep.Applied,
			Deployed:                                  rep.Deployed,
			HumanApprovalRequired:                     rep.HumanApprovalRequired,
		},
	}

	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	for _, re := range privatePatterns {
		if re.Match(raw) {
			return fmt.Errorf("export contains private data matching %s", re.String())
		}
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, raw, 0o644)
}

func diag(d *repair.Diagnosis) *diagnosis {
	if d == nil {
		return nil
	}
	return &diagnosis{
		RootCause: d.RootCause, Mechanism: d.Mechanism, ProposedFix: d.ProposedFix,
		SuspectedFiles: d.SuspectedFiles, SupportingEvidence: d.SupportingEvidence,
		InvariantsAtRisk: d.InvariantsAtRisk, Uncertainties: d.Uncertainties,
	}
}

func patchSummary(rep *repair.Report) string {
	if len(rep.Attempts) == 0 || rep.Attempts[0].Patch == nil {
		return ""
	}
	return rep.Attempts[0].Patch.Summary
}

func changedFiles(rep *repair.Report) []string {
	if len(rep.Attempts) == 0 {
		return nil
	}
	return rep.Attempts[0].Changed
}

func steps(rep *repair.Report) []step {
	if len(rep.Attempts) == 0 {
		return nil
	}
	out := make([]step, 0, len(rep.Attempts[0].Steps))
	for _, s := range rep.Attempts[0].Steps {
		out = append(out, step{
			Name: s.Step, ExitCode: s.ExitCode, TimedOut: s.TimedOut,
			DurationMS: s.DurationMS, Passed: s.ExitCode == 0 && !s.TimedOut,
		})
	}
	return out
}

func readFreshGate(path string) (freshGate, error) {
	if path == "" {
		return freshGate{Executed: false, Result: "not_run"}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return freshGate{}, fmt.Errorf("read unsafe test log: %w", err)
	}
	text := string(raw)
	if !strings.Contains(text, "--- PASS: TestPipelineRejectsSeededUnsafeF3Candidate") {
		return freshGate{}, fmt.Errorf("fresh unsafe test did not pass:\n%s", tail(text, 40))
	}
	if !strings.Contains(text, "fulfillment effects = 2") {
		return freshGate{}, fmt.Errorf("fresh unsafe test did not report the duplicate fulfillment")
	}
	return freshGate{Executed: true, Result: "rejected_by_trusted_gate", Detail: "trusted test gate reported 2 fulfillment effects"}, nil
}

func readReport(path string) (*repair.Report, error) {
	var rep repair.Report
	if err := readJSON(path, &rep); err != nil {
		return nil, err
	}
	return &rep, nil
}

type evalFile struct {
	Accepted    bool   `json:"accepted"`
	Reason      string `json:"reason"`
	OrderStatus string `json:"order_status"`
	Oracle      struct {
		Verdict     string      `json:"verdict"`
		EvaluatedAt string      `json:"evaluated_at"`
		Invariants  []invariant `json:"invariants"`
	} `json:"oracle"`
}

func readEval(path string) (eval, error) {
	var ef evalFile
	if err := readJSON(path, &ef); err != nil {
		return eval{}, err
	}
	return eval{
		Accepted: ef.Accepted, Reason: ef.Reason, OrderStatus: ef.OrderStatus,
		Verdict: ef.Oracle.Verdict, EvaluatedAt: ef.Oracle.EvaluatedAt,
		Invariants: ef.Oracle.Invariants,
	}, nil
}

func readJSON(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return nil
}

func tail(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

var privatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`/Users/[^\s"\x60]+`),
	regexp.MustCompile(`/private/var/[^\s"\x60]+`),
	regexp.MustCompile(`/var/folders/[^\s"\x60]+`),
	regexp.MustCompile(`sk-or-v1-[A-Za-z0-9]+`),
	regexp.MustCompile(`OPENROUTER_API_KEY`),
	regexp.MustCompile(`postgres(?:ql)?://[^\s"']+:[^\s"'@]+@`),
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "demoexport: "+format+"\n", args...)
	os.Exit(1)
}
