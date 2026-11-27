#!/usr/bin/env bash
# Smoke test for a built removarr image (spec section 12): the container reports healthy, runs
# as a non-root user, and keeps a saved setting in its volume across a restart.
#
#   scripts/smoke-image.sh IMAGE
#
# Needs docker (or podman behind the docker CLI) and curl. Leaves nothing behind.
set -euo pipefail

image=${1:?usage: scripts/smoke-image.sh IMAGE}
name=removarr-smoke-$$
volume=removarr-smoke-$$

cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker volume rm -f "$volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
  echo "smoke test failed: $*" >&2
  docker logs "$name" 2>&1 | tail -20 >&2 || true
  exit 1
}

wait_healthy() {
  local status
  for _ in $(seq 1 120); do
    status=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$name" 2>/dev/null || echo gone)
    case $status in
      healthy) return 0 ;;
      none) fail "the image has no health check" ;;
      gone) fail "the container stopped" ;;
    esac
    sleep 1
  done
  fail "not healthy after 120 seconds (last status: $status)"
}

user=$(docker image inspect -f '{{.Config.User}}' "$image")
case $user in
  "" | root | 0 | 0:*) fail "the image runs as root (user '$user')" ;;
esac

docker volume create "$volume" >/dev/null
docker run -d --name "$name" -v "$volume:/config" -p 127.0.0.1::7272 "$image" >/dev/null
wait_healthy

addr=$(docker port "$name" 7272/tcp | head -1)
base="http://$addr"
curl -fsS -X PUT -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' \
  -d '{"rules":{"graceDays":17}}' "$base/api/settings" >/dev/null || fail "saving a setting was refused"

docker restart "$name" >/dev/null
wait_healthy
addr=$(docker port "$name" 7272/tcp | head -1)
curl -fsS "http://$addr/api/settings" | grep -q '"graceDays":17' || fail "the saved setting did not survive a restart"

echo "smoke test passed: $image (user $user)"
