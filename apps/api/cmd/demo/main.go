// Command demo renders and verifies the deterministic Waypoint repair demo.
//
// It reads the artifacts produced by:
//
//	go run ./cmd/repair -fault F3 -replay evidence/m4-live/f3 -out <dir> -attempts 1
//
// and fails if any required fact is missing or mislabelled. It never calls a
// model, PayPal, or PostgreSQL; the recorded replay is labelled as such.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/skcache/waypoint/apps/api/internal/repair"
)

type evalDoc struct {
	Accepted    bool   `json:"accepted"`
	Reason      string `json:"reason"`
	OrderStatus string `json:"order_status"`
	Oracle      struct {
		Verdict    string `json:"verdict"`
		Invariants []struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Summary string `json:"summary"`
		} `json:"invariants"`
	} `json:"oracle"`
}

func main() {
	repo := flag.String("repo", "", "repository root (default: detect)")
	out := flag.String("out", "", "repair artifact directory (required)")
	unsafe := flag.String("unsafe", "evidence/m4-unsafe-f3/eval.json", "seeded unsafe-candidate eval")
	unsafeLog := flag.String("unsafe-test-log", "", "fresh trusted-gate test log (optional)")
	web := flag.String("web", "apps/web/src/data/m5.json", "workbench data")
	flag.Parse()

	root, err := repoRoot(*repo)
	if err != nil {
		fail("%v", err)
	}
	if *out == "" {
		fail("-out is required")
	}
	outDir := *out
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(root, outDir)
	}
	unsafePath := resolve(root, *unsafe)
	webPath := resolve(root, *web)

	report, err := readReport(filepath.Join(outDir, "report.json"))
	if err != nil {
		fail("%v", err)
	}
	pre, err := readEval(filepath.Join(outDir, "pre-repair-eval.json"))
	if err != nil {
		fail("%v", err)
	}
	unsafeEval, err := readEval(unsafePath)
	if err != nil {
		fail("%v", err)
	}

	if report.Status != repair.StatusAccepted {
		fail("report status = %s, want %s", report.Status, repair.StatusAccepted)
	}
	if !report.Final.Accepted || !report.Final.BuildPassed || !report.Final.TestsPassed {
		fail("final ladder is incomplete: %+v", report.Final)
	}
	if report.Final.OracleVerdict != "VERIFIED_FOR_TESTED_SCENARIOS" {
		fail("oracle verdict = %q, want VERIFIED_FOR_TESTED_SCENARIOS", report.Final.OracleVerdict)
	}
	if len(report.Attempts) == 0 || strings.TrimSpace(report.Attempts[0].Diff) == "" {
		fail("no generated diff recorded")
	}
	if !strings.Contains(report.Attempts[0].Diff, "'FULFILL'") {
		fail("diff does not contain the stable FULFILL effect key:\n%s", report.Attempts[0].Diff)
	}
	if !report.HumanApprovalRequired || report.Applied || report.Deployed {
		fail("human-review boundary is wrong: approval=%v applied=%v deployed=%v",
			report.HumanApprovalRequired, report.Applied, report.Deployed)
	}
	if !containsReplayLimitation(report.Limitations) {
		fail("report does not label the recorded replay")
	}
	if len(report.ModelCalls) == 0 {
		fail("no model provenance recorded")
	}
	for _, call := range report.ModelCalls {
		if !strings.HasPrefix(call.Model, "recorded:") {
			fail("model call %q is not labelled as recorded", call.Model)
		}
	}
	if pre.Oracle.Verdict != "UNSAFE" {
		fail("pre-repair oracle = %q, want UNSAFE", pre.Oracle.Verdict)
	}
	if unsafeEval.Oracle.Verdict != "UNSAFE" {
		fail("seeded unsafe oracle = %q, want UNSAFE", unsafeEval.Oracle.Verdict)
	}
	var i13 *struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Summary string `json:"summary"`
	}
	for i := range unsafeEval.Oracle.Invariants {
		if unsafeEval.Oracle.Invariants[i].ID == "I13" {
			i13 = &unsafeEval.Oracle.Invariants[i]
		}
	}
	if i13 == nil || i13.Status != "FAIL" || !strings.Contains(i13.Summary, "2 fulfillment effects") {
		fail("seeded unsafe candidate has no I13 duplicate-fulfillment counterexample")
	}

	// The presentation export and the artifacts must not carry a host path or
	// a credential. Check the files the demo actually points a reviewer at.
	for _, path := range []string{
		filepath.Join(outDir, "report.json"),
		filepath.Join(outDir, "report.md"),
		filepath.Join(outDir, "pre-repair-eval.json"),
		filepath.Join(outDir, "attempts", "attempt-1", "patch.diff"),
		filepath.Join(outDir, "attempts", "attempt-1", "eval.json"),
		unsafePath,
		webPath,
	} {
		if err := scanForPrivateData(path); err != nil {
			fail("%v", err)
		}
	}
	if err := checkWorkbench(webPath); err != nil {
		fail("%v", err)
	}
	freshDetail := ""
	if unsafeLog != nil && *unsafeLog != "" {
		raw, err := os.ReadFile(*unsafeLog)
		if err != nil {
			fail("read fresh unsafe test log: %v", err)
		}
		text := string(raw)
		if !strings.Contains(text, "--- PASS: TestPipelineRejectsSeededUnsafeF3Candidate") {
			fail("fresh unsafe test did not pass")
		}
		if !strings.Contains(text, "fulfillment effects = 2") {
			fail("fresh unsafe test log does not contain the duplicate-fulfillment evidence")
		}
		freshDetail = "trusted gate rejected the candidate (2 fulfillment effects)"
	}

	fmt.Println("WAYPOINT — DETERMINISTIC REPAIR DEMO")
	fmt.Println("model: recorded replay only; no hosted model call was made; all provider evidence is tier F")
	fmt.Println()
	fmt.Printf("1. DEFECT REPRODUCED   %s · evidence tier %s (scripted fixture)\n", report.Fault, report.EvidenceTier)
	fmt.Printf("   pre-repair: %s · order %s\n", strings.TrimSpace(pre.Oracle.Verdict+" — "+pre.Reason), pre.OrderStatus)
	fmt.Printf("   incident: %s\n", report.IncidentID)
	fmt.Println()
	fmt.Printf("2. RECORDED MODEL      %s\n", modelNames(report))
	if report.Diagnosis != nil {
		fmt.Printf("   diagnosis: %s\n", oneLine(report.Diagnosis.RootCause))
		fmt.Printf("   proposed fix: %s\n", oneLine(report.Diagnosis.ProposedFix))
	}
	fmt.Printf("   recorded tokens: %d prompt / %d completion (provenance, not measured now)\n",
		report.PromptTokens, report.CompletionTokens)
	fmt.Println()
	fmt.Printf("3. GENERATED DIFF      %d changed file(s), patch %s\n", len(report.Attempts[0].Changed), short(report.PatchDigest))
	fmt.Println(indent(report.Attempts[0].Diff, "   "))
	fmt.Println()
	fmt.Printf("4. CANDIDATE VERIFIED  patch=%v build=%v trusted_tests=%v\n",
		report.Final.PatchApplied, report.Final.BuildPassed, report.Final.TestsPassed)
	if report.Sandbox != "" {
		fmt.Printf("   sandbox: %s\n", report.Sandbox)
	}
	fmt.Printf("   base: %s\n", short(report.BaseRevision))
	fmt.Println()
	fmt.Printf("5. INDEPENDENT ORACLE  %s\n", report.Final.OracleVerdict)
	fmt.Println()
	fmt.Printf("6a. HISTORICAL ORACLE  %s · I13: %s\n", unsafeEval.Oracle.Verdict, i13.Summary)
	fmt.Printf("    source: %s (recorded M4 independent judge; not executed now)\n", displayPath(root, unsafePath))
	fmt.Println("    seeded by the repository, not generated by the model")
	if freshDetail != "" {
		fmt.Printf("6b. FRESH EXECUTION    %s\n", freshDetail)
	} else {
		fmt.Println("6b. FRESH EXECUTION    not run for this artifact")
	}
	fmt.Println()
	fmt.Printf("7. HUMAN BOUNDARY      %s\n", report.Status)
	fmt.Printf("   candidate_patch_applied_in_disposable_worktree=%v\n", report.Final.PatchApplied)
	fmt.Printf("   developer_tree_applied=%v deployed=%v human_approval_required=%v\n",
		report.Applied, report.Deployed, report.HumanApprovalRequired)
	fmt.Println("   no merge, push, deploy, capture, refund or payment retry")
	fmt.Println()
	fmt.Printf("run export: %s (sanitized; this run)\n", "apps/web/public/demo/local.json")
	fmt.Printf("benchmark data: %s (H1 recorded repair; H7 unsafe reference)\n", rel(root, webPath))
	fmt.Printf("artifacts: %s\n", rel(root, outDir))
	fmt.Println("open the run: make dev-web  ->  http://localhost:3000/demo")
}

func readReport(path string) (*repair.Report, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read report: %w", err)
	}
	var rep repair.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, fmt.Errorf("decode report: %w", err)
	}
	return &rep, nil
}

func readEval(path string) (*evalDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read eval %s: %w", filepath.Base(path), err)
	}
	var eval evalDoc
	if err := json.Unmarshal(raw, &eval); err != nil {
		return nil, fmt.Errorf("decode eval %s: %w", filepath.Base(path), err)
	}
	return &eval, nil
}

func containsReplayLimitation(limitations []string) bool {
	for _, line := range limitations {
		if strings.Contains(line, "replayed verbatim") && strings.Contains(line, "no hosted model") {
			return true
		}
	}
	return false
}

func modelNames(rep *repair.Report) string {
	seen := map[string]bool{}
	var names []string
	for _, call := range rep.ModelCalls {
		if !seen[call.Model] {
			seen[call.Model] = true
			names = append(names, call.Model)
		}
	}
	return strings.Join(names, ", ")
}

var privatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`/Users/[^\s"\x60]+`),
	regexp.MustCompile(`/private/var/[^\s"\x60]+`),
	regexp.MustCompile(`sk-or-v1-[A-Za-z0-9]+`),
	regexp.MustCompile(`OPENROUTER_API_KEY`),
	regexp.MustCompile(`postgres(?:ql)?://[^\s"']+:[^\s"'@]+@`),
}

func scanForPrivateData(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("scan %s: %w", filepath.Base(path), err)
	}
	for _, re := range privatePatterns {
		if re.Match(raw) {
			return fmt.Errorf("%s contains private data matching %s", filepath.Base(path), re.String())
		}
	}
	return nil
}

func checkWorkbench(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read workbench data: %w", err)
	}
	var doc struct {
		Cases []struct {
			Case struct {
				ID string `json:"id"`
			} `json:"case"`
			PreRepair struct {
				Verdict string `json:"verdict"`
			} `json:"pre_repair"`
			Runs map[string]struct {
				Accepted bool `json:"accepted"`
			} `json:"runs"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("decode workbench data: %w", err)
	}
	seenH1, seenH7 := false, false
	for _, row := range doc.Cases {
		switch {
		case row.Case.ID == "H1-identity-rewrite":
			seenH1 = true
			if !row.Runs["B1"].Accepted || !row.Runs["B2"].Accepted {
				return fmt.Errorf("workbench H1 is not recorded as accepted under B1 and B2")
			}
		case strings.HasPrefix(row.Case.ID, "H7-"):
			seenH7 = true
			if row.PreRepair.Verdict != "UNSAFE" {
				return fmt.Errorf("workbench H7 pre-repair verdict = %q, want UNSAFE", row.PreRepair.Verdict)
			}
		}
	}
	if !seenH1 || !seenH7 {
		return fmt.Errorf("workbench data is missing H1 or H7 navigation cases")
	}
	return nil
}

func indent(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func displayPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(path)
}

func oneLine(v string) string {
	return strings.Join(strings.Fields(v), " ")
}

func short(v string) string {
	if len(v) > 16 {
		return v[:16] + "…"
	}
	return v
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return path
}

func resolve(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func repoRoot(flagValue string) (string, error) {
	start := flagValue
	if start == "" {
		var err error
		start, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "apps", "api", "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("cannot find repository root from %s", start)
		}
		dir = parent
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "demo: "+format+"\n", args...)
	os.Exit(1)
}
