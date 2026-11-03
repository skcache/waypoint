package merchant_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/skcache/waypoint/apps/api/internal/merchant"
	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// A refusal of a later send says nothing about the first send's financial
// effect. Keep the original unresolved operation through refusal and reload.
func TestRetryRefusalCannotCloseEarlierAmbiguousCapture(t *testing.T) {
	for _, httpStatus := range []int{401, 422} {
		t.Run(fmt.Sprintf("HTTP_%d", httpStatus), func(t *testing.T) {
			store := newFixtureStore(t)
			calls := 0
			fake := &fakePayPal{
				getFn: func(id string) (*paypal.Order, error) { return approvedOrder(id, "USD", "12.34"), nil },
				captureFn: func(id, request string) (*paypal.Order, error) {
					calls++
					if calls == 1 {
						return nil, errors.New("response lost after provider applied capture")
					}
					return nil, &paypal.APIError{StatusCode: httpStatus, Name: "RETRY_REFUSED"}
				},
			}
			svc := merchant.NewService(store, fake, merchant.FaultNone, "")
			order := newOrder(t, store, svc, 1)
			approve(t, svc, order.ID)
			_, err := svc.Capture(context.Background(), order.ID)
			var unknown *merchant.UnknownCaptureError
			if !errors.As(err, &unknown) {
				t.Fatalf("first capture = %v, want unresolved outcome", err)
			}
			original := fake.captureCalls[0]
			for i := 0; i < 2; i++ {
				// Recreate the service to prove the retry decision survives restart.
				svc = merchant.NewService(merchant.NewStore(store.Pool()), fake, merchant.FaultNone, "")
				_, err := svc.Capture(context.Background(), order.ID)
				if !errors.As(err, &unknown) {
					t.Fatalf("retry refusal = %v, want unresolved outcome", err)
				}
				detail := detailFor(t, store, order.ID)
				if detail.Order.Status != merchant.StatusCaptureOutcomeUnknown {
					t.Fatalf("retry refusal changed order status to %s", detail.Order.Status)
				}
				count := 0
				for _, op := range detail.Operations {
					if op.Type != merchant.OpCapture {
						continue
					}
					count++
					if op.Status != merchant.OpStatusUnknown || op.ProviderRequestID != original || op.AttemptCount != i+2 {
						t.Fatalf("retry refusal erased unresolved operation: %+v", op)
					}
				}
				if count != 1 {
					t.Fatalf("capture operation count = %d, want 1", count)
				}
			}
			if _, err := svc.Capture(context.Background(), order.ID); !errors.Is(err, merchant.ErrConflict) {
				t.Fatalf("retry limit = %v, want conflict", err)
			}
			if len(fake.captureCalls) != 3 || detailFor(t, store, order.ID).Order.Status != merchant.StatusReconciliationRequired {
				t.Fatal("retry limit must stop sends and require reconciliation")
			}
			for _, request := range fake.captureCalls {
				if request != original {
					t.Fatalf("fresh identity after ambiguity: %q vs %q", request, original)
				}
			}
		})
	}
}

// Direct retry after a crashed owner must claim the expired lease without
// requiring a separate reconcile call, while retaining the original identity.
func TestCaptureDirectlyTakesOverExpiredLease(t *testing.T) {
	store := newFixtureStore(t)
	ctx := context.Background()
	fake := &fakePayPal{
		getFn: func(id string) (*paypal.Order, error) { return approvedOrder(id, "USD", "12.34"), nil },
		captureFn: func(id, request string) (*paypal.Order, error) {
			return nil, &paypal.APIError{StatusCode: 422, Name: "RETRY_REFUSED"}
		},
	}
	svc := merchant.NewService(store, fake, merchant.FaultNone, "")
	order := newOrder(t, store, svc, 1)
	approve(t, svc, order.ID)
	op, err := store.InsertCaptureOp(ctx, order.ID, "req-crashed-owner", "fp-crashed-owner", "crashed-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool().Exec(ctx, `UPDATE merchant_operations SET lease_expires_at = now() - interval '1 second' WHERE id = $1::uuid`, op.ID); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Capture(ctx, order.ID)
	var unknown *merchant.UnknownCaptureError
	if !errors.As(err, &unknown) {
		t.Fatalf("expired-lease retry = %v, want unresolved outcome", err)
	}
	active, err := store.ActiveCapture(ctx, order.ID)
	if err != nil || active == nil {
		t.Fatalf("active operation = %v, %v", active, err)
	}
	if active.ID != op.ID || active.ProviderRequestID != op.ProviderRequestID || active.Fence != op.Fence+1 || active.AttemptCount != 2 || active.OwnerID != svc.InstanceID() || active.Status != merchant.OpStatusUnknown {
		t.Fatalf("takeover did not preserve and fence the unresolved operation: %+v", active)
	}
	if len(fake.captureCalls) != 1 || fake.captureCalls[0] != op.ProviderRequestID {
		t.Fatalf("takeover sends = %v, want original request only", fake.captureCalls)
	}
}
