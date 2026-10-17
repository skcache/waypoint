// Command server runs the Waypoint API.
//
// Phase 02 scope: process bootstrap, environment-based configuration, the
// liveness endpoint, and database readiness reporting. Payment execution,
// semantic verification and the transaction state machine are introduced in
// their assigned phases (see the local product specification at docs/PRD.md
// — kept off version control on purpose).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/config"
	"github.com/skcache/waypoint/apps/api/internal/httpapi"
	"github.com/skcache/waypoint/apps/api/internal/intent"
	"github.com/skcache/waypoint/apps/api/internal/llm"
	"github.com/skcache/waypoint/apps/api/internal/store/postgres"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("waypoint api exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	ready, closeDatabase := prepareReadiness(cfg)
	defer closeDatabase()

	deps := prepareDeps(cfg)

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           httpapi.NewHandler(ready, deps),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("waypoint api listening", "addr", cfg.Addr(), "env", cfg.AppEnv)
		serveErr <- srv.ListenAndServe()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case sig := <-shutdown:
		slog.Info("shutdown signal received", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// prepareReadiness wires the /readyz checker from configuration. A missing or
// unusable DATABASE_URL is not fatal: the process starts and readiness
// fail-closes with a non-secret reason code. The pool connects lazily, so no
// database I/O happens here. The returned cleanup func closes the pool and
// runs on every shutdown path (F09).
func prepareReadiness(cfg config.Config) (httpapi.ReadinessChecker, func()) {
	noop := func() {}

	if cfg.DatabaseURL == "" {
		slog.Info("no database configured; /readyz will report not_configured")
		return nil, noop
	}

	store, err := postgres.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		// Deliberately no error detail: it may echo parts of the DSN.
		slog.Warn("DATABASE_URL rejected; /readyz will report not_configured")
		return nil, noop
	}
	slog.Info("database configured; /readyz probes it on request")
	return store, func() {
		slog.Info("closing database connection pool")
		store.Close()
	}
}

// prepareDeps wires the HTTP feature dependencies from configuration. A
// missing or unusable LLM configuration is not fatal: the process starts and
// the draft endpoint fail-closes with NOT_CONFIGURED — never a fabricated
// model result (PRD §5.4).
func prepareDeps(cfg config.Config) httpapi.Deps {
	deps := httpapi.Deps{AllowedOrigin: cfg.WebOrigin}

	if !cfg.LLMConfigured() {
		slog.Info("no LLM configuration; /api/v1/intents/draft will report NOT_CONFIGURED")
		return deps
	}

	client, err := llm.New(llm.Config{
		BaseURL: cfg.LLMBaseURL,
		APIKey:  cfg.LLMAPIKey,
		Model:   cfg.LLMModel,
	})
	if err != nil {
		// Deliberately no error detail: it could echo configuration values.
		slog.Warn("LLM configuration rejected; /api/v1/intents/draft will report NOT_CONFIGURED")
		return deps
	}

	provider := ""
	if u, err := url.Parse(cfg.LLMBaseURL); err == nil {
		provider = u.Host
	}
	deps.IntentCompiler = intent.NewHostedCompiler(client, provider, cfg.LLMModel)
	slog.Info("intent draft compiler configured", "provider", provider, "model", cfg.LLMModel)
	return deps
}
