package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Fingerprint is a SHA-256 digest (32 bytes).
type Fingerprint [sha256.Size]byte

// NewFingerprint validates and copies a raw digest.
func NewFingerprint(raw []byte) (Fingerprint, error) {
	var f Fingerprint
	if len(raw) != sha256.Size {
		return f, fmt.Errorf("fingerprint must be %d bytes, got %d", sha256.Size, len(raw))
	}
	copy(f[:], raw)
	return f, nil
}

// Hex returns the lowercase hex encoding (64 characters).
func (f Fingerprint) Hex() string { return hex.EncodeToString(f[:]) }

// IsZero reports whether the fingerprint is the zero value (unset).
func (f Fingerprint) IsZero() bool { return f == Fingerprint{} }

// String renders the digest with its algorithm prefix (see PRD §7.1).
func (f Fingerprint) String() string { return "sha256:" + f.Hex() }

// actionScheme versions the canonical byte representation, so any future
// change to the scheme is a deliberate, visible version bump rather than a
// silent change in fingerprint meaning.
const actionScheme = "waypoint.action.v1"

// canonicalAction is the deterministic byte-level representation of the
// authorized purchase: fixed field order, only scalar fields, no maps — so
// encoding/json output is stable across runs and machines.
type canonicalAction struct {
	Scheme          string `json:"scheme"`
	Principal       string `json:"principal"`
	ContractID      string `json:"contract_id"`
	ContractVersion int    `json:"contract_version"`
	ContractHash    string `json:"contract_hash"`
	ItemID          string `json:"item_id"`
	ItemVersion     int    `json:"item_version"`
	Quantity        int    `json:"quantity"`
	AmountMinor     int64  `json:"amount_minor"`
	Currency        string `json:"currency"`
}

// ComputeActionFingerprint derives the fingerprint of the immutable purchase
// description. Same inputs produce the same digest; any bound field changing
// produces a different digest — which is what makes operation-key reuse with
// different content detectable (see ValidateProposal).
func ComputeActionFingerprint(a AuthorizedAction) Fingerprint {
	payload, err := json.Marshal(canonicalAction{
		Scheme:          actionScheme,
		Principal:       string(a.Principal),
		ContractID:      string(a.ContractID),
		ContractVersion: int(a.ContractVersion),
		ContractHash:    a.ContractHash.Hex(),
		ItemID:          string(a.ItemID),
		ItemVersion:     int(a.ItemVersion),
		Quantity:        int(a.Quantity),
		AmountMinor:     int64(a.Amount),
		Currency:        string(a.Currency),
	})
	if err != nil {
		// Unreachable for this plain scalar struct; kept explicit rather than
		// returning a fingerprint of nothing.
		panic(fmt.Sprintf("domain: canonical action marshal failed: %v", err))
	}
	return sha256.Sum256(payload)
}

// contractScheme versions the canonical contract encoding, same rationale as
// actionScheme: any change to the scheme is a deliberate version bump rather
// than a silent change in hash meaning.
const contractScheme = "waypoint.contract.v1"

// ContractTerms is the deterministic content covered by the confirmed
// contract hash (PRD §7.1): the normalized request, the hard spending limits
// and the qualitative preferences a purchase is judged against.
//
// Deliberately NOT covered: confirmation/expiry/creation timestamps (they
// change without the terms changing — expiry integrity is a database
// concern), transient HTML (callers pass the already-normalized request
// text), and the version number (the database owns the version sequence; the
// version is bound into the action fingerprint via ContractVersion instead).
type ContractTerms struct {
	Principal       PrincipalID
	OriginalRequest string
	Currency        Currency
	MaxTotal        MinorUnits
	MaxQuantity     Quantity
	Category        string
	// Preferences is the qualitative mandate ("quiet", "no gaming RGB", …).
	// Source order carries no meaning and is normalized away.
	Preferences []string
}

// canonicalContract is the fixed-field-order encoding of ContractTerms.
// Preferences are sorted (and nil normalized to empty) before encoding, so
// any source ordering — including the nondeterministic iteration order of a
// map — produces identical bytes.
type canonicalContract struct {
	Scheme          string   `json:"scheme"`
	Principal       string   `json:"principal"`
	OriginalRequest string   `json:"original_request"`
	Currency        string   `json:"currency"`
	MaxTotalMinor   int64    `json:"max_total_minor"`
	MaxQuantity     int      `json:"max_quantity"`
	Category        string   `json:"category"`
	Preferences     []string `json:"qualitative_preferences"`
}

// ComputeContractHash derives the canonical hash of confirmed contract
// terms. Identical terms produce an identical digest; any changed authorized
// term — a limit, the currency, a preference — produces a different digest.
// That is what lets the system treat re-confirmation of unchanged terms as
// the same mandate while a changed mandate requires a new confirmed version
// (exactly the versioning the database enforces).
func ComputeContractHash(t ContractTerms) Fingerprint {
	prefs := make([]string, len(t.Preferences))
	copy(prefs, t.Preferences)
	sort.Strings(prefs)
	if prefs == nil {
		prefs = []string{}
	}
	payload, err := json.Marshal(canonicalContract{
		Scheme:          contractScheme,
		Principal:       string(t.Principal),
		OriginalRequest: t.OriginalRequest,
		Currency:        string(t.Currency),
		MaxTotalMinor:   int64(t.MaxTotal),
		MaxQuantity:     int(t.MaxQuantity),
		Category:        t.Category,
		Preferences:     prefs,
	})
	if err != nil {
		panic(fmt.Sprintf("domain: canonical contract marshal failed: %v", err))
	}
	return sha256.Sum256(payload)
}
