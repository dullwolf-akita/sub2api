#!/usr/bin/env bash
# 读取 backend/go.mod 的 go 版本；若本机 Go 不满足，从国内镜像安装到项目 .toolchain/
# 避免 go build 卡在 go: downloading goX.Y.Z (go.dev 国内很慢)

_go_version_tag() {
  "$1" version 2>/dev/null | awk '{print $3}'
}

_go_version_exact_match() {
  local go_bin="$1"
  local required="$2"
  [ "$(_go_version_tag "$go_bin")" = "go${required}" ]
}

ensure_go_toolchain() {
  local root_dir="${1:?project root required}"
  local go_mod="${root_dir}/backend/go.mod"
  local required=""

  if [ ! -f "$go_mod" ]; then
    echo "WARNING: go.mod not found at ${go_mod}, skip toolchain check"
    return 0
  fi

  required=$(awk '/^go / { print $2; exit }' "$go_mod")
  if [ -z "$required" ]; then
    return 0
  fi

  local install_dir="${root_dir}/.toolchain/go${required}"

  # 优先使用项目内已缓存的 toolchain，避免系统 go 版本接近但不满足 go.mod
  if [ -x "${install_dir}/bin/go" ] && _go_version_exact_match "${install_dir}/bin/go" "$required"; then
    export PATH="${install_dir}/bin:${PATH}"
    export GOTOOLCHAIN=local
    echo "Go toolchain OK (cached): $(_go_version_tag go)"
    return 0
  fi

  if command -v go >/dev/null 2>&1 && _go_version_exact_match go "$required"; then
    export GOTOOLCHAIN=local
    echo "Go toolchain OK: $(_go_version_tag go)"
    return 0
  fi

  local os arch tarball
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$arch" in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
  esac
  tarball="go${required}.${os}-${arch}.tar.gz"

  echo "Go ${required} required (current: $(_go_version_tag go 2>/dev/null || echo 'not found'))"

  local tmpdir archive
  tmpdir=$(mktemp -d)
  archive="${tmpdir}/${tarball}"

  local mirrors=(
    "https://mirrors.aliyun.com/golang/${tarball}"
    "https://golang.google.cn/dl/${tarball}"
    "https://go.dev/dl/${tarball}"
  )

  local url downloaded=0
  for url in "${mirrors[@]}"; do
    echo "Downloading Go ${required} from ${url} ..."
    if command -v curl >/dev/null 2>&1; then
      if curl -fsSL --connect-timeout 20 --retry 2 --retry-delay 3 --max-time 900 "$url" -o "$archive"; then
        downloaded=1
      fi
    elif command -v wget >/dev/null 2>&1; then
      if wget -q --timeout=30 -O "$archive" "$url"; then
        downloaded=1
      fi
    fi

    if [ "$downloaded" = 1 ] && gzip -t "$archive" 2>/dev/null; then
      echo "Download OK"
      break
    fi

    downloaded=0
    rm -f "$archive"
    echo "Mirror failed or tarball corrupt, trying next..."
  done

  if [ "$downloaded" != 1 ]; then
    rm -rf "$tmpdir"
    echo "ERROR: failed to download Go ${required} from all mirrors"
    return 1
  fi

  mkdir -p "${root_dir}/.toolchain"
  rm -rf "$install_dir"
  mkdir -p "$install_dir"
  if ! tar -C "$install_dir" --strip-components=1 -xzf "$archive"; then
    rm -rf "$tmpdir" "$install_dir"
    echo "ERROR: failed to extract Go ${required} tarball"
    return 1
  fi
  rm -rf "$tmpdir"

  if ! _go_version_exact_match "${install_dir}/bin/go" "$required"; then
    rm -rf "$install_dir"
    echo "ERROR: Go ${required} install verification failed"
    return 1
  fi

  export PATH="${install_dir}/bin:${PATH}"
  export GOTOOLCHAIN=local
  echo "Go toolchain installed: $(_go_version_tag go)"
}
