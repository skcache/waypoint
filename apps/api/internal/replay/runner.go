package replay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/oracle"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// Candidate names the implementation under test.
type Candidate string

const (
	// CandidateReference is the known-good merchant reference implementation.
	CandidateReference Candidate = "reference"
	// CandidateUnsafeIdentitySwap is a KNOWINGLY UNSAFE seed patch: on an
	// ambiguous capture it treats the outcome as failure, force-closes the
	// preserved attempt and reissues the capture under a FRESH identity,
	// then marks the order verified. Violates I4/I5/I7; must be rejected.
	CandidateUnsafeIdentitySwap Candidate = "unsafe-identity-swap-seed"
	// CandidateUnsafeAcceptMismatch is a KNOWINGLY UNSAFE seed patch: it
	// marks a capture verified even though the provider echo carries the
	// WRONG amount. Violates I6; must be rejected.
	CandidateUnsafeAcceptMismatch Candidate = "unsafe-accept-mismatch-seed"
)

// Driver is the narrow surface the runner drives: the real merchant service
// or a seed candidate. It is deliberately merchant-only — the oracle is fed
// from the DURABLE journal and provider observations, not from this object.
type Driver interface {
	Checkout(ctx context.Context, traceID, productID string, quantity int) (merchant.Order, error)
	Observe(ctx context.Context, orderID string) (merchant.Order, error)
	Capture(ctx context.Context, orderID string) (merchant.Order, error)
	Reconcile(ctx context.Context, orderID string) (merchant.Order, error)
}

// Step actions (deterministic script; no sleeps, no randomness).
const (
	StepCheckout             = "checkout"
	StepApprove              = "approve"
	StepCapture              = "capture"
	StepCaptureConcurrent    = "capture_concurrent"
	StepReconcile            = "reconcile"
	StepObserve              = "observe"
	StepRestart              = "restart"
	StepStaleSnapshot        = "stale_snapshot"
	StepResolveStale         = "resolve_stale"
	StepSimulateCrashedBegin = "simulate_crashed_capture_begin"
	StepSimulateOwnedBegin   = "simulate_live_owned_capture_begin"
	StepExpireLease          = "expire_lease"
)

// Step is one scripted action.
type Step struct {
	Action    string
	ProductID string
	Quantity  int
	Count     int
}

// Scenario is one deterministic fault scenario.
type Scenario struct {
	ID               string
	Title            string
	Family           string
	Description      string
	ExpectedVerdict  oracle.Verdict
	EvidenceTier     string
	Unsafe           bool
	Schedule         FaultSchedule
	HideCaptureReads bool
	// CaptureAmountOverride models a provider capture echo whose amount does
	// not match the authorized order (the I6 mismatch family).
	CaptureAmountOverride string
	// OmitCaptureEvidence models genuinely unavailable provider proof: the
	// capture observations are dropped while building oracle evidence.
	OmitCaptureEvidence bool
	Steps               []Step
}

// FaultEvent records one injection decision taken by the harness.
type FaultEvent struct {
	Seq    int       `json:"seq"`
	Phase  string    `json:"phase"`
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
}

// RunRecord is everything one scenario execution produced, before the oracle
// reads the durable journal. It becomes Evidence via ToEvidence.
type RunRecord struct {
	ScenarioID   string            `json:"scenario_id"`
	Candidate    Candidate         `json:"candidate"`
	OrderIDs     []string          `json:"order_ids"`
	Observations []Observation     `json:"observations"`
	Faults       []FaultEvent      `json:"faults"`
	Facts        map[string]string `json:"facts"`
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   time.Time         `json:"finished_at"`
	// EvidenceOmitted, when true, drops capture observations while building
	// oracle evidence (models genuinely unavailable provider proof).
	EvidenceOmitted bool `json:"evidence_omitted"`
}

// Env wires the harness to a store, pool and driver factory.
type Env struct {
	Store     *merchant.Store
	Pool      *pgxpool.Pool
	Provider  *ScriptedProvider
	NewDriver func(cand Candidate) Driver
}

// Run executes a scenario against a candidate and returns the raw record.
func Run(ctx context.Context, env *Env, sc Scenario, cand Candidate) (RunRecord, error) {
	rec := RunRecord{
		ScenarioID:      sc.ID,
		Candidate:       cand,
		Facts:           map[string]string{},
		StartedAt:       time.Now().UTC(),
		EvidenceOmitted: sc.OmitCaptureEvidence,
	}
	faultSeq := 0
	addFault := func(phase, detail string) {
		faultSeq++
		rec.Faults = append(rec.Faults, FaultEvent{Seq: faultSeq, Phase: phase, Detail: detail, At: time.Now().UTC()})
	}
	if sc.Schedule.Capture != FaultOff {
		addFault("capture", string(sc.Schedule.Capture))
	}
	if sc.Schedule.Create != FaultOff {
		addFault("create", string(sc.Schedule.Create))
	}
	if sc.HideCaptureReads {
		env.Provider.HideCaptureUntilResolved = true
		addFault("read", "hide_capture_until_resolved")
	}

	driver := env.NewDriver(cand)
	var orderID string

	fail := func(format string, args ...any) (RunRecord, error) {
		rec.FinishedAt = time.Now().UTC()
		return rec, fmt.Errorf("scenario %s: %s", sc.ID, fmt.Sprintf(format, args...))
	}

	for si, step := range sc.Steps {
		switch step.Action {
		case StepCheckout:
			product := step.ProductID
			if product == "" {
				product = "maple-mug"
			}
			qty := step.Quantity
			if qty == 0 {
				qty = 1
			}
			traceID, err := paypal.NewRequestID()
			if err != nil {
				return fail("trace id: %v", err)
			}
			order, err := driver.Checkout(ctx, traceID, product, qty)
			if err != nil {
				var rejected *merchant.CreateRejectedError
				if errors.As(err, &rejected) {
					orderID = rejected.Order.ID
					rec.Facts["checkout_rejected"] = "true"
				} else {
					return fail("step %d checkout: %v", si, err)
				}
			} else {
				orderID = order.ID
			}
			if orderID == "" {
				return fail("step %d checkout produced no order id", si)
			}
			rec.OrderIDs = append(rec.OrderIDs, orderID)
		case StepApprove:
			ref, err := env.providerRef(ctx, orderID)
			if err != nil {
				return fail("step %d approve: %v", si, err)
			}
			env.Provider.ApproveOrder(ref)
			if _, err := driver.Observe(ctx, orderID); err != nil {
				return fail("step %d observe-approval: %v", si, err)
			}
		case StepCapture:
			_, err := driver.Capture(ctx, orderID)
			rec.Facts[factKey("capture_error", si)] = errString(err)
		case StepCaptureConcurrent:
			n := step.Count
			if n < 2 {
				n = 2
			}
			var wg sync.WaitGroup
			errs := make([]error, n)
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					_, errs[i] = driver.Capture(ctx, orderID)
				}(i)
			}
			wg.Wait()
			ok, refused := 0, 0
			msgs := make([]string, 0, n)
			for _, e := range errs {
				if e == nil {
					ok++
					continue
				}
				refused++
				msgs = append(msgs, e.Error())
			}
			rec.Facts[factKey("concurrent_ok", si)] = fmt.Sprint(ok)
			rec.Facts[factKey("concurrent_refused", si)] = fmt.Sprint(refused)
			rec.Facts[factKey("concurrent_errors", si)] = strings.Join(msgs, " | ")
		case StepReconcile:
			_, err := driver.Reconcile(ctx, orderID)
			rec.Facts[factKey("reconcile_error", si)] = errString(err)
		case StepObserve:
			_, err := driver.Observe(ctx, orderID)
			rec.Facts[factKey("observe_error", si)] = errString(err)
		case StepRestart:
			// A fresh driver instance on the same durable store models a
			// process restart (no in-memory state survives).
			driver = env.NewDriver(cand)
			rec.Facts["restarted"] = "true"
		case StepStaleSnapshot:
			env.Provider.TakeStaleSnapshot()
		case StepResolveStale:
			env.Provider.ResolveStaleSnapshot()
		case StepSimulateCrashedBegin:
			ref, err := env.providerRef(ctx, orderID)
			if err != nil {
				return fail("step %d crashed-begin: %v", si, err)
			}
			_ = ref
			// The durable begin committed (IN_FLIGHT) and the process died
			// before any outbound call: no lease is recorded.
			if _, err := env.Store.InsertCaptureOp(ctx, orderID, "req-crash-"+sc.ID, "fp-crash-"+sc.ID, "worker-crashed", 0); err != nil {
				return fail("step %d crashed-begin insert: %v", si, err)
			}
			addFault("process", "crash_after_durable_begin_before_outbound")
		case StepSimulateOwnedBegin:
			if _, err := env.Store.InsertCaptureOp(ctx, orderID, "req-owned-"+sc.ID, "fp-owned-"+sc.ID, "worker-w-other", 5*time.Minute); err != nil {
				return fail("step %d owned-begin insert: %v", si, err)
			}
			addFault("process", "second_worker_holds_live_lease")
		case StepExpireLease:
			if _, err := env.Pool.Exec(ctx, `
				UPDATE merchant_operations SET lease_expires_at = now() - interval '1 second'
				 WHERE order_id = $1::uuid AND operation_type = 'CAPTURE'`, orderID); err != nil {
				return fail("step %d expire lease: %v", si, err)
			}
		default:
			return fail("step %d: unknown action %q", si, step.Action)
		}
	}

	env.Provider.HideCaptureUntilResolved = false
	rec.Observations = env.Provider.Observations()
	rec.FinishedAt = time.Now().UTC()
	return rec, nil
}

func factKey(name string, step int) string { return fmt.Sprintf("%s_%d", name, step) }

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// providerRef resolves the scripted provider order id for a local order.
func (e *Env) providerRef(ctx context.Context, orderID string) (string, error) {
	detail, err := e.Store.OrderDetail(ctx, orderID)
	if err != nil {
		return "", err
	}
	if detail.Order.ProviderOrderID == "" {
		return "", fmt.Errorf("local order %s has no provider order id yet", orderID)
	}
	return detail.Order.ProviderOrderID, nil
}

// ToEvidence assembles the oracle input: the durable journal read by the
// ORACLE itself (own SQL) plus the harness-recorded provider observations.
func (r RunRecord) ToEvidence(ctx context.Context, pool *pgxpool.Pool, sc Scenario, cand Candidate) (oracle.Evidence, error) {
	local, err := oracle.LoadLocal(ctx, pool, r.OrderIDs)
	if err != nil {
		return oracle.Evidence{}, err
	}
	obs := make([]oracle.ProviderObs, 0, len(r.Observations))
	for _, o := range r.Observations {
		if r.EvidenceOmitted && o.Method == "capture" {
			continue // model genuinely unavailable provider proof
		}
		obs = append(obs, oracle.ProviderObs{
			Seq:               o.Seq,
			At:                o.At,
			Method:            o.Method,
			OrderRef:          o.OrderRef,
			RequestID:         o.RequestID,
			DurableBeforeCall: o.DurableBeforeCall,
			SentAmount:        o.SentAmount,
			SentCurrency:      o.SentCurrency,
			Applied:           o.Applied,
			Replayed:          o.Replayed,
			ResponseDropped:   o.ResponseDropped,
			DefiniteRefusal:   o.DefiniteRefusal,
			OrderStatus:       o.OrderStatus,
			CaptureID:         o.CaptureID,
			CaptureStatus:     o.CaptureStatus,
			CaptureAmount:     o.CaptureAmount,
			CaptureCurrency:   o.CaptureCurrency,
			Stale:             o.Stale,
			Source:            o.Source,
			Tier:              o.Tier,
		})
	}
	faults := make([]oracle.FaultEvent, 0, len(r.Faults))
	for _, f := range r.Faults {
		faults = append(faults, oracle.FaultEvent{Seq: f.Seq, Phase: f.Phase, Detail: f.Detail, At: f.At})
	}
	return oracle.Evidence{
		Scenario: oracle.ScenarioMeta{
			ID:                    sc.ID,
			Title:                 sc.Title,
			Family:                sc.Family,
			Description:           sc.Description,
			ExpectedVerdict:       sc.ExpectedVerdict,
			EvidenceTier:          sc.EvidenceTier,
			UnsafeCandidate:       sc.Unsafe,
			RequiresProviderProof: true,
		},
		Candidate:     string(cand),
		Orders:        local.Orders,
		Operations:    local.Operations,
		Events:        local.Events,
		Fulfillments:  local.Fulfillments,
		WebhookEvents: local.WebhookEvents,
		Provider:      obs,
		Faults:        faults,
		Facts:         r.Facts,
	}, nil
}
