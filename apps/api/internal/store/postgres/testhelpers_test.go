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

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("TEST_DATABASE_URL is not a valid URL (value redacted)")
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if !strings.HasSuffix(dbName, "_test") {
		t.Fatalf("refusing to run integration tests against database %q: the name must end in _test", dbName)
	}
	return dsn
}

// testPool returns a pool to the dedicated test database. When the database
// is not reachable the test skips with an explicit reason, so `go test ./...`
// stays green on machines without the dev stack running.
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
		t.Skipf("PostgreSQL test database not reachable (%v); start it with `docker compose up -d db` or set TEST_DATABASE_URL", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// resetSchema gives the test database a pristine, fully migrated schema.
func resetSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
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

// seedIntentDraft inserts version 1 of a fully specified but unconfirmed
// contract and returns its contract id.
func seedIntentDraft(t *testing.T, pool *pgxpool.Pool, principalID string) string {
	t.Helper()
	var contractID string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO intent_contracts
			(contract_id, version, principal_id, original_request, currency,
			 max_total_minor, max_quantity, category, qualitative_preferences)
		VALUES (gen_random_uuid(), 1, $1,
			'Buy one quiet office keyboard under $150',
			'USD', 15000, 1, 'keyboard', ARRAY['quiet', 'office use'])
		RETURNING contract_id`, principalID).Scan(&contractID)
	if err != nil {
		t.Fatalf("seed intent draft: %v", err)
	}
	return contractID
}

func confirmContract(t *testing.T, pool *pgxpool.Pool, contractID string, version int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		UPDATE intent_contracts
		   SET confirmed_at = now(),
		       expires_at = now() + interval '30 minutes',
		       canonical_hash = $3
		 WHERE contract_id = $1 AND version = $2`,
		contractID, version, randomHash(t))
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
