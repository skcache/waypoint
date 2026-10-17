package postgres

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skcache/waypoint/apps/api/migrations"
)

// Internal tests (package postgres) exercise unexported migration machinery:
// rollback behavior and advisory-lock lifecycle. They run against the same
// dedicated test database with the same guards as the external test package.

const internalDefaultTestDSN = "postgres://waypoint:waypoint@localhost:5433/waypoint_test?sslmode=disable"

// internalTestDSN resolves and guards the test database DSN.
func internalTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = internalDefaultTestDSN
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("TEST_DATABASE_URL is not a valid URL (value redacted)")
	}
	name := strings.TrimPrefix(u.Path, "/")
	if !strings.HasSuffix(name, "_test") {
		t.Fatalf("refusing to run against database %q: the name must end in _test", name)
	}
	return dsn
}

// assertAdvisoryLockNotStranded opens a FRESH dedicated connection — never a
// pooled one, which could be re-entrant within the same session that holds a
// stranded lock — and proves the migration advisory lock is free.
func assertAdvisoryLockNotStranded(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, internalTestDSN(t))
	if err != nil {
		t.Fatalf("open probe connection: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var held bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, migrationLockID).Scan(&held); err != nil {
		t.Fatalf("probe advisory lock: %v", err)
	}
	if !held {
		t.Fatal("the migration advisory lock is still held by some session: a failed or cancelled run stranded it")
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockID); err != nil {
		t.Fatalf("release probe lock: %v", err)
	}
}

func internalTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := internalTestDSN(t)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	name := strings.TrimPrefix(u.Path, "/")

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		switch strings.ToLower(os.Getenv("REQUIRE_DATABASE_TESTS")) {
		case "1", "true", "yes":
			t.Fatalf("database tests are REQUIRED but PostgreSQL is unreachable: %v", err)
		}
		t.Skipf("PostgreSQL test database not reachable (%v); start it with `docker compose up -d db`", err)
	}
	t.Cleanup(pool.Close)

	// Authoritative identity check against the live session (F07).
	var current string
	if err := pool.QueryRow(context.Background(), `SELECT current_database()`).Scan(&current); err != nil {
		t.Fatalf("determine connected database: %v", err)
	}
	if current != name || !strings.HasSuffix(current, "_test") {
		t.Fatalf("connected database %q does not match test target %q; refusing to continue", current, name)
	}
	return pool
}

// TestFailedMigrationRollsBack proves a migration failure is atomic: the
// partial DDL must not survive and the ledger must not record the version.
func TestFailedMigrationRollsBack(t *testing.T) {
	pool := internalTestPool(t)
	ctx := context.Background()

	for _, stmt := range []string{`DROP SCHEMA IF EXISTS public CASCADE`, `CREATE SCHEMA public`} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("prepare schema: %v", err)
		}
	}

	bad := []migrations.File{{
		Version: 1,
		Name:    "0001_boom.sql",
		SQL:     `CREATE TABLE rollback_probe_probe (id int); SELECT 1/0;`,
	}}
	if _, err := migrateFiles(ctx, pool, bad); err == nil {
		t.Fatal("expected the migration to fail")
	}

	var surviving *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.rollback_probe_probe')::text`).Scan(&surviving); err != nil {
		t.Fatalf("probe rollback: %v", err)
	}
	if surviving != nil {
		t.Fatalf("partial DDL survived a failed migration: %s exists", *surviving)
	}

	var recorded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&recorded); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if recorded != 0 {
		t.Fatalf("failed migration was recorded in schema_migrations (rows=%d)", recorded)
	}

	// A failed migration must not strand the advisory lock either.
	assertAdvisoryLockNotStranded(t)
}

// TestMigrateLockContentionAndCancellation proves (a) concurrent migration
// runs genuinely serialize on the advisory lock, (b) a cancelled run returns
// an error instead of hanging or proceeding, and (c) the cancellation does
// not leak the lock — a later run acquires it normally (F06).
func TestMigrateLockContentionAndCancellation(t *testing.T) {
	pool := internalTestPool(t)
	ctx := context.Background()

	for _, stmt := range []string{`DROP SCHEMA IF EXISTS public CASCADE`, `CREATE SCHEMA public`} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("prepare schema: %v", err)
		}
	}
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("initial migrate: %v", err)
	}

	// A separate session holds the migration lock.
	holder, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire holder connection: %v", err)
	}
	defer holder.Release()
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		t.Fatalf("hold migration lock: %v", err)
	}

	// A migrate run with a bounded context must block on the lock and then
	// fail on cancellation — not proceed.
	cancelCtx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	start := time.Now()
	_, err = Migrate(cancelCtx, pool)
	cancel()
	if err == nil {
		t.Fatal("expected Migrate to fail while the lock is held")
	}
	if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
		t.Fatalf("Migrate returned after %v without contending on the lock", elapsed)
	}

	// Release the lock, then prove from a fresh dedicated connection that the
	// cancelled run did not strand the lock anywhere.
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockID); err != nil {
		t.Fatalf("release migration lock: %v", err)
	}
	assertAdvisoryLockNotStranded(t)

	// A subsequent run must acquire the lock cleanly.
	applied, err := Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("Migrate after contention release: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("expected no new migrations, applied %v", applied)
	}
}

// TestMigrationUpgradeFromV1PreservesData exercises the full upgrade path on a
// dedicated test database: start at schema v1, seed representative data,
// apply v2 + v3, then prove the data survived unchanged, v1's checksum still
// validates, a repeat run is a no-op, and the new integrity constraints are
// active.
func TestMigrationUpgradeFromV1PreservesData(t *testing.T) {
	pool := internalTestPool(t)
	ctx := context.Background()

	files, err := migrations.All()
	if err != nil {
		t.Fatalf("embedded migrations: %v", err)
	}
	if len(files) < 3 {
		t.Fatalf("expected at least 3 embedded migrations, found %d", len(files))
	}

	for _, stmt := range []string{`DROP SCHEMA IF EXISTS public CASCADE`, `CREATE SCHEMA public`} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("prepare schema: %v", err)
		}
	}
	if _, err := migrateFiles(ctx, pool, files[:1]); err != nil {
		t.Fatalf("apply schema v1: %v", err)
	}

	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v", query, err)
		}
	}

	// Representative v1-era data across the core tables.
	mustExec(`INSERT INTO demo_sessions (id, token_hash, csrf_hash, expires_at)
	          VALUES ('dddddddd-dddd-dddd-dddd-dddddddddddd', decode(repeat('d1',32),'hex'), decode(repeat('d2',32),'hex'), now() + interval '1 day')`)
	mustExec(`INSERT INTO catalog_items (item_id, version, name, description, price_minor)
	          VALUES ('upgrade-item', 1, 'Upgrade Item', 'survives the upgrade', 4200)`)
	mustExec(`INSERT INTO intent_contracts (contract_id, version, principal_id, original_request, currency, max_total_minor, max_quantity, category,
	             canonical_hash, confirmed_at, expires_at)
	          VALUES ('eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee', 1, 'dddddddd-dddd-dddd-dddd-dddddddddddd',
	             'upgrade-era purchase', 'USD', 15000, 1, 'keyboard',
	             decode(repeat('e1',32),'hex'), now(), now() + interval '1 day')`)
	mustExec(`INSERT INTO transactions (id, principal_id, contract_id, contract_version, client_operation_key, catalog_item_id, catalog_item_version,
	             quantity, amount_minor, currency, action_fingerprint)
	          VALUES ('ffffffff-ffff-ffff-ffff-ffffffffffff', 'dddddddd-dddd-dddd-dddd-dddddddddddd',
	             'eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee', 1, 'upgrade-op-1',
	             'upgrade-item', 1, 1, 4200, 'USD', decode(repeat('f1',32),'hex'))`)
	mustExec(`INSERT INTO payment_operations (transaction_id, operation_type, paypal_request_id, payload_fingerprint)
	          VALUES ('ffffffff-ffff-ffff-ffff-ffffffffffff', 'CREATE_ORDER', 'req-upgrade-1', decode(repeat('f2',32),'hex'))`)
	mustExec(`INSERT INTO semantic_verifications (transaction_id, classification, model)
	          VALUES ('ffffffff-ffff-ffff-ffff-ffffffffffff', 'SUPPORTED', 'upgrade-model')`)
	mustExec(`INSERT INTO transaction_events (transaction_id, seq, event_type, source)
	          VALUES ('ffffffff-ffff-ffff-ffff-ffffffffffff', 1, 'TRANSACTION_CREATED', 'RUNTIME')`)
	mustExec(`INSERT INTO provider_webhook_events (provider_event_id, dedup_key, verification_result)
	          VALUES ('WH-upgrade-1', 'dedup-upgrade-1', 'VERIFIED')`)

	snapshot := func() map[string]string {
		t.Helper()
		out := make(map[string]string)
		for _, tbl := range []string{
			"demo_sessions", "intent_contracts", "catalog_items", "transactions",
			"payment_operations", "semantic_verifications", "transaction_events",
			"provider_webhook_events",
		} {
			query := `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text), '[]'::jsonb)::text FROM ` + tbl + ` t`
			var text string
			if err := pool.QueryRow(ctx, query).Scan(&text); err != nil {
				t.Fatalf("snapshot %s: %v", tbl, err)
			}
			out[tbl] = text
		}
		return out
	}

	before := snapshot()

	// The upgrade itself: v2 + v3 applied over live data.
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	after := snapshot()
	for tbl, want := range before {
		if after[tbl] != want {
			t.Fatalf("table %s changed across the upgrade\nbefore: %s\nafter:  %s", tbl, want, after[tbl])
		}
	}

	// v1's recorded checksum still validates against the embedded file.
	var recorded string
	if err := pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version = 1`).Scan(&recorded); err != nil {
		t.Fatalf("read v1 checksum: %v", err)
	}
	if recorded != fileChecksum(files[0].SQL) {
		t.Fatalf("v1 checksum drifted across the upgrade")
	}

	// The v2 backfill adopted the existing contract under its own principal.
	var principal string
	if err := pool.QueryRow(ctx,
		`SELECT principal_id::text FROM intent_contract_roots WHERE contract_id = 'eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee'`).Scan(&principal); err != nil {
		t.Fatalf("backfilled root: %v", err)
	}
	if principal != "dddddddd-dddd-dddd-dddd-dddddddddddd" {
		t.Fatalf("backfilled root principal = %q", principal)
	}

	// Repeat migration is a no-op.
	applied, err := Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("repeat migrate: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("repeat migrate applied %v, want none", applied)
	}

	// The new integrity constraints are active:
	// (a) transaction id immutability (v3).
	_, err = pool.Exec(ctx, `UPDATE transactions SET id = '99999999-0000-0000-0000-000000000001' WHERE id = 'ffffffff-ffff-ffff-ffff-ffffffffffff'`)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("v3 transaction id guard is not active: %v", err)
	}
	// (b) payment operation identity immutability (v3).
	_, err = pool.Exec(ctx, `UPDATE payment_operations SET paypal_request_id = 'req-rewritten' WHERE paypal_request_id = 'req-upgrade-1'`)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("v3 payment operation identity guard is not active: %v", err)
	}
	// (c) cross-principal authority binding (v2).
	mustExec(`INSERT INTO demo_sessions (id, token_hash, csrf_hash, expires_at)
	          VALUES ('99999999-9999-9999-9999-999999999999', decode(repeat('9a',32),'hex'), decode(repeat('9b',32),'hex'), now() + interval '1 day')`)
	_, err = pool.Exec(ctx, `
		INSERT INTO transactions (principal_id, contract_id, contract_version, client_operation_key, catalog_item_id, catalog_item_version,
		                          quantity, amount_minor, currency, action_fingerprint)
		VALUES ('99999999-9999-9999-9999-999999999999', 'eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee', 1, 'xpr-upgrade',
		        'upgrade-item', 1, 1, 4200, 'USD', decode(repeat('f3',32),'hex'))`)
	if err == nil || !strings.Contains(err.Error(), "different principal") {
		t.Fatalf("v2 cross-principal guard is not active: %v", err)
	}
}
