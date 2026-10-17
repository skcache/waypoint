# Waypoint

**An intent-aware execution boundary for AI-initiated financial transactions.**

Waypoint sits between an AI agent's purchase proposal and a payment provider.
It checks the proposal against a human-confirmed purchase mandate and
deterministic policy, executes approved payments through the PayPal sandbox,
and records a correlated, auditable trace of every decision and outcome.

Built for the [PayPal AI Hackathon 2026](https://paypalaihackathon.devpost.com/).

> **Status: early prototype.** The repository currently contains a Go API
> with liveness, database-readiness and intent-draft endpoints, the
> PostgreSQL schema with versioned SQL migrations, a static workbench
> preview (Next.js), a sandbox-only PayPal feasibility spike verified
> end-to-end, and local development tooling. The intent-draft compiler is
> implemented and honestly reports `NOT_CONFIGURED` until model credentials
> are provided; the semantic verifier and checkout wiring are not built yet.
> Nothing here processes real money: the spike talks only to the PayPal
> sandbox.

## Current status

| Area | State |
|---|---|
| Go API (`apps/api`) | **Implemented** — `GET /healthz`, `GET /readyz`, and `POST /api/v1/intents/draft` (strict-schema draft compiler; answers `NOT_CONFIGURED` without model credentials) |
| Web app (`apps/web`) | Static workbench / checkout / trace preview (Next.js + TypeScript + Tailwind); not wired to the API yet |
| PostgreSQL | **Implemented** — schema + versioned SQL migrations; `/readyz` reports database readiness |
| PayPal sandbox (Orders v2) | **Feasibility spike verified end-to-end** — sandbox-only developer CLI; real OAuth, order creation, manual buyer approval, and a provider-confirmed `COMPLETED` capture of a USD 1.00 sandbox order |
| Runtime AI (intent compiler, semantic verifier) | Intent compiler **implemented** (`POST /api/v1/intents/draft`, non-authoritative drafts, strict JSON validation); semantic verifier planned — not implemented |
| Transaction state machine, trace explorer, evaluation | Planned — not implemented |

## Planned capabilities

Each of these is a locked product requirement. None of them is implemented yet,
and this README will be rewritten from observed functionality as phases land.

- **Human-confirmed purchase mandates** — a natural-language request becomes a
  typed contract the human explicitly confirms, with enforced limits (currency,
  ceiling, quantity, expiration).
- **Deterministic policy + semantic verification** — hard rules always win;
  an LLM additionally flags qualitative mismatches (e.g. "quiet office
  keyboard" proposed against a loud RGB gaming keyboard) with cited evidence.
  Ambiguous outcomes fail closed to review — never auto-pay.
- **PayPal sandbox checkout** — one-time Orders v2 create, buyer approval, and
  server-side capture, with durable operation identity and no blind retries on
  unknown outcomes.
- **Transaction traces** — a causal timeline (model assessment, policy
  decision, provider calls, outcomes) explaining why a purchase was allowed,
  blocked, or left unresolved.

## Technology stack

| Layer | Choice |
|---|---|
| API | Go — standard library `net/http` and `slog` |
| Web | Next.js (App Router) + TypeScript + Tailwind CSS |
| Database | PostgreSQL via Docker Compose (schema arrives with the database phase) |
| Payments | PayPal REST Orders v2 — sandbox only (planned) |
| Evaluation | Python scripts + JSONL datasets (planned) |

## Repository layout

```text
apps/api       Go API service
apps/web       Next.js web application
compose.yaml   Local PostgreSQL development service
eval/          Evaluation datasets and scripts (planned)
scripts/       Development utilities
```

Local-only planning documents, progress notes, and environment files are kept
out of version control on purpose.

## Prerequisites

- Go 1.27+ — `go version`
- Node.js 22 LTS and npm — `node -v && npm -v` (a `.nvmrc` pins 22; Next.js
  needs ≥ 20.9, but on Apple Silicon use the native arm64 build — an Intel
  Node under Rosetta builds several times slower)
- Docker with Compose — `docker compose version` (only for the local database)
- `make` — optional; every target is a short command you can also run directly

## Local development

```bash
# 1. Environment file (placeholders only — example credentials for local dev)
cp .env.example .env

# 2. Local PostgreSQL on 127.0.0.1:5433 (5432 is commonly occupied locally)
make db-up
make db-migrate    # applies the versioned SQL migrations

# 3. API — http://localhost:8080
make dev-api
curl -s http://localhost:8080/healthz   # => {"status":"ok"}
curl -s http://localhost:8080/readyz    # => {"status":"ok"} when the database and
                                        #    schema are ready; 503 + reason code otherwise

# 4. Web — http://localhost:3000
make dev-web

# Reset the local database (destructive; local development only)
make db-reset && make db-migrate
```

Configuration is optional at this stage: the API starts correctly on an
empty environment and `/healthz` never needs configuration. Copy
`.env.example` to `.env` to wire `DATABASE_URL` (and later PayPal/LLM
settings) — `/readyz` reports `not_configured` until the database is set up.

## Tests and checks

```bash
make test        # go test ./... — integration tests run against a real PostgreSQL
                 # (waypoint_test, created by the compose init script; override with
                 # TEST_DATABASE_URL; they skip cleanly when the database is down)
make test-integration  # database integration REQUIRED: fails (not skips) when
                       # PostgreSQL is unavailable — for CI and pre-merge checks
make lint        # go vet + gofmt check, ESLint
make build-web   # production build of the web app
```

## PayPal sandbox feasibility spike (developer-only)

`apps/api/cmd/paypal-spike` is an isolated feasibility tool for the PayPal
REST Orders v2 integration. It is **not** a product surface: no HTTP
endpoints, sandbox-only (any `PAYPAL_ENV` other than `sandbox` is refused),
and a fixed synthetic $1.00 "Waypoint sandbox feasibility test" order —
amounts are never accepted from arguments or browsers.

```bash
# 1. Sandbox credential from a PayPal developer app (server-side only):
#    PAYPAL_ENV=sandbox, PAYPAL_CLIENT_ID, PAYPAL_CLIENT_SECRET — see .env.example
set -a && source .env && set +a

# 2. Run from apps/api
cd apps/api
go run ./cmd/paypal-spike auth      # verify sandbox OAuth (token never printed)
go run ./cmd/paypal-spike create    # create the fixed $1.00 sandbox order
#   -> prints the buyer approval URL; approve it MANUALLY in a browser with a
#      PayPal sandbox buyer account. Approval is never automated or simulated.
go run ./cmd/paypal-spike status    # refresh provider state
go run ./cmd/paypal-spike capture   # capture only after the provider reports
                                    # APPROVED; verifies the provider response
                                    # before reporting success
```

Safety properties: capture is refused until the provider confirms buyer
approval (nothing is sent on refusal); success is reported only when the
provider response proves a COMPLETED capture of the exact expected amount and
currency; timeouts, 5xx responses, and unreadable responses are reported as
`PAYMENT_OUTCOME_UNKNOWN` and never blindly retried — a retry reuses the same
idempotency request id so the provider returns the original result. Local
records are written under `docs/spike/records/`, which is git-ignored.

**Current status:** implemented and verified end-to-end against the PayPal
sandbox: real OAuth, order creation, manual buyer approval, and a
provider-confirmed `COMPLETED` capture of the fixed $1.00 order. Evidence
(order/capture identifiers, provider debug ids) is kept in local, git-ignored
records — never published.

## Security and privacy

- Sandbox-only: this prototype targets the PayPal **sandbox**. Live payments
  are out of scope and the application is not built to touch real money.
- Secrets stay out of the repository: `.env` files are ignored and
  `.env.example` contains placeholders only.
- No tokens, provider payloads, or personal data are logged or exposed to the
  browser.

## License

MIT — see [LICENSE](LICENSE).
