#!/usr/bin/env bash
set -e

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${PORT:-18080}"

PG_CONTAINER="${PG_CONTAINER:-sub2api-latest-postgres}"
REDIS_CONTAINER="${REDIS_CONTAINER:-sub2api-latest-redis}"

# 宿主机直跑专用端口（勿与 Docker .env 里的 5432/6379 混用）
HOST_DATABASE_HOST="${HOST_DATABASE_HOST:-127.0.0.1}"
HOST_DATABASE_PORT="${HOST_DATABASE_PORT:-5434}"
HOST_REDIS_HOST="${HOST_REDIS_HOST:-127.0.0.1}"
HOST_REDIS_PORT="${HOST_REDIS_PORT:-6381}"
STARTUP_WAIT_SEC="${STARTUP_WAIT_SEC:-60}"
USE_EXTERNAL_DATABASE="${USE_EXTERNAL_DATABASE:-}"

echo "=== Sub2API Restart ==="

# Source .env early so HOST_*/DATABASE_* drive container skip logic
ENV_FILE="$ROOT_DIR/deploy/.env"
if [ ! -f "$ENV_FILE" ]; then
  ENV_FILE="$ROOT_DIR/.env"
fi
if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

# Prefer HOST_* overrides after sourcing .env (fall back to DATABASE_* / defaults)
HOST_DATABASE_HOST="${HOST_DATABASE_HOST:-${DATABASE_HOST:-127.0.0.1}}"
HOST_DATABASE_PORT="${HOST_DATABASE_PORT:-${DATABASE_PORT:-5434}}"
HOST_REDIS_HOST="${HOST_REDIS_HOST:-${REDIS_HOST:-127.0.0.1}}"
HOST_REDIS_PORT="${HOST_REDIS_PORT:-${REDIS_PORT:-6381}}"
USE_EXTERNAL_DATABASE="${USE_EXTERNAL_DATABASE:-false}"

is_local_host() {
  case "$1" in
    127.0.0.1|localhost|::1|"") return 0 ;;
    *) return 1 ;;
  esac
}

# TCP reachability check for local Redis (no redis-cli required)
redis_port_open() {
  local host="$1"
  local port="$2"
  if command -v redis-cli >/dev/null 2>&1; then
    redis-cli -h "$host" -p "$port" ping 2>/dev/null | grep -qi pong && return 0
  fi
  # bash /dev/tcp fallback
  (exec 3<>"/dev/tcp/${host}/${port}") >/dev/null 2>&1 && exec 3<&- && exec 3>&- && return 0
  return 1
}

# External Postgres: explicit flag, Aliyun RDS host, or any non-loopback host
use_external_db=false
case "${USE_EXTERNAL_DATABASE}" in
  1|true|TRUE|yes|YES|on|ON) use_external_db=true ;;
esac
if echo "${HOST_DATABASE_HOST}" | grep -qi 'aliyuncs\.com'; then
  use_external_db=true
fi
if ! is_local_host "${HOST_DATABASE_HOST}"; then
  use_external_db=true
fi

ensure_container() {
  local c="$1"
  if docker ps --format '{{.Names}}' | grep -qx "$c"; then
    echo "$c running"
    return 0
  elif docker ps -a --format '{{.Names}}' | grep -qx "$c"; then
    echo "Starting $c..."
    docker start "$c" >/dev/null
    return 0
  else
    return 1
  fi
}

ensure_redis_container() {
  if ensure_container "$REDIS_CONTAINER"; then
    return 0
  fi

  echo "Creating Redis container ${REDIS_CONTAINER} on 127.0.0.1:${HOST_REDIS_PORT}..."
  docker run -d --name "$REDIS_CONTAINER" \
    -p "127.0.0.1:${HOST_REDIS_PORT}:6379" \
    redis:8-alpine >/dev/null
}

# Ensure dependency containers are running (skip local PG when using external RDS/host)
if [ "$use_external_db" = true ]; then
  echo "Using external database at ${HOST_DATABASE_HOST}:${HOST_DATABASE_PORT} (skip ${PG_CONTAINER})"
else
  if ! ensure_container "$PG_CONTAINER"; then
    echo "ERROR: PostgreSQL container $PG_CONTAINER not found, and no external database is configured."
    echo "       Set DATABASE_HOST to the external database host or start the configured PostgreSQL service."
    exit 1
  fi
fi

# Redis stays local: reuse any reachable local port; only require named container if port is down
if ! is_local_host "${HOST_REDIS_HOST}"; then
  echo "Using non-local Redis at ${HOST_REDIS_HOST}:${HOST_REDIS_PORT} (skip ${REDIS_CONTAINER})"
else
  if redis_port_open "${HOST_REDIS_HOST}" "${HOST_REDIS_PORT}"; then
    echo "Local Redis already reachable at ${HOST_REDIS_HOST}:${HOST_REDIS_PORT} (skip ${REDIS_CONTAINER})"
  else
    reused=false
    for alt in 6380 6381; do
      if [ "$alt" = "${HOST_REDIS_PORT}" ]; then
        continue
      fi
      if redis_port_open 127.0.0.1 "$alt"; then
        echo "Configured Redis ${HOST_REDIS_HOST}:${HOST_REDIS_PORT} not up; reusing local Redis on 127.0.0.1:${alt} (skip ${REDIS_CONTAINER})"
        HOST_REDIS_HOST="127.0.0.1"
        HOST_REDIS_PORT="$alt"
        reused=true
        break
      fi
    done

    if [ "$reused" != true ]; then
      ensure_redis_container
      echo "Waiting for Redis container ${REDIS_CONTAINER}..."
      ready=false
      for i in $(seq 1 30); do
        if docker exec "$REDIS_CONTAINER" redis-cli ping >/dev/null 2>&1 \
          || redis_port_open "${HOST_REDIS_HOST}" "${HOST_REDIS_PORT}"; then
          ready=true
          break
        fi
        sleep 1
      done
      if [ "$ready" != true ]; then
        echo "ERROR: started ${REDIS_CONTAINER} but Redis not reachable on ${HOST_REDIS_HOST}:${HOST_REDIS_PORT}"
        exit 1
      fi
      echo "Redis ready via ${REDIS_CONTAINER}"
    fi
  fi
fi

if [ "$use_external_db" = true ]; then
  echo "Skipping local PostgreSQL container health check"
else
  echo "Waiting for PostgreSQL..."
  for i in $(seq 1 30); do
    docker exec "$PG_CONTAINER" pg_isready -U sub2api >/dev/null 2>&1 && break
    sleep 1
  done
fi

# Kill existing process on port
PID=$(lsof -ti :$PORT 2>/dev/null || true)
if [ -n "$PID" ]; then
  echo "Killing process on port $PORT (PID: $PID)..."
  kill -9 $PID 2>/dev/null || true
  sleep 1
fi

# Check if binary exists, if not rebuild
cd "$ROOT_DIR/backend"
if [ ! -f sub2api ]; then
  echo "Binary not found, building backend..."
  export GOPROXY=https://goproxy.cn,direct
  go build -tags embed -o sub2api ./cmd/server
fi

DB_USER="${HOST_DATABASE_USER:-${DATABASE_USER:-${POSTGRES_USER:-sub2api}}}"
DB_PASSWORD="${HOST_DATABASE_PASSWORD:-${DATABASE_PASSWORD:-${POSTGRES_PASSWORD:-sub2api_dev_password}}}"
DB_NAME="${HOST_DATABASE_DBNAME:-${DATABASE_DBNAME:-${POSTGRES_DB:-sub2api}}}"
DB_SSLMODE="${HOST_DATABASE_SSLMODE:-${DATABASE_SSLMODE:-disable}}"

# Start with required env vars（显式覆盖 Docker .env 中的 DATABASE_PORT/REDIS_PORT）
echo "Starting sub2api on port $PORT..."
echo "  DB:  ${HOST_DATABASE_HOST}:${HOST_DATABASE_PORT}/${DB_NAME} (user=${DB_USER})"
echo "  Redis: ${HOST_REDIS_HOST}:${HOST_REDIS_PORT}"
AUTO_SETUP=true \
  SERVER_HOST=0.0.0.0 \
  SERVER_PORT="$PORT" \
  DATABASE_HOST="$HOST_DATABASE_HOST" \
  DATABASE_PORT="$HOST_DATABASE_PORT" \
  DATABASE_USER="$DB_USER" \
  DATABASE_PASSWORD="$DB_PASSWORD" \
  DATABASE_DBNAME="$DB_NAME" \
  DATABASE_SSLMODE="$DB_SSLMODE" \
  REDIS_HOST="$HOST_REDIS_HOST" \
  REDIS_PORT="$HOST_REDIS_PORT" \
  REDIS_PASSWORD="${REDIS_PASSWORD:-}" \
  REDIS_DB="${REDIS_DB:-0}" \
  JWT_SECRET="${JWT_SECRET:-}" \
  TOTP_ENCRYPTION_KEY="${TOTP_ENCRYPTION_KEY:-}" \
  nohup ./sub2api > /tmp/sub2api-prod.log 2>&1 &

# Wait and verify
for i in $(seq 1 "$STARTUP_WAIT_SEC"); do
  sleep 1
  if curl -fsS "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then
    echo "sub2api started successfully (PID: $(lsof -ti :$PORT))"
    exit 0
  fi
done

echo "ERROR: sub2api failed to start within ${STARTUP_WAIT_SEC}s"
echo "Check log: /tmp/sub2api-prod.log"
exit 1
