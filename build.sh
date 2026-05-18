#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# ============================================================
# 配置区
# ============================================================
BINARY_NAME="llm-proxy"
MAIN_PATH="./cmd/llm-proxy"
GIT_COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X main.Version=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}"

# ============================================================
# 模式选择
# ============================================================
TARGET="${1:-native}"

case "$TARGET" in
  native)
    echo ">>> 编译本机二进制..."
    mkdir -p bin
    go build -ldflags="$LDFLAGS" -o "./bin/${BINARY_NAME}" "$MAIN_PATH"
    echo ">>> 完成: ./bin/${BINARY_NAME}"
    ;;
  linux)
    echo ">>> 交叉编译 Linux amd64..."
    mkdir -p bin
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "./bin/${BINARY_NAME}-linux-amd64" "$MAIN_PATH"
    echo ">>> 完成: ./bin/${BINARY_NAME}-linux-amd64"
    ;;
  all)
    echo ">>> 编译全部平台..."
    "$0" native
    "$0" linux
    ;;
  clean)
    echo ">>> 清理 bin/..."
    rm -rf bin/
    echo ">>> 完成"
    ;;
  *)
    echo "用法: $0 [native|linux|all|clean]"
    echo "  native — 编译本机二进制（默认）"
    echo "  linux  — 交叉编译 Linux amd64"
    echo "  all    — 编译全部平台"
    echo "  clean  — 清理构建产物"
    exit 1
    ;;
esac
