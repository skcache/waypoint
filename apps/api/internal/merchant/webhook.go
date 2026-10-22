package merchant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// HandleWebhook is the ingress for provider event deliveries. Authenticity is
// checked before any durable write; the selected apply implementation decides
// how the event affects merchant state.
func (s *Service) HandleWebhook(ctx context.Context, payload []byte, headers map[string]string) error {
	if s.webhooks == nil {
		return errors.New("merchant: no webhook verifier configured")
	}
	ev, err := s.webhooks.Verify(payload, headers)
	if err != nil {
		return err
	}
	return s.applyWebhook(ctx, ev)
}

// applyWebhookEvent is the correct event handler.
//
// Webhooks are at-least-once and unordered. The handler therefore:
//   - records a durable receipt keyed by the provider event id (redelivery
//     bumps a counter, it never creates a second receipt);
//   - fulfills only when the local order is already CAPTURED_VERIFIED and the
//     event names the very capture operation the merchant sealed;
//   - uses a stable per-order effect key, so several events describing the
//     same capture still produce exactly one fulfillment effect;
//   - never advances or rewinds payment state from an event. Provider reads
//     (Observe/Capture/reconcile) are the only paths allowed to do that, which
//     is what makes an older event harmless.
func (s *Service) applyWebhookEvent(ctx context.Context, ev WebhookEvent) error {
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	orderID, status, err := s.webhookOrder(ctx, tx, ev.ProviderOrderID)
	if err != nil {
		return err
	}
	if err := s.recordWebhookReceipt(ctx, tx, ev, orderID, true); err != nil {
		return err
	}

	switch ev.Type {
	case "PAYMENT.CAPTURE.COMPLETED":
		if status != StatusCapturedVerified {
			// Early event: durable receipt only. A later redelivery, after the
			// merchant has verified the capture, can still fulfill.
			break
		}
		verifiedCapture, err := s.verifiedCaptureID(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if ev.CaptureID == "" || ev.CaptureID != verifiedCapture {
			break
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO merchant_fulfillment_effects (order_id, effect_key, event_id, tier)
			VALUES ($1::uuid, 'FULFILL', $2, $3)
			ON CONFLICT (order_id, effect_key) DO NOTHING`,
			orderID, ev.ID, s.evidenceTier()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE merchant_webhook_events SET processed = true, last_received_at = now() WHERE event_id = $1`, ev.ID); err != nil {
			return err
		}
	default:
		// Non-capture events are receipts, never state transitions.
		if _, err := tx.Exec(ctx, `UPDATE merchant_webhook_events SET processed = true WHERE event_id = $1`, ev.ID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// legacyApplyWebhookEvent is the historical handler retained by the fixture's
// F3 wiring. It trusts the event payload as the source of truth: it keys the
// fulfillment by event id (so two distinct events for one capture produce two
// effects) and writes the event's status straight onto the order (so a late
// approval event can rewind a verified capture).
func (s *Service) legacyApplyWebhookEvent(ctx context.Context, ev WebhookEvent) error {
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	orderID, _, err := s.webhookOrder(ctx, tx, ev.ProviderOrderID)
	if err != nil {
		return err
	}
	if err := s.recordWebhookReceipt(ctx, tx, ev, orderID, false); err != nil {
		return err
	}
	if orderID == "" {
		return tx.Commit(ctx)
	}

	switch ev.Type {
	case "ORDER.APPROVED":
		if _, err := tx.Exec(ctx, `UPDATE merchant_orders SET status = $2, updated_at = now() WHERE id = $1::uuid`, orderID, StatusApproved); err != nil {
			return err
		}
	case "PAYMENT.CAPTURE.COMPLETED":
		if _, err := tx.Exec(ctx, `
			INSERT INTO merchant_fulfillment_effects (order_id, effect_key, event_id, tier)
			VALUES ($1::uuid, $2, $2, $3)
			ON CONFLICT (order_id, effect_key) DO NOTHING`,
			orderID, ev.ID, s.evidenceTier()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE merchant_webhook_events SET processed = true, last_received_at = now() WHERE event_id = $1`, ev.ID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) webhookOrder(ctx context.Context, tx pgx.Tx, providerOrderID string) (string, string, error) {
	if providerOrderID == "" {
		return "", "", nil
	}
	var id, status string
	err := tx.QueryRow(ctx, `SELECT id::text, status FROM merchant_orders WHERE provider_order_id = $1`, providerOrderID).Scan(&id, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return id, status, nil
}

func (s *Service) recordWebhookReceipt(ctx context.Context, tx pgx.Tx, ev WebhookEvent, orderID string, strict bool) error {
	digest := sha256.Sum256(ev.Raw)
	var id string
	query := `
		INSERT INTO merchant_webhook_events
			(event_id, event_type, order_id, provider_order_id, provider_capture_id, payload_digest, authenticated, tier)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, true, $7)
		ON CONFLICT (event_id) DO UPDATE
			SET deliveries = merchant_webhook_events.deliveries + 1,
			    last_received_at = now()
		RETURNING event_id`
	if strict {
		// A redelivery may only bump the counter. If the same event id arrives
		// with different bytes, order or capture identity, the original
		// authenticated receipt must not be redefined.
		query = `
		INSERT INTO merchant_webhook_events
			(event_id, event_type, order_id, provider_order_id, provider_capture_id, payload_digest, authenticated, tier)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6, true, $7)
		ON CONFLICT (event_id) DO UPDATE
			SET deliveries = merchant_webhook_events.deliveries + 1,
			    last_received_at = now()
			WHERE merchant_webhook_events.payload_digest = EXCLUDED.payload_digest
			  AND merchant_webhook_events.provider_capture_id IS NOT DISTINCT FROM EXCLUDED.provider_capture_id
			  AND merchant_webhook_events.order_id IS NOT DISTINCT FROM EXCLUDED.order_id
		RETURNING event_id`
	}
	err := tx.QueryRow(ctx, query,
		ev.ID, ev.Type, orderID, ev.ProviderOrderID, ev.CaptureID, hex.EncodeToString(digest[:]), s.evidenceTier()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConflictingWebhook
	}
	if err != nil {
		return fmt.Errorf("merchant: record webhook receipt: %w", err)
	}
	return nil
}

func (s *Service) verifiedCaptureID(ctx context.Context, tx pgx.Tx, orderID string) (string, error) {
	var captureID string
	err := tx.QueryRow(ctx, `
		SELECT provider_capture_id FROM merchant_operations
		 WHERE order_id = $1::uuid AND operation_type = 'CAPTURE'
		   AND status = 'SUCCEEDED' AND provider_capture_id IS NOT NULL
		 ORDER BY updated_at DESC LIMIT 1`, orderID).Scan(&captureID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return captureID, nil
}
