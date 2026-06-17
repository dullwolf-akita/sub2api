# Sub2API 生产环境部署

## 概述

在新服务器上部署最新版 Sub2API，与旧版本并行运行，数据从旧数据库迁移。

## 服务器要求

| 依赖 | 版本要求 |
|------|----------|
| Docker | 24+ |
| Go | 1.25+ |
| Node.js | 20+ |
| pnpm | 9+ |
| curl | - |

## 快速部署

```bash
# 下载脚本
curl -sSL https://raw.githubusercontent.com/dullwolf-akita/sub2api/main/scripts/deploy-prod.sh -o deploy-prod.sh
chmod +x deploy-prod.sh

# 运行（使用默认参数）
./deploy-prod.sh

# 或自定义端口
NEW_BACKEND_PORT=18080 NEW_FRONTEND_PORT=18081 ./deploy-prod.sh
```

## 手动分步部署

### 第一步：克隆项目

```bash
cd /opt
git clone --depth 1 https://github.com/dullwolf-akita/sub2api.git sub2api-latest
```

### 第二步：创建新数据库容器

```bash
# PostgreSQL（端口 5434，不与旧项目 5433 冲突）
docker run -d --name sub2api-latest-postgres \
  -e POSTGRES_USER=sub2api \
  -e POSTGRES_PASSWORD=sub2api_dev_password \
  -e POSTGRES_DB=sub2api \
  -p 127.0.0.1:5434:5432 \
  postgres:18-alpine

# Redis（端口 6381，不与旧项目 6380 冲突）
docker run -d --name sub2api-latest-redis \
  -p 127.0.0.1:6381:6379 \
  redis:8-alpine
```

### 第三步：迁移数据库

```bash
# 从旧容器导出
docker exec sub2api-postgres pg_dump -U sub2api -d sub2api \
  --format=custom --compress=9 > /tmp/sub2api_old.dump

# 导入到新容器
docker exec -i sub2api-latest-postgres pg_restore -U sub2api -d sub2api \
  < /tmp/sub2api_old.dump

# 验证
docker exec sub2api-latest-postgres psql -U sub2api -d sub2api \
  -c "SELECT count(*) as users, count(DISTINCT email) as emails FROM users;"
```

### 第四步：启动新项目后端

```bash
cd /opt/sub2api-latest/backend

AUTO_SETUP=true \
  SERVER_HOST=0.0.0.0 \
  SERVER_PORT=18080 \
  DATABASE_HOST=127.0.0.1 \
  DATABASE_PORT=5434 \
  DATABASE_USER=sub2api \
  DATABASE_PASSWORD=sub2api_dev_password \
  DATABASE_DBNAME=sub2api \
  DATABASE_SSLMODE=disable \
  REDIS_HOST=127.0.0.1 \
  REDIS_PORT=6381 \
  nohup go run ./cmd/server > /tmp/sub2api-latest.log 2>&1 &
```

### 第五步：启动前端

```bash
cd /opt/sub2api-latest/frontend
pnpm install

VITE_DEV_PORT=18081 \
  VITE_DEV_PROXY_TARGET=http://127.0.0.1:18080 \
  nohup ./node_modules/.bin/vite --host 0.0.0.0 --port 18081 > /tmp/sub2api-latest-frontend.log 2>&1 &
```

## 验证

```bash
# 后端健康检查
curl http://127.0.0.1:18080/health

# 前端
curl -I http://127.0.0.1:18081
```

## 停止新项目

```bash
pkill -f "go run.*cmd/server"    # 停止后端
pkill -f "vite.*18081"           # 停止前端
docker rm -f sub2api-latest-postgres sub2api-latest-redis  # 删除新容器
```

## 端口规划参考

| 组件 | 旧项目 | 新项目 | 说明 |
|------|--------|--------|------|
| PostgreSQL | 5433 | 5434 | 不同端口避免冲突 |
| Redis | 6380 | 6381 | 不同端口避免冲突 |
| 后端 API | 8080 | 18080 | 可自定义 |
| 前端页面 | - | 18081 | 可自定义 |
