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
