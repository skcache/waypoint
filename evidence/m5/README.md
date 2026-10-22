# M5 held-out repair benchmark — results and evidence

This directory is written by `apps/api/cmd/bench`. `results.json` is the
machine-readable dataset; the workbench renders `apps/web/src/data/m5.json`,
generated from the same results. Nothing here is hand-edited.

## Corpus

Frozen before the hosted evaluation:

- benchmark `waypoint-m5-heldout-v1`
- digest `sha256:8a094d39caf5…` (see `benchmark.json`)
- corpus frozen at `8e29636fb0`; hosted/evaluation runs used `fa7b3283c8`
  (harness fixes after the freeze; the corpus digest is unchanged)
- six **held-out** mechanisms plus one **exposed** reference (the M4 duplicate
  webhook mechanism, excluded from held-out totals)

| Case | Family | Expected diagnosis | Acceptance |
|---|---|---|---|
| H1-identity-rewrite | idempotency/identity | identity | verified repair |
| H2-webhook-conflict | webhook/fulfillment | fulfillment | verified repair, 1 effect |
| H3-currency-mismatch | monetary evidence | monetary | safe refusal |
| H4-pending-capture | response interpretation | status | safe refusal |
| H5-early-fulfillment | webhook/fulfillment | fulfillment | verified repair, 1 effect |
| H6-ambiguous-reconcile | ambiguity/reconciliation | ambiguity | safe refusal |
| H7-webhook-duplicate-exposed | webhook/fulfillment | fulfillment | exposed reference only |

Each case is a controlled mutation applied to a detached worktree at the pinned
base. The mutation is committed and becomes the repair pipeline's `BaseRef`.
The journey (provider adversity) lives in trusted replay code and stays active
after the repair. The defect injector is separate from the candidate's writable
surface.

## Contamination controls

- The model prompt receives only the bounded evidence bundle and the
  candidate-writable source files. It never receives the case ID, the mutation
  text, the reference fix, the expected category, the hidden gate logic, or
  previous successful answers.
- The evidence label is neutral (`payment integration incident`); no
  `F1`/`F2`/case number appears.
- `TestModelVisiblePromptsCarryNoCaseOrReference` asserts the static prompt
  templates contain no case ID, mutation replacement or reference fix.
- The corpus digest is frozen before any hosted run
  (`evidence/m5/benchmark.json`) and `TestFrozenCorpusMatchesRegistry` fails if
  the registry drifts.
- `TestMutationsApplyExactlyOnce` fails if a mutation no longer matches the
  source exactly once.

## Results actually obtained

Raw counts. No statistical significance is claimed.

| Condition | Held-out attempted | Safe accepted | Verified | Safe refusal | Rejected | Errors | Not run |
|---|---|---|---|---|---|---|---|
| B0 (rules, diagnosis only) | 6 | — | — | — | — | 0 | 0 |
| B1 (single-shot AI) | 2 | 1 | 1 | 0 | 0 | 1 | 4 |
| B2 (oracle-guided AI) | 2 | 1 | 1 | 0 | 0 | 1 | 4 |

- **B0** diagnosed 6/6 by the deterministic rules (the rules read the
  independent oracle's failing invariants). B0 cannot write a patch; it is not
  scored as a repair system.
- **H1** was accepted under both B1 and B2 by the trusted judge, with one
  capture effect and the original durable request identity. Model:
  `deepseek/deepseek-v4.1-flash`, temperature 0. B1 used 17,866 prompt / 6,093
  completion tokens; B2 used 17,730 / 4,794. The model's diagnosis category was
  `authorization`, scored incorrect against the expected `identity`; acceptance
  came from the build, trusted gate and oracle, not from the label.
- **H2** failed with `HTTP 402` (provider credit exhausted) in both conditions.
  The failure is recorded, not retried until green.
- **H3–H6** are **NOT RUN** for B1/B2 because the same budget was exhausted.
  Their pre-repair phase and B0 diagnosis did run, and every one is UNSAFE
  before repair.
- A separate free-model attempt (`minimax/minimax-m3.1-flash:free`) on H3 also
  failed (output-budget exhaustion). It is a different model and is **not**
  part of the controlled B1/B2 comparison.

The denominator is therefore **6 held-out cases in the corpus, 2 attempted
under B1/B2, 4 not run**. This is not a completed benchmark; it is the honest
state after the provider budget was exhausted.

### Model-budget check (2026-10-22)

The configured hosted model was re-checked before any further work. The
provider account reports `total_credits=0` and `total_usage=0.189259787`. No
resume was attempted: the next call would only reproduce the HTTP 402, and no
other model was substituted. H2 remains `ERROR`; H3–H6 remain `NOT RUN`.

## Workbench security and sanitized export

- `/api/decisions` is **disabled by default**. `POST` returns `403` and writes
  nothing unless the deployment sets `WAYPOINT_REVIEW_TOKEN` (at least 16
  characters); with it, the request must carry that exact bearer token or it
  gets `401`. No anonymous visitor can record an integration approval.
- Recording only appends a local JSONL note. It never merges a patch, deploys,
  captures, refunds or retries a payment.
- `apps/web/src/data/m5.json` is produced by an explicit sanitized projection
  (`bench.Project`), not by serializing the evaluator dataset. It omits defect
  mutation text, reference fixes, expected categories, gate configuration,
  prompt facts and absolute artifact paths. Reviewable diffs, reports, evals
  and provider observations are retained. A generated repair diff necessarily
  shows the source line the candidate changed; that is candidate output, not
  the injector's mutation specification or reference fix.
- `results.json` itself no longer serializes mutations, reference fixes,
  expected categories or host artifact paths.
- `TestPublicProjectionExcludesEvaluatorData`,
  `TestCommittedWebDataHasNoEvaluatorData`,
  `TestCommittedResultsHasNoEvaluatorFields` and
  `src/lib/review-guard.test.mjs` guard these boundaries.

## Deterministic demo

See [`DEMO.md`](DEMO.md) or run `make demo-m5` followed by `make dev-web`. The
demo is offline, uses committed evidence, and needs no PostgreSQL or API keys.


## What is demonstrated

- A real hosted model diagnosed H1 and produced a scoped patch that the
  independent judge accepted, with the adversity still active.
- Every held-out pre-repair run produced a trustworthy unsafe verdict from the
  independent oracle (I4/I5, I8/I13/I14, I6, I5).
- The trusted gates for all six journeys accept the corresponding safe
  behavior (verified repair or explicit safe refusal) when run against clean
  code.
- The workbench renders the real artifacts, including the NOT RUN and ERROR
  states.

## Limitations

- Six cases are a demonstration corpus, not a benchmark with statistical power.
- The hosted comparison is incomplete (2/6 attempted). B1 vs B2 cannot be
  compared on one accepted case.
- All provider evidence is tier F (scripted fixture). No PayPal Sandbox
  transaction or signed webhook was used; the webhook verifier is a fixture
  HMAC.
- The judge authenticates the evidence it evaluates, not real provider
  behavior. The tier-F fixture writes the journal it runs against.
- Cost is estimated from list prices; the API does not report pricing.
- A zero observed false-acceptance count does not establish a zero real-world
  probability.
