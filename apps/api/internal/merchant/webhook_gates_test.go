package merchant_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// deliverEvent signs and delivers one fixture webhook.
func deliverEvent(t *testing.T, svc *merchant.Service, secret, id, eventType, providerOrderID, captureID string) error {
	t.Helper()
	payload, err := json.Marshal(map[string]string{
		"id": id, "type": eventType,
		"provider_order_id": providerOrderID, "capture_id": captureID,
	})
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	return svc.HandleWebhook(context.Background(), payload, map[string]string{
		merchant.FixtureSignatureHeader(): merchant.SignFixtureWebhook(secret, payload),
	})
}

// TestEarlyCaptureEventWaitsForLocalVerification is the M4-review P2 gate: a
// capture webhook that arrives before the merchant has verified the capture is
// recorded, not fulfilled. A later redelivery, after verification, fulfills
// exactly once.
func TestEarlyCaptureEventWaitsForLocalVerification(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			return completedOrder(orderID, "CAP-EARLY", "USD", "12.34"), nil
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	svc.SetProviderEvidenceTier(merchant.TierFault)
	const secret = "early-secret"
	svc.SetWebhookVerifier(merchant.NewFixtureWebhookVerifier(secret))

	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)

	ctx := context.Background()
	count := func() int {
		var n int
		if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM merchant_fulfillment_effects WHERE order_id=$1::uuid`, order.ID).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	// The provider has not captured yet; the event is early.
	if err := deliverEvent(t, svc, secret, "evt-early", "PAYMENT.CAPTURE.COMPLETED", order.ProviderOrderID, "CAP-EARLY"); err != nil {
		t.Fatalf("early delivery: %v", err)
	}
	if got := count(); got != 0 {
		t.Fatalf("early event created %d fulfillment effects, want 0", got)
	}
	var processed bool
	if err := store.Pool().QueryRow(ctx, `SELECT processed FROM merchant_webhook_events WHERE event_id='evt-early'`).Scan(&processed); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if processed {
		t.Fatal("early event was marked processed without a verified capture")
	}

	if _, err := svc.Capture(ctx, order.ID); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := deliverEvent(t, svc, secret, "evt-early", "PAYMENT.CAPTURE.COMPLETED", order.ProviderOrderID, "CAP-EARLY"); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if got := count(); got != 1 {
		t.Fatalf("fulfillment effects = %d, want exactly 1 after redelivery", got)
	}
}

// TestConflictingEventIDCannotRedefineReceipt is the second M4-review P2 gate:
// the same authenticated event id arriving with a different capture identity is
// rejected, the original receipt is unchanged, and no second effect appears.
func TestConflictingEventIDCannotRedefineReceipt(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			return completedOrder(orderID, "CAP-ORIGINAL", "USD", "12.34"), nil
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	svc.SetProviderEvidenceTier(merchant.TierFault)
	const secret = "conflict-secret"
	svc.SetWebhookVerifier(merchant.NewFixtureWebhookVerifier(secret))

	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)
	if _, err := svc.Capture(context.Background(), order.ID); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := deliverEvent(t, svc, secret, "evt-1", "PAYMENT.CAPTURE.COMPLETED", order.ProviderOrderID, "CAP-ORIGINAL"); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	err := deliverEvent(t, svc, secret, "evt-1", "PAYMENT.CAPTURE.COMPLETED", order.ProviderOrderID, "CAP-CONFLICT")
	if !errors.Is(err, merchant.ErrConflictingWebhook) {
		t.Fatalf("conflicting delivery err = %v, want ErrConflictingWebhook", err)
	}

	ctx := context.Background()
	var captureID string
	var deliveries int
	if err := store.Pool().QueryRow(ctx, `SELECT coalesce(provider_capture_id,''), deliveries FROM merchant_webhook_events WHERE event_id='evt-1'`).Scan(&captureID, &deliveries); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if captureID != "CAP-ORIGINAL" || deliveries != 1 {
		t.Fatalf("receipt was redefined: capture=%q deliveries=%d, want CAP-ORIGINAL/1", captureID, deliveries)
	}
	var effects int
	if err := store.Pool().QueryRow(ctx, `SELECT count(*) FROM merchant_fulfillment_effects WHERE order_id=$1::uuid`, order.ID).Scan(&effects); err != nil {
		t.Fatalf("effects: %v", err)
	}
	if effects != 1 {
		t.Fatalf("fulfillment effects = %d, want exactly 1", effects)
	}
}
