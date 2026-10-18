// Package merchant is the Waypoint V2 M1 merchant checkout fixture: a small,
// deliberately fault-injectable storefront that talks to the real PayPal
// Sandbox. It is the *target* application that Waypoint V2 will later
// investigate and repair; for M1 it must be a working checkout with one
// reproducible provider-request failure (F1).
//
// Financial-safety rules this package upholds (normative source:
// docs/waypoint-v2/FINANCIAL_CORRECTNESS_AND_SECURITY.md):
//
//   - money is integer minor units; totals are computed server-side from the
//     fixture catalog and never accepted from the browser;
//   - the operation row (stable provider request id + payload fingerprint +
//     IN_FLIGHT status) is committed BEFORE the outbound provider call — the
//     V1 spike's crash window must not be repeated here;
//   - a timeout, 5xx, or unreadable response is an UNKNOWN outcome, never
//     proof of failure, and a retry of an unknown operation reuses the
//     original request id instead of minting a new operation;
//   - a payment is CAPTURED_VERIFIED only with provider evidence: matching
//     order id, a COMPLETED capture id, and the exact amount and currency;
//   - evidence is never downgraded once verified, and the append-only event
//     journal records what actually happened.
package merchant

import (
	"encoding/json"
	"time"
)

// Order lifecycle statuses (mirror the 0004 CHECK constraint exactly).
const (
	StatusOrderCreated           = "ORDER_CREATED"
	StatusBuyerApprovalPending   = "BUYER_APPROVAL_PENDING"
	StatusApproved               = "APPROVED"
	StatusCaptureInFlight        = "CAPTURE_IN_FLIGHT"
	StatusCaptureOutcomeUnknown  = "CAPTURE_OUTCOME_UNKNOWN"
	StatusReconciliationRequired = "RECONCILIATION_REQUIRED"
	StatusCapturedVerified       = "CAPTURED_VERIFIED"
	StatusFailedDefinite         = "FAILED_DEFINITE"
	StatusCancelled              = "CANCELLED"
)

// Provider operation kinds and statuses (mirror the 0004 CHECK constraints).
const (
	OpCreateOrder = "CREATE_ORDER"
	OpCapture     = "CAPTURE"

	OpStatusInFlight  = "IN_FLIGHT"
	OpStatusSucceeded = "SUCCEEDED"
	OpStatusRejected  = "REJECTED"
	OpStatusUnknown   = "UNKNOWN"
)

// Evidence tiers (PRD §5): R = real provider observation, F = local fault
// injection, S = sample. Evidence is never presented above its tier.
const (
	TierReal   = "R"
	TierFault  = "F"
	TierSample = "S"
)

// Fault modes for the fixture. F1 is the seeded M1 defect: a float-formatting
// bug leaks six decimal places into the provider amount value, producing a
// malformed provider request that the real API rejects.
const (
	FaultNone = "none"
	FaultF1   = "F1"
)

// Product is one fixture catalog item. Price is server-owned trust: the
// checkout only ever references the product id.
type Product struct {
	ID          string `json:"product_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PriceMinor  int64  `json:"price_minor"`
	Currency    string `json:"currency"`
}

// Order is one merchant order as persisted locally.
type Order struct {
	ID              string    `json:"order_id"`
	TraceID         string    `json:"trace_id"`
	ProductID       string    `json:"product_id"`
	Quantity        int       `json:"quantity"`
	TotalMinor      int64     `json:"total_minor"`
	Currency        string    `json:"currency"`
	Status          string    `json:"status"`
	ProviderOrderID string    `json:"provider_order_id,omitempty"`
	ApprovalURL     string    `json:"approval_url,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Operation is one durable provider operation (create or capture) for an
// order. ProviderRequestID and PayloadFingerprint are persisted before the
// outbound call and never rewritten.
type Operation struct {
	ID                 string     `json:"operation_id"`
	OrderID            string     `json:"order_id"`
	Type               string     `json:"operation_type"`
	ProviderRequestID  string     `json:"provider_request_id"`
	PayloadFingerprint string     `json:"payload_fingerprint"`
	Status             string     `json:"status"`
	ProviderOrderID    string     `json:"provider_order_id,omitempty"`
	ProviderCaptureID  string     `json:"provider_capture_id,omitempty"`
	AttemptCount       int        `json:"attempt_count"`
	OwnerID            string     `json:"owner_id,omitempty"`
	LeaseExpiresAt     *time.Time `json:"lease_expires_at,omitempty"`
	FirstAttemptedAt   time.Time  `json:"first_attempted_at"`
	LastCheckedAt      *time.Time `json:"last_checked_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// Event is one append-only journal entry with an explicit evidence tier.
type Event struct {
	ID        int64           `json:"event_id"`
	OrderID   string          `json:"order_id"`
	TraceID   string          `json:"trace_id"`
	Source    string          `json:"source"`
	EventType string          `json:"event_type"`
	Detail    json.RawMessage `json:"detail"`
	Tier      string          `json:"tier"`
	CreatedAt time.Time       `json:"created_at"`
}

// OrderDetail is the full local view of an order: state, operations, and the
// event journal.
type OrderDetail struct {
	Order      Order       `json:"order"`
	Operations []Operation `json:"operations"`
	Events     []Event     `json:"events"`
}
