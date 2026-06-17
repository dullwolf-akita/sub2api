#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="${LOG_DIR:-/tmp/sub2api-latest}"

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-sub2api-latest-postgres}"
REDIS_CONTAINER="${REDIS_CONTAINER:-sub2api-latest-redis}"

POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:18-alpine}"
REDIS_IMAGE="${REDIS_IMAGE:-redis:8-alpine}"

POSTGRES_HOST_PORT="${POSTGRES_HOST_PORT:-55432}"
REDIS_HOST_PORT="${REDIS_HOST_PORT:-56379}"
BACKEND_PORT="${BACKEND_PORT:-18080}"
FRONTEND_PORT="${FRONTEND_PORT:-18081}"

POSTGRES_USER="${POSTGRES_USER:-sub2api}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-sub2api_dev_password}"
POSTGRES_DB="${POSTGRES_DB:-sub2api}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@sub2api.local}"

BACKEND_LOG="${LOG_DIR}/backend.log"
FRONTEND_LOG="${LOG_DIR}/frontend.log"

mkdir -p "${LOG_DIR}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

container_exists() {
  docker ps -a --format '{{.Names}}' | grep -Fxq "$1"
}

container_running() {
  docker ps --format '{{.Names}}' | grep -Fxq "$1"
}

wait_for_postgres() {
  for _ in {1..60}; do
    if docker exec "${POSTGRES_CONTAINER}" pg_isready -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  echo "PostgreSQL did not become ready in time" >&2
  exit 1
}

wait_for_redis() {
  for _ in {1..60}; do
    if docker exec "${REDIS_CONTAINER}" redis-cli ping >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  echo "Redis did not become ready in time" >&2
  exit 1
}

wait_for_http() {
  local url="$1"
  local name="$2"

  for _ in {1..60}; do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  echo "${name} did not become ready in time: ${url}" >&2
  exit 1
}

start_postgres() {
  if container_exists "${POSTGRES_CONTAINER}"; then
    docker start "${POSTGRES_CONTAINER}" >/dev/null
  else
    docker run -d \
      --name "${POSTGRES_CONTAINER}" \
      -e POSTGRES_USER="${POSTGRES_USER}" \
      -e POSTGRES_PASSWORD="${POSTGRES_PASSWORD}" \
      -e POSTGRES_DB="${POSTGRES_DB}" \
      -p "127.0.0.1:${POSTGRES_HOST_PORT}:5432" \
      "${POSTGRES_IMAGE}" >/dev/null
  fi

  wait_for_postgres
}

start_redis() {
  if container_exists "${REDIS_CONTAINER}"; then
    docker start "${REDIS_CONTAINER}" >/dev/null
  else
    docker run -d \
      --name "${REDIS_CONTAINER}" \
      -p "127.0.0.1:${REDIS_HOST_PORT}:6379" \
      "${REDIS_IMAGE}" >/dev/null
  fi

  wait_for_redis
}

start_backend() {
  if curl -fsS "http://127.0.0.1:${BACKEND_PORT}/health" >/dev/null 2>&1; then
    return 0
  fi

  (
    cd "${ROOT_DIR}/backend"
    AUTO_SETUP=true \
      SERVER_HOST=127.0.0.1 \
      SERVER_PORT="${BACKEND_PORT}" \
      DATABASE_HOST=127.0.0.1 \
      DATABASE_PORT="${POSTGRES_HOST_PORT}" \
      DATABASE_USER="${POSTGRES_USER}" \
      DATABASE_PASSWORD="${POSTGRES_PASSWORD}" \
      DATABASE_DBNAME="${POSTGRES_DB}" \
      DATABASE_SSLMODE=disable \
      REDIS_HOST=127.0.0.1 \
      REDIS_PORT="${REDIS_HOST_PORT}" \
      REDIS_PASSWORD= \
      REDIS_DB=0 \
      ADMIN_EMAIL="${ADMIN_EMAIL}" \
      nohup go run ./cmd/server >"${BACKEND_LOG}" 2>&1 &
  )

  wait_for_http "http://127.0.0.1:${BACKEND_PORT}/health" "Backend"
}

start_frontend() {
  if curl -fsS "http://127.0.0.1:${FRONTEND_PORT}" >/dev/null 2>&1; then
    return 0
  fi

  (
    cd "${ROOT_DIR}/frontend"
    if [ ! -d node_modules ]; then
      pnpm install
    fi

    VITE_DEV_PORT="${FRONTEND_PORT}" \
      VITE_DEV_PROXY_TARGET="http://127.0.0.1:${BACKEND_PORT}" \
      nohup pnpm dev >"${FRONTEND_LOG}" 2>&1 &
  )

  wait_for_http "http://127.0.0.1:${FRONTEND_PORT}" "Frontend"
}

require_cmd docker
require_cmd curl
require_cmd go
require_cmd pnpm

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is not running. Start Docker Desktop and retry." >&2
  exit 1
fi

start_postgres
start_redis
start_backend
start_frontend

echo "Sub2API local dev stack is running."
echo "Frontend: http://127.0.0.1:${FRONTEND_PORT}"
echo "Backend:  http://127.0.0.1:${BACKEND_PORT}"
echo "Health:   http://127.0.0.1:${BACKEND_PORT}/health"
echo "Logs:"
echo "  Backend:  ${BACKEND_LOG}"
echo "  Frontend: ${FRONTEND_LOG}"
