#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# ============================================================
# 配置区 — 按需修改
# ============================================================
ENCRYPTION_KEY="${ENCRYPTION_KEY:-01234567890123456789012345678901}"
ADMIN_TOKEN="${ADMIN_TOKEN:-my-secret-token}"
LOG_LEVEL="${LOG_LEVEL:-debug}"
PORT="${PORT:-9002}"

export ENCRYPTION_KEY ADMIN_TOKEN LOG_LEVEL PORT

# ============================================================
# 模式选择
# ============================================================
MODE="${1:-dev}"

case "$MODE" in
  dev)
    echo ">>> 开发模式启动 (端口 $PORT)"
    LOG_LEVEL=debug go run ./cmd/llm-proxy
    ;;
  build)
    echo ">>> 编译..."
    make build
    echo ">>> 启动 (端口 $PORT)"
    ./bin/llm-proxy
    ;;
  docker)
    echo ">>> Docker 开发模式启动 (端口 $PORT)"
    docker compose up -d
    echo ">>> 查看日志: docker compose logs -f"
    ;;
  *)
    echo "用法: $0 [dev|build|docker]"
    echo "  dev    — go run 直接运行（默认）"
    echo "  build  — 先编译再运行"
    echo "  docker — docker compose 启动"
    exit 1
    ;;
esac
