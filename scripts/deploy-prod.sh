#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Sub2API 生产环境部署脚本
# 用途：在一台已有旧 sub2api 运行的服务器上部署新版本（并行运行）
# 步骤：创建新容器 → 迁移数据库 → 启动新项目
# =============================================================================

# ---------- 可配置参数（按需修改）----------
NEW_DIR="${NEW_DIR:-/opt/sub2api-latest}"          # 新项目路径
OLD_CONTAINER="${OLD_CONTAINER:-sub2api-postgres}"  # 旧数据库容器名
NEW_PG_CONTAINER="${NEW_PG_CONTAINER:-sub2api-latest-postgres}"
NEW_REDIS_CONTAINER="${NEW_REDIS_CONTAINER:-sub2api-latest-redis}"

NEW_PG_PORT="${NEW_PG_PORT:-5434}"        # 新 PostgreSQL 宿主机端口
NEW_REDIS_PORT="${NEW_REDIS_PORT:-6381}"  # 新 Redis 宿主机端口
NEW_BACKEND_PORT="${NEW_BACKEND_PORT:-18080}"
NEW_FRONTEND_PORT="${NEW_FRONTEND_PORT:-18081}"

NEW_PG_USER="${NEW_PG_USER:-sub2api}"
NEW_PG_PASSWORD="${NEW_PG_PASSWORD:-sub2api_dev_password}"
NEW_PG_DATABASE="${NEW_PG_DATABASE:-sub2api}"

BACKEND_LOG="${BACKEND_LOG:-/tmp/sub2api-latest.log}"
FRONTEND_LOG="${FRONTEND_LOG:-/tmp/sub2api-latest-frontend.log}"
# -----------------------------------------

GIT_REPO="${GIT_REPO:-https://github.com/dullwolf-akita/sub2api.git}"

echo "=============================================="
echo " Sub2API 生产环境部署"
echo "=============================================="

# ---------- Step 1: 克隆新项目 ----------
echo ""
echo "[1/5] 克隆新项目..."
if [ -d "$NEW_DIR" ]; then
  echo "  目录已存在，跳过克隆"
  cd "$NEW_DIR"
  git pull --depth 1 2>/dev/null || true
else
  git clone --depth 1 "$GIT_REPO" "$NEW_DIR"
fi

# ---------- Step 2: 启动新数据库 ----------
echo ""
echo "[2/5] 启动新 PostgreSQL (端口 $NEW_PG_PORT) & Redis (端口 $NEW_REDIS_PORT)..."

docker rm -f "$NEW_PG_CONTAINER" 2>/dev/null || true
docker rm -f "$NEW_REDIS_CONTAINER" 2>/dev/null || true

docker run -d --name "$NEW_PG_CONTAINER" \
  -e POSTGRES_USER="$NEW_PG_USER" \
  -e POSTGRES_PASSWORD="$NEW_PG_PASSWORD" \
  -e POSTGRES_DB="$NEW_PG_DATABASE" \
  -p "127.0.0.1:${NEW_PG_PORT}:5432" \
  postgres:18-alpine

docker run -d --name "$NEW_REDIS_CONTAINER" \
  -p "127.0.0.1:${NEW_REDIS_PORT}:6379" \
  redis:8-alpine

echo "  等待新 PostgreSQL 就绪..."
for i in $(seq 1 60); do
  if docker exec "$NEW_PG_CONTAINER" pg_isready -U "$NEW_PG_USER" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
echo "  OK"

echo "  等待新 Redis 就绪..."
for i in $(seq 1 60); do
  if docker exec "$NEW_REDIS_CONTAINER" redis-cli ping >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
echo "  OK"

# ---------- Step 3: 复制 .env ----------
echo ""
echo "[3/5] 复制旧项目配置文件..."
if [ -f "${NEW_DIR}/deploy/.env" ]; then
  echo "  deploy/.env 已存在，跳过"
else
  if command -v docker &>/dev/null && docker ps --format '{{.Names}}' | grep -q "sub2api$"; then
    OLD_COMPOSE_DIR=$(docker inspect sub2api-postgres --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}' 2>/dev/null || echo "")
    OLD_DIR=$(dirname "$OLD_COMPOSE_DIR" 2>/dev/null || echo "")
    if [ -n "$OLD_DIR" ] && [ -f "${OLD_DIR}/.env" ]; then
      cp "${OLD_DIR}/.env" "${NEW_DIR}/deploy/.env"
      echo "  从 ${OLD_DIR}/.env 复制成功"
    else
      echo "  ⚠️  找不到旧项目 .env，请手动配置 ${NEW_DIR}/deploy/.env"
    fi
  else
    echo "  ⚠️  找不到旧项目 .env，请手动配置 ${NEW_DIR}/deploy/.env"
  fi
fi

# ---------- Step 4: 迁移数据库 ----------
echo ""
echo "[4/5] 迁移数据库..."
if ! docker ps --format '{{.Names}}' | grep -q "$OLD_CONTAINER"; then
  echo "  ⚠️  旧数据库容器 $OLD_CONTAINER 未运行，跳过迁移"
  echo "  后续请手动执行: docker exec -i $NEW_PG_CONTAINER pg_restore ..."
else
  echo "  从 $OLD_CONTAINER 导出..."
  docker exec "$OLD_CONTAINER" pg_dump -U "$NEW_PG_USER" -d "$NEW_PG_DATABASE" \
    --format=custom --compress=9 > /tmp/sub2api_migrate.dump

  echo "  导入到 $NEW_PG_CONTAINER..."
  docker exec -i "$NEW_PG_CONTAINER" pg_restore -U "$NEW_PG_USER" -d "$NEW_PG_DATABASE" < /tmp/sub2api_migrate.dump
  rm /tmp/sub2api_migrate.dump

  echo "  验证:"
  docker exec "$NEW_PG_CONTAINER" psql -U "$NEW_PG_USER" -d "$NEW_PG_DATABASE" -c "SELECT count(*) as users FROM users;"
fi

# ---------- Step 5: 启动新项目 ----------
echo ""
echo "[5/5] 启动新项目..."

# 后端
echo "  启动后端 (端口 $NEW_BACKEND_PORT)..."
cd "${NEW_DIR}/backend"
AUTO_SETUP=true \
  SERVER_HOST=0.0.0.0 \
  SERVER_PORT="$NEW_BACKEND_PORT" \
  DATABASE_HOST=127.0.0.1 \
  DATABASE_PORT="$NEW_PG_PORT" \
  DATABASE_USER="$NEW_PG_USER" \
  DATABASE_PASSWORD="$NEW_PG_PASSWORD" \
  DATABASE_DBNAME="$NEW_PG_DATABASE" \
  DATABASE_SSLMODE=disable \
  REDIS_HOST=127.0.0.1 \
  REDIS_PORT="$NEW_REDIS_PORT" \
  REDIS_PASSWORD= \
  REDIS_DB=0 \
  nohup go run ./cmd/server > "$BACKEND_LOG" 2>&1 &

echo "  等待后端就绪..."
for i in $(seq 1 45); do
  sleep 2
  if curl -fsS "http://127.0.0.1:${NEW_BACKEND_PORT}/health" >/dev/null 2>&1; then
    echo "  后端 OK"
    break
  fi
done

# 前端
echo "  安装前端依赖..."
cd "${NEW_DIR}/frontend"
if [ ! -d node_modules ]; then
  pnpm install 2>&1 | tail -3
fi

echo "  启动前端 (端口 $NEW_FRONTEND_PORT)..."
VITE_DEV_PORT="$NEW_FRONTEND_PORT" \
  VITE_DEV_PROXY_TARGET="http://127.0.0.1:${NEW_BACKEND_PORT}" \
  nohup ./node_modules/.bin/vite --host 0.0.0.0 --port "$NEW_FRONTEND_PORT" > "$FRONTEND_LOG" 2>&1 &

sleep 3
if curl -sI "http://127.0.0.1:${NEW_FRONTEND_PORT}" >/dev/null 2>&1; then
  echo "  前端 OK"
fi

# ---------- 完成 ----------
echo ""
echo "=============================================="
echo "  部署完成！"
echo "=============================================="
echo ""
echo "  后端 API:  http://<服务器IP>:${NEW_BACKEND_PORT}"
echo "  Health:    http://<服务器IP>:${NEW_BACKEND_PORT}/health"
echo "  前端页面:  http://<服务器IP>:${NEW_FRONTEND_PORT}"
echo ""
echo "  旧项目仍然运行在原有端口，不受影响。"
echo "  后端日志:  ${BACKEND_LOG}"
echo "  前端日志:  ${FRONTEND_LOG}"
echo ""
echo "  停止新项目:"
echo "    pkill -f 'go run.*cmd/server'"
echo "    pkill -f 'vite.*${NEW_FRONTEND_PORT}'"
echo ""
echo "  删除新数据库（如需重置）:"
echo "    docker rm -f ${NEW_PG_CONTAINER} ${NEW_REDIS_CONTAINER}"
echo ""
