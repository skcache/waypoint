// Package postgres owns the database connection pool, the SQL migration
// runner, and the readiness check exposed by the API.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skcache/waypoint/apps/api/migrations"
)

// Readiness status codes reported by Store.Check. They are machine-readable
// and deliberately contain no connection details or credentials.
const (
	ReadinessOK              = "ok"
	ReadinessUnreachable     = "unreachable"
	ReadinessSchemaNotReady  = "schema_not_ready"
	ReadinessNotConfigured   = "not_configured"
)

// Pool bounds. V1 is a single modest API process; keep the pool small and
// connections short-lived enough to survive a bounced container.
const (
	poolMaxConns        = 10
	poolMinConns        = 1
	poolMaxConnLifetime = 30 * time.Minute
	poolMaxConnIdleTime = 5 * time.Minute
	connConnectTimeout  = 5 * time.Second
	readinessTimeout    = 2 * time.Second
)

// Store wraps the PostgreSQL connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// Open parses the DSN and creates a lazy connection pool. It performs no
// network I/O: a database that is currently down does not prevent the API
// process from starting — readiness reporting surfaces that state instead.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// The wrapped error is intentionally discarded: pgx parse errors can
		// echo the raw DSN (including a password) into logs.
		return nil, fmt.Errorf("invalid DATABASE_URL (value redacted); want a postgres:// or postgresql:// URL")
	}
	cfg.MaxConns = poolMaxConns
	cfg.MinConns = poolMinConns
	cfg.MaxConnLifetime = poolMaxConnLifetime
	cfg.MaxConnIdleTime = poolMaxConnIdleTime
	cfg.ConnConfig.ConnectTimeout = connConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create database connection pool (details redacted)")
	}
	return &Store{pool: pool}, nil
}

// Pool exposes the underlying pool to the migration command. Application
// code should prefer Store methods.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// Close releases the pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Check implements the API readiness contract: it returns ReadinessOK only
// when the database answers a bounded probe AND the applied schema version
// covers every migration embedded in this binary. Every other outcome
// returns a non-secret reason code.
func (s *Store) Check(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		return ReadinessUnreachable
	}

	var applied int64
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&applied)
	if err != nil {
		// Missing schema_migrations table (or any other query failure) means
		// the schema is not ready from this binary's point of view.
		return ReadinessSchemaNotReady
	}

	required, err := migrations.Latest()
	if err != nil {
		return ReadinessSchemaNotReady
	}
	if applied < required {
		return ReadinessSchemaNotReady
	}
	return ReadinessOK
}
