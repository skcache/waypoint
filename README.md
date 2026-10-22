# Waypoint

**Waypoint V2 — Verified Payment Repair.** An AI payment-integration repair engineer
that investigates failed payment workflows, generates scoped code fixes, and
independently verifies that the repairs preserve correct financial state.

Built for the [PayPal AI Hackathon 2026](https://paypalaihackathon.devpost.com/).

> **Status: early V2 prototype (milestones M1–M4 implemented).** The repository contains:
> a runnable **merchant checkout fixture** (Go + PostgreSQL) that performs real PayPal
> **Sandbox** orders with a durable operation journal and deliberately injectable
> defects; an independent **deterministic financial-correctness oracle** with a fault
> harness and knowingly-unsafe patch seeds it rejects; an **AI investigation and repair
> pipeline** that diagnoses an incident, generates a scoped source patch, applies it in
> a disposable worktree, and accepts it only after the candidate's own code passes
> independent verification; the Go API with PostgreSQL migrations; and the previous
> static three-route web preview, which will be replaced by the V2 incident console in a
> later milestone. **The M3 and M4 evidence directories contain genuine hosted-model
> runs** (raw replies, diffs, oracle verdicts, token counts), alongside clearly labelled
> scripted boundary tests. The M4 matrix is in `evidence/m4-live/README.md`. No model
> credentials are stored in this repository. Nothing here processes real money; all
> provider traffic is sandbox, and M4 webhook events are trusted **fixture** events,
> not PayPal-signed webhooks.

## Current status

| Area | State |
|---|---|
| Merchant checkout fixture (`apps/api/cmd/merchant`) | **Implemented (M1)** — server-owned amounts, durable operation identity committed before outbound calls, real PayPal Sandbox order creation, buyer-approval redirect flow, server-side capture with provider-evidence gating, redacted incident artifacts, `MERCHANT_FAULT=F1` reproducible provider-request failure; binds to `127.0.0.1` only |
| Financial oracle (`apps/api/internal/oracle`) | **Implemented (M2/M4)** — deterministic I1–I14 invariant engine over the durable journal and trusted provider observations; verdicts `VERIFIED_FOR_TESTED_SCENARIOS` / `UNSAFE` / `INCONCLUSIVE`; I13/I14 judge fulfillment cardinality and authenticated event backing; NOT_APPLICABLE for unexercised invariants; redacted proof bundles |
| Fault harness + unsafe seeds (`apps/api/internal/replay`) | **Implemented (M2)** — scripted deterministic provider (tier F), scenarios for lost response, duplicate delivery, concurrency, restart, stale reads; two knowingly-unsafe seed patches (fresh-identity reissue; wrong-amount acceptance) rejected by the oracle with counterexamples |
| Go API (`apps/api`) | `GET /healthz`, `GET /readyz`, and the legacy V1 `POST /api/v1/intents/draft` endpoint (kept running; the V1 product direction is retired) |
| Investigation + repair pipeline (`apps/api/internal/repair`, `cmd/repair`) | **Implemented (M3)** — evidence-bounded structured diagnosis, schema-validated, allowlisted patches applied in a disposable git worktree, candidate execution in a **disposable Docker sandbox** (no host home/env, internal network only, non-root, read-only root, CPU/memory/PID/time limits, fail-closed), bounded retries on failure evidence, review report that keeps diagnosis/patch/build/tests/oracle/acceptance as separate facts; never applies to the working tree, pushes or deploys. Live hosted-model evaluation runs only when `LLM_*` credentials are configured; otherwise the command reports `NOT_CONFIGURED`. |
| Webhook + fulfillment fixture (`apps/api/internal/merchant/webhook*.go`) | **Implemented (M4)** — durable `merchant_webhook_events` receipts (event id unique, delivery counter), `merchant_fulfillment_effects` with a stable per-order effect key, transactional apply, fixture HMAC authenticity boundary (tier F; signed PayPal webhooks unverified) |
| Verifier (`apps/api/cmd/fixturecheck` + `apps/api/cmd/judgecheck`) | **Implemented (M3)** — the fixture run (candidate-linked) records only provider observations; a separate **trusted judge** (imports the oracle only, no merchant/replay code) re-reads the durable journal, evaluates the evidence, and writes the verdict plus proof bundle. Acceptance requires a provider-observed capture identity and an oracle `VERIFIED_FOR_TESTED_SCENARIOS`; candidate code cannot run inside, forge, or terminate the judge |
| PostgreSQL | **Implemented** — versioned migrations `0001`–`0007` (fixture tables in `0004`, capture-ownership lease in `0005`, ownership-generation fence in `0006`, webhook receipts and fulfillment effects in `0007`); `/readyz` reports database readiness |
| PayPal Sandbox (Orders v2) | **Verified** — real sandbox order creation, genuine buyer approval, provider-confirmed capture (`COMPLETED`), and a provider-rejected malformed request observed end-to-end; the developer-only feasibility spike CLI is retained under `cmd/paypal-spike` |
| Web app (`apps/web`) | Static V1 preview pages; will be rebuilt around real incident data |
| Full evaluation + workbench UI | Planned — milestone M5 (held-out statistical benchmark, incident console) |

## Merchant fixture (the M1 vertical slice)

The fixture is a small storefront: catalog → checkout → real PayPal Sandbox order →
buyer approval in the browser → **server-side** capture. Its financial-safety design:

- Amounts come from the server-owned catalog only; the browser never supplies a price.
- Every provider operation (create, capture) stores a stable request id + payload
  fingerprint + `IN_FLIGHT` status **before** the outbound call, so a crash can never
  leave a silently-retryable payment (the old developer spike's crash window is not
  reused).
- A timeout or unreadable response is `PAYMENT_OUTCOME_UNKNOWN` — never a failure — and
  retries reuse the original request id. At most one alive capture operation per order
  is enforced by a partial unique database index.
- `CAPTURED_VERIFIED` requires provider evidence: matching order id, a `COMPLETED`
  capture id, and the exact amount and currency.
- An append-only event journal with evidence tiers (`R` real provider observation,
  `F` fault injection, `S` sample) records what actually happened.

### Run it

```bash
# 1. Local prerequisites: Go 1.27+, Docker (for PostgreSQL), make
cp .env.example .env        # fill PAYPAL_ENV=sandbox + sandbox REST credentials

make db-up                  # PostgreSQL on 127.0.0.1:5433
make db-migrate             # schema (includes the fixture tables)

# 2. Valid checkout path (fault off)
make dev-merchant           # http://localhost:8090

# 3. Reproduce the seeded provider-request failure (fault F1)
MERCHANT_FAULT=F1 make dev-merchant
#    Check out any product: the fixture formats the amount with six decimal places
#    ("12.340000"), and the real PayPal Sandbox rejects the request
#    (observed: HTTP 422 UNPROCESSABLE_ENTITY, issue DECIMAL_PRECISION). The order
#    is FAILED_DEFINITE, the operation is REJECTED, and a redacted incident artifact
#    lands in docs/incidents/ (git-ignored).

make merchant-reset         # clear fixture orders/operations/events (keeps the catalog)
```

Buyer approval is **manual by design**: the order page links to the PayPal-hosted
sandbox approval page; capture is a separate, explicit server-side step, and it is
refused until the provider actually reports `APPROVED`.

### Observed provider evidence (2026-10-17, sandbox)

- Valid path: real sandbox order created, buyer approval observed as `APPROVED`
  (payer confirmed), then a provider-confirmed capture
  (`COMPLETED`, exact amount/currency, capture id recorded locally). Evidence is
  recorded locally and never fabricated.
- Fault path (F1): real provider rejection `422 UNPROCESSABLE_ENTITY` /
  `DECIMAL_PRECISION` for the malformed six-decimal amount.

## Financial oracle (the M2 vertical slice)

`apps/api/internal/oracle` is an independently authored, deterministic invariant
engine. It judges **only** durable evidence — the PostgreSQL journal read with its
own SQL, plus provider observations recorded by the harness (or a real provider) —
and returns `VERIFIED_FOR_TESTED_SCENARIOS`, `UNSAFE`, or `INCONCLUSIVE` per run,
with every I1–I14 invariant listed (unexercised ones are `NOT_APPLICABLE`, never
fabricated passes). The AI component is never the authority on correctness, and a
scope-guard test asserts the oracle's dependency graph stays outside candidate-patch
scope.

I13 and I14 extend the same engine to business effects: at most one fulfillment
per legitimate payment, and every fulfillment must be backed by an authenticated
event naming the capture the merchant independently verified. They read the
`merchant_fulfillment_effects` and `merchant_webhook_events` rows directly.

The M2 fault harness (`apps/api/internal/replay`) runs deterministic scenarios
against a scripted provider (tier F, local injection): lost capture response,
duplicate delivery with idempotent request ids, concurrent deliveries, process
restart, live-owner lease refusal, and stale reads. Two **knowingly unsafe seed
patches** ship with it — one reissues a capture under a fresh identity after an
ambiguous outcome, one accepts a wrong-amount provider echo — and the oracle
rejects both with concrete counterexamples.

```bash
make db-up                                # PostgreSQL
cd apps/api
go run ./cmd/oracle -out /tmp/bundles -revision "$(git rev-parse --short HEAD)"
# => 8 scenarios | verified=5 unsafe=2 inconclusive=1 | unexpected=0
# redacted proof bundles land in /tmp/bundles (see
# internal/oracle/testdata/sample-bundles/ for committed samples)
```

## Repair pipeline (the M3 vertical slice)

`cmd/repair` turns one fixture incident into a reviewable repair report:

```
fixture incident (tier F, reproducible) ─► bounded evidence bundle
        ─► hosted-model diagnosis (schema-validated, citations checked)
        ─► hosted-model patch (allowlisted files only)
        ─► disposable worktree ─► build ─► trusted tests
        ─► candidate fixture run ─► independent oracle verdict
        ─► report for a human (never applied, pushed or deployed)
```

Safety properties that are enforced in code, not prose:

- The writable surface is per fault family: `service.go` plus
  `ambiguity.go` for F2, `webhook.go` for F3, plus new lowercase `.go` files in
  that directory. The fixture-only wiring (`fixture_wiring.go`), tests, the
  store, the fault catalogue, migrations, the harness, the oracle and `cmd/*`
  are rejected by path, before anything is written.
- Patches are exact-match textual edits; an `old_text` that does not occur
  exactly once is refused. Symlinks, absolute paths, traversal, deletions and
  renames are refused. A post-apply `git status` check must show exactly the
  files the applier wrote.
- Candidate commands run inside a **disposable Docker sandbox**: no host home
  directory or host environment, an internal network with no outbound route,
  a non-root uid, read-only root filesystem, and memory/CPU/PID/wall-clock
  limits. The candidate source tree and the preloaded **module cache** are
  mounted **read-only**, so generated code can be compiled and tested but
  cannot rewrite trusted tests, the oracle, the replay harness, the verifier,
  or a dependency used to build the judge. The trusted judge uses a separate
  pristine module cache and its own build cache; the candidate's build cache
  is private to candidate steps. If Docker is unavailable the pipeline fails
  closed — it never executes candidate code on the host.
- Persisted artifacts are **credential-redacted**: PostgreSQL URLs are parsed
  and their passwords removed, DSN flag arguments are replaced wholesale,
  Authorization headers are stripped, and configured secret values are
  scrubbed. The verifier still receives the real DSN through the sandbox
  environment; only report JSON, Markdown, step logs and model feedback are
  sanitized.
- The proof report records the **resolved candidate base commit** and a
  **sha256 digest of the applied diff**; the trusted judge refuses to write a
  bundle whose source revision is unknown.
- Verification runs the **candidate's own compiled code** in the worktree. The
  oracle is built from the trusted revision and reads PostgreSQL with its own
  SQL; a candidate cannot make the judge agree with it.
- Acceptance is separate from every earlier rung: diagnosis produced, patch
  generated, patch applied, build passed, tests passed, oracle verdict,
  accepted for human review. A clean build is never reported as a fix.

Run it (requires `LLM_API_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`, and the local
PostgreSQL test database; `LLM_REASONING_EFFORT=low` is optional for
reasoning-heavy providers):

```bash
cd apps/api
go run ./cmd/repair -fault F2 -out ../../tmp/repair-f2 -attempts 2
go run ./cmd/repair -fault F3 -out ../../tmp/repair-f3 -attempts 2
go run ./cmd/repair -fault F4 -out ../../tmp/repair-f4 -attempts 2
go run ./cmd/repair -fault F4 -out ../../tmp/repair-f4-baseline -mode baseline  # B0 comparison
```

Without model configuration the command exits 3 with status `NOT_CONFIGURED`
and writes an honest report; it never fabricates a model run.

**F1: reproduction vs. correctness.** A genuine F1 repair removes the malformed
payload, so the pre-repair assertion cannot be part of the candidate gate.
The two phases are separate build-tagged suites:

- `-tags fault_reproduction` — `TestF1DefectReproducesOnThePinnedRevision`
  (`internal/merchant/f1_reproduction_test.go`) proves the pinned base really
  malforms the request, gets rejected, and emits the incident artifact. Run it
  against the base revision, never as a pass criterion for a repair.
- `-tags f1_repair` — `TestCheckoutF1FaultMustNotLeakPresentationFormatting`
  (`internal/merchant/f1_repair_test.go`) asserts correct provider-facing
  behavior and is the F1 candidate gate. The verifier adds the tag only for F1
  candidates, so F4 repairs are not forced to fix an unrelated defect.

The oracle is unchanged in both phases; a repair is accepted only when the
candidate's own compiled fixture passes the gate and the oracle verdict.

## Adversarial families (the M4 vertical slice)

M4 adds two genuinely different failure families on the same durable-identity
foundation:

- **F2 — capture applied, response lost.** The scripted provider applies the
  capture and drops the response. The fixture wires the historical ambiguity
  policy, which treats an unreadable 2xx response as a definite rejection. The
  journey then restarts the process and retries: the unsafe journal closes the
  first attempt as `REJECTED` and later verifies without a successful capture
  operation (I5/I6). The repaired policy keeps the attempt `UNKNOWN` and
  recovers it with the original request id and one capture effect.
- **F3 — duplicate/reordered webhooks.** Delivery is at-least-once and
  unordered: the same event id twice, a second event id for the same capture, a
  late approval, eight concurrent handlers, a restart replay, an early event,
  and an unauthenticated delivery. The legacy handler keys fulfillment by event
  id and writes event status onto the order (two effects, status rewound); the
  repaired handler verifies the sealed capture, uses one effect key per order,
  and never mutates payment state from an event. I13/I14 judge the effects.

The **unsafe trap** is seeded, not model-generated: `evidence/m4-unsafe-f3/`
contains a candidate that compiles, returns success and ends
`CAPTURED_VERIFIED`, but creates two fulfillment effects; the trusted financial
test rejects it first and the independent judge rejects it with an I13
counterexample. The M4 live matrix, raw model replies and failed attempts are in
`evidence/m4-live/README.md`.

## Local development

```bash
make dev-api                # Go API on http://localhost:8080
make dev-web                # Next.js on http://localhost:3000 (V1 static preview)
curl -s http://localhost:8080/healthz   # => {"status":"ok"}
curl -s http://localhost:8080/readyz    # 200 when the database + schema are ready
```

## Tests and checks

```bash
make test               # go test ./... (database-backed suites skip cleanly when
                        # PostgreSQL is down; TEST_DATABASE_URL overrides)
make test-integration   # database integration REQUIRED (fails instead of skipping)
make lint               # go vet + gofmt check, ESLint
make build-web          # production build of the web app
```

The database-backed suites (`internal/store/postgres`, `internal/merchant`,
`internal/oracle`) run against real PostgreSQL in **separate databases** (suffix
`_test` enforced; created on demand). There are no in-memory substitutes.

## PayPal sandbox feasibility spike (developer-only)

`apps/api/cmd/paypal-spike` is an isolated feasibility tool for the PayPal REST Orders
v2 integration. It is **not** a product surface: sandbox-only (any `PAYPAL_ENV` other
than `sandbox` is refused), no HTTP endpoints, and a fixed synthetic $1.00 order —
amounts are never accepted from arguments or browsers. Its provider-evidence logic is
reused by the merchant fixture; its file-backed capture orchestration deliberately is
not (see the fixture's durable journal).

## Security and privacy

- Sandbox-only: this prototype targets the PayPal **sandbox**; production hosts are
  refused. No real money, no real customer data.
- Secrets stay out of the repository: `.env` files are ignored, `.env.example` holds
  placeholders only; incident artifacts and local payment records live under the
  git-ignored `docs/` tree.
- No tokens, provider payloads beyond sanitized diagnostic fields, or personal data are
  logged, committed, or exposed to the browser.

## Repository layout

```text
apps/api       Go services: API (cmd/server), merchant fixture (cmd/merchant),
               PayPal sandbox developer CLI (cmd/paypal-spike), internal packages
apps/web       Next.js web application (static preview; V2 UI planned)
compose.yaml   Local PostgreSQL development service
```

Local-only planning documents, progress notes, and environment files stay out of
version control on purpose.

## License

MIT — see [LICENSE](LICENSE).
