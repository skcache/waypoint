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
	"time"

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

// ambiguousCapture drives an order to the ambiguous-capture state using a
// provider that fails the ONE capture call with a 503, then returns up to the
// caller to decide what the provider reports on subsequent reads.
func ambiguousCapture(t *testing.T, store *merchant.Store, fake *fakePayPal) (merchant.Order, string) {
	t.Helper()
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)
	if _, err := svc.Capture(context.Background(), order.ID); err == nil {
		t.Fatal("ambiguous capture unexpectedly succeeded")
	}
	d := detailFor(t, store, order.ID)
	for _, op := range d.Operations {
		if op.Type == merchant.OpCapture {
			return order, op.ProviderRequestID
		}
	}
	t.Fatal("capture operation missing after ambiguous outcome")
	return merchant.Order{}, ""
}

// A provider order merely observed as APPROVED after an ambiguous capture is
// NOT evidence that the outbound mutation had no effect. The operation must
// stay unresolved with its request id preserved — never REJECTED, never
// released back to APPROVED (which would mint a fresh capture identity).
func TestReconcileKeepsUnknownWhenProviderShowsApprovedNoCapture(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(_ string, _ string) (*paypal.Order, error) {
			return nil, &paypal.APIError{StatusCode: 503, Name: "SERVICE_UNAVAILABLE"}
		},
	}
	order, requestID := ambiguousCapture(t, store, fake)
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")

	updated, err := svc.Reconcile(context.Background(), order.ID)
	if err == nil {
		t.Fatal("reconcile returned success after a single APPROVED observation; APPROVED must never resolve an ambiguous capture")
	}
	if updated.Status != merchant.StatusCaptureOutcomeUnknown {
		t.Fatalf("order status = %s, want CAPTURE_OUTCOME_UNKNOWN", updated.Status)
	}
	d := detailFor(t, store, order.ID)
	for _, op := range d.Operations {
		if op.Type != merchant.OpCapture {
			continue
		}
		if op.Status != merchant.OpStatusUnknown {
			t.Fatalf("capture op status = %s, want UNKNOWN (unresolved)", op.Status)
		}
		if op.ProviderRequestID != requestID {
			t.Fatalf("request id changed from %s to %s", requestID, op.ProviderRequestID)
		}
	}
	foundInconclusive := false
	for _, e := range d.Events {
		if e.EventType == "RECONCILIATION_INCONCLUSIVE" {
			foundInconclusive = true
		}
	}
	if !foundInconclusive {
		t.Fatal("journal must record RECONCILIATION_INCONCLUSIVE for the read that proved nothing")
	}
}

// A PENDING capture is not completion. Reconciliation must not seal it as
// verified and must not fail the order; it stays unresolved.
func TestReconcilePendingCaptureStaysUnresolved(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(_ string, _ string) (*paypal.Order, error) {
			return nil, &paypal.APIError{StatusCode: 503, Name: "SERVICE_UNAVAILABLE"}
		},
	}
	order, requestID := ambiguousCapture(t, store, fake)
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")

	// Only NOW does the provider reveal a capture resource — in PENDING state.
	fake.getFn = func(orderID string) (*paypal.Order, error) {
		return &paypal.Order{
			ID:     orderID,
			Status: paypal.OrderStatusCompleted,
			PurchaseUnits: []paypal.PurchaseUnit{{
				Amount: paypal.Money{CurrencyCode: "USD", Value: "12.34"},
				Payments: paypal.PaymentCollection{Captures: []paypal.Capture{{
					ID: "CAP-PENDING-1", Status: "PENDING",
					Amount: paypal.Money{CurrencyCode: "USD", Value: "12.34"},
				}}},
			}},
		}, nil
	}

	updated, err := svc.Reconcile(context.Background(), order.ID)
	if err == nil {
		t.Fatal("reconcile accepted a PENDING capture as resolution; only COMPLETED evidence may verify")
	}
	if updated.Status == merchant.StatusCapturedVerified {
		t.Fatal("a PENDING capture must never seal CAPTURED_VERIFIED")
	}
	d := detailFor(t, store, order.ID)
	foundPending := false
	for _, e := range d.Events {
		if e.EventType != "RECONCILIATION_INCONCLUSIVE" {
			continue
		}
		var det map[string]any
		if err := json.Unmarshal(e.Detail, &det); err == nil {
			if statuses, ok := det["capture_statuses"].([]any); ok {
				for _, s := range statuses {
					if s == "PENDING" {
						foundPending = true
					}
				}
			}
		}
	}
	if !foundPending {
		t.Fatal("journal must record the observed PENDING capture status")
	}
	for _, op := range d.Operations {
		if op.Type == merchant.OpCapture && op.ProviderRequestID != requestID {
			t.Fatalf("request id changed from %s to %s", requestID, op.ProviderRequestID)
		}
	}
}

// REGRESSION: an ambiguous capture followed by an APPROVED provider
// observation (no capture) must NOT mint a new capture identity. The only
// permitted mutation path is a same-id retry; the request id is permanent.
func TestAmbiguousCaptureCannotMintNewIdentity(t *testing.T) {
	store := newFixtureStore(t)
	captureAttempts := 0
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			captureAttempts++
			if captureAttempts == 1 {
				return nil, &paypal.APIError{StatusCode: 503, Name: "SERVICE_UNAVAILABLE"}
			}
			return completedOrder(orderID, "CAP-TEST-SAME-ID", "USD", "12.34"), nil
		},
	}
	order, requestID := ambiguousCapture(t, store, fake)
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")

	// Read-only reconciliation proves nothing and must not release the op.
	if _, err := svc.Reconcile(context.Background(), order.ID); err == nil {
		t.Fatal("reconcile unexpectedly resolved the ambiguous capture")
	}

	// Retrying now must reuse the ORIGINAL request id — never a new one.
	updated, err := svc.Capture(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("same-id retry capture: %v", err)
	}
	if updated.Status != merchant.StatusCapturedVerified {
		t.Fatalf("status = %s, want CAPTURED_VERIFIED after same-id retry", updated.Status)
	}
	if len(fake.captureCalls) != 2 {
		t.Fatalf("capture calls = %d, want 2", len(fake.captureCalls))
	}
	if fake.captureCalls[0] != fake.captureCalls[1] {
		t.Fatalf("capture identity changed across retry: %q then %q (a fresh identity must never be minted)", fake.captureCalls[0], fake.captureCalls[1])
	}
	if fake.captureCalls[1] != requestID {
		t.Fatalf("retry used %q, want the preserved request id %q", fake.captureCalls[1], requestID)
	}
	captureOps := 0
	for _, op := range detailFor(t, store, order.ID).Operations {
		if op.Type == merchant.OpCapture {
			captureOps++
			if op.ProviderRequestID != requestID {
				t.Fatalf("capture op carries %q, want %q", op.ProviderRequestID, requestID)
			}
		}
	}
	if captureOps != 1 {
		t.Fatalf("capture operations = %d; exactly one durable identity may exist", captureOps)
	}
}

// A crash can leave a capture attempt IN_FLIGHT (the durable begin committed,
// the provider call never observed). After restart, read-only reconciliation
// must demote the stale attempt to unresolved WITHOUT sending any capture,
// and later provider evidence must be sealable the same way.
func TestReconcileAfterRestartDemotesInFlightWithoutNewCapture(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)

	// Simulate the crash window: the durable begin committed, but the
	// outbound call and its classification never happened. TTL 0 records no
	// lease — a crashed owner that never got a lease is treated as abandoned.
	if _, err := store.InsertCaptureOp(context.Background(), order.ID, "req-crash-1", "fp-crash-1", "worker-crashed", 0); err != nil {
		t.Fatalf("simulate crash-window capture begin: %v", err)
	}
	if got := detailFor(t, store, order.ID).Order.Status; got != merchant.StatusCaptureInFlight {
		t.Fatalf("precondition: order status = %s, want CAPTURE_IN_FLIGHT", got)
	}

	updated, err := svc.Reconcile(context.Background(), order.ID)
	if err == nil {
		t.Fatal("reconcile of a stale IN_FLIGHT attempt with no capture evidence must stay unresolved")
	}
	if updated.Status != merchant.StatusCaptureOutcomeUnknown {
		t.Fatalf("status = %s, want CAPTURE_OUTCOME_UNKNOWN after restart demotion", updated.Status)
	}
	if len(fake.captureCalls) != 0 {
		t.Fatalf("reconciliation sent %d capture calls; it must send none", len(fake.captureCalls))
	}
	d := detailFor(t, store, order.ID)
	for _, op := range d.Operations {
		if op.Type != merchant.OpCapture {
			continue
		}
		if op.Status != merchant.OpStatusUnknown {
			t.Fatalf("op status = %s, want UNKNOWN", op.Status)
		}
		if op.ProviderRequestID != "req-crash-1" {
			t.Fatalf("request id = %q, want the preserved req-crash-1", op.ProviderRequestID)
		}
	}

	// The provider later reveals the original capture completed.
	fake.getFn = func(orderID string) (*paypal.Order, error) {
		return completedOrder(orderID, "CAP-TEST-CRASH-RECOVERED", "USD", "12.34"), nil
	}
	updated, err = svc.Reconcile(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("reconcile with completed evidence: %v", err)
	}
	if updated.Status != merchant.StatusCapturedVerified {
		t.Fatalf("status = %s, want CAPTURED_VERIFIED", updated.Status)
	}
	if len(fake.captureCalls) != 0 {
		t.Fatalf("recovery sent %d capture calls; it must send none", len(fake.captureCalls))
	}
}

// Same-id retry relies on the provider returning the ORIGINAL result for a
// repeated request id; that guarantee is time-bounded. Past the window the
// order escalates to manual reconciliation and nothing is sent.
func TestSameIDRetryWindowBoundRefusesStaleRetry(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(_ string, _ string) (*paypal.Order, error) {
			return nil, &paypal.APIError{StatusCode: 503, Name: "SERVICE_UNAVAILABLE"}
		},
	}
	order, requestID := ambiguousCapture(t, store, fake)
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	svc.SetSameIDRetryWindow(time.Hour)

	ctx := context.Background()
	if _, err := store.Pool().Exec(ctx, `
		UPDATE merchant_operations SET first_attempted_at = now() - interval '7 days'
		 WHERE order_id = $1::uuid AND operation_type = 'CAPTURE'`, order.ID); err != nil {
		t.Fatalf("backdate first attempt: %v", err)
	}

	_, err := svc.Capture(ctx, order.ID)
	if err == nil {
		t.Fatal("retry outside the same-id window must be refused")
	}
	if !strings.Contains(err.Error(), "window") {
		t.Fatalf("error = %q, want the same-id retry window refusal", err)
	}
	if len(fake.captureCalls) != 1 {
		t.Fatalf("capture calls = %d; a stale retry must send nothing", len(fake.captureCalls))
	}
	d := detailFor(t, store, order.ID)
	if d.Order.Status != merchant.StatusReconciliationRequired {
		t.Fatalf("status = %s, want RECONCILIATION_REQUIRED", d.Order.Status)
	}
	for _, op := range d.Operations {
		if op.Type == merchant.OpCapture {
			if op.Status != merchant.OpStatusUnknown {
				t.Fatalf("op status = %s, want UNKNOWN (identity preserved)", op.Status)
			}
			if op.ProviderRequestID != requestID {
				t.Fatalf("request id = %q, want %q", op.ProviderRequestID, requestID)
			}
		}
	}
}

// The stored CAPTURE fingerprint must equal an independent digest over the
// canonical outbound operation (scheme, local order, provider order, amount,
// currency, request id) and must materially depend on amount and currency.
func TestCaptureFingerprintMatchesCanonicalFinancialOperation(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			return completedOrder(orderID, "CAP-TEST-FP", "USD", "12.34"), nil
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)
	if _, err := svc.Capture(context.Background(), order.ID); err != nil {
		t.Fatalf("capture: %v", err)
	}

	d := detailFor(t, store, order.ID)
	var cap *merchant.Operation
	for i := range d.Operations {
		if d.Operations[i].Type == merchant.OpCapture {
			cap = &d.Operations[i]
		}
	}
	if cap == nil {
		t.Fatal("capture operation missing")
	}
	providerOrderID := d.Order.ProviderOrderID
	if want := canonicalFP("waypoint.merchant.capture.v1", order.ID, providerOrderID, "1234", "USD", cap.ProviderRequestID); cap.PayloadFingerprint != want {
		t.Fatalf("stored capture fingerprint %s does not match the canonical digest %s", cap.PayloadFingerprint, want)
	}
	if cap.PayloadFingerprint == canonicalFP("waypoint.merchant.capture.v1", order.ID, providerOrderID, "1235", "USD", cap.ProviderRequestID) {
		t.Fatal("capture fingerprint does not cover the amount")
	}
	if cap.PayloadFingerprint == canonicalFP("waypoint.merchant.capture.v1", order.ID, providerOrderID, "1234", "EUR", cap.ProviderRequestID) {
		t.Fatal("capture fingerprint does not cover the currency")
	}
}

// M2-P1 regression (sequential form): a recovery worker must NOT demote or
// retry an attempt whose owner lease is still live — only an expired lease
// (crashed owner) permits takeover, and takeover must keep the request id.
func TestReconcileRefusesLiveLeaseAndTakesOverExpiredLease(t *testing.T) {
	store := newFixtureStore(t)
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			return completedOrder(orderID, "CAP-TEST-LEASE", "USD", "12.34"), nil
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)

	// Worker A began a capture and is (conceptually) still running: the op is
	// IN_FLIGHT with a live lease.
	if _, err := store.InsertCaptureOp(context.Background(), order.ID, "req-lease-1", "fp-lease-1", "worker-A", 5*time.Minute); err != nil {
		t.Fatalf("simulate worker A capture begin: %v", err)
	}

	// Worker B (a different service instance) must refuse to touch it.
	svcB := merchant.NewService(store, fake, merchant.FaultNone, "")
	if _, err := svcB.Reconcile(context.Background(), order.ID); err == nil || !strings.Contains(err.Error(), "actively owned") {
		t.Fatalf("reconcile = %v, want refusal for a live lease", err)
	}
	if got := detailFor(t, store, order.ID).Order.Status; got != merchant.StatusCaptureInFlight {
		t.Fatalf("status = %s; a live-lease refusal must not change state", got)
	}
	if _, err := svcB.Capture(context.Background(), order.ID); err == nil || !strings.Contains(err.Error(), "actively owned") {
		t.Fatalf("capture = %v, want refusal for a live lease", err)
	}
	if len(fake.captureCalls) != 0 {
		t.Fatalf("capture calls = %d; nothing may be sent while a live owner exists", len(fake.captureCalls))
	}

	// The owner has now demonstrably stopped: its lease expired.
	ctx := context.Background()
	if _, err := store.Pool().Exec(ctx, `
		UPDATE merchant_operations SET lease_expires_at = now() - interval '1 second'
		 WHERE order_id = $1::uuid AND operation_type = 'CAPTURE'`, order.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	updated, err := svcB.Reconcile(ctx, order.ID)
	if err == nil {
		t.Fatal("reconcile after lease expiry must still be unresolved (no capture evidence yet)")
	}
	if updated.Status != merchant.StatusCaptureOutcomeUnknown {
		t.Fatalf("status = %s, want CAPTURE_OUTCOME_UNKNOWN after expired-lease demotion", updated.Status)
	}

	// Takeover sends the SAME request id, and the op now records worker B.
	updated, err = svcB.Capture(ctx, order.ID)
	if err != nil {
		t.Fatalf("takeover capture: %v", err)
	}
	if updated.Status != merchant.StatusCapturedVerified {
		t.Fatalf("status = %s, want CAPTURED_VERIFIED", updated.Status)
	}
	if len(fake.captureCalls) != 1 || fake.captureCalls[0] != "req-lease-1" {
		t.Fatalf("capture calls = %v, want exactly one send reusing req-lease-1", fake.captureCalls)
	}
	for _, op := range detailFor(t, store, order.ID).Operations {
		if op.Type == merchant.OpCapture && op.OwnerID != svcB.InstanceID() {
			t.Fatalf("owner after takeover = %q, want %q", op.OwnerID, svcB.InstanceID())
		}
	}
}

// M2-P1 regression (concurrent form, deterministic): while worker A's capture
// call is genuinely in flight (blocked inside the provider), worker B's
// reconcile and capture attempts must both be refused and send nothing;
// A then completes normally and seals the capture.
func TestRecoveryWorkerCannotRaceActiveCapture(t *testing.T) {
	store := newFixtureStore(t)
	started := make(chan struct{})
	release := make(chan struct{})
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			close(started)
			<-release
			return completedOrder(orderID, "CAP-TEST-RACE", "USD", "12.34"), nil
		},
	}
	svcA := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svcA, 1)
	approve(t, svcA, order.ID)

	type result struct {
		order merchant.Order
		err   error
	}
	done := make(chan result, 1)
	go func() {
		o, err := svcA.Capture(context.Background(), order.ID)
		done <- result{o, err}
	}()
	<-started // A is now inside the provider call; its lease is live.

	svcB := merchant.NewService(store, fake, merchant.FaultNone, "")
	if _, err := svcB.Reconcile(context.Background(), order.ID); err == nil || !strings.Contains(err.Error(), "actively owned") {
		t.Fatalf("reconcile during a live call = %v, want refusal", err)
	}
	if _, err := svcB.Capture(context.Background(), order.ID); err == nil || !strings.Contains(err.Error(), "actively owned") {
		t.Fatalf("capture during a live call = %v, want refusal", err)
	}
	if len(fake.captureCalls) != 1 {
		t.Fatalf("capture calls = %d; only worker A's call may exist", len(fake.captureCalls))
	}

	close(release)
	res := <-done
	if res.err != nil {
		t.Fatalf("worker A capture: %v", res.err)
	}
	if res.order.Status != merchant.StatusCapturedVerified {
		t.Fatalf("status = %s, want CAPTURED_VERIFIED", res.order.Status)
	}
}

// M2 review correction (fencing): a reconciler that inspected an EXPIRED
// lease must not be able to demote the operation once another worker has
// taken it over and is actively capturing. A lease timestamp alone is not a
// fencing token — the demotion must be conditioned on the fencing generation
// the reconciler actually inspected. Deterministic interleaving: every step
// is ordered by channels, no sleeps.
func TestStaleReconcilerCannotDemoteAfterTakeover(t *testing.T) {
	store := newFixtureStore(t)
	ctx := context.Background()

	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	startedB := make(chan struct{})
	releaseB := make(chan struct{})

	var mu sync.Mutex
	getCalls := 0
	fake := &fakePayPal{
		getFn: func(orderID string) (*paypal.Order, error) {
			mu.Lock()
			getCalls++
			n := getCalls
			mu.Unlock()
			// Call 1 is the approval observation. Call 2 is worker A's
			// reconciliation read: block it mid-flight.
			if n == 2 {
				close(startedA)
				<-releaseA
			}
			return approvedOrder(orderID, "USD", "12.34"), nil
		},
		captureFn: func(orderID, _ string) (*paypal.Order, error) {
			close(startedB)
			<-releaseB
			return completedOrder(orderID, "CAP-TEST-FENCE", "USD", "12.34"), nil
		},
	}

	svcA := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svcA, 1)
	approve(t, svcA, order.ID)

	// Worker A began a capture, its process died, and its lease has expired;
	// the operation row (generation 1) is preserved.
	if _, err := store.InsertCaptureOp(ctx, order.ID, "req-fence-1", "fp-fence-1", "worker-A", 5*time.Minute); err != nil {
		t.Fatalf("insert worker A capture begin: %v", err)
	}
	if _, err := store.Pool().Exec(ctx, `
		UPDATE merchant_operations SET lease_expires_at = now() - interval '1 second'
		 WHERE order_id = $1::uuid AND operation_type = 'CAPTURE'`, order.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	// A starts reconciling; its provider read blocks while it holds a stale
	// view of the operation (expired lease, generation 1).
	doneA := make(chan error, 1)
	go func() {
		_, err := svcA.Reconcile(ctx, order.ID)
		doneA <- err
	}()
	<-startedA

	// B reconciles (demotes the abandoned attempt) and takes over the SAME
	// operation identity, beginning a live capture under a fresh lease.
	svcB := merchant.NewService(store, fake, merchant.FaultNone, "")
	if _, err := svcB.Reconcile(ctx, order.ID); err == nil {
		t.Fatal("B's reconcile must report the outcome as still unresolved")
	}
	doneB := make(chan error, 1)
	go func() {
		_, err := svcB.Capture(ctx, order.ID)
		doneB <- err
	}()
	<-startedB // B is inside the provider call; its lease is live.

	// Release A: its stale demotion must be refused and must not touch B's
	// operation.
	close(releaseA)
	errA := <-doneA
	if errA == nil {
		t.Fatal("stale reconciler must not succeed after a takeover")
	}

	d := detailFor(t, store, order.ID)
	if d.Order.Status != merchant.StatusCaptureInFlight {
		t.Fatalf("order status = %s after stale reconcile (A err: %v); want CAPTURE_IN_FLIGHT (B's active attempt untouched)", d.Order.Status, errA)
	}
	for _, op := range d.Operations {
		if op.Type != merchant.OpCapture {
			continue
		}
		if op.Status != merchant.OpStatusInFlight {
			t.Fatalf("op status = %s (fence=%d owner=%q; A err: %v); B's attempt was overwritten", op.Status, op.Fence, op.OwnerID, errA)
		}
		if op.ProviderRequestID != "req-fence-1" {
			t.Fatalf("request id = %q, want the preserved req-fence-1", op.ProviderRequestID)
		}
		if op.OwnerID != svcB.InstanceID() {
			t.Fatalf("owner = %q, want worker B (%s); a stale worker must not reclaim ownership", op.OwnerID, svcB.InstanceID())
		}
	}

	// B completes normally with the same request id.
	close(releaseB)
	if err := <-doneB; err != nil {
		t.Fatalf("B capture: %v", err)
	}
	updated := detailFor(t, store, order.ID).Order
	if updated.Status != merchant.StatusCapturedVerified {
		t.Fatalf("final status = %s, want CAPTURED_VERIFIED", updated.Status)
	}
	if len(fake.captureCalls) != 1 || fake.captureCalls[0] != "req-fence-1" {
		t.Fatalf("capture calls = %v, want exactly one send reusing req-fence-1", fake.captureCalls)
	}
}
