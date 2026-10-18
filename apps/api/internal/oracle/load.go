// Package oracle — durable evidence loader.
//
// The oracle reads the merchant journal with its OWN SQL statements against
// the documented durable tables. It deliberately does not call the fixture's
// store or service layer: a candidate patch's bugs (or lies) must not be able
// to influence how the judge reads the evidence.
package oracle

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LocalEvidence is the durable slice of Evidence, read straight from
// PostgreSQL by the oracle.
type LocalEvidence struct {
	Orders     []LocalOrder
	Operations []LocalOp
	Events     []LocalEvent
}

// LoadLocal reads merchant_orders, merchant_operations and merchant_events
// for the given order ids inside one REPEATABLE READ transaction, so the
// snapshot is internally consistent.
func LoadLocal(ctx context.Context, pool *pgxpool.Pool, orderIDs []string) (LocalEvidence, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return LocalEvidence{}, fmt.Errorf("oracle: begin evidence tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return LocalEvidence{}, err
	}

	var out LocalEvidence

	rows, err := tx.Query(ctx, `
		SELECT id::text, product_id, quantity, total_minor, currency, status,
		       coalesce(provider_order_id, ''), created_at, updated_at
		  FROM merchant_orders WHERE id = ANY($1::uuid[]) ORDER BY created_at, id`, orderIDs)
	if err != nil {
		return LocalEvidence{}, fmt.Errorf("oracle: read orders: %w", err)
	}
	for rows.Next() {
		var o LocalOrder
		if err := rows.Scan(&o.ID, &o.ProductID, &o.Quantity, &o.TotalMinor, &o.Currency,
			&o.Status, &o.ProviderOrderID, &o.CreatedAt, &o.UpdatedAt); err != nil {
			rows.Close()
			return LocalEvidence{}, err
		}
		out.Orders = append(out.Orders, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return LocalEvidence{}, err
	}

	rows, err = tx.Query(ctx, `
		SELECT id::text, order_id::text, operation_type, provider_request_id,
		       payload_fingerprint, status, coalesce(provider_order_id, ''),
		       coalesce(provider_capture_id, ''), attempt_count, owner_id, created_at, updated_at
		  FROM merchant_operations WHERE order_id = ANY($1::uuid[]) ORDER BY created_at, id`, orderIDs)
	if err != nil {
		return LocalEvidence{}, fmt.Errorf("oracle: read operations: %w", err)
	}
	for rows.Next() {
		var op LocalOp
		if err := rows.Scan(&op.ID, &op.OrderID, &op.Type, &op.ProviderRequestID,
			&op.PayloadFingerprint, &op.Status, &op.ProviderOrderID, &op.ProviderCaptureID,
			&op.AttemptCount, &op.OwnerID, &op.CreatedAt, &op.UpdatedAt); err != nil {
			rows.Close()
			return LocalEvidence{}, err
		}
		out.Operations = append(out.Operations, op)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return LocalEvidence{}, err
	}

	rows, err = tx.Query(ctx, `
		SELECT id, order_id::text, source, event_type, detail::text, tier, created_at
		  FROM merchant_events WHERE order_id = ANY($1::uuid[]) ORDER BY id`, orderIDs)
	if err != nil {
		return LocalEvidence{}, fmt.Errorf("oracle: read events: %w", err)
	}
	for rows.Next() {
		var e LocalEvent
		var detail []byte
		if err := rows.Scan(&e.ID, &e.OrderID, &e.Source, &e.EventType, &detail, &e.Tier, &e.CreatedAt); err != nil {
			rows.Close()
			return LocalEvidence{}, err
		}
		if !json.Valid(detail) {
			detail = []byte(`{"_unreadable":true}`)
		}
		e.Detail = string(detail)
		out.Events = append(out.Events, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return LocalEvidence{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return LocalEvidence{}, err
	}
	return out, nil
}
