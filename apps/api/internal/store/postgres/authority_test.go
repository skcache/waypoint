package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/store/postgres"
	"github.com/skcache/waypoint/apps/api/migrations"
)

// ---------------------------------------------------------------------------
// F01 — cross-principal authorization binding
// ---------------------------------------------------------------------------

func TestCrossPrincipalAuthorityBinding(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	sessionA := seedSession(t, pool)
	contractA := seedIntentDraft(t, pool, sessionA)
	confirmContract(t, pool, contractA, 1)
	seedCatalogItem(t, pool, "keyboard-office-quiet")
	sessionB := seedSession(t, pool)

	// Same principal: succeeds.
	seedTransaction(t, pool, sessionA, contractA, 1, "keyboard-office-quiet")

	// Principal B referencing principal A's contract: refused.
	_, err := pool.Exec(ctx, `
		INSERT INTO transactions
			(principal_id, contract_id, contract_version, client_operation_key,
			 catalog_item_id, catalog_item_version, quantity, amount_minor, currency, action_fingerprint)
		VALUES ($1, $2, 1, $3, 'keyboard-office-quiet', 1, 1, 12000, 'USD', $4)`,
		sessionB, contractA, "key-"+randomHex(t), randomHash(t))
	pgErr := wantPgError(t, err, "P0001")
	if !strings.Contains(pgErr.Message, "different principal") {
		t.Fatalf("want principal guidance, got %q", pgErr.Message)
	}

	// A new version of a contract cannot change its owner (declarative
	// composite FK to the contract root).
	_, err = pool.Exec(ctx, `
		INSERT INTO intent_contracts
			(contract_id, version, principal_id, original_request, currency,
			 max_total_minor, max_quantity, category, qualitative_preferences)
		VALUES ($1, 2, $2, 'Hijack attempt', 'USD', 15000, 1, 'keyboard', ARRAY[]::text[])`,
		contractA, sessionB)
	wantPgError(t, err, "23503")
}

func TestTransactionRequiresExistingAuthority(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	session := seedSession(t, pool)
	seedCatalogItem(t, pool, "keyboard-office-quiet")

	// An unknown contract version cannot back a transaction.
	_, err := pool.Exec(ctx, `
		INSERT INTO transactions
			(principal_id, contract_id, contract_version, client_operation_key,
			 catalog_item_id, catalog_item_version, quantity, amount_minor, currency, action_fingerprint)
		VALUES ($1, gen_random_uuid(), 1, $2, 'keyboard-office-quiet', 1, 1, 12000, 'USD', $3)`,
		session, "key-"+randomHex(t), randomHash(t))
	pgErr := wantPgError(t, err, "P0001")
	if !strings.Contains(pgErr.Message, "unknown") {
		t.Fatalf("want unknown-contract guidance, got %q", pgErr.Message)
	}
}

// ---------------------------------------------------------------------------
// F02 — authorized transaction details are immutable
// ---------------------------------------------------------------------------

func TestAuthorizedTransactionDetailsAreImmutable(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	session := seedSession(t, pool)
	contract := seedIntentDraft(t, pool, session)
	confirmContract(t, pool, contract, 1)
	seedCatalogItem(t, pool, "keyboard-office-quiet")
	txn := seedTransaction(t, pool, session, contract, 1, "keyboard-office-quiet")

	// Every statement rewrites one authorized detail. The guard must fire
	// before any constraint, for every protected column.
	updates := []struct{ name, stmt string }{
		{"principal_id", `UPDATE transactions SET principal_id = '99999999-9999-9999-9999-999999999999' WHERE id = $1`},
		{"contract_id", `UPDATE transactions SET contract_id = '99999999-9999-9999-9999-999999999998' WHERE id = $1`},
		{"contract_version", `UPDATE transactions SET contract_version = 2 WHERE id = $1`},
		{"client_operation_key", `UPDATE transactions SET client_operation_key = 'rewritten' WHERE id = $1`},
		{"catalog_item_id", `UPDATE transactions SET catalog_item_id = 'rewritten' WHERE id = $1`},
		{"catalog_item_version", `UPDATE transactions SET catalog_item_version = 2 WHERE id = $1`},
		{"quantity", `UPDATE transactions SET quantity = 5 WHERE id = $1`},
		{"amount_minor", `UPDATE transactions SET amount_minor = 99999 WHERE id = $1`},
		{"currency", `UPDATE transactions SET currency = 'EUR' WHERE id = $1`},
		{"action_fingerprint", `UPDATE transactions SET action_fingerprint = decode(repeat('ff',32),'hex') WHERE id = $1`},
		{"created_at", `UPDATE transactions SET created_at = created_at - interval '1 day' WHERE id = $1`},
	}
	for _, u := range updates {
		t.Run(u.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, u.stmt, txn)
			pgErr := wantPgError(t, err, "P0001")
			if !strings.Contains(pgErr.Message, "immutable") {
				t.Fatalf("want immutability guidance, got %q", pgErr.Message)
			}
		})
	}

	// Execution state remains mutable.
	if _, err := pool.Exec(ctx, `UPDATE transactions SET state = 'VERIFYING' WHERE id = $1`, txn); err != nil {
		t.Fatalf("state update must be allowed: %v", err)
	}
	var state string
	var updatedAt, createdAt time.Time
	if err := pool.QueryRow(ctx,
		`SELECT state, updated_at, created_at FROM transactions WHERE id = $1`, txn).
		Scan(&state, &updatedAt, &createdAt); err != nil {
		t.Fatalf("read transaction: %v", err)
	}
	if state != "VERIFYING" {
		t.Fatalf("state = %q, want VERIFYING", state)
	}
	if updatedAt.Before(createdAt) {
		t.Fatalf("updated_at went backwards: %v < %v", updatedAt, createdAt)
	}
}

// ---------------------------------------------------------------------------
// F03 — contract validity at creation time (database time)
// ---------------------------------------------------------------------------

func TestTransactionRequiresUnexpiredContract(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	session := seedSession(t, pool)
	seedCatalogItem(t, pool, "keyboard-office-quiet")

	insertTxn := func(contractID string) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO transactions
				(principal_id, contract_id, contract_version, client_operation_key,
				 catalog_item_id, catalog_item_version, quantity, amount_minor, currency, action_fingerprint)
			VALUES ($1, $2, 1, $3, 'keyboard-office-quiet', 1, 1, 12000, 'USD', $4)`,
			session, contractID, "key-"+randomHex(t), randomHash(t))
		return err
	}

	// Expired: confirmed 2h ago, window closed 1h ago.
	expired := seedIntentDraft(t, pool, session)
	confirmContractWindow(t, pool, expired, 1, 7200, 3600)
	pgErr := wantPgError(t, insertTxn(expired), "P0001")
	if !strings.Contains(pgErr.Message, "expired") {
		t.Fatalf("want expiry guidance, got %q", pgErr.Message)
	}

	// Boundary: expires_at == now() at confirmation moment; any later insert
	// is past the boundary and must be refused.
	boundary := seedIntentDraft(t, pool, session)
	confirmContractWindow(t, pool, boundary, 1, 3600, 3600)
	pgErr = wantPgError(t, insertTxn(boundary), "P0001")
	if !strings.Contains(pgErr.Message, "expired") {
		t.Fatalf("boundary case: want expiry guidance, got %q", pgErr.Message)
	}

	// Valid authority still works.
	valid := seedIntentDraft(t, pool, session)
	confirmContract(t, pool, valid, 1)
	if err := insertTxn(valid); err != nil {
		t.Fatalf("valid contract must be accepted: %v", err)
	}
}

// ---------------------------------------------------------------------------
// F05 — readiness detects and never mutates schema drift
// ---------------------------------------------------------------------------

func TestReadinessDetectsSchemaDrift(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	store, err := postgres.Open(ctx, testDSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	check := func(want, label string) {
		t.Helper()
		if got := store.Check(ctx); got != want {
			t.Fatalf("%s: Check = %q, want %q", label, got, want)
		}
	}
	check(postgres.ReadinessOK, "freshly migrated")

	// Tampered checksum -> drift detected, and readiness must NOT repair it.
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET checksum = 'tampered' WHERE version = 1`); err != nil {
		t.Fatalf("tamper checksum: %v", err)
	}
	check(postgres.ReadinessSchemaNotReady, "tampered checksum")
	var stored string
	if err := pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version = 1`).Scan(&stored); err != nil {
		t.Fatalf("read checksum: %v", err)
	}
	if stored != "tampered" {
		t.Fatalf("readiness mutated migration history (checksum now %q)", stored)
	}

	// Restore the true checksum.
	files, err := migrations.All()
	if err != nil {
		t.Fatalf("embedded migrations: %v", err)
	}
	sum := sha256.Sum256([]byte(files[0].SQL))
	if _, err := pool.Exec(ctx,
		`UPDATE schema_migrations SET checksum = $1 WHERE version = 1`, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("restore checksum: %v", err)
	}
	check(postgres.ReadinessOK, "restored checksum")

	// Renamed migration -> drift.
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET name = 'renamed.sql' WHERE version = 1`); err != nil {
		t.Fatalf("rename migration: %v", err)
	}
	check(postgres.ReadinessSchemaNotReady, "renamed migration")
	if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET name = $1 WHERE version = 1`, files[0].Name); err != nil {
		t.Fatalf("restore name: %v", err)
	}
	check(postgres.ReadinessOK, "restored name")

	// Unknown extra version -> inconsistent history.
	if _, err := pool.Exec(ctx,
		`INSERT INTO schema_migrations (version, name, checksum) VALUES (99, '9999_future.sql', 'unknown')`); err != nil {
		t.Fatalf("insert unknown version: %v", err)
	}
	check(postgres.ReadinessSchemaNotReady, "unknown extra version")
	if _, err := pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version = 99`); err != nil {
		t.Fatalf("remove unknown version: %v", err)
	}
	check(postgres.ReadinessOK, "history consistent again")

	// Missing schema entirely, then recovered by migration.
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	check(postgres.ReadinessSchemaNotReady, "schema missing")
	resetSchema(t, pool)
	check(postgres.ReadinessOK, "re-migrated")
}

// ---------------------------------------------------------------------------
// F09 — store close
// ---------------------------------------------------------------------------

func TestStoreCloseClosesPool(t *testing.T) {
	// No live database needed: Open only parses, and Check must fail closed
	// on a closed pool.
	store, err := postgres.Open(context.Background(), defaultTestDSN)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	store.Close()
	if got := store.Check(context.Background()); got != postgres.ReadinessUnreachable {
		t.Fatalf("Check after Close = %q, want %q", got, postgres.ReadinessUnreachable)
	}
}
