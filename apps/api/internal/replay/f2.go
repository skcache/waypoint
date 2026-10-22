package replay

import (
	"context"
	"errors"
	"fmt"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// runF2 exercises the "capture applied, response lost" family. The provider
// applies the capture and then drops the response; the merchant must classify
// that as UNKNOWN, keep the durable request identity, and resolve it later
// without a second logical capture.
//
// The fault injector is the scripted provider's dropped response. It stays
// active for the whole run, including after the application-level repair. The
// seeded application defect is the fixture's legacy ambiguity policy, wired
// here on both service instances (the first attempt and the post-restart
// recovery) so a repair of that policy is what changes the outcome.
func runF2(ctx context.Context, h *Harness) (FixtureRun, error) {
	provider := NewScriptedProvider(h.Pool, FaultSchedule{Capture: FaultDropResponseAfterMutation})
	run := FixtureRun{
		Fault: merchant.FaultF2,
		Facts: map[string]string{
			"fault":            merchant.FaultF2,
			"provider":         "scripted-fixture",
			"fault_injection":  "capture response dropped after the mutation applied",
			"recovery":         "process restart, then a second Capture call",
			"ambiguity_policy": "legacy (defective)",
		},
	}

	svc := merchant.NewLegacyAmbiguityService(h.Store, provider)
	svc.SetProviderEvidenceTier(merchant.TierFault)

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
		run.Facts["observe_error"] = err.Error()
	}

	// First capture: the provider applies it, then the response is lost.
	if _, err := svc.Capture(ctx, order.ID); err != nil {
		run.CaptureError = "first capture: " + err.Error()
		run.Facts["first_capture_error"] = err.Error()
		var unknown *merchant.UnknownCaptureError
		if errors.As(err, &unknown) {
			run.Facts["first_classification"] = "UNKNOWN"
		} else {
			run.Facts["first_classification"] = "NOT_UNKNOWN"
		}
	}

	// The worker dies and a fresh process (new instance id) takes over. It
	// repeats the legacy wiring because the deployment is unchanged.
	restarted := merchant.NewLegacyAmbiguityService(h.Store, provider)
	restarted.SetProviderEvidenceTier(merchant.TierFault)

	// Recovery attempt. A correct implementation reuses the durable request id
	// and learns the capture applied; the defective policy closes the first
	// operation as REJECTED, so this mints a NEW identity and asks the provider
	// to capture again.
	if _, err := restarted.Capture(ctx, order.ID); err != nil {
		run.Facts["second_capture_error"] = err.Error()
	}

	detail, err := h.Store.OrderDetail(ctx, order.ID)
	if err != nil {
		return run, err
	}
	run.OrderStatus = detail.Order.Status
	run.Observations = provider.Observations()

	captureOps := 0
	requestIDs := []string{}
	for _, op := range detail.Operations {
		if op.Type != "CAPTURE" {
			continue
		}
		captureOps++
		requestIDs = append(requestIDs, op.ProviderRequestID)
		if op.Status == merchant.OpStatusSucceeded {
			run.ClaimedCapture = op.ProviderCaptureID
		}
	}
	run.Facts["capture_operations"] = fmt.Sprint(captureOps)
	run.Facts["request_ids"] = fmt.Sprint(requestIDs)
	if captureOps > 1 && requestIDs[0] != requestIDs[1] {
		run.Facts["minted_new_identity"] = "true"
	}
	applied := 0
	for _, o := range run.Observations {
		if o.Method == "capture" && o.Applied && !o.Replayed {
			applied++
		}
	}
	run.Facts["provider_capture_effects"] = fmt.Sprint(applied)
	return run, nil
}
