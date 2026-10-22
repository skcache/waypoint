// Command judgecheck is the TRUSTED financial verifier. It imports the oracle
// and pgx only — never the merchant fixture, the replay harness or any other
// candidate-linked package — so candidate code cannot run in this process,
// forge its output, or terminate it early.
//
// It re-reads the durable journal from PostgreSQL with the oracle's own SQL,
// combines it with the provider observations recorded by the fixture run, and
// writes the acceptance decision. A missing or malformed observation file is
// a NOT-VERIFIED outcome, never an accepted one.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skcache/waypoint/apps/api/internal/oracle"
)

type observationsFile struct {
	Fault        string               `json:"fault"`
	OrderID      string               `json:"order_id"`
	Observations []oracle.ProviderObs `json:"observations"`
}

type evalOut struct {
	Accepted          bool          `json:"accepted"`
	Reason            string        `json:"reason"`
	OrderStatus       string        `json:"order_status"`
	ClaimedCaptureID  string        `json:"claimed_capture_id,omitempty"`
	ProviderCaptureID string        `json:"provider_capture_id,omitempty"`
	Oracle            oracle.Result `json:"oracle"`
}

func main() {
	fault := flag.String("fault", "F1", "fixture defect that was exercised")
	in := flag.String("in", "", "observations.json produced by the fixture run (required)")
	out := flag.String("out", "", "directory for eval.json and the proof bundle (required)")
	dsn := flag.String("dsn", os.Getenv("TEST_DATABASE_URL"), "PostgreSQL test DSN")
	baseRevision := flag.String("base-revision", "", "candidate base commit, resolved by the trusted orchestrator")
	patchDigest := flag.String("patch-digest", "", "sha256 of the applied candidate diff")
	flag.Parse()
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: judgecheck -fault F1|F4 -in <observations.json> -out <dir> [-dsn dsn] [-base-revision sha] [-patch-digest sha256:…]")
		os.Exit(2)
	}
	if err := run(context.Background(), *fault, *in, *out, *dsn, *baseRevision, *patchDigest); err != nil {
		fmt.Fprintln(os.Stderr, "judgecheck:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, fault, in, outDir, dsn, baseRevision, patchDigest string) error {
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	// The judge writes eval.json on EVERY path, so a candidate-forged file in
	// the writable artifact directory is always overwritten.
	write := func(ev *oracle.Evidence, res oracle.Result, accepted bool, reason string, orderID, claimed, provider string) error {
		e := evalOut{
			Accepted: accepted, Reason: reason, OrderStatus: statusOf(ev),
			ClaimedCaptureID: claimed, ProviderCaptureID: provider, Oracle: res,
		}
		data, err := json.MarshalIndent(e, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, "eval.json"), append(data, 0x0a), 0o600); err != nil {
			return err
		}
		if ev != nil && baseRevision != "" {
			bundle := oracle.BuildBundle(res, ev, baseRevision, "orchestrator-base")
			bundle.PatchDigest = patchDigest
			bdata, err := json.MarshalIndent(bundle, "", "  ")
			if err != nil {
				return err
			}
			_ = os.WriteFile(filepath.Join(outDir, "bundle.json"), append(bdata, 0x0a), 0o600)
		}
		if accepted {
			fmt.Printf("judge: ACCEPTED — %s\n", reason)
			return nil
		}
		fmt.Printf("judge: NOT VERIFIED — %s\n", reason)
		return fmt.Errorf("verification failed: %s", reason)
	}

	var of observationsFile
	raw, err := os.ReadFile(in)
	if err != nil {
		res := oracle.Result{Verdict: oracle.VerdictInconclusive}
		_ = write(nil, res, false, "the fixture run produced no observations file: "+err.Error(), "", "", "")
		return fmt.Errorf("read observations: %w", err)
	}
	if err := json.Unmarshal(raw, &of); err != nil {
		res := oracle.Result{Verdict: oracle.VerdictInconclusive}
		_ = write(nil, res, false, "the observations file is unreadable: "+err.Error(), "", "", "")
		return fmt.Errorf("decode observations: %w", err)
	}
	if of.OrderID == "" {
		res := oracle.Result{Verdict: oracle.VerdictInconclusive}
		_ = write(nil, res, false, "the fixture run produced no order id", "", "", "")
		return fmt.Errorf("missing order id")
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		res := oracle.Result{Verdict: oracle.VerdictInconclusive}
		_ = write(nil, res, false, "judge could not open the journal database", "", "", "")
		return err
	}
	defer pool.Close()
	local, err := oracle.LoadLocal(ctx, pool, []string{of.OrderID})
	if err != nil {
		res := oracle.Result{Verdict: oracle.VerdictInconclusive}
		_ = write(nil, res, false, "judge could not read the durable journal: "+err.Error(), "", "", "")
		return err
	}

	ev := &oracle.Evidence{
		Scenario: oracle.ScenarioMeta{
			ID: "candidate-" + fault, Title: "Candidate fixture journey (" + fault + ")",
			Family: fault, Description: "checkout → approval → capture under the Docker sandbox",
			EvidenceTier: "F",
		},
		Candidate:     "candidate",
		Orders:        local.Orders,
		Operations:    local.Operations,
		Events:        local.Events,
		Fulfillments:  local.Fulfillments,
		WebhookEvents: local.WebhookEvents,
		Provider:      of.Observations,
	}
	res := oracle.Evaluate(ev)

	claimed := claimCaptureID(local.Operations)
	provider := providerCaptureID(of.Observations)
	status := statusOf(ev)
	fulfillments := 0
	for _, f := range local.Fulfillments {
		if f.OrderID == of.OrderID {
			fulfillments++
		}
	}
	i6 := invariantStatus(res, "I6")
	reason := ""
	accepted := false
	switch {
	case len(local.Orders) == 0:
		reason = "the durable journal contains no order for the fixture run"
	case status != "CAPTURED_VERIFIED":
		reason = "order did not reach CAPTURED_VERIFIED (status " + status + ")"
	case res.Verdict != oracle.VerdictVerified:
		reason = "oracle verdict is " + string(res.Verdict) + ", not VERIFIED_FOR_TESTED_SCENARIOS"
	case i6 != oracle.StatusPass:
		reason = "the capture identity invariant I6 is " + string(i6)
	case provider == "":
		reason = "no applied provider capture observation carries a capture id"
	case claimed != provider:
		reason = fmt.Sprintf("the journal certifies capture %q but the provider issued %q", claimed, provider)
	case fault == "F3" && fulfillments != 1:
		reason = fmt.Sprintf("the merchant recorded %d fulfillment effects for the payment, want exactly 1", fulfillments)
	default:
		accepted = true
		reason = "provider-issued capture identity, amount, currency and terminal status match the journal; the oracle verified the tested scenario"
		if fault == "F3" {
			reason = "exactly one fulfillment effect is backed by an authenticated capture event; the oracle verified the tested scenario"
		}
	}
	if !accepted {
		if cp := firstCounterexample(res); cp != "" {
			reason += "; counterexample: " + cp
		}
	}
	return write(ev, res, accepted, reason, of.OrderID, claimed, provider)
}

func statusOf(ev *oracle.Evidence) string {
	if ev == nil || len(ev.Orders) == 0 {
		return ""
	}
	return ev.Orders[0].Status
}

func claimCaptureID(ops []oracle.LocalOp) string {
	for i := len(ops) - 1; i >= 0; i-- {
		if ops[i].Type == "CAPTURE" && ops[i].Status == "SUCCEEDED" && ops[i].ProviderCaptureID != "" {
			return ops[i].ProviderCaptureID
		}
	}
	return ""
}

func providerCaptureID(obs []oracle.ProviderObs) string {
	for _, o := range obs {
		if o.Method == "capture" && o.Applied && o.CaptureID != "" {
			return o.CaptureID
		}
	}
	return ""
}

func invariantStatus(res oracle.Result, id string) oracle.Status {
	for _, inv := range res.Invariants {
		if inv.ID == id {
			return inv.Status
		}
	}
	return oracle.StatusNotApplicable
}

func firstCounterexample(res oracle.Result) string {
	for _, inv := range res.Counterexamples() {
		return fmt.Sprintf("%s: %s", inv.ID, inv.Summary)
	}
	return ""
}

var _ = strings.TrimSpace
