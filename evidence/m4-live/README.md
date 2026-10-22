# M4 live repair evidence (tier F)

This directory contains the raw artifacts of the M4 adversarial repair
experiments. Every provider interaction is the deterministic scripted fixture
(tier **F**). No PayPal Sandbox transaction, webhook or capture was performed
for M4. The webhook authenticity boundary is the **fixture HMAC verifier**, not
a PayPal signature; the signed-provider path is unimplemented and reported as
unverified.

The model prompts contain only the bounded evidence bundle and the
candidate-writable source files. Credentials are read from the environment at
runtime and never appear in prompts or artifacts; the reports were produced by
the pipeline's redactor.

## Results matrix

| Family | Defect (seeded, tier F) | Hosted model | Attempts | Tokens (in/out) | Latency | Result | Patch sha256 |
|---|---|---|---|---|---|---|---|
| F1 | malformed provider amount (`12.340000`) | M3 evidence, reused | 1 | 11,085 / 1,980 + 9,728 / 5,956 | 21.3 s + 69.6 s | ACCEPTED (M3) | see `evidence/m3-live/f1` |
| F2 | legacy ambiguity policy treats an unreadable 2xx capture response as a definite rejection | `minimax/minimax-m3.1-flash:free` | 1 | 23,409 / 8,253 | 130.7 s model | ACCEPTED | `b51163cdc99adb5b…` (`f2/patch.diff`) |
| F3 | legacy webhook handler keys fulfillment by event id and writes event status onto the order | `deepseek/deepseek-v4.1-flash` | 1 | 22,639 / 11,597 | 33.7 s model | ACCEPTED | `5625ca9426f661ea…` (`f3/patch.diff`) |

Both accepted candidates were applied only in a disposable worktree. Nothing
was merged, pushed to a product branch, or deployed. A human owns integration.

### F2 — lost capture response

- Baseline: `2d7dde3e7ec6`; fixture: provider applies the capture, drops the
  response, the process restarts, a second `Capture` call recovers.
- Pre-repair: order `CAPTURED_VERIFIED` in the end, but the journal records the
  ambiguous attempt as `REJECTED` and the later verification has no successful
  capture operation behind it. Oracle: **UNSAFE**, `I5` counterexample
  (`f2/pre-repair-eval.json`).
- Model diagnosis: the legacy ambiguity policy classifies only provider 5xx as
  ambiguous. Patch: the capture path calls `paypal.AmbiguousOutcome(err)`
  directly in `service.go` (it bypasses the policy field rather than editing
  `ambiguity.go`; recorded as such).
- Post-repair: one capture effect, the dropped response still injected, order
  `CAPTURED_VERIFIED`, operation bound to the provider capture id. Oracle:
  **VERIFIED** (`f2/eval.json`).
- The first accepted F2 run (`f2-shortcut/`, base `648a3d4`) repaired the
  defect by pointing the legacy constructor hook at the canonical policy. That
  was a wiring shortcut, so the fixture wiring was moved to the reserved
  `fixture_wiring.go` and the run was repeated. The shortcut artifact is kept
  for audit; it is **not** the accepted evidence.

### F3 — duplicate / reordered webhooks

- Baseline: `7cd25ae2c689`; the journey delivers a duplicate, a second event id
  for the same capture, a late approval, eight concurrent handlers, a restart
  replay, an early event and an unauthenticated event.
- Pre-repair: order `APPROVED` (regressed), **two** fulfillment effects.
  Oracle: **UNSAFE**, `I8`, `I13`, `I14` (`f3/pre-repair-eval.json`).
- Model diagnosis: event payload treated as the source of truth. Patch: verify
  the order is `CAPTURED_VERIFIED`, require the event capture id to match the
  sealed capture, use the stable `FULFILL` effect key, and stop writing status
  from events (`f3/patch.diff`).
- Post-repair: order `CAPTURED_VERIFIED`, exactly one fulfillment effect,
  duplicate deliveries recorded (11 durable deliveries for `evt-cap-1`), the
  unauthenticated delivery rejected. Oracle: **VERIFIED** (`f3/eval.json`).

### Unsafe candidate (seeded, not AI-generated)

`m4-unsafe-f3/` is a candidate **we wrote by hand**: it removes the visible
status regression but keeps the per-event fulfillment key. It compiles, every
handler call succeeds, and the order ends `CAPTURED_VERIFIED` — yet two events
for one capture create two effects. The trusted financial test rejects it before
the judge runs; when the same binary is exercised outside that test, the
independent judge rejects it with an **I13** counterexample
(`m4-unsafe-f3/eval.json`, patch `27f7fde907eff700…`). It must not be described
as model-generated.

## Failed model attempts (kept honest)

| Run | Model | Outcome |
|---|---|---|
| `m4-live-f3-budget`, `m4-live-f3c-truncated` (not committed) | `minimax/minimax-m3.1-flash:free` | diagnosis ok; patch call exhausted the output budget with no text (3 attempts across runs). The free model could not emit the F3 patch. |
| F3 with `deepseek/deepseek-v4.1-flash`, 8k output | deepseek | diagnosis reply was cut off mid-JSON (invalid schema). Retried at 16k; diagnosis valid; patch accepted. |

Model/provider failures are reported as failures; no attempt was relabeled as
success. Usage figures above come from the provider's reported token counts;
pricing was not reported, so no cost is claimed.

## Trust scope

The judge (cmd/judgecheck) imports the oracle only and reads the journal with
its own SQL, in a process that never loads candidate code. Its acceptance is
independent of the candidate's own claims. It does **not** authenticate real
provider behavior: the tier-F fixture writes the durable journal and provider
observations, and the HMAC verifier only proves the fixture produced the event
bytes. M4 makes no claim about signed PayPal webhooks, and no statistical
generalization beyond the executed scenarios.
