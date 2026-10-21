// Command repaircheck is the trusted verification surface for the M3 repair
// pipeline: it drives the merchant fixture (compiled from the workspace it is
// built in) through one injected-defect journey and prints the independent
// financial verdict. The repair agent cannot modify this command or the
// oracle.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/skcache/waypoint/apps/api/internal/oracle"
	"github.com/skcache/waypoint/apps/api/internal/replay"
)

func main() {
	fault := flag.String("fault", "F1", "fixture defect to exercise (F1 or F4)")
	out := flag.String("out", "", "directory for eval.json and the proof bundle (required)")
	dsn := flag.String("dsn", os.Getenv("TEST_DATABASE_URL"), "PostgreSQL test DSN (database name must end in _test)")
	baseRevision := flag.String("base-revision", "", "candidate base commit, resolved by the trusted orchestrator")
	patchDigest := flag.String("patch-digest", "", "sha256 of the applied candidate diff")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "usage: repaircheck -fault F1|F4 -out <dir> [-dsn dsn]")
		os.Exit(2)
	}
	if err := run(*fault, *out, *dsn, *baseRevision, *patchDigest); err != nil {
		fmt.Fprintln(os.Stderr, "repaircheck:", err)
		os.Exit(1)
	}
}

func run(fault, outDir, dsn, baseRevision, patchDigest string) error {
	ctx := context.Background()
	h, err := replay.OpenHarness(ctx, dsn)
	if err != nil {
		return err
	}
	defer h.Close()
	if err := h.ResetFixtureState(ctx); err != nil {
		return err
	}

	verdict, err := replay.EvaluateFixture(ctx, h, fault)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(verdict, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "eval.json"), append(data, 0x0a), 0o600); err != nil {
		return err
	}

	revision, source := baseRevision, "orchestrator-base"
	if revision == "" {
		revision, source = oracle.DetectRevision()
	}
	if revision == "" || revision == "unknown" {
		// A proof bundle must not claim an unidentified source identity.
		return fmt.Errorf("repaircheck: candidate base revision is unknown; refusing to write the proof bundle")
	}
	bundle := oracle.BuildBundle(verdict.Oracle, &verdict.Evidence, revision, source)
	bundle.PatchDigest = patchDigest
	bdata, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "bundle.json"), append(bdata, 0x0a), 0o600); err != nil {
		return err
	}

	if verdict.Accepted {
		fmt.Printf("fixture %s: ACCEPTED — %s\n", fault, verdict.Reason)
		fmt.Printf("oracle: %s (revision %s, %s)\n", verdict.Oracle.Verdict, revision, source)
		return nil
	}
	fmt.Printf("fixture %s: NOT VERIFIED — %s\n", fault, verdict.Reason)
	fmt.Printf("oracle: %s (revision %s, %s)\n", verdict.Oracle.Verdict, revision, source)
	os.Exit(1)
	return nil
}
