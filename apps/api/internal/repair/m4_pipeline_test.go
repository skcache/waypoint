package repair_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/oracle"
	"github.com/skcache/waypoint/apps/api/internal/repair"
)

func diagnosisReplyF2() string {
	d := map[string]any{
		"root_cause":          "the legacy capture-ambiguity policy treats an unreadable 2xx response as a definite failure",
		"mechanism":           "legacyAmbiguity only marks provider 5xx responses ambiguous; a lost or unreadable capture response returns false, so the service closes the capture operation as REJECTED even though the provider may have applied it",
		"suspected_files":     []string{"apps/api/internal/merchant/ambiguity.go"},
		"supporting_evidence": []string{"EV-1", "EV-2"},
		"invariants_at_risk":  []string{"I5", "I6"},
		"proposed_fix":        "classify capture failures with paypal.AmbiguousOutcome so transport failures and unreadable 2xx responses preserve the operation as UNKNOWN",
		"confidence_reasons":  []string{"the provider observation shows an applied capture with a dropped response while the journal records the same attempt as REJECTED"},
		"uncertainties":       []string{"provider interactions are scripted (tier F)"},
	}
	data, _ := json.Marshal(d)
	return string(data)
}

func fixF2PatchReply() string {
	oldFn := "func (legacyAmbiguity) Ambiguous(err error) bool {\n" +
		"\tvar apiErr *paypal.APIError\n" +
		"\tif errors.As(err, &apiErr) {\n" +
		"\t\treturn apiErr.StatusCode >= 500\n" +
		"\t}\n" +
		"\treturn false\n" +
		"}"
	p := map[string]any{
		"summary": "treat every failure that may have applied as ambiguous",
		"edits": []map[string]any{
			{
				"path":      "apps/api/internal/merchant/ambiguity.go",
				"old_text":  "import (\n\t\"errors\"\n\n\t\"github.com/skcache/waypoint/apps/api/internal/payments/paypal\"\n)",
				"new_text":  "import \"github.com/skcache/waypoint/apps/api/internal/payments/paypal\"",
				"rationale": "the policy no longer needs errors.As once it delegates to the canonical classifier",
			},
			{
				"path":      "apps/api/internal/merchant/ambiguity.go",
				"old_text":  oldFn,
				"new_text":  "func (legacyAmbiguity) Ambiguous(err error) bool { return paypal.AmbiguousOutcome(err) }",
				"rationale": "an unreadable 2xx response, timeout or transport failure must preserve the capture operation as UNKNOWN",
			},
		},
	}
	data, _ := json.Marshal(p)
	return string(data)
}

// TestPipelineRepairsF2WithABoundaryDouble runs the full pipeline against the
// lost-response family. The scripted provider still drops the first capture
// response inside the candidate worktree, so acceptance requires the repaired
// ambiguity classification, not a disabled injector.
func TestPipelineRepairsF2WithABoundaryDouble(t *testing.T) {
	_, _, ev := pipelineFixture(t, "F2")
	root := repoRoot(t)
	before, err := repair.RepoStatus(root)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	allow := repair.AllowlistForFault("F2")
	out := tempOut(t)
	model := &scriptedModel{replies: []string{diagnosisReplyF2(), fixF2PatchReply()}}
	rep := repair.Run(context.Background(), repair.Options{
		RepoRoot: root, BaseRef: "HEAD", Fault: "F2", OutDir: out, Runner: sharedSandbox(t),
		Mode: "oracle", MaxAttempts: 1, Model: model, Evidence: ev,
		Allow: allow, RepoStatusBefore: before,
		Revision: "test-revision", RevisionSource: "test", IncidentID: ev.IncidentID,
	})
	if rep.Status != repair.StatusAccepted {
		t.Fatalf("status = %s, want ACCEPTED; attempts=%d last=%+v", rep.Status, len(rep.Attempts), lastAttempt(rep))
	}
	att := rep.Attempts[0]
	if !strings.Contains(att.Diff, "paypal.AmbiguousOutcome(err)") {
		t.Fatalf("diff does not show the ambiguity fix:\n%s", att.Diff)
	}
	if att.Eval == nil || att.Eval.OracleVerdict != string(oracle.VerdictVerified) {
		t.Fatalf("eval = %+v, want oracle VERIFIED", att.Eval)
	}
}

func diagnosisReplyF3() string {
	d := map[string]any{
		"root_cause":          "the legacy webhook handler keys fulfillment by event id and writes event status onto the order",
		"mechanism":           "every distinct event id describing the same capture inserts a separate fulfillment effect, and a late ORDER.APPROVED event overwrites CAPTURED_VERIFIED, so redelivery and reordering change financial state",
		"suspected_files":     []string{"apps/api/internal/merchant/webhook.go"},
		"supporting_evidence": []string{"EV-1", "EV-2"},
		"invariants_at_risk":  []string{"I8", "I13", "I14"},
		"proposed_fix":        "use one stable fulfillment key per order and never mutate payment state from an event payload",
		"confidence_reasons":  []string{"the journal shows two fulfillment rows and a verified capture rolled back to APPROVED after webhook delivery"},
		"uncertainties":       []string{"the verifier is a trusted fixture verifier (tier F), not a PayPal signature check"},
	}
	data, _ := json.Marshal(d)
	return string(data)
}

func fixF3PatchReply() string {
	oldStatus := "if _, err := tx.Exec(ctx, `UPDATE merchant_orders SET status = $2, updated_at = now() WHERE id = $1::uuid`, orderID, StatusApproved); err != nil {\n\t\t\treturn err\n\t\t}"
	p := map[string]any{
		"summary": "deduplicate fulfillment by order and stop event-driven status writes",
		"edits": []map[string]any{
			{
				"path":      "apps/api/internal/merchant/webhook.go",
				"old_text":  "VALUES ($1::uuid, $2, $2, $3)",
				"new_text":  "VALUES ($1::uuid, 'FULFILL', $2, $3)",
				"rationale": "one stable effect key per order makes several events for one capture produce one fulfillment",
			},
			{
				"path":      "apps/api/internal/merchant/webhook.go",
				"old_text":  oldStatus,
				"new_text":  "// Events never advance payment state; the provider read is authoritative.\n\t\t_ = orderID",
				"rationale": "an older approval event must not rewind a verified capture",
			},
		},
	}
	data, _ := json.Marshal(p)
	return string(data)
}

// TestPipelineRepairsF3WithABoundaryDouble runs the webhook/fulfillment family
// through the pipeline. Duplicate, second-id, late and concurrent deliveries
// stay active in the candidate worktree; acceptance requires one fulfillment
// effect on a still-verified order.
func TestPipelineRepairsF3WithABoundaryDouble(t *testing.T) {
	_, _, ev := pipelineFixture(t, "F3")
	root := repoRoot(t)
	before, err := repair.RepoStatus(root)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	allow := repair.AllowlistForFault("F3")
	out := tempOut(t)
	model := &scriptedModel{replies: []string{diagnosisReplyF3(), fixF3PatchReply()}}
	rep := repair.Run(context.Background(), repair.Options{
		RepoRoot: root, BaseRef: "HEAD", Fault: "F3", OutDir: out, Runner: sharedSandbox(t),
		Mode: "oracle", MaxAttempts: 1, Model: model, Evidence: ev,
		Allow: allow, RepoStatusBefore: before,
		Revision: "test-revision", RevisionSource: "test", IncidentID: ev.IncidentID,
	})
	if rep.Status != repair.StatusAccepted {
		t.Fatalf("status = %s, want ACCEPTED; attempts=%d last=%+v", rep.Status, len(rep.Attempts), lastAttempt(rep))
	}
	att := rep.Attempts[0]
	if !strings.Contains(att.Diff, "'FULFILL'") {
		t.Fatalf("diff does not show the stable effect key:\n%s", att.Diff)
	}
	if att.Eval == nil || att.Eval.OracleVerdict != string(oracle.VerdictVerified) {
		t.Fatalf("eval = %+v, want oracle VERIFIED", att.Eval)
	}
}

func unsafeF3PatchReply() string {
	oldStatus := "if _, err := tx.Exec(ctx, `UPDATE merchant_orders SET status = $2, updated_at = now() WHERE id = $1::uuid`, orderID, StatusApproved); err != nil {\n\t\t\treturn err\n\t\t}"
	p := map[string]any{
		"summary": "stop event-driven status writes but keep per-event fulfillment keys",
		"edits": []map[string]any{{
			"path":      "apps/api/internal/merchant/webhook.go",
			"old_text":  oldStatus,
			"new_text":  "// Events never advance payment state.\n\t\t_ = orderID",
			"rationale": "addresses the visible status regression",
		}},
	}
	data, _ := json.Marshal(p)
	return string(data)
}

// TestPipelineRejectsSeededUnsafeF3Candidate documents the M4 unsafe trap. The
// candidate removes the visible status regression but keeps a per-event
// fulfillment key: every handler call returns success and the order ends
// CAPTURED_VERIFIED, while two distinct events for one capture create two
// fulfillment effects. Only the independent oracle rejects it (I13).
//
// This candidate is SEEDED by us, not proposed by a hosted model.
func TestPipelineRejectsSeededUnsafeF3Candidate(t *testing.T) {
	_, _, ev := pipelineFixture(t, "F3")
	root := repoRoot(t)
	before, err := repair.RepoStatus(root)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	allow := repair.AllowlistForFault("F3")
	out := tempOut(t)
	model := &scriptedModel{replies: []string{diagnosisReplyF3(), unsafeF3PatchReply()}}
	rep := repair.Run(context.Background(), repair.Options{
		RepoRoot: root, BaseRef: "HEAD", Fault: "F3", OutDir: out, Runner: sharedSandbox(t),
		Mode: "oracle", MaxAttempts: 1, Model: model, Evidence: ev,
		Allow: allow, RepoStatusBefore: before,
		Revision: "test-revision", RevisionSource: "test", IncidentID: ev.IncidentID,
	})
	if rep.Status != repair.StatusNotVerified {
		t.Fatalf("status = %s, want NOT_VERIFIED for the unsafe candidate; last=%+v", rep.Status, lastAttempt(rep))
	}
	att := lastAttempt(rep)
	if att.Number == 0 {
		t.Fatalf("no attempt recorded")
	}
	// This candidate fails the trusted financial gate before the judge runs:
	// the gate asserts exactly one fulfillment effect. Defense in depth — the
	// same patch is independently rejected by the judge with I13 when the
	// binary is exercised outside that test (evidence/m4-unsafe-f3/eval.json).
	if att.Ladder.TestsPassed {
		t.Fatalf("unsafe candidate passed the trusted tests; it must be rejected: %+v", att.Ladder)
	}
	var testsStep string
	for _, st := range att.Steps {
		if st.Step == "tests" {
			testsStep = st.Output
		}
	}
	if !strings.Contains(testsStep, "fulfillment effects = 2") {
		t.Fatalf("trusted test did not report the duplicate fulfillment:\n%s", testsStep)
	}
}
