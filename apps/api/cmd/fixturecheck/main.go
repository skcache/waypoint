// Command fixturecheck runs the merchant fixture journey under test and
// records ONLY the raw provider observations to a file. It is linked with the
// candidate package, so it is not trusted to decide anything: the independent
// judge (cmd/judgecheck) re-reads the durable journal and evaluates the
// evidence in a process that never loads candidate code.
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

type evidenceFile struct {
	Fault        string               `json:"fault"`
	OrderID      string               `json:"order_id"`
	Observations []oracle.ProviderObs `json:"observations"`
}

func main() {
	fault := flag.String("fault", "F1", "fixture defect to exercise (F1 or F4)")
	out := flag.String("out", "", "directory for observations.json (required)")
	dsn := flag.String("dsn", os.Getenv("TEST_DATABASE_URL"), "PostgreSQL test DSN (database name must end in _test)")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "usage: fixturecheck -fault F1|F4 -out <dir> [-dsn dsn]")
		os.Exit(2)
	}
	if err := run(context.Background(), *fault, *out, *dsn); err != nil {
		fmt.Fprintln(os.Stderr, "fixturecheck:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, fault, outDir, dsn string) error {
	h, err := replay.OpenHarness(ctx, dsn)
	if err != nil {
		return err
	}
	defer h.Close()
	if err := h.ResetFixtureState(ctx); err != nil {
		return err
	}
	run, err := replay.RunFixtureFault(ctx, h, fault)
	if err != nil {
		return err
	}
	obs := make([]oracle.ProviderObs, 0, len(run.Observations))
	for _, o := range run.Observations {
		obs = append(obs, oracle.ProviderObs{
			Seq: o.Seq, At: o.At, Method: o.Method, OrderRef: o.OrderRef,
			RequestID: o.RequestID, DurableBeforeCall: o.DurableBeforeCall,
			SentAmount: o.SentAmount, SentCurrency: o.SentCurrency,
			Applied: o.Applied, Replayed: o.Replayed, ResponseDropped: o.ResponseDropped,
			DefiniteRefusal: o.DefiniteRefusal, OrderStatus: o.OrderStatus,
			CaptureID: o.CaptureID, CaptureStatus: o.CaptureStatus,
			CaptureAmount: o.CaptureAmount, CaptureCurrency: o.CaptureCurrency,
			Stale: o.Stale, Source: o.Source, Tier: o.Tier,
		})
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(evidenceFile{Fault: fault, OrderID: run.OrderID, Observations: obs}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "observations.json"), append(data, '\n'), 0o600)
}
