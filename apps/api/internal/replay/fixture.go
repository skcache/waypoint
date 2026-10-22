// Fixture-fault evaluation: drive the merchant fixture through a REAL checkout
// journey (create → buyer approval → capture) against the deterministic
// scripted provider, then let the independent oracle judge the durable journal.
//
// This is the verification surface M3 exposes to a candidate patch: the
// scenario executes the compiled merchant package from the candidate
// workspace, while the oracle reads PostgreSQL with its own SQL and is built
// from the trusted revision. A candidate cannot make the judge agree with it.
package replay

import (
	"context"
	"fmt"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/oracle"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// FixtureRun is one execution of the fixture journey with a single injected
// fixture defect (F1, F4, or none).
type FixtureRun struct {
	Fault           string            `json:"fault"`
	OrderID         string            `json:"order_id"`
	ProviderOrderID string            `json:"provider_order_id,omitempty"`
	OrderStatus     string            `json:"order_status"`
	ClaimedCapture  string            `json:"claimed_capture_id,omitempty"`
	TotalMinor      int64             `json:"total_minor"`
	Currency        string            `json:"currency"`
	CheckoutError   string            `json:"checkout_error,omitempty"`
	CaptureError    string            `json:"capture_error,omitempty"`
	Observations    []Observation     `json:"observations"`
	Facts           map[string]string `json:"facts"`
}

// RunFixtureFault drives checkout → approval → capture for one fault. No
// provider response is dropped: the only defect under test is the fixture's
// own code path, which is exactly what a candidate patch changes.
func RunFixtureFault(ctx context.Context, h *Harness, fault string) (FixtureRun, error) {
	switch fault {
	case merchant.FaultF2:
		return runF2(ctx, h)
	case merchant.FaultF3:
		return runF3(ctx, h)
	}
	provider := NewScriptedProvider(h.Pool, FaultSchedule{})
	svc := merchant.NewService(h.Store, provider, fault, "")
	// Provenance at the origin: this service is wired to the scripted
	// provider, so every provider-derived journal event it writes is tier F.
	svc.SetProviderEvidenceTier(merchant.TierFault)

	run := FixtureRun{Fault: fault, Facts: map[string]string{"fault": fault, "provider": "scripted-fixture"}}

	traceID, err := paypal.NewRequestID()
	if err != nil {
		return run, err
	}
	order, err := svc.Checkout(ctx, traceID, "maple-mug", 1)
	if err != nil {
		var rejected *merchant.CreateRejectedError
		if !asCreateRejected(err, &rejected) {
			return run, err
		}
		run.OrderID = rejected.Order.ID
		run.OrderStatus = rejected.Order.Status
		run.TotalMinor = rejected.Order.TotalMinor
		run.Currency = rejected.Order.Currency
		run.CheckoutError = err.Error()
		run.Facts["checkout_rejected"] = "true"
		run.Observations = provider.Observations()
		return run, nil
	}

	run.OrderID = order.ID
	run.ProviderOrderID = order.ProviderOrderID
	run.TotalMinor = order.TotalMinor
	run.Currency = order.Currency

	// Buyer approval is a real lifecycle step of the scripted provider, never
	// implied by order creation.
	provider.ApproveOrder(order.ProviderOrderID)
	if _, err := svc.Observe(ctx, order.ID); err != nil {
		run.Facts["observe_error"] = err.Error()
	}

	if _, err := svc.Capture(ctx, order.ID); err != nil {
		run.CaptureError = err.Error()
		run.Facts["capture_error"] = err.Error()
	}

	detail, err := h.Store.OrderDetail(ctx, order.ID)
	if err != nil {
		return run, err
	}
	run.OrderStatus = detail.Order.Status
	for i := len(detail.Operations) - 1; i >= 0; i-- {
		op := detail.Operations[i]
		if op.Type == "CAPTURE" && op.Status == merchant.OpStatusSucceeded {
			run.ClaimedCapture = op.ProviderCaptureID
			break
		}
	}
	run.Observations = provider.Observations()
	return run, nil
}

// asCreateRejected is errors.As without importing errors in every caller's
// mental model; kept tiny and explicit.
func asCreateRejected(err error, target **merchant.CreateRejectedError) bool {
	for err != nil {
		if re, ok := err.(*merchant.CreateRejectedError); ok {
			*target = re
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// ScenarioMeta names the fixture evaluation for the oracle bundle.
func (r FixtureRun) ScenarioMeta() oracle.ScenarioMeta {
	return oracle.ScenarioMeta{
		ID:           "fixture-" + r.Fault,
		Title:        "Merchant fixture journey with injected defect " + r.Fault,
		Family:       r.Fault,
		Description:  "checkout → buyer approval → capture against the deterministic scripted provider; the oracle judges the durable journal",
		EvidenceTier: merchant.TierFault,
	}
}

// ToOracleEvidence assembles the oracle input from the durable journal (read
// by the oracle's own SQL) and the harness-recorded provider observations.
func (r FixtureRun) ToOracleEvidence(ctx context.Context, h *Harness) (oracle.Evidence, error) {
	rec := RunRecord{
		ScenarioID:   r.ScenarioMeta().ID,
		Candidate:    Candidate("candidate"),
		OrderIDs:     []string{r.OrderID},
		Observations: r.Observations,
		Facts:        r.Facts,
	}
	if rec.Facts == nil {
		rec.Facts = map[string]string{}
	}
	return rec.ToEvidence(ctx, h.Pool, Scenario{
		ID:           r.ScenarioMeta().ID,
		Title:        r.ScenarioMeta().Title,
		Family:       r.ScenarioMeta().Family,
		Description:  r.ScenarioMeta().Description,
		EvidenceTier: merchant.TierFault,
	}, Candidate("candidate"))
}

// FixtureVerdict is the trusted acceptance decision. It deliberately keeps the
// raw oracle result separate from the acceptance reason: "the oracle ran" and
// "the repair is acceptable" are different statements.
type FixtureVerdict struct {
	Fault             string        `json:"fault"`
	OrderID           string        `json:"order_id"`
	OrderStatus       string        `json:"order_status"`
	ClaimedCaptureID  string        `json:"claimed_capture_id,omitempty"`
	ProviderCaptureID string        `json:"provider_capture_id,omitempty"`
	Accepted          bool          `json:"accepted"`
	Reason            string        `json:"reason"`
	Oracle            oracle.Result `json:"oracle"`

	// Evidence is the full oracle input, carried for the proof bundle. It is
	// never serialized into eval.json: bundles are the artifact of record.
	Evidence oracle.Evidence `json:"-"`
}

// EvaluateFixture runs the journey and judges it. Acceptance requires BOTH a
// financially correct provider-observed capture and an oracle verdict of
// VERIFIED_FOR_TESTED_SCENARIOS with I6 applicable and passing — a build or a
// green unit suite is never sufficient.
func EvaluateFixture(ctx context.Context, h *Harness, fault string) (FixtureVerdict, error) {
	run, err := RunFixtureFault(ctx, h, fault)
	if err != nil {
		return FixtureVerdict{}, err
	}
	return JudgeFixtureRun(ctx, h, run)
}

// JudgeFixtureRun applies the acceptance rules to an already-executed run, so
// callers that need the raw evidence (the repair pipeline) do not have to
// execute the journey twice.
func JudgeFixtureRun(ctx context.Context, h *Harness, run FixtureRun) (FixtureVerdict, error) {
	fault := run.Fault
	ev, err := run.ToOracleEvidence(ctx, h)
	if err != nil {
		return FixtureVerdict{}, err
	}
	res := oracle.Evaluate(&ev)

	v := FixtureVerdict{
		Fault:            fault,
		OrderID:          run.OrderID,
		OrderStatus:      run.OrderStatus,
		ClaimedCaptureID: run.ClaimedCapture,
		Oracle:           res,
		Evidence:         ev,
	}
	for _, obs := range ev.Provider {
		if obs.Method == "capture" && obs.Applied && obs.CaptureID != "" {
			v.ProviderCaptureID = obs.CaptureID
		}
	}

	i6 := invariantStatus(res, "I6")
	switch {
	case run.OrderID == "":
		v.Reason = "the journey produced no local order"
	case run.OrderStatus != "CAPTURED_VERIFIED":
		v.Reason = fmt.Sprintf("order did not reach CAPTURED_VERIFIED (status %s)%s", run.OrderStatus, suffixIf(run.CheckoutError, run.CaptureError))
	case res.Verdict != oracle.VerdictVerified:
		v.Reason = fmt.Sprintf("oracle verdict is %s, not VERIFIED_FOR_TESTED_SCENARIOS", res.Verdict)
	case i6 != oracle.StatusPass:
		v.Reason = fmt.Sprintf("the capture identity invariant I6 is %s", i6)
	case v.ProviderCaptureID == "":
		v.Reason = "no applied provider capture observation carries a capture id"
	case v.ClaimedCaptureID != v.ProviderCaptureID:
		v.Reason = fmt.Sprintf("the journal certifies capture %q but the provider issued %q", v.ClaimedCaptureID, v.ProviderCaptureID)
	default:
		v.Accepted = true
		v.Reason = "provider-issued capture identity, amount, currency and terminal status match the journal; the oracle verified the tested scenario"
	}
	if !v.Accepted {
		if cp := firstCounterexample(res); cp != "" {
			v.Reason += "; counterexample: " + cp
		}
	}
	return v, nil
}

func invariantStatus(res oracle.Result, id string) oracle.Status {
	for _, inv := range res.Invariants {
		if inv.ID == id {
			return inv.Status
		}
	}
	return oracle.StatusNotApplicable
}

func firstCounterexample(res oracle.Result) string {
	for _, inv := range res.Counterexamples() {
		return fmt.Sprintf("%s: %s", inv.ID, inv.Summary)
	}
	return ""
}

func suffixIf(parts ...string) string {
	for _, p := range parts {
		if p != "" {
			return ": " + p
		}
	}
	return ""
}
