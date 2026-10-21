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

`report.json` carries model provenance, per-call latency and reported token
usage, the verification ladder, the applied diff and the acceptance reason.
`eval.json` is the verifier's output from inside the sandbox.
