# Waypoint

**An intent-aware execution boundary for AI-initiated financial transactions.**

Waypoint sits between an AI agent's purchase proposal and a payment provider.
It checks the proposal against a human-confirmed purchase mandate and
deterministic policy, executes approved payments through the PayPal sandbox,
and records a correlated, auditable trace of every decision and outcome.

Built for the [PayPal AI Hackathon 2026](https://paypalaihackathon.devpost.com/).

> **Status: Phase 01 scaffold.** The repository currently contains a Go API
> with a liveness endpoint, a static Next.js page, and local development
> tooling. There is no PayPal integration, no AI integration, and no database
> schema yet. Nothing here processes real money.

## Current status

| Area | State |
|---|---|
| Go API (`apps/api`) | Scaffolded — `GET /healthz` implemented with tests |
| Web app (`apps/web`) | Scaffolded — static status page (Next.js + TypeScript + Tailwind) |
| PostgreSQL | Provided via `compose.yaml`; not yet wired to the application |
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
- Node.js 20.9+ and npm — `node -v && npm -v` (Next.js 16 requires ≥ 20.9)
- Docker with Compose — `docker compose version` (only for the local database)
- `make` — optional; every target is a short command you can also run directly

## Local development

```bash
# API — http://localhost:8080
make dev-api
curl -s http://localhost:8080/healthz
# => {"status":"ok"}

# Web — http://localhost:3000
make dev-web

# Local PostgreSQL (optional for the current scaffold)
make db-up     # postgres:17.7 on localhost:5432, example credentials in compose.yaml
make db-down
```

Configuration is optional at this stage: the API starts correctly on an empty
environment. To override defaults (`API_PORT`, `APP_ENV`, ...), copy
`.env.example` to `.env`.

## Tests and checks

```bash
make test        # go test ./...
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
