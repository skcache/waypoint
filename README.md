# Waypoint

**Waypoint V2 — Verified Payment Repair.** An AI payment-integration repair engineer
that investigates failed payment workflows, generates scoped code fixes, and
independently verifies that the repairs preserve correct financial state.

Built for the [PayPal AI Hackathon 2026](https://paypalaihackathon.devpost.com/).

> **Status: early V2 prototype (milestones M1–M3 implemented).** The repository contains:
> a runnable **merchant checkout fixture** (Go + PostgreSQL) that performs real PayPal
> **Sandbox** orders with a durable operation journal and deliberately injectable
> defects; an independent **deterministic financial-correctness oracle** with a fault
> harness and knowingly-unsafe patch seeds it rejects; an **AI investigation and repair
> pipeline** that diagnoses an incident, generates a scoped source patch, applies it in
> a disposable worktree, and accepts it only after the candidate's own code passes
> independent verification; the Go API with PostgreSQL migrations; and the previous
> static three-route web preview, which will be replaced by the V2 incident console in a
> later milestone. **The live hosted-model evaluation is NOT RUN in this repository's
> checked-in evidence: no model credentials are configured here.** The pipeline's
> integration boundaries are exercised with a clearly labelled scripted double, never
> presented as a hosted-AI result. Nothing here processes real money; all provider
> traffic is sandbox.

## Current status

| Area | State |
|---|---|
| Merchant checkout fixture (`apps/api/cmd/merchant`) | **Implemented (M1)** — server-owned amounts, durable operation identity committed before outbound calls, real PayPal Sandbox order creation, buyer-approval redirect flow, server-side capture with provider-evidence gating, redacted incident artifacts, `MERCHANT_FAULT=F1` reproducible provider-request failure; binds to `127.0.0.1` only |
| Financial oracle (`apps/api/internal/oracle`) | **Implemented (M2)** — deterministic I1–I12 invariant engine over the durable journal and trusted provider observations; verdicts `VERIFIED_FOR_TESTED_SCENARIOS` / `UNSAFE` / `INCONCLUSIVE`; NOT_APPLICABLE for unexercised invariants; redacted proof bundles |
| Fault harness + unsafe seeds (`apps/api/internal/replay`) | **Implemented (M2)** — scripted deterministic provider (tier F), scenarios for lost response, duplicate delivery, concurrency, restart, stale reads; two knowingly-unsafe seed patches (fresh-identity reissue; wrong-amount acceptance) rejected by the oracle with counterexamples |
| Go API (`apps/api`) | `GET /healthz`, `GET /readyz`, and the legacy V1 `POST /api/v1/intents/draft` endpoint (kept running; the V1 product direction is retired) |
| Investigation + repair pipeline (`apps/api/internal/repair`, `cmd/repair`) | **Implemented (M3)** — evidence-bounded structured diagnosis, schema-validated, allowlisted patches applied in a disposable git worktree, candidate execution in a **disposable Docker sandbox** (no host home/env, internal network only, non-root, read-only root, CPU/memory/PID/time limits, fail-closed), bounded retries on failure evidence, review report that keeps diagnosis/patch/build/tests/oracle/acceptance as separate facts; never applies to the working tree, pushes or deploys. Live hosted-model evaluation runs only when `LLM_*` credentials are configured; otherwise the command reports `NOT_CONFIGURED`. |
| Fixture verifier (`apps/api/cmd/repaircheck`) | **Implemented (M3)** — runs the candidate's compiled merchant package through checkout → approval → capture and returns the oracle's verdict plus a proof bundle; acceptance requires a provider-observed capture identity and an oracle `VERIFIED_FOR_TESTED_SCENARIOS` |
| PostgreSQL | **Implemented** — versioned migrations `0001`–`0006` (fixture tables in `0004`, capture-ownership lease in `0005`, ownership-generation fence in `0006`); `/readyz` reports database readiness |
| PayPal Sandbox (Orders v2) | **Verified** — real sandbox order creation, genuine buyer approval, provider-confirmed capture (`COMPLETED`), and a provider-rejected malformed request observed end-to-end; the developer-only feasibility spike CLI is retained under `cmd/paypal-spike` |
| Web app (`apps/web`) | Static V1 preview pages; will be rebuilt around real incident data |
| Full evaluation + workbench UI | Planned — milestone M4+ (multi-family fault/repair benchmark, incident console) |

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
with every I1–I12 invariant listed (unexercised ones are `NOT_APPLICABLE`, never
fabricated passes). The AI component is never the authority on correctness, and a
scope-guard test asserts the oracle's dependency graph stays outside candidate-patch
scope.

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

- The only writable surface is `apps/api/internal/merchant/service.go` plus new
  lowercase `.go` files in that directory. Tests, the store, the fault
  catalogue, migrations, the harness, the oracle and `cmd/*` are rejected by
  path, before anything is written.
- Patches are exact-match textual edits; an `old_text` that does not occur
  exactly once is refused. Symlinks, absolute paths, traversal, deletions and
  renames are refused. A post-apply `git status` check must show exactly the
  files the applier wrote.
- Candidate commands run inside a **disposable Docker sandbox**: no host home
  directory or host environment, an internal network with no outbound route,
  a non-root uid, read-only root filesystem, and memory/CPU/PID/wall-clock
  limits. Only the worktree, an artifact directory and a disposable module
  cache are mounted. If Docker is unavailable the pipeline fails closed — it
  never executes candidate code on the host.
- Verification runs the **candidate's own compiled code** in the worktree. The
  oracle is built from the trusted revision and reads PostgreSQL with its own
  SQL; a candidate cannot make the judge agree with it.
- Acceptance is separate from every earlier rung: diagnosis produced, patch
  generated, patch applied, build passed, tests passed, oracle verdict,
  accepted for human review. A clean build is never reported as a fix.

Run it (requires `LLM_API_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`, and the local
PostgreSQL test database):

```bash
cd apps/api
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
