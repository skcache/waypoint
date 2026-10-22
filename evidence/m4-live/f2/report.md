# Waypoint repair report

**Incident:** M3-INC-F2-98fa60a3-3be2-401a-a788-e659ca5846ce  
**Fault family:** F2 (evidence tier F)  
**Base revision:** 2d7dde3e7ec6d6368bc6f485a2cb060cdc6611b8 (git-resolve)  
**Mode:** oracle  
**Status:** `ACCEPTED_FOR_HUMAN_REVIEW`

**Candidate base revision:** `2d7dde3e7ec6d6368bc6f485a2cb060cdc6611b8`  
**Patch digest:** `sha256:b51163cdc99adb5b21c970acd5b218d60377bf79e33dd1e4450f5fe7b644804a`

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

**Root cause:** The F2 capture path incorrectly classifies an unreadable successful capture response as a definite rejection because legacyAmbiguity only treats provider 5xx API errors as ambiguous.

**Mechanism:** The provider applied the USD 12.34 capture but dropped its response, producing an unreadable-response error; legacyAmbiguity returned false for that error, so the service took its rejection branch and persisted a REJECTED capture operation with no capture ID even though CAP-SCRIPT-0001 existed, after which a later provider read recovered the capture and marked the order CAPTURED_VERIFIED, leaving the rejected-operation history inconsistent with the provider-recorded capture.

**Proposed fix:** Make all capture-outcome decisions use the canonical paypal.AmbiguousOutcome classification, including the F2 fixture wiring, rather than the 5xx-only shortcut; an unreadable 2xx response or transport failure should mark the operation and order outcome unknown, preserve the original request ID, and proceed only through provider reconciliation or a bounded same-ID retry.

- Suspected files: apps/api/internal/merchant/ambiguity.go, apps/api/internal/merchant/service.go
- Evidence: EV-1, EV-4, EV-9, EV-10, EV-14, EV-15
- Invariants at risk: I6
- Uncertainties: The evidence is tier F from a scripted fixture, so it does not establish behavior against the real PayPal Sandbox or production.; The file that wires legacyAmbiguity into the F2 scenario was not supplied, so that wiring is inferred from the implementation and incident pattern rather than directly inspected.; The supplied observations show one provider capture and no duplicate capture, but they cannot establish whether any separate customer-visible effect or additional capture occurred outside the fixture.

## Attempt 1 — `ACCEPTED_FOR_HUMAN_REVIEW`

**Patch summary:** Use PayPal's canonical ambiguity classifier for capture failures so an unreadable successful response or transport failure leaves the capture operation unresolved instead of recording a definite rejection.

```diff
diff --git a/apps/api/internal/merchant/service.go b/apps/api/internal/merchant/service.go
index eeec14a..32a5a4a 100644
--- a/apps/api/internal/merchant/service.go
+++ b/apps/api/internal/merchant/service.go
@@ -463,7 +463,7 @@ func (s *Service) Capture(ctx context.Context, orderID string) (Order, error) {
 	result, err := s.api.CaptureOrder(ctx, order.ProviderOrderID, op.ProviderRequestID)
 	if err != nil {
 		detail := failureDetail(op, err)
-		if s.ambiguity.Ambiguous(err) {
+		if paypal.AmbiguousOutcome(err) {
 			if _, mErr := s.store.MarkCaptureUnknown(ctx, orderID, op.ID, op.Fence, detail, s.evidenceTier()); mErr != nil {
 				return Order{}, mErr
 			}
```

**Fixture acceptance:** true — provider-issued capture identity, amount, currency and terminal status match the journal; the oracle verified the tested scenario

**Oracle:** VERIFIED_FOR_TESTED_SCENARIOS  
**Order status:** CAPTURED_VERIFIED  
**Claimed capture:** CAP-SCRIPT-0001  
**Provider capture:** CAP-SCRIPT-0001

- `gofmt`: exit 0, 384ms
- `build`: exit 0, 18505ms
- `tests`: exit 0, 6583ms
- `fixture`: exit 0, 1200ms
- `judge`: exit 0, 14972ms

## Model usage

- diagnosis — minimax/minimax-m3.1-flash:free, 75408ms, prompt 47674B, reply 2963B, 13030+5598 tokens
- patch — minimax/minimax-m3.1-flash:free, 55271ms, prompt 42136B, reply 775B, 10379+2655 tokens

Candidate sandbox: docker image golang:1.27-alpine; internal network wp-net-e6f71b910b77; disposable postgres wp-db-feb9f3800d3d; non-root uid 501; memory 2g; cpus 2; pids 256; read-only root; read-only module cache; separate pristine judge cache

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
