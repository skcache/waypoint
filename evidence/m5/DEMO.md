# M5 deterministic demo — verified payment repair

This is a five-minute, offline walkthrough of the committed M5 evidence. It
makes **no model calls, no PayPal calls and no payment mutations**. Every number
below is read from `evidence/m5/results.json` and
`apps/web/src/data/m5.json`.

All provider observations in this demo are **tier F**: a scripted fixture, not a
PayPal Sandbox transaction. The webhook authenticity check is a fixture HMAC,
not PayPal signature verification.

## Setup

```bash
cd /path/to/waypoint
nvm use 22.23.3            # Node 22 is required by the web build
make demo-m5               # evidence + sanitization + authorization checks
make dev-web               # http://localhost:3000
```

No PostgreSQL or Docker is needed for the workbench: it renders committed
evidence. The demo does not need any API keys.

## Walkthrough

Open <http://localhost:3000/incidents>. Seven rows appear: six held-out cases
and one clearly marked **exposed** reference.

### 1. A real recorded defect

Open **H1-identity-rewrite**. The pre-repair panel shows:

| Field | Expected |
|---|---|
| Order status | `APPROVED` |
| Oracle verdict | `UNSAFE` |
| Failing invariants | `I4` outbound call before durable identity; `I5` new capture identity after ambiguity |
| Repair | not yet applied |

This is a tier-F replay of a lost capture response: the merchant retries with a
new identity instead of reconciling the original operation.

### 2. Hosted-model diagnosis and patch

The B1 and B2 panels show the recorded replies from
`deepseek/deepseek-v4.1-flash` at temperature 0:

- diagnosis category: `authorization` (recorded as an incorrect category against
  the expected `identity`; the patch was still independently accepted);
- a scoped patch, visible as the generated diff;
- one attempt, with token usage and latency.

The model saw a neutral incident bundle and the candidate source only. It did
not see the case ID, mutation text, reference fix or expected category.

### 3. Candidate build and trusted tests

In the same panel the ladder reports:

- patch applied: yes;
- build: passed;
- trusted scenario gate: passed;
- the adversity (dropped capture response) was still active.

### 4. Independent oracle verdict

The judge's eval reports `VERIFIED_FOR_TESTED_SCENARIOS`, one capture effect,
and the original durable request id. The verdict is produced outside the
candidate's writable scope and is shown as evidence, not as a code claim.

H2 is shown as `ERROR` with the provider's HTTP 402. H3–H6 are shown as
**not run**. Those are deliberate, honest states, not hidden failures.

### 5. Financial counterexample from an unsafe candidate

Open the exposed **H7-webhook-duplicate-exposed** case. Its pre-repair verdict
is `UNSAFE` with three counterexamples:

- `I8` a verified capture is rolled back;
- `I13` two fulfillment effects for one payment;
- `I14` fulfillment before durable capture verification.

A second, independent counterexample is committed at
`evidence/m4-unsafe-f3/eval.json`: a candidate patch that made the tests look
green was still rejected by the oracle as `UNSAFE` (`I13`). This is the core
claim: a plausible patch is not accepted unless the money and fulfillment
evidence is safe.

### 6. Human-review boundary

The right-hand review panel is **read-only by default**. It states that review
recording is disabled and offers no approval buttons. Confirm the API directly:

```bash
curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST -H 'content-type: application/json' \
  -d '{"case_id":"H1-identity-rewrite","condition":"B1","decision":"accept_for_integration"}' \
  http://localhost:3000/api/decisions
# 403
```

A decision is a local JSONL note. It never merges, deploys, captures, refunds or
retries a payment. Recording requires an operator-configured
`WAYPOINT_REVIEW_TOKEN`; without it the endpoint is disabled.

## What the numbers are

| Condition | Held-out attempted | Safe accepted | Verified | Errors | Not run |
|---|---|---|---|---|---|
| B0 rules-only | 6 (diagnosis) | — | — | 0 | 0 |
| B1 single-shot | 2 | 1 | 1 | 1 | 4 |
| B2 oracle-guided | 2 | 1 | 1 | 1 | 4 |

B0 diagnosed 6/6 but cannot patch code; it is not a repair system. H1 was
accepted under both B1 and B2. H2 was blocked by HTTP 402. H3–H6 were not run.
No B1/B2 advantage is claimed, and six cases are not statistical evidence.

## Safety boundaries

- No production deployment, PayPal capture/refund, or signed-webhook claim.
- The workbench only reads generated data and writes local review notes when an
  operator explicitly enables recording.
- The presentation export is a sanitized projection: it contains no mutation
  text, reference fix, expected category, gate configuration, absolute host
  path or credential.
- The hosted benchmark remains incomplete because the provider account has no
  remaining credit; the dataset and the blocker are preserved.
