#!/usr/bin/env bash
set -euo pipefail

REPO_DIR="/home/banhsbao/workspace/goclaw-orca"
ORCA_HOST="100.79.212.126"
ORCA_PORT="6768"
DOCKER_WAIT_SECONDS=180
HEALTH_WAIT_SECONDS=120

cd "$REPO_DIR"

log() {
  printf '[%s] %s\n' "$(date '+%F %T')" "$*"
}

wait_for_docker() {
  local waited=0
  until docker info >/dev/null 2>&1; do
    if (( waited >= DOCKER_WAIT_SECONDS )); then
      log "ERROR: docker daemon not ready after ${DOCKER_WAIT_SECONDS}s"
      exit 1
    fi
    sleep 2
    waited=$((waited + 2))
  done
  log "docker ready"
}

wait_for_health() {
  local container="$1" waited=0 status
  until status=$(docker inspect -f '{{.State.Health.Status}}' "$container" 2>/dev/null) && [[ "$status" == "healthy" ]]; do
    if (( waited >= HEALTH_WAIT_SECONDS )); then
      log "WARN: $container not healthy after ${HEALTH_WAIT_SECONDS}s (status: ${status:-unknown})"
      return 1
    fi
    sleep 3
    waited=$((waited + 3))
  done
  log "$container healthy"
}

wait_for_docker

docker compose up -d
log "compose up done"

wait_for_health goclaw-orca-postgres-1 || true
wait_for_health goclaw-orca-goclaw-1 || true

if timeout 5 bash -c "echo > /dev/tcp/${ORCA_HOST}/${ORCA_PORT}" 2>/dev/null; then
  log "orca runtime reachable at ${ORCA_HOST}:${ORCA_PORT}"
else
  log "WARN: orca runtime at ${ORCA_HOST}:${ORCA_PORT} unreachable (mac-m1 off or Orca not running with --serve)"
fi
