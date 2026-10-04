#!/usr/bin/env bash
# Собирает готовые архивы для релиза в папку dist/
# Использование: bash scripts/build-release.sh v0.1.0
set -euo pipefail
VER="${1:-dev}"
cd "$(dirname "$0")/.."
rm -rf dist build && mkdir -p dist build
go mod tidy

# --- полная сборка: клиент + сервер, с примерами и документацией ---
full() { # goos goarch label [env...]
  local goos=$1 goarch=$2 label=$3; shift 3
  local extra="$*" ext=""
  [ "$goos" = windows ] && ext=".exe"
  local name="voess2-${VER}-${label}"
  local dir="build/${name}"
  mkdir -p "$dir"
  env GOOS="$goos" GOARCH="$goarch" $extra CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -o "$dir/voess2${ext}" .
  cp -r README.md OPENWRT.md LICENSE examples "$dir/"
  cp examples/client.json "$dir/client.json"
  cp examples/server.json "$dir/server.json"
  if [ "$goos" = windows ]; then
    printf '@echo off\r\nvoess2.exe client -c client.json\r\npause\r\n' > "$dir/start-client.bat"
    (cd build && zip -qr "../dist/${name}.zip" "$name")
  else
    tar -C build -czf "dist/${name}.tar.gz" "$name"
  fi
}

# --- серверная сборка для роутеров (меньше размер) ---
router() { # label goarch [env...]
  local label=$1 goarch=$2; shift 2
  local extra="$*"
  local name="voess2-router-${VER}-${label}"
  local dir="build/${name}"
  mkdir -p "$dir"
  env GOOS=linux GOARCH="$goarch" $extra CGO_ENABLED=0 \
    go build -tags server -trimpath -ldflags="-s -w" -o "$dir/voess2" .
  cp examples/server-router.json examples/voess2.init scripts/install-openwrt.sh OPENWRT.md LICENSE "$dir/"
  tar -C build -czf "dist/${name}.tar.gz" "$name"
}

full linux   amd64 linux-amd64
full linux   arm64 linux-arm64
full linux   arm   linux-armv7 GOARM=7
full windows amd64 windows-amd64
full windows arm64 windows-arm64
full darwin  amd64 macos-amd64
full darwin  arm64 macos-arm64

router mipsle mipsle GOMIPS=softfloat
router mips   mips   GOMIPS=softfloat
router arm64  arm64
router armv7  arm    GOARM=7
router amd64  amd64

cp scripts/install-linux.sh dist/install-linux.sh
(cd dist && sha256sum * > SHA256SUMS.txt)
ls -lh dist
