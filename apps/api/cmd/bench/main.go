// Command bench freezes and runs the M5 held-out repair benchmark. It writes
// the machine-readable dataset and the workbench's static data file from the
// actual run artifacts; nothing in the results file is hand-edited.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/skcache/waypoint/apps/api/internal/bench"
	"github.com/skcache/waypoint/apps/api/internal/llm"
	"github.com/skcache/waypoint/apps/api/internal/repair"
)

func main() {
	repo := flag.String("repo", "", "repository root (default: detect)")
	base := flag.String("base", "", "base revision (default: HEAD)")
	out := flag.String("out", "evidence/m5", "artifact directory (repo-relative)")
	web := flag.String("web", "apps/web/src/data", "workbench data directory (repo-relative)")
	caseIDs := flag.String("case", "", "comma-separated case ids (default: all)")
	condition := flag.String("condition", "", "B0, B1, B2, or empty for all")
	freeze := flag.Bool("freeze", false, "write the corpus freeze file and exit")
	dsn := flag.String("dsn", "", "benchmark PostgreSQL DSN (default: derive *_test from DATABASE_URL)")
	webonly := flag.Bool("webonly", false, "regenerate the workbench data from an existing results.json and exit")
	resume := flag.Bool("resume", false, "merge this invocation into an existing results.json")
	flag.Parse()

	root, err := repoRoot(*repo)
	if err != nil {
		fatal(err)
	}
	baseSHA := *base
	if baseSHA == "" {
		baseSHA = gitRev(root, "HEAD")
	}
	if *freeze {
		if err := freezeCorpus(root, baseSHA, filepath.Join(root, *out)); err != nil {
			fatal(err)
		}
		fmt.Printf("frozen: benchmark=%s digest=%s base=%s\n", "waypoint-m5-heldout-v1", bench.Digest(), baseSHA)
		return
	}

	if *webonly {
		outDir := filepath.Join(root, *out)
		raw, err := os.ReadFile(filepath.Join(outDir, "results.json"))
		if err != nil {
			fatal(err)
		}
		var ds bench.Dataset
		if err := json.Unmarshal(raw, &ds); err != nil {
			fatal(err)
		}
		if err := writeWebData(filepath.Join(root, *web), outDir, &ds); err != nil {
			fatal(err)
		}
		fmt.Println("web data regenerated")
		return
	}
	dsnValue := *dsn
	if dsnValue == "" {
		dsnValue, err = deriveBenchDSN()
		if err != nil {
			fatal(err)
		}
	}
	cfg, cfgErr := llm.ConfigFromEnv(os.Getenv)
	if cfgErr != nil {
		fmt.Printf("model: NOT_CONFIGURED (%v); B0 only\n", cfgErr)
	}
	var ids []string
	if *caseIDs != "" {
		ids = strings.Split(*caseIDs, ",")
	}
	opts := bench.Options{
		RepoRoot: root, BaseSHA: baseSHA, OutDir: filepath.Join(root, *out),
		WebDataDir: filepath.Join(root, *web), DSN: dsnValue, Model: cfg,
		Condition: *condition, CaseIDs: ids, MaxAttemptsB2: 2, Resume: *resume,
	}
	ds, err := bench.Run(context.Background(), opts)
	if err != nil {
		fatal(err)
	}
	outDir := opts.OutDir
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		fatal(err)
	}
	if err := writeResults(outDir, ds); err != nil {
		fatal(err)
	}
	if err := writeWebData(opts.WebDataDir, outDir, ds); err != nil {
		fatal(err)
	}
	fmt.Printf("results: %s\n", filepath.Join(outDir, "results.json"))
	printTotals(ds)
}

func printTotals(ds *bench.Dataset) {
	for _, cond := range []string{"B0", "B1", "B2"} {
		t := ds.Totals.ByCondition[cond]
		if t == nil {
			continue
		}
		fmt.Printf("%s: safe_accepted=%d verified=%d refusals=%d rejected=%d errors=%d false_acceptances=%d\n",
			cond, t.SafeAccepted, t.VerifiedAccepted, t.SafeRefusals, t.Rejected, t.Errors, t.FalseAcceptances)
	}
}

func freezeCorpus(root, base, outDir string) error {
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	type freezeCase struct {
		ID      string `json:"id"`
		Family  string `json:"family"`
		Title   string `json:"title"`
		Journey string `json:"journey"`
		Mode    string `json:"mode"`
		Exposed bool   `json:"exposed"`
	}
	var list []freezeCase
	for _, c := range bench.Cases() {
		list = append(list, freezeCase{c.ID, c.Family, c.Title, c.Journey, c.Mode, c.Exposed})
	}
	doc := map[string]any{
		"schema_version": 1,
		"benchmark_id":   "waypoint-m5-heldout-v1",
		"base_sha":       base,
		"digest":         bench.Digest(),
		"cases":          list,
		"note":           "Frozen before hosted evaluation. Reference fixes and expected categories are evaluator-only and never enter model prompts.",
	}
	data, _ := json.MarshalIndent(doc, "", "  ")
	return os.WriteFile(filepath.Join(outDir, "benchmark.json"), append(data, '\n'), 0o600)
}

func writeResults(outDir string, ds *bench.Dataset) error {
	data, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "results.json"), append(data, '\n'), 0o600)
}

// webRun is the compact artifact the read-only workbench renders.
type webRun struct {
	bench.Result
	Report       *repair.Report  `json:"report,omitempty"`
	Diff         string          `json:"diff,omitempty"`
	PreEval      json.RawMessage `json:"pre_eval,omitempty"`
	Eval         json.RawMessage `json:"eval,omitempty"`
	Diag         json.RawMessage `json:"diagnosis_reply,omitempty"`
	Patch        json.RawMessage `json:"patch_reply,omitempty"`
	Observations json.RawMessage `json:"observations,omitempty"`
}

type webDoc struct {
	bench.Dataset
	Views map[string]map[string]*webRun `json:"views"`
}

func writeWebData(webDir, outDir string, ds *bench.Dataset) error {
	doc := webDoc{Dataset: *ds, Views: map[string]map[string]*webRun{}}
	for _, cr := range ds.Cases {
		doc.Views[cr.Case.ID] = map[string]*webRun{}
		for cond, r := range cr.Runs {
			v := &webRun{Result: r}
			base := filepath.Join(outDir, "cases", cr.Case.ID, cond)
			// B1/B2 reports are written by the repair pipeline; B0 has only a result.
			if data, err := os.ReadFile(filepath.Join(base, "report.json")); err == nil {
				var rep repair.Report
				if json.Unmarshal(data, &rep) == nil {
					v.Report = &rep
				}
			}
			if data, err := os.ReadFile(filepath.Join(base, "attempts", "attempt-1", "patch.diff")); err == nil {
				v.Diff = string(data)
			}
			if data, err := os.ReadFile(filepath.Join(base, "attempts", "attempt-1", "eval.json")); err == nil {
				v.Eval = data
			}
			if data, err := os.ReadFile(filepath.Join(base, "diagnosis-reply.txt")); err == nil {
				v.Diag = data
			}
			if data, err := os.ReadFile(filepath.Join(base, "attempts", "attempt-1", "patch-reply.txt")); err == nil {
				v.Patch = data
			}
			if data, err := os.ReadFile(filepath.Join(outDir, "cases", cr.Case.ID, "pre-repair", "eval.json")); err == nil {
				v.PreEval = data
			}
			if data, err := os.ReadFile(filepath.Join(outDir, "cases", cr.Case.ID, "pre-repair", "observations.json")); err == nil {
				v.Observations = data
			}
			doc.Views[cr.Case.ID][cond] = v
		}
	}
	if err := os.MkdirAll(webDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(webDir, "m5.json"), append(data, '\n'), 0o644)
}

func repoRoot(flagValue string) (string, error) {
	if flagValue != "" {
		return filepath.Abs(flagValue)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, "apps", "api", "go.mod")); err == nil {
			return dir, nil
		}
		if dir == filepath.Dir(dir) {
			return "", fmt.Errorf("bench: repository root not found (pass -repo)")
		}
	}
}

func deriveBenchDSN() (string, error) {
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		raw = os.Getenv("DATABASE_URL")
	}
	if raw == "" {
		return "", fmt.Errorf("bench: no DSN; pass -dsn or set DATABASE_URL")
	}
	// Replace the database name with waypoint_bench_test. DSNs are used, never
	// printed.
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		tail := raw[i:]
		dbName := tail[1:]
		query := ""
		if q := strings.IndexByte(dbName, '?'); q >= 0 {
			query = dbName[q:]
			dbName = dbName[:q]
		}
		raw = raw[:i] + "/waypoint_bench_test" + query
	}
	return raw, nil
}

func gitRev(root, ref string) string {
	out, err := execCommand(root, "git", "rev-parse", ref)
	if err != nil {
		fatal(fmt.Errorf("git rev-parse %s: %w", ref, err))
	}
	return strings.TrimSpace(out)
}

func execCommand(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bench:", err)
	os.Exit(1)
}
