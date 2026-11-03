package oracle_test

import (
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/oracle"
)

func TestReadRecoveryRequiresExplicitCompletedCapture(t *testing.T) {
	order, op := claimedOrder("CAP-1")
	for _, status := range []string{"", "PENDING", "DENIED"} {
		t.Run("capture_status="+status, func(t *testing.T) {
			ev := &oracle.Evidence{
				Scenario: oracle.ScenarioMeta{ID: "missing-terminal-proof", EvidenceTier: "F"},
				Orders:   []oracle.LocalOrder{order}, Operations: []oracle.LocalOp{op},
				Events: []oracle.LocalEvent{{ID: 1, OrderID: order.ID, Source: "PAYPAL", EventType: "PROVIDER_CAPTURE_CONFIRMED", Tier: "F"}},
				Provider: []oracle.ProviderObs{{Seq: 1, Method: "read", OrderRef: "REF-1", OrderStatus: "APPROVED",
					CaptureID: "CAP-1", CaptureStatus: status, CaptureAmount: "12.34", CaptureCurrency: "USD", Source: "scripted-fixture", Tier: "F"}},
			}
			res := oracle.Evaluate(ev)
			if res.Verdict == oracle.VerdictVerified || statusOf(t, ev, "I6") == oracle.StatusPass {
				t.Fatalf("missing COMPLETED proof produced %s (I6=%s)", res.Verdict, statusOf(t, ev, "I6"))
			}
		})
	}
}

func TestReconfirmationCannotHideTerminalStateRollback(t *testing.T) {
	order, op := claimedOrder("CAP-1")
	for _, reversed := range []bool{false, true} {
		events := []oracle.LocalEvent{
			{ID: 10, OrderID: order.ID, Source: "PAYPAL", EventType: "PROVIDER_CAPTURE_CONFIRMED", Tier: "F"},
			{ID: 11, OrderID: order.ID, Source: "PAYPAL", EventType: "CAPTURE_OUTCOME_UNKNOWN", Tier: "F"},
			{ID: 12, OrderID: order.ID, Source: "PAYPAL", EventType: "PROVIDER_CAPTURE_RECOVERED", Tier: "F"},
		}
		if reversed {
			events[0], events[2] = events[2], events[0]
		}
		obs := goodCaptureObs()
		approval := oracle.ProviderObs{Seq: 1, Method: "read", OrderRef: "REF-1", OrderStatus: "APPROVED", Source: "scripted-fixture", Tier: "F"}
		ev := &oracle.Evidence{Scenario: oracle.ScenarioMeta{ID: "hidden-rollback", EvidenceTier: "F"},
			Orders: []oracle.LocalOrder{order}, Operations: []oracle.LocalOp{op}, Events: events, Provider: []oracle.ProviderObs{approval, obs}}
		if res := oracle.Evaluate(ev); res.Verdict != oracle.VerdictUnsafe || statusOf(t, ev, "I8") != oracle.StatusFail {
			t.Fatalf("reconfirmation hid a rollback: verdict=%s I8=%s reversed=%v", res.Verdict, statusOf(t, ev, "I8"), reversed)
		}
	}
}

// Go's integer multiplication wraps: 2^64 + 1234 must never be accepted as
// the authorized 1234 minor units, through either a capture call or a read.
func TestOverflowingProviderAmountCannotMatchAuthorizedTotal(t *testing.T) {
	order, op := claimedOrder("CAP-1")
	for _, method := range []string{"capture", "read"} {
		t.Run(method, func(t *testing.T) {
			obs := goodCaptureObs()
			obs.Method = method
			obs.CaptureAmount = "184467440737095528.50"
			ev := &oracle.Evidence{Orders: []oracle.LocalOrder{order}, Operations: []oracle.LocalOp{op}, Provider: []oracle.ProviderObs{obs}}
			if got := statusOf(t, ev, "I6"); got != oracle.StatusFail {
				t.Fatalf("overflowing amount produced I6=%s, want FAIL", got)
			}
		})
	}
}
