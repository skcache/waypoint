# Waypoint repair report

**Incident:** M5-INC-H1-identity-rewrite  
**Fault family:** benchmark (evidence tier F)  
**Base revision:** 92a0e659ec2bf1b7cbc173d345e10230f9378b40 (bench-mutation)  
**Mode:** baseline  
**Status:** `ACCEPTED_FOR_HUMAN_REVIEW`

**Candidate base revision:** `92a0e659ec2bf1b7cbc173d345e10230f9378b40`  
**Patch digest:** `sha256:450e52aef9f271f9b8ad8ccd012244de09e2fb6316ce214d22acf32f625eeda2`

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

**Root cause:** On retry of an unresolved capture, beginCaptureAttempt mutates the durable request id by appending "-retry" (claimed.ProviderRequestID += "-retry") instead of reusing the original request id verbatim, so the retry is not an idempotent replay of the original capture and is definitively refused while the original capture remains completed provider-side.

**Mechanism:** The capture at seq 4 applied provider-side (mutation_applied=true, capture_id CAP-SCRIPT-0001, capture_status COMPLETED, 12.34 USD) but its response was dropped (EV-5 response_dropped=true), so the service correctly classified the outcome as unknown and preserved the operation. On the retry, beginCaptureAttempt called BeginCaptureRetry and then appended "-retry" to the in-memory request id, so the id sent on the wire (32005aae-832c-437a-a9e7-de1d81b78abc-retry, EV-7) no longer matched the durably recorded id (32005aae-832c-437a-a9e7-de1d81b78abc, EV-5). Because the provider did not see a repeat of the original request id, it did not return the original result; it definitively refused the call (EV-7 definite_refusal=true, mutation_applied=false, request_id_durable_before_call=false). The service treated that refusal as a definite rejection and moved the order back to APPROVED, while the provider still holds a COMPLETED capture CAP-SCRIPT-0001 for 12.34 USD. The result is the divergence in the incident artifact: local order_status APPROVED with an empty claimed_capture_id but a provider_capture_id of CAP-SCRIPT-0001 (EV-1), i.e. money captured at the provider while the local journal says the order is still capturable — the precondition for a later double capture.

**Proposed fix:** In beginCaptureAttempt, stop rewriting the request id on the retry branch: delete the `claimed.ProviderRequestID += "-retry"` mutation and return the operation with the original, already-durable ProviderRequestID so the retry is a true same-id replay. Ensure the retry path re-reads provider capture evidence (completeCapture) before sending anything, so an already-completed capture is certified via sealRecoveredCapture rather than re-sent, and so a definite refusal of a mutated id can never be interpreted as proof that no capture occurred.

- Suspected files: apps/api/internal/merchant/service.go
- Evidence: EV-5, EV-7, EV-1, EV-6, EV-2, EV-3, EV-4
- Invariants at risk: I4
- Uncertainties: Evidence tier is F (scripted fixture, not a real Sandbox run), so the provider behaviors are simulated and cannot be treated as real PayPal responses.; The invariant catalog is not present in the evidence; only I4 is directly grounded (named in the service.go comment about committing before the outbound call). The same-id retry identity rule is described in code comments but has no citable invariant id here.; The incident artifact reports total_minor=0 and an empty currency while the provider observations show 12.34 USD; the evidence cannot establish whether this is redaction, a separate local-amount defect, or a fixture artifact.; The evidence cannot explain why the read at EV-6 reports APPROVED after the capture at EV-5 reports COMPLETED (stale_read is flagged false), so the read/write ordering on the provider side is unresolved.; The evidence does not show whether a subsequent capture was attempted, so actual double capture or financial loss cannot be confirmed from these blocks alone.; The behavior of store.BeginCaptureRetry (whether it also rewrites the durable request id) is not visible in the supplied source, so the fix may need a matching change there.

## Attempt 1 — `ACCEPTED_FOR_HUMAN_REVIEW`

**Patch summary:** Remove the retry-branch mutation that appended "-retry" to the durable capture request id. A retry of an unresolved capture must replay the ORIGINAL request id verbatim so the provider returns the original result (idempotent same-id replay) instead of definitively refusing a mutated id and leaving a completed provider-side capture uncertified locally.

```diff
diff --git a/apps/api/internal/merchant/service.go b/apps/api/internal/merchant/service.go
index 82e6e95..e522a70 100644
--- a/apps/api/internal/merchant/service.go
+++ b/apps/api/internal/merchant/service.go
@@ -546,7 +546,10 @@ func (s *Service) beginCaptureAttempt(ctx context.Context, order Order) (Operati
 		if err != nil {
 			return Operation{}, err
 		}
-		claimed.ProviderRequestID += "-retry"
+		// The retry MUST reuse the original, already-durable request id
+		// verbatim: same-id replay is what makes the provider return the
+		// original result instead of treating this as a new capture. The
+		// identity is never rewritten here.
 		return claimed, nil
 	}
 
```

**Fixture acceptance:** true — provider-issued capture identity, amount, currency and terminal status match the journal; the oracle verified the tested scenario

**Oracle:** VERIFIED_FOR_TESTED_SCENARIOS  
**Order status:** CAPTURED_VERIFIED  
**Claimed capture:** CAP-SCRIPT-0001  
**Provider capture:** CAP-SCRIPT-0001

- `gofmt`: exit 0, 261ms
- `build`: exit 0, 19186ms
- `tests`: exit 0, 6303ms
- `fixture`: exit 0, 1245ms
- `judge`: exit 0, 14428ms

## Model usage

- diagnosis — deepseek/deepseek-v4.1-flash, 61774ms, prompt 33763B, reply 4686B, 9185+5353 tokens
- patch — deepseek/deepseek-v4.1-flash, 7911ms, prompt 34217B, reply 1774B, 8681+740 tokens

Candidate sandbox: docker image golang:1.27-alpine; internal network wp-net-e6877eee96f3; disposable postgres wp-db-62a5108969a8; non-root uid 501; memory 2g; cpus 2; pids 256; read-only root; read-only module cache; separate pristine judge cache

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
