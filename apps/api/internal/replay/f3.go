package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

const fixtureWebhookSecret = "m4-fixture-webhook-secret"

// runF3 exercises the webhook/fulfillment family: duplicate delivery, a second
// event describing the same capture, an older event arriving after the
// capture, concurrent handlers, a restart replay, an early event that arrives
// before the merchant's local state, and an unauthenticated delivery.
//
// The seeded application defect is the fixture's legacy event handler. The
// adversity (duplicates, reordering, concurrency) is generated here and stays
// active after repair.
func runF3(ctx context.Context, h *Harness) (FixtureRun, error) {
	provider := NewScriptedProvider(h.Pool, FaultSchedule{})
	run := FixtureRun{
		Fault: merchant.FaultF3,
		Facts: map[string]string{
			"fault":            merchant.FaultF3,
			"provider":         "scripted-fixture",
			"webhook_verifier": "fixture HMAC (tier F; not a PayPal signature)",
			"handler":          "legacy (defective)",
		},
	}

	svc := merchant.NewService(h.Store, provider, merchant.FaultNone, "")
	svc.UseLegacyWebhook()
	svc.SetProviderEvidenceTier(merchant.TierFault)
	svc.SetWebhookVerifier(merchant.NewFixtureWebhookVerifier(fixtureWebhookSecret))

	traceID, err := paypal.NewRequestID()
	if err != nil {
		return run, err
	}
	order, err := svc.Checkout(ctx, traceID, "maple-mug", 1)
	if err != nil {
		return run, err
	}
	run.OrderID = order.ID
	run.ProviderOrderID = order.ProviderOrderID
	run.TotalMinor = order.TotalMinor
	run.Currency = order.Currency

	deliver := func(s *merchant.Service, id, eventType, captureID, secret string) error {
		payload, err := json.Marshal(map[string]string{
			"id": id, "type": eventType,
			"provider_order_id": order.ProviderOrderID, "capture_id": captureID,
		})
		if err != nil {
			return err
		}
		return s.HandleWebhook(ctx, payload, map[string]string{
			merchant.FixtureSignatureHeader(): merchant.SignFixtureWebhook(secret, payload),
		})
	}

	// The buyer approves at the provider before the merchant has observed it.
	// A real webhook can arrive in that window; it must be recorded, not used
	// to advance local payment state.
	provider.ApproveOrder(order.ProviderOrderID)
	if err := deliver(svc, "evt-appr-early", "ORDER.APPROVED", "", fixtureWebhookSecret); err != nil {
		run.Facts["early_event_error"] = err.Error()
	}
	run.Facts["status_after_early_event"] = h.orderStatus(ctx, order.ID)

	if _, err := svc.Observe(ctx, order.ID); err != nil {
		return run, err
	}
	if _, err := svc.Capture(ctx, order.ID); err != nil {
		return run, err
	}
	detail, err := h.Store.OrderDetail(ctx, order.ID)
	if err != nil {
		return run, err
	}
	run.OrderStatus = detail.Order.Status
	var captureID string
	for _, op := range detail.Operations {
		if op.Type == merchant.OpCapture && op.Status == merchant.OpStatusSucceeded {
			captureID = op.ProviderCaptureID
			run.ClaimedCapture = op.ProviderCaptureID
		}
	}
	run.Facts["capture_id"] = captureID

	// An unauthenticated delivery must be rejected before any durable write.
	unauthErr := deliver(svc, "evt-unauth-1", "PAYMENT.CAPTURE.COMPLETED", captureID, "not-the-secret")
	if unauthErr == nil {
		run.Facts["unauthorized_rejected"] = "false"
	} else {
		run.Facts["unauthorized_rejected"] = "true"
		run.Facts["unauthorized_error"] = unauthErr.Error()
	}

	// The legitimate capture event, then its redelivery.
	if err := deliver(svc, "evt-cap-1", "PAYMENT.CAPTURE.COMPLETED", captureID, fixtureWebhookSecret); err != nil {
		run.Facts["capture_event_error"] = err.Error()
	}
	if err := deliver(svc, "evt-cap-1", "PAYMENT.CAPTURE.COMPLETED", captureID, fixtureWebhookSecret); err != nil {
		run.Facts["duplicate_event_error"] = err.Error()
	}
	// A second, distinct event id describing the same capture. Exactly-once
	// business effects must come from a stable effect key, not event identity.
	if err := deliver(svc, "evt-cap-2", "PAYMENT.CAPTURE.COMPLETED", captureID, fixtureWebhookSecret); err != nil {
		run.Facts["second_event_error"] = err.Error()
	}
	// An older event arriving after the capture must not rewind local state.
	if err := deliver(svc, "evt-appr-late", "ORDER.APPROVED", "", fixtureWebhookSecret); err != nil {
		run.Facts["late_event_error"] = err.Error()
	}

	// Concurrent handlers for the same delivery.
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := deliver(svc, "evt-cap-1", "PAYMENT.CAPTURE.COMPLETED", captureID, fixtureWebhookSecret); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	concurrentErrs := 0
	for range errs {
		concurrentErrs++
	}
	run.Facts["concurrent_errors"] = fmt.Sprint(concurrentErrs)

	// Process restart: a fresh handler replays the original delivery.
	restarted := merchant.NewService(h.Store, provider, merchant.FaultNone, "")
	restarted.UseLegacyWebhook()
	restarted.SetProviderEvidenceTier(merchant.TierFault)
	restarted.SetWebhookVerifier(merchant.NewFixtureWebhookVerifier(fixtureWebhookSecret))
	if err := deliver(restarted, "evt-cap-1", "PAYMENT.CAPTURE.COMPLETED", captureID, fixtureWebhookSecret); err != nil {
		run.Facts["restart_replay_error"] = err.Error()
	}

	detail, err = h.Store.OrderDetail(ctx, order.ID)
	if err != nil {
		return run, err
	}
	run.OrderStatus = detail.Order.Status
	run.Facts["final_status"] = detail.Order.Status
	run.Facts["fulfillment_effects"] = fmt.Sprint(h.countFulfillments(ctx, order.ID))
	run.Facts["distinct_receipts"] = fmt.Sprint(h.countWebhookReceipts(ctx, order.ID))
	run.Facts["evt_cap_1_deliveries"] = fmt.Sprint(h.webhookDeliveries(ctx, "evt-cap-1"))
	run.Facts["unauthorized_receipts"] = fmt.Sprint(h.webhookDeliveries(ctx, "evt-unauth-1"))
	run.Observations = provider.Observations()
	return run, nil
}

func (h *Harness) orderStatus(ctx context.Context, orderID string) string {
	var status string
	_ = h.Pool.QueryRow(ctx, `SELECT status FROM merchant_orders WHERE id = $1::uuid`, orderID).Scan(&status)
	return status
}

func (h *Harness) countFulfillments(ctx context.Context, orderID string) int {
	var n int
	_ = h.Pool.QueryRow(ctx, `SELECT count(*) FROM merchant_fulfillment_effects WHERE order_id = $1::uuid`, orderID).Scan(&n)
	return n
}

func (h *Harness) countWebhookReceipts(ctx context.Context, orderID string) int {
	var n int
	_ = h.Pool.QueryRow(ctx, `SELECT count(*) FROM merchant_webhook_events WHERE order_id = $1::uuid`, orderID).Scan(&n)
	return n
}

func (h *Harness) webhookDeliveries(ctx context.Context, eventID string) int {
	var n int
	_ = h.Pool.QueryRow(ctx, `SELECT deliveries FROM merchant_webhook_events WHERE event_id = $1`, eventID).Scan(&n)
	return n
}
