package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/repair"
)

const (
	verifiedVerdict = "VERIFIED_FOR_TESTED_SCENARIOS"
	unsafeVerdict   = "UNSAFE"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "apps", "api", "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeText(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func observationFixture(orderID, captureID string) []map[string]any {
	return []map[string]any{
		{
			"Seq": 1, "At": "2026-11-03T00:00:00.000000001Z", "Method": "create",
			"OrderRef": "ORD-" + orderID, "RequestID": "req-create-" + orderID,
			"DurableBeforeCall": true, "SentAmount": "12.34", "SentCurrency": "USD",
			"Applied": true, "Replayed": false, "ResponseDropped": false,
			"DefiniteRefusal": false, "OrderStatus": "CREATED", "CaptureID": "",
			"CaptureStatus": "", "CaptureAmount": "", "CaptureCurrency": "",
			"Stale": false, "Source": "scripted-fixture", "Tier": "F",
		},
		{
			"Seq": 4, "At": "2026-11-03T00:00:00.000000002Z", "Method": "capture",
			"OrderRef": "ORD-" + orderID, "RequestID": "req-capture-" + orderID,
			"DurableBeforeCall": true, "SentAmount": "12.34", "SentCurrency": "USD",
			"Applied": true, "Replayed": false, "ResponseDropped": false,
			"DefiniteRefusal": false, "OrderStatus": "COMPLETED", "CaptureID": captureID,
			"CaptureStatus": "COMPLETED", "CaptureAmount": "12.34", "CaptureCurrency": "USD",
			"Stale": false, "Source": "scripted-fixture", "Tier": "F",
		},
	}
}

func attemptFixture(number int, accepted bool, token string) repair.Attempt {
	diff := fmt.Sprintf("diff --git a/apps/api/internal/merchant/webhook.go b/apps/api/internal/merchant/webhook.go\n--- a/apps/api/internal/merchant/webhook.go\n+++ b/apps/api/internal/merchant/webhook.go\n@@ -1 +1 @@\n-old-%s\n+new-%s\n", token, token)
	verdict := verifiedVerdict
	if !accepted {
		verdict = unsafeVerdict
	}
	return repair.Attempt{
		Number: number,
		Ladder: repair.Ladder{
			DiagnosisProduced: true, PatchGenerated: true, PatchApplied: true,
			BuildPassed: true, TestsPassed: true, OracleVerdict: verdict, Accepted: accepted,
		},
		Patch:       &repair.Patch{Summary: "repair summary " + token},
		Diff:        diff,
		PatchDigest: digest(diff),
		Changed:     []string{"apps/api/internal/merchant/webhook.go"},
		Steps: []repair.StepResult{
			{Step: "gofmt", ExitCode: 0},
			{Step: "build", ExitCode: 0},
			{Step: "tests", ExitCode: 0},
			{Step: "fixture", ExitCode: 0},
			{Step: "judge", ExitCode: 0},
		},
		Eval: &repair.EvalSummary{
			Accepted: accepted, Reason: "reason-" + token, OrderStatus: "CAPTURED_VERIFIED",
			ClaimedCaptureID: "CAP-" + token, ProviderCaptureID: "CAP-" + token,
		},
	}
}

func makeMultiAttemptFixture(t *testing.T) (string, repair.Attempt, repair.Attempt) {
	t.Helper()
	dir := t.TempDir()
	first := attemptFixture(1, false, "one")
	second := attemptFixture(2, true, "two")

	rep := repair.Report{
		SchemaVersion: 3, IncidentID: "M3-INC-TEST", Fault: "F3", Mode: "oracle",
		EvidenceTier: "F", BaseRevision: "test-base", RevisionSource: "test",
		StartedAt: time.Unix(1, 0).UTC(), FinishedAt: time.Unix(2, 0).UTC(),
		Status: repair.StatusAccepted, Attempts: []repair.Attempt{first, second},
		Final: second.Ladder, PatchDigest: second.PatchDigest,
		ModelCalls:   []repair.CallMeta{{Purpose: "patch", Model: "recorded:test", UsageReported: true}},
		PromptTokens: 12, CompletionTokens: 7, UsageReported: true,
		HumanApprovalRequired: true,
	}
	writeJSON(t, filepath.Join(dir, "report.json"), rep)
	writeJSON(t, filepath.Join(dir, "incident.json"), map[string]any{
		"incident_id": "M3-INC-TEST", "fault_family": "F3", "order_id": "incident-order",
		"order_status": "APPROVED", "evidence_tier": "F", "total_minor": 1234, "currency": "USD",
	})
	writeJSON(t, filepath.Join(dir, "pre-repair-eval.json"), map[string]any{
		"accepted": false, "reason": "pre-repair", "order_status": "APPROVED",
		"oracle": map[string]any{"verdict": unsafeVerdict, "invariants": []any{}},
	})
	for _, att := range []repair.Attempt{first, second} {
		token := "one"
		if att.Ladder.Accepted {
			token = "two"
		}
		attemptDir := filepath.Join(dir, "attempts", fmt.Sprintf("attempt-%d", att.Number))
		writeText(t, filepath.Join(attemptDir, "patch.diff"), att.Diff)
		writeJSON(t, filepath.Join(attemptDir, "observations.json"), map[string]any{
			"fault": "F3", "order_id": "order-" + fmt.Sprint(att.Number),
			"observations": observationFixture("order-"+fmt.Sprint(att.Number), "CAP-"+token),
		})
		writeJSON(t, filepath.Join(attemptDir, "eval.json"), map[string]any{
			"accepted": att.Eval.Accepted, "reason": att.Eval.Reason, "order_status": att.Eval.OrderStatus,
			"claimed_capture_id": att.Eval.ClaimedCaptureID, "provider_capture_id": att.Eval.ProviderCaptureID,
			"oracle": map[string]any{
				"verdict": att.Ladder.OracleVerdict, "evaluated_at": "2026-11-03T00:00:00Z",
				"invariants": []any{map[string]any{"id": "I13", "status": "PASS", "summary": "single fulfillment"}},
			},
		})
		writeJSON(t, filepath.Join(attemptDir, "bundle.json"), map[string]any{
			"patch_digest": att.PatchDigest, "verdict": att.Ladder.OracleVerdict,
		})
	}

	// Deliberately complete but contradictory root evidence. A fallback to the
	// root or attempt 1 files must be caught by the export tests.
	writeText(t, filepath.Join(dir, "patch.diff"), "ROOT-DIFF\n")
	writeJSON(t, filepath.Join(dir, "observations.json"), map[string]any{
		"fault": "F3", "order_id": "root-order",
		"observations": observationFixture("root-order", "CAP-ROOT"),
	})
	writeJSON(t, filepath.Join(dir, "eval.json"), map[string]any{
		"accepted": true, "reason": "root-reason", "order_status": "CAPTURED_VERIFIED",
		"claimed_capture_id": "CAP-ROOT", "provider_capture_id": "CAP-ROOT",
		"oracle": map[string]any{
			"verdict": verifiedVerdict, "evaluated_at": "2026-11-03T00:00:00Z",
			"invariants": []any{map[string]any{"id": "I13", "status": "PASS", "summary": "root"}},
		},
	})
	writeJSON(t, filepath.Join(dir, "bundle.json"), map[string]any{
		"patch_digest": digest("ROOT-DIFF\n"), "verdict": verifiedVerdict,
	})
	return dir, first, second
}

func exportFixture(t *testing.T, dir string) (*export, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "local.json")
	err := run(
		dir, out, "replay", "tmp/demo", "Local make demo replay",
		filepath.Join(repoRoot(t), "evidence", "m4-unsafe-f3", "eval.json"),
		"evidence/m4-unsafe-f3/eval.json", "", "",
	)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc export
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return &doc, nil
}

func TestExportUsesAcceptedAttemptEvidenceOnly(t *testing.T) {
	dir, _, second := makeMultiAttemptFixture(t)
	doc, err := exportFixture(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Run.EvidenceLayout != "attempt" {
		t.Fatalf("layout = %q", doc.Run.EvidenceLayout)
	}
	if doc.Patch.Diff != second.Diff || doc.Patch.Digest != second.PatchDigest {
		t.Fatalf("patch came from the wrong attempt")
	}
	if !strings.Contains(doc.Patch.Summary, "two") {
		t.Fatalf("summary = %q", doc.Patch.Summary)
	}
	if doc.Oracle.Verdict != verifiedVerdict || doc.Oracle.Reason != "reason-two" {
		t.Fatalf("oracle = %+v", doc.Oracle)
	}
	if doc.Incident.OriginalOrderID != "incident-order" || doc.Incident.VerificationOrderID != "order-2" {
		t.Fatalf("order identity = original %q verification %q", doc.Incident.OriginalOrderID, doc.Incident.VerificationOrderID)
	}
	if doc.Incident.OriginalOrderID == doc.Incident.VerificationOrderID {
		t.Fatal("original and verification orders must remain distinct")
	}
	if len(doc.Incident.Observations) != 2 {
		t.Fatalf("observations = %+v", doc.Incident.Observations)
	}
	last := doc.Incident.Observations[len(doc.Incident.Observations)-1]
	if last.CaptureID != "CAP-two" || last.Tier != "F" || last.At == "" || last.Seq != 4 {
		t.Fatalf("accepted observation = %+v", last)
	}
	if doc.Incident.Observations[0].OrderRef != "ORD-order-2" {
		t.Fatalf("first observation = %+v", doc.Incident.Observations[0])
	}
	seenJudge := false
	for _, s := range doc.Verification.Steps {
		if s.Name == "judge" {
			seenJudge = true
		}
	}
	if !seenJudge {
		t.Fatalf("steps = %+v", doc.Verification.Steps)
	}
	if strings.Contains(fmt.Sprint(doc), "root-order") || strings.Contains(fmt.Sprint(doc), "old-one") {
		t.Fatal("export leaked rejected or root evidence")
	}
}

func TestAcceptedAttemptRejectsAmbiguousAndMismatchedEvidence(t *testing.T) {
	t.Run("mismatched digest", func(t *testing.T) {
		dir, _, _ := makeMultiAttemptFixture(t)
		path := filepath.Join(dir, "report.json")
		var rep repair.Report
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &rep); err != nil {
			t.Fatal(err)
		}
		rep.Attempts[1].PatchDigest = digest("not-the-diff")
		rep.PatchDigest = rep.Attempts[1].PatchDigest
		writeJSON(t, path, rep)
		if _, err := exportFixture(t, dir); err == nil {
			t.Fatal("expected a digest mismatch failure")
		}
	})

	t.Run("missing observations", func(t *testing.T) {
		dir, _, _ := makeMultiAttemptFixture(t)
		if err := os.Remove(filepath.Join(dir, "attempts", "attempt-2", "observations.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "observations.json")); err != nil {
			t.Fatal("test fixture lost its root fallback file")
		}
		if _, err := exportFixture(t, dir); err == nil {
			t.Fatal("expected missing observations to fail instead of using root files")
		}
	})

	t.Run("missing attempt directory", func(t *testing.T) {
		dir, _, _ := makeMultiAttemptFixture(t)
		if err := os.RemoveAll(filepath.Join(dir, "attempts", "attempt-2")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "observations.json")); err != nil {
			t.Fatal("test fixture lost its root fallback file")
		}
		if _, err := exportFixture(t, dir); err == nil {
			t.Fatal("expected a missing attempt directory to fail instead of using root files")
		}
	})

	t.Run("stale observations from another attempt", func(t *testing.T) {
		dir, _, _ := makeMultiAttemptFixture(t)
		path := filepath.Join(dir, "attempts", "attempt-2", "observations.json")
		var obs map[string]any
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &obs); err != nil {
			t.Fatal(err)
		}
		items := obs["observations"].([]any)
		items[len(items)-1].(map[string]any)["CaptureID"] = "CAP-one"
		writeJSON(t, path, obs)
		if _, err := exportFixture(t, dir); err == nil {
			t.Fatal("expected stale observations to fail the capture-identity check")
		}
	})

	t.Run("wrong oracle verdict", func(t *testing.T) {
		dir, _, _ := makeMultiAttemptFixture(t)
		path := filepath.Join(dir, "attempts", "attempt-2", "eval.json")
		var ev map[string]any
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatal(err)
		}
		ev["oracle"].(map[string]any)["verdict"] = unsafeVerdict
		writeJSON(t, path, ev)
		if _, err := exportFixture(t, dir); err == nil {
			t.Fatal("expected a wrong oracle verdict failure")
		}
	})

	t.Run("wrong bundle digest", func(t *testing.T) {
		dir, _, _ := makeMultiAttemptFixture(t)
		path := filepath.Join(dir, "attempts", "attempt-2", "bundle.json")
		var b map[string]any
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Fatal(err)
		}
		b["patch_digest"] = digest("wrong-bundle")
		writeJSON(t, path, b)
		if _, err := exportFixture(t, dir); err == nil {
			t.Fatal("expected a bundle digest mismatch failure")
		}
	})

	t.Run("missing judge evidence", func(t *testing.T) {
		dir, _, _ := makeMultiAttemptFixture(t)
		if err := os.Remove(filepath.Join(dir, "attempts", "attempt-2", "eval.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := exportFixture(t, dir); err == nil {
			t.Fatal("expected missing judge evidence to fail")
		}
	})

	t.Run("missing judge step", func(t *testing.T) {
		dir, _, _ := makeMultiAttemptFixture(t)
		path := filepath.Join(dir, "report.json")
		var rep repair.Report
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &rep); err != nil {
			t.Fatal(err)
		}
		rep.Attempts[1].Steps = rep.Attempts[1].Steps[:4]
		writeJSON(t, path, rep)
		if _, err := exportFixture(t, dir); err == nil {
			t.Fatal("expected a missing judge step failure")
		}
	})

	t.Run("multiple accepted attempts", func(t *testing.T) {
		dir, _, _ := makeMultiAttemptFixture(t)
		path := filepath.Join(dir, "report.json")
		var rep repair.Report
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &rep); err != nil {
			t.Fatal(err)
		}
		rep.Attempts[0].Ladder.Accepted = true
		writeJSON(t, path, rep)
		if _, err := exportFixture(t, dir); err == nil {
			t.Fatal("expected multiple accepted attempts to fail")
		}
	})
}

func TestExportRecordedEvidenceIsSanitizedAndTyped(t *testing.T) {
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "demo.json")
	err := run(
		filepath.Join(root, "evidence", "m4-live", "f3"),
		out,
		"recorded",
		"evidence/m4-live/f3",
		"M4 recorded F3 hosted run",
		filepath.Join(root, "evidence", "m4-unsafe-f3", "eval.json"),
		"evidence/m4-unsafe-f3/eval.json",
		"",
		filepath.Join(root, "evidence", "m4-live", "f3", "report.json"),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"/Users/", "/private/var", "/var/folders", "sk-or-v1", "reference_fix", "mutations"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("export leaked %q", forbidden)
		}
	}
	var doc export
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Run.EvidenceLayout != "legacy_recorded_root" {
		t.Fatalf("layout = %q", doc.Run.EvidenceLayout)
	}
	if doc.Incident.Fault != "F3" || doc.Incident.ID == "" {
		t.Fatalf("incident = %+v", doc.Incident)
	}
	if doc.Incident.OriginalOrderID == "" || doc.Incident.VerificationOrderID == "" || doc.Incident.OriginalOrderID == doc.Incident.VerificationOrderID {
		t.Fatalf("order identity = original %q verification %q", doc.Incident.OriginalOrderID, doc.Incident.VerificationOrderID)
	}
	if len(doc.Incident.Observations) == 0 {
		t.Fatal("observations were not exported")
	}
	if doc.Incident.Observations[len(doc.Incident.Observations)-1].CaptureID != "CAP-SCRIPT-0001" {
		t.Fatalf("capture identity missing: %+v", doc.Incident.Observations[len(doc.Incident.Observations)-1])
	}
	if doc.Run.Kind != "recorded" || doc.Run.Model == "" || doc.Run.PromptTokens == 0 {
		t.Fatalf("run = %+v", doc.Run)
	}
	var duplicate *invariant
	for i := range doc.PreRepair.Invariants {
		if doc.PreRepair.Invariants[i].ID == "I13" {
			duplicate = &doc.PreRepair.Invariants[i]
		}
	}
	if duplicate == nil || duplicate.Counterexample == nil {
		t.Fatal("the pre-repair duplicate-fulfillment counterexample was not exported")
	}
	if duplicate.Counterexample.EffectCount != 2 || len(duplicate.Counterexample.Effects) != 2 {
		t.Fatalf("I13 counterexample = %+v", duplicate.Counterexample)
	}
	if doc.Oracle.Verdict != verifiedVerdict {
		t.Fatalf("oracle = %+v", doc.Oracle)
	}
	if doc.Unsafe.Verdict != unsafeVerdict || doc.Unsafe.FreshGate.Executed {
		t.Fatalf("unsafe = %+v", doc.Unsafe)
	}
	if doc.HumanReview.DeveloperTreeApplied || doc.HumanReview.Deployed || !doc.HumanReview.CandidatePatchAppliedInDisposableWorktree {
		t.Fatalf("human review = %+v", doc.HumanReview)
	}
	want, err := os.ReadFile(filepath.Join(root, "evidence", "m4-unsafe-f3", "patch.diff"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Unsafe.Diff != string(want) || doc.Unsafe.DiffDigest != diffDigest(want) {
		t.Fatalf("unsafe diff does not match the recorded seeded patch bytes")
	}
	if len(doc.Unsafe.ChangedFiles) != 1 || doc.Unsafe.ChangedFiles[0] != "apps/api/internal/merchant/webhook.go" {
		t.Fatalf("unsafe changed files = %v", doc.Unsafe.ChangedFiles)
	}
}

func TestReadUnsafeDiffIsOptionalAndValidated(t *testing.T) {
	dir := t.TempDir()
	evalPath := filepath.Join(dir, "eval.json")
	got, err := readUnsafeDiff(evalPath)
	if err != nil || got.text != "" || got.files != nil {
		t.Fatalf("missing sibling diff should export nothing: %+v %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "patch.diff"), []byte("not a diff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readUnsafeDiff(evalPath); err == nil {
		t.Fatal("a non-diff sibling must be rejected")
	}
	ok := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n"
	if err := os.WriteFile(filepath.Join(dir, "patch.diff"), []byte(ok), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = readUnsafeDiff(evalPath)
	if err != nil || len(got.files) != 1 || got.files[0] != "x.go" || got.digest != diffDigest([]byte(ok)) {
		t.Fatalf("valid diff: %+v %v", got, err)
	}
}
