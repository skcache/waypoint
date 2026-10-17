# Waypoint

**An intent-aware execution boundary for AI-initiated financial transactions.**

Waypoint sits between an AI agent's purchase proposal and a payment provider.
It checks the proposal against a human-confirmed purchase mandate and
deterministic policy, executes approved payments through the PayPal sandbox,
and records a correlated, auditable trace of every decision and outcome.

Built for the [PayPal AI Hackathon 2026](https://paypalaihackathon.devpost.com/).

> **Status: early prototype.** The repository currently contains a Go API
> with liveness and database-readiness endpoints, the PostgreSQL schema with
> versioned SQL migrations, a static Next.js page, and local development
> tooling. There is no PayPal integration and no AI integration yet. Nothing
> here processes real money.

## Current status

| Area | State |
|---|---|
| Go API (`apps/api`) | Scaffolded — `GET /healthz` implemented with tests |
| Web app (`apps/web`) | Scaffolded — static status page (Next.js + TypeScript + Tailwind) |
| PostgreSQL | **Implemented** — schema + versioned SQL migrations; `/readyz` reports database readiness |
| PayPal sandbox (Orders v2) | Planned — not implemented |
| Runtime AI (intent compiler, semantic verifier) | Planned — not implemented |
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

## Security and privacy

- Sandbox-only: this prototype targets the PayPal **sandbox**. Live payments
  are out of scope and the application is not built to touch real money.
- Secrets stay out of the repository: `.env` files are ignored and
  `.env.example` contains placeholders only.
- No tokens, provider payloads, or personal data are logged or exposed to the
  browser.

## License

MIT — see [LICENSE](LICENSE).
