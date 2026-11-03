package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/repair"
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

func TestFinalAttemptPrefersTheAcceptedCandidate(t *testing.T) {
	rep := &repair.Report{Attempts: []repair.Attempt{
		{Ladder: repair.Ladder{Accepted: false}, Diff: "rejected"},
		{Ladder: repair.Ladder{Accepted: true}, Diff: "accepted"},
	}}
	att := finalAttempt(rep)
	if att == nil || att.Diff != "accepted" {
		t.Fatalf("final attempt = %+v", att)
	}
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
	if doc.Incident.Fault != "F3" || doc.Incident.ID == "" {
		t.Fatalf("incident = %+v", doc.Incident)
	}
	if len(doc.Incident.Observations) == 0 {
		t.Fatal("observations were not exported")
	}
	if doc.Run.Kind != "recorded" || doc.Run.Model == "" || doc.Run.PromptTokens == 0 {
		t.Fatalf("run = %+v", doc.Run)
	}
	if doc.Oracle.Verdict != "VERIFIED_FOR_TESTED_SCENARIOS" {
		t.Fatalf("oracle = %+v", doc.Oracle)
	}
	if doc.Unsafe.Verdict != "UNSAFE" || doc.Unsafe.FreshGate.Executed {
		t.Fatalf("unsafe = %+v", doc.Unsafe)
	}
	if doc.HumanReview.DeveloperTreeApplied || doc.HumanReview.Deployed || !doc.HumanReview.CandidatePatchAppliedInDisposableWorktree {
		t.Fatalf("human review = %+v", doc.HumanReview)
	}
}
