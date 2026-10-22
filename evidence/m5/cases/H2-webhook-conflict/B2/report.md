# Waypoint repair report

**Incident:** M5-INC-H2-webhook-conflict  
**Fault family:** benchmark (evidence tier F)  
**Base revision:** dfef311407bd16debfa2a6ce639c7db8de261328 (bench-mutation)  
**Mode:** oracle  
**Status:** `ERROR`

**Candidate base revision:** `dfef311407bd16debfa2a6ce639c7db8de261328`

## Verification ladder

| Stage | Result |
|---|---|
| Diagnosis produced | false |
| Patch generated | false |
| Patch applied | false |
| Build passed | false |
| Trusted tests passed | false |
| Oracle verdict |  |
| Accepted for human review | false |

## Model usage

-  — deepseek/deepseek-v4.1-flash, 0ms, prompt 0B, reply 0B, tokens not reported

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
- the model call failed: llm: UNAVAILABLE (HTTP 402): the provider did not complete the request
- No repair candidate passed independent verification in this run; the incident remains open.

## Human review checklist

- [ ] Read the diff and confirm it fixes behavior rather than suppressing detection.
- [ ] Run `make test-integration` and the oracle suite on the patched revision.
- [ ] Confirm the evidence tier is understood (F = scripted, R = real Sandbox).
- [ ] Decide whether a real Sandbox checkout is required before integration.
