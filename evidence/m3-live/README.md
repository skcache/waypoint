# M3 live repair evidence (genuine hosted model)

These are unedited artifacts from two real runs of `cmd/repair` at HEAD
`08b4124`. The reports record the revision as `08b4124-dirty` because the
adapter's timeout/token plumbing was still uncommitted during the runs; the
merchant fixture source under repair was exactly `08b4124` (no fixture file was
modified in the working tree at that time). The model was called over
the hosted OpenRouter API (`minimax/minimax-m3.1-flash:free`); no answer was
embedded in the prompt or the runtime, and the model's reply is preserved in
`diagnosis.json` and `patch.diff`.

| Run | Incident | Pre-repair | Model calls | Verdict |
|---|---|---|---|---|
| F4 (held-out response handling) | `M3-INC-F4-*` | oracle UNSAFE, I6 identity mismatch | 2 (diagnosis + patch) | `VERIFIED_FOR_TESTED_SCENARIOS`, accepted for human review |
| F1 (provider payload formatting) | `M3-INC-F1-*` | checkout `FAILED_DEFINITE` (DECIMAL_PRECISION) | 2 (diagnosis + patch) | `VERIFIED_FOR_TESTED_SCENARIOS`, accepted for human review |

Evidence tier is **F** throughout: the provider is the deterministic scripted
fixture. No real PayPal Sandbox transaction was made. The candidate code ran
inside the disposable Docker sandbox described in `report.json`
(`candidate_sandbox`), and the trusted suite plus the independent oracle
decided acceptance — the model never certifies its own patch.

The verifier recorded in these reports is the original single-process
`cmd/repaircheck`. It was replaced after the verifier-integrity review by the
split `cmd/fixturecheck` (candidate-linked observations only) and
`cmd/judgecheck` (trusted oracle process); historical command strings are kept
verbatim.

`report.json` carries model provenance, per-call latency and reported token
usage, the verification ladder, the applied diff and the acceptance reason.
`eval.json` is the verifier's output from inside the sandbox.

## Provenance and digests

The runs applied these diffs to base commit **`08b4124`** (the reports record
`08b4124-dirty` because the adapter's timeout/token plumbing was still
uncommitted; no fixture file was modified). The reports predate the
`patch_digest` field, so the digests below were recomputed from the committed
diffs:

| Run | Patch digest (sha256) |
|---|---|
| F4 | `sha256:a5ca12ba2d69bc1c384a3e6a0595561248cd4d938794cbb9392498c21530f524` |
| F1 | `sha256:7ab46787b484aefe8d8677d46bda5464413ba36ac8ad2f63d60971f39a24398f` |

Newer runs record the same digest and the resolved base commit directly in
`report.json`/`report.md` and in the oracle bundle.

## Credential-history note

Earlier revisions of these reports (commits `a6577b0` and `acb4c39`) contained
the PostgreSQL DSN of the run's **disposable** database container, including
its randomly generated password. That container and its Docker network were
removed when the run finished; the password was never valid outside that
sandbox and cannot be reused. The reports in this directory are sanitized and
the DSN is now `[REDACTED-DSN]`.

Published history was **not rewritten automatically**. If repository policy
requires removing the string from history, rewrite with `git filter-repo`
(or BFG) over the `evidence/m3-live` paths and force-push with the
maintainer's approval; otherwise document the exposure as a rotated, ephemeral
credential. No live Anthropic/PayPal/model credential was ever present in the
artifacts.
