//go:build m4_repair

package merchant_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// TestWebhookDuplicatesAndReorderingYieldOneFulfillment is the F3 correctness
// gate. The same delivery is repeated, a second event id describes the same
// capture, an older approval arrives after the capture, and an unauthenticated
// delivery is attempted. The only acceptable end state is one fulfillment
// effect on a still-verified order and no receipt for the rejected delivery.
func TestWebhookDuplicatesAndReorderingYieldOneFulfillment(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			return completedOrder(orderID, "CAP-WH-1", "USD", "12.34"), nil
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	svc.SetProviderEvidenceTier(merchant.TierFault)
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)
	if _, err := svc.Capture(context.Background(), order.ID); err != nil {
		t.Fatalf("capture: %v", err)
	}

	const secret = "wh-test-secret"
	svc.SetWebhookVerifier(merchant.NewFixtureWebhookVerifier(secret))
	svc.UseLegacyWebhook()

	deliver := func(id, eventType, captureID, signWith string) error {
		payload, err := json.Marshal(map[string]string{
			"id": id, "type": eventType,
			"provider_order_id": order.ProviderOrderID, "capture_id": captureID,
		})
		if err != nil {
			t.Fatalf("payload: %v", err)
		}
		return svc.HandleWebhook(context.Background(), payload, map[string]string{
			merchant.FixtureSignatureHeader(): merchant.SignFixtureWebhook(signWith, payload),
		})
	}

	if err := deliver("evt-1", "PAYMENT.CAPTURE.COMPLETED", "CAP-WH-1", secret); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := deliver("evt-1", "PAYMENT.CAPTURE.COMPLETED", "CAP-WH-1", secret); err != nil {
		t.Fatalf("duplicate delivery: %v", err)
	}
	if err := deliver("evt-2", "PAYMENT.CAPTURE.COMPLETED", "CAP-WH-1", secret); err != nil {
		t.Fatalf("second event id: %v", err)
	}
	if err := deliver("evt-appr-late", "ORDER.APPROVED", "", secret); err != nil {
		t.Fatalf("late approval: %v", err)
	}
	if err := deliver("evt-unauth", "PAYMENT.CAPTURE.COMPLETED", "CAP-WH-1", "wrong"); !errors.Is(err, merchant.ErrUnauthenticatedWebhook) {
		t.Fatalf("unauthenticated delivery err = %v, want ErrUnauthenticatedWebhook", err)
	}

	d := detailFor(t, store, order.ID)
	if d.Order.Status != merchant.StatusCapturedVerified {
		t.Fatalf("order status = %s, want CAPTURED_VERIFIED after a late approval event", d.Order.Status)
	}
	var effects int
	if err := store.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM merchant_fulfillment_effects WHERE order_id = $1::uuid`, order.ID).Scan(&effects); err != nil {
		t.Fatalf("count effects: %v", err)
	}
	if effects != 1 {
		t.Fatalf("fulfillment effects = %d, want exactly 1", effects)
	}
	var deliveries int
	if err := store.Pool().QueryRow(context.Background(),
		`SELECT deliveries FROM merchant_webhook_events WHERE event_id = 'evt-1'`).Scan(&deliveries); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if deliveries != 2 {
		t.Fatalf("evt-1 deliveries = %d, want 2 durable deliveries", deliveries)
	}
	var receipts int
	if err := store.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM merchant_webhook_events WHERE event_id = 'evt-unauth'`).Scan(&receipts); err != nil {
		t.Fatalf("unauth receipt: %v", err)
	}
	if receipts != 0 {
		t.Fatalf("unauthenticated delivery created %d durable receipts, want 0", receipts)
	}
}
