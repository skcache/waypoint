# Waypoint — deterministic local demo

This is the fastest way to inspect Waypoint without a model key, PayPal
credentials, or a payment. It replays a **previously recorded hosted-model
repair** against the current checkout, runs the real candidate in the real
Docker sandbox, and asks the independent oracle for the verdict.

All provider interactions are **tier F** (deterministic scripted fixture). The
model replies are **recorded**, not live. Nothing is merged, deployed, captured,
refunded, or retried.

## Clean checkout

Prerequisites: Go 1.27+, Node 22+ (`nvm use 22`), Docker, `make`, `curl`.

```bash
git clone https://github.com/skcache/waypoint.git
cd waypoint
cp .env.example .env
make setup
make demo
```

`make demo` starts local PostgreSQL, applies migrations, replays the F3
duplicate-webhook incident, builds and verifies the candidate, executes the
seeded unsafe candidate, builds the workbench, and probes its navigation.

First run may take a few minutes: Docker pulls the Go sandbox image and prefetches
the module cache. Later runs are faster.

## Expected output

The exact IDs vary; these facts do not:

```text
==> replaying the recorded F3 repair (no model call)
model: RECORDED REPLAY recorded:deepseek/deepseek-v4.1-flash (no hosted call)
pre-repair: status=APPROVED accepted=false — ... I8 ...
attempt 1: stage=ACCEPTED_FOR_HUMAN_REVIEW oracle=VERIFIED_FOR_TESTED_SCENARIOS accepted=true
status: ACCEPTED_FOR_HUMAN_REVIEW

WAYPOINT — DETERMINISTIC REPAIR DEMO
1. DEFECT REPRODUCED   F3 · evidence tier F (scripted fixture)
2. RECORDED MODEL      recorded:deepseek/deepseek-v4.1-flash
3. GENERATED DIFF      1 changed file(s)
4. CANDIDATE VERIFIED  patch=true build=true trusted_tests=true
5. INDEPENDENT ORACLE  VERIFIED_FOR_TESTED_SCENARIOS
6. UNSAFE SEED         UNSAFE · I13: the merchant recorded 2 fulfillment effects for one payment
7. HUMAN BOUNDARY      ACCEPTED_FOR_HUMAN_REVIEW · applied_to_worktree=false deployed=false

DEMO OK
  artifacts: .../tmp/demo
  workbench: make dev-web  ->  http://localhost:3000/incidents
  PostgreSQL is still running; stop it with: make db-down
```

Artifacts include `report.json`, `report.md`, `incident.json`,
`pre-repair-eval.json`, the applied diff, the trusted eval, the unsafe test log,
and the workbench build/navigation logs.

## Browse the workbench

```bash
make dev-web          # http://localhost:3000/incidents
```

Open `/incidents`, then **H1-identity-rewrite** (recorded hosted repair) and
**H7-webhook-duplicate-exposed** (unsafe counterexample). Review recording is
disabled by default; no button can approve anything.

Shutdown:

```text
Ctrl-C                 # stop the Next.js dev server
make db-down           # stop PostgreSQL (keeps the volume)
```

## Optional fresh hosted-model repair

The demo above does **not** call a model. To run a new hosted repair, provide
your own provider configuration and a small budget:

```bash
export LLM_API_BASE_URL=https://openrouter.ai/api/v1
export LLM_API_KEY=...                 # never commit this
export LLM_MODEL=deepseek/deepseek-v4.1-flash

make demo-live                         # starts PG, migrates, runs F3 live
```

This may cost money and depends on provider availability. It still uses the
tier-F PayPal fixture: **no PayPal Sandbox capture is made**. Inspect
`tmp/demo-live/report.json` and `report.md`; the model name must not start with
`recorded:`.

## What is intentionally not here

- No support for arbitrary external merchant repositories.
- No real signed PayPal webhooks; the fixture HMAC is not a PayPal signature.
- No production payment, deployment, merge, or automatic approval.
- The M5 benchmark is unchanged; this demo does not rerun H1–H6.
