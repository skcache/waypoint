package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
