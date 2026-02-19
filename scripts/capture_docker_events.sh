#!/usr/bin/env bash
set -euo pipefail

OUT="${1:-/tmp/healthmon-events.jsonl}"
SCENARIO_SCRIPT="${2:-$(dirname "$0")/scenario.sh}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

: > "$OUT"

LABEL_FILTER="healthmon.test=1"

docker events --format '{{json .}}' \
  --filter type=container \
  --filter label="$LABEL_FILTER" > "$OUT" &
EVENTS_PID=$!

cleanup() {
  kill "$EVENTS_PID" >/dev/null 2>&1 || true
  wait "$EVENTS_PID" >/dev/null 2>&1 || true
}
trap cleanup EXIT

"$SCENARIO_SCRIPT"

sleep 2
cleanup

echo "Captured events to $OUT"
