package replay

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// RunCaseJourney executes the M5 benchmark journey for one held-out case. The
// case ID selects the adversity; the application defect is a mutation applied
// to the candidate workspace by the benchmark harness, never a fault switch in
// application code.
func RunCaseJourney(ctx context.Context, h *Harness, caseID string) (FixtureRun, error) {
	switch caseID {
	case "H1-identity-rewrite":
		return runH1(ctx, h)
	case "H7-webhook-duplicate-exposed":
		run, err := runF3(ctx, h)
		run.Fault = caseID
		return run, err
	case "H2-webhook-conflict":
		return runH2(ctx, h)
	case "H3-currency-mismatch":
		return runCaptureJourney(ctx, h, caseID, captureSetup{currency: "CAD"}, true)
	case "H4-pending-capture":
		return runCaptureJourney(ctx, h, caseID, captureSetup{status: "PENDING"}, true)
	case "H5-early-fulfillment":
		return runH5(ctx, h)
	case "H6-ambiguous-reconcile":
		return runCaptureJourney(ctx, h, caseID, captureSetup{ambiguousCapture: true}, true)
	}
	return FixtureRun{}, fmt.Errorf("replay: unknown benchmark case %q", caseID)
}

// runH1 is the identity-rewrite journey: a capture whose response is lost,
// then a restart and a same-identity retry. The canonical ambiguity policy is
// in place; the benchmark mutation is the rewritten retry request id.
func runH1(ctx context.Context, h *Harness) (FixtureRun, error) {
	provider := NewScriptedProvider(h.Pool, FaultSchedule{Capture: FaultDropResponseAfterMutation})
	provider.HideCaptureUntilResolved = true
	run := FixtureRun{Fault: "H1-identity-rewrite", Facts: map[string]string{"provider": "scripted-fixture"}}
	svc := merchant.NewService(h.Store, provider, merchant.FaultNone, "")
	svc.SetProviderEvidenceTier(merchant.TierFault)

	traceID, err := paypal.NewRequestID()
	if err != nil {
		return run, err
	}
	order, err := svc.Checkout(ctx, traceID, "maple-mug", 1)
	if err != nil {
		return run, err
	}
	run.OrderID, run.ProviderOrderID = order.ID, order.ProviderOrderID
	run.TotalMinor, run.Currency = order.TotalMinor, order.Currency
	provider.ApproveOrder(order.ProviderOrderID)
	if _, err := svc.Observe(ctx, order.ID); err != nil {
		return run, err
	}
	if _, err := svc.Capture(ctx, order.ID); err != nil {
		run.CaptureError = err.Error()
		run.Facts["capture_error"] = err.Error()
	}
	restarted := merchant.NewService(h.Store, provider, merchant.FaultNone, "")
	restarted.SetProviderEvidenceTier(merchant.TierFault)
	if _, err := restarted.Capture(ctx, order.ID); err != nil {
		run.Facts["retry_error"] = err.Error()
	}
	detail, err := h.Store.OrderDetail(ctx, order.ID)
	if err != nil {
		return run, err
	}
	run.OrderStatus = detail.Order.Status
	run.Facts["final_status"] = detail.Order.Status
	for _, op := range detail.Operations {
		if op.Type == merchant.OpCapture && op.Status == merchant.OpStatusSucceeded {
			run.ClaimedCapture = op.ProviderCaptureID
		}
	}
	run.Observations = provider.Observations()
	return run, nil
}

type captureSetup struct {
	currency         string
	status           string
	skipApproval     bool
	ambiguousCapture bool
}

// runCaptureJourney is the shared checkout → approval → capture journey for the
// single-order benchmark cases. It records the same facts a repair run needs
// and leaves the provider/DB in the state the judge reads.
func runCaptureJourney(ctx context.Context, h *Harness, caseID string, setup captureSetup, reconcile bool) (FixtureRun, error) {
	schedule := FaultSchedule{}
	if setup.ambiguousCapture {
		schedule.Capture = FaultAmbiguousNoEffect
	}
	provider := NewScriptedProvider(h.Pool, schedule)
	provider.CaptureCurrencyOverride = setup.currency
	provider.CaptureStatusOverride = setup.status

	svc := merchant.NewService(h.Store, provider, merchant.FaultNone, "")
	svc.SetProviderEvidenceTier(merchant.TierFault)

	run := FixtureRun{Fault: caseID, Facts: map[string]string{
		"fault": caseID, "provider": "scripted-fixture", "tier": merchant.TierFault,
	}}
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

	if !setup.skipApproval {
		provider.ApproveOrder(order.ProviderOrderID)
		if _, err := svc.Observe(ctx, order.ID); err != nil {
			run.Facts["observe_error"] = err.Error()
		}
	}

	if _, err := svc.Capture(ctx, order.ID); err != nil {
		run.CaptureError = err.Error()
		run.Facts["capture_error"] = err.Error()
	}
	if setup.ambiguousCapture && reconcile {
		if _, err := svc.Reconcile(ctx, order.ID); err != nil {
			run.Facts["reconcile_error"] = err.Error()
		}
	}

	detail, err := h.Store.OrderDetail(ctx, order.ID)
	if err != nil {
		return run, err
	}
	run.OrderStatus = detail.Order.Status
	run.Facts["final_status"] = detail.Order.Status
	for _, op := range detail.Operations {
		if op.Type == merchant.OpCapture {
			run.Facts["capture_status"] = op.Status
			if op.Status == merchant.OpStatusSucceeded {
				run.ClaimedCapture = op.ProviderCaptureID
			}
		}
	}
	run.Observations = provider.Observations()
	return run, nil
}

// runH2 is the conflicting-webhook journey: an authenticated capture event,
// then a second delivery with the same event id, a different payload and a
// different capture id. The handler must reject the conflict, not redefine the
// original receipt or create a second fulfillment.
func runH2(ctx context.Context, h *Harness) (FixtureRun, error) {
	provider := NewScriptedProvider(h.Pool, FaultSchedule{})
	svc := merchant.NewService(h.Store, provider, merchant.FaultNone, "")
	svc.SetProviderEvidenceTier(merchant.TierFault)
	svc.SetWebhookVerifier(merchant.NewFixtureWebhookVerifier(fixtureWebhookSecret))

	run := FixtureRun{Fault: "H2-webhook-conflict", Facts: map[string]string{
		"fault": "H2-webhook-conflict", "provider": "scripted-fixture", "tier": merchant.TierFault,
	}}
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

	provider.ApproveOrder(order.ProviderOrderID)
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

	deliverID := func(id, capID string) error {
		payload, err := json.Marshal(map[string]string{
			"id": id, "type": "PAYMENT.CAPTURE.COMPLETED",
			"provider_order_id": order.ProviderOrderID, "capture_id": capID,
		})
		if err != nil {
			return err
		}
		return svc.HandleWebhook(ctx, payload, map[string]string{
			merchant.FixtureSignatureHeader(): merchant.SignFixtureWebhook(fixtureWebhookSecret, payload),
		})
	}
	if err := deliverID("evt-cap-1", captureID); err != nil {
		run.Facts["first_event_error"] = err.Error()
	}
	if err := deliverID("evt-cap-2", "CAP-CONFLICT-0001"); err != nil {
		run.Facts["conflict_rejected"] = "true"
		run.Facts["conflict_error"] = err.Error()
	} else {
		run.Facts["conflict_rejected"] = "false"
	}
	run.Facts["fulfillment_effects"] = fmt.Sprint(h.countFulfillments(ctx, order.ID))
	run.Facts["evt_cap_1_capture"] = h.webhookCaptureID(ctx, "evt-cap-1")
	run.Observations = provider.Observations()
	return run, nil
}

// runH5 delivers a capture event before the merchant has verified the capture,
// then captures and redelivers. The effect must exist only after verification.
func runH5(ctx context.Context, h *Harness) (FixtureRun, error) {
	provider := NewScriptedProvider(h.Pool, FaultSchedule{})
	svc := merchant.NewService(h.Store, provider, merchant.FaultNone, "")
	svc.SetProviderEvidenceTier(merchant.TierFault)
	svc.SetWebhookVerifier(merchant.NewFixtureWebhookVerifier(fixtureWebhookSecret))
	run := FixtureRun{Fault: "H5-early-fulfillment", Facts: map[string]string{"provider": "scripted-fixture"}}

	traceID, err := paypal.NewRequestID()
	if err != nil {
		return run, err
	}
	order, err := svc.Checkout(ctx, traceID, "maple-mug", 1)
	if err != nil {
		return run, err
	}
	run.OrderID, run.ProviderOrderID = order.ID, order.ProviderOrderID
	run.TotalMinor, run.Currency = order.TotalMinor, order.Currency
	provider.ApproveOrder(order.ProviderOrderID)
	if _, err := svc.Observe(ctx, order.ID); err != nil {
		return run, err
	}
	deliver := func() error {
		payload, err := json.Marshal(map[string]string{
			"id": "evt-early", "type": "PAYMENT.CAPTURE.COMPLETED",
			"provider_order_id": order.ProviderOrderID, "capture_id": "CAP-SCRIPT-0001",
		})
		if err != nil {
			return err
		}
		return svc.HandleWebhook(ctx, payload, map[string]string{
			merchant.FixtureSignatureHeader(): merchant.SignFixtureWebhook(fixtureWebhookSecret, payload),
		})
	}
	if err := deliver(); err != nil {
		run.Facts["early_event_error"] = err.Error()
	}
	run.Facts["fulfillment_effects_early"] = fmt.Sprint(h.countFulfillments(ctx, order.ID))
	if _, err := svc.Capture(ctx, order.ID); err != nil {
		run.Facts["capture_error"] = err.Error()
	}
	if err := deliver(); err != nil {
		run.Facts["redelivery_error"] = err.Error()
	}
	detail, err := h.Store.OrderDetail(ctx, order.ID)
	if err != nil {
		return run, err
	}
	run.OrderStatus = detail.Order.Status
	run.ClaimedCapture = "CAP-SCRIPT-0001"
	run.Facts["final_status"] = detail.Order.Status
	run.Facts["fulfillment_effects"] = fmt.Sprint(h.countFulfillments(ctx, order.ID))
	run.Observations = provider.Observations()
	return run, nil
}

// webhookCaptureID reads the capture identity stored on a durable receipt.
func (h *Harness) webhookCaptureID(ctx context.Context, eventID string) string {
	var captureID string
	_ = h.Pool.QueryRow(ctx, `SELECT coalesce(provider_capture_id,'') FROM merchant_webhook_events WHERE event_id=$1`, eventID).Scan(&captureID)
	return captureID
}
