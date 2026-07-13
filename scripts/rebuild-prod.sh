#!/usr/bin/env bash
set -e

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=ensure-go-toolchain.sh
source "${ROOT_DIR}/scripts/ensure-go-toolchain.sh"

# 检查内存/swap，如果内存不足自动提示
TOTAL_MEM=$(free -m | awk '/^Mem:/{print $2}')
TOTAL_SWAP=$(free -m | awk '/^Swap:/{print $2}')
if [ "$TOTAL_MEM" -lt 1024 ] && [ "$TOTAL_SWAP" -lt 512 ]; then
  echo "WARNING: Low memory (${TOTAL_MEM}MB) with insufficient swap (${TOTAL_SWAP}MB)"
  echo "Run 'bash scripts/setup-swap.sh' first to prevent OOM"
fi

echo "=== Sub2API Rebuild ==="

# Build frontend
echo "Building frontend..."
cd "$ROOT_DIR/frontend"
if [ ! -d node_modules ]; then
  pnpm install --no-frozen-lockfile
fi
export NODE_OPTIONS="--max-old-space-size=2048"
./node_modules/.bin/vite build

# Build backend (embed frontend)
echo "Building backend..."
cd "$ROOT_DIR/backend"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export GOSUMDB="${GOSUMDB:-sum.golang.google.cn}"
ensure_go_toolchain "$ROOT_DIR"
go build -tags embed -o sub2api ./cmd/server

echo "Build complete. Run restart-prod.sh to apply."
