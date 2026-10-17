package domain

import (
	"testing"
	"time"
)

var (
	testNow   = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	testOwner = PrincipalID("11111111-1111-1111-1111-111111111111")
	testOther = PrincipalID("22222222-2222-2222-2222-222222222222")
)

func baseRequest() ValidationRequest {
	return ValidationRequest{
		Contract: Contract{
			ID:            ContractID("aaaaaaaa-0000-0000-0000-000000000001"),
			Version:       1,
			Principal:     testOwner,
			Currency:      CurrencyUSD,
			MaxTotal:      15000,
			MaxQuantity:   1,
			CanonicalHash: testFingerprint("contract-canonical"),
			ConfirmedAt:   testNow.Add(-time.Hour),
			ExpiresAt:     testNow.Add(24 * time.Hour),
		},
		Item: CatalogItem{
			ID:       CatalogItemID("keyboard-office-quiet"),
			Version:  1,
			Price:    12000,
			Currency: CurrencyUSD,
		},
		Proposal: Proposal{
			Principal:          testOwner,
			ContractID:         ContractID("aaaaaaaa-0000-0000-0000-000000000001"),
			ContractVersion:    1,
			CatalogItemID:      CatalogItemID("keyboard-office-quiet"),
			CatalogItemVersion: 1,
			Quantity:           1,
			ClaimedAmountMinor: 12000,
			OperationKey:       OperationKey("op-base-1"),
		},
		Now: testNow,
	}
}

func codesOf(vs []Violation) []ViolationCode {
	out := make([]ViolationCode, len(vs))
	for i, v := range vs {
		out[i] = v.Code
	}
	return out
}

func hasCode(vs []Violation, code ViolationCode) bool {
	for _, v := range vs {
		if v.Code == code {
			return true
		}
	}
	return false
}

func TestValidateProposalTable(t *testing.T) {
	tests := []struct {
		name string
		// mutate adjusts a fresh base request.
		mutate func(*ValidationRequest)
		// wantCodes: exact sequence when non-nil, "contains" otherwise.
		wantCodes []ViolationCode
		wantAny   []ViolationCode
	}{
		{
			name:   "valid confirmed proposal",
			mutate: func(*ValidationRequest) {},
		},
		{
			name:   "cross-principal proposal",
			mutate: func(r *ValidationRequest) { r.Proposal.Principal = testOther },
			wantCodes: []ViolationCode{
				ViolationPrincipalMismatch,
			},
		},
		{
			name:   "proposal names a different contract",
			mutate: func(r *ValidationRequest) { r.Proposal.ContractID = ContractID("bbbbbbbb-0000-0000-0000-000000000002") },
			wantCodes: []ViolationCode{
				ViolationContractMismatch,
			},
		},
		{
			name:   "contract version not bound",
			mutate: func(r *ValidationRequest) { r.Proposal.ContractVersion = 2 },
			wantCodes: []ViolationCode{
				ViolationContractVersionUnbound,
			},
		},
		{
			name:   "unconfirmed contract",
			mutate: func(r *ValidationRequest) { r.Contract.ConfirmedAt = time.Time{} },
			wantCodes: []ViolationCode{
				ViolationContractUnconfirmed,
			},
		},
		{
			name:   "unconfirmed contract reports exactly that breach",
			mutate: func(r *ValidationRequest) { r.Contract.ConfirmedAt = time.Time{} },
			wantCodes: []ViolationCode{
				ViolationContractUnconfirmed,
			},
		},
		{
			name:   "contract expired exactly at decision time",
			mutate: func(r *ValidationRequest) { r.Contract.ExpiresAt = testNow },
			wantCodes: []ViolationCode{
				ViolationContractExpired,
			},
		},
		{
			name:   "contract expired in the past",
			mutate: func(r *ValidationRequest) { r.Contract.ExpiresAt = testNow.Add(-time.Second) },
			wantCodes: []ViolationCode{
				ViolationContractExpired,
			},
		},
		{
			name:   "contract still valid one nanosecond before expiry",
			mutate: func(r *ValidationRequest) { r.Contract.ExpiresAt = testNow.Add(time.Nanosecond) },
		},
		{
			name:   "currency mismatch item vs contract",
			mutate: func(r *ValidationRequest) { r.Item.Currency = Currency("EUR") },
			wantCodes: []ViolationCode{
				ViolationCurrencyMismatch,
			},
		},
		{
			name:   "unsupported contract currency",
			mutate: func(r *ValidationRequest) { r.Contract.Currency = Currency("EUR"); r.Item.Currency = Currency("EUR") },
			wantCodes: []ViolationCode{
				ViolationCurrencyUnsupported,
			},
		},
		{
			name:   "zero quantity",
			mutate: func(r *ValidationRequest) { r.Proposal.Quantity = 0; r.Proposal.ClaimedAmountMinor = 0 },
			wantCodes: []ViolationCode{
				ViolationQuantityInvalid,
			},
		},
		{
			name: "quantity exceeds confirmed maximum",
			mutate: func(r *ValidationRequest) {
				r.Contract.MaxTotal = 30000
				r.Proposal.Quantity = 2
				r.Proposal.ClaimedAmountMinor = 24000
			},
			wantCodes: []ViolationCode{
				ViolationQuantityExceedsMax,
			},
		},
		{
			name: "quantity at confirmed maximum is allowed",
			mutate: func(r *ValidationRequest) {
				r.Contract.MaxQuantity = 2
				r.Contract.MaxTotal = 24000
				r.Proposal.Quantity = 2
				r.Proposal.ClaimedAmountMinor = 24000
			},
		},
		{
			name:   "loaded catalog item does not match proposal",
			mutate: func(r *ValidationRequest) { r.Item.ID = CatalogItemID("other-item") },
			wantCodes: []ViolationCode{
				ViolationItemMismatch,
			},
		},
		{
			name:   "item version not bound",
			mutate: func(r *ValidationRequest) { r.Item.Version = 2 },
			wantCodes: []ViolationCode{
				ViolationItemVersionUnbound,
			},
		},
		{
			name:   "claimed amount does not match catalog price",
			mutate: func(r *ValidationRequest) { r.Proposal.ClaimedAmountMinor = 1 },
			wantCodes: []ViolationCode{
				ViolationPriceMismatch,
			},
		},
		{
			name:   "computed total exceeds confirmed ceiling",
			mutate: func(r *ValidationRequest) { r.Item.Price = 20000; r.Proposal.ClaimedAmountMinor = 20000 },
			wantCodes: []ViolationCode{
				ViolationAmountExceedsMax,
			},
		},
		{
			name:   "computed total at confirmed ceiling is allowed",
			mutate: func(r *ValidationRequest) { r.Item.Price = 15000; r.Proposal.ClaimedAmountMinor = 15000 },
		},
		{
			name: "computed total exceeds hard cap",
			mutate: func(r *ValidationRequest) {
				r.Item.Price = MaxAmountMinor + 1
				r.Contract.MaxTotal = MinorUnits(MaxAmountMinor) * 2
				r.Proposal.ClaimedAmountMinor = MaxAmountMinor + 1
			},
			wantCodes: []ViolationCode{
				ViolationAmountExceedsMax,
			},
		},
		{
			name: "computed total exactly at hard cap is allowed",
			mutate: func(r *ValidationRequest) {
				r.Item.Price = MaxAmountMinor / 2
				r.Contract.MaxQuantity = 2
				r.Contract.MaxTotal = MaxAmountMinor
				r.Proposal.Quantity = 2
				r.Proposal.ClaimedAmountMinor = MaxAmountMinor
			},
		},
		{
			name: "price times quantity overflows",
			mutate: func(r *ValidationRequest) {
				const maxI64 = int64(^uint64(0) >> 1)
				r.Item.Price = MinorUnits(maxI64/2 + 1)
				r.Contract.MaxQuantity = 2
				r.Proposal.Quantity = 2
			},
			wantCodes: []ViolationCode{
				ViolationAmountOverflow,
			},
		},
		{
			name:   "zero decision time",
			mutate: func(r *ValidationRequest) { r.Now = time.Time{} },
			wantCodes: []ViolationCode{
				ViolationTimeInvalid,
			},
		},
		{
			name: "multiple breaches reported in deterministic order",
			mutate: func(r *ValidationRequest) {
				r.Proposal.Principal = testOther
				r.Contract.ExpiresAt = testNow.Add(-time.Hour)
				r.Item.Currency = Currency("EUR")
			},
			wantCodes: []ViolationCode{
				ViolationPrincipalMismatch,
				ViolationContractExpired,
				ViolationCurrencyMismatch,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := baseRequest()
			tc.mutate(&req)
			res := ValidateProposal(req)

			if len(tc.wantCodes) == 0 && len(tc.wantAny) == 0 {
				if !res.OK() {
					t.Fatalf("expected acceptance, got %v", codesOf(res.Violations))
				}
				return
			}
			if res.OK() {
				t.Fatalf("expected violations %v, got acceptance", tc.wantCodes)
			}
			if len(tc.wantCodes) > 0 {
				got := codesOf(res.Violations)
				if len(got) != len(tc.wantCodes) {
					t.Fatalf("violations %v, want %v", got, tc.wantCodes)
				}
				for i := range got {
					if got[i] != tc.wantCodes[i] {
						t.Fatalf("violations %v, want %v", got, tc.wantCodes)
					}
				}
			}
			for _, code := range tc.wantAny {
				if !hasCode(res.Violations, code) {
					t.Fatalf("violations %v missing %s", codesOf(res.Violations), code)
				}
			}
		})
	}
}

func TestAcceptedProposalBindsAction(t *testing.T) {
	req := baseRequest()
	res := ValidateProposal(req)
	if !res.OK() {
		t.Fatalf("base proposal rejected: %v", codesOf(res.Violations))
	}
	a := res.Action
	if a.Amount != 12000 || a.Currency != CurrencyUSD || a.Quantity != 1 {
		t.Fatalf("bound action = %+v", a)
	}
	if a.Principal != testOwner || a.ContractID != req.Contract.ID || a.ContractHash != req.Contract.CanonicalHash {
		t.Fatalf("bound action lost authority identity: %+v", a)
	}
	if a.Fingerprint != ComputeActionFingerprint(a) {
		t.Fatal("bound fingerprint does not match the canonical action")
	}
	if a.Fingerprint.IsZero() {
		t.Fatal("bound fingerprint is zero")
	}
}

func TestOperationKeyReplaySemantics(t *testing.T) {
	req := baseRequest()
	first := ValidateProposal(req)
	if !first.OK() {
		t.Fatalf("first proposal rejected: %v", codesOf(first.Violations))
	}

	// Exact replay under the same key: idempotent success.
	replay := baseRequest()
	replay.Existing = &StoredAction{Key: replay.Proposal.OperationKey, Fingerprint: first.Action.Fingerprint}
	if res := ValidateProposal(replay); !res.OK() {
		t.Fatalf("exact replay must succeed, got %v", codesOf(res.Violations))
	}

	// Same key, different quantity: conflict, not idempotent success.
	changedQty := baseRequest()
	changedQty.Contract.MaxQuantity = 5
	changedQty.Contract.MaxTotal = 60000
	changedQty.Proposal.Quantity = 2
	changedQty.Proposal.ClaimedAmountMinor = 24000
	changedQty.Existing = &StoredAction{Key: changedQty.Proposal.OperationKey, Fingerprint: first.Action.Fingerprint}
	res := ValidateProposal(changedQty)
	if !hasCode(res.Violations, ViolationOperationKeyReuse) {
		t.Fatalf("same key with different content must conflict, got %v", codesOf(res.Violations))
	}

	// Same key, different item: conflict too.
	changedItem := baseRequest()
	changedItem.Item.ID = CatalogItemID("mouse-office-quiet")
	changedItem.Proposal.CatalogItemID = CatalogItemID("mouse-office-quiet")
	changedItem.Existing = &StoredAction{Key: changedItem.Proposal.OperationKey, Fingerprint: first.Action.Fingerprint}
	res = ValidateProposal(changedItem)
	if !hasCode(res.Violations, ViolationOperationKeyReuse) {
		t.Fatalf("same key with a different item must conflict, got %v", codesOf(res.Violations))
	}

	// Stored action under a different key: caller bug, refused.
	otherKey := baseRequest()
	otherKey.Existing = &StoredAction{Key: OperationKey("op-some-other"), Fingerprint: first.Action.Fingerprint}
	res = ValidateProposal(otherKey)
	if !hasCode(res.Violations, ViolationOperationKeyReuse) {
		t.Fatalf("mismatched stored key must be refused, got %v", codesOf(res.Violations))
	}

	// A different key with the same content is simply a new operation: fine.
	newKey := baseRequest()
	newKey.Proposal.OperationKey = OperationKey("op-base-2")
	if res := ValidateProposal(newKey); !res.OK() {
		t.Fatalf("a fresh key with valid content must be accepted, got %v", codesOf(res.Violations))
	}
}
