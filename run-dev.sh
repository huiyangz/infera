#!/usr/bin/env bash
# 启动 infera 本地开发：后端(:8080) + 前端(:3000)。
# 前置：postgres 已起（docker compose up -d）、迁移已跑（已是 v6）、infera-agent 镜像已构建。
# 用法：cp .env.example .env 填密钥 → ./run-dev.sh
set -euo pipefail
cd "$(dirname "$0")"

if [ ! -f .env ]; then
  echo "缺少 .env：请先 cp .env.example .env 并填入密钥" >&2
  exit 1
fi
set -a; source .env; set +a

( cd server && exec go run ./cmd/server ) &
BACK_PID=$!
( cd apps/web && exec npm run dev ) &
WEB_PID=$!

trap 'kill $BACK_PID $WEB_PID 2>/dev/null || true' EXIT INT TERM
echo "► 后端 http://localhost:8080  前端 http://localhost:3000  （Ctrl-C 退出）"
wait
