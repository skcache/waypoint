package domain

import (
	"crypto/sha256"
	"testing"
)

func testFingerprint(material string) Fingerprint {
	return sha256.Sum256([]byte(material))
}

func baseAction() AuthorizedAction {
	return AuthorizedAction{
		Principal:       PrincipalID("11111111-1111-1111-1111-111111111111"),
		ContractID:      ContractID("aaaaaaaa-0000-0000-0000-000000000001"),
		ContractVersion: 1,
		ContractHash:    testFingerprint("contract-canonical"),
		ItemID:          CatalogItemID("keyboard-office-quiet"),
		ItemVersion:     1,
		Quantity:        1,
		Amount:          12000,
		Currency:        CurrencyUSD,
	}
}

func TestActionFingerprintIsDeterministic(t *testing.T) {
	a := baseAction()
	fp1 := ComputeActionFingerprint(a)
	fp2 := ComputeActionFingerprint(a)
	if fp1 != fp2 {
		t.Fatalf("fingerprint not deterministic: %s vs %s", fp1, fp2)
	}
	// Rebuilt from the same values: still identical.
	b := baseAction()
	if ComputeActionFingerprint(b) != fp1 {
		t.Fatal("fingerprint differs across equivalent rebuilds")
	}
	if fp1.IsZero() {
		t.Fatal("fingerprint of a real action must not be zero")
	}
	if len(fp1.String()) != len("sha256:")+64 {
		t.Fatalf("String() = %q", fp1.String())
	}
}

func TestActionFingerprintCoversEveryBoundField(t *testing.T) {
	base := ComputeActionFingerprint(baseAction())

	mutations := map[string]func(*AuthorizedAction){
		"principal":        func(a *AuthorizedAction) { a.Principal = PrincipalID("22222222-2222-2222-2222-222222222222") },
		"contract id":      func(a *AuthorizedAction) { a.ContractID = ContractID("bbbbbbbb-0000-0000-0000-000000000002") },
		"contract version": func(a *AuthorizedAction) { a.ContractVersion = 2 },
		"contract hash":    func(a *AuthorizedAction) { a.ContractHash = testFingerprint("other-contract") },
		"item id":          func(a *AuthorizedAction) { a.ItemID = CatalogItemID("mouse-office-quiet") },
		"item version":     func(a *AuthorizedAction) { a.ItemVersion = 2 },
		"quantity":         func(a *AuthorizedAction) { a.Quantity = 2 },
		"amount":           func(a *AuthorizedAction) { a.Amount = 12001 },
		"currency":         func(a *AuthorizedAction) { a.Currency = Currency("EUR") },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			a := baseAction()
			mutate(&a)
			if ComputeActionFingerprint(a) == base {
				t.Fatalf("changing %s did not change the fingerprint", name)
			}
		})
	}
}

func TestNewFingerprintValidatesLength(t *testing.T) {
	if _, err := NewFingerprint(make([]byte, 31)); err == nil {
		t.Error("31-byte digest must be rejected")
	}
	raw := make([]byte, 32)
	raw[0] = 0xAB
	f, err := NewFingerprint(raw)
	if err != nil {
		t.Fatalf("32-byte digest rejected: %v", err)
	}
	if f[0] != 0xAB {
		t.Fatal("fingerprint bytes not copied")
	}
}
