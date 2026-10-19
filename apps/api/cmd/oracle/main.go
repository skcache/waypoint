// Command oracle runs the Waypoint V2 M2 deterministic scenario suite against
// the merchant fixture and prints the independent financial-correctness
// verdict for each (scenario, candidate) pair, writing redacted proof bundles.
//
// It is the reproducible operator surface for the oracle:
//
//	go run ./cmd/oracle -out /tmp/waypoint-oracle
//
// Requires a PostgreSQL test database (name must end in _test); no provider
// credentials are involved — every scenario uses the deterministic scripted
// provider (tier F). Real PayPal evidence comes only from the M1 acceptance
// run and is referenced, never re-executed here.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/skcache/waypoint/apps/api/internal/oracle"
	"github.com/skcache/waypoint/apps/api/internal/replay"
)

func main() {
	outDir := flag.String("out", "", "directory for proof bundles (required)")
	dsn := flag.String("dsn", os.Getenv("TEST_DATABASE_URL"), "PostgreSQL test DSN (database name must end in _test)")
	only := flag.String("scenario", "all", "scenario id to run, or 'all'")
	flag.Parse()

	if *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: oracle -out <bundle-dir> [-scenario id] [-dsn dsn]")
		os.Exit(2)
	}
	// The evaluated revision is DETECTED (Go build VCS stamp, then git), never
	// supplied by the caller: a bundle must not be able to claim a revision
	// the run did not actually execute.
	revision, revisionSource := oracle.DetectRevision()
	fmt.Printf("evaluated revision: %s (source: %s)\n", revision, revisionSource)
	if err := run(*outDir, *dsn, *only, revision, revisionSource); err != nil {
		fmt.Fprintln(os.Stderr, "oracle:", err)
		os.Exit(1)
	}
}

func run(outDir, dsn, only, revision, revisionSource string) error {
	ctx := context.Background()
	h, err := replay.OpenHarness(ctx, dsn)
	if err != nil {
		return err
	}
	defer h.Close()

	scenarios := replay.StandardScenarios
	if only != "all" {
		sc, ok := replay.ScenarioByID(only)
		if !ok {
			return fmt.Errorf("unknown scenario %q", only)
		}
		scenarios = []replay.Scenario{sc}
	}

	verdicts := map[oracle.Verdict]int{}
	unexpected := 0
	for _, sc := range scenarios {
		cand := replay.CandidateReference
		if sc.Unsafe {
			switch sc.ID {
			case "unsafe-identity-swap":
				cand = replay.CandidateUnsafeIdentitySwap
			case "unsafe-accept-mismatch":
				cand = replay.CandidateUnsafeAcceptMismatch
			default:
				return fmt.Errorf("unsafe scenario %q has no seed candidate", sc.ID)
			}
		}
		if err := h.ResetFixtureState(ctx); err != nil {
			return err
		}
		rec, err := replay.Run(ctx, h.NewEnv(sc, cand), sc, cand)
		if err != nil {
			return fmt.Errorf("scenario %s: %w", sc.ID, err)
		}
		ev, err := rec.ToEvidence(ctx, h.Pool, sc, cand)
		if err != nil {
			return err
		}
		result := oracle.Evaluate(&ev)
		bundle := oracle.BuildBundle(result, &ev, revision, revisionSource)
		dir := fmt.Sprintf("%s/%s-%s", outDir, sc.ID, cand)
		if _, err := oracle.WriteBundle(dir, bundle, &ev); err != nil {
			return err
		}

		fmt.Printf("%-34s %-26s verdict=%-32s expected=%s\n", sc.ID, cand, result.Verdict, sc.ExpectedVerdict)
		for _, inv := range result.Invariants {
			fmt.Printf("    %-3s %-10s %s\n", inv.ID, inv.Status, inv.Summary)
		}
		if result.Verdict != sc.ExpectedVerdict {
			unexpected++
			fmt.Printf("    !! UNEXPECTED: oracle returned %s, scenario declares %s\n", result.Verdict, sc.ExpectedVerdict)
		}
		verdicts[result.Verdict]++
	}
	fmt.Printf("\nsummary: %d scenarios | verified=%d unsafe=%d inconclusive=%d | unexpected=%d\n",
		len(scenarios), verdicts[oracle.VerdictVerified], verdicts[oracle.VerdictUnsafe], verdicts[oracle.VerdictInconclusive], unexpected)
	fmt.Printf("bundles written to %s\n", outDir)

	if unexpected > 0 {
		return fmt.Errorf("%d scenario(s) diverged from their declared expected verdict", unexpected)
	}
	return nil
}
