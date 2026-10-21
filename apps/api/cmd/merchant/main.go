// Command merchant runs the Waypoint V2 M1 merchant checkout fixture: a small
// storefront that sells from a server-owned catalog through the real PayPal
// Sandbox, with a durable PostgreSQL operation journal and one deliberately
// injectable provider-request defect (MERCHANT_FAULT=F1).
//
// This is the *merchant application* the Waypoint control plane will later
// investigate and repair. It is not the Waypoint API itself. Run it with:
//
//	make dev-merchant            # serves on :8090 (see .env for credentials)
//	cd apps/api && go run ./cmd/merchant --reset   # clear fixture state
//
// The fixture binds to 127.0.0.1 only: it serves unauthenticated mutation
// endpoints (checkout, capture) and must never be reachable from the network.
//
// The fixture never simulates payments: without PayPal credentials every
// checkout answers NOT_CONFIGURED, and no code path fabricates an approval or
// a capture.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
	"github.com/skcache/waypoint/apps/api/internal/store/postgres"
)

const defaultPort = 8090

// loopbackHost is the only interface the fixture ever binds. The mutation
// endpoints are unauthenticated; a network-reachable fixture would be a
// public exposure path. Do not make this configurable without adding
// authentication and request authorization first.
const loopbackHost = "127.0.0.1"

// listenAddr returns the fixture bind address (loopback only — see above).
func listenAddr(port int) string {
	return net.JoinHostPort(loopbackHost, strconv.Itoa(port))
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("merchant fixture exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	reset := flag.Bool("reset", false, "truncate fixture transactional tables (orders, operations, events) and exit")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required (the merchant fixture stores orders in PostgreSQL)")
	}

	ctx := context.Background()
	store, err := merchant.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer store.Close()

	if _, err := postgres.Migrate(ctx, store.Pool()); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	if err := store.SeedProducts(ctx); err != nil {
		return err
	}

	if *reset {
		if err := store.Reset(ctx); err != nil {
			return err
		}
		fmt.Println("merchant fixture reset: orders, operations and events cleared (catalog kept)")
		return nil
	}

	port := defaultPort
	if raw := os.Getenv("MERCHANT_PORT"); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("invalid MERCHANT_PORT %q", raw)
		}
		port = p
	}

	fault := os.Getenv("MERCHANT_FAULT")
	if fault == "" {
		fault = merchant.FaultNone
	}
	if fault != merchant.FaultNone && fault != merchant.FaultF1 && fault != merchant.FaultF4 {
		return fmt.Errorf("invalid MERCHANT_FAULT %q (want none, F1 or F4)", fault)
	}

	app := &server{
		store:       store,
		incidentDir: os.Getenv("MERCHANT_INCIDENT_DIR"),
	}

	if _, err := paypal.ConfigFromEnv(os.Getenv); err != nil {
		slog.Warn("PayPal sandbox credentials are not configured; checkout will honestly report NOT_CONFIGURED")
	} else {
		app.paypal = paypal.New(mustPaypalConfig())
	}

	app.svc = merchant.NewService(store, app.paypal, fault, app.incidentDir)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", app.handleCatalog)
	mux.HandleFunc("POST /checkout", app.handleCheckout)
	mux.HandleFunc("GET /orders/{$}", app.handleOrderList)
	mux.HandleFunc("GET /orders/{id}", app.handleOrderPage)
	mux.HandleFunc("POST /orders/{id}/capture", app.handleCapture)
	mux.HandleFunc("POST /orders/{id}/refresh", app.handleRefresh)
	mux.HandleFunc("POST /orders/{id}/resolve-create", app.handleResolveCreate)
	mux.HandleFunc("GET /api/orders/{id}", app.handleOrderJSON)
	mux.HandleFunc("GET /api/orders/{id}/incident", app.handleIncidentJSON)
	mux.HandleFunc("GET /healthz", app.handleHealthz)

	srv := &http.Server{Addr: listenAddr(port), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	slog.Info("merchant checkout fixture listening",
		"addr", srv.Addr, "fault", fault,
		"paypal_configured", app.paypal != nil,
		"incident_dir_set", app.incidentDir != "")

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

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

func mustPaypalConfig() paypal.Config {
	cfg, err := paypal.ConfigFromEnv(os.Getenv)
	if err != nil {
		// Unreachable: the caller checked first; fail loudly if it changes.
		panic(err)
	}
	return cfg
}

type server struct {
	store       *merchant.Store
	svc         *merchant.Service
	paypal      merchant.OrdersAPI
	incidentDir string
}

func (s *server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Pool().Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "database": "unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	products, err := s.store.Products(r.Context())
	if err != nil {
		s.renderError(w, http.StatusInternalServerError, "The catalog could not be loaded.", err)
		return
	}
	s.render(w, http.StatusOK, catalogTmpl, map[string]any{
		"Products":   products,
		"Fault":      s.svc.Fault(),
		"Configured": s.paypal != nil,
	})
}

func (s *server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	if s.paypal == nil {
		s.renderError(w, http.StatusServiceUnavailable, "NOT_CONFIGURED: PayPal sandbox credentials are not set, so nothing was attempted.", errors.New("paypal credentials missing"))
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, http.StatusBadRequest, "Invalid form submission.", err)
		return
	}
	productID := r.PostFormValue("product_id")
	quantity, err := strconv.Atoi(r.PostFormValue("quantity"))
	if err != nil {
		s.renderError(w, http.StatusBadRequest, "Quantity must be a whole number.", err)
		return
	}
	traceID, err := paypal.NewRequestID() // UUID; used as the correlation id
	if err != nil {
		s.renderError(w, http.StatusInternalServerError, "Could not start the checkout.", err)
		return
	}

	order, err := s.svc.Checkout(r.Context(), traceID, productID, quantity)
	if err != nil {
		var rejected *merchant.CreateRejectedError
		var notFound error = merchant.ErrNotFound
		switch {
		case errors.As(err, &rejected):
			// Real provider rejection: render the failure with correlation.
			s.renderOrderWithNotice(w, rejected.Order.ID, "The provider rejected this checkout (FAILED_DEFINITE).", err)
			return
		case errors.Is(err, notFound):
			s.renderError(w, http.StatusNotFound, "Unknown product.", err)
			return
		case errors.Is(err, merchant.ErrConflict):
			s.renderError(w, http.StatusBadRequest, err.Error(), err)
			return
		default:
			// Ambiguous create or infrastructure failure: find the latest
			// order to show its preserved state when possible.
			s.renderError(w, http.StatusBadGateway, "Checkout did not complete cleanly; the local operation state is preserved and inspectable (see the order list).", err)
			return
		}
	}
	http.Redirect(w, r, "/orders/"+order.ID, http.StatusSeeOther)
}

func (s *server) handleOrderPage(w http.ResponseWriter, r *http.Request) {
	s.renderOrderWithNotice(w, r.PathValue("id"), "", nil)
}

func (s *server) handleOrderList(w http.ResponseWriter, r *http.Request) {
	orders, err := s.store.ListOrders(r.Context(), 50)
	if err != nil {
		s.renderError(w, http.StatusInternalServerError, "The order list could not be loaded.", err)
		return
	}
	s.render(w, http.StatusOK, ordersTmpl, map[string]any{
		"Orders":     orders,
		"Fault":      s.svc.Fault(),
		"Configured": s.paypal != nil,
	})
}

func (s *server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.svc.Observe(r.Context(), id); err != nil {
		s.renderOrderWithNotice(w, id, "Provider status refresh failed (local state unchanged): "+err.Error(), err)
		return
	}
	http.Redirect(w, r, "/orders/"+id, http.StatusSeeOther)
}

func (s *server) handleCapture(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := s.svc.Capture(r.Context(), id)
	if err != nil {
		var unknown *merchant.UnknownCaptureError
		switch {
		case errors.As(err, &unknown):
			s.renderOrderWithNotice(w, id, "PAYMENT_OUTCOME_UNKNOWN: the capture outcome is unresolved. The operation and its request id are preserved; reconcile before retrying.", err)
		case errors.Is(err, merchant.ErrConflict):
			s.renderOrderWithNotice(w, id, err.Error(), err)
		default:
			s.renderOrderWithNotice(w, id, "Capture failed: "+err.Error(), err)
		}
		return
	}
	http.Redirect(w, r, "/orders/"+id, http.StatusSeeOther)
}

func (s *server) handleResolveCreate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.svc.ResolveCreate(r.Context(), id); err != nil {
		s.renderOrderWithNotice(w, id, "Create resolution reported: "+err.Error(), err)
		return
	}
	http.Redirect(w, r, "/orders/"+id, http.StatusSeeOther)
}

func (s *server) renderOrderWithNotice(w http.ResponseWriter, id, notice string, cause error) {
	detail, err := s.store.OrderDetail(context.Background(), id)
	if err != nil {
		s.renderError(w, http.StatusNotFound, "Order not found.", err)
		return
	}
	hasIncident := false
	canResolveCreate := false
	for _, op := range detail.Operations {
		if op.Status == merchant.OpStatusRejected || op.Status == merchant.OpStatusUnknown {
			hasIncident = true
		}
		if op.Type == merchant.OpCreateOrder && op.Status == merchant.OpStatusUnknown &&
			detail.Order.Status == merchant.StatusOrderCreated {
			canResolveCreate = true
		}
	}
	s.render(w, http.StatusOK, orderTmpl, map[string]any{
		"Detail":     detail,
		"Order":      detail.Order,
		"Operations": detail.Operations,
		"Events":     formatEvents(detail.Events),
		"Notice":     notice,
		"Incident":   hasIncident,
		"CanResolve": canResolveCreate,
		"Fault":      s.svc.Fault(),
		"Configured": s.paypal != nil,
	})
}

func (s *server) handleOrderJSON(w http.ResponseWriter, r *http.Request) {
	detail, err := s.store.OrderDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, merchant.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": "order not found"})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *server) handleIncidentJSON(w http.ResponseWriter, r *http.Request) {
	inc, err := s.svc.IncidentFor(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, inc)
}

func formatEvents(events []merchant.Event) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		var pretty string
		var d any
		if err := json.Unmarshal(e.Detail, &d); err == nil {
			b, _ := json.MarshalIndent(d, "", "  ")
			pretty = string(b)
		} else {
			pretty = string(e.Detail)
		}
		out = append(out, map[string]any{
			"ID": e.ID, "Source": e.Source, "Type": e.EventType,
			"Tier": e.Tier, "Time": e.CreatedAt.Format("15:04:05.000"), "Detail": pretty,
		})
	}
	return out
}

func (s *server) renderError(w http.ResponseWriter, status int, message string, cause error) {
	if cause != nil {
		slog.Warn("merchant fixture request failed", "status", status, "error", cause)
	}
	s.render(w, status, errorTmpl, map[string]any{"Message": message})
}

func (s *server) render(w http.ResponseWriter, status int, tmpl *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := tmpl.Execute(w, data); err != nil {
		slog.Error("merchant template render failed", "error", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func statusTone(status string) string {
	switch status {
	case merchant.StatusCapturedVerified:
		return "ok"
	case merchant.StatusFailedDefinite:
		return "bad"
	case merchant.StatusCaptureOutcomeUnknown, merchant.StatusReconciliationRequired, merchant.StatusCaptureInFlight:
		return "warn"
	default:
		return "muted"
	}
}

var templateFuncs = template.FuncMap{
	"tone":  statusTone,
	"usd":   func(minor int64) string { return paypal.FormatMinorUSD(minor) },
	"lower": strings.ToLower,
}
