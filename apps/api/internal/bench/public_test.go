package bench_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/bench"
	"github.com/skcache/waypoint/apps/api/internal/repair"
)

// TestPublicProjectionExcludesEvaluatorData builds a dataset full of sentinel
// evaluator values and proves the workbench projection cannot carry them.
func TestPublicProjectionExcludesEvaluatorData(t *testing.T) {
	ds := &bench.Dataset{
		SchemaVersion: 1, BenchmarkID: "b", Digest: "sha256:x", BaseSHA: "abc",
		Cases: []bench.CaseReport{{
			Case: bench.Case{
				ID: "case-1", Family: "f", Title: "t", Mode: "verified",
				Journey:          "SECRET-JOURNEY",
				Mutations:        []bench.Mutation{{Path: "p", Old: "SECRET-OLD", New: "SECRET-NEW"}},
				GateFault:        "SECRET-GATE",
				ExpectedCategory: "SECRET-CATEGORY",
				ReferenceFix:     []bench.Mutation{{Path: "p", Old: "REF-OLD", New: "REF-NEW"}},
				Facts:            []string{"SECRET-FACT"},
			},
			MutationSHA: "abc",
			PreRepair:   bench.EvalView{Verdict: "UNSAFE", OrderStatus: "APPROVED"},
			Runs: map[string]bench.Result{
				"B1": {CaseID: "case-1", Condition: "B1", Status: "REPAIRED", Accepted: true, Artifacts: "/Users/secret/artifacts"},
			},
		}},
	}
	run := &bench.PublicRun{PublicResult: bench.PublicResultFrom(ds.Cases[0].Runs["B1"]), Diff: "+ allowed repair diff", Report: &repair.Report{Status: "ACCEPTED_FOR_HUMAN_REVIEW"}}
	doc := bench.Project(ds, map[string]map[string]*bench.PublicRun{"case-1": {"B1": run}})
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{
		"SECRET-JOURNEY", "SECRET-OLD", "SECRET-NEW", "SECRET-GATE", "SECRET-CATEGORY",
		"REF-OLD", "REF-NEW", "SECRET-FACT", "/Users/", `"artifacts"`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public projection leaked %q:\n%s", forbidden, text)
		}
	}
	for _, required := range []string{"+ allowed repair diff", "ACCEPTED_FOR_HUMAN_REVIEW", `"case-1"`} {
		if !strings.Contains(text, required) {
			t.Fatalf("public projection dropped reviewable data %q", required)
		}
	}
}

// TestCommittedWebDataHasNoEvaluatorData guards the file the browser loads.
func TestCommittedWebDataHasNoEvaluatorData(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "apps", "web", "src", "data", "m5.json"))
	if err != nil {
		t.Fatalf("read workbench data: %v", err)
	}
	if bytes.Contains(raw, []byte("/Users/")) || bytes.Contains(raw, []byte(`\\Users\\`)) {
		t.Fatal("workbench data contains an absolute host path")
	}
	for _, key := range []string{`"mutations"`, `"reference_fix"`, `"expected_category"`, `"gate_fault"`, `"journey"`, `"artifacts"`} {
		if bytes.Contains(raw, []byte(key)) {
			t.Fatalf("workbench data contains evaluator-only field %s", key)
		}
	}
	var doc struct {
		Views map[string]map[string]struct {
			Diff string `json:"diff"`
		} `json:"views"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode workbench data: %v", err)
	}
	if len(doc.Views["H1-identity-rewrite"]["B1"].Diff) == 0 {
		t.Fatal("workbench data dropped the reviewable H1 diff")
	}
}

// TestCommittedResultsHasNoEvaluatorFields keeps the dataset published in the
// repository free of mutation text, reference fixes, expectations and host
// paths as well.
func TestCommittedResultsHasNoEvaluatorFields(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "evidence", "m5", "results.json"))
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	if bytes.Contains(raw, []byte("/Users/")) {
		t.Fatal("results.json contains an absolute host path")
	}
	for _, key := range []string{`"Mutations"`, `"ReferenceFix"`, `"ExpectedCategory"`, `"GateFault"`, `"Artifacts"`} {
		if bytes.Contains(raw, []byte(key)) {
			t.Fatalf("results.json contains evaluator-only field %s", key)
		}
	}
}

// TestCommittedEvidenceIsConsistent pins the demo story to the committed data.
func TestCommittedEvidenceIsConsistent(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "evidence", "m5", "results.json"))
	if err != nil {
		t.Fatalf("read results: %v", err)
	}
	var ds struct {
		Model struct {
			ID string `json:"id"`
		} `json:"model"`
		Cases []struct {
			Case struct {
				ID string `json:"id"`
			} `json:"case"`
			Runs map[string]struct {
				Status   string `json:"status"`
				Accepted bool   `json:"accepted"`
			} `json:"runs"`
		} `json:"cases"`
		Totals struct {
			ByCondition map[string]struct {
				SafeAccepted int `json:"safe_accepted"`
				NotRun       int `json:"not_run"`
			} `json:"by_condition"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(raw, &ds); err != nil {
		t.Fatal(err)
	}
	if ds.Model.ID != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("model = %q", ds.Model.ID)
	}
	byID := map[string]map[string]struct {
		Status   string `json:"status"`
		Accepted bool   `json:"accepted"`
	}{}
	for _, c := range ds.Cases {
		byID[c.Case.ID] = c.Runs
	}
	for _, cond := range []string{"B1", "B2"} {
		if !byID["H1-identity-rewrite"][cond].Accepted {
			t.Fatalf("H1 %s is not recorded as accepted", cond)
		}
		if byID["H2-webhook-conflict"][cond].Status != "ERROR" {
			t.Fatalf("H2 %s status = %q, want ERROR", cond, byID["H2-webhook-conflict"][cond].Status)
		}
		for _, id := range []string{"H3-currency-mismatch", "H4-pending-capture", "H5-early-fulfillment", "H6-ambiguous-reconcile"} {
			if _, ok := byID[id][cond]; ok {
				t.Fatalf("%s %s should be NOT RUN", id, cond)
			}
		}
	}
	if got := ds.Totals.ByCondition["B1"].NotRun; got != 4 {
		t.Fatalf("B1 not_run = %d, want 4", got)
	}
	if got := ds.Totals.ByCondition["B1"].SafeAccepted; got != 1 {
		t.Fatalf("B1 safe_accepted = %d, want 1", got)
	}
}
