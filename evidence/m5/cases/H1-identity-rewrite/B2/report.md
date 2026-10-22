# Waypoint repair report

**Incident:** M5-INC-H1-identity-rewrite  
**Fault family:** benchmark (evidence tier F)  
**Base revision:** 92a0e659ec2bf1b7cbc173d345e10230f9378b40 (bench-mutation)  
**Mode:** oracle  
**Status:** `ACCEPTED_FOR_HUMAN_REVIEW`

**Candidate base revision:** `92a0e659ec2bf1b7cbc173d345e10230f9378b40`  
**Patch digest:** `sha256:3bc26a90e1913b0874d9c88c219b57328a555a684f7e75f1c442cd86df6eb105`

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

**Root cause:** The capture retry path in beginCaptureAttempt mints a new provider identity by appending "-retry" to the durable request id instead of re-sending the original request id, so the provider does not recognize the retry as an idempotent replay and definitively refuses it while the original capture has already applied.

**Mechanism:** The first capture call (EV-5) applied provider-side: mutation_applied=true, order_status=COMPLETED, capture_id=CAP-SCRIPT-0001, capture_status=COMPLETED, capture_amount=12.34 USD — but its response was dropped (response_dropped=true), so the client could not seal the result and the operation stayed unresolved. On the retry (EV-7) the code reuses the operation row but rewrites the request id to "32005aae-832c-437a-a9e7-de1d81b78abc-retry", which is a different provider identity than the one that already captured. The provider therefore reports idempotent_replay=false and definite_refusal=true with mutation_applied=false, because the order is already captured under the original id. The result is a split-brain financial state: the provider holds a completed 12.34 USD capture (CAP-SCRIPT-0001) while the local order remains APPROVED (EV-1) and the incident artifact carries no usable amount (total_minor=0, currency empty). The mutated id was also not durably committed before the outbound call (request_id_durable_before_call=false in EV-7), so the identity actually sent to the provider is not recoverable from the journal.

**Proposed fix:** In beginCaptureAttempt, stop mutating the durable request id on retry: reuse the original operation's ProviderRequestID verbatim (remove the `claimed.ProviderRequestID += "-retry"` rewrite) so a repeated capture is a true same-id replay, and persist/confirm that id durably before the outbound call. Additionally, treat a definite refusal that follows an ambiguous capture outcome as non-conclusive: before returning the order to APPROVED, reconcile against provider capture evidence (matching capture id/amount/currency) so an already-applied capture is sealed as CAPTURED_VERIFIED rather than left APPROVED.

- Suspected files: apps/api/internal/merchant/service.go
- Evidence: EV-5, EV-7, EV-1, EV-6
- Invariants at risk: I4, I6
- Uncertainties: Evidence tier is F (scripted fixture), so the provider's real idempotency and refusal behavior is simulated and cannot be confirmed from this evidence.; EV-6 (read returning APPROVED after EV-5 reported order_status COMPLETED with stale_read=false) is internally inconsistent and cannot be explained by the supplied evidence.; EV-1 reports total_minor=0 and an empty currency while EV-2/EV-5/EV-7 sent 12.34 USD; it is not determinable whether this is redaction of the artifact or a genuine loss of the local amount record.; The invariant identifiers beyond I4 (which the source explicitly ties to durable-before-send) are not defined in the supplied evidence, so the mapping of I6 to a specific rule is inferred, not established.; The evidence does not show whether any reconciliation path ran after the refusal, so the current persisted state of the operation row is unknown.

## Attempt 1 — `ACCEPTED_FOR_HUMAN_REVIEW`

**Patch summary:** The capture retry path in beginCaptureAttempt rewrote the durable provider request id by appending "-retry", minting a new provider identity. The provider therefore saw a fresh capture against an already-captured order and definitively refused it, leaving provider funds captured (CAP-SCRIPT-0001) while the local order stayed APPROVED. The fix removes the id mutation so a retry re-sends the original request id verbatim, which the provider recognizes as an idempotent replay and returns the original completed capture for, allowing captureEvidence to seal CAPTURED_VERIFIED.

```diff
diff --git a/apps/api/internal/merchant/service.go b/apps/api/internal/merchant/service.go
index 82e6e95..10b2845 100644
--- a/apps/api/internal/merchant/service.go
+++ b/apps/api/internal/merchant/service.go
@@ -546,7 +546,13 @@ func (s *Service) beginCaptureAttempt(ctx context.Context, order Order) (Operati
 		if err != nil {
 			return Operation{}, err
 		}
-		claimed.ProviderRequestID += "-retry"
+		// A retry MUST re-send the ORIGINAL request id verbatim: the provider
+		// treats a repeated id as an idempotent replay and returns the original
+		// result. Minting a new identity (e.g. a "-retry" suffix) is seen as a
+		// fresh capture against an already-captured order and is definitively
+		// refused, leaving provider funds captured while the local order stays
+		// APPROVED. The id is already durable on the operation row, so the id
+		// sent outbound matches the journaled identity.
 		return claimed, nil
 	}
 
```

**Fixture acceptance:** true — provider-issued capture identity, amount, currency and terminal status match the journal; the oracle verified the tested scenario

**Oracle:** VERIFIED_FOR_TESTED_SCENARIOS  
**Order status:** CAPTURED_VERIFIED  
**Claimed capture:** CAP-SCRIPT-0001  
**Provider capture:** CAP-SCRIPT-0001

- `gofmt`: exit 0, 307ms
- `build`: exit 0, 18388ms
- `tests`: exit 0, 6185ms
- `fixture`: exit 0, 1105ms
- `judge`: exit 0, 13739ms

## Model usage

- diagnosis — deepseek/deepseek-v4.1-flash, 33830ms, prompt 33763B, reply 4140B, 9185+3132 tokens
- patch — deepseek/deepseek-v4.1-flash, 18107ms, prompt 33671B, reply 2127B, 8545+1662 tokens

Candidate sandbox: docker image golang:1.27-alpine; internal network wp-net-ee9dd977ed3f; disposable postgres wp-db-53f2525c8588; non-root uid 501; memory 2g; cpus 2; pids 256; read-only root; read-only module cache; separate pristine judge cache

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
