#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root/local"
if [ "$(go env CGO_ENABLED)" = 1 ] && command -v "$(go env CC)" >/dev/null 2>&1; then
 go test -race ./...
else
 echo 'SKIP: race 检测需要启用 cgo 和 C 编译器；本次仅跑普通 Go 测试，未验证 race。'
 go test ./...
fi
go vet ./...
cd "$root"
node extension/self-test.cjs
node extension/network-test.cjs
node --check local/web/app.js
node --check extension/popup.js
if [ "${1:-}" = --release ]; then
 python3 scripts/check_artifacts.py
else
 echo '源码验证完成；发行包请构建后运行 scripts/verify.sh --release。'
fi
