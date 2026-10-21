package replay

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/store/postgres"
)

// defaultOracleDSN is the LOCAL development default (same convention as the
// V1 and merchant suites: separate database per suite so parallel schema
// rebuilds cannot corrupt each other; docker-compose's local dev credentials).
// TEST_DATABASE_URL or DATABASE_URL derivation takes precedence.
const defaultOracleDSN = "postgres://waypoint:waypoint@localhost:5433/waypoint_oracle_test?sslmode=disable"

var dbNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Harness owns a migrated, seeded fixture database for scenario runs.
type Harness struct {
	Store *merchant.Store
	Pool  *pgxpool.Pool
}

// OpenHarness connects to the oracle test database (creating + migrating +
// seeding it as needed). Resolution order: explicit DSN → TEST_DATABASE_URL →
// derived from DATABASE_URL (same host/credentials, database name swapped).
// The connected database name MUST end in "_test". Real credentials are never
// committed: they arrive via environment (.env is loaded by the Makefile).
func OpenHarness(ctx context.Context, dsn string) (*Harness, error) {
	resolved, err := ResolveDSN(dsn)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(resolved)
	if err != nil {
		return nil, fmt.Errorf("oracle harness: DSN is not a valid URL (value redacted)")
	}
	name := strings.TrimPrefix(u.Path, "/")
	return openNamed(ctx, u, name)
}

// ResolveDSN applies the documented resolution order and returns the concrete
// test DSN (explicit → TEST_DATABASE_URL → derived from DATABASE_URL →
// local default), validating the _test suffix. Callers that execute the
// verifier in a child process need the resolved value, not the empty input.
func ResolveDSN(dsn string) (string, error) {
	return ResolveDSNFor(dsn, "waypoint_oracle_test")
}

// ResolveDSNFor is ResolveDSN with an explicit default database name. Test
// packages use distinct names so `go test ./...` (which runs packages in
// parallel) cannot have two suites rebuilding the same schema.
func ResolveDSNFor(dsn, dbName string) (string, error) {
	if dsn == "" {
		dsn = os.Getenv("TEST_DATABASE_URL")
	}
	if dsn == "" {
		if base := os.Getenv("DATABASE_URL"); base != "" {
			if derived, ok := deriveTestDSN(base, dbName); ok {
				dsn = derived
			}
		}
	}
	if dsn == "" {
		dsn = withDBName(defaultOracleDSN, dbName)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("oracle harness: DSN is not a valid URL (value redacted)")
	}
	name := strings.TrimPrefix(u.Path, "/")
	if !strings.HasSuffix(name, "_test") {
		return "", fmt.Errorf("oracle harness: refusing database %q (name must end in _test)", name)
	}
	if !dbNamePattern.MatchString(name) {
		return "", fmt.Errorf("oracle harness: database name %q is not a simple identifier", name)
	}
	return dsn, nil
}

// deriveTestDSN swaps the database name in a base DSN, keeping host, port,
// credentials and options.
func deriveTestDSN(base, dbName string) (string, bool) {
	u, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	u.Path = "/" + dbName
	return u.String(), true
}

// OpenHarnessForTests is the test-side entry point with the same guards.
func OpenHarnessForTestsNamed(ctx context.Context, dsn, dbName string) (*Harness, error) {
	resolved, err := ResolveDSNFor(dsn, dbName)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(resolved)
	if err != nil {
		return nil, fmt.Errorf("oracle harness: DSN is not a valid URL (value redacted)")
	}
	name := strings.TrimPrefix(u.Path, "/")
	return openNamed(ctx, u, name)
}

func OpenHarnessForTests(ctx context.Context, dsn string) (*Harness, error) {
	return OpenHarness(ctx, dsn)
}

func openNamed(ctx context.Context, u *url.URL, name string) (*Harness, error) {
	admin := *u
	admin.Path = "/postgres"
	adminPool, err := pgxpool.New(ctx, admin.String())
	if err != nil {
		return nil, fmt.Errorf("oracle harness: connect maintenance db: %w", err)
	}
	pingErr := adminPool.Ping(ctx)
	if pingErr == nil {
		if _, err := adminPool.Exec(ctx, `CREATE DATABASE `+name); err != nil &&
			!strings.Contains(err.Error(), "already exists") {
			adminPool.Close()
			return nil, fmt.Errorf("oracle harness: create test database %s: %w", name, err)
		}
	}
	adminPool.Close()
	if pingErr != nil {
		return nil, fmt.Errorf("oracle harness: PostgreSQL unreachable at the configured test DSN: %w", pingErr)
	}

	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	var current string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&current); err != nil {
		pool.Close()
		return nil, err
	}
	if !strings.HasSuffix(current, "_test") {
		pool.Close()
		return nil, fmt.Errorf("oracle harness: connected database %q is not a test database", current)
	}
	for _, stmt := range []string{`DROP SCHEMA IF EXISTS public CASCADE`, `CREATE SCHEMA public`} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			return nil, fmt.Errorf("oracle harness: reset schema: %w", err)
		}
	}
	migCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := postgres.Migrate(migCtx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("oracle harness: migrate: %w", err)
	}
	store := merchant.NewStore(pool)
	if err := store.SeedProducts(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Harness{Store: store, Pool: pool}, nil
}

// Close releases the pool.
// withDBName replaces the database name in a base DSN while keeping
// credentials, host and options.
func withDBName(base, name string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	u.Path = "/" + name
	return u.String()
}

func (h *Harness) Close() {
	if h != nil && h.Pool != nil {
		h.Pool.Close()
	}
}

// ResetFixtureState clears transactional fixture state between scenario runs
// so no financial state carries over (catalog kept).
func (h *Harness) ResetFixtureState(ctx context.Context) error {
	return h.Store.Reset(ctx)
}

// NewEnv builds a runner Env for a scenario + candidate.
func (h *Harness) NewEnv(sc Scenario, cand Candidate) *Env {
	provider := NewScriptedProvider(h.Pool, sc.Schedule)
	if sc.CaptureAmountOverride != "" {
		provider.CaptureAmountOverride = sc.CaptureAmountOverride
	}
	return &Env{
		Store:    h.Store,
		Pool:     h.Pool,
		Provider: provider,
		NewDriver: func(c Candidate) Driver {
			svc := merchant.NewService(h.Store, provider, merchant.FaultNone, "")
			// Provenance at the ORIGIN: this service is wired to a scripted
			// fixture provider, so every provider-derived journal event it
			// writes must carry tier F — never a claim of real Sandbox
			// evidence.
			svc.SetProviderEvidenceTier(merchant.TierFault)
			switch c {
			case CandidateReference:
				return svc
			case CandidateUnsafeIdentitySwap:
				return NewUnsafeDriver("identity-swap", merchant.TierFault, svc, h.Store, provider)
			case CandidateUnsafeAcceptMismatch:
				return NewUnsafeDriver("accept-mismatch", merchant.TierFault, svc, h.Store, provider)
			default:
				panic("replay: unknown candidate " + string(c))
			}
		},
	}
}
