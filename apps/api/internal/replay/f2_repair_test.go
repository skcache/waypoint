//go:build m4_repair

package replay_test

import (
	"context"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/replay"
)

// TestF2LostResponseKeepsOneCaptureAndDurableIdentity is the F2 correctness
// gate: the provider applies the capture and loses the response, and the
// repair must still end with exactly one capture effect, one durable CAPTURE
// operation, and a verified claim bound to the provider-issued capture id.
//
// It runs behind the m4_repair tag because the fixture intentionally wires the
// defective legacy ambiguity policy on the base revision. The verifier adds
// this tag for F2 candidates; the scripted provider's dropped response stays
// active, so a patch that merely disables the fault injection fails here.
func TestF2LostResponseKeepsOneCaptureAndDurableIdentity(t *testing.T) {
	h := fixtureHarness(t)
	ctx := context.Background()
	if err := h.ResetFixtureState(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	run, err := replay.RunFixtureFault(ctx, h, merchant.FaultF2)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	dropped := 0
	applied := 0
	for _, o := range run.Observations {
		if o.Method != "capture" {
			continue
		}
		if o.ResponseDropped {
			dropped++
		}
		if o.Applied && !o.Replayed {
			applied++
		}
	}
	if dropped == 0 {
		t.Fatal("the dropped response never happened; the fault injector was disabled, which is not a repair")
	}
	if applied != 1 {
		t.Fatalf("provider executed %d capture effects, want exactly 1", applied)
	}

	v, err := replay.JudgeFixtureRun(ctx, h, run)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	if !v.Accepted {
		t.Fatalf("F2 repair not accepted: %s (status %s, verdict %s)", v.Reason, v.OrderStatus, v.Oracle.Verdict)
	}
	if v.ClaimedCaptureID == "" || v.ClaimedCaptureID != v.ProviderCaptureID {
		t.Fatalf("capture identity not bound: claimed=%q provider=%q", v.ClaimedCaptureID, v.ProviderCaptureID)
	}

	detail, err := h.Store.OrderDetail(ctx, run.OrderID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	captures := 0
	var succeeded *merchant.Operation
	for i := range detail.Operations {
		op := detail.Operations[i]
		if op.Type != "CAPTURE" {
			continue
		}
		captures++
		if op.Status == merchant.OpStatusSucceeded {
			succeeded = &detail.Operations[i]
		}
	}
	if captures != 1 || succeeded == nil {
		t.Fatalf("durable capture operations = %d (succeeded=%v), want exactly one succeeded operation: %+v", captures, succeeded != nil, detail.Operations)
	}
	if succeeded.ProviderCaptureID != v.ProviderCaptureID {
		t.Fatalf("operation capture id %q != provider capture id %q", succeeded.ProviderCaptureID, v.ProviderCaptureID)
	}
	for _, o := range run.Observations {
		if o.Method == "capture" && o.Applied && !o.Replayed && o.RequestID != succeeded.ProviderRequestID {
			t.Fatalf("provider applied capture under request %q, but the durable operation holds %q", o.RequestID, succeeded.ProviderRequestID)
		}
	}
}
