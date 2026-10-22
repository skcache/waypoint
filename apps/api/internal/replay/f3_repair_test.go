//go:build m4_f3

package replay_test

import (
	"context"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/replay"
)

// TestF3WebhookAdversityIsExactlyOnce is the F3 correctness gate. The same
// fixture adversity (duplicate delivery, a second event id, a late approval,
// concurrent handlers, restart replay, an unauthenticated delivery) must end
// with one fulfillment effect on a verified order.
func TestF3WebhookAdversityIsExactlyOnce(t *testing.T) {
	h := fixtureHarness(t)
	ctx := context.Background()
	if err := h.ResetFixtureState(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	run, err := replay.RunFixtureFault(ctx, h, merchant.FaultF3)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Facts["unauthorized_rejected"] != "true" {
		t.Fatalf("unauthenticated delivery was not rejected: %q", run.Facts["unauthorized_error"])
	}
	if got := run.Facts["unauthorized_receipts"]; got != "0" {
		t.Fatalf("unauthenticated delivery produced %s durable receipts, want 0", got)
	}
	v, err := replay.JudgeFixtureRun(ctx, h, run)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if !v.Accepted {
		t.Fatalf("F3 repair not accepted: %s (status %s, verdict %s)", v.Reason, v.OrderStatus, v.Oracle.Verdict)
	}
	if v.OrderStatus != merchant.StatusCapturedVerified {
		t.Fatalf("order status = %s, want CAPTURED_VERIFIED", v.OrderStatus)
	}
	if got := run.Facts["fulfillment_effects"]; got != "1" {
		t.Fatalf("fulfillment effects = %s, want 1", got)
	}
	if got := run.Facts["evt_cap_1_deliveries"]; got != "11" {
		t.Fatalf("evt-cap-1 durable deliveries = %s, want 11 (1 + 1 + 8 concurrent + 1 restart)", got)
	}
	// The adversity really ran: the receipt identity survived every delivery
	// and the second event id did not add an effect.
	if got := run.Facts["distinct_receipts"]; got != "4" {
		t.Fatalf("distinct receipts = %s, want 4 (early, cap-1, cap-2, late)", got)
	}
}
