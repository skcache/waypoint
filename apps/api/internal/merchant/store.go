package merchant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound marks a missing fixture record.
var ErrNotFound = errors.New("merchant: not found")

// ErrConflict marks a state conflict: capture before approval, capture while
// another attempt is alive, an already-captured order, and similar refusals
// that happen BEFORE any outbound provider call.
var ErrConflict = errors.New("merchant: conflicting state")

// Store is the fixture's PostgreSQL persistence. Every state transition that
// accompanies a provider mutation is committed here — the outbound call only
// ever happens after the local transaction that records the operation.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore wraps an existing pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Open connects a dedicated pool.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("merchant: open database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("merchant: database unreachable: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// Pool exposes the underlying pool (readiness checks, tests).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// leaseExpiry converts a lease TTL into an absolute expiry; TTLs <= 0 record
// no lease (NULL), which recovery treats as abandoned.
func leaseExpiry(ttl time.Duration) *time.Time {
	if ttl <= 0 {
		return nil
	}
	t := time.Now().Add(ttl)
	return &t
}

const orderColumns = `id::text, trace_id::text, product_id, quantity, total_minor, currency,
	status, coalesce(provider_order_id, ''),
	coalesce(approval_url, ''), created_at, updated_at`

func scanOrder(row pgx.Row) (Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.TraceID, &o.ProductID, &o.Quantity, &o.TotalMinor, &o.Currency,
		&o.Status, &o.ProviderOrderID, &o.ApprovalURL, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}

const operationColumns = `id::text, order_id::text, operation_type, provider_request_id,
	payload_fingerprint, status, coalesce(provider_order_id, ''),
	coalesce(provider_capture_id, ''), attempt_count, owner_id, lease_expires_at, fence,
	first_attempted_at, last_checked_at, created_at, updated_at`

func scanOperation(row pgx.Row) (Operation, error) {
	var op Operation
	err := row.Scan(&op.ID, &op.OrderID, &op.Type, &op.ProviderRequestID, &op.PayloadFingerprint,
		&op.Status, &op.ProviderOrderID, &op.ProviderCaptureID, &op.AttemptCount,
		&op.OwnerID, &op.LeaseExpiresAt, &op.Fence,
		&op.FirstAttemptedAt, &op.LastCheckedAt, &op.CreatedAt, &op.UpdatedAt)
	return op, err
}

// SeedProducts upserts the fixture catalog. Prices are the only trusted
// source of amounts; they live here, never in browser input.
func (s *Store) SeedProducts(ctx context.Context) error {
	products := []Product{
		{ID: "maple-mug", Name: "Maple Ceramic Mug", Description: "Hand-glazed 350ml mug", PriceMinor: 1234},
		{ID: "field-notebook", Name: "Field Notebook Set", Description: "Three dotted A5 notebooks", PriceMinor: 999},
		{ID: "desk-lamp", Name: "Warm Desk Lamp", Description: "Dimmable 2700K desk lamp", PriceMinor: 2499},
	}
	for _, p := range products {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO merchant_products (product_id, name, description, price_minor)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (product_id) DO UPDATE
			   SET name = EXCLUDED.name, description = EXCLUDED.description, price_minor = EXCLUDED.price_minor`,
			p.ID, p.Name, p.Description, p.PriceMinor); err != nil {
			return fmt.Errorf("merchant: seed product %s: %w", p.ID, err)
		}
	}
	return nil
}

// Products lists the active fixture catalog.
func (s *Store) Products(ctx context.Context) ([]Product, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT product_id, name, description, price_minor, currency
		  FROM merchant_products WHERE active ORDER BY product_id`)
	if err != nil {
		return nil, fmt.Errorf("merchant: list products: %w", err)
	}
	defer rows.Close()
	var out []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.PriceMinor, &p.Currency); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateLocalOrder persists, in ONE transaction: the order, the CREATE_ORDER
// operation with its stable request id and payload fingerprint (status
// IN_FLIGHT), and the first journal event. This transaction commits BEFORE
// any provider call — that ordering is the I4 guarantee. The fingerprint is
// built here because only this layer knows the server-computed total and
// currency that the canonical financial operation must cover.
func (s *Store) CreateLocalOrder(ctx context.Context, traceID, productID string, quantity int, requestID string) (Order, Operation, error) {
	if quantity < 1 || quantity > 10 {
		return Order{}, Operation{}, fmt.Errorf("%w: quantity %d outside 1..10", ErrConflict, quantity)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, Operation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var priceMinor int64
	var currency string
	var productName string
	err = tx.QueryRow(ctx, `
		SELECT price_minor, currency, name FROM merchant_products
		 WHERE product_id = $1 AND active`, productID).Scan(&priceMinor, &currency, &productName)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, Operation{}, fmt.Errorf("%w: product %q", ErrNotFound, productID)
	}
	if err != nil {
		return Order{}, Operation{}, err
	}

	total := priceMinor * int64(quantity)
	if total <= 0 || total > 1_000_000_000 {
		return Order{}, Operation{}, fmt.Errorf("%w: computed total out of range", ErrConflict)
	}

	order, err := scanOrder(tx.QueryRow(ctx, `
		INSERT INTO merchant_orders (trace_id, product_id, quantity, total_minor, currency, status)
		VALUES ($1::uuid, $2, $3, $4, $5, $6)
		RETURNING `+orderColumns,
		traceID, productID, quantity, total, currency, StatusOrderCreated))
	if err != nil {
		return Order{}, Operation{}, fmt.Errorf("merchant: insert order: %w", err)
	}

	fp := payloadFingerprint("waypoint.merchant.create.v1", productID, strconv.Itoa(quantity),
		strconv.FormatInt(total, 10), currency, requestID)
	op, err := scanOperation(tx.QueryRow(ctx, `
		INSERT INTO merchant_operations (order_id, operation_type, provider_request_id, payload_fingerprint, status)
		VALUES ($1::uuid, $2, $3, $4, $5)
		RETURNING `+operationColumns,
		order.ID, OpCreateOrder, requestID, fp, OpStatusInFlight))
	if err != nil {
		return Order{}, Operation{}, fmt.Errorf("merchant: insert create operation: %w", err)
	}

	detail, _ := json.Marshal(map[string]any{
		"product_id": productID, "product_name": productName,
		"quantity": quantity, "total_minor": total, "currency": currency,
		"provider_request_id": requestID,
	})
	if _, err := tx.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		VALUES ($1::uuid, $2::uuid, 'API', 'ORDER_CREATED', $3, $4)`,
		order.ID, traceID, detail, TierReal); err != nil {
		return Order{}, Operation{}, fmt.Errorf("merchant: insert order event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Order{}, Operation{}, err
	}
	return order, op, nil
}

// MarkCreateSucceeded records a provider-accepted create. Accepted from
// IN_FLIGHT (first call) or UNKNOWN (idempotent resolution with the same
// request id).
func (s *Store) MarkCreateSucceeded(ctx context.Context, orderID, providerOrderID, approvalURL, tier string) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	order, err := scanOrder(tx.QueryRow(ctx, `
		UPDATE merchant_orders
		   SET status = $2, provider_order_id = $3, approval_url = $4, updated_at = now()
		 WHERE id = $1::uuid AND status IN ('ORDER_CREATED')
		RETURNING `+orderColumns,
		orderID, StatusBuyerApprovalPending, providerOrderID, nullIfEmpty(approvalURL)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, fmt.Errorf("%w: order %s not in ORDER_CREATED state", ErrConflict, orderID)
	}
	if err != nil {
		return Order{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE merchant_operations
		   SET status = 'SUCCEEDED', provider_order_id = $2, last_checked_at = now(), updated_at = now()
		 WHERE order_id = $1::uuid AND operation_type = 'CREATE_ORDER' AND status IN ('IN_FLIGHT', 'UNKNOWN')`,
		orderID, providerOrderID); err != nil {
		return Order{}, err
	}

	detail, _ := json.Marshal(map[string]any{"provider_order_id": providerOrderID, "approval_url_present": approvalURL != ""})
	if _, err := tx.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		VALUES ($1::uuid, $2::uuid, 'PAYPAL', 'PROVIDER_ORDER_CREATED', $3, $4)`,
		orderID, order.TraceID, detail, tier); err != nil {
		return Order{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return order, nil
}

// MarkCreateRejected records a DEFINITE provider rejection (or a local
// validation refusal that never left the process): no provider order exists
// and the attempt is terminal.
func (s *Store) MarkCreateRejected(ctx context.Context, orderID string, detail json.RawMessage, tier string) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	order, err := scanOrder(tx.QueryRow(ctx, `
		UPDATE merchant_orders
		   SET status = $2, updated_at = now()
		 WHERE id = $1::uuid AND status = 'ORDER_CREATED'
		RETURNING `+orderColumns, orderID, StatusFailedDefinite))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, fmt.Errorf("%w: order %s not in ORDER_CREATED state", ErrConflict, orderID)
	}
	if err != nil {
		return Order{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE merchant_operations
		   SET status = 'REJECTED', last_checked_at = now(), updated_at = now()
		 WHERE order_id = $1::uuid AND operation_type = 'CREATE_ORDER' AND status IN ('IN_FLIGHT', 'UNKNOWN')`,
		orderID); err != nil {
		return Order{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		VALUES ($1::uuid, $2::uuid, 'PAYPAL', 'PROVIDER_CREATE_REJECTED', $3, $4)`,
		orderID, order.TraceID, detail, tier); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return order, nil
}

// MarkCreateUnknown records an ambiguous create outcome. The order stays
// ORDER_CREATED and the operation stays unresolved — never failed — until a
// same-request-id resolution or reconciliation establishes the truth.
func (s *Store) MarkCreateUnknown(ctx context.Context, orderID string, detail json.RawMessage, tier string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var traceID string
	if err := tx.QueryRow(ctx, `SELECT trace_id::text FROM merchant_orders WHERE id = $1::uuid`, orderID).Scan(&traceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE merchant_operations SET status = 'UNKNOWN', last_checked_at = now(), updated_at = now()
		 WHERE order_id = $1::uuid AND operation_type = 'CREATE_ORDER' AND status = 'IN_FLIGHT'`, orderID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		VALUES ($1::uuid, $2::uuid, 'PAYPAL', 'PROVIDER_CREATE_UNKNOWN', $3, $4)`,
		orderID, traceID, detail, tier); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ObserveOrderStatus folds a live provider read into local state. It only
// ever moves an order forward toward APPROVED and never touches capture
// outcomes — verified evidence stays sticky, and an APPROVED observation can
// never resurrect a capture-eligible state after a capture attempt.
func (s *Store) ObserveOrderStatus(ctx context.Context, orderID, providerStatus, payerID, tier string) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	order, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM merchant_orders WHERE id = $1::uuid FOR UPDATE`, orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, fmt.Errorf("%w: order %s", ErrNotFound, orderID)
	}
	if err != nil {
		return Order{}, err
	}

	if providerStatus == "APPROVED" && order.Status == StatusBuyerApprovalPending {
		order, err = scanOrder(tx.QueryRow(ctx, `
			UPDATE merchant_orders SET status = $2, updated_at = now()
			 WHERE id = $1::uuid AND status = 'BUYER_APPROVAL_PENDING'
			RETURNING `+orderColumns, orderID, StatusApproved))
		if err != nil {
			return Order{}, err
		}
	}

	detail, _ := json.Marshal(map[string]any{"provider_status": providerStatus, "payer_id": payerID})
	if _, err := tx.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		VALUES ($1::uuid, $2::uuid, 'PAYPAL', 'PROVIDER_ORDER_OBSERVED', $3, $4)`,
		orderID, order.TraceID, detail, tier); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return order, nil
}

// ActiveCapture returns the order's alive capture operation (IN_FLIGHT or
// UNKNOWN), if any.
func (s *Store) ActiveCapture(ctx context.Context, orderID string) (*Operation, error) {
	op, err := scanOperation(s.pool.QueryRow(ctx, `
		SELECT `+operationColumns+` FROM merchant_operations
		 WHERE order_id = $1::uuid AND operation_type = 'CAPTURE' AND status IN ('IN_FLIGHT', 'UNKNOWN')`,
		orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

// InsertCaptureOp durably begins a capture: in one transaction it flips the
// order to CAPTURE_IN_FLIGHT and inserts the CAPTURE operation with its
// stable request id, BEFORE any outbound capture call. The owner id and lease
// record which worker owns the attempt and until when — recovery workers may
// only demote or take it over once the lease has expired. If another capture
// operation is already alive, the partial unique index rejects the insert
// (`merchant_operations_one_active_capture`) — concurrent attempts cannot
// both claim the right to call the provider.
func (s *Store) InsertCaptureOp(ctx context.Context, orderID, requestID, payloadFingerprint, ownerID string, leaseTTL time.Duration) (Operation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Operation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	order, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM merchant_orders WHERE id = $1::uuid FOR UPDATE`, orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Operation{}, fmt.Errorf("%w: order %s", ErrNotFound, orderID)
	}
	if err != nil {
		return Operation{}, err
	}
	switch order.Status {
	case StatusApproved:
		// Capture is legitimate: buyer approval was observed.
	case StatusCapturedVerified:
		return Operation{}, fmt.Errorf("%w: order %s is already CAPTURED_VERIFIED", ErrConflict, orderID)
	case StatusCaptureOutcomeUnknown, StatusCaptureInFlight, StatusReconciliationRequired:
		return Operation{}, fmt.Errorf("%w: order %s has an unresolved capture attempt", ErrConflict, orderID)
	default:
		return Operation{}, fmt.Errorf("%w: order %s is %s; capture requires observed buyer approval", ErrConflict, orderID, order.Status)
	}

	op, err := scanOperation(tx.QueryRow(ctx, `
		INSERT INTO merchant_operations (order_id, operation_type, provider_request_id, payload_fingerprint, status, owner_id, lease_expires_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)
		RETURNING `+operationColumns,
		orderID, OpCapture, requestID, payloadFingerprint, OpStatusInFlight, ownerID, leaseExpiry(leaseTTL)))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Operation{}, fmt.Errorf("%w: another capture attempt is alive for order %s", ErrConflict, orderID)
		}
		return Operation{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE merchant_orders SET status = $2, updated_at = now() WHERE id = $1::uuid`,
		orderID, StatusCaptureInFlight); err != nil {
		return Operation{}, err
	}

	detail, _ := json.Marshal(map[string]any{"provider_request_id": requestID, "provider_order_id": order.ProviderOrderID})
	if _, err := tx.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		VALUES ($1::uuid, $2::uuid, 'API', 'CAPTURE_ATTEMPT_STARTED', $3, $4)`,
		orderID, order.TraceID, detail, TierReal); err != nil {
		return Operation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Operation{}, err
	}
	return op, nil
}

// AppendEvent adds one journal entry (FAULT markers, operator notes).
func (s *Store) AppendEvent(ctx context.Context, orderID, source, eventType string, detail json.RawMessage, tier string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		SELECT id, trace_id, $2, $3, $4, $5 FROM merchant_orders WHERE id = $1::uuid`,
		orderID, source, eventType, detail, tier)
	if err != nil {
		return fmt.Errorf("merchant: append event: %w", err)
	}
	return nil
}

// TouchCaptureAttempt records a retry/reconciliation attempt against an
// existing operation (the request id is reused, never replaced).
func (s *Store) TouchCaptureAttempt(ctx context.Context, operationID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE merchant_operations
		   SET attempt_count = attempt_count + 1, last_checked_at = now(), updated_at = now()
		 WHERE id = $1::uuid`, operationID)
	return err
}

// TouchLastChecked marks an operation as freshly inspected without counting
// an attempt: read-only reconciliation must not consume the bounded same-id
// retry budget.
func (s *Store) TouchLastChecked(ctx context.Context, operationID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE merchant_operations SET last_checked_at = now(), updated_at = now()
		 WHERE id = $1::uuid`, operationID)
	return err
}

// BeginCaptureRetry durably re-enters the in-flight state for an existing
// unresolved capture operation, reusing its preserved request id and bumping
// the ownership FENCE. Two cases are permitted, both fenced:
//
//   - the operation is UNKNOWN (a previous attempt ended unresolved);
//   - the operation is IN_FLIGHT but its lease has expired (or was never
//     set): the crashed owner has demonstrably stopped, so the retry TAKES
//     OVER the same operation — new owner, new lease, new generation, SAME
//     request id.
//
// An IN_FLIGHT operation with a LIVE lease is refused: another worker may be
// mid-mutation and must not be raced. The returned operation carries the
// post-claim fence, which every subsequent transition must present.
func (s *Store) BeginCaptureRetry(ctx context.Context, orderID, operationID, ownerID string, leaseTTL time.Duration) (Operation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Operation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	op, err := scanOperation(tx.QueryRow(ctx, `
		UPDATE merchant_operations
		   SET status = 'IN_FLIGHT', attempt_count = attempt_count + 1,
		       owner_id = $2, lease_expires_at = $3,
		       fence = fence + 1, last_checked_at = now(), updated_at = now()
		 WHERE id = $1::uuid AND operation_type = 'CAPTURE'
		   AND (status = 'UNKNOWN'
		        OR (status = 'IN_FLIGHT' AND (lease_expires_at IS NULL OR lease_expires_at < now())))
		RETURNING `+operationColumns,
		operationID, ownerID, leaseExpiry(leaseTTL)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Operation{}, fmt.Errorf("%w: capture operation %s is not retryable (unresolved, or in flight with an expired lease)", ErrConflict, operationID)
	}
	if err != nil {
		return Operation{}, err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE merchant_orders
		   SET status = $2, updated_at = now()
		 WHERE id = $1::uuid AND status IN ('APPROVED', 'CAPTURE_OUTCOME_UNKNOWN', 'RECONCILIATION_REQUIRED')`,
		orderID, StatusCaptureInFlight)
	if err != nil {
		return Operation{}, err
	}
	if tag.RowsAffected() != 1 {
		return Operation{}, fmt.Errorf("%w: order %s cannot re-enter capture (status changed)", ErrConflict, orderID)
	}

	var requestID string
	if err := tx.QueryRow(ctx, `SELECT provider_request_id FROM merchant_operations WHERE id = $1::uuid`, operationID).Scan(&requestID); err != nil {
		return Operation{}, err
	}
	detail, _ := json.Marshal(map[string]any{"provider_request_id": requestID, "note": "retry reuses the original request id", "fence": op.Fence})
	if _, err := tx.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		SELECT id, trace_id, 'API', 'CAPTURE_RETRY_STARTED', $2, $3 FROM merchant_orders WHERE id = $1::uuid`,
		orderID, detail, TierReal); err != nil {
		return Operation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Operation{}, err
	}
	return op, nil
}

// MarkCaptureVerified seals a provider-confirmed capture. Only states that
// are unresolved-or-in-flight may reach CAPTURED_VERIFIED; the update is
// guarded by the order status AND by the fencing generation the worker
// inspected, so a stale observation (or a stale worker after a takeover) can
// never rewrite an operation that moved beneath it.
func (s *Store) MarkCaptureVerified(ctx context.Context, orderID, operationID string, expectedFence int64, captureID string, detail json.RawMessage, tier string) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE merchant_operations
		   SET status = 'SUCCEEDED', provider_capture_id = $2, last_checked_at = now(), updated_at = now()
		 WHERE id = $1::uuid AND operation_type = 'CAPTURE'
		   AND status IN ('IN_FLIGHT', 'UNKNOWN') AND fence = $3`,
		operationID, captureID, expectedFence)
	if err != nil {
		return Order{}, err
	}
	if tag.RowsAffected() != 1 {
		return Order{}, fmt.Errorf("%w: capture operation %s moved under this worker (ownership generation changed); refusing a stale write", ErrConflict, operationID)
	}

	order, err := scanOrder(tx.QueryRow(ctx, `
		UPDATE merchant_orders
		   SET status = $2, updated_at = now()
		 WHERE id = $1::uuid AND status IN ('CAPTURE_IN_FLIGHT', 'CAPTURE_OUTCOME_UNKNOWN', 'RECONCILIATION_REQUIRED')
		RETURNING `+orderColumns, orderID, StatusCapturedVerified))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, fmt.Errorf("%w: order %s is not in a capturable-unresolved state", ErrConflict, orderID)
	}
	if err != nil {
		return Order{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		VALUES ($1::uuid, $2::uuid, 'PAYPAL', 'PROVIDER_CAPTURE_CONFIRMED', $3, $4)`,
		orderID, order.TraceID, detail, tier); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return order, nil
}

// MarkCaptureUnknown records an unresolved capture. The order keeps its
// unknown status and the operation keeps its request id; nothing is failed.
// The transition is fenced: the caller must present the ownership generation
// it inspected.
func (s *Store) MarkCaptureUnknown(ctx context.Context, orderID, operationID string, expectedFence int64, detail json.RawMessage, tier string) (Order, error) {
	return s.captureTransition(ctx, orderID, operationID, expectedFence, "UNKNOWN", StatusCaptureOutcomeUnknown, "CAPTURE_OUTCOME_UNKNOWN", detail, tier)
}

// MarkCaptureRejected resolves an attempt as a definite no-effect rejection:
// the provider refused without capturing. The order returns to APPROVED (a
// buyer may still be charged through a later authorized attempt), never to a
// fake failure. Fenced like every capture transition.
func (s *Store) MarkCaptureRejected(ctx context.Context, orderID, operationID string, expectedFence int64, detail json.RawMessage, tier string) (Order, error) {
	return s.captureTransition(ctx, orderID, operationID, expectedFence, "REJECTED", StatusApproved, "CAPTURE_REJECTED_DEFINITE", detail, tier)
}

// MarkReconciliationRequired records that local and provider evidence
// disagree or remain insufficient; nothing is failed or completed by guess.
//
// The transition is FENCED when an operation is named: opID/expectedFence must
// still identify the caller's ownership generation, otherwise a stale worker
// could demote an order whose capture another worker has already taken over
// and is actively mutating. With no operation named (the pre-capture
// COMPLETED-without-evidence path), the transition is refused while any live
// IN_FLIGHT capture exists — the same ownership rule, expressed as a
// predicate, because there is no generation to compare.
//
// A refusal returns ErrConflict; callers must not retry blindly.
func (s *Store) MarkReconciliationRequired(ctx context.Context, orderID, opID string, expectedFence int64, detail json.RawMessage, tier string) error {
	if tier == "" {
		tier = TierReal
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var traceID string
	if err := tx.QueryRow(ctx, `SELECT trace_id::text FROM merchant_orders WHERE id = $1::uuid`, orderID).Scan(&traceID); err != nil {
		return err
	}

	var tag pgconn.CommandTag
	if opID != "" {
		tag, err = tx.Exec(ctx, `
			UPDATE merchant_orders SET status = $4, updated_at = now()
			 WHERE id = $1::uuid
			   AND status IN ('APPROVED', 'CAPTURE_OUTCOME_UNKNOWN', 'CAPTURE_IN_FLIGHT')
			   AND EXISTS (SELECT 1 FROM merchant_operations op
			                WHERE op.id = $2::uuid AND op.order_id = $1::uuid
			                  AND op.fence = $3 AND op.status IN ('IN_FLIGHT', 'UNKNOWN'))`,
			orderID, opID, expectedFence, StatusReconciliationRequired)
	} else {
		tag, err = tx.Exec(ctx, `
			UPDATE merchant_orders SET status = $2, updated_at = now()
			 WHERE id = $1::uuid
			   AND status IN ('APPROVED', 'CAPTURE_OUTCOME_UNKNOWN', 'CAPTURE_IN_FLIGHT')
			   AND NOT EXISTS (SELECT 1 FROM merchant_operations op
			                    WHERE op.order_id = $1::uuid AND op.status = 'IN_FLIGHT'
			                      AND op.lease_expires_at IS NOT NULL AND op.lease_expires_at > now())`,
			orderID, StatusReconciliationRequired)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: reconciliation demotion refused for order %s — the operation moved under this worker (ownership generation changed or a live attempt owns it); re-read the durable state", ErrConflict, orderID)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		VALUES ($1::uuid, $2::uuid, 'API', 'RECONCILIATION_REQUIRED', $3, $4)`,
		orderID, traceID, detail, tier); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) captureTransition(ctx context.Context, orderID, operationID string, expectedFence int64, opStatus, orderStatus, eventType string, detail json.RawMessage, tier string) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Atomic fencing condition: the operation must still be in the state this
	// worker inspected (same ownership generation). A takeover by another
	// worker bumps the fence, so this UPDATE then matches zero rows and the
	// stale transition is refused rather than overwriting a live attempt.
	tag, err := tx.Exec(ctx, `
		UPDATE merchant_operations
		   SET status = $2, last_checked_at = now(), updated_at = now()
		 WHERE id = $1::uuid AND operation_type = 'CAPTURE'
		   AND status IN ('IN_FLIGHT', 'UNKNOWN') AND fence = $3`,
		operationID, opStatus, expectedFence)
	if err != nil {
		return Order{}, err
	}
	if tag.RowsAffected() != 1 {
		return Order{}, fmt.Errorf("%w: capture operation %s moved under this worker (ownership generation changed); refusing a stale write", ErrConflict, operationID)
	}

	order, err := scanOrder(tx.QueryRow(ctx, `
		UPDATE merchant_orders SET status = $2, updated_at = now()
		 WHERE id = $1::uuid AND status IN ('CAPTURE_IN_FLIGHT', 'CAPTURE_OUTCOME_UNKNOWN')
		RETURNING `+orderColumns, orderID, orderStatus))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, fmt.Errorf("%w: order %s is not in an in-flight or unresolved capture state", ErrConflict, orderID)
	}
	if err != nil {
		return Order{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		VALUES ($1::uuid, $2::uuid, 'PAYPAL', $3, $4, $5)`,
		orderID, order.TraceID, eventType, detail, tier); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return order, nil
}

// MarkCaptureVerifiedRecovered seals a capture proven by provider evidence
// when no local in-flight operation must be closed first (recovery after a
// restart, or evidence that predates this process). When operationID is
// non-empty, the unresolved operation is closed as SUCCEEDED with the capture
// id — fenced by the ownership generation the caller inspected. Sticky:
// CAPTURED_VERIFIED is never re-entered or downgraded.
func (s *Store) MarkCaptureVerifiedRecovered(ctx context.Context, orderID, operationID string, expectedFence int64, captureID string, detail json.RawMessage, tier string) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if operationID != "" {
		tag, err := tx.Exec(ctx, `
			UPDATE merchant_operations
			   SET status = 'SUCCEEDED', provider_capture_id = $2, last_checked_at = now(), updated_at = now()
			 WHERE id = $1::uuid AND operation_type = 'CAPTURE'
			   AND status IN ('IN_FLIGHT', 'UNKNOWN') AND fence = $3`,
			operationID, captureID, expectedFence)
		if err != nil {
			return Order{}, err
		}
		if tag.RowsAffected() != 1 {
			return Order{}, fmt.Errorf("%w: capture operation %s moved under this worker (ownership generation changed); refusing a stale write", ErrConflict, operationID)
		}
	}

	order, err := scanOrder(tx.QueryRow(ctx, `
		UPDATE merchant_orders
		   SET status = $2, updated_at = now()
		 WHERE id = $1::uuid AND status IN ('ORDER_CREATED', 'BUYER_APPROVAL_PENDING', 'APPROVED',
		       'CAPTURE_IN_FLIGHT', 'CAPTURE_OUTCOME_UNKNOWN', 'RECONCILIATION_REQUIRED')
		RETURNING `+orderColumns, orderID, StatusCapturedVerified))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, fmt.Errorf("%w: order %s is not in a recoverable state", ErrConflict, orderID)
	}
	if err != nil {
		return Order{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO merchant_events (order_id, trace_id, source, event_type, detail, tier)
		VALUES ($1::uuid, $2::uuid, 'PAYPAL', 'PROVIDER_CAPTURE_RECOVERED', $3, $4)`,
		orderID, order.TraceID, detail, tier); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return order, nil
}

// OrderDetail loads the complete local view: order, operations, journal.
func (s *Store) OrderDetail(ctx context.Context, orderID string) (OrderDetail, error) {
	var detail OrderDetail
	order, err := scanOrder(s.pool.QueryRow(ctx, `SELECT `+orderColumns+` FROM merchant_orders WHERE id = $1::uuid`, orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderDetail{}, fmt.Errorf("%w: order %s", ErrNotFound, orderID)
	}
	if err != nil {
		return OrderDetail{}, err
	}
	detail.Order = order

	rows, err := s.pool.Query(ctx, `SELECT `+operationColumns+` FROM merchant_operations WHERE order_id = $1::uuid ORDER BY created_at`, orderID)
	if err != nil {
		return OrderDetail{}, err
	}
	for rows.Next() {
		op, err := scanOperation(rows)
		if err != nil {
			rows.Close()
			return OrderDetail{}, err
		}
		detail.Operations = append(detail.Operations, op)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return OrderDetail{}, err
	}

	events, err := s.pool.Query(ctx, `
		SELECT id, order_id::text, trace_id::text, source, event_type, detail, tier, created_at
		  FROM merchant_events WHERE order_id = $1::uuid ORDER BY id`, orderID)
	if err != nil {
		return OrderDetail{}, err
	}
	for events.Next() {
		var e Event
		if err := events.Scan(&e.ID, &e.OrderID, &e.TraceID, &e.Source, &e.EventType, &e.Detail, &e.Tier, &e.CreatedAt); err != nil {
			events.Close()
			return OrderDetail{}, err
		}
		detail.Events = append(detail.Events, e)
	}
	events.Close()
	return detail, events.Err()
}

// ListOrders returns the most recent orders.
func (s *Store) ListOrders(ctx context.Context, limit int) ([]Order, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT `+orderColumns+` FROM merchant_orders ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// FindByProviderOrderID resolves the merchant order behind a provider order
// id (redirect/return handling).
func (s *Store) FindByProviderOrderID(ctx context.Context, providerOrderID string) (Order, error) {
	order, err := scanOrder(s.pool.QueryRow(ctx, `SELECT `+orderColumns+` FROM merchant_orders WHERE provider_order_id = $1`, providerOrderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, fmt.Errorf("%w: provider order %s", ErrNotFound, providerOrderID)
	}
	return order, err
}

// Reset truncates the fixture's transactional tables (orders, operations,
// events) and keeps the seeded catalog. This is the reproducible-reset
// command for demos and tests; it never runs against non-fixture tables.
func (s *Store) Reset(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `TRUNCATE merchant_events, merchant_operations, merchant_orders RESTART IDENTITY CASCADE`)
	if err != nil {
		return fmt.Errorf("merchant: reset fixture tables: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
