package domain

import (
	"fmt"
	"strings"
)

// Identifier bounds, aligned with the migrations (0001–0003).
const (
	maxIdentifierLen = 128
	maxOperationKey  = 128
)

// TransactionID identifies one logical transaction. UUID.
type TransactionID string

// PrincipalID identifies one demo-session principal. UUID.
type PrincipalID string

// ContractID identifies one logical intent contract (all versions share it).
// UUID.
type ContractID string

// PaymentOperationID identifies one payment_operations row. UUID.
type PaymentOperationID string

// OperationKey is the client-generated logical idempotency key for one
// intended financial operation. Conceptually per principal.
type OperationKey string

// CatalogItemID identifies one controlled catalog fixture (a slug).
type CatalogItemID string

// ContractVersion is the monotonic version of an intent contract. Starts at 1.
type ContractVersion int

// ItemVersion is the version of a catalog item row. Starts at 1.
type ItemVersion int

// ProviderRequestID is a stable provider request identity persisted before an
// outbound call. Opaque here; only shape is validated.
type ProviderRequestID string

// OperationType mirrors the payment_operations.operation_type constraint.
type OperationType string

const (
	OperationCreateOrder OperationType = "CREATE_ORDER"
	OperationCapture     OperationType = "CAPTURE"
)

// Valid reports whether the operation type is one this release performs.
func (t OperationType) Valid() bool {
	return t == OperationCreateOrder || t == OperationCapture
}

// NewTransactionID validates and canonicalizes a transaction identifier.
func NewTransactionID(raw string) (TransactionID, error) {
	v, err := canonicalUUID(raw)
	return TransactionID(v), err
}

// NewPrincipalID validates and canonicalizes a principal identifier.
func NewPrincipalID(raw string) (PrincipalID, error) {
	v, err := canonicalUUID(raw)
	return PrincipalID(v), err
}

// NewContractID validates and canonicalizes a contract identifier.
func NewContractID(raw string) (ContractID, error) {
	v, err := canonicalUUID(raw)
	return ContractID(v), err
}

// NewPaymentOperationID validates and canonicalizes a payment operation id.
func NewPaymentOperationID(raw string) (PaymentOperationID, error) {
	v, err := canonicalUUID(raw)
	return PaymentOperationID(v), err
}

// NewContractVersion validates a contract version (>= 1).
func NewContractVersion(v int) (ContractVersion, error) {
	if v < 1 {
		return 0, fmt.Errorf("contract version %d must be >= 1", v)
	}
	return ContractVersion(v), nil
}

// NewItemVersion validates a catalog item version (>= 1).
func NewItemVersion(v int) (ItemVersion, error) {
	if v < 1 {
		return 0, fmt.Errorf("item version %d must be >= 1", v)
	}
	return ItemVersion(v), nil
}

// NewOperationKey validates an idempotency key: required, trimmed, bounded.
func NewOperationKey(raw string) (OperationKey, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("operation key must be non-empty with no surrounding whitespace")
	}
	if len(raw) > maxOperationKey {
		return "", fmt.Errorf("operation key exceeds %d bytes", maxOperationKey)
	}
	return OperationKey(raw), nil
}

// NewCatalogItemID validates a catalog item slug.
func NewCatalogItemID(raw string) (CatalogItemID, error) {
	if raw == "" || len(raw) > maxIdentifierLen {
		return "", fmt.Errorf("catalog item id must be 1..%d characters", maxIdentifierLen)
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
		if !ok {
			return "", fmt.Errorf("catalog item id contains invalid character %q", c)
		}
	}
	return CatalogItemID(raw), nil
}

// NewProviderRequestID validates a provider request identity.
func NewProviderRequestID(raw string) (ProviderRequestID, error) {
	if raw == "" || len(raw) > maxIdentifierLen {
		return "", fmt.Errorf("provider request id must be 1..%d characters", maxIdentifierLen)
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < 0x21 || raw[i] > 0x7e {
			return "", fmt.Errorf("provider request id contains non-printable character")
		}
	}
	return ProviderRequestID(raw), nil
}

// canonicalUUID checks the canonical 8-4-4-4-12 hex form and lowercases it.
func canonicalUUID(raw string) (string, error) {
	if len(raw) != 36 {
		return "", fmt.Errorf("identifier must be a 36-character UUID")
	}
	lower := strings.ToLower(raw)
	for i := 0; i < 36; i++ {
		c := lower[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return "", fmt.Errorf("identifier is not in canonical UUID form")
			}
			continue
		}
		if !isHexDigit(c) {
			return "", fmt.Errorf("identifier is not in canonical UUID form")
		}
	}
	return lower, nil
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}
