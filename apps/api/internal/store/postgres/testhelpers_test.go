// Integration test helpers. Every test in this package runs against a real
// PostgreSQL instance — there is deliberately no in-memory substitute.
//
// The test database is separate from the development database and its name
// MUST end in "_test"; anything else is refused up front so a test run can
// never reset non-test data. Override the DSN with TEST_DATABASE_URL.
package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skcache/waypoint/apps/api/internal/store/postgres"
)

const defaultTestDSN = "postgres://waypoint:waypoint@localhost:5433/waypoint_test?sslmode=disable"

// dbNameFromDSN extracts the database name from a PostgreSQL URL DSN.
func dbNameFromDSN(t *testing.T, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("TEST_DATABASE_URL is not a valid URL (value redacted)")
	}
	return strings.TrimPrefix(u.Path, "/")
}

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	if name := dbNameFromDSN(t, dsn); !strings.HasSuffix(name, "_test") {
		t.Fatalf("refusing to run integration tests against database %q: the name must end in _test", name)
	}
	return dsn
}

// requireDatabaseTests reports whether an unreachable database must FAIL the
// run instead of skipping (REQUIRE_DATABASE_TESTS=1|true|yes).
func requireDatabaseTests() bool {
	switch strings.ToLower(os.Getenv("REQUIRE_DATABASE_TESTS")) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// verifyConnectedDatabase asks the LIVE session which database it is really
// connected to. DSN parsing and the server can disagree (parameters,
// proxying, a stale environment variable); destructive test cleanup must
// trust the server's answer, not the DSN text.
func verifyConnectedDatabase(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var current string
	err := pool.QueryRow(context.Background(), `SELECT current_database()`).Scan(&current)
	if err != nil {
		t.Fatalf("determine connected database: %v", err)
	}
	if !strings.HasSuffix(current, "_test") {
		t.Fatalf("connected database %q is not a test database (name must end in _test); refusing to continue", current)
	}
	if want := dbNameFromDSN(t, testDSN(t)); current != want {
		t.Fatalf("connected database %q does not match TEST_DATABASE_URL database %q; refusing to continue", current, want)
	}
	return current
}

// testPool returns a pool to the dedicated test database. When the database
// is unreachable the test skips with an explicit reason — unless
// REQUIRE_DATABASE_TESTS is set, in which case it fails (F07).
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := testDSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create test pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		if requireDatabaseTests() {
			t.Fatalf("database tests are REQUIRED (REQUIRE_DATABASE_TESTS=1) but PostgreSQL is unreachable: %v", err)
		}
		t.Skipf("PostgreSQL test database not reachable (%v); start it with `docker compose up -d db` or set TEST_DATABASE_URL", err)
	}
	t.Cleanup(pool.Close)
	verifyConnectedDatabase(t, pool)
	return pool
}

// resetSchema gives the test database a pristine, fully migrated schema.
// The connected database identity is re-verified immediately before the
// destructive statements (F07).
func resetSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	verifyConnectedDatabase(t, pool)
	ctx := context.Background()
	for _, stmt := range []string{
		`DROP SCHEMA IF EXISTS public CASCADE`,
		`CREATE SCHEMA public`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("reset test schema: %v", err)
		}
	}
	if _, err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test schema: %v", err)
	}
}

func randomHash(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random bytes: %v", err)
	}
	return b
}

func randomHex(t *testing.T) string {
	t.Helper()
	return hex.EncodeToString(randomHash(t))
}

// --- seed helpers (explicit values; the tests assert around them) --------

func seedSession(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO demo_sessions (token_hash, csrf_hash, expires_at)
		VALUES ($1, $2, now() + interval '1 hour')
		RETURNING id`, randomHash(t), randomHash(t)).Scan(&id)
	if err != nil {
		t.Fatalf("seed demo session: %v", err)
	}
	return id
}

// seedIntentDraft creates the contract root (authoritative owner) and inserts
// version 1 of a fully specified but unconfirmed contract; returns the
// contract id (F01 application contract: root row precedes version 1).
func seedIntentDraft(t *testing.T, pool *pgxpool.Pool, principalID string) string {
	t.Helper()
	ctx := context.Background()

	var contractID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO intent_contract_roots (contract_id, principal_id)
		VALUES (gen_random_uuid(), $1)
		RETURNING contract_id`, principalID).Scan(&contractID); err != nil {
		t.Fatalf("seed intent contract root: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO intent_contracts
			(contract_id, version, principal_id, original_request, currency,
			 max_total_minor, max_quantity, category, qualitative_preferences)
		VALUES ($1, 1, $2,
			'Buy one quiet office keyboard under $150',
			'USD', 15000, 1, 'keyboard', ARRAY['quiet', 'office use'])`,
		contractID, principalID); err != nil {
		t.Fatalf("seed intent draft: %v", err)
	}
	return contractID
}

func confirmContract(t *testing.T, pool *pgxpool.Pool, contractID string, version int) {
	t.Helper()
	confirmContractWindow(t, pool, contractID, version, 60, 1800)
}

// confirmContractWindow confirms a contract using database-time offsets:
// confirmed_at = now() - confirmedAgoSeconds, expires_at = confirmed_at +
// windowSeconds.
func confirmContractWindow(t *testing.T, pool *pgxpool.Pool, contractID string, version, confirmedAgoSeconds, windowSeconds int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		UPDATE intent_contracts
		   SET confirmed_at = now() - make_interval(secs => $3),
		       expires_at   = now() - make_interval(secs => $3) + make_interval(secs => $4),
		       canonical_hash = $5
		 WHERE contract_id = $1 AND version = $2`,
		contractID, version, confirmedAgoSeconds, windowSeconds, randomHash(t))
	if err != nil {
		t.Fatalf("confirm contract: %v", err)
	}
}

func seedCatalogItem(t *testing.T, pool *pgxpool.Pool, itemID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO catalog_items (item_id, version, name, description, price_minor)
		VALUES ($1, 1, 'Quiet Office Keyboard', 'Low-profile mechanical keyboard, tactile quiet switches', 12000)`,
		itemID)
	if err != nil {
		t.Fatalf("seed catalog item: %v", err)
	}
}

func seedTransaction(t *testing.T, pool *pgxpool.Pool, principalID, contractID string, contractVersion int, itemID string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO transactions
			(principal_id, contract_id, contract_version, client_operation_key,
			 catalog_item_id, catalog_item_version, quantity, amount_minor, currency, action_fingerprint)
		VALUES ($1, $2, $3, $4, $5, 1, 1, 12000, 'USD', $6)
		RETURNING id`,
		principalID, contractID, contractVersion, "key-"+randomHex(t), itemID, randomHash(t)).Scan(&id)
	if err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
	return id
}
