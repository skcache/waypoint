//go:build fault_reproduction

// PRE-REPAIR REPRODUCTION TESTS.
//
// These tests assert the behavior of the PINNED BASE revision: the seeded F1
// fault really malforms the provider payload, the provider rejects it, and the
// incident artifact carries the provider diagnostics. They are excluded from
// the candidate gate by the build tag because a genuine repair MUST remove the
// behavior they assert. Run them against a base worktree:
//
//	go test -tags fault_reproduction ./internal/merchant -run TestF1DefectReproduces
package merchant_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

func TestF1DefectReproducesOnThePinnedRevision(t *testing.T) {
	store := newFixtureStore(t)
	incidentDir := t.TempDir()

	// The fake stands in for the real provider save for one thing: it
	// rejects the malformed payload exactly like PayPal does. The live M1
	// acceptance run exercises the same path against the real sandbox.
	fake := &fakePayPal{
		createFn: func(in paypal.CreateOrderInput, _ string) (*paypal.Order, error) {
			return nil, &paypal.APIError{
				StatusCode: 400,
				Name:       "INVALID_REQUEST",
				Message:    "Currency amount must be a non-negative number with at most 2 decimal places",
				DebugID:    "debug-test-f1",
			}
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultF1, incidentDir)

	_, err := svc.Checkout(context.Background(), testTrace, "maple-mug", 1)
	var rejected *merchant.CreateRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("err = %v, want CreateRejectedError", err)
	}
	if rejected.Order.Status != merchant.StatusFailedDefinite {
		t.Fatalf("order status = %s, want FAILED_DEFINITE", rejected.Order.Status)
	}

	// The injected defect must really malform the payload: six decimals.
	if got := fake.createCalls[0].AmountValue; got != "12.340000" {
		t.Fatalf("fault F1 payload amount = %q, want %q", got, "12.340000")
	}

	// The incident artifact exists, is redacted, and carries the provider's
	// own diagnostics plus explicit fault provenance.
	path := filepath.Join(incidentDir, rejected.Order.ID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read incident artifact: %v", err)
	}
	var inc merchant.Incident
	if err := json.Unmarshal(raw, &inc); err != nil {
		t.Fatalf("parse incident artifact: %v", err)
	}
	if inc.FaultMode != merchant.FaultF1 || inc.EvidenceTier != merchant.TierFault {
		t.Fatalf("incident fault/tier = %s/%s", inc.FaultMode, inc.EvidenceTier)
	}
	if inc.ProviderObservation["provider_http_status"] != float64(400) {
		t.Fatalf("provider observation = %+v", inc.ProviderObservation)
	}
	if inc.ProviderObservation["provider_debug_id"] != "debug-test-f1" {
		t.Fatalf("provider observation = %+v", inc.ProviderObservation)
	}
	if inc.FailingOperation.Status != merchant.OpStatusRejected {
		t.Fatalf("failing operation = %+v", inc.FailingOperation)
	}

	// Sanitization sweep: no credential-ish material anywhere in the artifact.
	for _, banned := range []string{"client_secret", "CLIENT_SECRET", "Bearer ", "access_token"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("incident artifact contains %q", banned)
		}
	}

	// The journal records the fault marker with tier F.
	var foundFault bool
	for _, e := range detailFor(t, store, rejected.Order.ID).Events {
		if e.Source == "FAULT" && e.EventType == "FAULT_INJECTED" && e.Tier == merchant.TierFault {
			foundFault = true
		}
	}
	if !foundFault {
		t.Fatal("journal is missing the FAULT_INJECTED (tier F) marker")
	}
}
