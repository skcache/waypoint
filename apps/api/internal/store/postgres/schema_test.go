package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/skcache/waypoint/apps/api/internal/store/postgres"
	"github.com/skcache/waypoint/apps/api/migrations"
)

// wantPgError asserts that err is a PostgreSQL error with the given SQLSTATE.
func wantPgError(t *testing.T, err error, sqlstate string) *pgconn.PgError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected PostgreSQL error %s, got nil", sqlstate)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected *pgconn.PgError, got %T: %v", err, err)
	}
	if pgErr.Code != sqlstate {
		t.Fatalf("SQLSTATE = %s (%s), want %s", pgErr.Code, pgErr.Message, sqlstate)
	}
	return pgErr
}

// ---------------------------------------------------------------------------
// P02.2 — migrations
// ---------------------------------------------------------------------------

func TestCleanMigrationFromScratch(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool) // drop schema, migrate from scratch

	rows, err := pool.Query(context.Background(),
		`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	var applied []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan version: %v", err)
		}
		applied = append(applied, v)
	}
	rows.Close()

	files, err := migrations.All()
	if err != nil {
		t.Fatalf("embedded migrations: %v", err)
	}
	if len(applied) != len(files) {
		t.Fatalf("applied %d migrations, embedded set has %d", len(applied), len(files))
	}
	for i, f := range files {
		if applied[i] != f.Version {
			t.Fatalf("applied[%d] = %d, want %d", i, applied[i], f.Version)
		}
	}

	var missing int
	tables := []string{
		"demo_sessions", "intent_contracts", "catalog_items", "transactions",
		"payment_operations", "semantic_verifications", "transaction_events",
		"provider_webhook_events", "schema_migrations",
	}
	err = pool.QueryRow(context.Background(), `
		SELECT count(*) FROM unnest($1::text[]) AS name
		 WHERE to_regclass('public.' || name) IS NULL`, tables).Scan(&missing)
	if err != nil {
		t.Fatalf("table existence check: %v", err)
	}
	if missing != 0 {
		t.Fatalf("%d expected tables are missing after migration", missing)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)

	applied, err := postgres.Migrate(context.Background(), pool)
	if err != nil {
		t.Fatalf("second migrate run: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("second migrate run applied %v, want none", applied)
	}
}

func TestMigrateDetectsModifiedAppliedMigration(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)

	if _, err := pool.Exec(context.Background(),
		`UPDATE schema_migrations SET checksum = 'tampered' WHERE version = 1`); err != nil {
		t.Fatalf("tamper checksum: %v", err)
	}

	_, err := postgres.Migrate(context.Background(), pool)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// P02.5 — constraint and invariant tests
// ---------------------------------------------------------------------------

func TestForeignKeyEnforcement(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	// Operations must reference an existing transaction (23503).
	_, err := pool.Exec(ctx, `
		INSERT INTO payment_operations (transaction_id, operation_type, paypal_request_id, payload_fingerprint)
		VALUES (gen_random_uuid(), 'CREATE_ORDER', $1, $2)`, randomHex(t), randomHash(t))
	wantPgError(t, err, "23503")

	// Events must reference an existing transaction (23503).
	_, err = pool.Exec(ctx, `
		INSERT INTO transaction_events (transaction_id, seq, event_type, source)
		VALUES (gen_random_uuid(), 1, 'TEST', 'RUNTIME')`)
	wantPgError(t, err, "23503")

	// A transaction must reference an existing catalog item (23503); the
	// contract side is covered by the confirmed-contract trigger test.
	session := seedSession(t, pool)
	contract := seedIntentDraft(t, pool, session)
	confirmContract(t, pool, contract, 1)
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions
			(principal_id, contract_id, contract_version, client_operation_key,
			 catalog_item_id, catalog_item_version, quantity, amount_minor, currency, action_fingerprint)
		VALUES ($1, $2, 1, $3, 'no-such-item', 1, 1, 100, 'USD', $4)`,
		session, contract, "key-"+randomHex(t), randomHash(t))
	wantPgError(t, err, "23503")

	// Deleting a session that still has contracts is refused (RESTRICT).
	_, err = pool.Exec(ctx, `DELETE FROM demo_sessions WHERE id = $1`, session)
	wantPgError(t, err, "23503")
}

func TestUniqueLogicalOperationIdentity(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	session := seedSession(t, pool)
	contract := seedIntentDraft(t, pool, session)
	confirmContract(t, pool, contract, 1)
	seedCatalogItem(t, pool, "keyboard-office-quiet")
	txn := seedTransaction(t, pool, session, contract, 1, "keyboard-office-quiet")

	// One operation row per (transaction_id, operation_type).
	reqID1 := randomHex(t)
	reqID2 := randomHex(t)
	_, err := pool.Exec(ctx, `
		INSERT INTO payment_operations (transaction_id, operation_type, paypal_request_id, payload_fingerprint)
		VALUES ($1, 'CREATE_ORDER', $2, $3)`, txn, reqID1, randomHash(t))
	if err != nil {
		t.Fatalf("first CREATE_ORDER operation: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO payment_operations (transaction_id, operation_type, paypal_request_id, payload_fingerprint)
		VALUES ($1, 'CREATE_ORDER', $2, $3)`, txn, reqID2, randomHash(t))
	wantPgError(t, err, "23505")

	// Provider request ids are unique system-wide (a second transaction may
	// not reuse a request id).
	txn2 := seedTransaction(t, pool, session, contract, 1, "keyboard-office-quiet")
	_, err = pool.Exec(ctx, `
		INSERT INTO payment_operations (transaction_id, operation_type, paypal_request_id, payload_fingerprint)
		VALUES ($1, 'CREATE_ORDER', $2, $3)`, txn2, reqID1, randomHash(t))
	wantPgError(t, err, "23505")

	// Capture is a distinct operation type for the same transaction.
	_, err = pool.Exec(ctx, `
		INSERT INTO payment_operations (transaction_id, operation_type, paypal_request_id, payload_fingerprint)
		VALUES ($1, 'CAPTURE', $2, $3)`, txn, randomHex(t), randomHash(t))
	if err != nil {
		t.Fatalf("CAPTURE operation alongside CREATE_ORDER: %v", err)
	}
}

func TestConflictingDuplicateIdentities(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	session := seedSession(t, pool)
	contract := seedIntentDraft(t, pool, session)
	confirmContract(t, pool, contract, 1)
	seedCatalogItem(t, pool, "keyboard-office-quiet")

	key := "key-" + randomHex(t)
	_, err := pool.Exec(ctx, `
		INSERT INTO transactions
			(principal_id, contract_id, contract_version, client_operation_key,
			 catalog_item_id, catalog_item_version, quantity, amount_minor, currency, action_fingerprint)
		VALUES ($1, $2, 1, $3, 'keyboard-office-quiet', 1, 1, 12000, 'USD', $4)`,
		session, contract, key, randomHash(t))
	if err != nil {
		t.Fatalf("first transaction: %v", err)
	}

	// Same principal + client_operation_key with a different payload
	// fingerprint (e.g. a changed proposal) is a conflict, not a duplicate:
	// the database refuses the second row regardless of payload.
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions
			(principal_id, contract_id, contract_version, client_operation_key,
			 catalog_item_id, catalog_item_version, quantity, amount_minor, currency, action_fingerprint)
		VALUES ($1, $2, 1, $3, 'keyboard-office-quiet', 1, 1, 12500, 'USD', $4)`,
		session, contract, key, randomHash(t))
	wantPgError(t, err, "23505")

	// The same key under a different principal is a different logical request.
	session2 := seedSession(t, pool)
	contract2 := seedIntentDraft(t, pool, session2)
	confirmContract(t, pool, contract2, 1)
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions
			(principal_id, contract_id, contract_version, client_operation_key,
			 catalog_item_id, catalog_item_version, quantity, amount_minor, currency, action_fingerprint)
		VALUES ($1, $2, 1, $3, 'keyboard-office-quiet', 1, 1, 12000, 'USD', $4)`,
		session2, contract2, key, randomHash(t))
	if err != nil {
		t.Fatalf("same key under a different principal should be allowed: %v", err)
	}
}

func TestAuthorityVersioningAndImmutability(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	session := seedSession(t, pool)
	contract := seedIntentDraft(t, pool, session)

	// Draft rows are still mutable-but-not-editable: the only permitted
	// update is the confirmation transition.
	_, err := pool.Exec(ctx, `
		UPDATE intent_contracts SET category = 'monitor'
		 WHERE contract_id = $1 AND version = 1`, contract)
	pgErr := wantPgError(t, err, "P0001")
	if !strings.Contains(pgErr.Message, "new version row") {
		t.Fatalf("edit of draft: want 'new version row' guidance, got %q", pgErr.Message)
	}

	// Confirming with incomplete terms violates the completeness CHECK.
	_, err = pool.Exec(ctx, `
		UPDATE intent_contracts SET confirmed_at = now()
		 WHERE contract_id = $1 AND version = 1`, contract)
	wantPgError(t, err, "23514")

	// A complete confirmation succeeds.
	confirmContract(t, pool, contract, 1)

	// Confirmed rows are immutable: content edits and re-confirmation fail.
	_, err = pool.Exec(ctx, `
		UPDATE intent_contracts SET category = 'monitor'
		 WHERE contract_id = $1 AND version = 1`, contract)
	wantPgError(t, err, "P0001")

	_, err = pool.Exec(ctx, `
		UPDATE intent_contracts SET confirmed_at = now()
		 WHERE contract_id = $1 AND version = 1`, contract)
	wantPgError(t, err, "P0001")

	// Confirmed rows cannot be deleted.
	_, err = pool.Exec(ctx, `DELETE FROM intent_contracts WHERE contract_id = $1 AND version = 1`, contract)
	wantPgError(t, err, "P0001")

	// Editing creates a new version: v2 inserts cleanly...
	_, err = pool.Exec(ctx, `
		INSERT INTO intent_contracts
			(contract_id, version, principal_id, original_request, currency,
			 max_total_minor, max_quantity, category, qualitative_preferences)
		VALUES ($1, 2, $2, 'Buy one quiet office keyboard under $170', 'USD', 17000, 1, 'keyboard', ARRAY['quiet'])`,
		contract, session)
	if err != nil {
		t.Fatalf("insert version 2: %v", err)
	}

	// ...but version numbers must be contiguous.
	_, err = pool.Exec(ctx, `
		INSERT INTO intent_contracts
			(contract_id, version, principal_id, original_request, currency,
			 max_total_minor, max_quantity, category, qualitative_preferences)
		VALUES ($1, 4, $2, 'Buy one quiet office keyboard under $180', 'USD', 18000, 1, 'keyboard', ARRAY['quiet'])`,
		contract, session)
	pgErr = wantPgError(t, err, "P0001")
	if !strings.Contains(pgErr.Message, "contiguous") {
		t.Fatalf("version gap: want 'contiguous' guidance, got %q", pgErr.Message)
	}

	// Unconfirmed drafts may be deleted (they are not authority records).
	draft2 := seedIntentDraft(t, pool, session)
	_, err = pool.Exec(ctx, `DELETE FROM intent_contracts WHERE contract_id = $1 AND version = 1`, draft2)
	if err != nil {
		t.Fatalf("delete unconfirmed draft: %v", err)
	}
}

func TestTransactionRequiresConfirmedContract(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	session := seedSession(t, pool)
	contract := seedIntentDraft(t, pool, session)
	seedCatalogItem(t, pool, "keyboard-office-quiet")

	// Unconfirmed (draft) contract: insert is refused.
	_, err := pool.Exec(ctx, `
		INSERT INTO transactions
			(principal_id, contract_id, contract_version, client_operation_key,
			 catalog_item_id, catalog_item_version, quantity, amount_minor, currency, action_fingerprint)
		VALUES ($1, $2, 1, $3, 'keyboard-office-quiet', 1, 1, 12000, 'USD', $4)`,
		session, contract, "key-"+randomHex(t), randomHash(t))
	pgErr := wantPgError(t, err, "P0001")
	if !strings.Contains(pgErr.Message, "confirmed") {
		t.Fatalf("want 'confirmed' guidance, got %q", pgErr.Message)
	}

	// After confirmation the same insert succeeds.
	confirmContract(t, pool, contract, 1)
	seedTransaction(t, pool, session, contract, 1, "keyboard-office-quiet")
}

func TestTransactionEventRelationshipIntegrity(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	session := seedSession(t, pool)
	contract := seedIntentDraft(t, pool, session)
	confirmContract(t, pool, contract, 1)
	seedCatalogItem(t, pool, "keyboard-office-quiet")
	txn := seedTransaction(t, pool, session, contract, 1, "keyboard-office-quiet")

	if _, err := pool.Exec(ctx, `
		INSERT INTO transaction_events (transaction_id, seq, event_type, source, payload)
		VALUES ($1, 1, 'TRANSACTION_CREATED', 'RUNTIME', '{"note":"seed"}')`, txn); err != nil {
		t.Fatalf("first event: %v", err)
	}

	// Sequence numbers are unique per transaction.
	_, err := pool.Exec(ctx, `
		INSERT INTO transaction_events (transaction_id, seq, event_type, source)
		VALUES ($1, 1, 'DUPLICATE_ORDER', 'RUNTIME')`, txn)
	wantPgError(t, err, "23505")

	// Events are append-only: no updates, no deletes.
	_, err = pool.Exec(ctx, `
		UPDATE transaction_events SET event_type = 'CHANGED' WHERE transaction_id = $1 AND seq = 1`, txn)
	pgErr := wantPgError(t, err, "P0001")
	if !strings.Contains(pgErr.Message, "append-only") {
		t.Fatalf("want 'append-only' guidance, got %q", pgErr.Message)
	}
	_, err = pool.Exec(ctx, `
		DELETE FROM transaction_events WHERE transaction_id = $1 AND seq = 1`, txn)
	wantPgError(t, err, "P0001")

	// The same guarantees hold for semantic verification records.
	if _, err := pool.Exec(ctx, `
		INSERT INTO semantic_verifications (transaction_id, classification, model)
		VALUES ($1, 'UNKNOWN', 'test-model')`, txn); err != nil {
		t.Fatalf("seed semantic verification: %v", err)
	}
	_, err = pool.Exec(ctx, `
		UPDATE semantic_verifications SET classification = 'SUPPORTED' WHERE transaction_id = $1`, txn)
	wantPgError(t, err, "P0001")
}

func TestWebhookEventDedupIdentity(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	providerEventID := "WH-" + randomHex(t)
	dedupKey := "dedup-" + randomHex(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_webhook_events (provider_event_id, dedup_key, verification_result)
		VALUES ($1, $2, 'VERIFIED')`, providerEventID, dedupKey); err != nil {
		t.Fatalf("first webhook event: %v", err)
	}

	// Same provider event id (a PayPal retry) is a duplicate.
	_, err := pool.Exec(ctx, `
		INSERT INTO provider_webhook_events (provider_event_id, dedup_key, verification_result)
		VALUES ($1, $2, 'VERIFIED')`, providerEventID, "dedup-"+randomHex(t))
	wantPgError(t, err, "23505")

	// Same dedup key under a different provider id is also a duplicate.
	_, err = pool.Exec(ctx, `
		INSERT INTO provider_webhook_events (provider_event_id, dedup_key, verification_result)
		VALUES ($1, $2, 'VERIFIED')`, "WH-"+randomHex(t), dedupKey)
	wantPgError(t, err, "23505")
}

func TestInvalidMoneyAndCurrency(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	session := seedSession(t, pool)
	contract := seedIntentDraft(t, pool, session)
	confirmContract(t, pool, contract, 1)
	seedCatalogItem(t, pool, "keyboard-office-quiet")

	insertTxn := func(quantity int, amount int64, currency string) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO transactions
				(principal_id, contract_id, contract_version, client_operation_key,
				 catalog_item_id, catalog_item_version, quantity, amount_minor, currency, action_fingerprint)
			VALUES ($1, $2, 1, $3, 'keyboard-office-quiet', 1, $4, $5, $6, $7)`,
			session, contract, "key-"+randomHex(t), quantity, amount, currency, randomHash(t))
		return err
	}

	cases := []struct {
		name     string
		quantity int
		amount   int64
		currency string
	}{
		{"zero amount", 1, 0, "USD"},
		{"negative amount", 1, -500, "USD"},
		{"amount above hard cap", 1, 1_000_000_001, "USD"},
		{"zero quantity", 0, 12000, "USD"},
		{"negative quantity", -1, 12000, "USD"},
		{"non-USD currency", 1, 12000, "EUR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := insertTxn(tc.quantity, tc.amount, tc.currency)
			wantPgError(t, err, "23514")
		})
	}

	// Catalog prices follow the same rules and are immutable.
	_, err := pool.Exec(ctx, `
		INSERT INTO catalog_items (item_id, version, name, description, price_minor)
		VALUES ('bad-item', 1, 'Bad', 'Bad price', 0)`)
	wantPgError(t, err, "23514")

	_, err = pool.Exec(ctx, `
		INSERT INTO catalog_items (item_id, version, name, description, price_minor, currency)
		VALUES ('bad-currency', 1, 'Bad', 'Bad currency', 100, 'EUR')`)
	wantPgError(t, err, "23514")

	_, err = pool.Exec(ctx, `
		UPDATE catalog_items SET price_minor = 999 WHERE item_id = 'keyboard-office-quiet'`)
	wantPgError(t, err, "P0001")

	_, err = pool.Exec(ctx, `
		DELETE FROM catalog_items WHERE item_id = 'keyboard-office-quiet'`)
	wantPgError(t, err, "P0001")

	// Contract monetary fields have the same bounds (checked on insert; the
	// update path against a confirmed row is blocked earlier by immutability).
	_, err = pool.Exec(ctx, `
		INSERT INTO intent_contracts
			(contract_id, version, principal_id, original_request, currency,
			 max_total_minor, max_quantity, category, qualitative_preferences)
		VALUES (gen_random_uuid(), 1, $1, 'Zero budget', 'USD', 0, 1, 'keyboard', ARRAY[]::text[])`,
		session)
	wantPgError(t, err, "23514")
}

func TestReadinessLifecycleAgainstRealDatabase(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	store, err := postgres.Open(ctx, testDSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// 1. Migrated schema -> ready.
	if code := store.Check(ctx); code != postgres.ReadinessOK {
		t.Fatalf("Check after migration = %q, want %q", code, postgres.ReadinessOK)
	}

	// 2. Migrations missing -> schema_not_ready.
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if code := store.Check(ctx); code != postgres.ReadinessSchemaNotReady {
		t.Fatalf("Check without schema = %q, want %q", code, postgres.ReadinessSchemaNotReady)
	}

	// 3. Re-migrated -> ready again.
	resetSchema(t, pool)
	if code := store.Check(ctx); code != postgres.ReadinessOK {
		t.Fatalf("Check after re-migration = %q, want %q", code, postgres.ReadinessOK)
	}

	// 4. Unreachable database -> unreachable (bounded, no hang).
	dead, err := postgres.Open(ctx, "postgres://waypoint:waypoint@127.0.0.1:1/waypoint_test?sslmode=disable")
	if err != nil {
		t.Fatalf("open unreachable store: %v", err)
	}
	defer dead.Close()
	start := time.Now()
	if code := dead.Check(ctx); code != postgres.ReadinessUnreachable {
		t.Fatalf("Check against closed port = %q, want %q", code, postgres.ReadinessUnreachable)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("unreachable check took %v; the probe should be bounded", elapsed)
	}
}
