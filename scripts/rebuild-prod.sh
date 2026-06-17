#!/usr/bin/env bash
set -e

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== Sub2API Rebuild ==="

# Build frontend
echo "Building frontend..."
cd "$ROOT_DIR/frontend"
if [ ! -d node_modules ]; then
  pnpm install --no-frozen-lockfile
fi
./node_modules/.bin/vite build

# Build backend (embed frontend)
echo "Building backend..."
cd "$ROOT_DIR/backend"
export GOPROXY=https://goproxy.cn,direct
go build -tags embed -o sub2api ./cmd/server

echo "Build complete. Run restart-prod.sh to apply."
