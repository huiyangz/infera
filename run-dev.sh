#!/usr/bin/env bash
# 启动 infera 本地开发：后端(:8080) + 前端(vite :5173，/api 代理到 8080)。
# 前置：postgres 已起（docker compose up -d）；迁移在启动时自动执行（v1 起）。
# 需求流转（发起需求/闸门卡/合并策略）：.env 配齐 MULTICA_* 六键 + GITHUB_TOKEN
# 即装配并启动闸门轮询（缺省未接入，需求 API 503）；配置说明见 .env.example
# 与 docs/requirements-flow.md。
# agent 默认本地进程模式；AGENT_BACKEND=docker 切容器。换 agent（如 pi）改 AGENT_CMD。
# 前端需要 pnpm；npm 不可用（依赖装在 pnpm store）。
set -euo pipefail
cd "$(dirname "$0")"

if [ ! -f .env ]; then
  echo "缺少 .env：请先 cp .env.example .env 并填入密钥" >&2
  exit 1
fi
set -a; source .env; set +a

( cd server && exec go run ./cmd/infera ) &
BACK_PID=$!
( cd apps/web && exec pnpm dev ) &
WEB_PID=$!

trap 'kill $BACK_PID $WEB_PID 2>/dev/null || true' EXIT INT TERM
echo "► 后端 http://localhost:8080  前端 http://localhost:5173  （Ctrl-C 退出）"
wait
