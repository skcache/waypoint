//go:build m4_repair

package merchant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// TestLegacyAmbiguityTreatsUnreadable2xxAsUnknown is the focused F2 gate: a
// mutating capture whose response cannot be read may have applied, so the
// service must move to CAPTURE_OUTCOME_UNKNOWN and keep the operation.
func TestLegacyAmbiguityTreatsUnreadable2xxAsUnknown(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			return nil, &paypal.ResponseError{}
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	svc.UseLegacyAmbiguity()
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)

	_, err := svc.Capture(context.Background(), order.ID)
	var unknown *merchant.UnknownCaptureError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want UnknownCaptureError for an unreadable 2xx response", err)
	}
	if got := detailFor(t, store, order.ID).Order.Status; got != merchant.StatusCaptureOutcomeUnknown {
		t.Fatalf("status = %s, want CAPTURE_OUTCOME_UNKNOWN", got)
	}
}
