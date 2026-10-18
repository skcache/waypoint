package merchant_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
)

// canonicalFP is an INDEPENDENT reimplementation of the fixture's payload
// fingerprint contract (sha256 over 0x1f-joined parts). The tests below pin
// the exact canonical part lists so a change to what the stored fingerprint
// covers is a conscious, reviewed change — not silent drift.
func canonicalFP(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(h[:])
}

// rawExec runs one statement expected to fail, and returns the error text.
func rawExecErr(t *testing.T, store *merchant.Store, sql string, args ...any) string {
	t.Helper()
	_, err := store.Pool().Exec(context.Background(), sql, args...)
	if err == nil {
		t.Fatalf("statement unexpectedly succeeded: %s", sql)
	}
	return err.Error()
}

func TestMerchantSchemaGuards(t *testing.T) {
	store := newFixtureStore(t)
	ctx := context.Background()

	order, _, err := store.CreateLocalOrder(ctx, testTrace, "maple-mug", 1, "req-id-1")
	if err != nil {
		t.Fatalf("create local order: %v", err)
	}

	t.Run("duplicate provider request id is rejected", func(t *testing.T) {
		msg := rawExecErr(t, store, `
			INSERT INTO merchant_operations (order_id, operation_type, provider_request_id, payload_fingerprint, status)
			VALUES ($1::uuid, 'CAPTURE', 'req-id-1', 'fp-x', 'IN_FLIGHT')`, order.ID)
		if !strings.Contains(msg, "merchant_operations_request_id_unique") {
			t.Fatalf("unexpected error: %s", msg)
		}
	})

	t.Run("at most one active capture per order", func(t *testing.T) {
		if _, err := store.Pool().Exec(ctx, `
			INSERT INTO merchant_operations (order_id, operation_type, provider_request_id, payload_fingerprint, status)
			VALUES ($1::uuid, 'CAPTURE', 'cap-a', 'fp', 'IN_FLIGHT')`, order.ID); err != nil {
			t.Fatalf("insert first capture op: %v", err)
		}
		// A second ALIVE operation for the same order violates the partial
		// unique index — concurrent attempts cannot both claim the right to
		// call the provider.
		msg := rawExecErr(t, store, `
			INSERT INTO merchant_operations (order_id, operation_type, provider_request_id, payload_fingerprint, status)
			VALUES ($1::uuid, 'CAPTURE', 'cap-b', 'fp', 'IN_FLIGHT')`, order.ID)
		if !strings.Contains(msg, "merchant_operations_one_active_capture") {
			t.Fatalf("unexpected error: %s", msg)
		}
		// Once the first attempt is resolved, a new attempt may begin.
		if _, err := store.Pool().Exec(ctx, `
			UPDATE merchant_operations SET status = 'REJECTED' WHERE provider_request_id = 'cap-a'`); err != nil {
			t.Fatalf("resolve first op: %v", err)
		}
		if _, err := store.Pool().Exec(ctx, `
			INSERT INTO merchant_operations (order_id, operation_type, provider_request_id, payload_fingerprint, status)
			VALUES ($1::uuid, 'CAPTURE', 'cap-c', 'fp', 'IN_FLIGHT')`, order.ID); err != nil {
			t.Fatalf("resolved attempt must not block a new one: %v", err)
		}
	})

	t.Run("event journal is append-only", func(t *testing.T) {
		msg := rawExecErr(t, store, `UPDATE merchant_events SET event_type = 'REWRITTEN' WHERE order_id = $1::uuid`, order.ID)
		if !strings.Contains(msg, "append-only") {
			t.Fatalf("unexpected error: %s", msg)
		}
		msg = rawExecErr(t, store, `DELETE FROM merchant_events WHERE order_id = $1::uuid`, order.ID)
		if !strings.Contains(msg, "append-only") {
			t.Fatalf("unexpected error: %s", msg)
		}
	})

	t.Run("order status vocabulary is constrained", func(t *testing.T) {
		msg := rawExecErr(t, store, `
			INSERT INTO merchant_orders (trace_id, product_id, quantity, total_minor, currency, status)
			VALUES ($1::uuid, 'maple-mug', 1, 1234, 'USD', 'COMPLETED_FOR_REAL')`, order.ID)
		if !strings.Contains(msg, "merchant_orders_status_check") {
			t.Fatalf("unexpected error: %s", msg)
		}
	})

	t.Run("quantity bounds are constrained", func(t *testing.T) {
		msg := rawExecErr(t, store, `
			INSERT INTO merchant_orders (trace_id, product_id, quantity, total_minor, currency, status)
			VALUES ($1::uuid, 'maple-mug', 0, 1234, 'USD', 'ORDER_CREATED')`, order.ID)
		if !strings.Contains(msg, "merchant_orders_quantity_check") {
			t.Fatalf("unexpected error: %s", msg)
		}
	})

	t.Run("reset clears orders but keeps the catalog", func(t *testing.T) {
		if err := store.Reset(ctx); err != nil {
			t.Fatalf("reset: %v", err)
		}
		orders, err := store.ListOrders(ctx, 10)
		if err != nil {
			t.Fatalf("list orders: %v", err)
		}
		if len(orders) != 0 {
			t.Fatalf("orders remain after reset: %d", len(orders))
		}
		products, err := store.Products(ctx)
		if err != nil || len(products) == 0 {
			t.Fatalf("catalog lost by reset: %v (%d products)", err, len(products))
		}
	})
}

// The payload fingerprint must represent the exact canonical outbound
// financial operation. Rather than only asserting that values differ, these
// checks pin the canonical part list: the stored fingerprint equals an
// independently recomputed digest over (scheme, product, quantity, total,
// currency, request id) — and materially depends on amount and currency,
// since swapping either changes the digest.
func TestCreateFingerprintMatchesCanonicalFinancialOperation(t *testing.T) {
	store := newFixtureStore(t)
	ctx := context.Background()

	one, op1, err := store.CreateLocalOrder(ctx, testTrace, "maple-mug", 1, "fp-req-1")
	if err != nil {
		t.Fatalf("create qty 1: %v", err)
	}
	if want := canonicalFP("waypoint.merchant.create.v1", "maple-mug", "1", "1234", "USD", "fp-req-1"); op1.PayloadFingerprint != want {
		t.Fatalf("stored fingerprint %s does not match the canonical financial operation digest %s", op1.PayloadFingerprint, want)
	}
	// Amount isolation: the same operation with any other total must digest
	// differently — proving the amount is materially covered.
	if op1.PayloadFingerprint == canonicalFP("waypoint.merchant.create.v1", "maple-mug", "1", "1235", "USD", "fp-req-1") {
		t.Fatal("fingerprint does not cover the amount: changing only the total left the digest unchanged")
	}
	// Currency isolation.
	if op1.PayloadFingerprint == canonicalFP("waypoint.merchant.create.v1", "maple-mug", "1", "1234", "EUR", "fp-req-1") {
		t.Fatal("fingerprint does not cover the currency: changing only the currency left the digest unchanged")
	}

	// A different quantity changes only the amount-bearing parts and must
	// still digest as the exact canonical operation for that order.
	_, op2, err := store.CreateLocalOrder(ctx, testTrace, "maple-mug", 2, "fp-req-2")
	if err != nil {
		t.Fatalf("create qty 2: %v", err)
	}
	if want := canonicalFP("waypoint.merchant.create.v1", "maple-mug", "2", "2468", "USD", "fp-req-2"); op2.PayloadFingerprint != want {
		t.Fatalf("stored fingerprint %s does not match the canonical digest %s for the qty-2 order", op2.PayloadFingerprint, want)
	}
	if one.TotalMinor != mugPrice {
		t.Fatalf("total = %d, want %d", one.TotalMinor, mugPrice)
	}
}
