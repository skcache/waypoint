// Package oracle is the Waypoint V2 deterministic financial-correctness
// oracle: an independently authored invariant engine that judges the merchant
// fixture and its provider evidence.
//
// Trust boundary (normative): the AI component is never the authority deciding
// financial correctness. This package evaluates invariants I1–I12 as ordinary
// deterministic Go code over durable evidence only:
//
//   - local PostgreSQL journal rows, read with this package's OWN SQL
//     (load.go) — never through the fixture's service layer;
//   - provider observations recorded by the replay harness (scripted fixture
//     provider, tier F) or by a real provider integration (tier R).
//
// Scope note (enforced by scope_test.go): this package must never import the
// merchant fixture, the replay harness, the LLM adapter, or any cmd package.
// A candidate patch's writable scope must not reach the judge.
//
// Verdict vocabulary (FINANCIAL_CORRECTNESS_AND_SECURITY.md §1):
//
//	VERIFIED_FOR_TESTED_SCENARIOS — every applicable invariant PASSed for the
//	  executed scenarios; NOT a guarantee about unseen failures.
//	UNSAFE — a tested, reproducible counterexample violates an invariant.
//	INCONCLUSIVE — applicable evidence is missing, incomplete, or
//	  contradictory; never converted to success.
package oracle

import "time"

// Status is one invariant's outcome for one scenario.
type Status string

const (
	StatusPass          Status = "PASS"
	StatusFail          Status = "FAIL"
	StatusInconclusive  Status = "INCONCLUSIVE"
	StatusNotApplicable Status = "NOT_APPLICABLE"
)

// Verdict is the combined result for a candidate under a scenario.
type Verdict string

const (
	VerdictVerified     Verdict = "VERIFIED_FOR_TESTED_SCENARIOS"
	VerdictUnsafe       Verdict = "UNSAFE"
	VerdictInconclusive Verdict = "INCONCLUSIVE"
)

// Evidence tiers (normative labels): R = real PayPal Sandbox, F = local
// injection/scripted fixture, S = sample.
const (
	TierReal     = "R"
	TierInjected = "F"
	TierSample   = "S"
)

// LocalOrder mirrors one durable merchant_orders row. The oracle reads these
// itself; nothing here is accepted from fixture code as a claim.
type LocalOrder struct {
	ID              string
	ProductID       string
	Quantity        int
	TotalMinor      int64
	Currency        string
	Status          string
	ProviderOrderID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// LocalOp mirrors one durable merchant_operations row.
type LocalOp struct {
	ID                 string
	OrderID            string
	Type               string
	ProviderRequestID  string
	PayloadFingerprint string
	Status             string
	ProviderOrderID    string
	ProviderCaptureID  string
	AttemptCount       int
	OwnerID            string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// LocalEvent mirrors one append-only merchant_events row.
type LocalEvent struct {
	ID        int64
	OrderID   string
	Source    string
	EventType string
	Tier      string
	Detail    string // raw JSON
	CreatedAt time.Time
}

// ProviderObs is one recorded provider interaction, produced by trusted test
// instrumentation (the replay harness's scripted provider) or a real provider
// integration. It is the only acceptable source of "what the provider saw".
type ProviderObs struct {
	Seq               int // recording order (causal)
	At                time.Time
	Method            string // create | capture | read
	OrderRef          string // provider order id
	RequestID         string // Provider-Request-Id sent (create/capture)
	DurableBeforeCall bool   // the request id row existed in the DB BEFORE the call
	SentAmount        string // raw amount value sent
	SentCurrency      string
	Applied           bool // the provider executed the mutation effect
	Replayed          bool // an idempotent replay of an earlier request id
	ResponseDropped   bool // mutation applied, but the response was lost/unreadable
	DefiniteRefusal   bool // provider refused without processing (4xx family)
	OrderStatus       string
	CaptureID         string
	CaptureStatus     string
	CaptureAmount     string
	CaptureCurrency   string
	Stale             bool // read served from a deliberately stale snapshot
	Source            string
	Tier              string
}

// FaultEvent records one deterministic injection (schedule point) applied by
// the harness.
type FaultEvent struct {
	Seq    int
	Phase  string
	Detail string
	At     time.Time
}

// ScenarioMeta describes the executed scenario so the verdict is scoped and
// the provenance tier is explicit.
type ScenarioMeta struct {
	ID                    string
	Title                 string
	Family                string
	Description           string
	ExpectedVerdict       Verdict
	EvidenceTier          string
	UnsafeCandidate       bool
	RequiresProviderProof bool
}

// Evidence is the complete input to evaluation. Constructed by the harness
// (replay) plus this package's own SQL loader; never by fixture service code.
type Evidence struct {
	Scenario   ScenarioMeta
	Candidate  string
	Orders     []LocalOrder
	Operations []LocalOp
	Events     []LocalEvent
	Provider   []ProviderObs
	Faults     []FaultEvent
	Facts      map[string]string
}

// order returns the order with the given id.
func (e *Evidence) order(id string) *LocalOrder {
	for i := range e.Orders {
		if e.Orders[i].ID == id {
			return &e.Orders[i]
		}
	}
	return nil
}

// orderIDs returns every local order id, in input order.
func (e *Evidence) orderIDs() []string {
	ids := make([]string, 0, len(e.Orders))
	for _, o := range e.Orders {
		ids = append(ids, o.ID)
	}
	return ids
}

// captureObsForRequest returns the capture observations for one request id.
func (e *Evidence) captureObsForRequest(requestID string) []ProviderObs {
	var out []ProviderObs
	for _, o := range e.Provider {
		if o.Method == "capture" && o.RequestID == requestID {
			out = append(out, o)
		}
	}
	return out
}

// capturesForOrderRef returns every instrumented capture CALL for a provider
// order (used for effect counting and identity continuity).
func (e *Evidence) capturesForOrderRef(ref string) []ProviderObs {
	var out []ProviderObs
	for _, o := range e.Provider {
		if o.Method == "capture" && o.OrderRef == ref {
			out = append(out, o)
		}
	}
	return out
}

// captureEvidenceForOrderRef returns everything that carries a capture
// identity for an order: instrumented capture calls AND read observations
// that expose a capture resource. Reads legitimately carry no request header
// (the provider does not echo the original request id on a GET), so consumers
// must treat a missing request id on a read as absent — not contradictory —
// evidence.
func (e *Evidence) captureEvidenceForOrderRef(ref string) []ProviderObs {
	var out []ProviderObs
	for _, o := range e.Provider {
		if o.OrderRef != ref {
			continue
		}
		switch {
		case o.Method == "capture":
			out = append(out, o)
		case o.Method == "read" && o.CaptureID != "":
			out = append(out, o)
		}
	}
	return out
}

// readsForOrderRef returns every read observation for a provider order.
func (e *Evidence) readsForOrderRef(ref string) []ProviderObs {
	var out []ProviderObs
	for _, o := range e.Provider {
		if o.Method == "read" && o.OrderRef == ref {
			out = append(out, o)
		}
	}
	return out
}

// eventsOfType reports whether an event type appears for an order.
func (e *Evidence) eventSeen(orderID, eventType string) bool {
	for _, ev := range e.Events {
		if ev.OrderID == orderID && ev.EventType == eventType {
			return true
		}
	}
	return false
}

// opByRequest finds the capture operation for a request id.
func (e *Evidence) opByRequest(requestID string) *LocalOp {
	for i := range e.Operations {
		if e.Operations[i].ProviderRequestID == requestID {
			return &e.Operations[i]
		}
	}
	return nil
}

// captureOps returns all capture operations for an order, creation order.
func (e *Evidence) captureOps(orderID string) []LocalOp {
	var out []LocalOp
	for _, op := range e.Operations {
		if op.OrderID == orderID && op.Type == "CAPTURE" {
			out = append(out, op)
		}
	}
	return out
}
