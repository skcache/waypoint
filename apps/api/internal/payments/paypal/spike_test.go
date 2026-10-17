package paypal

// Mock-server tests for the spike flows: order creation, capture
// preconditions, provider-confirmed capture evidence, and unknown-outcome
// handling.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newSpikeStore(t *testing.T) FileRecordStore {
	t.Helper()
	return FileRecordStore{Dir: filepath.Join(t.TempDir(), "records")}
}

func mustCreateRecord(t *testing.T, c *Client, store FileRecordStore) Record {
	t.Helper()
	rec, err := CreateSpikeOrder(context.Background(), c, store, "req-create-1")
	if err != nil {
		t.Fatalf("CreateSpikeOrder: %v", err)
	}
	return rec
}

func TestCreateSpikeOrderPersistsRecord(t *testing.T) {
	_, c := newFake(t)
	store := newSpikeStore(t)
	rec, err := CreateSpikeOrder(context.Background(), c, store, "req-create-1")
	if err != nil {
		t.Fatalf("CreateSpikeOrder: %v", err)
	}
	if rec.OrderID != "ORDERUNIT001" || rec.State != StateAwaitingApproval || rec.ProviderStatus != "CREATED" {
		t.Fatalf("record = %+v", rec)
	}
	if rec.AmountMinor != 100 || rec.AmountValue != "1.00" || rec.Currency != "USD" || rec.Environment != EnvironmentSandbox {
		t.Fatalf("record money/environment = %+v", rec)
	}
	if !strings.Contains(rec.ApprovalURL, "checkoutnow") {
		t.Fatalf("approval URL = %q", rec.ApprovalURL)
	}
	loaded, err := store.Load("ORDERUNIT001")
	if err != nil || loaded.OrderID != rec.OrderID || loaded.CreateRequestID != "req-create-1" {
		t.Fatalf("loaded = %+v, %v", loaded, err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "ORDERUNIT001.json")); err != nil {
		t.Fatalf("record file: %v", err)
	}
}

func TestCreateSpikeOrderRejectsUnexpectedStatus(t *testing.T) {
	f, c := newFake(t)
	f.create = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"id": "ORDERUNIT001", "status": "APPROVED"})
	}
	store := newSpikeStore(t)
	if _, err := CreateSpikeOrder(context.Background(), c, store, "req-1"); err == nil {
		t.Fatal("unexpected create status accepted")
	}
	if _, err := store.Latest(); err == nil {
		t.Fatal("a refused create must not leave a record")
	}
}

func TestCaptureRefusedWithoutApproval(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	mustCreateRecord(t, c, store)

	f.get = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, orderJSON("ORDERUNIT001", "CREATED", "1.00"))
	}
	_, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-1")
	if err == nil {
		t.Fatal("capture without buyer approval succeeded")
	}
	if !strings.Contains(err.Error(), "approval") {
		t.Fatalf("err = %v, want approval refusal", err)
	}
	if n := len(f.callsTo("/v2/checkout/orders/ORDERUNIT001/capture")); n != 0 {
		t.Fatalf("capture request was sent despite the refusal (%d calls)", n)
	}
	after, err := store.Load("ORDERUNIT001")
	if err != nil || after.State != StateAwaitingApproval {
		t.Fatalf("state after refusal = %+v, %v", after, err)
	}
}

func TestCaptureRefusedOnAmountMismatch(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	mustCreateRecord(t, c, store)

	f.get = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, orderJSON("ORDERUNIT001", "APPROVED", "2.00"))
	}
	if _, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-1"); err == nil {
		t.Fatal("capture succeeded despite a provider amount mismatch")
	}
	if n := len(f.callsTo("/v2/checkout/orders/ORDERUNIT001/capture")); n != 0 {
		t.Fatalf("capture sent despite the amount mismatch (%d)", n)
	}
}

func TestCaptureFlowVerifiesOutcome(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	mustCreateRecord(t, c, store)

	ev, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-1")
	if err != nil {
		t.Fatalf("CaptureSpikeOrder: %v", err)
	}
	if ev.CaptureID != "CAPTUREUNIT001" || ev.Status != "COMPLETED" || ev.Currency != "USD" || ev.AmountValue != "1.00" || ev.PayerID != "PAYERUNIT1" {
		t.Fatalf("evidence = %+v", ev)
	}
	calls := f.callsTo("/v2/checkout/orders/ORDERUNIT001/capture")
	if len(calls) != 1 || calls[0].RequestID != "req-cap-1" {
		t.Fatalf("capture calls = %+v", calls)
	}
	if calls[0].ContentType != "application/json" || calls[0].Body != "{}" {
		t.Fatalf("empty-body POST must still be JSON (415 trap): content-type=%q body=%q", calls[0].ContentType, calls[0].Body)
	}
	rec, err := store.Load("ORDERUNIT001")
	if err != nil || rec.State != StateCapturedVerified || rec.CaptureID != "CAPTUREUNIT001" || rec.CaptureRequestID != "req-cap-1" {
		t.Fatalf("record = %+v, %v", rec, err)
	}
	if _, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-2"); err == nil {
		t.Fatal("a second capture of a verified order succeeded")
	}
}

func TestCaptureUnknownOnServerErrorReusesRequestID(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	mustCreateRecord(t, c, store)

	f.capture = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"name": "INTERNAL_SERVER_ERROR"})
	}
	_, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-1")
	var unknown *UnknownOutcomeError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want *UnknownOutcomeError", err)
	}
	rec, err := store.Load("ORDERUNIT001")
	if err != nil || rec.State != StatePaymentUnknown || rec.CaptureRequestID != "req-cap-1" {
		t.Fatalf("record = %+v, %v", rec, err)
	}

	// The operator retries with a FRESH candidate id, but the persisted
	// identity must win: same request id means the provider returns the
	// original result instead of capturing twice.
	if _, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-2"); err == nil {
		t.Fatal("retry unexpectedly succeeded (server still 500s)")
	}
	calls := f.callsTo("/v2/checkout/orders/ORDERUNIT001/capture")
	if len(calls) != 2 || calls[1].RequestID != "req-cap-1" {
		t.Fatalf("retry did not reuse the original request id: %+v", calls)
	}
}

func TestCaptureTimeoutIsUnknown(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	mustCreateRecord(t, c, store)

	f.capture = slowHandler(2*time.Second, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, orderJSON("ORDERUNIT001", "COMPLETED", "1.00"))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := CaptureSpikeOrder(ctx, c, store, "ORDERUNIT001", "req-cap-1")
	var unknown *UnknownOutcomeError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want PAYMENT_OUTCOME_UNKNOWN", err)
	}
	if !strings.Contains(err.Error(), "PAYMENT_OUTCOME_UNKNOWN") {
		t.Fatalf("err text = %v", err)
	}
	rec, err := store.Load("ORDERUNIT001")
	if err != nil || rec.State != StatePaymentUnknown || rec.CaptureRequestID != "req-cap-1" {
		t.Fatalf("record = %+v, %v", rec, err)
	}
}

func TestCaptureDefiniteRejectionUsesFreshIdentityOnRetry(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	mustCreateRecord(t, c, store)

	f.capture = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"name": "UNPROCESSABLE_ENTITY"})
	}
	_, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-1")
	if err == nil {
		t.Fatal("a 422 capture was reported as success")
	}
	var unknown *UnknownOutcomeError
	if errors.As(err, &unknown) {
		t.Fatal("a definite 422 must not be reported as an unknown outcome")
	}
	rec, err := store.Load("ORDERUNIT001")
	if err != nil || rec.State != StateFailedDefinite {
		t.Fatalf("record = %+v, %v", rec, err)
	}

	// A confirmed-rejected request id is NOT reused on a later attempt.
	if _, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-2"); err == nil {
		t.Fatal("second attempt unexpectedly succeeded")
	}
	calls := f.callsTo("/v2/checkout/orders/ORDERUNIT001/capture")
	if len(calls) != 2 || calls[1].RequestID != "req-cap-2" {
		t.Fatalf("definite-failure retry should use a fresh id: %+v", calls)
	}
}

func TestCaptureWrongAmountNotVerified(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	mustCreateRecord(t, c, store)

	f.capture = func(w http.ResponseWriter, r *http.Request) {
		body := orderJSON("ORDERUNIT001", "COMPLETED", "2.00")
		body["purchase_units"] = []map[string]any{{
			"amount": map[string]any{"currency_code": "USD", "value": "2.00"},
			"payments": map[string]any{"captures": []map[string]any{{
				"id": "CAPTUREUNIT001", "status": "COMPLETED",
				"amount": map[string]any{"currency_code": "USD", "value": "2.00"},
			}}},
		}}
		writeJSON(w, http.StatusCreated, body)
	}
	_, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-1")
	var unknown *UnknownOutcomeError
	if !errors.As(err, &unknown) || !strings.Contains(err.Error(), "does not match expected") {
		t.Fatalf("err = %v, want an amount-mismatch unknown outcome", err)
	}
	rec, err := store.Load("ORDERUNIT001")
	if err != nil || rec.State != StatePaymentUnknown || rec.CaptureID != "" {
		t.Fatalf("record = %+v, %v", rec, err)
	}
}

func TestCapturePendingNotConfirmed(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	mustCreateRecord(t, c, store)

	f.capture = func(w http.ResponseWriter, r *http.Request) {
		body := orderJSON("ORDERUNIT001", "COMPLETED", "1.00")
		body["purchase_units"] = []map[string]any{{
			"amount": map[string]any{"currency_code": "USD", "value": "1.00"},
			"payments": map[string]any{"captures": []map[string]any{{
				"id": "CAPTUREUNIT001", "status": "PENDING",
				"amount": map[string]any{"currency_code": "USD", "value": "1.00"},
			}}},
		}}
		writeJSON(w, http.StatusCreated, body)
	}
	_, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-1")
	var unknown *UnknownOutcomeError
	if !errors.As(err, &unknown) || !strings.Contains(err.Error(), "PENDING") {
		t.Fatalf("err = %v, want a pending-capture unknown outcome", err)
	}
}

func TestVerifyCaptureReadyTable(t *testing.T) {
	rec := Record{OrderID: "ORDERUNIT001", Currency: "USD", AmountValue: "1.00"}
	approvedPU := []PurchaseUnit{{Amount: Money{CurrencyCode: "USD", Value: "1.00"}}}
	cases := []struct {
		name  string
		order *Order
		ok    bool
	}{
		{"approved with matching amount", &Order{ID: "ORDERUNIT001", Status: "APPROVED", PurchaseUnits: approvedPU}, true},
		{"created means no approval yet", &Order{ID: "ORDERUNIT001", Status: "CREATED", PurchaseUnits: approvedPU}, false},
		{"completed refuses recapture", &Order{ID: "ORDERUNIT001", Status: "COMPLETED", PurchaseUnits: approvedPU}, false},
		{"voided refuses", &Order{ID: "ORDERUNIT001", Status: "VOIDED", PurchaseUnits: approvedPU}, false},
		{"identity mismatch refuses", &Order{ID: "ORDERUNIT002", Status: "APPROVED", PurchaseUnits: approvedPU}, false},
		{"amount mismatch refuses", &Order{ID: "ORDERUNIT001", Status: "APPROVED", PurchaseUnits: []PurchaseUnit{{Amount: Money{CurrencyCode: "USD", Value: "2.00"}}}}, false},
		{"missing purchase units refuses", &Order{ID: "ORDERUNIT001", Status: "APPROVED"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyCaptureReady(tc.order, rec)
			if (err == nil) != tc.ok {
				t.Fatalf("VerifyCaptureReady = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestRecordStoreRoundTripLatestAndValidation(t *testing.T) {
	store := newSpikeStore(t)
	older := Record{OrderID: "ORDEROLD001", State: StateAwaitingApproval, UpdatedAt: time.Now().Add(-time.Hour)}
	newer := Record{OrderID: "ORDERNEW001", State: StateCapturedVerified, UpdatedAt: time.Now()}
	if err := store.Save(older); err != nil {
		t.Fatalf("Save older: %v", err)
	}
	if err := store.Save(newer); err != nil {
		t.Fatalf("Save newer: %v", err)
	}
	latest, err := store.Latest()
	if err != nil || latest.OrderID != "ORDERNEW001" {
		t.Fatalf("Latest = %+v, %v", latest, err)
	}
	if err := store.Save(Record{OrderID: "../escape"}); err == nil {
		t.Fatal("path-traversal order id accepted")
	}
	if _, err := store.Load("bad id!"); err == nil {
		t.Fatal("invalid order id accepted by Load")
	}
}

func TestFormatMinorUSD(t *testing.T) {
	cases := map[int64]string{0: "0.00", 5: "0.05", 100: "1.00", 12345: "123.45", 999999999: "9999999.99", -5: "-0.05"}
	for in, want := range cases {
		if got := FormatMinorUSD(in); got != want {
			t.Errorf("FormatMinorUSD(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestNewRequestIDShape(t *testing.T) {
	id, err := NewRequestID()
	if err != nil {
		t.Fatalf("NewRequestID: %v", err)
	}
	if len(id) != 36 || id[14] != '4' {
		t.Fatalf("request id %q is not a UUIDv4", id)
	}
	id2, err := NewRequestID()
	if err != nil || id2 == id {
		t.Fatalf("request ids must differ: %q vs %q", id, id2)
	}
}

// ---------------------------------------------------------------------------
// Certification regressions: uncertainty identity, ambiguous 2xx responses,
// provider-grounded recovery, sticky verified state.
// ---------------------------------------------------------------------------

// completedOrderJSON builds a capture-response-shaped order carrying a
// COMPLETED capture (or an empty/absent capture resource when captureID or
// the callers' overrides say so).
func completedOrderJSON(captureID, amountValue string) map[string]any {
	body := orderJSON("ORDERUNIT001", "COMPLETED", amountValue)
	body["payer"] = map[string]any{"payer_id": "PAYERUNIT1"}
	captures := []map[string]any{{
		"id": captureID, "status": "COMPLETED",
		"amount": map[string]any{"currency_code": "USD", "value": amountValue},
	}}
	body["purchase_units"] = []map[string]any{{
		"amount":   map[string]any{"currency_code": "USD", "value": amountValue},
		"payments": map[string]any{"captures": captures},
	}}
	return body
}

// seedUncertainCapture drives a fresh record into PAYMENT_OUTCOME_UNKNOWN
// with capture request id "req-cap-1" (the 500 leaves it unresolved).
func seedUncertainCapture(t *testing.T, f *fakePayPal, c *Client, store FileRecordStore) { //nolint:unparam
	t.Helper()
	mustCreateRecord(t, c, store)
	f.capture = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"name": "INTERNAL_SERVER_ERROR"})
	}
	if _, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-1"); err == nil {
		t.Fatal("seed capture unexpectedly succeeded")
	}
	rec, err := store.Load("ORDERUNIT001")
	if err != nil || rec.State != StatePaymentUnknown || rec.CaptureRequestID != "req-cap-1" {
		t.Fatalf("seed record = %+v, %v", rec, err)
	}
}

func TestCaptureInvalid2xxResponsePreservesUncertainty(t *testing.T) {
	cases := []struct {
		name    string
		respond http.HandlerFunc
	}{
		{"missing order id", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusCreated, map[string]any{"status": "COMPLETED"})
		}},
		{"missing status", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusCreated, map[string]any{"id": "ORDERUNIT001"})
		}},
		{"malformed json", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("{not json"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, c := newFake(t)
			store := newSpikeStore(t)
			mustCreateRecord(t, c, store)
			f.capture = tc.respond

			_, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-1")
			var unknown *UnknownOutcomeError
			if !errors.As(err, &unknown) {
				t.Fatalf("err = %v, want *UnknownOutcomeError", err)
			}
			if !AmbiguousOutcome(unknown.Err) {
				t.Fatalf("underlying err %v must be ambiguous", unknown.Err)
			}
			rec, loadErr := store.Load("ORDERUNIT001")
			if loadErr != nil || rec.State != StatePaymentUnknown || rec.CaptureRequestID != "req-cap-1" {
				t.Fatalf("record = %+v, %v", rec, loadErr)
			}
		})
	}
}

func TestStatusRefreshPreservesUncertaintyUntilProviderResolves(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	seedUncertainCapture(t, f, c, store)

	// The provider has not executed the capture (still APPROVED). A status
	// refresh must NOT flip the record back into a capture-eligible state.
	f.get = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, orderJSON("ORDERUNIT001", "APPROVED", "1.00"))
	}
	if _, err := RetrieveSpikeOrder(context.Background(), c, store, "ORDERUNIT001"); err != nil {
		t.Fatalf("RetrieveSpikeOrder: %v", err)
	}
	mid, err := store.Load("ORDERUNIT001")
	if err != nil {
		t.Fatal(err)
	}
	if mid.State != StatePaymentUnknown {
		t.Fatalf("status refresh erased uncertainty: state = %s", mid.State)
	}
	if mid.CaptureRequestID != "req-cap-1" {
		t.Fatalf("capture request id lost: %q", mid.CaptureRequestID)
	}
	if mid.ProviderStatus != "APPROVED" {
		t.Fatalf("provider observation not recorded: %q", mid.ProviderStatus)
	}

	// The retry succeeds — on the wire it must carry the ORIGINAL request id
	// even though the operator passed a fresh candidate.
	f.capture = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, completedOrderJSON("CAPTUREUNIT001", "1.00"))
	}
	ev, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-2-candidate")
	if err != nil {
		t.Fatalf("CaptureSpikeOrder retry: %v", err)
	}
	if ev.CaptureID != "CAPTUREUNIT001" {
		t.Fatalf("evidence = %+v", ev)
	}
	calls := f.callsTo("/v2/checkout/orders/ORDERUNIT001/capture")
	if len(calls) != 2 || calls[0].RequestID != "req-cap-1" || calls[1].RequestID != "req-cap-1" {
		t.Fatalf("unresolved operation must keep its request id: %+v", calls)
	}
	final, err := store.Load("ORDERUNIT001")
	if err != nil || final.State != StateCapturedVerified || final.CaptureRequestID != "req-cap-1" {
		t.Fatalf("final record = %+v, %v", final, err)
	}
}

func TestStatusRecoversVerifiedCaptureFromProviderState(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	seedUncertainCapture(t, f, c, store)

	// The provider actually completed the capture; the response was lost.
	// A status refresh recovers the completion from full evidence.
	f.get = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, completedOrderJSON("CAPTUREUNIT001", "1.00"))
	}
	if _, err := RetrieveSpikeOrder(context.Background(), c, store, "ORDERUNIT001"); err != nil {
		t.Fatalf("RetrieveSpikeOrder: %v", err)
	}
	rec, err := store.Load("ORDERUNIT001")
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != StateCapturedVerified || rec.CaptureID != "CAPTUREUNIT001" {
		t.Fatalf("recovery failed: %+v", rec)
	}
	if rec.CaptureRequestID != "req-cap-1" {
		t.Fatalf("recovery must preserve the original request id: %q", rec.CaptureRequestID)
	}
	// Recovery never issues a capture request of its own.
	if n := len(f.callsTo("/v2/checkout/orders/ORDERUNIT001/capture")); n != 1 {
		t.Fatalf("status issued %d capture calls, want only the seeded one", n)
	}
	// And a follow-up capture attempt is refused outright.
	if _, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-new"); err == nil {
		t.Fatal("capture after recovery succeeded; it must refuse")
	}
}

func TestStatusDoesNotRecoverWithoutSufficientEvidence(t *testing.T) {
	cases := []struct {
		name    string
		respond http.HandlerFunc
	}{
		{"wrong capture amount", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, completedOrderJSON("CAPTUREUNIT001", "2.00"))
		}},
		{"missing capture id", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, completedOrderJSON("", "1.00"))
		}},
		{"no capture resource", func(w http.ResponseWriter, r *http.Request) {
			body := orderJSON("ORDERUNIT001", "COMPLETED", "1.00")
			body["payer"] = map[string]any{"payer_id": "PAYERUNIT1"}
			writeJSON(w, http.StatusOK, body)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, c := newFake(t)
			store := newSpikeStore(t)
			seedUncertainCapture(t, f, c, store)
			f.get = tc.respond

			if _, err := RetrieveSpikeOrder(context.Background(), c, store, "ORDERUNIT001"); err != nil {
				t.Fatalf("RetrieveSpikeOrder: %v", err)
			}
			rec, err := store.Load("ORDERUNIT001")
			if err != nil {
				t.Fatal(err)
			}
			if rec.State != StatePaymentUnknown {
				t.Fatalf("insufficient evidence must not certify completion: state = %s", rec.State)
			}
			if rec.CaptureID != "" {
				t.Fatalf("no capture id may be recorded without full evidence: %q", rec.CaptureID)
			}
			if rec.CaptureRequestID != "req-cap-1" {
				t.Fatalf("request id lost: %q", rec.CaptureRequestID)
			}
		})
	}
}

func TestStatusNeverDowngradesVerified(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	seedUncertainCapture(t, f, c, store)

	// Recover first.
	f.get = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, completedOrderJSON("CAPTUREUNIT001", "1.00"))
	}
	if _, err := RetrieveSpikeOrder(context.Background(), c, store, "ORDERUNIT001"); err != nil {
		t.Fatal(err)
	}

	// A later stale APPROVED observation must not downgrade the record.
	f.get = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, orderJSON("ORDERUNIT001", "APPROVED", "1.00"))
	}
	if _, err := RetrieveSpikeOrder(context.Background(), c, store, "ORDERUNIT001"); err != nil {
		t.Fatal(err)
	}
	rec, err := store.Load("ORDERUNIT001")
	if err != nil || rec.State != StateCapturedVerified || rec.CaptureID != "CAPTUREUNIT001" {
		t.Fatalf("stale observation downgraded verified state: %+v, %v", rec, err)
	}

	// Repeated COMPLETED status calls are stable too.
	f.get = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, completedOrderJSON("CAPTUREUNIT001", "1.00"))
	}
	for i := 0; i < 2; i++ {
		if _, err := RetrieveSpikeOrder(context.Background(), c, store, "ORDERUNIT001"); err != nil {
			t.Fatal(err)
		}
	}
	rec, err = store.Load("ORDERUNIT001")
	if err != nil || rec.State != StateCapturedVerified || rec.CaptureID != "CAPTUREUNIT001" || rec.CaptureRequestID != "req-cap-1" {
		t.Fatalf("repeated status calls drifted: %+v, %v", rec, err)
	}
}

func TestCaptureRefusedWhenUncertainRecordHasNoRequestID(t *testing.T) {
	f, c := newFake(t)
	store := newSpikeStore(t)
	now := time.Now().UTC()
	if err := store.Save(Record{
		Provider: "paypal", Environment: EnvironmentSandbox,
		OrderID: "ORDERUNIT001", Currency: "USD", AmountMinor: 100, AmountValue: "1.00",
		CreateRequestID: "req-create-1", State: StatePaymentUnknown, ProviderStatus: "APPROVED",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := CaptureSpikeOrder(context.Background(), c, store, "ORDERUNIT001", "req-cap-x")
	if err == nil || !strings.Contains(err.Error(), "no preserved request id") {
		t.Fatalf("err = %v, want a refusal about the missing request identity", err)
	}
	if n := len(f.callsTo("/v2/checkout/orders/ORDERUNIT001/capture")); n != 0 {
		t.Fatalf("capture request was sent despite the missing identity (%d calls)", n)
	}
}
