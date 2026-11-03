#!/usr/bin/env bash
# Deterministic Waypoint demo: recorded model replay + tier-F fixture evidence.
# No hosted model or PayPal call is made. PostgreSQL is started locally.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [ ! -f .env ]; then
  echo "demo: .env is missing. Run: cp .env.example .env" >&2
  exit 1
fi
set -a
# shellcheck disable=SC1091
. ./.env
set +a

command -v docker >/dev/null || { echo "demo: docker is required" >&2; exit 1; }
command -v go >/dev/null || { echo "demo: go is required" >&2; exit 1; }
command -v npm >/dev/null || { echo "demo: node/npm are required" >&2; exit 1; }
NODE_MAJOR="$(node -p 'process.versions.node.split(".")[0]')"
if [ "$NODE_MAJOR" -lt 22 ]; then
  echo "demo: Node 22+ is required (the project pins .nvmrc=22); run 'nvm use 22'" >&2
  exit 1
fi

echo "==> starting PostgreSQL"
docker compose up -d db >/dev/null
for _ in $(seq 1 30); do
  if docker compose exec -T db pg_isready -U waypoint -d waypoint >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker compose exec -T db pg_isready -U waypoint -d waypoint >/dev/null

echo "==> applying migrations"
make --no-print-directory db-migrate >/dev/null

OUT="${OUT:-$ROOT/tmp/demo}"
if [[ "$OUT" != /* ]]; then
  OUT="$ROOT/$OUT"
fi
if [[ "$OUT" == "$ROOT"/tmp/* ]]; then
  rm -rf "$OUT"
fi
mkdir -p "$OUT"

echo "==> replaying the recorded F3 repair (no model call)"
cd "$ROOT/apps/api"
go run ./cmd/repair -fault F3 -replay evidence/m4-live/f3 -out "$OUT" -attempts 1

echo
echo "==> verifying the demo facts"
go run ./cmd/demo -repo "$ROOT" -out "$OUT"

echo
echo "==> executing the seeded unsafe candidate through the pipeline"
if ! REQUIRE_DATABASE_TESTS=1 go test -count=1 -run '^TestPipelineRejectsSeededUnsafeF3Candidate$' -v ./internal/repair >"$OUT/unsafe-test.log" 2>&1; then
  tail -80 "$OUT/unsafe-test.log" >&2
  echo "demo: seeded unsafe candidate was not rejected" >&2
  exit 1
fi
echo "seeded unsafe candidate: REJECTED by the trusted gate (2 fulfillment effects)"

echo
echo "==> checking the review boundary and building the workbench"
cd "$ROOT/apps/web"
npm test >"$OUT/web-tests.log" 2>&1
# A .next tree produced by a different Node architecture can poison a later
# build; removing it is cheap and makes the demo deterministic across machines.
rm -rf .next
npm run build >"$OUT/web-build.log" 2>&1
echo "workbench build: ok; review guard tests: ok"

echo "==> probing workbench navigation"
PORT=3199
"$ROOT/apps/web/node_modules/.bin/next" start -p "$PORT" >"$OUT/web-server.log" 2>&1 &
WEB_PID=$!
cleanup() {
  kill "$WEB_PID" >/dev/null 2>&1 || true
  wait "$WEB_PID" >/dev/null 2>&1 || true
}
trap cleanup EXIT

ready=0
for _ in $(seq 1 40); do
  if curl -fsS "http://127.0.0.1:$PORT/" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.5
done
if [ "$ready" -ne 1 ]; then
  echo "demo: workbench did not start" >&2
  tail -40 "$OUT/web-server.log" >&2
  exit 1
fi

check_page() {
  local path="$1" needle="$2"
  local body="$OUT/page-check.html"
  local code
  code="$(curl -sS -o "$body" -w '%{http_code}' "http://127.0.0.1:$PORT$path")"
  if [ "$code" != "200" ]; then
    echo "demo: $path returned HTTP $code" >&2
    exit 1
  fi
  if ! grep -qi "$needle" "$body"; then
    echo "demo: $path did not contain '$needle'" >&2
    exit 1
  fi
  echo "  $path 200"
}

check_page "/" "Incidents"
check_page "/incidents" "Held-out payment-integration defects"
check_page "/incidents/H1-identity-rewrite" "A retry rewrites the durable capture request id"
check_page "/benchmark" "Benchmark"

trap - EXIT
cleanup

echo "==> scanning demo artifacts for private data"
if grep -RIlE '/Users/|/private/var|/var/folders|sk-or-v1|OPENROUTER_API_KEY|postgres(ql)?://[^ ]+:[^@ ]+@' "$OUT" >/tmp/waypoint-demo-private.txt 2>/dev/null; then
  echo "demo: private data found in:" >&2
  cat /tmp/waypoint-demo-private.txt >&2
  exit 1
fi

echo
echo "DEMO OK"
echo "  artifacts: $OUT"
echo "  workbench: make dev-web  ->  http://localhost:3000/incidents"
echo "  PostgreSQL is still running; stop it with: make db-down"
