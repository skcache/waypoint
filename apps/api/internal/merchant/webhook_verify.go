package merchant

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrUnauthenticatedWebhook means the delivery failed authenticity
// verification. The handler must reject it without touching durable state.
var ErrUnauthenticatedWebhook = errors.New("merchant: webhook failed authenticity verification")

// ErrConflictingWebhook means an authenticated delivery reused a durable event
// id with different content (payload digest, order or capture identity). The
// original receipt is never redefined; the conflicting delivery is rejected.
var ErrConflictingWebhook = errors.New("merchant: webhook event id conflicts with an existing authenticated receipt")

// WebhookEvent is one authenticated delivery, already parsed by a verifier.
// Handlers must treat every field as untrusted input that only became
// actionable because the verifier accepted the raw payload.
type WebhookEvent struct {
	ID              string
	Type            string
	ProviderOrderID string
	CaptureID       string
	Raw             []byte
}

// WebhookVerifier establishes that a delivery is authentic. The production
// implementation for PayPal's signed webhooks is NOT part of M4 (no new
// Sandbox webhook configuration was authorized); the interface exists so the
// handler does not depend on a fixture-only mechanism, and the real
// signed-event path is reported as unverified.
type WebhookVerifier interface {
	Verify(payload []byte, headers map[string]string) (WebhookEvent, error)
}

// fixtureSignatureHeader is deliberately vendor-neutral: an event signed with
// this header is a TRUSTED FIXTURE event (tier F), never a PayPal-signed
// event. Nothing in the pipeline may present it as genuine provider proof.
const fixtureSignatureHeader = "X-Waypoint-Fixture-Signature"

type fixtureWebhookPayload struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	ProviderOrderID string `json:"provider_order_id"`
	CaptureID       string `json:"capture_id"`
}

// FixtureWebhookVerifier is the tier-F authenticity boundary: HMAC-SHA256 over
// the exact raw payload with a per-run secret. It proves the fixture produced
// the event and that the bytes were not altered, nothing more.
type FixtureWebhookVerifier struct {
	secret []byte
}

func NewFixtureWebhookVerifier(secret string) *FixtureWebhookVerifier {
	return &FixtureWebhookVerifier{secret: []byte(secret)}
}

func (v *FixtureWebhookVerifier) Verify(payload []byte, headers map[string]string) (WebhookEvent, error) {
	got, err := hex.DecodeString(headers[fixtureSignatureHeader])
	if err != nil || len(got) == 0 {
		return WebhookEvent{}, fmt.Errorf("%w: missing or malformed signature", ErrUnauthenticatedWebhook)
	}
	mac := hmac.New(sha256.New, v.secret)
	mac.Write(payload)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return WebhookEvent{}, fmt.Errorf("%w: signature mismatch", ErrUnauthenticatedWebhook)
	}
	var p fixtureWebhookPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return WebhookEvent{}, fmt.Errorf("webhook payload is unreadable: %w", err)
	}
	if p.ID == "" || p.Type == "" {
		return WebhookEvent{}, errors.New("webhook payload is missing id or type")
	}
	return WebhookEvent{ID: p.ID, Type: p.Type, ProviderOrderID: p.ProviderOrderID, CaptureID: p.CaptureID, Raw: payload}, nil
}

// SignFixtureWebhook produces the fixture signature for a payload. It is used
// by the trusted replay harness only.
func SignFixtureWebhook(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// FixtureSignatureHeader exposes the header name to the trusted harness.
func FixtureSignatureHeader() string { return fixtureSignatureHeader }
