# Waypoint repair report

**Incident:** M3-INC-F2-39efad70-cd15-4a6d-9586-a6b987f97abb  
**Fault family:** F2 (evidence tier F)  
**Base revision:** 648a3d4ba15f8a2905e938f2a59102ae3d2d6fa1 (git-resolve)  
**Mode:** oracle  
**Status:** `ACCEPTED_FOR_HUMAN_REVIEW`

**Candidate base revision:** `648a3d4ba15f8a2905e938f2a59102ae3d2d6fa1`  
**Patch digest:** `sha256:051aab27ff6d472de79588bfacb5199ebf315735c12b457867d7466e05b201c9`

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

**Root cause:** The F2 capture path used legacyAmbiguity, which treated the unreadable capture-response error as definite because it classified only APIError responses with status >= 500 as ambiguous.

**Mechanism:** The provider applied the USD 12.34 capture and completed it, but dropped the response (EV-14). The service then consulted the defective legacy policy and selected the rejection path, recording the capture operation as REJECTED with no provider capture ID even though the failure detail classified the error as AMBIGUOUS (EV-4, EV-9). A later provider read exposed the matching COMPLETED capture (EV-15), and provider-evidence recovery marked the order CAPTURED_VERIFIED (EV-2, EV-10), leaving the local history with a rejected capture attempt, an empty claimed capture ID, and a capture error alongside the successful provider capture and verified order status (EV-1).

**Proposed fix:** Remove the F2 legacy-policy wiring and use canonicalAmbiguity, or have legacyAmbiguity delegate to paypal.AmbiguousOutcome, consistently for capture errors. An unreadable or dropped capture response must mark the durable operation and order as outcome UNKNOWN while preserving the original request ID, never as REJECTED; recovery should set CAPTURED_VERIFIED only from matching COMPLETED provider evidence and update the capture operation and claimed capture identifier consistently with the order.

- Suspected files: apps/api/internal/merchant/ambiguity.go, apps/api/internal/merchant/service.go
- Evidence: EV-14, EV-9, EV-4, EV-15, EV-10, EV-1, EV-2
- Invariants at risk: I6
- Uncertainties: This is tier-F scripted-fixture evidence rather than a real Sandbox run, so the frequency and production impact cannot be established.; The exact dropped HTTP response body and status cannot be reconstructed from the supplied evidence.; The Store implementation is not provided, so the exact persistence path that leaves the historical capture operation REJECTED during later recovery cannot be inspected.; No duplicate capture is evidenced; the supplied provider observations show one capture mutation, but they cannot rule out behavior outside the recorded fixture sequence.

## Attempt 1 — `ACCEPTED_FOR_HUMAN_REVIEW`

**Patch summary:** Keep the legacy configuration hook for compatibility while routing capture-error ambiguity through the canonical provider policy. An unreadable or dropped capture response will now remain durably UNKNOWN with its request identity preserved, allowing matching provider evidence to recover the capture operation and claimed capture ID consistently.

```diff
diff --git a/apps/api/internal/merchant/service.go b/apps/api/internal/merchant/service.go
index eb83779..ca9951a 100644
--- a/apps/api/internal/merchant/service.go
+++ b/apps/api/internal/merchant/service.go
@@ -53,9 +53,8 @@ type Service struct {
 	sameIDRetryWindow time.Duration
 	instanceID        string
 	leaseTTL          time.Duration
-	// ambiguity classifies failed capture calls. The canonical policy treats
-	// an unreadable 2xx response as "may have applied"; the fixture can wire
-	// the legacy policy to reproduce the F2 integration defect.
+	// ambiguity classifies failed capture calls using the canonical provider
+	// policy, which treats an unreadable 2xx response as "may have applied".
 	ambiguity ambiguityPolicy
 	// webhooks verifies provider event authenticity; applyWebhook is the
 	// selected event handler (canonical, or the fixture's legacy defect).
@@ -97,10 +96,10 @@ func (s *Service) SetWebhookVerifier(v WebhookVerifier) { s.webhooks = v }
 // it exists only so the fixture can reproduce a seeded application defect.
 func (s *Service) UseLegacyWebhook() { s.applyWebhook = s.legacyApplyWebhookEvent }
 
-// UseLegacyAmbiguity wires the historical (defective) capture-ambiguity
-// policy. It exists so the fixture can reproduce the F2 defect; production
-// wiring always uses the canonical policy.
-func (s *Service) UseLegacyAmbiguity() { s.ambiguity = legacyAmbiguity{} }
+// UseLegacyAmbiguity retains the legacy configuration hook while ensuring
+// capture failures use canonical provider ambiguity classification. An
+// unreadable response may mean that the capture applied.
+func (s *Service) UseLegacyAmbiguity() { s.ambiguity = canonicalAmbiguity{} }
 
 // newInstanceID identifies this process as the owner of capture attempts.
 func newInstanceID() string {
```

**Fixture acceptance:** true — provider-issued capture identity, amount, currency and terminal status match the journal; the oracle verified the tested scenario

**Oracle:** VERIFIED_FOR_TESTED_SCENARIOS  
**Order status:** CAPTURED_VERIFIED  
**Claimed capture:** CAP-SCRIPT-0001  
**Provider capture:** CAP-SCRIPT-0001

- `gofmt`: exit 0, 319ms
- `build`: exit 0, 18066ms
- `tests`: exit 0, 6166ms
- `fixture`: exit 0, 1135ms
- `judge`: exit 0, 14339ms

## Model usage

- diagnosis — minimax/minimax-m3.1-flash:free, 94726ms, prompt 48043B, reply 3382B, 13155+5677 tokens
- patch — minimax/minimax-m3.1-flash:free, 44209ms, prompt 42930B, reply 2115B, 10575+5999 tokens

Candidate sandbox: docker image golang:1.27-alpine; internal network wp-net-5ce814467fc8; disposable postgres wp-db-f857720f5bb8; non-root uid 501; memory 2g; cpus 2; pids 256; read-only root; read-only module cache; separate pristine judge cache

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
