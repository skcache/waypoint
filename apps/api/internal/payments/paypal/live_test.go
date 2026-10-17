package paypal

// The only live test in this package. It talks to the real PayPal sandbox and
// runs solely when explicitly requested with PAYPAL_SPIKE_LIVE=1 plus valid
// sandbox credentials. Mock-server tests never claim live evidence; this file
// is where live evidence would come from.

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveSandboxFeasibility proves sandbox OAuth authentication and creation
// of the fixed $1.00 order. Buyer approval and capture are operator steps
// performed through the CLI and deliberately NOT automated here.
func TestLiveSandboxFeasibility(t *testing.T) {
	if os.Getenv("PAYPAL_SPIKE_LIVE") != "1" {
		t.Skip("live sandbox test: set PAYPAL_SPIKE_LIVE=1 (with PAYPAL_ENV=sandbox and valid sandbox credentials) to run")
	}
	cfg, err := ConfigFromEnv(os.Getenv)
	if err != nil {
		t.Fatalf("live run requested but configuration is invalid: %v", err)
	}
	client := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	info, err := client.Authenticate(ctx)
	if err != nil {
		t.Fatalf("sandbox authentication failed: %v", err)
	}
	t.Logf("sandbox authentication OK (app_id=%s expires_in=%ds)", info.AppID, info.ExpiresIn)

	requestID, err := NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	order, err := client.CreateOrder(ctx, CreateOrderInput{
		Intent:       "CAPTURE",
		CurrencyCode: SpikeCurrency,
		AmountValue:  FormatMinorUSD(SpikeAmountMinor),
		Description:  SpikeDescription,
	}, requestID)
	if err != nil {
		t.Fatalf("sandbox order creation failed: %v", err)
	}
	t.Logf("sandbox order created: id=%s status=%s approval_url=%s", order.ID, order.Status, order.ApprovalURL())
}
