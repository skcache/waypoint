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

func baseContractTerms() ContractTerms {
	return ContractTerms{
		Principal:       testOwner,
		OriginalRequest: "Buy one quiet keyboard for office work under $150",
		Currency:        CurrencyUSD,
		MaxTotal:        15000,
		MaxQuantity:     1,
		Category:        "keyboard",
		Preferences:     []string{"quiet", "office use", "no gaming RGB"},
	}
}

func TestContractHashIsDeterministic(t *testing.T) {
	a := ComputeContractHash(baseContractTerms())
	b := ComputeContractHash(baseContractTerms())
	if a != b {
		t.Fatalf("same terms produced different hashes: %s vs %s", a, b)
	}
	if a.IsZero() {
		t.Fatal("contract hash must not be zero")
	}
	if len(a.String()) != len("sha256:")+64 {
		t.Fatalf("String() = %q", a.String())
	}
	terms := baseContractTerms()
	terms.Preferences = []string{"quiet", "office use", "no gaming RGB"}
	if ComputeContractHash(terms) != a {
		t.Fatal("equivalent rebuild produced a different hash")
	}
}

func TestContractHashCoversEveryAuthorizedTerm(t *testing.T) {
	base := ComputeContractHash(baseContractTerms())
	mutations := map[string]func(*ContractTerms){
		"principal":        func(tm *ContractTerms) { tm.Principal = testOther },
		"original request": func(tm *ContractTerms) { tm.OriginalRequest = "buy a loud keyboard" },
		"currency":         func(tm *ContractTerms) { tm.Currency = Currency("EUR") },
		"max total":        func(tm *ContractTerms) { tm.MaxTotal = 15100 },
		"max quantity":     func(tm *ContractTerms) { tm.MaxQuantity = 2 },
		"category":         func(tm *ContractTerms) { tm.Category = "mouse" },
		"preferences":      func(tm *ContractTerms) { tm.Preferences = append(tm.Preferences, "red") },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			terms := baseContractTerms()
			mutate(&terms)
			if ComputeContractHash(terms) == base {
				t.Fatalf("changing %s did not change the contract hash", name)
			}
		})
	}
}

func TestContractHashIgnoresPreferenceOrderingAndRepresentation(t *testing.T) {
	canonical := ComputeContractHash(baseContractTerms())

	// nil and empty preferences describe the same mandate.
	nilPrefs := baseContractTerms()
	nilPrefs.Preferences = nil
	emptyPrefs := baseContractTerms()
	emptyPrefs.Preferences = []string{}
	if ComputeContractHash(nilPrefs) != ComputeContractHash(emptyPrefs) {
		t.Fatal("nil and empty preferences must hash identically")
	}

	// Explicit reordering cannot matter.
	reversed := baseContractTerms()
	reversed.Preferences = []string{"no gaming RGB", "office use", "quiet"}
	if ComputeContractHash(reversed) != canonical {
		t.Fatal("reordered preferences changed the hash")
	}

	// The classic nondeterminism bug: preferences collected by ranging over a
	// map (random iteration order). Every collected order must agree.
	source := map[string]bool{"quiet": true, "office use": true, "no gaming RGB": true}
	for i := 0; i < 20; i++ {
		collected := baseContractTerms()
		collected.Preferences = collected.Preferences[:0]
		for k := range source {
			collected.Preferences = append(collected.Preferences, k)
		}
		if ComputeContractHash(collected) != canonical {
			t.Fatalf("map-iteration order leaked into the contract hash (iteration %d)", i)
		}
	}
}
