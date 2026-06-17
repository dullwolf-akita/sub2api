#!/usr/bin/env bash
set -euo pipefail

# 在小内存服务器上创建 swap 文件，防止 OOM
# 用法: sudo bash setup-swap.sh [大小]
# 默认大小 = 2G

SIZE="${1:-2G}"
SWAPFILE="/swapfile"

if swapon --show | grep -q "$SWAPFILE"; then
  echo "Swap already active on $SWAPFILE ($(swapon --show=SIZE --noheadings | head -1))"
  exit 0
fi

echo "Creating ${SIZE} swap file at $SWAPFILE..."
fallocate -l "$SIZE" "$SWAPFILE"
chmod 600 "$SWAPFILE"
mkswap "$SWAPFILE" >/dev/null
swapon "$SWAPFILE"

grep -q "$SWAPFILE" /etc/fstab 2>/dev/null || echo "$SWAPFILE none swap sw 0 0" >> /etc/fstab

echo "Done: $(free -m | grep Swap)"
