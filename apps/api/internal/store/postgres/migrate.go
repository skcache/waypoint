package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skcache/waypoint/apps/api/migrations"
)

// migrationLockID is an arbitrary fixed key for a PostgreSQL advisory lock
// that serializes concurrent migration runs (e.g. tests and a running
// migrate command starting at the same time).
const migrationLockID int64 = 728198431

// lockCleanupTimeout bounds the deferred unlock attempt so a cancelled
// migrate call cannot hang during cleanup (F06).
const lockCleanupTimeout = 5 * time.Second

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
	files, err := migrations.All()
	if err != nil {
		return nil, err
	}
	return migrateFiles(ctx, pool, files)
}

// migrateFiles applies an explicit, version-ordered migration set. It is the
// internal seam the tests use to exercise failure and rollback behavior
// without shipping deliberately broken migrations.
func migrateFiles(ctx context.Context, pool *pgxpool.Pool, files []migrations.File) ([]int64, error) {
	ordered := append([]migrations.File(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Version < ordered[j].Version })

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire migration connection: %w", err)
	}
	// Exactly one deferred closure manages both the advisory lock and the
	// pooled connection, in that order (F06):
	//   - the unlock runs under a bounded cleanup context, so a cancelled
	//     migrate call cannot block here;
	//   - if the unlock cannot be confirmed, the underlying session is
	//     destroyed (Hijack + Close) so a still-held session-level lock can
	//     never leak back into the pool and deadlock later migration runs.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), lockCleanupTimeout)
		defer cancel()
		if _, err := conn.Exec(cleanupCtx, `SELECT pg_advisory_unlock($1)`, migrationLockID); err != nil {
			raw := conn.Hijack()
			_ = raw.Close(cleanupCtx)
			return
		}
		conn.Release()
	}()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}

	if _, err := conn.Exec(ctx, createMigrationsTableSQL); err != nil {
		return nil, fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	applied, err := loadApplied(ctx, conn)
	if err != nil {
		return nil, err
	}

	var newlyApplied []int64
	for _, m := range ordered {
		checksum := fileChecksum(m.SQL)

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

// fileChecksum is the hex SHA-256 of a migration's contents, used both for
// the apply-time ledger and for readiness verification.
func fileChecksum(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
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
