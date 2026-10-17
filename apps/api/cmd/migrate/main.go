// Command migrate applies the embedded SQL migrations to the database named
// by DATABASE_URL. Usage:
//
//	DATABASE_URL=postgres://... go run ./cmd/migrate
//
// The command prints the versions it applied; running it again on an
// up-to-date database is a no-op. It never prints credentials.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/config"
	"github.com/skcache/waypoint/apps/api/internal/store/postgres"
	"github.com/skcache/waypoint/apps/api/migrations"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("migrate failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if cfg.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required (see .env.example; the default local value targets port 5433)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.Pool().Ping(ctx); err != nil {
		// pgx connection errors include host/user/database but never the
		// password; keep the message short and actionable.
		return errors.New("database is not reachable; start it with `make db-up` (`docker compose up -d db`)")
	}

	applied, err := postgres.Migrate(ctx, store.Pool())
	if err != nil {
		return err
	}

	required, err := migrations.Latest()
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		slog.Info("database schema already up to date", "schema_version", required)
	} else {
		slog.Info("migrations applied", "versions", applied, "schema_version", required)
	}
	return nil
}
