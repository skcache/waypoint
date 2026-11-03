// Command demoexport projects one real repair artifact directory into the
// typed, sanitized JSON consumed by the Next.js demo page.
//
// It is deliberately a whitelist: only reviewable fields are copied. Candidate
// step commands/output, source files, evaluator-only fixes, host paths and
// credentials never enter the export.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	EvidenceLayout   string      `json:"evidence_layout"`
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
	ID                  string        `json:"id"`
	Fault               string        `json:"fault"`
	OrderID             string        `json:"order_id"`
	OriginalOrderID     string        `json:"original_order_id"`
	VerificationOrderID string        `json:"verification_order_id"`
	OrderStatus         string        `json:"order_status"`
	EvidenceTier        string        `json:"evidence_tier"`
	TotalMinor          int64         `json:"total_minor"`
	Currency            string        `json:"currency"`
	CheckoutError       string        `json:"checkout_error,omitempty"`
	CaptureError        string        `json:"capture_error,omitempty"`
	Observations        []observation `json:"observations"`
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
	CaptureAmount     string `json:"capture_amount,omitempty"`
	CaptureCurrency   string `json:"capture_currency,omitempty"`
	Stale             bool   `json:"stale_read"`
	Source            string `json:"source,omitempty"`
	Tier              string `json:"tier,omitempty"`
}

// UnmarshalJSON accepts both the canonical lower-case observation schema used
// by the early fixture bundle and the capitalised oracle.ProviderObs schema
// written by the accepted attempt. The wire shape differs; the fields do not.
func (o *observation) UnmarshalJSON(data []byte) error {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	type canonical observation
	if _, ok := keys["seq"]; ok {
		var c canonical
		if err := json.Unmarshal(data, &c); err != nil {
			return err
		}
		*o = observation(c)
		return nil
	}
	var p struct {
		Seq               int    `json:"Seq"`
		At                string `json:"At"`
		Method            string `json:"Method"`
		OrderRef          string `json:"OrderRef"`
		RequestID         string `json:"RequestID"`
		DurableBeforeCall bool   `json:"DurableBeforeCall"`
		SentAmount        string `json:"SentAmount"`
		SentCurrency      string `json:"SentCurrency"`
		Applied           bool   `json:"Applied"`
		Replayed          bool   `json:"Replayed"`
		ResponseDropped   bool   `json:"ResponseDropped"`
		DefiniteRefusal   bool   `json:"DefiniteRefusal"`
		OrderStatus       string `json:"OrderStatus"`
		CaptureID         string `json:"CaptureID"`
		CaptureStatus     string `json:"CaptureStatus"`
		CaptureAmount     string `json:"CaptureAmount"`
		CaptureCurrency   string `json:"CaptureCurrency"`
		Stale             bool   `json:"Stale"`
		Source            string `json:"Source"`
		Tier              string `json:"Tier"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*o = observation{
		Seq: p.Seq, At: p.At, Method: p.Method, OrderRef: p.OrderRef,
		RequestID: p.RequestID, DurableBeforeCall: p.DurableBeforeCall,
		SentAmount: p.SentAmount, SentCurrency: p.SentCurrency,
		Applied: p.Applied, Replayed: p.Replayed, ResponseDropped: p.ResponseDropped,
		DefiniteRefusal: p.DefiniteRefusal, OrderStatus: p.OrderStatus,
		CaptureID: p.CaptureID, CaptureStatus: p.CaptureStatus,
		CaptureAmount: p.CaptureAmount, CaptureCurrency: p.CaptureCurrency,
		Stale: p.Stale, Source: p.Source, Tier: p.Tier,
	}
	return nil
}

type observationsFile struct {
	Fault        string        `json:"fault"`
	OrderID      string        `json:"order_id"`
	Observations []observation `json:"observations"`
}

type oracleBundle struct {
	PatchDigest string `json:"patch_digest"`
	Verdict     string `json:"verdict"`
}

type attemptArtifacts struct {
	Dir              string
	DiffPath         string
	ObservationsPath string
	EvalPath         string
	BundlePath       string
	Layout           string
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
	Accepted          bool        `json:"accepted"`
	Reason            string      `json:"reason"`
	OrderStatus       string      `json:"order_status"`
	ClaimedCaptureID  string      `json:"claimed_capture_id,omitempty"`
	ProviderCaptureID string      `json:"provider_capture_id,omitempty"`
	Verdict           string      `json:"verdict"`
	EvaluatedAt       string      `json:"evaluated_at,omitempty"`
	Invariants        []invariant `json:"invariants"`
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

	att, err := acceptedAttempt(rep)
	if err != nil {
		return err
	}
	artifacts, err := resolveAttemptArtifacts(from, mode, att)
	if err != nil {
		return err
	}

	// All candidate evidence is read from the one accepted attempt. There is no
	// root-level or attempt-1 fallback for a fresh run: that would let a later
	// accepted patch be displayed with earlier verification evidence.
	diffBytes, err := os.ReadFile(artifacts.DiffPath)
	if err != nil {
		return fmt.Errorf("read accepted patch diff: %w", err)
	}
	if strings.TrimSpace(string(diffBytes)) == "" {
		return fmt.Errorf("accepted patch diff is empty")
	}
	if strings.TrimSpace(att.Diff) == "" {
		return fmt.Errorf("accepted attempt %d has no recorded diff", att.Number)
	}
	if string(diffBytes) != att.Diff {
		return fmt.Errorf("accepted patch diff does not match attempt %d report bytes", att.Number)
	}
	digest := diffDigest(diffBytes)
	if att.PatchDigest == "" || att.PatchDigest != digest {
		return fmt.Errorf("accepted attempt %d patch digest mismatch: report=%q computed=%q", att.Number, att.PatchDigest, digest)
	}
	if rep.PatchDigest == "" || rep.PatchDigest != digest {
		return fmt.Errorf("report patch digest mismatch: report=%q computed=%q", rep.PatchDigest, digest)
	}

	obs, err := readObservations(artifacts.ObservationsPath)
	if err != nil {
		return err
	}
	oracle, err := readEval(artifacts.EvalPath)
	if err != nil {
		return err
	}
	bundle, err := readBundle(artifacts.BundlePath)
	if err != nil {
		return err
	}
	if err := validateAcceptedEvidence(rep, att, obs, oracle, bundle, digest); err != nil {
		return err
	}

	var rawIncident incidentFile
	if err := readJSON(filepath.Join(from, "incident.json"), &rawIncident); err != nil {
		return err
	}
	inc := incident{
		ID: rawIncident.IncidentID, Fault: rawIncident.FaultFamily, OrderID: rawIncident.OrderID,
		OriginalOrderID: rawIncident.OrderID, VerificationOrderID: obs.OrderID,
		OrderStatus: rawIncident.OrderStatus, EvidenceTier: rawIncident.EvidenceTier,
		TotalMinor: rawIncident.TotalMinor, Currency: rawIncident.Currency,
		CheckoutError: rawIncident.CheckoutError, CaptureError: rawIncident.CaptureError,
		Observations: obs.Observations,
	}
	pre, err := readEval(filepath.Join(from, "pre-repair-eval.json"))
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
			EvidenceLayout: artifacts.Layout,
			FinishedAt:     rep.FinishedAt.UTC().Format(time.RFC3339), ModelRecordedAt: recordedAt,
			Source: source, SourceLabel: sourceLabel,
		},
		Incident:  inc,
		PreRepair: pre,
		Diagnosis: diag(rep.Diagnosis),
		Patch: patch{
			Summary: patchSummary(att), ChangedFiles: changedFiles(att),
			Digest: digest, Diff: string(diffBytes),
		},
		Verification: verification{
			Ladder: ladder{
				DiagnosisProduced: att.Ladder.DiagnosisProduced,
				PatchGenerated:    att.Ladder.PatchGenerated,
				PatchApplied:      att.Ladder.PatchApplied,
				BuildPassed:       att.Ladder.BuildPassed,
				TestsPassed:       att.Ladder.TestsPassed,
				OracleVerdict:     att.Ladder.OracleVerdict,
				Accepted:          att.Ladder.Accepted,
			},
			Steps: steps(att), Sandbox: rep.Sandbox,
		},
		Oracle: oracle,
		Unsafe: unsafe{
			Kind: "seeded", Source: unsafeSource, Provenance: "Recorded M4 independent oracle evaluation; this is not a fresh judge run.",
			Verdict: unsafeEval.Verdict, Reason: unsafeEval.Reason, EvaluatedAt: unsafeEval.EvaluatedAt,
			Invariants: unsafeEval.Invariants, FreshGate: fresh,
		},
		HumanReview: humanReview{
			Status: rep.Status,
			CandidatePatchAppliedInDisposableWorktree: att.Ladder.PatchApplied,
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
	return writeAtomic(out, raw)
}

func acceptedAttempt(rep *repair.Report) (*repair.Attempt, error) {
	var accepted *repair.Attempt
	for i := range rep.Attempts {
		if rep.Attempts[i].Ladder.Accepted {
			if accepted != nil {
				return nil, fmt.Errorf("report contains multiple accepted attempts")
			}
			accepted = &rep.Attempts[i]
		}
	}
	if accepted == nil {
		return nil, fmt.Errorf("report contains no accepted attempt")
	}
	if accepted.Number <= 0 {
		return nil, fmt.Errorf("accepted attempt has invalid number %d", accepted.Number)
	}
	if !reflect.DeepEqual(rep.Final, accepted.Ladder) {
		return nil, fmt.Errorf("report final ladder does not match accepted attempt %d", accepted.Number)
	}
	if accepted.Patch == nil || strings.TrimSpace(accepted.Patch.Summary) == "" {
		return nil, fmt.Errorf("accepted attempt %d has no patch summary", accepted.Number)
	}
	if len(accepted.Changed) == 0 {
		return nil, fmt.Errorf("accepted attempt %d has no changed files", accepted.Number)
	}
	if strings.TrimSpace(accepted.Diff) == "" || accepted.PatchDigest == "" {
		return nil, fmt.Errorf("accepted attempt %d has no reviewable diff or digest", accepted.Number)
	}
	if accepted.Eval == nil || !accepted.Eval.Accepted {
		return nil, fmt.Errorf("accepted attempt %d has no accepted judge summary", accepted.Number)
	}
	if len(accepted.Steps) == 0 {
		return nil, fmt.Errorf("accepted attempt %d has no verification steps", accepted.Number)
	}
	hasJudge := false
	for _, s := range accepted.Steps {
		if !s.Passed() {
			return nil, fmt.Errorf("accepted attempt %d contains a failed verification step %q", accepted.Number, s.Step)
		}
		if s.Step == "judge" {
			hasJudge = true
		}
	}
	if !hasJudge {
		return nil, fmt.Errorf("accepted attempt %d has no independent judge step", accepted.Number)
	}
	return accepted, nil
}

func resolveAttemptArtifacts(from, mode string, att *repair.Attempt) (attemptArtifacts, error) {
	attemptDir := filepath.Join(from, "attempts", fmt.Sprintf("attempt-%d", att.Number))
	if info, err := os.Stat(attemptDir); err == nil {
		if !info.IsDir() {
			return attemptArtifacts{}, fmt.Errorf("accepted attempt path is not a directory: %s", attemptDir)
		}
		return requireArtifacts(attemptDir, "attempt")
	} else if !os.IsNotExist(err) {
		return attemptArtifacts{}, fmt.Errorf("inspect accepted attempt: %w", err)
	}
	// The committed M4 recorded evidence predates the attempt directory layout.
	// It is accepted only in the explicit recorded mode and only as a complete
	// legacy root bundle; fresh replay/live runs never fall back to root files.
	if mode == "recorded" {
		return requireArtifacts(from, "legacy_recorded_root")
	}
	return attemptArtifacts{}, fmt.Errorf("accepted attempt %d evidence directory is missing; refusing root fallback", att.Number)
}

func requireArtifacts(dir, layout string) (attemptArtifacts, error) {
	required := map[string]string{
		"diff":         filepath.Join(dir, "patch.diff"),
		"observations": filepath.Join(dir, "observations.json"),
		"eval":         filepath.Join(dir, "eval.json"),
		"bundle":       filepath.Join(dir, "bundle.json"),
	}
	for name, path := range required {
		info, err := os.Stat(path)
		if err != nil {
			return attemptArtifacts{}, fmt.Errorf("%s evidence %s is missing: %w", layout, name, err)
		}
		if !info.Mode().IsRegular() {
			return attemptArtifacts{}, fmt.Errorf("%s evidence %s is not a regular file", layout, name)
		}
	}
	return attemptArtifacts{
		Dir: dir, DiffPath: required["diff"], ObservationsPath: required["observations"],
		EvalPath: required["eval"], BundlePath: required["bundle"], Layout: layout,
	}, nil
}

func readObservations(path string) (observationsFile, error) {
	var of observationsFile
	if err := readJSON(path, &of); err != nil {
		return of, err
	}
	if of.OrderID == "" {
		return of, fmt.Errorf("observations file has no order id")
	}
	if len(of.Observations) == 0 {
		return of, fmt.Errorf("observations file has no provider observations")
	}
	prev := 0
	for i, o := range of.Observations {
		if o.Seq <= 0 || (i > 0 && o.Seq <= prev) {
			return of, fmt.Errorf("observations are not in increasing sequence order at item %d", i)
		}
		if o.At == "" || o.Method == "" || o.Tier == "" {
			return of, fmt.Errorf("observation %d is missing timestamp, method or tier", o.Seq)
		}
		prev = o.Seq
	}
	return of, nil
}

func readBundle(path string) (oracleBundle, error) {
	var b oracleBundle
	if err := readJSON(path, &b); err != nil {
		return b, err
	}
	if b.PatchDigest == "" || b.Verdict == "" {
		return b, fmt.Errorf("judge bundle is missing patch digest or verdict")
	}
	return b, nil
}

func validateAcceptedEvidence(rep *repair.Report, att *repair.Attempt, obs observationsFile, oracle eval, bundle oracleBundle, digest string) error {
	if !oracle.Accepted {
		return fmt.Errorf("independent oracle did not accept the repaired candidate")
	}
	if oracle.Verdict == "" || oracle.Verdict != att.Ladder.OracleVerdict || oracle.Verdict != rep.Final.OracleVerdict {
		return fmt.Errorf("oracle verdict does not match accepted attempt %d", att.Number)
	}
	if len(oracle.Invariants) == 0 {
		return fmt.Errorf("independent oracle evidence contains no invariants")
	}
	if att.Eval != nil {
		if !att.Eval.Accepted || oracle.Reason != att.Eval.Reason || oracle.OrderStatus != att.Eval.OrderStatus {
			return fmt.Errorf("oracle result does not match accepted attempt %d summary", att.Number)
		}
		if oracle.ClaimedCaptureID != att.Eval.ClaimedCaptureID || oracle.ProviderCaptureID != att.Eval.ProviderCaptureID {
			return fmt.Errorf("oracle capture identity does not match accepted attempt %d", att.Number)
		}
	}
	if bundle.PatchDigest != digest || bundle.Verdict != oracle.Verdict {
		return fmt.Errorf("judge bundle does not describe the accepted attempt %d", att.Number)
	}
	if obs.Fault != "" && rep.Fault != "" && obs.Fault != rep.Fault {
		return fmt.Errorf("observations fault %q does not match report fault %q", obs.Fault, rep.Fault)
	}
	if att.Eval != nil && (att.Eval.ClaimedCaptureID != "" || att.Eval.ProviderCaptureID != "") {
		matchedCapture := false
		for _, o := range obs.Observations {
			if o.CaptureID != "" && (o.CaptureID == att.Eval.ClaimedCaptureID || o.CaptureID == att.Eval.ProviderCaptureID) {
				matchedCapture = true
				break
			}
		}
		if !matchedCapture {
			return fmt.Errorf("observations do not contain the capture identity for accepted attempt %d", att.Number)
		}
	}
	if rep.EvidenceTier != "" {
		for _, o := range obs.Observations {
			if o.Tier != rep.EvidenceTier {
				return fmt.Errorf("observation %d tier %q does not match report tier %q", o.Seq, o.Tier, rep.EvidenceTier)
			}
		}
	}
	return nil
}

func diffDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".demo-export-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(0o644); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
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

func patchSummary(att *repair.Attempt) string {
	if att == nil || att.Patch == nil {
		return ""
	}
	return att.Patch.Summary
}

func changedFiles(att *repair.Attempt) []string {
	if att == nil {
		return nil
	}
	return att.Changed
}

func steps(att *repair.Attempt) []step {
	if att == nil {
		return nil
	}
	out := make([]step, 0, len(att.Steps))
	for _, s := range att.Steps {
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
	Accepted          bool   `json:"accepted"`
	Reason            string `json:"reason"`
	OrderStatus       string `json:"order_status"`
	ClaimedCaptureID  string `json:"claimed_capture_id"`
	ProviderCaptureID string `json:"provider_capture_id"`
	Oracle            struct {
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
		ClaimedCaptureID: ef.ClaimedCaptureID, ProviderCaptureID: ef.ProviderCaptureID,
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
