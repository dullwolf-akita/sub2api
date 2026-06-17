#!/usr/bin/env bash
set -e

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${PORT:-18080}"

PG_CONTAINER="${PG_CONTAINER:-sub2api-latest-postgres}"
REDIS_CONTAINER="${REDIS_CONTAINER:-sub2api-latest-redis}"

echo "=== Sub2API Restart ==="

# Ensure database containers are running
for c in "$PG_CONTAINER" "$REDIS_CONTAINER"; do
  if docker ps --format '{{.Names}}' | grep -qx "$c"; then
    echo "$c running"
  elif docker ps -a --format '{{.Names}}' | grep -qx "$c"; then
    echo "Starting $c..."
    docker start "$c" >/dev/null
  else
    echo "ERROR: container $c not found, run deploy-prod.sh first"
    exit 1
  fi
done

echo "Waiting for PostgreSQL..."
for i in $(seq 1 30); do
  docker exec "$PG_CONTAINER" pg_isready -U sub2api >/dev/null 2>&1 && break
  sleep 1
done

echo "Waiting for Redis..."
for i in $(seq 1 30); do
  docker exec "$REDIS_CONTAINER" redis-cli ping >/dev/null 2>&1 && break
  sleep 1
done

# Kill existing process on port
PID=$(lsof -ti :$PORT 2>/dev/null || true)
if [ -n "$PID" ]; then
  echo "Killing process on port $PORT (PID: $PID)..."
  kill -9 $PID 2>/dev/null || true
  sleep 1
fi

# Build frontend
echo "Building frontend..."
cd "$ROOT_DIR/frontend"
if [ ! -d node_modules ]; then
  pnpm install
fi
pnpm run build

# Build backend
echo "Building backend..."
cd "$ROOT_DIR/backend"
export GOPROXY=https://goproxy.cn,direct
go build -tags embed -o sub2api ./cmd/server

# Start
echo "Starting sub2api on port $PORT..."
nohup ./sub2api > /tmp/sub2api-prod.log 2>&1 &

# Wait and verify
for i in $(seq 1 15); do
  sleep 1
  if curl -fsS "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then
    echo "sub2api started successfully (PID: $(lsof -ti :$PORT))"
    exit 0
  fi
done

echo "ERROR: sub2api failed to start within 15s"
echo "Check log: /tmp/sub2api-prod.log"
exit 1
