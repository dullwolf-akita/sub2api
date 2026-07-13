#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# Sub2API 生产环境部署脚本
# 用途：在一台已有旧 sub2api 运行的服务器上部署新版本（并行运行）
# 步骤：创建新容器 → 迁移数据库 → 编译启动新项目
# =============================================================================

# ---------- 可配置参数（按需修改）----------
NEW_DIR="${NEW_DIR:-/opt/sub2api-latest}"
OLD_CONTAINER="${OLD_CONTAINER:-sub2api-postgres}"
NEW_PG_CONTAINER="${NEW_PG_CONTAINER:-sub2api-latest-postgres}"
NEW_REDIS_CONTAINER="${NEW_REDIS_CONTAINER:-sub2api-latest-redis}"

NEW_PG_PORT="${NEW_PG_PORT:-5434}"
NEW_REDIS_PORT="${NEW_REDIS_PORT:-6381}"
NEW_BACKEND_PORT="${NEW_BACKEND_PORT:-18080}"

NEW_PG_USER="${NEW_PG_USER:-sub2api}"
NEW_PG_PASSWORD="${NEW_PG_PASSWORD:-sub2api_dev_password}"
NEW_PG_DATABASE="${NEW_PG_DATABASE:-sub2api}"

LOGFILE="${LOGFILE:-/tmp/sub2api-latest.log}"

GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
# -----------------------------------------

GIT_REPO="${GIT_REPO:-https://github.com/dullwolf-akita/sub2api.git}"

echo "=============================================="
echo " Sub2API 生产环境部署"
echo "=============================================="

# ---------- Step 1: 克隆 ----------
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

echo "  等待 PostgreSQL..."
for i in $(seq 1 60); do
  docker exec "$NEW_PG_CONTAINER" pg_isready -U "$NEW_PG_USER" >/dev/null 2>&1 && break
  sleep 1
done
echo "  OK"

echo "  等待 Redis..."
for i in $(seq 1 60); do
  docker exec "$NEW_REDIS_CONTAINER" redis-cli ping >/dev/null 2>&1 && break
  sleep 1
done
echo "  OK"

# ---------- Step 3: 复制 .env ----------
echo ""
echo "[3/5] 复制旧项目配置文件..."
if [ -f "${NEW_DIR}/deploy/.env" ]; then
  echo "  已存在，跳过"
else
  for cand in "${NEW_DIR}/.env" /opt/sub2api/.env /opt/sub2api/sub2api/deploy/.env; do
    if [ -f "$cand" ]; then
      cp "$cand" "${NEW_DIR}/deploy/.env"
      echo "  从 $cand 复制"
      break
    fi
  done
  [ -f "${NEW_DIR}/deploy/.env" ] || echo "  ⚠️  请手动配置 ${NEW_DIR}/deploy/.env"
fi

# ---------- Step 4: 迁移数据库 ----------
echo ""
echo "[4/5] 迁移数据库..."
if ! docker ps --format '{{.Names}}' | grep -q "$OLD_CONTAINER"; then
  echo "  ⚠️  旧数据库容器 $OLD_CONTAINER 未运行，跳过"
else
  echo "  导出中..."
  docker exec "$OLD_CONTAINER" pg_dump -U "$NEW_PG_USER" -d "$NEW_PG_DATABASE" \
    --format=custom --compress=9 > /tmp/sub2api_migrate.dump
  echo "  导入中..."
  docker exec -i "$NEW_PG_CONTAINER" pg_restore -U "$NEW_PG_USER" -d "$NEW_PG_DATABASE" \
    < /tmp/sub2api_migrate.dump
  rm /tmp/sub2api_migrate.dump
  echo "  验证:"
  docker exec "$NEW_PG_CONTAINER" psql -U "$NEW_PG_USER" -d "$NEW_PG_DATABASE" \
    -c "SELECT count(*) as users FROM users;"
fi

# ---------- Step 5: 编译启动 ----------
echo ""
echo "[5/5] 编译并启动新项目..."

# 如果端口已被占用则杀掉旧进程
OLD_PID=$(lsof -ti :$NEW_BACKEND_PORT 2>/dev/null || true)
if [ -n "$OLD_PID" ]; then
  echo "  清理端口 $NEW_BACKEND_PORT (PID: $OLD_PID)..."
  kill -9 "$OLD_PID" 2>/dev/null || true
  sleep 1
fi

# 编译前端
echo "  编译前端..."
cd "${NEW_DIR}/frontend"
if [ ! -d node_modules ]; then
  pnpm install --no-frozen-lockfile
fi
export NODE_OPTIONS="--max-old-space-size=2048"
./node_modules/.bin/vite build

# 编译后端（嵌入前端）
echo "  编译后端..."
cd "${NEW_DIR}/backend"
export GOPROXY
export GOSUMDB="${GOSUMDB:-sum.golang.google.cn}"
# shellcheck source=ensure-go-toolchain.sh
source "${NEW_DIR}/scripts/ensure-go-toolchain.sh"
ensure_go_toolchain "$NEW_DIR"
go build -tags embed -o sub2api ./cmd/server

# 启动
echo "  启动..."
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
  nohup ./sub2api > "$LOGFILE" 2>&1 &

echo "  等待就绪..."
for i in $(seq 1 30); do
  sleep 2
  if curl -fsS "http://127.0.0.1:${NEW_BACKEND_PORT}/health" >/dev/null 2>&1; then
    echo "  后端 OK"
    break
  fi
done

echo ""
echo "=============================================="
echo "  部署完成！"
echo "=============================================="
echo ""
echo "  访问地址:  http://<服务器IP>:${NEW_BACKEND_PORT}"
echo "  Health:    http://<服务器IP>:${NEW_BACKEND_PORT}/health"
echo "  日志文件:  ${LOGFILE}"
echo ""
echo "  旧项目不受影响，仍然运行在原有端口。"
echo ""
echo "  停止新项目:"
echo "    kill \$(lsof -ti :${NEW_BACKEND_PORT})"
echo ""
echo "  删除新数据库（如需重置）:"
echo "    docker rm -f ${NEW_PG_CONTAINER} ${NEW_REDIS_CONTAINER}"
echo ""
