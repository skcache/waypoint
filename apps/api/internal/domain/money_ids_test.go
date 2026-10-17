package domain

import (
	"strings"
	"testing"
)

func TestNewQuantityRejectsInvalid(t *testing.T) {
	for _, n := range []int{-1, 0, MaxQuantity + 1, 100000} {
		if _, err := NewQuantity(n); err == nil {
			t.Errorf("NewQuantity(%d) succeeded, want error", n)
		}
	}
	for _, n := range []int{1, 2, MaxQuantity} {
		q, err := NewQuantity(n)
		if err != nil {
			t.Errorf("NewQuantity(%d) failed: %v", n, err)
		}
		if int(q) != n {
			t.Errorf("NewQuantity(%d) = %d", n, q)
		}
	}
}

func TestMultiplyPrice(t *testing.T) {
	const maxI64 = int64(^uint64(0) >> 1)

	tests := []struct {
		name    string
		price   MinorUnits
		qty     Quantity
		want    MinorUnits
		wantErr bool
	}{
		{"simple", 12000, 2, 24000, false},
		{"single", 4999, 1, 4999, false},
		{"zero quantity", 12000, 0, 0, true},
		{"negative quantity", 12000, -3, 0, true},
		{"zero price", 0, 1, 0, true},
		{"negative price", -100, 1, 0, true},
		{"largest price single unit", MinorUnits(maxI64), 1, MinorUnits(maxI64), false},
		{"largest price times two overflows", MinorUnits(maxI64), 2, 0, true},
		{"just over half overflows", MinorUnits(maxI64/2 + 1), 2, 0, true},
		{"exactly half fits", MinorUnits(maxI64 / 2), 2, MinorUnits(maxI64 - 1), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MultiplyPrice(tc.price, tc.qty)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("MultiplyPrice(%d, %d) = %d, want error", tc.price, tc.qty, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("MultiplyPrice(%d, %d) failed: %v", tc.price, tc.qty, err)
			}
			if got != tc.want {
				t.Fatalf("MultiplyPrice(%d, %d) = %d, want %d", tc.price, tc.qty, got, tc.want)
			}
		})
	}
}

func TestMinorUnitsValidate(t *testing.T) {
	for _, m := range []MinorUnits{-1, 0, MaxAmountMinor + 1, MaxAmountMinor * 1000} {
		if err := m.Validate(); err == nil {
			t.Errorf("MinorUnits(%d).Validate() succeeded, want error", m)
		}
	}
	for _, m := range []MinorUnits{1, 100, MaxAmountMinor} {
		if err := m.Validate(); err != nil {
			t.Errorf("MinorUnits(%d).Validate() failed: %v", m, err)
		}
	}
}

func TestCurrencyIsUSDOnly(t *testing.T) {
	if !CurrencyUSD.Valid() {
		t.Error("CurrencyUSD must be valid")
	}
	for _, c := range []Currency{"", "EUR", "usd", "USDC", "JPY"} {
		if c.Valid() {
			t.Errorf("Currency(%q).Valid() = true, want false", c)
		}
	}
}

func TestIdentifiers(t *testing.T) {
	const good = "a3f1b2c4-1111-2222-3333-444455556666"

	tx, err := NewTransactionID(good)
	if err != nil || string(tx) != good {
		t.Fatalf("NewTransactionID(good) = %q, %v", tx, err)
	}
	if up, err := NewTransactionID(strings.ToUpper(good)); err != nil || string(up) != good {
		t.Fatalf("uppercase uuid must canonicalize to lowercase, got %q, %v", up, err)
	}

	bad := []string{
		"",
		"not-a-uuid",
		"a3f1b2c4-1111-2222-3333-44445555666",   // 35 chars
		"a3f1b2c4-1111-2222-3333-4444555566666", // 37 chars
		"g3f1b2c4-1111-2222-3333-444455556666",  // non-hex
		"a3f1b2c41111-2222-3333-444455556666xx", // hyphens misplaced
	}
	for _, s := range bad {
		if _, err := NewTransactionID(s); err == nil {
			t.Errorf("NewTransactionID(%q) succeeded, want error", s)
		}
	}
	for _, ctor := range []func(string) (string, error){
		func(s string) (string, error) { v, err := NewPrincipalID(s); return string(v), err },
		func(s string) (string, error) { v, err := NewContractID(s); return string(v), err },
		func(s string) (string, error) { v, err := NewPaymentOperationID(s); return string(v), err },
	} {
		if v, err := ctor(good); err != nil || v != good {
			t.Errorf("uuid constructor(good) = %q, %v", v, err)
		}
		if _, err := ctor("nope"); err == nil {
			t.Error("uuid constructor accepted garbage")
		}
	}

	if _, err := NewContractVersion(0); err == nil {
		t.Error("contract version 0 must be rejected")
	}
	if v, err := NewContractVersion(3); err != nil || v != 3 {
		t.Errorf("NewContractVersion(3) = %d, %v", v, err)
	}
	if _, err := NewItemVersion(-1); err == nil {
		t.Error("item version -1 must be rejected")
	}
	if v, err := NewItemVersion(2); err != nil || v != 2 {
		t.Errorf("NewItemVersion(2) = %d, %v", v, err)
	}
}

func TestOperationKey(t *testing.T) {
	if k, err := NewOperationKey("op-abc-123"); err != nil || string(k) != "op-abc-123" {
		t.Fatalf("valid key rejected: %q, %v", k, err)
	}
	for _, s := range []string{"", "   ", " padded", "padded ", strings.Repeat("x", maxOperationKey+1)} {
		if _, err := NewOperationKey(s); err == nil {
			t.Errorf("NewOperationKey(%q) succeeded, want error", s)
		}
	}
	if k, err := NewOperationKey(strings.Repeat("x", maxOperationKey)); err != nil || len(k) != maxOperationKey {
		t.Errorf("max-length key must be accepted: %v", err)
	}
}

func TestCatalogItemID(t *testing.T) {
	if id, err := NewCatalogItemID("keyboard-office-quiet"); err != nil || string(id) != "keyboard-office-quiet" {
		t.Fatalf("valid slug rejected: %q, %v", id, err)
	}
	for _, s := range []string{"", "Bad Item", "UPPER", "no/slash", strings.Repeat("x", maxIdentifierLen+1)} {
		if _, err := NewCatalogItemID(s); err == nil {
			t.Errorf("NewCatalogItemID(%q) succeeded, want error", s)
		}
	}
}

func TestProviderRequestID(t *testing.T) {
	if id, err := NewProviderRequestID("req-123_ABC"); err != nil || string(id) != "req-123_ABC" {
		t.Fatalf("valid request id rejected: %q, %v", id, err)
	}
	for _, s := range []string{"", "has space", "line\nbreak", strings.Repeat("x", maxIdentifierLen+1)} {
		if _, err := NewProviderRequestID(s); err == nil {
			t.Errorf("NewProviderRequestID(%q) succeeded, want error", s)
		}
	}
}

func TestOperationType(t *testing.T) {
	if !OperationCreateOrder.Valid() || !OperationCapture.Valid() {
		t.Error("declared operation types must be valid")
	}
	for _, s := range []OperationType{"", "REFUND", "create_order", "VOID"} {
		if s.Valid() {
			t.Errorf("OperationType(%q).Valid() = true, want false", s)
		}
	}
}
