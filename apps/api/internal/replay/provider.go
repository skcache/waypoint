// Package replay is the Waypoint V2 M2 fault harness: the "isolated replay
// runner". It drives the merchant fixture through deterministic scenarios
// against a SCRIPTED provider and records every provider interaction as
// evidence. It never touches a real PayPal endpoint; all interactions are
// labelled tier F (local injection).
//
// The harness is the producer of evidence; the oracle (internal/oracle) is
// the independent judge of it. The harness never evaluates invariants.
package replay

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// FaultMode names one deterministic provider-fault injection.
type FaultMode string

const (
	// FaultOff: the provider behaves normally.
	FaultOff FaultMode = ""
	// FaultDropResponseAfterMutation: the provider EXECUTES the mutation
	// (applies the effect), then the response is lost — the caller sees an
	// ambiguous outcome. This is the dropped-response-after-success fault.
	FaultDropResponseAfterMutation FaultMode = "drop_response_after_mutation"
	// FaultDefiniteRefusal: the provider refuses without processing (4xx).
	FaultDefiniteRefusal FaultMode = "definite_refusal"
	// FaultAmbiguousError: the provider applies the mutation and then returns
	// an ambiguous 5xx (lost-response variant).
	FaultAmbiguousError FaultMode = "ambiguous_error"
	// FaultAmbiguousNoEffect: the provider returns an ambiguous 5xx without
	// applying the mutation.
	FaultAmbiguousNoEffect FaultMode = "ambiguous_no_effect"
)

// FaultSchedule selects faults per provider method.
type FaultSchedule struct {
	Create  FaultMode
	Capture FaultMode
}

// Observation is one recorded provider interaction. It is the only acceptable
// account of "what the provider saw and did".
type Observation struct {
	Seq               int       `json:"seq"`
	At                time.Time `json:"at"`
	Method            string    `json:"method"` // create | capture | read
	OrderRef          string    `json:"order_ref"`
	RequestID         string    `json:"request_id,omitempty"`
	DurableBeforeCall bool      `json:"request_id_durable_before_call"`
	SentAmount        string    `json:"sent_amount,omitempty"`
	SentCurrency      string    `json:"sent_currency,omitempty"`
	Applied           bool      `json:"mutation_applied"`
	Replayed          bool      `json:"idempotent_replay"`
	ResponseDropped   bool      `json:"response_dropped"`
	DefiniteRefusal   bool      `json:"definite_refusal"`
	OrderStatus       string    `json:"order_status,omitempty"`
	CaptureID         string    `json:"capture_id,omitempty"`
	CaptureStatus     string    `json:"capture_status,omitempty"`
	CaptureAmount     string    `json:"capture_amount,omitempty"`
	CaptureCurrency   string    `json:"capture_currency,omitempty"`
	Stale             bool      `json:"stale_read"`
	Source            string    `json:"source"`
	Tier              string    `json:"tier"`
}

// providerOrder is the scripted provider's internal state for one order.
type providerOrder struct {
	id              string
	status          string // CREATED | APPROVED | COMPLETED
	amountValue     string
	currency        string
	captureID       string
	captureState    string // "" | PENDING | COMPLETED
	captureAmount   string
	captureCurrency string
}

// ScriptedProvider implements merchant.OrdersAPI deterministically. Every
// call is recorded; captures are idempotent by request id; the drop-response
// fault applies the mutation first and then withholds the response.
type ScriptedProvider struct {
	mu       sync.Mutex
	pool     *pgxpool.Pool // durability probe target (read-only)
	schedule FaultSchedule

	orders    map[string]*providerOrder
	byRequest map[string]string // request id -> provider order id
	byCapture map[string]string // capture request id -> capture id
	stale     map[string]providerOrder
	staleOn   bool

	// HideCaptureUntilResolved models provider-side read lag: while set, the
	// capture effect exists internally but reads still serve the pre-capture
	// view (used to exercise ambiguity handling and reconciliation).
	HideCaptureUntilResolved bool

	// CaptureAmountOverride / CaptureCurrencyOverride model a provider echo
	// that does not match the authorized order (the I6 mismatch family).
	CaptureAmountOverride   string
	CaptureCurrencyOverride string
	// CaptureStatusOverride models an asynchronous capture state (for example
	// PENDING) in the response while the capture effect exists.
	CaptureStatusOverride string

	observations []Observation
	seq          int
	captureSeq   int
	createSeq    int
}

// NewScriptedProvider builds the provider. pool may be nil; when set, the
// provider probes the durable journal before each outbound call and records
// whether the request id's operation row already existed (the I4 causal
// probe — the call can only happen because the row was committed first).
func NewScriptedProvider(pool *pgxpool.Pool, schedule FaultSchedule) *ScriptedProvider {
	return &ScriptedProvider{
		pool:      pool,
		schedule:  schedule,
		orders:    map[string]*providerOrder{},
		byRequest: map[string]string{},
		byCapture: map[string]string{},
	}
}

func (p *ScriptedProvider) record(obs Observation) Observation {
	p.seq++
	obs.Seq = p.seq
	obs.At = time.Now().UTC()
	obs.Source = "scripted-fixture"
	obs.Tier = "F"
	p.observations = append(p.observations, obs)
	return obs
}

// Observations returns a copy of everything recorded, in causal order.
func (p *ScriptedProvider) Observations() []Observation {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Observation, len(p.observations))
	copy(out, p.observations)
	return out
}

// durabilityProbe: does a merchant_operations row with this request id exist
// RIGHT NOW (before the simulated network call)?
func (p *ScriptedProvider) durabilityProbe(ctx context.Context, requestID string) bool {
	if p.pool == nil || requestID == "" {
		return false
	}
	var n int
	err := p.pool.QueryRow(ctx,
		`SELECT count(*) FROM merchant_operations WHERE provider_request_id = $1`, requestID).Scan(&n)
	return err == nil && n > 0
}

// ApproveOrder marks the scripted order approved (simulating the buyer
// approval that the real Sandbox requires).
func (p *ScriptedProvider) ApproveOrder(orderRef string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if o := p.orders[orderRef]; o != nil {
		o.status = paypal.OrderStatusApproved
	}
}

// TakeStaleSnapshot freezes the provider's current view; subsequent reads are
// served from it (used to prove stale reads cannot downgrade verified state).
func (p *ScriptedProvider) TakeStaleSnapshot() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stale = map[string]providerOrder{}
	for k, v := range p.orders {
		p.stale[k] = *v
	}
	p.staleOn = true
}

// ResolveStaleSnapshot stops serving the frozen view.
func (p *ScriptedProvider) ResolveStaleSnapshot() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.staleOn = false
	p.stale = nil
}

// CreateOrder implements merchant.OrdersAPI with scripted semantics: exact
// two-decimal amounts are accepted; malformed amounts are refused with the
// same 422 family the real sandbox uses.
func (p *ScriptedProvider) CreateOrder(ctx context.Context, in paypal.CreateOrderInput, requestID string) (*paypal.Order, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	obs := Observation{Method: "create", RequestID: requestID, SentAmount: in.AmountValue, SentCurrency: in.CurrencyCode}
	obs.DurableBeforeCall = p.durabilityProbe(ctx, requestID)

	if ref, ok := p.byRequest[requestID]; ok {
		obs.OrderRef = ref
		obs.Replayed = true
		obs.OrderStatus = p.orders[ref].status
		p.record(obs)
		return p.snapshot(ref), nil
	}

	// Reject malformed amounts exactly like the provider does (the F1 family).
	if !isExactAmount(in.AmountValue) {
		obs.DefiniteRefusal = true
		p.record(obs)
		return nil, &paypal.APIError{
			StatusCode: 422,
			Name:       "UNPROCESSABLE_ENTITY",
			Message:    "the amount value is not a valid two-decimal amount",
			Issues:     []string{"DECIMAL_PRECISION"},
		}
	}

	p.createSeq++
	ref := fmt.Sprintf("ORD-SCRIPT-%04d", p.createSeq)
	p.orders[ref] = &providerOrder{
		id: ref, status: paypal.OrderStatusCreated,
		amountValue: in.AmountValue, currency: in.CurrencyCode,
	}
	p.byRequest[requestID] = ref
	obs.OrderRef = ref
	obs.OrderStatus = paypal.OrderStatusCreated

	switch p.schedule.Create {
	case FaultDropResponseAfterMutation:
		obs.Applied = true
		obs.ResponseDropped = true
		p.record(obs)
		return nil, &paypal.ResponseError{}
	case FaultDefiniteRefusal:
		obs.DefiniteRefusal = true
		p.record(obs)
		return nil, &paypal.APIError{StatusCode: 422, Name: "UNPROCESSABLE_ENTITY"}
	}
	obs.Applied = true
	p.record(obs)
	return p.snapshot(ref), nil
}

// GetOrder serves the current (or frozen, when a stale snapshot is active)
// view of the order. While HideCaptureUntilResolved is set, an already-applied
// capture is withheld from the served view (provider-side read lag).
func (p *ScriptedProvider) GetOrder(ctx context.Context, orderID string) (*paypal.Order, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	obs := Observation{Method: "read", OrderRef: orderID}

	if p.staleOn {
		if snap, ok := p.stale[orderID]; ok {
			obs.Stale = true
			obs.OrderStatus = snap.status
			obs.CaptureStatus = snap.captureState
			obs.CaptureID = snap.captureID
			obs.CaptureAmount = snap.captureAmount
			obs.CaptureCurrency = snap.captureCurrency
			p.record(obs)
			st := snap
			return snapshotOf(&st), nil
		}
	}

	po, ok := p.orders[orderID]
	if !ok {
		p.record(obs)
		return nil, &paypal.APIError{StatusCode: 404, Name: "RESOURCE_NOT_FOUND"}
	}
	served := p.viewLocked(po)
	obs.OrderStatus = served.status
	obs.CaptureStatus = served.captureState
	obs.CaptureID = served.captureID
	// A GET response exposes the capture resource's amount; record it so
	// read-only reconciliation can corroborate financial evidence without
	// inventing a request header the provider never echoes.
	obs.CaptureAmount = served.captureAmount
	obs.CaptureCurrency = served.captureCurrency
	p.record(obs)
	return snapshotOf(served), nil
}

// viewLocked returns the order view reads are allowed to see right now.
func (p *ScriptedProvider) viewLocked(po *providerOrder) *providerOrder {
	if p.HideCaptureUntilResolved && po.captureState != "" {
		hide := *po
		hide.captureID = ""
		hide.captureState = ""
		hide.captureAmount = ""
		hide.captureCurrency = ""
		hide.status = paypal.OrderStatusApproved
		return &hide
	}
	return po
}

// CaptureOrder implements idempotent capture: a repeated request id returns
// the ORIGINAL capture result; the drop-response fault applies the mutation
// and then withholds the response (ambiguous).
func (p *ScriptedProvider) CaptureOrder(ctx context.Context, orderID, requestID string) (*paypal.Order, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	obs := Observation{Method: "capture", OrderRef: orderID, RequestID: requestID}
	obs.DurableBeforeCall = p.durabilityProbe(ctx, requestID)

	po, ok := p.orders[orderID]
	if !ok {
		obs.DefiniteRefusal = true
		p.record(obs)
		return nil, &paypal.APIError{StatusCode: 404, Name: "RESOURCE_NOT_FOUND"}
	}
	obs.SentAmount = po.amountValue
	obs.SentCurrency = po.currency

	if p.schedule.Capture == FaultAmbiguousNoEffect {
		obs.ResponseDropped = false
		p.record(obs)
		return nil, &paypal.APIError{StatusCode: 503, Name: "SERVICE_UNAVAILABLE"}
	}

	// Idempotency: an already-seen capture request id returns the ORIGINAL
	// result without applying a second effect.
	if capID, ok := p.byCapture[requestID]; ok {
		obs.Replayed = true
		obs.Applied = false
		obs.CaptureID = capID
		obs.CaptureStatus = po.captureState
		obs.CaptureAmount = po.amountValue
		obs.CaptureCurrency = po.currency
		obs.OrderStatus = po.status
		p.record(obs)
		return p.snapshot(orderID), nil
	}

	if po.status != paypal.OrderStatusCompleted || po.captureState == "" {
		// Apply the capture effect.
		p.captureSeq++
		capID := fmt.Sprintf("CAP-SCRIPT-%04d", p.captureSeq)
		p.byCapture[requestID] = capID
		po.captureID = capID
		po.captureState = paypal.CaptureStatusCompleted
		if p.CaptureStatusOverride != "" {
			po.captureState = p.CaptureStatusOverride
		}
		po.captureAmount = po.amountValue
		po.captureCurrency = po.currency
		if p.CaptureAmountOverride != "" {
			po.captureAmount = p.CaptureAmountOverride
		}
		if p.CaptureCurrencyOverride != "" {
			po.captureCurrency = p.CaptureCurrencyOverride
		}
		po.status = paypal.OrderStatusCompleted
		obs.Applied = true
		obs.CaptureID = capID
		obs.CaptureStatus = po.captureState
		obs.CaptureAmount = po.captureAmount
		obs.CaptureCurrency = po.captureCurrency
		obs.OrderStatus = po.status
	} else {
		// Order already captured by another request id: the real API family
		// refuses a second capture. Recorded as a definite refusal (no new
		// effect), which is exactly what the oracle's I5/I7 checks need.
		obs.DefiniteRefusal = true
		obs.OrderStatus = po.status
		obs.CaptureID = po.captureID
		obs.CaptureStatus = po.captureState
		p.record(obs)
		return nil, &paypal.APIError{
			StatusCode: 422,
			Name:       "ORDER_ALREADY_CAPTURED",
			Message:    "the order has already been captured",
		}
	}

	switch p.schedule.Capture {
	case FaultAmbiguousError:
		obs.ResponseDropped = false
		p.record(obs)
		return nil, &paypal.APIError{StatusCode: 503, Name: "SERVICE_UNAVAILABLE"}
	case FaultDropResponseAfterMutation:
		obs.ResponseDropped = true
		p.record(obs)
		return nil, &paypal.ResponseError{}
	}
	p.record(obs)
	return p.snapshot(orderID), nil
}

func (p *ScriptedProvider) snapshot(ref string) *paypal.Order {
	po := p.orders[ref]
	if po == nil {
		return nil
	}
	return snapshotOf(po)
}

func snapshotOf(po *providerOrder) *paypal.Order {
	o := &paypal.Order{
		ID:     po.id,
		Status: po.status,
		PurchaseUnits: []paypal.PurchaseUnit{{
			Amount: paypal.Money{CurrencyCode: po.currency, Value: po.amountValue},
		}},
	}
	if po.captureState != "" {
		o.PurchaseUnits[0].Payments = paypal.PaymentCollection{Captures: []paypal.Capture{{
			ID:     po.captureID,
			Status: po.captureState,
			Amount: paypal.Money{CurrencyCode: po.captureCurrency, Value: po.captureAmount},
		}}}
	}
	return o
}

// isExactAmount accepts only plain two-decimal amounts ("12.34").
func isExactAmount(s string) bool {
	if s == "" {
		return false
	}
	dot := -1
	for i, c := range s {
		if c == '.' {
			if dot >= 0 {
				return false
			}
			dot = i
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	if dot <= 0 || len(s)-dot-1 != 2 {
		return false
	}
	return true
}
