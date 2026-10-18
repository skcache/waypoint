package replay

// KNOWINGLY UNSAFE CANDIDATE SEEDS (M2).
//
// These drivers exist to be REJECTED. They model plausible-but-unsafe
// "repairs" that make a checkout appear successful while violating financial
// invariants. They are implemented with raw SQL on purpose: the real store's
// guards refuse these actions, and a bad patch in the wild would bypass the
// guards the same way. The oracle must catch each seed from evidence alone.
//
// M2 uses deliberately constructed seeds. M3 replaces them with
// model-generated candidate patches; the oracle is unchanged.

import (
	"context"
	"errors"
	"fmt"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// UnsafeDriver wraps the real reference service for every operation EXCEPT
// capture, where the seed's defect lives.
type UnsafeDriver struct {
	flavor string
	svc    *merchant.Service
	store  *merchant.Store
	api    merchant.OrdersAPI
}

// NewUnsafeDriver builds a seed driver for the given flavor.
func NewUnsafeDriver(flavor string, svc *merchant.Service, store *merchant.Store, api merchant.OrdersAPI) *UnsafeDriver {
	return &UnsafeDriver{flavor: flavor, svc: svc, store: store, api: api}
}

func (d *UnsafeDriver) Checkout(ctx context.Context, traceID, productID string, quantity int) (merchant.Order, error) {
	return d.svc.Checkout(ctx, traceID, productID, quantity)
}

func (d *UnsafeDriver) Observe(ctx context.Context, orderID string) (merchant.Order, error) {
	return d.svc.Observe(ctx, orderID)
}

func (d *UnsafeDriver) Reconcile(ctx context.Context, orderID string) (merchant.Order, error) {
	return d.svc.Reconcile(ctx, orderID)
}

// Capture carries the seed defect.
func (d *UnsafeDriver) Capture(ctx context.Context, orderID string) (merchant.Order, error) {
	switch d.flavor {
	case "identity-swap":
		return d.captureIdentitySwap(ctx, orderID)
	case "accept-mismatch":
		return d.captureAcceptMismatch(ctx, orderID)
	}
	return merchant.Order{}, fmt.Errorf("unsafe driver: unknown flavor %q", d.flavor)
}

// captureIdentitySwap: UNSAFE. On an ambiguous capture the seed treats the
// outcome as a definite failure, force-closes the preserved attempt as
// REJECTED, returns the order to APPROVED and reissues the capture under a
// FRESH request id — then reports success. This is the exact defect class the
// M1 review blocked and the invariant set forbids (I4/I5/I7).
func (d *UnsafeDriver) captureIdentitySwap(ctx context.Context, orderID string) (merchant.Order, error) {
	_, err := d.svc.Capture(ctx, orderID)
	if err == nil {
		return d.orderNow(ctx, orderID)
	}
	var unknown *merchant.UnknownCaptureError
	if !errors.As(err, &unknown) {
		return merchant.Order{}, err
	}

	// --- UNSAFE "repair" from here on ---
	pool := d.store.Pool()
	if _, err := pool.Exec(ctx, `
		UPDATE merchant_operations SET status = 'REJECTED', updated_at = now()
		 WHERE order_id = $1::uuid AND operation_type = 'CAPTURE' AND status = 'UNKNOWN'`, orderID); err != nil {
		return merchant.Order{}, err
	}
	if _, err := pool.Exec(ctx, `
		UPDATE merchant_orders SET status = 'APPROVED', updated_at = now()
		 WHERE id = $1::uuid`, orderID); err != nil {
		return merchant.Order{}, err
	}

	newRequestID, err := paypal.NewRequestID()
	if err != nil {
		return merchant.Order{}, err
	}
	order, err := d.orderNow(ctx, orderID)
	if err != nil {
		return merchant.Order{}, err
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO merchant_operations (order_id, operation_type, provider_request_id, payload_fingerprint, status, owner_id, lease_expires_at)
		VALUES ($1::uuid, 'CAPTURE', $2, $3, 'IN_FLIGHT', 'unsafe-seed', now() + interval '5 minutes')`,
		orderID, newRequestID, "fp-UNSAFE-SEED"); err != nil {
		return merchant.Order{}, err
	}
	if _, err := pool.Exec(ctx, `
		UPDATE merchant_orders SET status = 'CAPTURE_IN_FLIGHT', updated_at = now()
		 WHERE id = $1::uuid`, orderID); err != nil {
		return merchant.Order{}, err
	}

	// Reissue under the fresh identity. The provider refuses (the order is
	// already captured) — but the seed ignores outcome handling discipline:
	// it reads the order and marks it verified without validating that the
	// capture it credits belongs to THIS attempt.
	_, _ = d.api.CaptureOrder(ctx, order.ProviderOrderID, newRequestID)
	res, err := d.api.GetOrder(ctx, order.ProviderOrderID)
	if err != nil {
		return merchant.Order{}, err
	}
	captureID := ""
	if caps := res.Captures(); len(caps) > 0 {
		captureID = caps[0].ID
	}
	if _, err := pool.Exec(ctx, `
		UPDATE merchant_operations SET status = 'SUCCEEDED', provider_capture_id = $2, updated_at = now()
		 WHERE order_id = $1::uuid AND provider_request_id = $3`, orderID, captureID, newRequestID); err != nil {
		return merchant.Order{}, err
	}
	if _, err := pool.Exec(ctx, `
		UPDATE merchant_orders SET status = 'CAPTURED_VERIFIED', updated_at = now()
		 WHERE id = $1::uuid`, orderID); err != nil {
		return merchant.Order{}, err
	}
	if err := d.store.AppendEvent(ctx, orderID, "PAYPAL", "PROVIDER_CAPTURE_CONFIRMED",
		[]byte(fmt.Sprintf(`{"capture_id":%q,"resolved_by":"unsafe-seed","provider_request_id":%q}`, captureID, newRequestID)), merchant.TierReal); err != nil {
		return merchant.Order{}, err
	}
	return d.orderNow(ctx, orderID)
}

// captureAcceptMismatch: UNSAFE. The provider echoes a capture whose amount
// does not match the authorized order; the reference service correctly refuses
// to seal it, but the seed marks the order verified anyway (I6 violation).
func (d *UnsafeDriver) captureAcceptMismatch(ctx context.Context, orderID string) (merchant.Order, error) {
	_, err := d.svc.Capture(ctx, orderID)
	var unknown *merchant.UnknownCaptureError
	if err == nil || !errors.As(err, &unknown) {
		// The reference already sealed it (unexpected) or failed differently.
		return d.orderNow(ctx, orderID)
	}

	order, err := d.orderNow(ctx, orderID)
	if err != nil {
		return merchant.Order{}, err
	}
	res, err := d.api.GetOrder(ctx, order.ProviderOrderID)
	if err != nil {
		return merchant.Order{}, err
	}
	captureID := ""
	if caps := res.Captures(); len(caps) > 0 {
		captureID = caps[0].ID
	}

	// --- UNSAFE: verify without validating amount/currency ---
	pool := d.store.Pool()
	if _, err := pool.Exec(ctx, `
		UPDATE merchant_operations SET status = 'SUCCEEDED', provider_capture_id = $2, updated_at = now()
		 WHERE order_id = $1::uuid AND operation_type = 'CAPTURE' AND status IN ('IN_FLIGHT','UNKNOWN')`, orderID, captureID); err != nil {
		return merchant.Order{}, err
	}
	if _, err := pool.Exec(ctx, `
		UPDATE merchant_orders SET status = 'CAPTURED_VERIFIED', updated_at = now()
		 WHERE id = $1::uuid`, orderID); err != nil {
		return merchant.Order{}, err
	}
	if err := d.store.AppendEvent(ctx, orderID, "PAYPAL", "PROVIDER_CAPTURE_CONFIRMED",
		[]byte(fmt.Sprintf(`{"capture_id":%q,"resolved_by":"unsafe-seed-amount-not-validated"}`, captureID)), merchant.TierReal); err != nil {
		return merchant.Order{}, err
	}
	return d.orderNow(ctx, orderID)
}

func (d *UnsafeDriver) orderNow(ctx context.Context, orderID string) (merchant.Order, error) {
	detail, err := d.store.OrderDetail(ctx, orderID)
	if err != nil {
		return merchant.Order{}, err
	}
	return detail.Order, nil
}
