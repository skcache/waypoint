// Integration test helpers for the merchant fixture. Every test runs against
// a real PostgreSQL instance (there is deliberately no in-memory substitute),
// and the connected database name MUST end in "_test" or the run aborts.
package merchant_test

import (
	"context"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
	"github.com/skcache/waypoint/apps/api/internal/store/postgres"
)

// defaultTestDSN points at a SEPARATE database from the V1 postgres
// integration tests on purpose: both suites drop and rebuild their schema,
// and two packages doing that to one database in parallel (go test ./...)
// would corrupt each other's runs. The database is created on demand.
// This is the LOCAL development default (matching docker-compose); override
// with TEST_DATABASE_URL, or rely on DATABASE_URL derivation below.
const defaultTestDSN = "postgres://waypoint:waypoint@localhost:5433/waypoint_merchant_test?sslmode=disable"

var dbNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// deriveTestDSN swaps the database name in a base DSN, keeping host, port,
// credentials and options — so `go test` works with only DATABASE_URL set
// (make targets load .env automatically) while no credential is ever
// committed to Git.
func deriveTestDSN(base, dbName string) (string, bool) {
	u, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	u.Path = "/" + dbName
	return u.String(), true
}

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if base := os.Getenv("DATABASE_URL"); base != "" {
			if derived, ok := deriveTestDSN(base, "waypoint_merchant_test"); ok {
				dsn = derived
			}
		}
	}
	if dsn == "" {
		dsn = defaultTestDSN
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("TEST_DATABASE_URL is not a valid URL (value redacted)")
	}
	name := strings.TrimPrefix(u.Path, "/")
	if !strings.HasSuffix(name, "_test") {
		t.Fatalf("refusing to run merchant tests against database %q: the name must end in _test", name)
	}
	if !dbNamePattern.MatchString(name) {
		t.Fatalf("database name %q is not a simple lowercase identifier; refusing to interpolate it into CREATE DATABASE", name)
	}
	return dsn
}

// ensureDatabaseExists connects to the maintenance database and creates the
// test database when it is missing. The name was validated by testDSN.
func ensureDatabaseExists(t *testing.T, dsn string) {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse test DSN (value redacted)")
	}
	name := strings.TrimPrefix(u.Path, "/")

	admin := *u
	admin.Path = "/postgres"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, admin.String())
	if err != nil {
		t.Fatalf("connect to maintenance database: %v", err)
	}
	defer pool.Close()
	if pool.Ping(ctx) != nil {
		return // server unreachable; testPool below decides skip vs fail
	}
	if _, err := pool.Exec(ctx, `CREATE DATABASE `+name); err != nil &&
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("create test database %s: %v", name, err)
	}
}

func requireDatabaseTests() bool {
	switch strings.ToLower(os.Getenv("REQUIRE_DATABASE_TESTS")) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// newFixtureStore gives the test a pristine migrated schema in the dedicated
// test database, with the fixture catalog seeded.
func newFixtureStore(t *testing.T) *merchant.Store {
	t.Helper()
	dsn := testDSN(t)
	ensureDatabaseExists(t, dsn)

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
		t.Skipf("PostgreSQL test database not reachable (%v); start it with `docker compose up -d db`", err)
	}

	var current string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&current); err != nil {
		pool.Close()
		t.Fatalf("determine connected database: %v", err)
	}
	if !strings.HasSuffix(current, "_test") {
		pool.Close()
		t.Fatalf("connected database %q is not a test database; refusing to continue", current)
	}

	for _, stmt := range []string{`DROP SCHEMA IF EXISTS public CASCADE`, `CREATE SCHEMA public`} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			t.Fatalf("reset test schema: %v", err)
		}
	}
	if _, err := postgres.Migrate(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("migrate test schema: %v", err)
	}

	store := merchant.NewStore(pool)
	t.Cleanup(store.Close)
	if err := store.SeedProducts(ctx); err != nil {
		t.Fatalf("seed products: %v", err)
	}
	return store
}

// fakePayPal is a TEST-ONLY deterministic provider stand-in used to exercise
// the service's ordering and classification logic. The live demo and the M1
// acceptance run always go through the real sandbox paypal.Client
// (cmd/merchant); this fake never powers a user-visible result.
type fakePayPal struct {
	createFn  func(in paypal.CreateOrderInput, requestID string) (*paypal.Order, error)
	getFn     func(orderID string) (*paypal.Order, error)
	captureFn func(orderID, requestID string) (*paypal.Order, error)

	createCalls  []paypal.CreateOrderInput
	createIDs    []string
	captureCalls []string
	getCalls     []string
}

func (f *fakePayPal) CreateOrder(_ context.Context, in paypal.CreateOrderInput, requestID string) (*paypal.Order, error) {
	f.createCalls = append(f.createCalls, in)
	f.createIDs = append(f.createIDs, requestID)
	if f.createFn != nil {
		return f.createFn(in, requestID)
	}
	return &paypal.Order{
		ID:     "ORD-TEST-1",
		Status: paypal.OrderStatusCreated,
		PurchaseUnits: []paypal.PurchaseUnit{{
			Amount: paypal.Money{CurrencyCode: in.CurrencyCode, Value: in.AmountValue},
		}},
		Links: []paypal.Link{{Rel: "payer-action", Href: "https://sandbox.paypal.example/checkoutnow?token=ORD-TEST-1"}},
	}, nil
}

func (f *fakePayPal) GetOrder(_ context.Context, orderID string) (*paypal.Order, error) {
	f.getCalls = append(f.getCalls, orderID)
	if f.getFn != nil {
		return f.getFn(orderID)
	}
	return &paypal.Order{ID: orderID, Status: paypal.OrderStatusApproved}, nil
}

func (f *fakePayPal) CaptureOrder(_ context.Context, orderID, requestID string) (*paypal.Order, error) {
	f.captureCalls = append(f.captureCalls, requestID)
	if f.captureFn != nil {
		return f.captureFn(orderID, requestID)
	}
	return nil, &paypal.APIError{StatusCode: 500, Name: "INTERNAL_SERVER_ERROR"}
}

// approvedOrder builds a provider order in APPROVED state carrying the given
// amount, as the preflight read would return it.
func approvedOrder(orderID, currencyCode, value string) *paypal.Order {
	return &paypal.Order{
		ID:     orderID,
		Status: paypal.OrderStatusApproved,
		PurchaseUnits: []paypal.PurchaseUnit{{
			Amount: paypal.Money{CurrencyCode: currencyCode, Value: value},
		}},
	}
}

// completedOrder builds a provider order with a COMPLETED capture.
func completedOrder(orderID, captureID, currencyCode, value string) *paypal.Order {
	return &paypal.Order{
		ID:     orderID,
		Status: paypal.OrderStatusCompleted,
		PurchaseUnits: []paypal.PurchaseUnit{{
			Amount: paypal.Money{CurrencyCode: currencyCode, Value: value},
			Payments: paypal.PaymentCollection{Captures: []paypal.Capture{{
				ID: captureID, Status: paypal.CaptureStatusCompleted,
				Amount: paypal.Money{CurrencyCode: currencyCode, Value: value},
			}}},
		}},
	}
}
