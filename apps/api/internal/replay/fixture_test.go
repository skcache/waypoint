package replay_test

import (
	"context"
	"os"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/oracle"
	"github.com/skcache/waypoint/apps/api/internal/replay"
)

// fixtureHarness opens the deterministic fixture harness against the derived
// _test database, with the same fail-closed policy as the oracle suite.
func fixtureHarness(t *testing.T) *replay.Harness {
	t.Helper()
	h, err := replay.OpenHarnessForTests(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		if os.Getenv("REQUIRE_DATABASE_TESTS") != "" {
			t.Fatalf("database tests are REQUIRED but the fixture harness failed: %v", err)
		}
		t.Skipf("PostgreSQL test database not reachable (%v); start it with `docker compose up -d db`", err)
	}
	t.Cleanup(h.Close)
	return h
}

// TestFixtureFaultF4IsRejectedByTheOracle is the M3 held-out defect's RED: the
// fixture certifies a capture the provider never issued. The build is clean and
// the ordinary merchant tests pass; only the independent oracle rejects it.
func TestFixtureFaultF4IsRejectedByTheOracle(t *testing.T) {
	h := fixtureHarness(t)
	defer h.Close()
	if err := h.ResetFixtureState(context.Background()); err != nil {
		t.Fatalf("reset: %v", err)
	}
	v, err := replay.EvaluateFixture(context.Background(), h, merchant.FaultF4)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if v.Accepted {
		t.Fatalf("F4 was accepted (%s); the oracle must reject a wrong capture identity", v.Reason)
	}
	if v.Oracle.Verdict != oracle.VerdictUnsafe {
		t.Fatalf("oracle verdict = %s, want UNSAFE", v.Oracle.Verdict)
	}
	if v.ClaimedCaptureID == v.ProviderCaptureID {
		t.Fatalf("seed did not diverge: claimed=%q provider=%q", v.ClaimedCaptureID, v.ProviderCaptureID)
	}
}

// TestFixtureFaultF1IsRejectedBeforeRepair: the malformed amount never reaches
// acceptance — the journey dies at create with a definite provider refusal.
func TestFixtureFaultF1IsRejectedBeforeRepair(t *testing.T) {
	h := fixtureHarness(t)
	defer h.Close()
	if err := h.ResetFixtureState(context.Background()); err != nil {
		t.Fatalf("reset: %v", err)
	}
	v, err := replay.EvaluateFixture(context.Background(), h, merchant.FaultF1)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if v.Accepted {
		t.Fatalf("F1 was accepted before repair (%s)", v.Reason)
	}
	if v.OrderStatus != merchant.StatusFailedDefinite {
		t.Fatalf("order status = %s, want FAILED_DEFINITE", v.OrderStatus)
	}
}

// TestFixtureNoFaultIsAccepted pins the acceptance path itself: a correct
// implementation must be accepted, so the pipeline is not simply rejecting
// everything.
func TestFixtureNoFaultIsAccepted(t *testing.T) {
	h := fixtureHarness(t)
	defer h.Close()
	if err := h.ResetFixtureState(context.Background()); err != nil {
		t.Fatalf("reset: %v", err)
	}
	v, err := replay.EvaluateFixture(context.Background(), h, merchant.FaultNone)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !v.Accepted {
		t.Fatalf("a correct fixture was not accepted: %s", v.Reason)
	}
	if v.Oracle.Verdict != oracle.VerdictVerified {
		t.Fatalf("oracle verdict = %s, want VERIFIED", v.Oracle.Verdict)
	}
	if v.ClaimedCaptureID == "" || v.ClaimedCaptureID != v.ProviderCaptureID {
		t.Fatalf("capture identity mismatch: claimed=%q provider=%q", v.ClaimedCaptureID, v.ProviderCaptureID)
	}
}
