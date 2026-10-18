# Waypoint

**Waypoint V2 — Verified Payment Repair.** An AI payment-integration repair engineer
that investigates failed payment workflows, generates scoped code fixes, and
independently verifies that the repairs preserve correct financial state.

Built for the [PayPal AI Hackathon 2026](https://paypalaihackathon.devpost.com/).

> **Status: early V2 prototype (milestone M1 in progress).** The repository contains:
> a runnable **merchant checkout fixture** (Go + PostgreSQL) that performs real PayPal
> **Sandbox** orders with a durable operation journal and a deliberately injectable
> provider-request defect; the Go API with PostgreSQL migrations; and the previous static
> three-route web preview, which will be replaced by the V2 incident console in a later
> milestone. The AI investigator, repair agent, and independent financial oracle are
> **not built yet**. Nothing here processes real money; all provider traffic is sandbox.

## Current status

| Area | State |
|---|---|
| Merchant checkout fixture (`apps/api/cmd/merchant`) | **Implemented (M1)** — server-owned amounts, durable operation identity committed before outbound calls, real PayPal Sandbox order creation, buyer-approval redirect flow, server-side capture with provider-evidence gating, redacted incident artifacts, `MERCHANT_FAULT=F1` reproducible provider-request failure |
| Go API (`apps/api`) | `GET /healthz`, `GET /readyz`, and the legacy V1 `POST /api/v1/intents/draft` endpoint (kept running; the V1 product direction is retired) |
| PostgreSQL | **Implemented** — versioned migrations `0001`–`0004` (fixture tables in `0004`); `/readyz` reports database readiness |
| PayPal Sandbox (Orders v2) | **Verified** — real sandbox order creation and a provider-rejected malformed request observed end-to-end (see below); the developer-only feasibility spike CLI is retained under `cmd/paypal-spike` |
| Web app (`apps/web`) | Static V1 preview pages; will be rebuilt around real incident data |
| AI repair loop, financial oracle, evaluation | Planned — not implemented |

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

- Valid path: order created, status `BUYER_APPROVAL_PENDING` (capture requires manual
  sandbox-buyer approval; evidence is recorded locally and never fabricated).
- Fault path (F1): real provider rejection `422 UNPROCESSABLE_ENTITY` /
  `DECIMAL_PRECISION` for the malformed six-decimal amount.

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

The database-backed suites (`internal/store/postgres`, `internal/merchant`) run against
real PostgreSQL in **separate databases** (suffix `_test` enforced; the merchant suite
creates `waypoint_merchant_test` on demand). There are no in-memory substitutes.

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
