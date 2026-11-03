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
seeded unsafe candidate, exports a sanitized run into the workbench, builds
the frontend, probes navigation, and (when `CHROME_PATH` is set) runs the
browser smoke test. The fixture generates fresh order/incident IDs and
timestamps per run; the defect, patch, verdicts and I13 counterexample are
deterministic.

First run may take a few minutes: Docker pulls the Go sandbox image and prefetches
the module cache. Later runs are faster. `tmp/demo` is a fixed generated-artifact
directory; `make test-demo-paths` covers traversal and symlink attempts.

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
6a. HISTORICAL ORACLE  UNSAFE · I13: the merchant recorded 2 fulfillment effects for one payment
    source: evidence/m4-unsafe-f3/eval.json (recorded M4 independent judge; not executed now)
6b. FRESH EXECUTION    trusted gate rejected the candidate (2 fulfillment effects)
7. HUMAN BOUNDARY      ACCEPTED_FOR_HUMAN_REVIEW
   candidate_patch_applied_in_disposable_worktree=true
   developer_tree_applied=false deployed=false human_approval_required=true

DEMO OK
  artifacts: .../tmp/demo
  run/workbench: make dev-web  ->  http://localhost:3000/demo
  PostgreSQL is still running; stop it with: make db-down
```

Artifacts include `report.json`, `report.md`, `incident.json`,
`pre-repair-eval.json`, the accepted attempt's `observations.json`, diff, judge
eval and bundle, the unsafe test log, and the workbench build/navigation logs.
The sanitized frontend projection is `apps/web/public/demo/local.json`
(gitignored); the committed fallback is
`apps/web/public/demo/recorded.json`.

For a fresh run, the exporter reads only the accepted attempt directory
(`attempts/attempt-N/`). Missing, stale, or contradictory attempt evidence makes
the export fail; it never falls back to root observations or attempt-1 files.
The committed M4 recorded artifact predates that layout and is explicitly marked
`legacy_recorded_root` in the export. A failed demo removes the local export, so
the page cannot show a previous successful run as the latest one.

For the interactive browser check, set `CHROME_PATH` when running `make demo`:

```bash
CHROME_PATH="/path/to/Google Chrome for Testing" make demo
```

Without a browser path the HTTP navigation probe still runs, and the browser
step is reported as skipped rather than silently passing.

## Browse the workbench

```bash
make dev-web          # http://localhost:3000
```

Open `/demo` for the current local repair run (or the committed recorded run on
a public deployment). The page shows the incident, observations, recorded
diagnosis, generated diff, verification steps, oracle invariants, the historical
I13 counterexample, the fresh trusted-gate result, and the human-review boundary.

Open `/incidents` for the M5 benchmark console, including **H1-identity-rewrite**
(recorded hosted repair) and **H7-webhook-duplicate-exposed** (unsafe reference).
Review recording is disabled by default; no button can approve anything.

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
