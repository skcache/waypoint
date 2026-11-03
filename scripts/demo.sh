#!/usr/bin/env bash
# Deterministic Waypoint demo: recorded model replay + tier-F fixture evidence.
# No hosted model or PayPal call is made. PostgreSQL is started locally.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/demo-paths.sh
. "$ROOT/scripts/lib/demo-paths.sh"
cd "$ROOT"

LOCAL_EXPORT="$ROOT/apps/web/public/demo/local.json"
WEB_PID=""
SUCCESS=0
# A prior successful run must not survive a failed new run.
rm -f "$LOCAL_EXPORT"
cleanup() {
  if [ -n "${WEB_PID:-}" ]; then
    kill "$WEB_PID" >/dev/null 2>&1 || true
    wait "$WEB_PID" >/dev/null 2>&1 || true
    WEB_PID=""
  fi
  if [ "${SUCCESS:-0}" -ne 1 ]; then
    rm -f "$LOCAL_EXPORT"
  fi
}
trap cleanup EXIT

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

# Fixed generated-artifact directory; never a caller-supplied cleanup target.
OUT="$(prepare_demo_dir "$ROOT")" || {
  echo "demo: refusing unsafe demo artifact directory (symlink or unexpected target)" >&2
  exit 1
}

echo "==> applying migrations"
MIGRATE_LOG="$ROOT/tmp/demo-migrate.log"
migrated=0
for _ in $(seq 1 8); do
  if make --no-print-directory db-migrate >"$MIGRATE_LOG" 2>&1; then
    migrated=1
    break
  fi
  sleep 1
done
if [ "$migrated" -ne 1 ]; then
  echo "demo: migrations did not complete" >&2
  cat "$MIGRATE_LOG" >&2
  exit 1
fi

echo "==> replaying the recorded F3 repair (no model call)"
cd "$ROOT/apps/api"
go run ./cmd/repair -fault F3 -replay evidence/m4-live/f3 -out "$OUT" -attempts 1

echo
echo "==> executing the seeded unsafe candidate through the pipeline"
if ! REQUIRE_DATABASE_TESTS=1 go test -count=1 -run '^TestPipelineRejectsSeededUnsafeF3Candidate$' -v ./internal/repair >"$OUT/unsafe-test.log" 2>&1; then
  tail -80 "$OUT/unsafe-test.log" >&2
  echo "demo: seeded unsafe candidate was not rejected" >&2
  exit 1
fi
echo "seeded unsafe candidate: rejected by the trusted gate (2 fulfillment effects)"

echo
echo "==> verifying the demo facts"
go run ./cmd/demo -repo "$ROOT" -out "$OUT" -unsafe-test-log "$OUT/unsafe-test.log"

echo
echo "==> exporting the sanitized repair run for the workbench"
go run ./cmd/demoexport \
  -from "$OUT" \
  -out "$ROOT/apps/web/public/demo/local.json" \
  -mode replay \
  -source tmp/demo \
  -source-label "Local make demo replay" \
  -source-report "$ROOT/evidence/m4-live/f3/report.json" \
  -unsafe "$ROOT/evidence/m4-unsafe-f3/eval.json" \
  -unsafe-source evidence/m4-unsafe-f3/eval.json \
  -unsafe-test-log "$OUT/unsafe-test.log"

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
# Pick a fresh port and verify the process we launched is the one serving it.
PORT="$(node -e 'const s=require("net").createServer(); s.listen(0,"127.0.0.1",()=>{console.log(s.address().port);s.close()})')"
"$ROOT/apps/web/node_modules/.bin/next" start -p "$PORT" >"$OUT/web-server.log" 2>&1 &
WEB_PID=$!

ready=0
for _ in $(seq 1 40); do
  if ! kill -0 "$WEB_PID" >/dev/null 2>&1; then
    echo "demo: the launched workbench exited before serving" >&2
    tail -40 "$OUT/web-server.log" >&2
    exit 1
  fi
  if grep -q "Ready" "$OUT/web-server.log" && curl -fsS "http://127.0.0.1:$PORT/" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.5
done
if [ "$ready" -ne 1 ]; then
  echo "demo: workbench did not start on the freshly selected port" >&2
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
check_page "/demo" "Loading repair evidence"

if [ -n "${CHROME_PATH:-}" ]; then
  echo "==> browser smoke: /demo"
  DEMO_URL="http://127.0.0.1:$PORT" \
    DEMO_SCREENSHOT="${DEMO_SCREENSHOT:-}" \
    node "$ROOT/apps/web/src/lib/demo-browser.test.mjs"
else
  echo "==> browser smoke skipped: set CHROME_PATH to Chrome/Chromium for the interactive check"
fi

# Stop the server before scanning; keep the EXIT trap armed so a failed scan
# removes the local export rather than presenting a half-verified run.
if [ -n "${WEB_PID:-}" ]; then
  kill "$WEB_PID" >/dev/null 2>&1 || true
  wait "$WEB_PID" >/dev/null 2>&1 || true
  WEB_PID=""
fi

echo "==> scanning demo artifacts for private data"
if grep -RIlE '/Users/|/private/var|/var/folders|sk-or-v1|OPENROUTER_API_KEY|postgres(ql)?://[^ ]+:[^@ ]+@' \
  "$OUT" "$ROOT/apps/web/public/demo/local.json" "$ROOT/apps/web/public/demo/recorded.json" \
  >/tmp/waypoint-demo-private.txt 2>/dev/null; then
  echo "demo: private data found in:" >&2
  cat /tmp/waypoint-demo-private.txt >&2
  exit 1
fi

SUCCESS=1
trap - EXIT

echo
echo "DEMO OK"
echo "  artifacts: $OUT"
echo "  run/workbench: make dev-web  ->  http://localhost:3000/demo"
echo "  PostgreSQL is still running; stop it with: make db-down"
