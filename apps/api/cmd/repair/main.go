// Command repair is Waypoint V2's M3 operator surface:
//
//	go run ./cmd/repair -fault F1 -out /tmp/waypoint-repair
//
// It reproduces a fixture incident, asks the configured hosted model for a
// diagnosis and a scoped patch, applies the patch in a disposable worktree,
// and runs independent verification (build, trusted tests, and the fixture
// journey judged by the oracle). Nothing is applied to the working tree,
// committed, pushed or deployed; the output is a repair report for a human.
//
// Without a configured model the command exits 3 with status NOT_CONFIGURED
// and never fabricates a model run.
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
	"time"

	"github.com/skcache/waypoint/apps/api/internal/llm"
	"github.com/skcache/waypoint/apps/api/internal/oracle"
	"github.com/skcache/waypoint/apps/api/internal/repair"
	"github.com/skcache/waypoint/apps/api/internal/replay"
)

func main() {
	repo := flag.String("repo", "", "repository root (default: detect from the current directory)")
	baseRef := flag.String("base-ref", "HEAD", "git revision the candidate patch is applied to")
	fault := flag.String("fault", merchantFaultDefault, "fixture defect family to investigate (F1 or F4)")
	outDir := flag.String("out", "", "directory for the repair report and attempt artifacts (required)")
	mode := flag.String("mode", "oracle", "repair mode: oracle (bounded feedback retries) or baseline (single shot, no oracle feedback)")
	attempts := flag.Int("attempts", 2, "maximum candidate attempts in oracle mode (hard cap 3)")
	dsn := flag.String("dsn", os.Getenv("TEST_DATABASE_URL"), "PostgreSQL test DSN (database name must end in _test)")
	checkModel := flag.Bool("check-model", false, "validate hosted model configuration and exit (no model call)")
	compare := flag.Bool("compare-baseline", false, "also run a single-shot baseline with the same model and compare outcomes")
	dumpPrompt := flag.Bool("dump-prompt", false, "write the exact diagnosis prompt to the output directory and exit (no model call)")
	flag.Parse()

	cfg, cfgErr := llm.ConfigFromEnv(os.Getenv)
	if *checkModel {
		if cfgErr != nil {
			fmt.Println("NOT_CONFIGURED")
			fmt.Fprintln(os.Stderr, cfgErr)
			os.Exit(3)
		}
		fmt.Printf("CONFIGURED model=%s base=%s timeout=%s\n", cfg.Model, cfg.BaseURL, cfg.Timeout)
		os.Exit(0)
	}
	if *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: repair -fault F1|F2|F3|F4 -out <dir> [-mode oracle|baseline] [-attempts 2] [-base-ref HEAD]")
		os.Exit(2)
	}
	switch *fault {
	case "F1", "F2", "F3", "F4":
	default:
		fmt.Fprintf(os.Stderr, "invalid -fault %q (want F1, F2, F3 or F4)\n", *fault)
		os.Exit(2)
	}
	if cfgErr != nil {
		// No hosted model: write the honest artifact and stop. No fake AI run.
		root, _ := repoRoot(*repo)
		rev, src := oracle.DetectRevision()
		rep := repair.NotConfigured(repair.Options{
			RepoRoot: root, BaseRef: *baseRef, Fault: *fault, Mode: *mode,
			OutDir: *outDir, Revision: rev, RevisionSource: src,
			IncidentID: "M3-INC-" + *fault + "-not-configured",
		}, cfgErr)
		fmt.Printf("status: %s\n", rep.Status)
		fmt.Fprintf(os.Stderr, "hosted model not configured: %v\n", cfgErr)
		os.Exit(3)
	}

	if err := run(cfg, *repo, *baseRef, *fault, *outDir, *mode, *attempts, *dsn, *compare, *dumpPrompt); err != nil {
		fmt.Fprintln(os.Stderr, "repair:", err)
		os.Exit(1)
	}
}

const merchantFaultDefault = "F1"

func run(cfg llm.Config, repoFlag, baseRef, fault, outDir, mode string, attempts int, dsn string, compare, dumpPrompt bool) error {
	ctx := context.Background()
	root, err := repoRoot(repoFlag)
	if err != nil {
		return err
	}
	// The candidate base is a concrete commit resolved from the trusted repo,
	// never the model's or the candidate's claim.
	baseSHA, err := resolveCommit(root, baseRef)
	if err != nil {
		return err
	}
	rev, revSource := baseSHA, "git-resolve"

	model, err := repair.NewLLMModel(cfg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}

	dsn, err = replay.ResolveDSN(dsn)
	if err != nil {
		return err
	}
	h, err := replay.OpenHarness(ctx, dsn)
	if err != nil {
		return err
	}
	defer h.Close()
	if err := h.ResetFixtureState(ctx); err != nil {
		return err
	}

	// 1. Reproduce the incident on the pinned revision.
	preRun, err := replay.RunFixtureFault(ctx, h, fault)
	if err != nil {
		return fmt.Errorf("reproduce incident: %w", err)
	}
	preVerdict, err := replay.JudgeFixtureRun(ctx, h, preRun)
	if err != nil {
		return err
	}
	if preVerdict.Accepted {
		return fmt.Errorf("the base revision already satisfies the acceptance rules (status %s); there is no incident to repair", preVerdict.OrderStatus)
	}

	allow := repair.AllowlistForFault(fault)
	ev, err := repair.BuildEvidence(ctx, h, preRun, root, rev, allow)
	if err != nil {
		return err
	}
	if err := ev.ValidateForModel(); err != nil {
		return err
	}
	incidentID := ev.IncidentID

	incidentJSON, _ := json.MarshalIndent(ev.Incident, "", "  ")
	_ = os.WriteFile(filepath.Join(outDir, "incident.json"), append(incidentJSON, '\n'), 0o600)
	preJSON, _ := json.MarshalIndent(preVerdict, "", "  ")
	_ = os.WriteFile(filepath.Join(outDir, "pre-repair-eval.json"), append(preJSON, '\n'), 0o600)

	fmt.Printf("incident: %s\n", incidentID)
	fmt.Printf("pre-repair: status=%s accepted=%v — %s\n", preVerdict.OrderStatus, preVerdict.Accepted, preVerdict.Reason)

	if dumpPrompt {
		if err := os.WriteFile(filepath.Join(outDir, "diagnosis-system.txt"), []byte(repair.DiagnosisSystemPrompt()), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, "diagnosis-user.txt"), []byte(repair.DiagnosisUserPrompt(ev)), 0o600); err != nil {
			return err
		}
		fmt.Printf("prompt written to %s (no model call)\n", outDir)
		return nil
	}

	statusBefore, err := repair.RepoStatus(root)
	if err != nil {
		return err
	}

	started := time.Now()
	rep := repair.Run(ctx, repair.Options{
		RepoRoot: root, BaseRef: baseSHA, Fault: fault, OutDir: outDir,
		Mode: mode, MaxAttempts: attempts, Model: model, Evidence: ev, Allow: allow,
		RepoStatusBefore: statusBefore, Revision: rev, RevisionSource: revSource,
		IncidentID: incidentID,
	})

	if compare && mode != "baseline" {
		baseOut := filepath.Join(outDir, "baseline")
		if err := os.MkdirAll(baseOut, 0o700); err != nil {
			return err
		}
		baseRep := repair.Run(ctx, repair.Options{
			RepoRoot: root, BaseRef: baseSHA, Fault: fault, OutDir: baseOut,
			Mode: "baseline", MaxAttempts: 1, Model: model, Evidence: ev, Allow: allow,
			RepoStatusBefore: statusBefore, Revision: rev, RevisionSource: revSource,
			IncidentID: incidentID,
		})
		rep.Baseline = &repair.Baseline{
			Mode:      "baseline (single shot, no oracle feedback)",
			Attempts:  len(baseRep.Attempts),
			Accepted:  baseRep.Status == repair.StatusAccepted,
			Stage:     baseRep.Final.Stage(),
			LatencyMS: baseRep.FinishedAt.Sub(baseRep.StartedAt).Milliseconds(),
		}
		_ = repair.WriteReport(rep, outDir)
	}

	fmt.Printf("mode: %s | attempts: %d | final stage: %s\n", rep.Mode, len(rep.Attempts), rep.Final.Stage())
	for _, a := range rep.Attempts {
		fmt.Printf("attempt %d: stage=%s oracle=%s accepted=%v", a.Number, a.Ladder.Stage(), oracleOrNone(a.Eval), a.Ladder.Accepted)
		if a.Error != "" {
			fmt.Printf(" error=%s", a.Error)
		}
		fmt.Println()
	}
	fmt.Printf("status: %s\nreport: %s\nrepair total: %dms\n", rep.Status, filepath.Join(outDir, "report.md"), time.Since(started).Milliseconds())

	if rep.Status == repair.StatusAccepted {
		return nil
	}
	return fmt.Errorf("no candidate passed independent verification (status %s)", rep.Status)
}

// resolveCommit pins a ref (HEAD, tag, SHA) to a commit SHA.
func resolveCommit(repo, ref string) (string, error) {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", ref+"^{commit}").Output()
	if err != nil {
		return "", fmt.Errorf("resolve -base-ref %q to a commit: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func oracleOrNone(e *repair.EvalSummary) string {
	if e == nil {
		return "-"
	}
	return e.OracleVerdict
}

func repoRoot(flagValue string) (string, error) {
	dir := flagValue
	if dir == "" {
		dir = "."
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("determine repository root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
