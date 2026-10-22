package repair_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/oracle"
	"github.com/skcache/waypoint/apps/api/internal/repair"
	"github.com/skcache/waypoint/apps/api/internal/replay"
)

// scriptedModel is a BOUNDARY DOUBLE. It exercises the real model interface,
// schema validation and pipeline, but it is not a hosted model and the report
// it produces says so (model name "scripted-boundary-double"). It exists to
// test the integration boundary when no hosted credentials are configured.
type scriptedModel struct {
	mu      sync.Mutex
	replies []string
	calls   int
}

func (m *scriptedModel) Name() string { return "scripted-boundary-double" }

func (m *scriptedModel) Complete(_ context.Context, purpose, system, user string) (repair.Reply, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls >= len(m.replies) {
		return repair.Reply{}, fmt.Errorf("scripted model has no reply for call %d", m.calls+1)
	}
	text := m.replies[m.calls]
	m.calls++
	return repair.Reply{Text: text, Meta: repair.CallMeta{
		Purpose: purpose, Model: m.Name(),
		PromptBytes: len(system) + len(user), ResponseBytes: len(text),
	}}, nil
}

func diagnosisReply() string {
	d := map[string]any{
		"root_cause":          "the fixture returns the provider order envelope id as the capture id",
		"mechanism":           "captureEvidence returns res.ID (the order identity) instead of the capture resource id, so the durable journal certifies a capture identity the provider never issued",
		"suspected_files":     []string{"apps/api/internal/merchant/service.go"},
		"supporting_evidence": []string{"EV-1"},
		"invariants_at_risk":  []string{"I6"},
		"proposed_fix":        "return the capture resource id produced by completeCapture",
		"confidence_reasons":  []string{"the journal capture id differs from the provider capture observation for the same request"},
		"uncertainties":       []string{"provider interactions are scripted (tier F)"},
	}
	data, _ := json.Marshal(d)
	return string(data)
}

func fixF4PatchReply() string {
	p := map[string]any{
		"summary": "return the capture resource id instead of the order envelope id",
		"edits": []map[string]any{{
			"path":      "apps/api/internal/merchant/service.go",
			"old_text":  "\t\treturn res.ID, nil",
			"new_text":  "\t\treturn captureID, nil",
			"rationale": "the journal must record the capture resource identity the provider issued",
		}},
	}
	data, _ := json.Marshal(p)
	return string(data)
}

func badPatchReply() string {
	p := map[string]any{
		"summary": "rewrite the store",
		"edits": []map[string]any{{
			"path":     "apps/api/internal/merchant/store.go",
			"old_text": "package merchant",
			"new_text": "package merchant",
		}},
	}
	data, _ := json.Marshal(p)
	return string(data)
}

func pipelineFixture(t *testing.T, fault string) (*replay.Harness, replay.FixtureRun, repair.Evidence) {
	t.Helper()
	h, err := replay.OpenHarnessForTestsNamed(context.Background(), os.Getenv("TEST_DATABASE_URL"), "waypoint_repair_test")
	if err != nil {
		if os.Getenv("REQUIRE_DATABASE_TESTS") != "" {
			t.Fatalf("database tests are REQUIRED but the harness failed: %v", err)
		}
		t.Skipf("PostgreSQL test database not reachable (%v); start it with `docker compose up -d db`", err)
	}
	t.Cleanup(h.Close)
	ctx := context.Background()
	if err := h.ResetFixtureState(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	run, err := replay.RunFixtureFault(ctx, h, fault)
	if err != nil {
		t.Fatalf("fixture run: %v", err)
	}
	verdict, err := replay.JudgeFixtureRun(ctx, h, run)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if verdict.Accepted {
		t.Fatalf("pre-repair fixture was already accepted; the test needs a real incident (%s)", verdict.Reason)
	}
	root := repoRoot(t)
	ev, err := repair.BuildEvidence(ctx, h, run, root, "test-revision", repair.AllowlistForFault(fault))
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}
	return h, run, ev
}

// TestPipelineRepairsTheHeldOutDefectWithABoundaryDouble exercises the whole M3
// pipeline — diagnosis, rejected candidate, bounded retry, patch application in
// an isolated worktree, trusted tests, candidate build, fixture verification and
// oracle verdict — against the held-out F4 defect. The model is a labelled
// boundary double, not a hosted model, and the report records that name.
func TestPipelineRepairsTheHeldOutDefectWithABoundaryDouble(t *testing.T) {
	_, _, ev := pipelineFixture(t, "F4")
	root := repoRoot(t)
	before, err := repair.RepoStatus(root)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	out := tempOut(t)

	model := &scriptedModel{replies: []string{diagnosisReply(), badPatchReply(), fixF4PatchReply()}}
	rep := repair.Run(context.Background(), repair.Options{
		RepoRoot: root, BaseRef: "HEAD", Fault: "F4", OutDir: out, Runner: sharedSandbox(t),
		Mode: "oracle", MaxAttempts: 2, Model: model, Evidence: ev,
		Allow: repair.DefaultAllowlist(), RepoStatusBefore: before,
		Revision: "test-revision", RevisionSource: "test", IncidentID: ev.IncidentID,
	})

	if rep.Status != repair.StatusAccepted {
		t.Fatalf("status = %s, want ACCEPTED; attempts=%d last=%+v", rep.Status, len(rep.Attempts), lastAttempt(rep))
	}
	if len(rep.Attempts) != 2 {
		t.Fatalf("attempts = %d, want 2 (one rejected candidate + one accepted)", len(rep.Attempts))
	}
	first := rep.Attempts[0]
	if first.Ladder.PatchApplied || first.Error == "" {
		t.Fatalf("first candidate should have been rejected before execution: %+v", first)
	}
	second := rep.Attempts[1]
	for name, ok := range map[string]bool{
		"patch generated": second.Ladder.PatchGenerated,
		"patch applied":   second.Ladder.PatchApplied,
		"build":           second.Ladder.BuildPassed,
		"tests":           second.Ladder.TestsPassed,
		"accepted":        second.Ladder.Accepted,
	} {
		if !ok {
			t.Fatalf("%s rung not reached: %+v", name, second.Ladder)
		}
	}
	if second.Eval == nil || !second.Eval.Accepted {
		t.Fatalf("fixture eval = %+v, want accepted", second.Eval)
	}
	if second.Eval.OracleVerdict != string(oracle.VerdictVerified) {
		t.Fatalf("oracle verdict = %q, want VERIFIED", second.Eval.OracleVerdict)
	}
	if !strings.Contains(second.Diff, "return captureID, nil") {
		t.Fatalf("diff does not show the applied fix:\n%s", second.Diff)
	}
	if rep.Applied || rep.Deployed || !rep.HumanApprovalRequired {
		t.Fatalf("report claims side effects: applied=%v deployed=%v human=%v", rep.Applied, rep.Deployed, rep.HumanApprovalRequired)
	}
	if len(rep.ModelCalls) != 3 || rep.ModelCalls[0].Model != "scripted-boundary-double" {
		t.Fatalf("model provenance missing/incorrect: %+v", rep.ModelCalls)
	}

	// Artifacts a reviewer needs.
	for _, p := range []string{"report.json", "report.md", "diagnosis.json",
		"attempts/attempt-1/patch-reply.txt", "attempts/attempt-2/patch.diff",
		"attempts/attempt-2/eval.json", "attempts/attempt-2/bundle.json"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Fatalf("missing artifact %s: %v", p, err)
		}
	}

	// The developer's tree is untouched.
	if after, err := repair.RepoStatus(root); err != nil || after != before {
		t.Fatalf("developer tree changed during the pipeline: %v\nbefore=%q\nafter=%q", err, before, after)
	}
}

// TestPipelineBaselineIsSingleShot pins the baseline semantics: one attempt, no
// feedback loop, and the honest NOT_VERIFIED status when that attempt is bad.
func TestPipelineBaselineIsSingleShot(t *testing.T) {
	_, _, ev := pipelineFixture(t, "F4")
	root := repoRoot(t)
	out := tempOut(t)

	model := &scriptedModel{replies: []string{diagnosisReply(), fixF4PatchReply(), fixF4PatchReply()}}
	rep := repair.Run(context.Background(), repair.Options{
		RepoRoot: root, BaseRef: "HEAD", Fault: "F4", OutDir: out, Runner: sharedSandbox(t),
		Mode: "baseline", MaxAttempts: 2, Model: model, Evidence: ev,
		Allow: repair.DefaultAllowlist(), Revision: "test-revision", RevisionSource: "test",
		IncidentID: ev.IncidentID,
	})
	if len(rep.Attempts) != 1 {
		t.Fatalf("baseline attempts = %d, want exactly 1", len(rep.Attempts))
	}
	if rep.Status != repair.StatusAccepted {
		t.Fatalf("status = %s, want ACCEPTED for a correct single-shot patch (%+v)", rep.Status, lastAttempt(rep))
	}
	if model.calls != 2 {
		t.Fatalf("baseline model calls = %d, want diagnosis + one patch", model.calls)
	}
}

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func lastAttempt(rep *repair.Report) repair.Attempt {
	if len(rep.Attempts) == 0 {
		return repair.Attempt{}
	}
	return rep.Attempts[len(rep.Attempts)-1]
}

// TestPipelineRepairsF1WithBoundaryDouble proves the F1 family is repairable
// now that pre-repair reproduction and post-repair correctness are separated:
// the pinned revision reproduces the malformed payload (tagged reproduction
// test / fixture journey), while the candidate must pass the normal suite,
// which asserts correct provider-facing behavior, plus the oracle. The model
// is the labelled boundary double, not a hosted model.
func TestPipelineRepairsF1WithBoundaryDouble(t *testing.T) {
	_, run, ev := pipelineFixture(t, "F1")
	if run.OrderStatus != "FAILED_DEFINITE" {
		t.Fatalf("pre-repair order status = %s, want FAILED_DEFINITE", run.OrderStatus)
	}
	root := repoRoot(t)
	before, err := repair.RepoStatus(root)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	out := tempOut(t)

	diag := map[string]any{
		"root_cause":          "presentation formatting leaks into the provider amount field",
		"mechanism":           "amountValue formats the money with %f while the fault is active, producing six decimal places that the provider rejects as an invalid amount",
		"suspected_files":     []string{"apps/api/internal/merchant/service.go"},
		"supporting_evidence": []string{"EV-1"},
		"invariants_at_risk":  []string{"I1", "I2"},
		"proposed_fix":        "render the amount through the canonical minor-unit formatter in every path",
		"confidence_reasons":  []string{"the provider refused the create with DECIMAL_PRECISION and the request carried 12.340000"},
		"uncertainties":       []string{"provider interactions are scripted (tier F)"},
	}
	diagJSON, _ := json.Marshal(diag)
	patch := map[string]any{
		"summary": "use the canonical two-decimal formatter",
		"edits": []map[string]any{{
			"path":      "apps/api/internal/merchant/service.go",
			"old_text":  `fmt.Sprintf("%f", float64(totalMinor)/100)`,
			"new_text":  "paypal.FormatMinorUSD(totalMinor)",
			"rationale": "the provider contract requires an exact two-decimal amount",
		}},
	}
	patchJSON, _ := json.Marshal(patch)

	model := &scriptedModel{replies: []string{string(diagJSON), string(patchJSON)}}
	rep := repair.Run(context.Background(), repair.Options{
		RepoRoot: root, BaseRef: "HEAD", Fault: "F1", OutDir: out,
		Runner: sharedSandbox(t),
		Mode:   "oracle", MaxAttempts: 2, Model: model, Evidence: ev,
		Allow: repair.DefaultAllowlist(), RepoStatusBefore: before,
		Revision: "test-revision", RevisionSource: "test", IncidentID: ev.IncidentID,
	})
	if rep.Status != repair.StatusAccepted {
		for _, a := range rep.Attempts {
			t.Logf("attempt %d error=%q", a.Number, a.Error)
			for _, st := range a.Steps {
				t.Logf("  %s exit=%d out=%s", st.Step, st.ExitCode, tailOf(st.Output, 1200))
			}
		}
		t.Fatalf("status = %s, want ACCEPTED (%+v)", rep.Status, lastAttempt(rep))
	}
	last := lastAttempt(rep)
	if last.Eval == nil || last.Eval.OracleVerdict != string(oracle.VerdictVerified) {
		t.Fatalf("oracle verdict = %+v, want VERIFIED", last.Eval)
	}
	if !strings.Contains(last.Diff, "paypal.FormatMinorUSD(totalMinor)") {
		t.Fatalf("diff does not show the fix:\n%s", last.Diff)
	}
	if after, err := repair.RepoStatus(root); err != nil || after != before {
		t.Fatalf("developer tree changed: %v", err)
	}
}

// tempOut returns a temp artifact directory, optionally mirrored to
// M3_DEMO_OUT so a reviewer can inspect a real run (test-only plumbing).
func tempOut(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if demo := os.Getenv("M3_DEMO_OUT"); demo != "" {
		t.Cleanup(func() { copyTree(dir, demo) })
	}
	return dir
}

func copyTree(src, dst string) {
	_ = filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			_ = os.MkdirAll(target, 0o700)
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		_ = os.MkdirAll(filepath.Dir(target), 0o700)
		_ = os.WriteFile(target, data, 0o600)
		return nil
	})
}

// TestBaselineWithoutFeedbackStopsAtTheRejectedCandidate is the comparison
// case: with identical model replies, the oracle-fed loop rejects a bad first
// patch, feeds the failure back, and accepts the second; the baseline stops at
// the first rejection. Both runs use the labelled boundary double.
func TestBaselineWithoutFeedbackStopsAtTheRejectedCandidate(t *testing.T) {
	_, _, ev := pipelineFixture(t, "F4")
	root := repoRoot(t)
	out := tempOut(t)

	model := &scriptedModel{replies: []string{diagnosisReply(), badPatchReply()}}
	rep := repair.Run(context.Background(), repair.Options{
		RepoRoot: root, BaseRef: "HEAD", Fault: "F4", OutDir: out, Runner: sharedSandbox(t),
		Mode: "baseline", MaxAttempts: 2, Model: model, Evidence: ev,
		Allow: repair.DefaultAllowlist(), Revision: "test-revision", RevisionSource: "test",
		IncidentID: ev.IncidentID,
	})
	if len(rep.Attempts) != 1 || model.calls != 2 {
		t.Fatalf("baseline attempts=%d model calls=%d, want 1/2 (diagnosis + one patch)", len(rep.Attempts), model.calls)
	}
	if rep.Status != repair.StatusNotVerified {
		t.Fatalf("baseline status = %s, want NOT_VERIFIED", rep.Status)
	}
	if rep.Attempts[0].Ladder.Accepted {
		t.Fatalf("a rejected patch was accepted: %+v", rep.Attempts[0].Ladder)
	}
}
