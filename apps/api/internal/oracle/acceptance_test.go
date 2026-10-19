// Acceptance suite for the M2 oracle: runs every deterministic scenario
// against the real merchant fixture in PostgreSQL, feeds the INDEPENDENT
// oracle, and asserts the verdict matches the scenario's declared
// expectation. The expectations were declared with the scenarios (never
// adjusted to fit an observed result).
package oracle_test

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/oracle"
	"github.com/skcache/waypoint/apps/api/internal/replay"
)

func openHarness(t *testing.T) *replay.Harness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dsn := os.Getenv("TEST_DATABASE_URL")
	h, err := replay.OpenHarnessForTests(ctx, dsn)
	if err != nil {
		if os.Getenv("REQUIRE_DATABASE_TESTS") != "" {
			t.Fatalf("database tests are REQUIRED but the oracle harness failed: %v", err)
		}
		t.Skipf("PostgreSQL test database not reachable (%v); start it with `docker compose up -d db`", err)
	}
	t.Cleanup(h.Close)
	return h
}

func candidateFor(t *testing.T, sc replay.Scenario) replay.Candidate {
	t.Helper()
	if !sc.Unsafe {
		return replay.CandidateReference
	}
	switch sc.ID {
	case "unsafe-identity-swap":
		return replay.CandidateUnsafeIdentitySwap
	case "unsafe-accept-mismatch":
		return replay.CandidateUnsafeAcceptMismatch
	}
	t.Fatalf("unsafe scenario %q declares no seed candidate", sc.ID)
	return ""
}

// runScenario executes one scenario and returns the oracle result + evidence.
func runScenario(t *testing.T, h *replay.Harness, sc replay.Scenario) (oracle.Result, oracle.Evidence, replay.RunRecord) {
	t.Helper()
	ctx := context.Background()
	cand := candidateFor(t, sc)
	if err := h.ResetFixtureState(ctx); err != nil {
		t.Fatalf("reset fixture state: %v", err)
	}
	rec, err := replay.Run(ctx, h.NewEnv(sc, cand), sc, cand)
	if err != nil {
		t.Fatalf("run scenario: %v", err)
	}
	ev, err := rec.ToEvidence(ctx, h.Pool, sc, cand)
	if err != nil {
		t.Fatalf("build evidence: %v", err)
	}
	result := oracle.Evaluate(&ev)
	return result, ev, rec
}

func invariantStatuses(r oracle.Result) map[string]oracle.Status {
	out := map[string]oracle.Status{}
	for _, inv := range r.Invariants {
		out[inv.ID] = inv.Status
	}
	return out
}

func dumpInvariants(t *testing.T, r oracle.Result) {
	t.Helper()
	for _, inv := range r.Invariants {
		t.Logf("  %s %-16s %s", inv.ID, inv.Status, inv.Summary)
	}
	for _, ce := range r.Counterexamples() {
		t.Logf("  counterexample %s: %v", ce.ID, ce.Counterexample)
	}
}

// TestOracleSuiteAgainstDeclaredExpectations is the M2 exit gate: for every
// scenario, the oracle's verdict must match the declared expectation, and the
// unsafe seeds must be rejected with concrete counterexamples.
func TestOracleSuiteAgainstDeclaredExpectations(t *testing.T) {
	h := openHarness(t)

	for _, sc := range replay.StandardScenarios {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			result, ev, _ := runScenario(t, h, sc)

			// The verdict must equal the declaration.
			if result.Verdict != sc.ExpectedVerdict {
				dumpInvariants(t, result)
				t.Fatalf("verdict = %s, scenario declares %s", result.Verdict, sc.ExpectedVerdict)
			}

			// Every result lists all twelve invariants — NA, never invented.
			if len(result.Invariants) != 12 {
				t.Fatalf("invariant count = %d, want 12", len(result.Invariants))
			}
			for _, inv := range result.Invariants {
				switch inv.Status {
				case oracle.StatusPass, oracle.StatusFail, oracle.StatusInconclusive, oracle.StatusNotApplicable:
				default:
					t.Fatalf("invariant %s carries invalid status %q", inv.ID, inv.Status)
				}
			}

			switch sc.ExpectedVerdict {
			case oracle.VerdictUnsafe:
				// UNSAFE requires concrete counterexamples; nothing else
				// suffices. Both seeds must also carry the specific
				// invariants their defect class violates.
				if len(result.Counterexamples()) == 0 {
					t.Fatal("UNSAFE verdict without a counterexample is not acceptable")
				}
				for _, ce := range result.Counterexamples() {
					if len(ce.Counterexample) == 0 {
						t.Fatalf("counterexample for %s has no concrete evidence payload", ce.ID)
					}
				}
				statuses := invariantStatuses(result)
				switch sc.ID {
				case "unsafe-identity-swap":
					for _, want := range []string{"I4", "I5", "I7"} {
						if statuses[want] != oracle.StatusFail {
							dumpInvariants(t, result)
							t.Fatalf("identity-swap seed must violate %s; status = %s", want, statuses[want])
						}
					}
				case "unsafe-accept-mismatch":
					if statuses["I6"] != oracle.StatusFail {
						dumpInvariants(t, result)
						t.Fatalf("accept-mismatch seed must violate I6; status = %s", statuses["I6"])
					}
				}
			case oracle.VerdictVerified:
				for _, inv := range result.Invariants {
					if inv.Status == oracle.StatusFail {
						dumpInvariants(t, result)
						t.Fatalf("VERIFIED verdict with a failing invariant %s", inv.ID)
					}
				}
			case oracle.VerdictInconclusive:
				// Missing proof must show up as INCONCLUSIVE on the
				// provider-evidence-dependent invariant, not as a pass.
				if invariantStatuses(result)["I6"] != oracle.StatusInconclusive {
					dumpInvariants(t, result)
					t.Fatalf("missing-provider-proof scenario must be INCONCLUSIVE on I6 (got %s)", invariantStatuses(result)["I6"])
				}
			}

			// The bundle must round-trip: write, re-read, validate.
			dir := t.TempDir()
			revision, revisionSource := oracle.DetectRevision()
			bundle := oracle.BuildBundle(result, &ev, revision, revisionSource)
			if _, err := oracle.WriteBundle(dir, bundle, &ev); err != nil {
				t.Fatalf("write bundle: %v", err)
			}
			raw, err := os.ReadFile(dir + "/bundle.json")
			if err != nil {
				t.Fatalf("read bundle: %v", err)
			}
			var back oracle.Bundle
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("bundle does not parse: %v", err)
			}
			if back.SchemaVersion != oracle.BundleSchemaVersion || back.Verdict != result.Verdict {
				t.Fatalf("bundle mismatch: schema=%d verdict=%s", back.SchemaVersion, back.Verdict)
			}
			if len(back.Invariants) != 12 || back.Applied {
				t.Fatalf("bundle invariants=%d applied=%v", len(back.Invariants), back.Applied)
			}
			if back.FixtureRevision == "" || back.FixtureRevision == "unknown" {
				t.Fatalf("bundle carries no detected revision: %q", back.FixtureRevision)
			}
			if back.RevisionSource == "" || back.RevisionSource == "unknown" {
				t.Fatalf("bundle does not say how its revision was determined: %q", back.RevisionSource)
			}
			if back.PayPalSandbox.Verified {
				t.Fatal("tier-F scenario bundle must not claim verified real Sandbox evidence")
			}
			if back.EvidenceDigest == "" {
				t.Fatal("bundle carries no evidence digest")
			}
			if regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`).MatchString(string(raw)) {
				t.Fatal("bundle leaked an email-shaped string")
			}
		})
	}
}

// The known-good reference implementation must pass its applicable checks in
// every non-unsafe scenario (no FAIL anywhere).
func TestReferenceImplementationPassesApplicableChecks(t *testing.T) {
	h := openHarness(t)
	for _, sc := range replay.StandardScenarios {
		if sc.Unsafe {
			continue
		}
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			result, _, _ := runScenario(t, h, sc)
			for _, inv := range result.Invariants {
				if inv.Status == oracle.StatusFail {
					dumpInvariants(t, result)
					t.Fatalf("reference implementation failed %s under %s", inv.ID, sc.ID)
				}
			}
		})
	}
}

// The unsafe seeds are rejected even when evaluated through the SAME pathway;
// this guards against the oracle silently special-casing them.
func TestUnsafeSeedsAreRejectedNotSpecialCased(t *testing.T) {
	h := openHarness(t)
	ctx := context.Background()

	// Run the identity-swap scenario twice: verdict must be stable.
	var verdicts []oracle.Verdict
	for i := 0; i < 2; i++ {
		sc, _ := replay.ScenarioByID("unsafe-identity-swap")
		result, _, _ := runScenario(t, h, sc)
		verdicts = append(verdicts, result.Verdict)
	}
	if verdicts[0] != oracle.VerdictUnsafe || verdicts[1] != oracle.VerdictUnsafe {
		t.Fatalf("identity-swap seed verdicts = %v, want UNSAFE twice (deterministic)", verdicts)
	}

	// And the refusal evidence must name both request ids.
	sc, _ := replay.ScenarioByID("unsafe-identity-swap")
	if err := h.ResetFixtureState(ctx); err != nil {
		t.Fatal(err)
	}
	cand := replay.CandidateUnsafeIdentitySwap
	rec, err := replay.Run(ctx, h.NewEnv(sc, cand), sc, cand)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := rec.ToEvidence(ctx, h.Pool, sc, cand)
	if err != nil {
		t.Fatal(err)
	}
	result := oracle.Evaluate(&ev)
	foundIdentityCe := false
	for _, ce := range result.Counterexamples() {
		if ce.ID != "I4" {
			continue
		}
		first, _ := ce.Counterexample["first_request_id"].(string)
		newID, _ := ce.Counterexample["new_request_id"].(string)
		if first != "" && newID != "" && first != newID {
			foundIdentityCe = true
		}
	}
	if !foundIdentityCe {
		dumpInvariants(t, result)
		t.Fatal("I4 counterexample must name both the preserved and the reissued request id")
	}
}

// Scenario invarian notes: the I10 refund invariant must be NOT_APPLICABLE in
// every M2 scenario (refunds are not exercised), never fabricated as PASS.
func TestRefundsAreNotApplicableInM2(t *testing.T) {
	h := openHarness(t)
	for _, sc := range replay.StandardScenarios {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			result, _, _ := runScenario(t, h, sc)
			if got := invariantStatuses(result)["I10"]; got != oracle.StatusNotApplicable {
				t.Fatalf("I10 = %s, want NOT_APPLICABLE (M2 executes no refunds)", got)
			}
		})
	}
}

// Evidence tiers must stay explicit: every observation in every scenario
// carries tier F (scripted fixture), never R.
func TestAllScriptedObservationsAreTierF(t *testing.T) {
	h := openHarness(t)
	for _, sc := range replay.StandardScenarios {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			_, ev, _ := runScenario(t, h, sc)
			if len(ev.Provider) == 0 {
				t.Fatal("scenario recorded no provider observations")
			}
			for _, obs := range ev.Provider {
				if obs.Tier != oracle.TierInjected {
					t.Fatalf("observation seq %d carries tier %q, want F (scripted fixture)", obs.Seq, obs.Tier)
				}
				if !strings.Contains(obs.Source, "scripted") {
					t.Fatalf("observation seq %d source = %q, want scripted fixture", obs.Seq, obs.Source)
				}
			}
		})
	}
}

// Journal provenance (M2 review correction): provider-derived journal events
// created while the scripted provider was wired must be tagged F at their
// origin — the durable journal may not claim tier R for them.
func TestScriptedJournalProvenanceIsTierF(t *testing.T) {
	h := openHarness(t)
	for _, sc := range replay.StandardScenarios {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			_, ev, _ := runScenario(t, h, sc)
			sawProviderEvent := false
			for _, e := range ev.Events {
				if e.Source != "PAYPAL" {
					continue
				}
				sawProviderEvent = true
				if e.Tier != oracle.TierInjected {
					t.Fatalf("provider-derived journal event %q carries tier %q in a scripted run; provider-derived events must be F", e.EventType, e.Tier)
				}
			}
			if !sawProviderEvent {
				t.Fatal("scenario recorded no provider-derived journal events (test would be vacuous)")
			}
		})
	}
}
