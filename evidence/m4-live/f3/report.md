# Waypoint repair report

**Incident:** M3-INC-F3-732229b4-03f6-4af1-bbee-6cb80c53619b  
**Fault family:** F3 (evidence tier F)  
**Base revision:** 7cd25ae2c689685f28c6f395ab55aecc9d19521c (git-resolve)  
**Mode:** oracle  
**Status:** `ACCEPTED_FOR_HUMAN_REVIEW`

**Candidate base revision:** `7cd25ae2c689685f28c6f395ab55aecc9d19521c`  
**Patch digest:** `sha256:5625ca9426f661ea7c381cc9c9edcabaa66316d6f962b1522f545d91cc0534fe`

## Verification ladder

| Stage | Result |
|---|---|
| Diagnosis produced | true |
| Patch generated | true |
| Patch applied | true |
| Build passed | true |
| Trusted tests passed | true |
| Oracle verdict | VERIFIED_FOR_TESTED_SCENARIOS |
| Accepted for human review | true |

## Diagnosis

**Root cause:** The F3 legacy webhook handler (legacyApplyWebhookEvent) treats the provider event payload as the source of truth: it writes ORDER.APPROVED directly onto merchant_orders and keys fulfillment by event id, so a late/duplicate approval event rewinds an already-verified capture back to APPROVED and can mint duplicate fulfillment effects.

**Mechanism:** Capture completed normally: the CAPTURE operation row is SUCCEEDED with ProviderCaptureID CAP-SCRIPT-0001 (EV-4) and the provider confirms a COMPLETED capture of 12.34 USD under the same request id (EV-9, EV-13), which is the path that seals the order as CAPTURED_VERIFIED. The order row nevertheless reads Status=APPROVED with UpdatedAt 19:22:08.856107 (EV-2), i.e. ~11 ms AFTER the capture operation was updated at 19:22:08.845393 (EV-4) and after the provider capture confirmation (EV-9). The only code path in the supplied sources that writes StatusApproved from an inbound event is legacyApplyWebhookEvent's ORDER.APPROVED branch, which unconditionally UPDATEs merchant_orders.status; the canonical applyWebhookEvent never advances or rewinds payment state from an event. The result is a financially inconsistent record: money is captured provider-side (EV-1 claimed_capture_id CAP-SCRIPT-0001, EV-9, EV-13) while the local order is presented as merely APPROVED, and the same handler's event-id-keyed effect_key allows more than one fulfillment effect for one capture.

**Proposed fix:** Stop wiring the legacy handler: have HandleWebhook dispatch to the canonical applyWebhookEvent (remove the F3 legacy selection in fixture_wiring.go). The canonical handler must remain the only event path: record a durable receipt keyed by provider event id, never write order status from an event, fulfill only when the local order is already CAPTURED_VERIFIED and the event's capture id equals the sealed provider_capture_id, and use a stable per-order effect key (e.g. 'FULFILL') so redelivered or distinct events for the same capture yield exactly one fulfillment effect.

- Suspected files: apps/api/internal/merchant/webhook.go, apps/api/internal/merchant/fixture_wiring.go
- Evidence: EV-1, EV-2, EV-4, EV-9, EV-13
- Invariants at risk: I6
- Uncertainties: No merchant_webhook_events or merchant_fulfillment_effects rows are included in the evidence, so the specific ORDER.APPROVED delivery (and any duplicate fulfillment effect) is inferred from timestamp ordering and the F3 wiring, not directly observed.; fixture_wiring.go, which selects the legacy handler, is referenced in the sources but was not provided for inspection, so the exact wiring line is unconfirmed.; Evidence tier is F (scripted fixture), so this diagnosis describes the fixture's seeded defect and does not establish behavior of a real PayPal Sandbox or production integration.; The evidence cannot establish whether any downstream consumer already acted on the rewound APPROVED status or on a duplicated fulfillment effect.

## Attempt 1 — `ACCEPTED_FOR_HUMAN_REVIEW`

**Patch summary:** The F3 legacy webhook handler (legacyApplyWebhookEvent) trusted the event payload as the source of truth: it rewound merchant_orders.status to APPROVED on any ORDER.APPROVED event (even after a verified capture) and keyed fulfillment by event id, allowing duplicate effects. This patch makes the legacy handler apply the canonical event semantics: it no longer writes order status from an event, it fulfills only when the local order is already CAPTURED_VERIFIED and the event's capture id matches the sealed provider_capture_id, and it uses the stable per-order effect key 'FULFILL' so redelivered or distinct events for one capture yield exactly one fulfillment effect. The F3 wiring is left intact; the defective behavior it exposes is fixed.

```diff
diff --git a/apps/api/internal/merchant/webhook.go b/apps/api/internal/merchant/webhook.go
index 58d9a23..d84ca2b 100644
--- a/apps/api/internal/merchant/webhook.go
+++ b/apps/api/internal/merchant/webhook.go
@@ -96,7 +96,7 @@ func (s *Service) legacyApplyWebhookEvent(ctx context.Context, ev WebhookEvent)
 	}
 	defer func() { _ = tx.Rollback(ctx) }()
 
-	orderID, _, err := s.webhookOrder(ctx, tx, ev.ProviderOrderID)
+	orderID, status, err := s.webhookOrder(ctx, tx, ev.ProviderOrderID)
 	if err != nil {
 		return err
 	}
@@ -108,14 +108,20 @@ func (s *Service) legacyApplyWebhookEvent(ctx context.Context, ev WebhookEvent)
 	}
 
 	switch ev.Type {
-	case "ORDER.APPROVED":
-		if _, err := tx.Exec(ctx, `UPDATE merchant_orders SET status = $2, updated_at = now() WHERE id = $1::uuid`, orderID, StatusApproved); err != nil {
+	case "PAYMENT.CAPTURE.COMPLETED":
+		if status != StatusCapturedVerified {
+			break
+		}
+		verifiedCapture, err := s.verifiedCaptureID(ctx, tx, orderID)
+		if err != nil {
 			return err
 		}
-	case "PAYMENT.CAPTURE.COMPLETED":
+		if ev.CaptureID == "" || ev.CaptureID != verifiedCapture {
+			break
+		}
 		if _, err := tx.Exec(ctx, `
 			INSERT INTO merchant_fulfillment_effects (order_id, effect_key, event_id, tier)
-			VALUES ($1::uuid, $2, $2, $3)
+			VALUES ($1::uuid, 'FULFILL', $2, $3)
 			ON CONFLICT (order_id, effect_key) DO NOTHING`,
 			orderID, ev.ID, s.evidenceTier()); err != nil {
 			return err
```

**Fixture acceptance:** true — exactly one fulfillment effect is backed by an authenticated capture event; the oracle verified the tested scenario

**Oracle:** VERIFIED_FOR_TESTED_SCENARIOS  
**Order status:** CAPTURED_VERIFIED  
**Claimed capture:** CAP-SCRIPT-0001  
**Provider capture:** CAP-SCRIPT-0001

- `gofmt`: exit 0, 363ms
- `build`: exit 0, 24310ms
- `tests`: exit 0, 7379ms
- `fixture`: exit 0, 1360ms
- `judge`: exit 0, 16514ms

## Model usage

- diagnosis — deepseek/deepseek-v4.1-flash, 8961ms, prompt 42740B, reply 4218B, 12273+2142 tokens
- patch — deepseek/deepseek-v4.1-flash, 24720ms, prompt 39958B, reply 2666B, 10366+9455 tokens

Candidate sandbox: docker image golang:1.27-alpine; internal network wp-net-df38acc71ceb; disposable postgres wp-db-bc2b7d217348; non-root uid 501; memory 2g; cpus 2; pids 256; read-only root; read-only module cache; separate pristine judge cache

Cost: not computed (the provider did not report pricing).

## What was applied or deployed

- Applied to the developer's working tree: **false**
- Committed/pushed/merged: **false**
- Deployed: **false**
- Human approval required before any integration: **true**

## Known limitations

- All provider interaction in this run is deterministic scripted-fixture instrumentation (tier F). No real PayPal Sandbox transaction was executed by the repair pipeline.
- The verifier judges behavior, not intent: a candidate that deletes the injected fault switch instead of fixing the underlying behavior can still pass if the resulting behavior is correct. Read the diff with that in mind.
- Resource bounds are wall-clock timeouts, capped output and an advisory Go memory limit (GOMEMLIMIT); there is no hard OS memory cgroup on this platform.
- Token usage is recorded only when the provider reports it; cost is not computed without a pricing source.
- The model proposes; the independent oracle and the fixture acceptance rules decide. VERIFIED_FOR_TESTED_SCENARIOS covers only the scenario that actually executed.
- Nothing was applied to the developer's working tree, committed, pushed, merged or deployed. A human owns any integration decision.

## Human review checklist

- [ ] Read the diff and confirm it fixes behavior rather than suppressing detection.
- [ ] Run `make test-integration` and the oracle suite on the patched revision.
- [ ] Confirm the evidence tier is understood (F = scripted, R = real Sandbox).
- [ ] Decide whether a real Sandbox checkout is required before integration.
