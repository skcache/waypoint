package merchant_test

import (
	"context"
	"strings"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
)

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

	order, _, err := store.CreateLocalOrder(ctx, testTrace, "maple-mug", 1, "req-id-1", "fp-1")
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
