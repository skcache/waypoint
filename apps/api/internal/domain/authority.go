package domain

import (
	"errors"
	"fmt"
	"time"
)

// Contract is one CONFIRMED intent-contract version (PRD §7.1) as loaded
// from the database. Draft rows never reach this type.
type Contract struct {
	ID            ContractID
	Version       ContractVersion
	Principal     PrincipalID
	Currency      Currency
	MaxTotal      MinorUnits
	MaxQuantity   Quantity
	CanonicalHash Fingerprint
	// ConfirmedAt zero means the version is not confirmed.
	ConfirmedAt time.Time
	ExpiresAt   time.Time
}

// CatalogItem is one server-trusted catalog fixture version (PRD §7.2/§6.3).
// Prices come from this row — never from the proposal.
type CatalogItem struct {
	ID       CatalogItemID
	Version  ItemVersion
	Price    MinorUnits
	Currency Currency
}

// Proposal is the incoming, UNTRUSTED purchase proposal. Every field is a
// claim to be verified against trusted data; nothing here grants permission.
type Proposal struct {
	Principal          PrincipalID
	ContractID         ContractID
	ContractVersion    ContractVersion
	CatalogItemID      CatalogItemID
	CatalogItemVersion ItemVersion
	Quantity           Quantity
	// ClaimedAmountMinor is the agent-supplied total. It must equal the
	// server-computed price × quantity, or the proposal is rejected.
	ClaimedAmountMinor MinorUnits
	OperationKey       OperationKey
}

// StoredAction is the action already recorded for an operation key — the
// echo of a previously accepted proposal, used to make replays idempotent.
type StoredAction struct {
	Key         OperationKey
	Fingerprint Fingerprint
}

// AuthorizedAction is the derived, server-computed description of the
// purchase that authorization binds. Immutable once produced.
type AuthorizedAction struct {
	Principal       PrincipalID
	ContractID      ContractID
	ContractVersion ContractVersion
	ContractHash    Fingerprint
	ItemID          CatalogItemID
	ItemVersion     ItemVersion
	Quantity        Quantity
	Amount          MinorUnits
	Currency        Currency
	Fingerprint     Fingerprint
}

// ViolationCode is a machine-readable invariant breach.
type ViolationCode string

const (
	ViolationTimeInvalid                ViolationCode = "TIME_INVALID"
	ViolationContractIDInvalid          ViolationCode = "CONTRACT_ID_INVALID"
	ViolationPrincipalInvalid           ViolationCode = "PRINCIPAL_INVALID"
	ViolationItemIDInvalid              ViolationCode = "ITEM_ID_INVALID"
	ViolationContractHashMissing        ViolationCode = "CONTRACT_HASH_MISSING"
	ViolationContractMismatch           ViolationCode = "CONTRACT_MISMATCH"
	ViolationPrincipalMismatch          ViolationCode = "PRINCIPAL_MISMATCH"
	ViolationContractVersionUnbound     ViolationCode = "CONTRACT_VERSION_UNBOUND"
	ViolationContractUnconfirmed        ViolationCode = "CONTRACT_UNCONFIRMED"
	ViolationConfirmationInFuture       ViolationCode = "CONFIRMATION_IN_FUTURE"
	ViolationContractExpired            ViolationCode = "CONTRACT_EXPIRED"
	ViolationContractMaxTotalInvalid    ViolationCode = "CONTRACT_MAX_TOTAL_INVALID"
	ViolationContractMaxQuantityInvalid ViolationCode = "CONTRACT_MAX_QUANTITY_INVALID"
	ViolationCurrencyUnsupported        ViolationCode = "CURRENCY_UNSUPPORTED"
	ViolationCurrencyMismatch           ViolationCode = "CURRENCY_MISMATCH"
	ViolationQuantityInvalid            ViolationCode = "QUANTITY_INVALID"
	ViolationQuantityExceedsMax         ViolationCode = "QUANTITY_EXCEEDS_MAX"
	ViolationItemMismatch               ViolationCode = "ITEM_MISMATCH"
	ViolationItemVersionUnbound         ViolationCode = "ITEM_VERSION_UNBOUND"
	ViolationAmountInvalid              ViolationCode = "AMOUNT_INVALID"
	ViolationAmountExceedsMax           ViolationCode = "AMOUNT_EXCEEDS_MAX"
	ViolationAmountOverflow             ViolationCode = "AMOUNT_OVERFLOW"
	ViolationPriceMismatch              ViolationCode = "CATALOG_PRICE_MISMATCH"
	ViolationOperationKeyInvalid        ViolationCode = "OPERATION_KEY_INVALID"
	ViolationOperationKeyReuse          ViolationCode = "OPERATION_KEY_REUSE_CONFLICT"
)

// Violation is one breached invariant with a human-readable detail.
type Violation struct {
	Code   ViolationCode
	Detail string
}

// ValidationResult carries the bound action (meaningful only when there are
// no violations) and every breach found. Checks never stop at the first
// violation; the order is deterministic.
type ValidationResult struct {
	Action     AuthorizedAction
	Violations []Violation
}

// OK reports whether every invariant held.
func (r ValidationResult) OK() bool { return len(r.Violations) == 0 }

// ValidationRequest bundles the trusted and untrusted inputs.
type ValidationRequest struct {
	Contract Contract
	Item     CatalogItem
	Proposal Proposal
	// Now is the decision time, passed in explicitly by the caller (database
	// or coordinator time). Expiry is evaluated against it — never against a
	// hidden wall clock. Zero time is rejected.
	Now time.Time
	// Existing, when non-nil, is the action already recorded for this
	// operation key: same fingerprint = idempotent replay (accepted),
	// different fingerprint = conflict.
	Existing *StoredAction
}

// ValidateProposal enforces the Phase 03 authority and monetary invariants of
// a proposed purchase against a confirmed contract and trusted catalog data:
//
//  1. the proposal's principal owns the contract;
//  2. the contract is explicitly confirmed;
//  3. the contract is unexpired at `Now` (the decision time);
//  4. the contract version is explicitly bound;
//  5. currencies match and are USD;
//  6. quantity is positive and within the confirmed maximum;
//  7. the server-computed total is positive and within the confirmed
//     ceiling (and the hard cap);
//  8. item identity and catalog version are explicitly bound to the loaded
//     catalog row;
//  9. the amount is recomputed as trusted price × quantity with
//     overflow-safe integer arithmetic — agent-supplied amounts are claims,
//     not facts;
//  10. the resulting action fingerprint is deterministic over the bound
//     fields; and
//  11. reusing an operation key for different action content is a conflict,
//     not an idempotent success.
//
// All inputs are also treated as hostile structs: values that bypassed their
// constructors (missing identifiers, zero or out-of-range limits, absent
// canonical hash, empty operation key, future confirmation timestamps) are
// rejected rather than silently skipped — validation is fail-closed.
//
// This is deterministic authorization only. Semantic verification remains a
// separate gate: AggregateDecision is where its verdict enters, and nothing
// here should be read as replacing it. Callers must re-run this validation at
// the moment of execution — creation-time checks alone are insufficient for
// contracts that can expire in between.
func ValidateProposal(req ValidationRequest) ValidationResult {
	var violations []Violation
	add := func(code ViolationCode, format string, args ...any) {
		violations = append(violations, Violation{Code: code, Detail: fmt.Sprintf(format, args...)})
	}

	c, item, p := req.Contract, req.Item, req.Proposal

	// Decision time must be explicit and sane.
	timeOK := !req.Now.IsZero()
	if !timeOK {
		add(ViolationTimeInvalid, "decision time must be provided")
	}

	// 0. Malformed inputs: reject anything that bypassed its constructor.
	if _, err := canonicalUUID(string(c.ID)); err != nil {
		add(ViolationContractIDInvalid, "contract id %q is not a canonical UUID", c.ID)
	}
	if _, err := canonicalUUID(string(c.Principal)); err != nil {
		add(ViolationPrincipalInvalid, "contract principal %q is not a canonical UUID", c.Principal)
	}
	if _, err := NewCatalogItemID(string(item.ID)); err != nil {
		add(ViolationItemIDInvalid, "catalog item id %q is invalid", item.ID)
	}
	if c.CanonicalHash.IsZero() {
		add(ViolationContractHashMissing, "contract %s v%d has no canonical hash", c.ID, c.Version)
	}

	// 1–4. Authority identity, ownership, version binding, confirmation.
	if p.ContractID != c.ID {
		add(ViolationContractMismatch, "proposal contract %s does not match loaded contract %s", p.ContractID, c.ID)
	}
	if p.Principal != c.Principal {
		add(ViolationPrincipalMismatch, "proposal principal %s does not own contract %s (owner %s)", p.Principal, c.ID, c.Principal)
	}
	if p.ContractVersion < 1 || p.ContractVersion != c.Version {
		add(ViolationContractVersionUnbound, "proposal contract version %d does not bind loaded version %d", p.ContractVersion, c.Version)
	}
	confirmed := !c.ConfirmedAt.IsZero()
	if !confirmed {
		add(ViolationContractUnconfirmed, "contract %s v%d is not confirmed", c.ID, c.Version)
	}
	if confirmed && timeOK && c.ConfirmedAt.After(req.Now) {
		add(ViolationConfirmationInFuture, "contract %s v%d claims confirmation at %s, after the decision time %s",
			c.ID, c.Version, c.ConfirmedAt.UTC().Format(time.RFC3339), req.Now.UTC().Format(time.RFC3339))
	}
	if confirmed && timeOK && !req.Now.Before(c.ExpiresAt) {
		add(ViolationContractExpired, "contract %s v%d expired at %s", c.ID, c.Version, c.ExpiresAt.UTC().Format(time.RFC3339))
	}

	// Confirmed limits must exist and be sane; mirror the database bounds.
	limitsOK := c.MaxTotal > 0 && c.MaxTotal <= MaxAmountMinor
	if !limitsOK {
		add(ViolationContractMaxTotalInvalid, "contract maximum total %d outside 1..%d", c.MaxTotal, MaxAmountMinor)
	}
	maxQtyOK := c.MaxQuantity >= 1 && c.MaxQuantity <= MaxQuantity
	if !maxQtyOK {
		add(ViolationContractMaxQuantityInvalid, "contract maximum quantity %d outside 1..%d", c.MaxQuantity, MaxQuantity)
	}

	// 5. Currency: contract must be USD, item must match the contract.
	if !c.Currency.Valid() {
		add(ViolationCurrencyUnsupported, "contract currency %q is not supported", c.Currency)
	} else if item.Currency != c.Currency {
		add(ViolationCurrencyMismatch, "item currency %q does not match contract currency %q", item.Currency, c.Currency)
	}

	// 6. Quantity bounds (hard bounds always; confirmed maximum when known).
	if p.Quantity < 1 || p.Quantity > MaxQuantity {
		add(ViolationQuantityInvalid, "quantity %d outside 1..%d", p.Quantity, MaxQuantity)
	} else if confirmed && maxQtyOK && p.Quantity > c.MaxQuantity {
		add(ViolationQuantityExceedsMax, "quantity %d exceeds confirmed maximum %d", p.Quantity, c.MaxQuantity)
	}

	// 8. Item binding: the loaded catalog row must be exactly the referenced
	// item version.
	if item.ID != p.CatalogItemID {
		add(ViolationItemMismatch, "loaded catalog item %s does not match proposal item %s", item.ID, p.CatalogItemID)
	}
	if p.CatalogItemVersion < 1 || item.Version != p.CatalogItemVersion {
		add(ViolationItemVersionUnbound, "proposal item version %d does not bind loaded version %d", p.CatalogItemVersion, item.Version)
	}

	// 9–10. Amount recomputed from trusted price; then bind the action and
	// derive its fingerprint (always computed — replay checks need it).
	action := AuthorizedAction{
		Principal:       c.Principal,
		ContractID:      c.ID,
		ContractVersion: c.Version,
		ContractHash:    c.CanonicalHash,
		ItemID:          item.ID,
		ItemVersion:     item.Version,
		Quantity:        p.Quantity,
		Currency:        c.Currency,
	}
	if p.Quantity >= 1 {
		computed, err := MultiplyPrice(item.Price, p.Quantity)
		switch {
		case errors.Is(err, ErrAmountOverflow):
			add(ViolationAmountOverflow, "price %d × quantity %d overflows integer minor units", item.Price, p.Quantity)
		case err != nil:
			add(ViolationAmountInvalid, "amount not computable: %v", err)
		default:
			action.Amount = computed
			switch {
			case computed <= 0:
				add(ViolationAmountInvalid, "computed total %d must be positive", computed)
			case computed > MaxAmountMinor:
				add(ViolationAmountExceedsMax, "computed total %d exceeds hard cap %d", computed, MaxAmountMinor)
			case confirmed && limitsOK && computed > c.MaxTotal:
				add(ViolationAmountExceedsMax, "computed total %d exceeds confirmed ceiling %d", computed, c.MaxTotal)
			}
			if p.ClaimedAmountMinor != computed {
				add(ViolationPriceMismatch, "claimed total %d does not match catalog price × quantity %d", p.ClaimedAmountMinor, computed)
			}
		}
	}
	action.Fingerprint = ComputeActionFingerprint(action)

	// 11. Operation-key validity and reuse semantics.
	if _, err := NewOperationKey(string(p.OperationKey)); err != nil {
		add(ViolationOperationKeyInvalid, "operation key invalid: %v", err)
	}
	if req.Existing != nil {
		if req.Existing.Key != p.OperationKey {
			add(ViolationOperationKeyReuse, "stored action is bound to key %q, not %q", req.Existing.Key, p.OperationKey)
		} else if req.Existing.Fingerprint != action.Fingerprint {
			add(ViolationOperationKeyReuse, "operation key %q was already used with different action content", p.OperationKey)
		}
	}

	if len(violations) > 0 {
		return ValidationResult{Violations: violations}
	}
	return ValidationResult{Action: action}
}
