#!/usr/bin/env bash
# Deterministic safety tests for the demo artifact directory. Uses only temp
# trees; never touches real user data.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/demo-paths.sh
. "$ROOT/scripts/lib/demo-paths.sh"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/waypoint-demo-paths.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# 1. Normal preparation returns the fixed directory and clears only children.
mkdir -p "$WORK/repo/tmp/demo"
echo stale >"$WORK/repo/tmp/demo/stale"
got="$(prepare_demo_dir "$WORK/repo")" || fail "normal preparation failed"
[ "$got" = "$WORK/repo/tmp/demo" ] || fail "unexpected directory: $got"
[ ! -e "$WORK/repo/tmp/demo/stale" ] || fail "stale child was not removed"

# 2. A caller-supplied OUT traversal cannot move the cleanup target.
mkdir -p "$WORK/victim"
echo keep >"$WORK/victim/keep"
OUT="$WORK/repo/tmp/../../victim" prepare_demo_dir "$WORK/repo" >/dev/null \
  || fail "preparation with hostile OUT failed"
[ -f "$WORK/victim/keep" ] || fail "hostile OUT deleted victim data"

# 3. A symlinked demo directory is refused before cleanup.
rm -rf "$WORK/repo/tmp/demo"
ln -s "$WORK/victim" "$WORK/repo/tmp/demo"
if prepare_demo_dir "$WORK/repo" >/dev/null; then
  fail "symlinked demo directory was accepted"
fi
[ -f "$WORK/victim/keep" ] || fail "symlink redirection deleted victim data"

# 4. A symlinked tmp directory is refused.
rm -f "$WORK/repo/tmp/demo"
rmdir "$WORK/repo/tmp"
ln -s "$WORK/victim" "$WORK/repo/tmp"
if prepare_demo_dir "$WORK/repo" >/dev/null; then
  fail "symlinked tmp directory was accepted"
fi
[ -f "$WORK/victim/keep" ] || fail "tmp symlink redirection deleted victim data"

# 5. A symlink child is removed as a link; its target survives.
rm -f "$WORK/repo/tmp"
mkdir -p "$WORK/repo/tmp/demo"
ln -s "$WORK/victim" "$WORK/repo/tmp/demo/child"
prepare_demo_dir "$WORK/repo" >/dev/null || fail "child symlink preparation failed"
[ ! -e "$WORK/repo/tmp/demo/child" ] || fail "child symlink was not removed"
[ -f "$WORK/victim/keep" ] || fail "child symlink target was deleted"

echo "demo path safety: PASS"
