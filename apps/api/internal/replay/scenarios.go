package replay

import "github.com/skcache/waypoint/apps/api/internal/oracle"

// StandardScenarios is the M2 deterministic fault catalog. Each scenario
// declares its expected oracle verdict; the acceptance tests assert the
// oracle agrees with the DECLARATION (never adjusted afterward to fit).
//
// Families follow EVALUATION_AND_DEMO.md §2: lost response, duplicate
// delivery, concurrency, process restart, stale reads, plus the mutation set
// (knowingly unsafe candidates the oracle must reject).
var StandardScenarios = []Scenario{
	{
		ID:              "ref-happy-path",
		Title:           "Reference implementation: valid checkout, approval, capture, stale read after verification",
		Family:          "baseline",
		Description:     "A valid checkout with observed approval and provider-confirmed capture, then a deliberately stale provider read after verification. Establishes the known-good reference behavior and proves a stale read cannot downgrade a verified capture (I8).",
		ExpectedVerdict: oracle.VerdictVerified,
		EvidenceTier:    oracle.TierInjected,
		Steps: []Step{
			{Action: StepCheckout, ProductID: "maple-mug", Quantity: 1},
			{Action: StepApprove},
			{Action: StepStaleSnapshot},
			{Action: StepCapture},
			{Action: StepObserve},
			{Action: StepResolveStale},
		},
	},
	{
		ID:               "ref-duplicate-delivery-same-id",
		Title:            "Duplicate delivery of the same logical capture (idempotent request id)",
		Family:           "duplicate",
		Description:      "The capture response is lost after the provider applied the mutation, and provider reads lag behind the capture. Retrying the capture must reuse the SAME request id; the provider's idempotency returns the original capture and no second effect occurs (I4/I5/I7).",
		ExpectedVerdict:  oracle.VerdictVerified,
		EvidenceTier:     oracle.TierInjected,
		Schedule:         FaultSchedule{Capture: FaultDropResponseAfterMutation},
		HideCaptureReads: true,
		Steps: []Step{
			{Action: StepCheckout, ProductID: "maple-mug", Quantity: 1},
			{Action: StepApprove},
			{Action: StepCapture, Quantity: 1},
			// Reads still lag, so the retry goes over the wire and exercises
			// the provider's request-id idempotency (original capture returned).
			{Action: StepCapture},
			{Action: StepResolveStale},
		},
	},
	{
		ID:              "ref-concurrent-delivery",
		Title:           "Concurrent capture deliveries produce exactly one operation and one effect",
		Family:          "concurrency",
		Description:     "Three concurrent capture requests race. Exactly one durable capture operation and one provider effect may result; the others are refused before any outbound call (I4/I7).",
		ExpectedVerdict: oracle.VerdictVerified,
		EvidenceTier:    oracle.TierInjected,
		Steps: []Step{
			{Action: StepCheckout, ProductID: "maple-mug", Quantity: 1},
			{Action: StepApprove},
			{Action: StepCaptureConcurrent, Count: 3},
		},
	},
	{
		ID:              "ref-restart-crashed-begin",
		Title:           "Process restart after a durable capture begin with no outbound call",
		Family:          "restart",
		Description:     "The durable capture begin commits and the process dies before any outbound call (no lease recorded). After restart, read-only reconciliation demotes the attempt and a same-id retry completes it — one identity, one effect (I4/I5).",
		ExpectedVerdict: oracle.VerdictVerified,
		EvidenceTier:    oracle.TierInjected,
		Steps: []Step{
			{Action: StepCheckout, ProductID: "maple-mug", Quantity: 1},
			{Action: StepApprove},
			{Action: StepSimulateCrashedBegin},
			{Action: StepRestart},
			{Action: StepReconcile},
			{Action: StepCapture},
		},
	},
	{
		ID:              "ref-live-owner-refusal",
		Title:           "Recovery worker refuses to race a live lease, then takes over after expiry",
		Family:          "concurrency",
		Description:     "A second worker holds a live lease on an in-flight capture. Reconciliation and capture must both be refused while the lease is live; after expiry the same request id is taken over and completed (M2-P1).",
		ExpectedVerdict: oracle.VerdictVerified,
		EvidenceTier:    oracle.TierInjected,
		Steps: []Step{
			{Action: StepCheckout, ProductID: "maple-mug", Quantity: 1},
			{Action: StepApprove},
			{Action: StepSimulateOwnedBegin},
			{Action: StepReconcile},
			{Action: StepCapture},
			{Action: StepExpireLease},
			{Action: StepReconcile},
			{Action: StepCapture},
		},
	},
	{
		ID:                  "fault-inconclusive-missing-proof",
		Title:               "Verified claim with provider proof genuinely unavailable is INCONCLUSIVE",
		Family:              "inconclusive",
		Description:         "The checkout completes and is marked verified, but the provider capture observations are unavailable to the verifier. Missing provider proof must produce INCONCLUSIVE — never VERIFIED.",
		ExpectedVerdict:     oracle.VerdictInconclusive,
		EvidenceTier:        oracle.TierInjected,
		OmitCaptureEvidence: true,
		Steps: []Step{
			{Action: StepCheckout, ProductID: "maple-mug", Quantity: 1},
			{Action: StepApprove},
			{Action: StepCapture},
		},
	},
	{
		ID:              "unsafe-identity-swap",
		Title:           "UNSAFE SEED: ambiguity treated as failure, capture reissued under a fresh identity",
		Family:          "mutation",
		Description:     "SEEDED UNSAFE PATCH (not AI-generated). After a lost capture response the seed force-closes the preserved attempt as failed and reissues the capture with a NEW request id, then reports success. The oracle must reject it on I4/I5/I7 with the recorded counterexample.",
		ExpectedVerdict: oracle.VerdictUnsafe,
		EvidenceTier:    oracle.TierInjected,
		Unsafe:          true,
		Schedule:        FaultSchedule{Capture: FaultDropResponseAfterMutation},
		Steps: []Step{
			{Action: StepCheckout, ProductID: "maple-mug", Quantity: 1},
			{Action: StepApprove},
			{Action: StepCapture},
		},
	},
	{
		ID:                    "unsafe-accept-mismatch",
		Title:                 "UNSAFE SEED: capture verified despite a provider amount mismatch",
		Family:                "mutation",
		Description:           "SEEDED UNSAFE PATCH (not AI-generated). The provider echo carries the wrong amount; the seed marks the order verified anyway. The oracle must reject it on I6: a completed-capture claim that the evidence contradicts.",
		ExpectedVerdict:       oracle.VerdictUnsafe,
		EvidenceTier:          oracle.TierInjected,
		Unsafe:                true,
		CaptureAmountOverride: "99.99",
		Steps: []Step{
			{Action: StepCheckout, ProductID: "maple-mug", Quantity: 1},
			{Action: StepApprove},
			{Action: StepCapture},
		},
	},
}

// ScenarioByID finds a scenario in the standard catalog.
func ScenarioByID(id string) (Scenario, bool) {
	for _, sc := range StandardScenarios {
		if sc.ID == id {
			return sc, true
		}
	}
	return Scenario{}, false
}
