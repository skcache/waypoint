package domain

import (
	"errors"
	"fmt"
	"math"
)

// Money in Waypoint is always integer minor units of a currency — never
// floating point. One minor unit of USD is one cent.
//
// MinorUnits is intentionally a plain named integer: explicit validation at
// the edges, no arithmetic framework.
type MinorUnits int64

// Currency codes are fixed strings for the locked MVP. This release supports
// USD only.
type Currency string

const (
	// CurrencyUSD is the only currency this release may transact.
	CurrencyUSD Currency = "USD"

	// MaxAmountMinor is the hard sanity cap, mirroring the database check
	// (0001_init.sql: amount_minor <= 1_000_000_000 — i.e. $10,000,000.00).
	MaxAmountMinor MinorUnits = 1_000_000_000

	// MaxQuantity mirrors the database bound (quantity <= 1000).
	MaxQuantity = 1000
)

// Valid reports whether the currency is one this release supports.
func (c Currency) Valid() bool { return c == CurrencyUSD }

// Quantity is a positive purchase quantity (1..MaxQuantity).
type Quantity int

// NewQuantity validates a quantity.
func NewQuantity(n int) (Quantity, error) {
	if n < 1 || n > MaxQuantity {
		return 0, fmt.Errorf("quantity %d outside 1..%d", n, MaxQuantity)
	}
	return Quantity(n), nil
}

// Validate reports whether an amount is positive and within the hard cap.
func (m MinorUnits) Validate() error {
	if m <= 0 {
		return fmt.Errorf("amount %d must be positive", int64(m))
	}
	if m > MaxAmountMinor {
		return fmt.Errorf("amount %d exceeds hard cap %d", int64(m), int64(MaxAmountMinor))
	}
	return nil
}

// ErrAmountOverflow marks a multiplication that would wrap int64.
var ErrAmountOverflow = errors.New("amount overflow")

// MultiplyPrice computes unit price × quantity in integer minor units,
// checking for overflow before it can wrap. Callers must still enforce the
// ceiling checks (Validate / contract maximum); this function only guarantees
// the arithmetic itself is sound.
func MultiplyPrice(unitPrice MinorUnits, quantity Quantity) (MinorUnits, error) {
	q := int64(quantity)
	if q < 1 {
		return 0, fmt.Errorf("quantity %d must be positive", q)
	}
	p := int64(unitPrice)
	if p <= 0 {
		return 0, fmt.Errorf("unit price %d must be positive", p)
	}
	if p > math.MaxInt64/q {
		return 0, ErrAmountOverflow
	}
	return MinorUnits(p * q), nil
}
