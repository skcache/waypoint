#!/usr/bin/env bash
# Safe paths for the deterministic demo. The demo output directory is fixed at
# <repo>/tmp/demo; callers cannot redirect cleanup at an arbitrary path.

prepare_demo_dir() {
  local root="${1:?repository root required}"
  local tmp="$root/tmp"
  local demo="$tmp/demo"

  [ -d "$root" ] || return 1

  # Never traverse a symlinked tmp or demo directory. A symlink could otherwise
  # make a recursive cleanup delete a directory outside the repository.
  if [ -L "$tmp" ]; then
    return 1
  fi
  mkdir -p "$tmp" || return 1
  if [ ! -d "$tmp" ] || [ -L "$tmp" ]; then
    return 1
  fi

  if [ -L "$demo" ]; then
    return 1
  fi
  if [ -e "$demo" ] && [ ! -d "$demo" ]; then
    return 1
  fi
  mkdir -p "$demo" || return 1

  local canon_tmp canon_demo
  canon_tmp="$(cd -P "$tmp" && pwd)" || return 1
  canon_demo="$(cd -P "$demo" && pwd)" || return 1
  if [ "$canon_demo" != "$canon_tmp/demo" ]; then
    return 1
  fi

  # Clear only direct children of the fixed directory. Never remove the
  # directory itself, and never accept a caller-supplied cleanup target.
  # rm on a child symlink removes the link, not its target.
  find "$demo" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} + 2>/dev/null || return 1

  printf '%s\n' "$demo"
}
