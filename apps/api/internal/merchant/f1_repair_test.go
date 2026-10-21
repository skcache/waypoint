//go:build f1_repair

// POST-REPAIR CORRECTNESS TESTS (candidate gate).
//
// The reference fixture deliberately contains the F1 presentation-formatting
// defect, activated by MERCHANT_FAULT=F1, so the incident is reproducible.
// These tests assert the behavior a repaired candidate must exhibit, and are
// run only for F1 repair candidates (the verifier passes -tags f1_repair).
// They must stay out of the fixture's default suite, whose subject still has
// the defect.
//
// Pre-repair reproduction lives in f1_reproduction_test.go (tag
// "fault_reproduction") and is run against the pinned base revision.
package merchant_test

import (
	"context"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// TestCheckoutF1FaultMustNotLeakPresentationFormatting is the POST-REPAIR
// correctness test for the F1 family. The injected fault models a presentation
// formatter leaking into the provider payload; a correct implementation must
// still send an exact two-decimal amount and complete the checkout. On the
// pre-repair revision this test FAILS (the provider rejects the malformed
// amount); after a genuine repair it passes.
func TestCheckoutF1FaultMustNotLeakPresentationFormatting(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		createFn: func(in paypal.CreateOrderInput, _ string) (*paypal.Order, error) {
			// The provider contract: a non-negative number with at most two
			// decimal places. Anything else is rejected exactly as PayPal does.
			parts := strings.SplitN(in.AmountValue, ".", 2)
			if len(parts) != 2 || len(parts[1]) != 2 || parts[0] == "" {
				return nil, &paypal.APIError{
					StatusCode: 400,
					Name:       "INVALID_REQUEST",
					Message:    "Currency amount must be a non-negative number with at most 2 decimal places",
					DebugID:    "debug-test-f1",
				}
			}
			return &paypal.Order{
				ID:     "ORD-TEST-1",
				Status: paypal.OrderStatusCreated,
				PurchaseUnits: []paypal.PurchaseUnit{{
					Amount: paypal.Money{CurrencyCode: in.CurrencyCode, Value: in.AmountValue},
				}},
				Links: []paypal.Link{{Rel: "payer-action", Href: "https://sandbox.paypal.example/checkoutnow?token=ORD-TEST-1"}},
			}, nil
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultF1, "")

	order, err := svc.Checkout(context.Background(), testTrace, "maple-mug", 1)
	if err != nil {
		t.Fatalf("a correct implementation must send a well-formed amount even with the presentation fault injected: %v", err)
	}
	if got := fake.createCalls[0].AmountValue; got != "12.34" {
		t.Fatalf("provider payload amount = %q, want the canonical two-decimal value", got)
	}
	if order.Status != merchant.StatusBuyerApprovalPending {
		t.Fatalf("order status = %s, want BUYER_APPROVAL_PENDING", order.Status)
	}
	d := detailFor(t, store, order.ID)
	var foundFault bool
	for _, e := range d.Events {
		if e.Source == "FAULT" && e.EventType == "FAULT_INJECTED" && e.Tier == merchant.TierFault {
			foundFault = true
		}
	}
	if !foundFault {
		t.Fatal("journal is missing the FAULT_INJECTED (tier F) marker")
	}
}
