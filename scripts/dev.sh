#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

if [ ! -d "$ROOT_DIR/web/node_modules" ]; then
  (cd "$ROOT_DIR/web" && npm install)
fi

if ! command -v air >/dev/null 2>&1; then
  echo "Installing air..."
  GOBIN="$ROOT_DIR/.bin" go install github.com/air-verse/air@latest
fi

AIR_BIN=${AIR_BIN:-"$ROOT_DIR/.bin/air"}
if [ ! -x "$AIR_BIN" ]; then
  AIR_BIN=$(command -v air)
fi

if [ -z "$AIR_BIN" ]; then
  echo "air not found"
  exit 1
fi

cleanup() {
  if [ -n "${BACK_PID:-}" ]; then
    kill "$BACK_PID" >/dev/null 2>&1 || true
  fi
  if [ -n "${FRONT_PID:-}" ]; then
    kill "$FRONT_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

(cd "$ROOT_DIR" && "$AIR_BIN" -c .air.toml) &
BACK_PID=$!

(cd "$ROOT_DIR/web" && npm run dev) &
FRONT_PID=$!

wait
