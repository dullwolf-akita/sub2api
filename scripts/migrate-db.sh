#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Old DB (the one you have data in)
OLD_CONTAINER="${OLD_CONTAINER:-sub2api-postgres}"
OLD_PGUSER="${OLD_PGUSER:-sub2api}"
OLD_PGDATABASE="${OLD_PGDATABASE:-sub2api}"

# New DB (the fresh one we use for testing)
NEW_CONTAINER="${NEW_CONTAINER:-sub2api-latest-postgres}"
NEW_PGUSER="${NEW_PGUSER:-sub2api}"
NEW_PGDATABASE="${NEW_PGDATABASE:-sub2api}"

BACKUP_DIR="${BACKUP_DIR:-${ROOT_DIR}/deploy/db-backups}"
TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
BACKUP_FILE="${BACKUP_DIR}/${OLD_PGDATABASE}_${TIMESTAMP}.dump"

mkdir -p "${BACKUP_DIR}"

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

wait_for_pg() {
  local c="$1"
  for _ in {1..60}; do
    if docker exec "$c" pg_isready -U sub2api >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "Container $c not ready in time" >&2
  exit 1
}

require_cmd docker

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon not running." >&2
  exit 1
fi

# ---- Step 1: start old postgres ----
if ! container_exists "$OLD_CONTAINER"; then
  echo "ERROR: Old DB container '$OLD_CONTAINER' not found."
  echo "Make sure your old project's docker-compose was used to create it."
  exit 1
fi

if ! container_running "$OLD_CONTAINER"; then
  echo "[1/5] Starting old database container ($OLD_CONTAINER)..."
  docker start "$OLD_CONTAINER" >/dev/null
fi
wait_for_pg "$OLD_CONTAINER"
echo "  OK"

# ---- Step 2: pg_dump ----
echo "[2/5] Dumping old database to ${BACKUP_FILE}..."
docker exec "$OLD_CONTAINER" pg_dump -U "$OLD_PGUSER" -d "$OLD_PGDATABASE" \
  --format=custom --compress=9 --no-owner \
  > "$BACKUP_FILE"
echo "  Done ($(ls -lh "$BACKUP_FILE" | awk '{print $5}'))"

# ---- Step 3: start new postgres ----
if ! container_running "$NEW_CONTAINER"; then
  echo "[3/5] Starting new database container ($NEW_CONTAINER)..."
  if container_exists "$NEW_CONTAINER"; then
    docker start "$NEW_CONTAINER" >/dev/null
  else
    echo "  New container doesn't exist; create it first with start-local-dev.sh" >&2
    exit 1
  fi
  wait_for_pg "$NEW_CONTAINER"
fi
echo "  OK (running on port $(docker port "$NEW_CONTAINER" 5432 2>/dev/null || echo '?'))"

# ---- Step 4: create test database & restore ----
TEST_DB="${OLD_PGDATABASE}_migrate_test_${TIMESTAMP}"
echo "[4/5] Creating test database '${TEST_DB}' on new container..."
docker exec "$NEW_CONTAINER" psql -U "$NEW_PGUSER" -d postgres \
  -c "CREATE DATABASE ${TEST_DB} WITH ENCODING 'UTF8' LC_COLLATE 'en_US.UTF-8' LC_CTYPE 'en_US.UTF-8' TEMPLATE template0;" 2>/dev/null || \
docker exec "$NEW_CONTAINER" psql -U "$NEW_PGUSER" -d postgres \
  -c "CREATE DATABASE ${TEST_DB};"
echo "  Restoring dump..."
docker exec -i "$NEW_CONTAINER" pg_restore -U "$NEW_PGUSER" -d "$TEST_DB" --no-owner < "$BACKUP_FILE"
echo "  OK"

# ---- Step 5: run new app migration against test DB ----
echo "[5/5] Running new version migration against '${TEST_DB}'..."
(
  cd "${ROOT_DIR}/backend"
  AUTO_SETUP=true \
    DATABASE_HOST=127.0.0.1 \
    DATABASE_PORT="$(docker port "$NEW_CONTAINER" 5432 | head -1 | sed 's/.*://')" \
    DATABASE_USER="$NEW_PGUSER" \
    DATABASE_PASSWORD=sub2api_dev_password \
    DATABASE_DBNAME="${TEST_DB}" \
    DATABASE_SSLMODE=disable \
    go run ./cmd/server 2>&1 | head -100
) || true

echo ""
echo "========================================"
echo "  Migration test complete!"
echo "========================================"
echo ""
echo "Backup:        ${BACKUP_FILE}"
echo "Test database: ${TEST_DB} (on container ${NEW_CONTAINER})"
echo ""
echo "If the new app started without errors above, the migration succeeded."
echo ""
echo "To proceed with real migration:"
echo "  1. Stop the current new-app backend"
echo "  2. Drop the old database on the new container, restore the backup there"
echo "  3. Restart the new app with AUTO_SETUP=true pointing to that database"
echo ""
echo "To clean up the test database:"
echo "  docker exec ${NEW_CONTAINER} psql -U ${NEW_PGUSER} -d postgres -c \"DROP DATABASE IF EXISTS ${TEST_DB};\""
