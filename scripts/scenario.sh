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
container_rm "${PREFIX}-swap"

# Two containers with interleaved lifecycle + rename/recreate.

docker run -d --name "${PREFIX}-alpha" --label "$LABEL" alpine sleep 120 >/dev/null
sleep 0.5

docker run -d --name "${PREFIX}-bravo" --label "$LABEL" alpine sleep 120 >/dev/null
sleep 0.5

# Interleaved stop/start to create close event timing.
(docker stop "${PREFIX}-alpha" >/dev/null &) 
(docker restart "${PREFIX}-bravo" >/dev/null &)
wait
sleep 0.5

# Rename alpha -> swap, then recreate alpha.

docker rename "${PREFIX}-alpha" "${PREFIX}-swap"
sleep 0.5

docker run -d --name "${PREFIX}-alpha" --label "$LABEL" alpine sleep 120 >/dev/null
sleep 0.5

# Kill swap and remove, while alpha/bravo still running.

docker kill "${PREFIX}-swap" >/dev/null
sleep 0.5

docker rm -f "${PREFIX}-swap" >/dev/null
sleep 0.5

# Recreate bravo to force name reuse with new container id.

docker rm -f "${PREFIX}-bravo" >/dev/null
sleep 0.5

docker run -d --name "${PREFIX}-bravo" --label "$LABEL" alpine sleep 120 >/dev/null
sleep 0.5

# Final cleanup.

docker rm -f "${PREFIX}-alpha" >/dev/null
sleep 0.5

docker rm -f "${PREFIX}-bravo" >/dev/null
