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
	ReadinessOK             = "ok"
	ReadinessUnreachable    = "unreachable"
	ReadinessSchemaNotReady = "schema_not_ready"
	ReadinessNotConfigured  = "not_configured"
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
// when the database answers a bounded probe AND the recorded migration
// history is exactly the set embedded in this binary — every version present
// with the same name and checksum, and nothing unknown on top. This detects
// missing, modified, and inconsistent migration metadata (F05).
//
// Readiness is strictly read-only: it never applies migrations.
func (s *Store) Check(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		return ReadinessUnreachable
	}

	expected, err := migrations.All()
	if err != nil {
		return ReadinessSchemaNotReady
	}

	rows, err := s.pool.Query(ctx, `SELECT version, name, checksum FROM schema_migrations`)
	if err != nil {
		// Missing schema_migrations table (or any other query failure) means
		// the schema is not ready from this binary's point of view.
		return ReadinessSchemaNotReady
	}
	defer rows.Close()

	type appliedRecord struct{ name, checksum string }
	applied := make(map[int64]appliedRecord)
	for rows.Next() {
		var version int64
		var record appliedRecord
		if err := rows.Scan(&version, &record.name, &record.checksum); err != nil {
			return ReadinessSchemaNotReady
		}
		applied[version] = record
	}
	if err := rows.Err(); err != nil {
		return ReadinessSchemaNotReady
	}

	if len(applied) != len(expected) {
		return ReadinessSchemaNotReady
	}
	for _, file := range expected {
		record, ok := applied[file.Version]
		if !ok || record.name != file.Name || record.checksum != fileChecksum(file.SQL) {
			return ReadinessSchemaNotReady
		}
	}
	return ReadinessOK
}
