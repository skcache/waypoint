package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skcache/waypoint/apps/api/migrations"
)

// migrationLockID is an arbitrary fixed key for a PostgreSQL advisory lock
// that serializes concurrent migration runs (e.g. tests and a running
// migrate command starting at the same time).
const migrationLockID int64 = 728198431

const createMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    bigint PRIMARY KEY,
    name       text NOT NULL,
    checksum   text NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
)`

// Migrate applies every embedded migration that has not been applied yet, in
// version order, each inside its own transaction. It returns the versions
// applied by this call (empty when the database is already current).
//
// Safety properties:
//   - Runs are serialized with an advisory lock held for the full run.
//   - Already-applied migrations are verified against their recorded SHA-256
//     checksum; a modified file aborts the run with an explicit error.
//   - Failed migrations roll back atomically and are not recorded.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]int64, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		// Unlock even if the caller's context was cancelled.
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	if _, err := conn.Exec(ctx, createMigrationsTableSQL); err != nil {
		return nil, fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	applied, err := loadApplied(ctx, conn)
	if err != nil {
		return nil, err
	}

	all, err := migrations.All()
	if err != nil {
		return nil, err
	}

	var newlyApplied []int64
	for _, m := range all {
		sum := sha256.Sum256([]byte(m.SQL))
		checksum := hex.EncodeToString(sum[:])

		if previous, ok := applied[m.Version]; ok {
			if previous != checksum {
				return newlyApplied, fmt.Errorf(
					"migration %s was modified after being applied (checksum mismatch); refusing to continue",
					m.Name)
			}
			continue
		}

		if err := applyOne(ctx, conn, m, checksum); err != nil {
			return newlyApplied, err
		}
		newlyApplied = append(newlyApplied, m.Version)
	}

	return newlyApplied, nil
}

func applyOne(ctx context.Context, conn *pgxpool.Conn, m migrations.File, checksum string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction for %s: %w", m.Name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful commit

	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return fmt.Errorf("apply migration %s: %w", m.Name, err)
	}
	const insertSQL = `INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)`
	if _, err := tx.Exec(ctx, insertSQL, m.Version, m.Name, checksum); err != nil {
		return fmt.Errorf("record migration %s: %w", m.Name, err)
	}
	return tx.Commit(ctx)
}

func loadApplied(ctx context.Context, conn *pgxpool.Conn) (map[int64]string, error) {
	rows, err := conn.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int64]string)
	for rows.Next() {
		var version int64
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = checksum
	}
	return applied, rows.Err()
}

// compile-time guard that pgx stays a direct dependency of this package
var _ = pgx.ErrNoRows
