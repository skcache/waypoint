// Command paypal-spike is a developer-only feasibility CLI for Phase 04 of
// Waypoint. It is not a product surface:
//
//   - no HTTP endpoints; nothing is exposed to browsers or the web app
//   - sandbox-only (the client refuses any environment other than sandbox)
//   - a fixed, synthetic $1.00 purchase; no amounts from arguments or users
//   - operator-driven steps; buyer approval happens manually in a browser
//     and capture is refused until the provider confirms approval
//   - ambiguous capture outcomes are reported as PAYMENT_OUTCOME_UNKNOWN,
//     never blindly retried
//
// Usage (from apps/api/, with credentials in the environment — for example
// `set -a && source ../../.env && set +a`):
//
//	go run ./cmd/paypal-spike auth
//	go run ./cmd/paypal-spike create
//	go run ./cmd/paypal-spike status  [--order-id ID]
//	go run ./cmd/paypal-spike capture [--order-id ID]
//
// Environment: PAYPAL_ENV=sandbox, PAYPAL_CLIENT_ID, PAYPAL_CLIENT_SECRET.
// Records are written under --dir (default docs/spike/records, git-ignored).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

const (
	defaultRecordDir = "docs/spike/records"
	operationTimeout = 45 * time.Second

	exitOK           = 0
	exitFailed       = 1
	exitNotReady     = 2 // usage or configuration problem
	exitUnknownState = 3 // PAYMENT_OUTCOME_UNKNOWN
)

const usageText = `paypal-spike — PayPal sandbox feasibility CLI (developer-only).

Usage:
  paypal-spike auth    [--dir DIR]                  verify sandbox credentials
  paypal-spike create  [--dir DIR]                  create the fixed $1.00 sandbox order
  paypal-spike status  [--dir DIR] [--order-id ID]  refresh provider state
  paypal-spike capture [--dir DIR] [--order-id ID]  capture after buyer approval

Environment: PAYPAL_ENV=sandbox  PAYPAL_CLIENT_ID  PAYPAL_CLIENT_SECRET
Records: --dir (default docs/spike/records, git-ignored). The purchase is a
fixed $1.00 test order; this tool never accepts arbitrary amounts and never
touches the Waypoint transaction tables.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return exitNotReady
	}
	command := args[0]
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", defaultRecordDir, "record directory (git-ignored)")
	orderID := fs.String("order-id", "", "provider order id (default: most recent record)")
	if err := fs.Parse(args[1:]); err != nil {
		return exitNotReady
	}
	switch command {
	case "auth", "create", "status", "capture":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", command)
		fmt.Fprint(stderr, usageText)
		return exitNotReady
	}

	cfg, err := paypal.ConfigFromEnv(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitNotReady
	}
	client := paypal.New(cfg)
	store := paypal.FileRecordStore{Dir: *dir}

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()

	switch command {
	case "auth":
		info, err := client.Authenticate(ctx)
		if err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stdout, "authentication OK — app_id=%s expires_in=%ds reused=%v (access token withheld)\n",
			info.AppID, info.ExpiresIn, info.Reused)
		return exitOK

	case "create":
		requestID, err := paypal.NewRequestID()
		if err != nil {
			return fail(stderr, err)
		}
		rec, err := paypal.CreateSpikeOrder(ctx, client, store, requestID)
		if err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stdout, "sandbox order created: id=%s status=%s amount=%s %s\n",
			rec.OrderID, rec.ProviderStatus, rec.Currency, rec.AmountValue)
		fmt.Fprintf(stdout, "create request id: %s\n", rec.CreateRequestID)
		if rec.ApprovalURL != "" {
			fmt.Fprintf(stdout, "buyer approval URL: %s\n", rec.ApprovalURL)
			fmt.Fprintf(stdout, "next: complete approval with a sandbox buyer account in a browser, then run:\n  paypal-spike capture --order-id %s\n", rec.OrderID)
		} else {
			fmt.Fprintln(stdout, "WARNING: the provider response carried no approval URL")
		}
		return exitOK

	case "status":
		target, err := resolveOrder(store, *orderID)
		if err != nil {
			return fail(stderr, err)
		}
		rec, err := paypal.RetrieveSpikeOrder(ctx, client, store, target)
		if err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stdout, "order=%s provider_status=%s local_state=%s capture_id=%s\n",
			rec.OrderID, rec.ProviderStatus, rec.State, dash(rec.CaptureID))
		return exitOK

	case "capture":
		target, err := resolveOrder(store, *orderID)
		if err != nil {
			return fail(stderr, err)
		}
		requestID, err := paypal.NewRequestID()
		if err != nil {
			return fail(stderr, err)
		}
		ev, err := paypal.CaptureSpikeOrder(ctx, client, store, target, requestID)
		if err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stdout, "CAPTURE VERIFIED — order=%s capture=%s status=%s amount=%s %s payer=%s\n",
			ev.OrderID, ev.CaptureID, ev.Status, ev.Currency, ev.AmountValue, dash(ev.PayerID))
		return exitOK
	}
	return exitNotReady
}

func resolveOrder(store paypal.FileRecordStore, orderID string) (string, error) {
	if orderID != "" {
		return orderID, nil
	}
	rec, err := store.Latest()
	if err != nil {
		return "", fmt.Errorf("no --order-id given and no local records found: %w", err)
	}
	return rec.OrderID, nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func fail(stderr io.Writer, err error) int {
	var unknown *paypal.UnknownOutcomeError
	if errors.As(err, &unknown) {
		fmt.Fprintf(stderr, "%v\n", err)
		fmt.Fprintln(stderr, "operator guidance: do not retry blindly. Check provider state (paypal-spike status), then re-run capture — the same capture request id is reused.")
		return exitUnknownState
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	return exitFailed
}
