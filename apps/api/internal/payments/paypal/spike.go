package paypal

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The fixed synthetic purchase of the Phase 04 feasibility spike. These are
// constants on purpose: the spike accepts no operator- or browser-supplied
// amounts, and it deliberately does not touch the Waypoint domain state
// machine or database — an isolated sandbox order is not a domain-authorized
// Waypoint transaction.
const (
	SpikeDescription = "Waypoint sandbox feasibility test"
	SpikeCurrency    = "USD"
	SpikeAmountMinor = int64(100) // $1.00
)

// FormatMinorUSD renders integer minor units as the provider's decimal string.
func FormatMinorUSD(minor int64) string {
	sign := ""
	v := minor
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// NewRequestID returns a random UUIDv4 suitable for PayPal-Request-Id
// (idempotency) values: unique per operation, well under every documented
// length limit.
func NewRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate request id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// Record states describe the spike's local view of the provider order. They
// are unrelated to the Waypoint domain transaction states.
const (
	StateAwaitingApproval    = "AWAITING_APPROVAL"
	StateReadyToCapture      = "READY_TO_CAPTURE"
	StateCapturedVerified    = "CAPTURED_VERIFIED"
	StatePaymentUnknown      = "PAYMENT_OUTCOME_UNKNOWN"
	StateFailedDefinite      = "FAILED_DEFINITE"
	StateCompletedOnProvider = "COMPLETED_ON_PROVIDER"
	StateVoided              = "VOIDED"
)

// Record is the local, non-secret spike record: provider order identity,
// expected amount, operation request ids, and observed state. It contains no
// credentials, tokens, or provider payloads beyond identifiers.
type Record struct {
	Provider         string    `json:"provider"`
	Environment      string    `json:"environment"`
	OrderID          string    `json:"order_id"`
	Currency         string    `json:"currency"`
	AmountMinor      int64     `json:"amount_minor"`
	AmountValue      string    `json:"amount_value"`
	CreateRequestID  string    `json:"create_request_id"`
	CaptureRequestID string    `json:"capture_request_id,omitempty"`
	ProviderStatus   string    `json:"provider_status,omitempty"`
	State            string    `json:"state"`
	CaptureID        string    `json:"capture_id,omitempty"`
	ApprovalURL      string    `json:"approval_url,omitempty"`
	PayerID          string    `json:"payer_id,omitempty"`
	Notes            []string  `json:"notes,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// RecordStore persists spike records.
type RecordStore interface {
	Save(Record) error
	Load(orderID string) (Record, error)
	Latest() (Record, error)
}

// FileRecordStore keeps one JSON file per order under Dir. The intended Dir
// lives inside the git-ignored docs directory.
type FileRecordStore struct {
	Dir string
}

func (s FileRecordStore) path(orderID string) (string, error) {
	if err := validateOrderID(orderID); err != nil {
		return "", fmt.Errorf("record path: %w", err)
	}
	return filepath.Join(s.Dir, orderID+".json"), nil
}

// Save writes the record atomically (temp file + rename).
func (s FileRecordStore) Save(rec Record) error {
	p, err := s.path(rec.OrderID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("create record directory: %w", err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode record: %w", err)
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write record: %w", err)
	}
	return os.Rename(tmp, p)
}

// Load reads one record by order id.
func (s FileRecordStore) Load(orderID string) (Record, error) {
	p, err := s.path(orderID)
	if err != nil {
		return Record{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Record{}, fmt.Errorf("load spike record %s: %w", orderID, err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, fmt.Errorf("parse spike record %s: %w", orderID, err)
	}
	return rec, nil
}

// Latest returns the most recently updated record in Dir.
func (s FileRecordStore) Latest() (Record, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return Record{}, fmt.Errorf("read record directory: %w", err)
	}
	var latest Record
	found := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		rec, err := s.Load(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		if !found || rec.UpdatedAt.After(latest.UpdatedAt) {
			latest, found = rec, true
		}
	}
	if !found {
		return Record{}, errors.New("no spike records found")
	}
	return latest, nil
}

// OrdersAPI is the subset of the PayPal client the spike flows use.
type OrdersAPI interface {
	CreateOrder(ctx context.Context, in CreateOrderInput, requestID string) (*Order, error)
	GetOrder(ctx context.Context, orderID string) (*Order, error)
	CaptureOrder(ctx context.Context, orderID, requestID string) (*Order, error)
}

// VerifyCaptureReady enforces the pre-capture preconditions against live
// provider state and the local record. Any error here means capture is
// refused before an outbound capture request is attempted.
func VerifyCaptureReady(order *Order, rec Record) error {
	if order == nil {
		return newValidationError("nil order")
	}
	if order.ID != rec.OrderID {
		return newValidationError("order identity mismatch: provider %q vs recorded %q", order.ID, rec.OrderID)
	}
	switch order.Status {
	case OrderStatusApproved:
		// buyer approval observed
	case OrderStatusCreated:
		return newValidationError("buyer approval has not been observed yet (provider status CREATED)")
	case OrderStatusCompleted:
		return newValidationError("order is already COMPLETED; refusing to capture again")
	default:
		return newValidationError("provider status %q does not permit capture", order.Status)
	}
	if len(order.PurchaseUnits) == 0 {
		return newValidationError("provider order carries no purchase units to verify")
	}
	pu := order.PurchaseUnits[0]
	if pu.Amount.CurrencyCode != rec.Currency || pu.Amount.Value != rec.AmountValue {
		return newValidationError("provider amount %s %s does not match recorded %s %s",
			pu.Amount.CurrencyCode, pu.Amount.Value, rec.Currency, rec.AmountValue)
	}
	return nil
}

// CaptureEvidence is the verified, provider-confirmed capture outcome.
type CaptureEvidence struct {
	OrderID     string
	CaptureID   string
	Status      string
	Currency    string
	AmountValue string
	PayerID     string
}

// VerifyCaptureOutcome proves a capture actually completed from the provider
// response: order and capture statuses, identity, and the exact amount.
// Anything less is not success — buyer approval is not payment completion,
// and an accepted request is not a completed capture.
func VerifyCaptureOutcome(order *Order, rec Record) (CaptureEvidence, error) {
	if order == nil {
		return CaptureEvidence{}, newValidationError("nil capture response")
	}
	if order.ID != rec.OrderID {
		return CaptureEvidence{}, newValidationError("capture response order %q does not match recorded order %q", order.ID, rec.OrderID)
	}
	if order.Status != OrderStatusCompleted {
		return CaptureEvidence{}, newValidationError("capture response order status %q is not COMPLETED", order.Status)
	}
	captures := order.Captures()
	var chosen *Capture
	for i := range captures {
		if captures[i].Status == CaptureStatusCompleted {
			chosen = &captures[i]
			break
		}
	}
	if chosen == nil {
		var statuses []string
		for _, c := range captures {
			if c.Status != "" {
				statuses = append(statuses, c.Status)
			}
		}
		if len(statuses) > 0 {
			return CaptureEvidence{}, newValidationError("no COMPLETED capture in provider response (capture status %q)", strings.Join(statuses, ", "))
		}
		return CaptureEvidence{}, newValidationError("capture response carries no capture resource")
	}
	if chosen.ID == "" {
		return CaptureEvidence{}, newValidationError("COMPLETED capture carried no capture id")
	}
	if chosen.Amount.CurrencyCode != rec.Currency || chosen.Amount.Value != rec.AmountValue {
		return CaptureEvidence{}, newValidationError("captured amount %s %s does not match expected %s %s",
			chosen.Amount.CurrencyCode, chosen.Amount.Value, rec.Currency, rec.AmountValue)
	}
	ev := CaptureEvidence{
		OrderID:     order.ID,
		CaptureID:   chosen.ID,
		Status:      chosen.Status,
		Currency:    chosen.Amount.CurrencyCode,
		AmountValue: chosen.Amount.Value,
	}
	if order.Payer != nil {
		ev.PayerID = order.Payer.PayerID
	}
	return ev, nil
}

// UnknownOutcomeError marks a capture whose definitive outcome could not be
// established. The record (with the original capture request id preserved)
// rides along for investigation; a retry reuses that same request id.
type UnknownOutcomeError struct {
	Err    error
	Record Record
}

func (e *UnknownOutcomeError) Error() string {
	return fmt.Sprintf("PAYMENT_OUTCOME_UNKNOWN for order %s: %v (evidence preserved in the local record)", e.Record.OrderID, e.Err)
}

// Unwrap exposes the underlying cause.
func (e *UnknownOutcomeError) Unwrap() error { return e.Err }

// CreateSpikeOrder creates the fixed $1.00 sandbox order and persists the
// local record.
func CreateSpikeOrder(ctx context.Context, api OrdersAPI, store RecordStore, requestID string) (Record, error) {
	order, err := api.CreateOrder(ctx, CreateOrderInput{
		Intent:       "CAPTURE",
		CurrencyCode: SpikeCurrency,
		AmountValue:  FormatMinorUSD(SpikeAmountMinor),
		Description:  SpikeDescription,
	}, requestID)
	if err != nil {
		return Record{}, err
	}
	if order.Status != OrderStatusCreated {
		return Record{}, newValidationError("create returned status %q; expected CREATED", order.Status)
	}
	if len(order.PurchaseUnits) > 0 {
		pu := order.PurchaseUnits[0]
		if pu.Amount.CurrencyCode != SpikeCurrency || pu.Amount.Value != FormatMinorUSD(SpikeAmountMinor) {
			return Record{}, newValidationError("create returned amount %s %s; expected %s %s",
				pu.Amount.CurrencyCode, pu.Amount.Value, SpikeCurrency, FormatMinorUSD(SpikeAmountMinor))
		}
	}
	now := time.Now().UTC()
	rec := Record{
		Provider:        "paypal",
		Environment:     EnvironmentSandbox,
		OrderID:         order.ID,
		Currency:        SpikeCurrency,
		AmountMinor:     SpikeAmountMinor,
		AmountValue:     FormatMinorUSD(SpikeAmountMinor),
		CreateRequestID: requestID,
		ProviderStatus:  order.Status,
		State:           StateAwaitingApproval,
		ApprovalURL:     order.ApprovalURL(),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if rec.ApprovalURL == "" {
		rec.Notes = append(rec.Notes, "provider response carried no approval URL")
	}
	if err := store.Save(rec); err != nil {
		return rec, err
	}
	return rec, nil
}

// RetrieveSpikeOrder refreshes the local record from live provider state.
func RetrieveSpikeOrder(ctx context.Context, api OrdersAPI, store RecordStore, orderID string) (Record, error) {
	rec, err := store.Load(orderID)
	if err != nil {
		return Record{}, err
	}
	order, err := api.GetOrder(ctx, orderID)
	if err != nil {
		return Record{}, fmt.Errorf("retrieve order: %w", err)
	}
	if order.ID != rec.OrderID {
		return Record{}, newValidationError("order identity mismatch: provider %q vs recorded %q", order.ID, rec.OrderID)
	}
	rec.ProviderStatus = order.Status
	if order.Payer != nil && order.Payer.PayerID != "" {
		rec.PayerID = order.Payer.PayerID
	}
	switch order.Status {
	case OrderStatusCreated:
		rec.State = StateAwaitingApproval
	case OrderStatusApproved:
		rec.State = StateReadyToCapture
	case OrderStatusCompleted:
		rec.State = StateCompletedOnProvider
		for _, c := range order.Captures() {
			if c.Status == CaptureStatusCompleted {
				rec.CaptureID = c.ID
				break
			}
		}
	case OrderStatusVoided:
		rec.State = StateVoided
	}
	rec.UpdatedAt = time.Now().UTC()
	if err := store.Save(rec); err != nil {
		return rec, err
	}
	return rec, nil
}

// CaptureSpikeOrder performs the operator-driven capture:
//
//  1. load the local record;
//  2. retrieve live provider state and verify identity, approval, amount;
//  3. refuse if approval cannot be verified (nothing sent);
//  4. persist the capture request id BEFORE the outbound call;
//  5. capture with that stable request id;
//  6. verify the authoritative provider response before reporting success.
//
// Ambiguous outcomes (timeouts, transport failures, 5xx, unreadable success
// bodies) are reported as PAYMENT_OUTCOME_UNKNOWN and never blindly retried —
// a subsequent attempt reuses the persisted capture request id so the
// provider returns the original result instead of capturing twice.
func CaptureSpikeOrder(ctx context.Context, api OrdersAPI, store RecordStore, orderID, requestID string) (CaptureEvidence, error) {
	rec, err := store.Load(orderID)
	if err != nil {
		return CaptureEvidence{}, err
	}
	if rec.State == StateCapturedVerified {
		return CaptureEvidence{}, newValidationError("order %s was already captured and verified (%s)", orderID, rec.CaptureID)
	}

	order, err := api.GetOrder(ctx, orderID)
	if err != nil {
		return CaptureEvidence{}, fmt.Errorf("pre-capture status check failed: %w", err)
	}
	if err := VerifyCaptureReady(order, rec); err != nil {
		return CaptureEvidence{}, err
	}

	// A retry after an unknown outcome reuses the original request identity.
	if rec.State == StatePaymentUnknown && rec.CaptureRequestID != "" {
		requestID = rec.CaptureRequestID
	}
	if requestID == "" {
		return CaptureEvidence{}, newValidationError("capture request id is required")
	}

	rec.CaptureRequestID = requestID
	if order.Payer != nil && rec.PayerID == "" {
		rec.PayerID = order.Payer.PayerID
	}
	rec.UpdatedAt = time.Now().UTC()
	if err := store.Save(rec); err != nil {
		return CaptureEvidence{}, fmt.Errorf("persist capture request identity: %w", err)
	}

	result, err := api.CaptureOrder(ctx, orderID, requestID)
	if err != nil {
		now := time.Now().UTC()
		rec.UpdatedAt = now
		if AmbiguousOutcome(err) {
			rec.State = StatePaymentUnknown
			rec.Notes = append(rec.Notes, fmt.Sprintf("capture outcome unknown at %s: %v", now.Format(time.RFC3339), err))
		} else {
			rec.State = StateFailedDefinite
			rec.Notes = append(rec.Notes, fmt.Sprintf("capture rejected definitively at %s: %v", now.Format(time.RFC3339), err))
		}
		_ = store.Save(rec)
		if rec.State == StatePaymentUnknown {
			return CaptureEvidence{}, &UnknownOutcomeError{Err: err, Record: rec}
		}
		return CaptureEvidence{}, err
	}

	ev, err := VerifyCaptureOutcome(result, rec)
	if err != nil {
		now := time.Now().UTC()
		rec.State = StatePaymentUnknown
		rec.Notes = append(rec.Notes, fmt.Sprintf("capture response not confirmed at %s: %v", now.Format(time.RFC3339), err))
		rec.UpdatedAt = now
		_ = store.Save(rec)
		return CaptureEvidence{}, &UnknownOutcomeError{Err: err, Record: rec}
	}

	rec.State = StateCapturedVerified
	rec.CaptureID = ev.CaptureID
	rec.ProviderStatus = result.Status
	rec.PayerID = ev.PayerID
	rec.UpdatedAt = time.Now().UTC()
	if err := store.Save(rec); err != nil {
		return ev, fmt.Errorf("persist verified capture: %w", err)
	}
	return ev, nil
}
