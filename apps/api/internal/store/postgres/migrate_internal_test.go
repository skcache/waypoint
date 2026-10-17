package postgres

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skcache/waypoint/apps/api/migrations"
)

// Internal tests (package postgres) exercise unexported migration machinery:
// rollback behavior and advisory-lock lifecycle. They run against the same
// dedicated test database with the same guards as the external test package.

const internalDefaultTestDSN = "postgres://waypoint:waypoint@localhost:5433/waypoint_test?sslmode=disable"

func internalTestPool(t *testing.T) *pgxpool.Pool {
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

	// Release the lock; a subsequent run must acquire it cleanly. This also
	// verifies the cancelled run did not leak a lock onto its connection.
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockID); err != nil {
		t.Fatalf("release migration lock: %v", err)
	}
	applied, err := Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("Migrate after contention release: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("expected no new migrations, applied %v", applied)
	}
}
