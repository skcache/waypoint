# Waypoint repair report

**Incident:** M3-INC-F4-b223e409-fc13-4787-ab2e-610f2e90800e  
**Fault family:** F4 (evidence tier F)  
**Base revision:** 08b412400118d434d9e348fa2254966f43036b3c-dirty (git)  
**Mode:** oracle  
**Status:** `ACCEPTED_FOR_HUMAN_REVIEW`

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

**Root cause:** In the FaultF4 path, captureEvidence returns the provider order ID (res.ID) instead of the already-validated capture resource ID, causing the wrong provider identity to be persisted as the capture ID.

**Mechanism:** EV-14 shows that the provider actually completed capture CAP-SCRIPT-0001 for USD 12.34, while EV-2 and EV-3 identify ORD-SCRIPT-0001 as the provider order ID; substituting res.ID causes the CAPTURE operation and confirmation event to persist ORD-SCRIPT-0001 as the capture identity (EV-4, EV-10), producing a local CAPTURED_VERIFIED record that conflicts with the actual provider capture identified in EV-1.

**Proposed fix:** Remove the FaultF4 substitution so captureEvidence always returns the capture resource ID produced by completeCapture, and add a regression test requiring persisted and reported capture identities to equal the provider capture resource ID and never the provider order ID.

- Suspected files: apps/api/internal/merchant/service.go
- Evidence: EV-1, EV-2, EV-3, EV-4, EV-10, EV-14
- Invariants at risk: I6
- Uncertainties: EV-6 describes float formatting as the F4 mechanism, but EV-11 and EV-14 show a canonical 12.34 amount and the supplied source only applies float formatting for FaultF1; the provenance of EV-6's mechanism text is unresolved.; The evidence establishes a capture-identity inconsistency, not an amount, currency, duplicate-capture, or fund-loss discrepancy.; Because the evidence tier is F, the behavior was observed only in a scripted fixture and cannot establish real Sandbox behavior.; No evidence shows how downstream reconciliation or payment consumers used the incorrect capture identity.

## Attempt 1 — `ACCEPTED_FOR_HUMAN_REVIEW`

**Patch summary:** Remove the FaultF4 identity substitution so capture verification always persists the validated provider capture resource ID rather than the provider order ID.

```diff
diff --git a/apps/api/internal/merchant/service.go b/apps/api/internal/merchant/service.go
index 5811e1e..e139b12 100644
--- a/apps/api/internal/merchant/service.go
+++ b/apps/api/internal/merchant/service.go
@@ -737,9 +737,6 @@ func (s *Service) captureEvidence(res *paypal.Order, order Order) (string, error
 	if !ok {
 		return "", errors.New("capture response carries no COMPLETED capture with matching amount and currency")
 	}
-	if s.fault == FaultF4 {
-		return res.ID, nil
-	}
 	return captureID, nil
 }
 
```

**Fixture acceptance:** true — provider-issued capture identity, amount, currency and terminal status match the journal; the oracle verified the tested scenario

**Oracle:** VERIFIED_FOR_TESTED_SCENARIOS  
**Order status:** CAPTURED_VERIFIED  
**Claimed capture:** CAP-SCRIPT-0001  
**Provider capture:** CAP-SCRIPT-0001

- `gofmt`: exit 0, 227ms
- `build`: exit 0, 18361ms
- `tests`: exit 0, 6252ms
- `eval`: exit 0, 1242ms

## Model usage

- diagnosis — minimax/minimax-m3.1-flash:free, 66811ms, prompt 44441B, reply 2518B, 12206+6581 tokens
- patch — minimax/minimax-m3.1-flash:free, 7587ms, prompt 39280B, reply 656B, 9776+226 tokens

Candidate sandbox: docker image golang:1.27-alpine; internal network wp-net-7a94a4552003; disposable postgres wp-db-b1a5a2a8430f; non-root uid 501; memory 2g; cpus 2; pids 256; read-only root

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
