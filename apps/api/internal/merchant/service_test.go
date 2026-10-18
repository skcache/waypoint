package merchant_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

const (
	testTrace = "11111111-2222-4333-8444-555555555555"
	mugPrice  = int64(1234) // maple-mug, seeded by SeedProducts
)

func newOrder(t *testing.T, store *merchant.Store, svc *merchant.Service, qty int) merchant.Order {
	t.Helper()
	order, err := svc.Checkout(context.Background(), testTrace, "maple-mug", qty)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	return order
}

// approve drives the order to APPROVED via a provider observation.
func approve(t *testing.T, svc *merchant.Service, orderID string) {
	t.Helper()
	if _, err := svc.Observe(context.Background(), orderID); err != nil {
		t.Fatalf("observe (approve): %v", err)
	}
}

func detailFor(t *testing.T, store *merchant.Store, orderID string) merchant.OrderDetail {
	t.Helper()
	d, err := store.OrderDetail(context.Background(), orderID)
	if err != nil {
		t.Fatalf("order detail: %v", err)
	}
	return d
}

func TestCheckoutCommitsDurablyBeforeProviderCall(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")

	order, err := svc.Checkout(context.Background(), testTrace, "maple-mug", 1)
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if order.Status != merchant.StatusBuyerApprovalPending {
		t.Fatalf("status = %s, want BUYER_APPROVAL_PENDING", order.Status)
	}
	if order.TotalMinor != mugPrice || order.Currency != "USD" {
		t.Fatalf("trusted total = %d %s, want %d USD", order.TotalMinor, order.Currency, mugPrice)
	}
	if len(fake.createCalls) != 1 {
		t.Fatalf("provider create calls = %d, want 1", len(fake.createCalls))
	}
	// The amount sent is the canonical two-decimal decimal string, derived
	// server-side from the catalog.
	if got := fake.createCalls[0].AmountValue; got != "12.34" {
		t.Fatalf("amount value sent = %q, want %q", got, "12.34")
	}

	d := detailFor(t, store, order.ID)
	if len(d.Operations) != 1 || d.Operations[0].Type != merchant.OpCreateOrder {
		t.Fatalf("operations = %+v", d.Operations)
	}
	op := d.Operations[0]
	if op.Status != merchant.OpStatusSucceeded || op.ProviderOrderID != "ORD-TEST-1" {
		t.Fatalf("operation = %+v", op)
	}
	// The request id that the provider saw is exactly the one committed
	// before the outbound call.
	if fake.createIDs[0] != op.ProviderRequestID {
		t.Fatalf("provider saw request id %q, durable op has %q", fake.createIDs[0], op.ProviderRequestID)
	}
	if len(d.Events) < 2 {
		t.Fatalf("expected ORDER_CREATED + PROVIDER_ORDER_CREATED events, got %d", len(d.Events))
	}
}

func TestCheckoutF1FaultProducesRealRejectionAndIncident(t *testing.T) {
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

func TestCaptureRefusedBeforeApprovalNothingSent(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{getFn: func(orderID string) (*paypal.Order, error) {
		return &paypal.Order{ID: orderID, Status: paypal.OrderStatusCreated}, nil
	}}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)

	_, err := svc.Capture(context.Background(), order.ID)
	if !errors.Is(err, merchant.ErrConflict) {
		t.Fatalf("err = %v, want conflict", err)
	}
	if len(fake.captureCalls) != 0 {
		t.Fatalf("capture was sent %d times before approval; want 0", len(fake.captureCalls))
	}
	if got := detailFor(t, store, order.ID).Order.Status; got != merchant.StatusBuyerApprovalPending {
		t.Fatalf("order status = %s; a refused capture must not change state", got)
	}
}

func TestCaptureHappyPathSealsProviderEvidence(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			return completedOrder(orderID, "CAP-TEST-1", "USD", "12.34"), nil
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)

	updated, err := svc.Capture(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if updated.Status != merchant.StatusCapturedVerified {
		t.Fatalf("status = %s, want CAPTURED_VERIFIED", updated.Status)
	}
	d := detailFor(t, store, order.ID)
	var captureOp *merchant.Operation
	for i := range d.Operations {
		if d.Operations[i].Type == merchant.OpCapture {
			captureOp = &d.Operations[i]
		}
	}
	if captureOp == nil || captureOp.Status != merchant.OpStatusSucceeded || captureOp.ProviderCaptureID != "CAP-TEST-1" {
		t.Fatalf("capture operation = %+v", captureOp)
	}
	// The provider saw exactly the request id that was durably committed.
	if fake.captureCalls[0] != captureOp.ProviderRequestID {
		t.Fatalf("provider saw %q, durable op has %q", fake.captureCalls[0], captureOp.ProviderRequestID)
	}
}

func TestCaptureEvidenceMismatchStaysUnknown(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			// Provider claims COMPLETED but with the WRONG amount: never trust it.
			return completedOrder(orderID, "CAP-TEST-WRONG", "USD", "99.99"), nil
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)

	_, err := svc.Capture(context.Background(), order.ID)
	var unknown *merchant.UnknownCaptureError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want UnknownCaptureError", err)
	}
	updated := detailFor(t, store, order.ID).Order
	if updated.Status != merchant.StatusCaptureOutcomeUnknown {
		t.Fatalf("status = %s, want CAPTURE_OUTCOME_UNKNOWN", updated.Status)
	}
}

func TestAmbiguousCaptureRetryReusesOriginalIdentity(t *testing.T) {
	store := newFixtureStore(t)
	calls := 0
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			calls++
			if calls == 1 {
				// Ambiguous: the provider may or may not have captured.
				return nil, &paypal.APIError{StatusCode: 503, Name: "SERVICE_UNAVAILABLE"}
			}
			return completedOrder(orderID, "CAP-TEST-RETRY", "USD", "12.34"), nil
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)

	if _, err := svc.Capture(context.Background(), order.ID); err == nil {
		t.Fatal("first capture unexpectedly succeeded")
	}
	if got := detailFor(t, store, order.ID).Order.Status; got != merchant.StatusCaptureOutcomeUnknown {
		t.Fatalf("status after ambiguous capture = %s", got)
	}

	updated, err := svc.Capture(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("retry capture: %v", err)
	}
	if updated.Status != merchant.StatusCapturedVerified {
		t.Fatalf("status after retry = %s", updated.Status)
	}
	// Identity rule: BOTH outbound capture calls used the same request id,
	// and exactly one capture operation row exists.
	if len(fake.captureCalls) != 2 || fake.captureCalls[0] != fake.captureCalls[1] {
		t.Fatalf("capture request ids = %v; a retry must reuse the original id", fake.captureCalls)
	}
	d := detailFor(t, store, order.ID)
	captureOps := 0
	for _, op := range d.Operations {
		if op.Type == merchant.OpCapture {
			captureOps++
			if op.AttemptCount != 2 {
				t.Fatalf("attempt count = %d, want 2", op.AttemptCount)
			}
		}
	}
	if captureOps != 1 {
		t.Fatalf("capture operations = %d, want exactly 1", captureOps)
	}
}

func TestConcurrentCaptureAttemptsCreateExactlyOneOperation(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(_ string, _ string) (*paypal.Order, error) {
			return nil, &paypal.APIError{StatusCode: 503, Name: "SERVICE_UNAVAILABLE"}
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.Capture(context.Background(), order.ID)
		}()
	}
	wg.Wait()

	d := detailFor(t, store, order.ID)
	captureOps := 0
	for _, op := range d.Operations {
		if op.Type == merchant.OpCapture {
			captureOps++
		}
	}
	if captureOps != 1 {
		t.Fatalf("capture operations after 5 concurrent attempts = %d, want exactly 1", captureOps)
	}
	// Every outbound capture call (however many races got through the retry
	// path) reused the ONE durable request id.
	cap := d.Operations[len(d.Operations)-1]
	for _, id := range fake.captureCalls {
		if id != cap.ProviderRequestID {
			t.Fatalf("outbound capture used %q; the durable identity is %q", id, cap.ProviderRequestID)
		}
	}
}

func TestCreateAmbiguityPreservesIdentityAndResolves(t *testing.T) {
	store := newFixtureStore(t)
	calls := 0
	fake := &fakePayPal{
		createFn: func(in paypal.CreateOrderInput, _ string) (*paypal.Order, error) {
			calls++
			if calls == 1 {
				return nil, &paypal.APIError{StatusCode: 503, Name: "SERVICE_UNAVAILABLE"}
			}
			// The idempotent re-send resolves to the original order.
			return &paypal.Order{
				ID:     "ORD-TEST-RESOLVED",
				Status: paypal.OrderStatusCreated,
				PurchaseUnits: []paypal.PurchaseUnit{{
					Amount: paypal.Money{CurrencyCode: in.CurrencyCode, Value: in.AmountValue},
				}},
			}, nil
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")

	_, err := svc.Checkout(context.Background(), testTrace, "maple-mug", 1)
	if err == nil {
		t.Fatal("ambiguous create unexpectedly succeeded")
	}

	// Find the order left in ORDER_CREATED with an UNKNOWN create op.
	orders, err := store.ListOrders(context.Background(), 10)
	if err != nil || len(orders) != 1 {
		t.Fatalf("orders = %v (err %v)", orders, err)
	}
	orderID := orders[0].ID
	if orders[0].Status != merchant.StatusOrderCreated {
		t.Fatalf("status = %s, want ORDER_CREATED (never failed)", orders[0].Status)
	}

	resolved, err := svc.ResolveCreate(context.Background(), orderID)
	if err != nil {
		t.Fatalf("resolve create: %v", err)
	}
	if resolved.Status != merchant.StatusBuyerApprovalPending || resolved.ProviderOrderID != "ORD-TEST-RESOLVED" {
		t.Fatalf("resolved order = %+v", resolved)
	}
	if len(fake.createIDs) != 2 || fake.createIDs[0] != fake.createIDs[1] {
		t.Fatalf("create request ids = %v; resolution must reuse the original", fake.createIDs)
	}
}

// d0 returns the id of the only order (helper convenience).
func d0(t *testing.T, store *merchant.Store) string {
	t.Helper()
	orders, err := store.ListOrders(context.Background(), 10)
	if err != nil || len(orders) == 0 {
		t.Fatalf("list orders: %v (%d)", err, len(orders))
	}
	return orders[0].ID
}

func TestObserveApprovalIsForwardOnly(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)

	if got := detailFor(t, store, order.ID).Order.Status; got != merchant.StatusApproved {
		t.Fatalf("status = %s, want APPROVED", got)
	}
	// A stale CREATED observation must not roll the approval back.
	fake.getFn = func(orderID string) (*paypal.Order, error) {
		return &paypal.Order{ID: orderID, Status: paypal.OrderStatusCreated}, nil
	}
	if _, err := svc.Observe(context.Background(), order.ID); err != nil {
		t.Fatalf("observe (stale): %v", err)
	}
	if got := detailFor(t, store, order.ID).Order.Status; got != merchant.StatusApproved {
		t.Fatalf("status after stale observation = %s; approval is forward-only", got)
	}
}

func TestRecoverySealsExistingProviderCaptureWithoutRecapturing(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(_ string, _ string) (*paypal.Order, error) {
			return nil, &paypal.APIError{StatusCode: 503, Name: "SERVICE_UNAVAILABLE"}
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)

	if _, err := svc.Capture(context.Background(), order.ID); err == nil {
		t.Fatal("ambiguous capture unexpectedly succeeded")
	}
	before := len(fake.captureCalls)

	// The provider now reveals the capture actually completed.
	fake.getFn = func(orderID string) (*paypal.Order, error) {
		return completedOrder(orderID, "CAP-TEST-RECOVERED", "USD", "12.34"), nil
	}
	updated, err := svc.Reconcile(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if updated.Status != merchant.StatusCapturedVerified {
		t.Fatalf("status = %s, want CAPTURED_VERIFIED", updated.Status)
	}
	if len(fake.captureCalls) != before {
		t.Fatalf("reconciliation sent %d additional capture calls; it must send none", len(fake.captureCalls)-before)
	}
}

func TestReconcileResolvesNoEffectAttemptBackToApproved(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(_ string, _ string) (*paypal.Order, error) {
			return nil, &paypal.APIError{StatusCode: 503, Name: "SERVICE_UNAVAILABLE"}
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)
	if _, err := svc.Capture(context.Background(), order.ID); err == nil {
		t.Fatal("ambiguous capture unexpectedly succeeded")
	}

	// The provider definitively shows no capture and a still-eligible order:
	// the unresolved attempt had no effect.
	updated, err := svc.Reconcile(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if updated.Status != merchant.StatusApproved {
		t.Fatalf("status = %s, want APPROVED", updated.Status)
	}
	d := detailFor(t, store, order.ID)
	for _, op := range d.Operations {
		if op.Type == merchant.OpCapture && op.Status != merchant.OpStatusRejected {
			t.Fatalf("capture op = %+v, want REJECTED", op)
		}
	}
}
