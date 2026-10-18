package merchant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// maxCaptureAttempts bounds the retry cycle for one unresolved capture
// operation. The request id is never replaced; after the limit the order
// escalates to manual reconciliation instead of looping.
const maxCaptureAttempts = 3

// defaultSameIDRetryWindow bounds how long after the first attempt a same-id
// retry is still considered safe. Same-id retry relies on the provider
// returning the ORIGINAL result for a repeated request id; that guarantee is
// documented as time-bounded (create-order request ids are retained for 6
// hours by default, up to 72 hours by arrangement — capture follows the same
// request-id discipline). Past this window a retry could be treated as a NEW
// capture, so the operation becomes manual reconciliation instead.
const defaultSameIDRetryWindow = 6 * time.Hour

// OrdersAPI is the provider surface the fixture depends on. The concrete
// implementation is the sandbox-only paypal.Client; tests substitute a
// TEST-ONLY fake. Nothing in this package accepts a non-sandbox provider.
type OrdersAPI interface {
	CreateOrder(ctx context.Context, in paypal.CreateOrderInput, requestID string) (*paypal.Order, error)
	GetOrder(ctx context.Context, orderID string) (*paypal.Order, error)
	CaptureOrder(ctx context.Context, orderID, requestID string) (*paypal.Order, error)
}

// Service orchestrates the fixture checkout against the provider while the
// local PostgreSQL journal remains the durable source of operation identity.
type Service struct {
	store             *Store
	api               OrdersAPI
	fault             string
	incidentDir       string
	sameIDRetryWindow time.Duration
}

// NewService wires the fixture. fault selects the seeded defect (FaultNone or
// FaultF1); incidentDir, when non-empty, receives redacted incident artifacts
// for definite failures.
func NewService(store *Store, api OrdersAPI, fault, incidentDir string) *Service {
	if fault == "" {
		fault = FaultNone
	}
	return &Service{store: store, api: api, fault: fault, incidentDir: incidentDir, sameIDRetryWindow: defaultSameIDRetryWindow}
}

// SetSameIDRetryWindow overrides the same-id retry window (operator/test
// knob; the default follows documented provider request-id retention).
func (s *Service) SetSameIDRetryWindow(d time.Duration) {
	if d > 0 {
		s.sameIDRetryWindow = d
	}
}

// Fault reports the configured fault mode (shown in the UI banner).
func (s *Service) Fault() string { return s.fault }

// evidenceTier labels evidence produced while a fault is active: the provider
// response is real, but the scenario is fault-injected (tier F), never
// presented as a clean real-world run.
func (s *Service) evidenceTier() string {
	if s.fault != FaultNone {
		return TierFault
	}
	return TierReal
}

// amountValue renders the provider amount string.
//
// FaultF1 is DELIBERATE: it reproduces a realistic float-formatting defect
// where presentation formatting leaks into the API payload. "%f" prints six
// decimal places ("12.340000"), so the request that reaches PayPal is
// malformed and is really rejected by the sandbox. The fault is active only
// when MERCHANT_FAULT=F1 is set explicitly; it exists so the V2 incident
// pipeline has a genuine, reproducible provider failure to investigate.
func (s *Service) amountValue(totalMinor int64) string {
	if s.fault == FaultF1 {
		return fmt.Sprintf("%f", float64(totalMinor)/100)
	}
	return paypal.FormatMinorUSD(totalMinor)
}

// payloadFingerprint binds the immutable intent of an operation (scheme,
// logical content, stable request id). The monetary amount is a pure
// function of the catalog product and quantity, so it is covered by those.
func payloadFingerprint(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(h[:])
}

// CreateRejectedError reports a DEFINITE create rejection (provider refused,
// or local validation refused before send). The order is FAILED_DEFINITE and
// no provider order exists.
type CreateRejectedError struct {
	Order Order
	Cause error
}

func (e *CreateRejectedError) Error() string {
	return fmt.Sprintf("checkout rejected for order %s: %v", e.Order.ID, e.Cause)
}

func (e *CreateRejectedError) Unwrap() error { return e.Cause }

// CaptureRejectedError reports a definite capture rejection with no capture
// effect; the order returns to APPROVED.
type CaptureRejectedError struct {
	Order Order
	Cause error
}

func (e *CaptureRejectedError) Error() string {
	return fmt.Sprintf("capture rejected for order %s: %v", e.Order.ID, e.Cause)
}

func (e *CaptureRejectedError) Unwrap() error { return e.Cause }

// UnknownCaptureError reports an unresolved capture. The operation and its
// request id remain durable; the caller must reconcile, never mint a new
// capture operation.
type UnknownCaptureError struct {
	OrderID           string
	ProviderRequestID string
	Cause             error
}

func (e *UnknownCaptureError) Error() string {
	return fmt.Sprintf("PAYMENT_OUTCOME_UNKNOWN for order %s (capture request id %s preserved): %v",
		e.OrderID, e.ProviderRequestID, e.Cause)
}

func (e *UnknownCaptureError) Unwrap() error { return e.Cause }

// Checkout creates the local order and its durable create operation, then
// calls the provider. The local transaction commits BEFORE the outbound call
// (I4): a crash at any point leaves either no attempt or a recoverable,
// identity-preserving attempt — never a silent one.
func (s *Service) Checkout(ctx context.Context, traceID, productID string, quantity int) (Order, error) {
	requestID, err := paypal.NewRequestID()
	if err != nil {
		return Order{}, fmt.Errorf("merchant: generate request id: %w", err)
	}

	order, op, err := s.store.CreateLocalOrder(ctx, traceID, productID, quantity, requestID)
	if err != nil {
		return Order{}, err
	}

	if s.fault != FaultNone {
		detail, _ := json.Marshal(map[string]any{
			"fault":     s.fault,
			"mechanism": "float formatting of the amount value leaks into the provider payload",
		})
		if err := s.store.AppendEvent(ctx, order.ID, "FAULT", "FAULT_INJECTED", detail, TierFault); err != nil {
			return Order{}, err
		}
	}

	slog.Info("merchant checkout: local order committed, calling provider",
		"order_id", order.ID, "trace_id", order.TraceID, "request_id", op.ProviderRequestID,
		"total_minor", order.TotalMinor, "fault", s.fault)

	sentValue := s.amountValue(order.TotalMinor)
	result, err := s.api.CreateOrder(ctx, paypal.CreateOrderInput{
		Intent:       "CAPTURE",
		CurrencyCode: order.Currency,
		AmountValue:  sentValue,
		Description:  "Waypoint V2 merchant fixture purchase",
	}, op.ProviderRequestID)
	if err != nil {
		return s.onCreateError(ctx, order, op, err)
	}
	return s.finishCreate(ctx, order, op, result)
}

// finishCreate validates the provider echo and seals a successful create.
// Shared by the first attempt and the idempotent resolution path.
func (s *Service) finishCreate(ctx context.Context, order Order, op Operation, result *paypal.Order) (Order, error) {
	// The provider accepted *something*: validate the echo before trusting it.
	if result.ID == "" || result.Status == "" {
		return s.onCreateUnknown(ctx, order, op, "provider create response carried no order id or status")
	}
	if result.Status != paypal.OrderStatusCreated && result.Status != paypal.OrderStatusApproved {
		return s.onCreateUnknown(ctx, order, op, fmt.Sprintf("provider create returned unexpected status %q", result.Status))
	}
	if len(result.PurchaseUnits) > 0 {
		pu := result.PurchaseUnits[0]
		if pu.Amount.CurrencyCode != order.Currency || pu.Amount.Value != s.amountValue(order.TotalMinor) {
			return s.onCreateUnknown(ctx, order, op,
				fmt.Sprintf("provider echoed amount %s %s, which does not match the sent %s %s",
					pu.Amount.CurrencyCode, pu.Amount.Value, order.Currency, s.amountValue(order.TotalMinor)))
		}
	}

	updated, err := s.store.MarkCreateSucceeded(ctx, order.ID, result.ID, result.ApprovalURL())
	if err != nil {
		return Order{}, err
	}
	if result.ApprovalURL() == "" {
		slog.Warn("provider order created without an approval URL", "order_id", order.ID)
	}
	return updated, nil
}

// ResolveCreate resolves an ambiguous create outcome: the same request id is
// re-sent (PayPal's create-order idempotency returns the original result for
// a repeated id within its documented retention window) and the response is
// classified exactly like the first attempt. A fresh request id is never
// minted for an unresolved create.
func (s *Service) ResolveCreate(ctx context.Context, orderID string) (Order, error) {
	order, err := s.localOrder(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if order.Status != StatusOrderCreated {
		return Order{}, fmt.Errorf("%w: order %s is not awaiting create resolution (status %s)", ErrConflict, orderID, order.Status)
	}
	detail, err := s.store.OrderDetail(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	var op *Operation
	for i := range detail.Operations {
		if detail.Operations[i].Type == OpCreateOrder {
			op = &detail.Operations[i]
		}
	}
	if op == nil || op.Status != OpStatusUnknown {
		return Order{}, fmt.Errorf("%w: order %s has no unresolved create operation", ErrConflict, orderID)
	}
	if err := s.store.TouchCaptureAttempt(ctx, op.ID); err != nil {
		return Order{}, err
	}

	result, err := s.api.CreateOrder(ctx, paypal.CreateOrderInput{
		Intent:       "CAPTURE",
		CurrencyCode: order.Currency,
		AmountValue:  s.amountValue(order.TotalMinor),
		Description:  "Waypoint V2 merchant fixture purchase",
	}, op.ProviderRequestID)
	if err != nil {
		return s.onCreateError(ctx, order, *op, err)
	}
	return s.finishCreate(ctx, order, *op, result)
}

// onCreateError classifies a failed create: ambiguous outcomes stay unknown
// and recoverable; definite rejections are terminal and produce an incident
// artifact.
func (s *Service) onCreateError(ctx context.Context, order Order, op Operation, cause error) (Order, error) {
	detail := failureDetail(op, cause)

	if paypal.AmbiguousOutcome(cause) {
		if err := s.store.MarkCreateUnknown(ctx, order.ID, detail); err != nil {
			return Order{}, err
		}
		slog.Warn("merchant checkout: provider create outcome unknown (operation preserved)",
			"order_id", order.ID, "request_id", op.ProviderRequestID, "error", cause)
		return Order{}, fmt.Errorf("provider create outcome is unknown for order %s (request id %s preserved for resolution): %w",
			order.ID, op.ProviderRequestID, cause)
	}

	updated, err := s.store.MarkCreateRejected(ctx, order.ID, detail, s.evidenceTier())
	if err != nil {
		return Order{}, err
	}
	slog.Warn("merchant checkout: provider create rejected definitively",
		"order_id", order.ID, "trace_id", order.TraceID, "fault", s.fault, "error", cause)
	s.writeIncidentFile(ctx, order.ID)
	return updated, &CreateRejectedError{Order: updated, Cause: cause}
}

func (s *Service) onCreateUnknown(ctx context.Context, order Order, op Operation, reason string) (Order, error) {
	detail := failureDetail(op, errors.New(reason))
	if err := s.store.MarkCreateUnknown(ctx, order.ID, detail); err != nil {
		return Order{}, err
	}
	return Order{}, fmt.Errorf("provider create outcome is unknown for order %s: %s", order.ID, reason)
}

// Observe folds a live provider read into local state (operator refresh).
func (s *Service) Observe(ctx context.Context, orderID string) (Order, error) {
	order, err := s.localOrder(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if order.ProviderOrderID == "" {
		return Order{}, fmt.Errorf("%w: order %s has no provider order yet", ErrConflict, orderID)
	}
	res, err := s.api.GetOrder(ctx, order.ProviderOrderID)
	if err != nil {
		return Order{}, fmt.Errorf("provider status check failed (local state unchanged): %w", err)
	}
	if res.ID != order.ProviderOrderID {
		return Order{}, fmt.Errorf("provider identity mismatch: %s vs %s", res.ID, order.ProviderOrderID)
	}
	payerID := ""
	if res.Payer != nil {
		payerID = res.Payer.PayerID
	}
	return s.store.ObserveOrderStatus(ctx, orderID, res.Status, payerID)
}

// Capture performs the server-side capture for an order whose buyer approval
// has been observed. Ordering contract:
//
//  1. preflight provider read — identity, APPROVED status, exact amount
//     (nothing is sent if any check fails; a preflight failure leaves the
//     order untouched);
//  2. complete provider capture evidence already present → certify without
//     sending anything (recovery, never re-capture);
//  3. durably begin the capture operation — request id committed BEFORE the
//     outbound call; a retry after an unknown outcome reuses the original
//     request id and the same operation row;
//  4. send the capture with that stable request id;
//  5. classify the result: verified evidence → CAPTURED_VERIFIED; ambiguous
//     → CAPTURE_OUTCOME_UNKNOWN (never a fake failure); definite rejection →
//     back to APPROVED with the attempt recorded.
func (s *Service) Capture(ctx context.Context, orderID string) (Order, error) {
	order, err := s.localOrder(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	switch order.Status {
	case StatusCapturedVerified:
		return order, nil // idempotent: already verified, nothing to do
	case StatusFailedDefinite:
		return Order{}, fmt.Errorf("%w: order %s is FAILED_DEFINITE", ErrConflict, orderID)
	}
	if order.ProviderOrderID == "" {
		return Order{}, fmt.Errorf("%w: order %s has no provider order to capture", ErrConflict, orderID)
	}

	res, err := s.api.GetOrder(ctx, order.ProviderOrderID)
	if err != nil {
		return Order{}, fmt.Errorf("pre-capture status check failed, nothing sent (local state unchanged): %w", err)
	}
	if res.ID != order.ProviderOrderID {
		return Order{}, fmt.Errorf("provider identity mismatch: %s vs %s", res.ID, order.ProviderOrderID)
	}

	// Recovery: the provider already proves a matching completed capture.
	if captureID, ok := completeCapture(res, order); ok {
		return s.sealRecoveredCapture(ctx, orderID, captureID)
	}

	switch res.Status {
	case paypal.OrderStatusCreated:
		return Order{}, fmt.Errorf("%w: buyer approval has not been observed yet (provider status CREATED)", ErrConflict)
	case paypal.OrderStatusCompleted:
		// COMPLETED without matching capture evidence must never be
		// "fixed" by sending another capture.
		detail, _ := json.Marshal(map[string]any{"provider_status": res.Status, "note": "COMPLETED without matching capture evidence"})
		if err := s.store.MarkReconciliationRequired(ctx, orderID, detail); err != nil {
			return Order{}, err
		}
		return Order{}, fmt.Errorf("%w: provider reports COMPLETED without matching capture evidence; reconciliation required", ErrConflict)
	case paypal.OrderStatusApproved:
		// fall through to the amount check below
	default:
		return Order{}, fmt.Errorf("%w: provider status %q does not permit capture", ErrConflict, res.Status)
	}

	if len(res.PurchaseUnits) == 0 {
		return Order{}, fmt.Errorf("%w: provider order carries no purchase unit to verify", ErrConflict)
	}
	pu := res.PurchaseUnits[0]
	if pu.Amount.CurrencyCode != order.Currency || pu.Amount.Value != paypal.FormatMinorUSD(order.TotalMinor) {
		return Order{}, fmt.Errorf("%w: provider amount %s %s does not match the authorized %s %s",
			ErrConflict, pu.Amount.CurrencyCode, pu.Amount.Value, order.Currency, paypal.FormatMinorUSD(order.TotalMinor))
	}

	// Step 3: durable operation begin (or bounded retry of the existing one).
	op, err := s.beginCaptureAttempt(ctx, order)
	if err != nil {
		return Order{}, err
	}

	result, err := s.api.CaptureOrder(ctx, order.ProviderOrderID, op.ProviderRequestID)
	if err != nil {
		detail := failureDetail(op, err)
		if paypal.AmbiguousOutcome(err) {
			if _, mErr := s.store.MarkCaptureUnknown(ctx, orderID, op.ID, detail, TierReal); mErr != nil {
				return Order{}, mErr
			}
			return Order{}, &UnknownCaptureError{OrderID: orderID, ProviderRequestID: op.ProviderRequestID, Cause: err}
		}
		updated, mErr := s.store.MarkCaptureRejected(ctx, orderID, op.ID, detail, s.evidenceTier())
		if mErr != nil {
			return Order{}, mErr
		}
		return updated, &CaptureRejectedError{Order: updated, Cause: err}
	}

	captureID, verr := captureEvidence(result, order)
	if verr != nil {
		detail, _ := json.Marshal(map[string]any{
			"operation_type":      op.Type,
			"provider_request_id": op.ProviderRequestID,
			"verification_error":  verr.Error(),
		})
		if _, mErr := s.store.MarkCaptureUnknown(ctx, orderID, op.ID, detail, TierReal); mErr != nil {
			return Order{}, mErr
		}
		return Order{}, &UnknownCaptureError{OrderID: orderID, ProviderRequestID: op.ProviderRequestID, Cause: verr}
	}

	detail, _ := json.Marshal(map[string]any{
		"capture_id":          captureID,
		"provider_status":     result.Status,
		"amount_minor":        order.TotalMinor,
		"currency":            order.Currency,
		"provider_request_id": op.ProviderRequestID,
	})
	updated, err := s.store.MarkCaptureVerified(ctx, orderID, op.ID, captureID, detail)
	if err != nil {
		return Order{}, err
	}
	slog.Info("merchant capture verified", "order_id", orderID, "capture_id", captureID, "trace_id", updated.TraceID)
	return updated, nil
}

// beginCaptureAttempt selects or creates the durable capture operation. A
// new attempt gets a fresh operation; a retry of an unresolved attempt reuses
// the ORIGINAL operation row and request id (never a fresh identity), bounded
// by both the attempt limit and the same-id retry window.
func (s *Service) beginCaptureAttempt(ctx context.Context, order Order) (Operation, error) {
	active, err := s.store.ActiveCapture(ctx, order.ID)
	if err != nil {
		return Operation{}, err
	}
	if active != nil {
		if active.Status == OpStatusInFlight {
			return Operation{}, fmt.Errorf("%w: a capture attempt is already in flight for order %s (reconcile before retrying)", ErrConflict, order.ID)
		}
		if active.AttemptCount >= maxCaptureAttempts {
			detail, _ := json.Marshal(map[string]any{"attempts": active.AttemptCount, "provider_request_id": active.ProviderRequestID})
			if err := s.store.MarkReconciliationRequired(ctx, order.ID, detail); err != nil {
				return Operation{}, err
			}
			return Operation{}, fmt.Errorf("%w: attempt limit reached for order %s; reconcile before retrying", ErrConflict, order.ID)
		}
		if time.Since(active.FirstAttemptedAt) > s.sameIDRetryWindow {
			detail, _ := json.Marshal(map[string]any{
				"first_attempted_at":  active.FirstAttemptedAt.UTC().Format(time.RFC3339),
				"window":              s.sameIDRetryWindow.String(),
				"provider_request_id": active.ProviderRequestID,
				"note":                "same-id retry window exceeded; a repeated request id may no longer be guaranteed to return the original result",
			})
			if err := s.store.MarkReconciliationRequired(ctx, order.ID, detail); err != nil {
				return Operation{}, err
			}
			return Operation{}, fmt.Errorf("%w: same-id retry window (%s) exceeded for order %s; manual reconciliation required (request id %s preserved)",
				ErrConflict, s.sameIDRetryWindow, order.ID, active.ProviderRequestID)
		}
		if err := s.store.BeginCaptureRetry(ctx, order.ID, active.ID); err != nil {
			return Operation{}, err
		}
		return *active, nil
	}

	requestID, err := paypal.NewRequestID()
	if err != nil {
		return Operation{}, err
	}
	// The fingerprint covers the exact canonical financial operation:
	// provider order identity, amount, currency, and the stable request id.
	fp := payloadFingerprint("waypoint.merchant.capture.v1", order.ID, order.ProviderOrderID,
		strconv.FormatInt(order.TotalMinor, 10), order.Currency, requestID)
	return s.store.InsertCaptureOp(ctx, order.ID, requestID, fp)
}

// Reconcile folds read-only provider state into local state for an unresolved
// capture: an ambiguous outcome, or an attempt left IN_FLIGHT by a crash
// (after a restart nothing can genuinely be in flight, so a read-only check
// is safe). It NEVER sends a capture — the only mutation-capable path is a
// bounded same-id retry via Capture.
//
// Safety rule (the reason this method is conservative): a provider order
// observed merely as APPROVED after an ambiguous outbound mutation is NOT
// evidence that the mutation had no financial effect — the earlier capture
// could still be processing provider-side, or the read could lag the write.
// APPROVED therefore never resolves the operation; it stays UNKNOWN. The only
// resolution to CAPTURED_VERIFIED is provider-confirmed matching capture
// evidence. Everything else remains unresolved (same request id preserved)
// for bounded same-id retry, then manual reconciliation.
func (s *Service) Reconcile(ctx context.Context, orderID string) (Order, error) {
	order, err := s.localOrder(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if order.ProviderOrderID == "" {
		return Order{}, fmt.Errorf("%w: order %s has no provider order", ErrConflict, orderID)
	}
	active, err := s.store.ActiveCapture(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if active == nil {
		return Order{}, fmt.Errorf("%w: order %s has no unresolved capture to reconcile", ErrConflict, orderID)
	}

	res, err := s.api.GetOrder(ctx, order.ProviderOrderID)
	if err != nil {
		// Provider unreachable: the outcome stays unresolved (never failed).
		return Order{}, fmt.Errorf("reconciliation read failed; outcome remains unresolved: %w", err)
	}
	if res.ID != order.ProviderOrderID {
		return Order{}, fmt.Errorf("provider identity mismatch: %s vs %s", res.ID, order.ProviderOrderID)
	}

	if captureID, ok := completeCapture(res, order); ok {
		detail, _ := json.Marshal(map[string]any{
			"capture_id":          captureID,
			"provider_status":     res.Status,
			"resolved_by":         "reconciliation",
			"provider_request_id": active.ProviderRequestID,
		})
		return s.store.MarkCaptureVerifiedRecovered(ctx, orderID, active.ID, captureID, detail)
	}

	// No matching COMPLETED capture. Record exactly what was observed — and
	// claim nothing about the mutation's effect.
	note := "provider shows no matching COMPLETED capture; outcome remains unresolved (a status read cannot prove the outbound capture had no effect)"
	if active.Status == OpStatusInFlight {
		note = "attempt was IN_FLIGHT (process restart); read-only check found no matching COMPLETED capture; demoted to unresolved with the request id preserved"
	}
	detail, _ := json.Marshal(map[string]any{
		"provider_status":     res.Status,
		"capture_statuses":    captureStatuses(res),
		"note":                note,
		"provider_request_id": active.ProviderRequestID,
	})
	if active.Status == OpStatusInFlight {
		if _, err := s.store.MarkCaptureUnknown(ctx, orderID, active.ID, detail, TierReal); err != nil {
			return Order{}, err
		}
	} else {
		if err := s.store.AppendEvent(ctx, orderID, "PAYPAL", "RECONCILIATION_INCONCLUSIVE", detail, TierReal); err != nil {
			return Order{}, err
		}
		if err := s.store.TouchLastChecked(ctx, active.ID); err != nil {
			return Order{}, err
		}
	}
	updated, err := s.localOrder(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	return updated, fmt.Errorf("%w: capture outcome for order %s remains unresolved (provider status %s; request id %s preserved for bounded same-id retry, then manual reconciliation)",
		ErrConflict, orderID, res.Status, active.ProviderRequestID)
}

// captureStatuses lists the capture resource statuses present on a provider
// order, for the reconciliation record (never raw payloads).
func captureStatuses(res *paypal.Order) []string {
	var out []string
	for _, c := range res.Captures() {
		if c.Status != "" {
			out = append(out, c.Status)
		}
	}
	return out
}

// sealRecoveredCapture certifies a capture proven by provider state, with or
// without a local unresolved operation.
func (s *Service) sealRecoveredCapture(ctx context.Context, orderID, captureID string) (Order, error) {
	opID := ""
	if active, err := s.store.ActiveCapture(ctx, orderID); err == nil && active != nil {
		opID = active.ID
	}
	detail, _ := json.Marshal(map[string]any{
		"capture_id":  captureID,
		"resolved_by": "provider evidence",
	})
	return s.store.MarkCaptureVerifiedRecovered(ctx, orderID, opID, captureID, detail)
}

func (s *Service) localOrder(ctx context.Context, orderID string) (Order, error) {
	detail, err := s.store.OrderDetail(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	return detail.Order, nil
}

// IncidentFor assembles the sanitized incident artifact for an order.
func (s *Service) IncidentFor(ctx context.Context, orderID string) (Incident, error) {
	detail, err := s.store.OrderDetail(ctx, orderID)
	if err != nil {
		return Incident{}, err
	}
	return BuildIncident(detail, s.fault)
}

// writeIncidentFile persists the incident artifact when a directory is
// configured. Failures are logged, never fatal: the artifact is always
// regenerable from the database.
func (s *Service) writeIncidentFile(ctx context.Context, orderID string) {
	if s.incidentDir == "" {
		return
	}
	inc, err := s.IncidentFor(ctx, orderID)
	if err != nil {
		slog.Warn("incident artifact build failed", "order_id", orderID, "error", err)
		return
	}
	path, err := WriteIncident(s.incidentDir, inc)
	if err != nil {
		slog.Warn("incident artifact write failed", "order_id", orderID, "error", err)
		return
	}
	slog.Info("incident artifact written", "order_id", orderID, "path", path)
}

// completeCapture returns the capture id of a COMPLETED capture whose amount
// and currency exactly match the order, when the provider order proves one.
func completeCapture(res *paypal.Order, order Order) (string, bool) {
	if res.Status != paypal.OrderStatusCompleted {
		return "", false
	}
	expected := paypal.FormatMinorUSD(order.TotalMinor)
	for _, c := range res.Captures() {
		if c.Status == paypal.CaptureStatusCompleted && c.ID != "" &&
			c.Amount.CurrencyCode == order.Currency && c.Amount.Value == expected {
			return c.ID, true
		}
	}
	return "", false
}

// captureEvidence verifies a capture response before it may be trusted:
// order id, COMPLETED status, a COMPLETED capture resource with an id, and
// the exact amount and currency. Buyer approval and an accepted request are
// not completion.
func captureEvidence(res *paypal.Order, order Order) (string, error) {
	if res == nil {
		return "", errors.New("empty capture response")
	}
	if res.ID != order.ProviderOrderID {
		return "", fmt.Errorf("capture response order %s does not match persisted order %s", res.ID, order.ProviderOrderID)
	}
	if res.Status != paypal.OrderStatusCompleted {
		return "", fmt.Errorf("capture response status %q is not COMPLETED", res.Status)
	}
	captureID, ok := completeCapture(res, order)
	if !ok {
		return "", errors.New("capture response carries no COMPLETED capture with matching amount and currency")
	}
	return captureID, nil
}

// failureDetail builds the sanitized failure record stored with an event:
// operation identity plus provider diagnostic fields (status, error name,
// message, debug id, issue codes) — never headers, tokens, or raw bodies.
func failureDetail(op Operation, cause error) json.RawMessage {
	detail := map[string]any{
		"operation_type":      op.Type,
		"provider_request_id": op.ProviderRequestID,
		"payload_fingerprint": op.PayloadFingerprint,
	}
	var apiErr *paypal.APIError
	if errors.As(cause, &apiErr) {
		detail["provider_http_status"] = apiErr.StatusCode
		detail["provider_error_name"] = apiErr.Name
		detail["provider_error_message"] = apiErr.Message
		detail["provider_debug_id"] = apiErr.DebugID
		if len(apiErr.Issues) > 0 {
			detail["provider_issues"] = apiErr.Issues
		}
	}
	var respErr *paypal.ResponseError
	if errors.As(cause, &respErr) {
		detail["response_unreadable"] = true
	}
	var valErr *paypal.ValidationError
	if errors.As(cause, &valErr) {
		detail["local_validation_refusal"] = true
	}
	detail["error_classified"] = classifyCause(cause)
	b, _ := json.Marshal(detail)
	return b
}

func classifyCause(cause error) string {
	if paypal.AmbiguousOutcome(cause) {
		return "AMBIGUOUS"
	}
	return "DEFINITE"
}
