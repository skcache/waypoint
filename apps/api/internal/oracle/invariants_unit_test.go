// Synthetic unit tests for the oracle's invariant checks. These use crafted
// Evidence values (no database, no harness) so the judge's behavior is pinned
// independently of the fixture and the replay machinery.
package oracle_test

import (
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/oracle"
)

func statusOf(t *testing.T, ev *oracle.Evidence, id string) oracle.Status {
	t.Helper()
	for _, r := range oracle.RunInvariants(ev) {
		if r.ID == id {
			return r.Status
		}
	}
	t.Fatalf("invariant %s missing", id)
	return ""
}

func baseOrder() oracle.LocalOrder {
	return oracle.LocalOrder{
		ID: "ord-1", ProductID: "maple-mug", Quantity: 1,
		TotalMinor: 1234, Currency: "USD", Status: "CAPTURED_VERIFIED", ProviderOrderID: "REF-1",
	}
}

func goodCaptureObs() oracle.ProviderObs {
	return oracle.ProviderObs{
		Seq: 3, Method: "capture", OrderRef: "REF-1", RequestID: "req-1",
		DurableBeforeCall: true, SentAmount: "12.34", SentCurrency: "USD",
		Applied: true, CaptureID: "CAP-1", CaptureStatus: "COMPLETED",
		CaptureAmount: "12.34", CaptureCurrency: "USD",
		Source: "scripted-fixture", Tier: oracle.TierInjected,
	}
}

func TestI1RejectsFloatFormattedAppliedAmount(t *testing.T) {
	ev := &oracle.Evidence{
		Orders: []oracle.LocalOrder{baseOrder()},
		Provider: []oracle.ProviderObs{{
			Seq: 1, Method: "create", OrderRef: "REF-1", RequestID: "req-0",
			DurableBeforeCall: true, SentAmount: "12.340000", SentCurrency: "USD",
			Applied: true, Source: "scripted-fixture", Tier: oracle.TierInjected,
		}},
	}
	if got := statusOf(t, ev, "I1"); got != oracle.StatusFail {
		t.Fatalf("I1 = %s, want FAIL for a float-formatted applied amount", got)
	}
}

func TestI1RejectsWrongAmount(t *testing.T) {
	ev := &oracle.Evidence{
		Orders:   []oracle.LocalOrder{baseOrder()},
		Provider: []oracle.ProviderObs{goodCaptureObs()},
	}
	ev.Provider[0].SentAmount = "12.99"
	if got := statusOf(t, ev, "I1"); got != oracle.StatusFail {
		t.Fatalf("I1 = %s, want FAIL when the sent amount differs from the order total", got)
	}
}

func TestI6MissingEvidenceIsInconclusiveNeverPass(t *testing.T) {
	ev := &oracle.Evidence{
		Orders: []oracle.LocalOrder{baseOrder()},
		Events: []oracle.LocalEvent{},
	}
	if got := statusOf(t, ev, "I6"); got != oracle.StatusInconclusive {
		t.Fatalf("I6 = %s, want INCONCLUSIVE when the provider evidence is missing", got)
	}
}

func TestI6ContradictoryEvidenceIsUnsafe(t *testing.T) {
	order, op := claimedOrder("CAP-1") // claim identity present; amount will contradict
	ev := &oracle.Evidence{
		Orders:     []oracle.LocalOrder{order},
		Operations: []oracle.LocalOp{op},
		Provider:   []oracle.ProviderObs{goodCaptureObs()},
	}
	ev.Provider[0].CaptureAmount = "99.99" // provider captured a different amount
	if got := statusOf(t, ev, "I6"); got != oracle.StatusFail {
		t.Fatalf("I6 = %s, want FAIL for contradictory evidence", got)
	}
}

func TestI6MatchingEvidencePasses(t *testing.T) {
	order, op := claimedOrder("CAP-1")
	ev := &oracle.Evidence{
		Orders:     []oracle.LocalOrder{order},
		Operations: []oracle.LocalOp{op},
		Provider:   []oracle.ProviderObs{goodCaptureObs()},
	}
	if got := statusOf(t, ev, "I6"); got != oracle.StatusPass {
		t.Fatalf("I6 = %s, want PASS with matching identity and financial evidence", got)
	}
}

func TestI4FailsWithoutDurableIdentity(t *testing.T) {
	obs := goodCaptureObs()
	obs.DurableBeforeCall = false
	ev := &oracle.Evidence{
		Orders: []oracle.LocalOrder{baseOrder()},
		Operations: []oracle.LocalOp{{
			ID: "op-1", OrderID: "ord-1", Type: "CAPTURE", ProviderRequestID: "req-1", Status: "SUCCEEDED",
		}},
		Provider: []oracle.ProviderObs{obs},
	}
	if got := statusOf(t, ev, "I4"); got != oracle.StatusFail {
		t.Fatalf("I4 = %s, want FAIL when the call preceded the durable identity", got)
	}
}

func TestI5FailsOnNewIdentityAfterAmbiguity(t *testing.T) {
	dropped := goodCaptureObs()
	dropped.ResponseDropped = true
	dropped.CaptureStatus = ""
	dropped.CaptureAmount = ""
	dropped.CaptureCurrency = ""
	second := goodCaptureObs()
	second.Seq = 5
	second.RequestID = "req-2"
	ev := &oracle.Evidence{
		Orders: []oracle.LocalOrder{baseOrder()},
		Operations: []oracle.LocalOp{
			{ID: "op-1", OrderID: "ord-1", Type: "CAPTURE", ProviderRequestID: "req-1", Status: "REJECTED"},
			{ID: "op-2", OrderID: "ord-1", Type: "CAPTURE", ProviderRequestID: "req-2", Status: "SUCCEEDED"},
		},
		Provider: []oracle.ProviderObs{dropped, second},
	}
	if got := statusOf(t, ev, "I5"); got != oracle.StatusFail {
		t.Fatalf("I5 = %s, want FAIL when a new identity follows an ambiguous outcome", got)
	}
}

func TestI8FailsOnDowngradeAfterVerification(t *testing.T) {
	ev := &oracle.Evidence{
		Orders:   []oracle.LocalOrder{baseOrder()},
		Provider: []oracle.ProviderObs{goodCaptureObs()},
		Events: []oracle.LocalEvent{
			{ID: 10, OrderID: "ord-1", EventType: "PROVIDER_CAPTURE_CONFIRMED"},
			{ID: 11, OrderID: "ord-1", EventType: "CAPTURE_OUTCOME_UNKNOWN"},
		},
	}
	if got := statusOf(t, ev, "I8"); got != oracle.StatusFail {
		t.Fatalf("I8 = %s, want FAIL when a verified capture is downgraded later", got)
	}
}

func TestI12FailsOnScriptedEvidenceClaimingRealTier(t *testing.T) {
	obs := goodCaptureObs()
	obs.Tier = "R" // a scripted interaction may never claim real provenance
	ev := &oracle.Evidence{
		Scenario: oracle.ScenarioMeta{ID: "x", EvidenceTier: "F"},
		Orders:   []oracle.LocalOrder{baseOrder()},
		Provider: []oracle.ProviderObs{obs},
	}
	if got := statusOf(t, ev, "I12"); got != oracle.StatusFail {
		t.Fatalf("I12 = %s, want FAIL when scripted evidence claims tier R", got)
	}
}

func TestVerdictAggregationPrecedence(t *testing.T) {
	mk := func(f, i, p, na int) []oracle.InvariantResult {
		var out []oracle.InvariantResult
		add := func(n int, s oracle.Status) {
			for j := 0; j < n; j++ {
				out = append(out, oracle.InvariantResult{Status: s})
			}
		}
		add(f, oracle.StatusFail)
		add(i, oracle.StatusInconclusive)
		add(p, oracle.StatusPass)
		add(na, oracle.StatusNotApplicable)
		return out
	}
	if got := oracle.CombineVerdict(mk(1, 1, 1, 9)); got != oracle.VerdictUnsafe {
		t.Fatalf("fail+inconclusive = %s, want UNSAFE", got)
	}
	if got := oracle.CombineVerdict(mk(0, 1, 5, 6)); got != oracle.VerdictInconclusive {
		t.Fatalf("inconclusive only = %s, want INCONCLUSIVE", got)
	}
	if got := oracle.CombineVerdict(mk(0, 0, 3, 9)); got != oracle.VerdictVerified {
		t.Fatalf("passes only = %s, want VERIFIED", got)
	}
	if got := oracle.CombineVerdict(mk(0, 0, 0, 12)); got != oracle.VerdictInconclusive {
		t.Fatalf("nothing applicable = %s, want INCONCLUSIVE (never success by absence)", got)
	}
}

// --- M2 review corrections: false-green regressions -------------------------

// claimedOrder builds a CAPTURED_VERIFIED order with one SUCCEEDED capture
// operation carrying the given local capture id.
func claimedOrder(localCaptureID string) (oracle.LocalOrder, oracle.LocalOp) {
	o := baseOrder()
	op := oracle.LocalOp{
		ID: "op-1", OrderID: "ord-1", Type: "CAPTURE", ProviderRequestID: "req-1",
		Status: "SUCCEEDED", ProviderCaptureID: localCaptureID, ProviderOrderID: "REF-1",
	}
	return o, op
}

// A local claim of capture id CAP-FAKE must not be corroborated by a provider
// that only ever produced CAP-REAL — even when the amount and currency match.
// Contradictory identity evidence is FAIL / unsafe.
func TestI6RejectsCaptureIDMismatchDespiteMatchingAmount(t *testing.T) {
	order, op := claimedOrder("CAP-FAKE")
	obs := goodCaptureObs()
	obs.CaptureID = "CAP-REAL"
	ev := &oracle.Evidence{
		Orders:     []oracle.LocalOrder{order},
		Operations: []oracle.LocalOp{op},
		Provider:   []oracle.ProviderObs{obs},
	}
	if got := statusOf(t, ev, "I6"); got != oracle.StatusFail {
		t.Fatalf("I6 = %s, want FAIL when the local capture id contradicts the provider capture id", got)
	}
}

// A verified claim with no local capture identity cannot be bound to provider
// evidence: the required identity evidence is missing → INCONCLUSIVE.
func TestI6InconclusiveWhenLocalCaptureIdentityIsMissing(t *testing.T) {
	order, op := claimedOrder("")
	obs := goodCaptureObs()
	ev := &oracle.Evidence{
		Orders:     []oracle.LocalOrder{order},
		Operations: []oracle.LocalOp{op},
		Provider:   []oracle.ProviderObs{obs},
	}
	if got := statusOf(t, ev, "I6"); got != oracle.StatusInconclusive {
		t.Fatalf("I6 = %s, want INCONCLUSIVE when the local claim carries no capture identity", got)
	}
}

// One matching observation must not hide another materially contradictory
// observation for the same order.
func TestI6MatchingObservationCannotHideContradiction(t *testing.T) {
	order, op := claimedOrder("CAP-REAL")
	match := goodCaptureObs()
	match.CaptureID = "CAP-REAL"
	contradiction := goodCaptureObs()
	contradiction.Seq = 9
	contradiction.RequestID = "req-other"
	contradiction.CaptureID = "CAP-OTHER"
	ev := &oracle.Evidence{
		Orders:     []oracle.LocalOrder{order},
		Operations: []oracle.LocalOp{op},
		Provider:   []oracle.ProviderObs{match, contradiction},
	}
	if got := statusOf(t, ev, "I6"); got != oracle.StatusFail {
		t.Fatalf("I6 = %s, want FAIL: a matching observation must not hide a contradictory one", got)
	}
}

// Approval evidence must belong to the SAME order: an APPROVED read for order
// B must not authorize a capture for order A.
func TestI3ApprovalMustBelongToTheCapturedOrder(t *testing.T) {
	ord1 := baseOrder()
	ord1.ID = "ord-1"
	ord1.ProviderOrderID = "REF-1"
	ord1.Status = "CAPTURE_OUTCOME_UNKNOWN"
	ord2 := baseOrder()
	ord2.ID = "ord-2"
	ord2.ProviderOrderID = "REF-2"
	ord2.Status = "APPROVED"

	approvalForOther := oracle.ProviderObs{
		Seq: 1, Method: "read", OrderRef: "REF-2", OrderStatus: "APPROVED",
		Source: "scripted-fixture", Tier: oracle.TierInjected,
	}
	captureForOrd1 := oracle.ProviderObs{
		Seq: 2, Method: "capture", OrderRef: "REF-1", RequestID: "req-1",
		DurableBeforeCall: true, SentAmount: "12.34", SentCurrency: "USD",
		ResponseDropped: true,
		Source:          "scripted-fixture", Tier: oracle.TierInjected,
	}
	ev := &oracle.Evidence{
		Orders: []oracle.LocalOrder{ord1, ord2},
		Operations: []oracle.LocalOp{{
			ID: "op-1", OrderID: "ord-1", Type: "CAPTURE", ProviderRequestID: "req-1", Status: "UNKNOWN",
		}},
		Provider: []oracle.ProviderObs{approvalForOther, captureForOrd1},
	}
	if got := statusOf(t, ev, "I3"); got != oracle.StatusFail {
		t.Fatalf("I3 = %s, want FAIL: order B's approval must not authorize order A's capture", got)
	}
}

// Journal provenance: a scripted (tier-F) run may not carry PAYPAL-source
// journal events labelled tier R.
func TestI12RejectsScriptedJournalEventsLabeledReal(t *testing.T) {
	order, op := claimedOrder("CAP-REAL")
	obs := goodCaptureObs()
	ev := &oracle.Evidence{
		Scenario:   oracle.ScenarioMeta{ID: "x", EvidenceTier: oracle.TierInjected},
		Orders:     []oracle.LocalOrder{order},
		Operations: []oracle.LocalOp{op},
		Provider:   []oracle.ProviderObs{obs},
		Events: []oracle.LocalEvent{{
			ID: 7, OrderID: "ord-1", Source: "PAYPAL",
			EventType: "PROVIDER_CAPTURE_CONFIRMED", Tier: oracle.TierReal,
		}},
	}
	if got := statusOf(t, ev, "I12"); got != oracle.StatusFail {
		t.Fatalf("I12 = %s, want FAIL when a scripted run labels a PAYPAL journal event as tier R", got)
	}

	// The same journal labelled F is honest and must not fail I12.
	ev.Events[0].Tier = oracle.TierInjected
	if got := statusOf(t, ev, "I12"); got == oracle.StatusFail {
		t.Fatalf("I12 = %s, want a non-FAIL status for an honestly-labelled tier-F journal", got)
	}
}

// --- M2 final correction: full capture identity chain -----------------------

// A capture observation whose request id identifies only a CREATE_ORDER
// operation does not establish a durable CAPTURE identity: I4 must fail.
func TestI4RejectsCaptureRequestIDBelongingToCreateOperation(t *testing.T) {
	order := baseOrder()
	order.Status = "CAPTURE_OUTCOME_UNKNOWN"
	createOp := oracle.LocalOp{
		ID: "op-create", OrderID: "ord-1", Type: "CREATE_ORDER",
		ProviderRequestID: "req-create-1", Status: "SUCCEEDED", ProviderOrderID: "REF-1",
	}
	obs := goodCaptureObs()
	obs.RequestID = "req-create-1" // reuses the CREATE operation's request id
	ev := &oracle.Evidence{
		Orders:     []oracle.LocalOrder{order},
		Operations: []oracle.LocalOp{createOp},
		Provider:   []oracle.ProviderObs{obs},
	}
	if got := statusOf(t, ev, "I4"); got != oracle.StatusFail {
		t.Fatalf("I4 = %s, want FAIL when the capture request id belongs only to a CREATE_ORDER operation", got)
	}
}

// The right capture id and amount under the WRONG provider request id is
// contradictory identity evidence: I6 must fail.
func TestI6RejectsProviderRequestIDMismatch(t *testing.T) {
	order, op := claimedOrder("CAP-1") // claim request id: req-1
	obs := goodCaptureObs()            // capture id CAP-1, amount 12.34
	obs.RequestID = "req-OTHER"        // executed under a different request id
	ev := &oracle.Evidence{
		Orders:     []oracle.LocalOrder{order},
		Operations: []oracle.LocalOp{op},
		Provider:   []oracle.ProviderObs{obs},
	}
	if got := statusOf(t, ev, "I6"); got != oracle.StatusFail {
		t.Fatalf("I6 = %s, want FAIL when the provider capture was executed under a different request id", got)
	}
}

// A capture observation attributed to one provider order whose request id
// belongs to a CAPTURE operation of ANOTHER merchant order is rejected.
func TestI4RejectsCaptureObservationBelongingToAnotherOrder(t *testing.T) {
	ord1 := baseOrder() // REF-1, claims nothing
	ord1.Status = "CAPTURE_OUTCOME_UNKNOWN"
	ord2 := baseOrder()
	ord2.ID = "ord-2"
	ord2.ProviderOrderID = "REF-2"
	ord2.Status = "CAPTURED_VERIFIED"

	otherOrderCaptureOp := oracle.LocalOp{
		ID: "op-2", OrderID: "ord-2", Type: "CAPTURE",
		ProviderRequestID: "req-b", Status: "SUCCEEDED", ProviderCaptureID: "CAP-B",
		ProviderOrderID: "REF-2",
	}
	obs := goodCaptureObs()
	obs.OrderRef = "REF-1"  // claims to be order 1's capture...
	obs.RequestID = "req-b" // ...but the request id belongs to order 2's operation
	obs.CaptureID = "CAP-B"
	ev := &oracle.Evidence{
		Orders:     []oracle.LocalOrder{ord1, ord2},
		Operations: []oracle.LocalOp{otherOrderCaptureOp},
		Provider:   []oracle.ProviderObs{obs},
	}
	if got := statusOf(t, ev, "I4"); got != oracle.StatusFail {
		t.Fatalf("I4 = %s, want FAIL when the capture observation borrows another order's operation identity", got)
	}
}

// The full chain — order, durable CAPTURE operation, request id, capture id,
// amount, currency, status — must pass when everything genuinely agrees.
func TestI6FullyMatchingIdentityChainPasses(t *testing.T) {
	order, op := claimedOrder("CAP-1")
	obs := goodCaptureObs() // request id req-1, capture CAP-1, 12.34 USD, COMPLETED
	ev := &oracle.Evidence{
		Orders:     []oracle.LocalOrder{order},
		Operations: []oracle.LocalOp{op},
		Provider:   []oracle.ProviderObs{obs},
	}
	if got := statusOf(t, ev, "I6"); got != oracle.StatusPass {
		t.Fatalf("I6 = %s, want PASS for a fully matching identity chain", got)
	}
}

// Read-only recovery: the provider does not expose the original request
// header on a read. That absence is legitimate — it must corroborate on
// capture identity, amount, currency, and status without inventing request-id
// evidence.
func TestI6ReadOnlyRecoveryWithoutRequestHeaderPasses(t *testing.T) {
	order, op := claimedOrder("CAP-1")
	read := oracle.ProviderObs{
		Seq: 4, Method: "read", OrderRef: "REF-1", // no request id: reads carry no header
		OrderStatus: "COMPLETED", CaptureID: "CAP-1", CaptureStatus: "COMPLETED",
		CaptureAmount: "12.34", CaptureCurrency: "USD",
		Source: "scripted-fixture", Tier: oracle.TierInjected,
	}
	ev := &oracle.Evidence{
		Orders:     []oracle.LocalOrder{order},
		Operations: []oracle.LocalOp{op},
		Provider:   []oracle.ProviderObs{read},
	}
	if got := statusOf(t, ev, "I6"); got != oracle.StatusPass {
		t.Fatalf("I6 = %s, want PASS from read-only capture evidence without a request header", got)
	}
}

// In contrast, an instrumented capture CALL without a request id lacks the
// identity the chain requires: missing evidence, not a contradiction.
func TestI6CaptureCallWithoutRequestIdentityIsInconclusive(t *testing.T) {
	order, op := claimedOrder("CAP-1")
	obs := goodCaptureObs()
	obs.RequestID = ""
	ev := &oracle.Evidence{
		Orders:     []oracle.LocalOrder{order},
		Operations: []oracle.LocalOp{op},
		Provider:   []oracle.ProviderObs{obs},
	}
	if got := statusOf(t, ev, "I6"); got != oracle.StatusInconclusive {
		t.Fatalf("I6 = %s, want INCONCLUSIVE when an instrumented capture call carries no request identity", got)
	}
}
