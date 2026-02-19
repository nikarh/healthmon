#!/usr/bin/env bash
set -euo pipefail

LABEL="healthmon.test=1"
PREFIX="hm-test"

container_rm() {
  local name="$1"
  if docker inspect "$name" >/dev/null 2>&1; then
    docker rm -f "$name" >/dev/null
  fi
}

container_rm "${PREFIX}-alpha"
container_rm "${PREFIX}-bravo"
container_rm "${PREFIX}-health"
container_rm "${PREFIX}-task"

# Basic lifecycle

docker run -d --name "${PREFIX}-alpha" --label "$LABEL" alpine sleep 120 >/dev/null
sleep 1

docker stop "${PREFIX}-alpha" >/dev/null
sleep 1

docker start "${PREFIX}-alpha" >/dev/null
sleep 1

docker kill "${PREFIX}-alpha" >/dev/null
sleep 1

docker restart "${PREFIX}-alpha" >/dev/null
sleep 1

# Rename flow

docker rename "${PREFIX}-alpha" "${PREFIX}-bravo"
sleep 1

# Recreate same name (destroy + create)

docker rm -f "${PREFIX}-bravo" >/dev/null
sleep 1

docker run -d --name "${PREFIX}-bravo" --label "$LABEL" alpine sleep 120 >/dev/null
sleep 1

# Health status events

docker run -d --name "${PREFIX}-health" --label "$LABEL" \
  --health-cmd="exit 1" --health-interval=1s --health-retries=1 --health-timeout=1s \
  alpine sleep 30 >/dev/null
sleep 3

docker rm -f "${PREFIX}-health" >/dev/null
sleep 1

# One-shot task

docker run --name "${PREFIX}-task" --label "$LABEL" alpine sh -c "exit 0" >/dev/null
sleep 1

# Final cleanup

docker rm -f "${PREFIX}-bravo" >/dev/null
