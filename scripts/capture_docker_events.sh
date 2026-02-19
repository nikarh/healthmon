#!/usr/bin/env bash
set -euo pipefail

OUT_EVENTS="${1:-/tmp/healthmon-events.jsonl}"
OUT_INSPECTS="${2:-/tmp/healthmon-inspects.jsonl}"
SCENARIO_SCRIPT="${3:-$(dirname "$0")/scenario.sh}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

: > "$OUT_EVENTS"
: > "$OUT_INSPECTS"

LABEL_FILTER="healthmon.test=1"

"$(dirname "$0")/capture_events.py" \
  --out-events "$OUT_EVENTS" \
  --out-inspects "$OUT_INSPECTS" \
  --label "$LABEL_FILTER" &
CAPTURE_PID=$!

cleanup() {
  kill "$CAPTURE_PID" >/dev/null 2>&1 || true
  wait "$CAPTURE_PID" >/dev/null 2>&1 || true
}
trap cleanup EXIT

"$SCENARIO_SCRIPT"

sleep 2
cleanup

echo "Captured events to $OUT_EVENTS"
echo "Captured inspects to $OUT_INSPECTS"
