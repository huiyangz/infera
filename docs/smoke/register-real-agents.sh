#!/bin/sh
# R11 真实 agent 接入注册脚本（幂等，可复现）。
#
# 三产品按真实接口形态注册（cli / http / docker），再按角色绑定到流水线节点：
#   infCode         → spec、code_gen（生成/设计/任务类；design/tasks 平台暂不可绑定，见下）
#   testAgent       → test_gen
#   codeReviewAgent → code_review（门禁预审）+ spec_conformance + code_quality（R10 双道审查）
#
# 接入信息全部来自环境变量（模板见 r11-agents.env.example，编排者填写）。
# 未提供 → 显式失败占位（绝不产出可被误读为产品结果的产物），交付停在对应阶段 blocked。
#
# 绑定落点：默认改全局编排（PUT /api/pipeline）；设 SMOKE_PROJECT_ID 则只覆盖该项目
# （PUT /api/projects/:id/pipeline，推荐用于共享环境）。
#
# 退出码：0 = 注册+绑定全部成功（占位形态也算——占位是合法的待接入状态）；
#         1 = 平台侧操作失败（登录/注册/绑定被拒）。
#
# 用法：
#   set -a; source r11-agents.env; set +a
#   ./register-real-agents.sh

set -u

BASE="${INFERA_BASE_URL:-http://localhost:18087}"
PASS="${INFERA_PASSWORD:?INFERA_PASSWORD 未设置（冒烟环境登录密码）}"
PROJECT="${SMOKE_PROJECT_ID:-}"
COOKIE="$(mktemp)"
trap 'rm -f "$COOKIE"' EXIT

log() { printf '[register] %s\n' "$*" >&2; }
fail() { log "错误: $*"; exit 1; }

# login → cookie
code=$(curl -s -o /dev/null -w '%{http_code}' -c "$COOKIE" \
  -H 'Content-Type: application/json' -d "{\"password\":\"$PASS\"}" "$BASE/api/login")
[ "$code" = 200 ] || fail "登录失败（HTTP $code）"

# api METHOD PATH BODY → 成功时输出响应体（jq 解析过），失败时输出错误并 exit 1。
api() {
  method=$1 path=$2 body=$3
  resp=$(curl -s -w '\n%{http_code}' -b "$COOKIE" -X "$method" \
    -H 'Content-Type: application/json' ${body:+-d "$body"} "$BASE$path")
  code=$(printf '%s' "$resp" | tail -1)
  body=$(printf '%s' "$resp" | sed '$d')
  [ "$code" = 200 ] || { log "$method $path → HTTP $code: $body"; exit 1; }
  printf '%s' "$body"
}

# placeholder_body FORM PREFIX → 未配置时的显式失败占位 config（jq 构造）。
# cli 占位 exit 70（stderr 说明缺哪个变量）；http 占位指向 127.0.0.1:9（瞬断）；
# docker 占位镜像名不存在（拉取失败）。三类占位都不可能产出内容。
placeholder_body() {
  form=$1 prefix=$2 what=$3
  case $form in
    cli)
      jq -n --arg c "echo '[占位] $what 未配置（${prefix}_COMMAND 缺失）——拒绝执行以免伪造产物' >&2; exit 70" \
        '{runner:"cli",config:{command:["sh","-c",$c]}}' ;;
    http)
      jq -n '{runner:"http",config:{url:"http://127.0.0.1:9/agent"}}' ;;
    docker)
      jq -n '{runner:"docker",config:{image:"infera/placeholder-not-configured:latest"}}' ;;
    *) fail "未知 FORM: $form（应为 cli|http|docker）" ;;
  esac
}

# register NAME PREFIX WHAT → agent id（存在则 PATCH 更新，否则 POST 创建）。
# 返回值同时写全局 $LAST_AGENT_JSON 供证据采集。
LAST_AGENT_JSON=''
register() {
  name=$1 prefix=$2 what=$3
  form=$(eval "echo \${${prefix}_FORM:-cli}")
  case $form in
    cli)      cmd=$(eval "echo \${${prefix}_COMMAND:-}")
              [ -n "$cmd" ] && body=$(jq -n --arg c "$cmd" '{runner:"cli",config:{command:["sh","-c",$c]}}') ;;
    http)     url=$(eval "echo \${${prefix}_URL:-}")
              [ -n "$url" ] && body=$(jq -n --arg u "$url" '{runner:"http",config:{url:$u}}') ;;
    docker)   img=$(eval "echo \${${prefix}_IMAGE:-}")
              dcmd=$(eval "echo \${${prefix}_DOCKER_CMD:-}")
              if [ -n "$img" ]; then
                body=$(jq -n --arg i "$img" --arg c "$dcmd" \
                  '{runner:"docker",config:{image:$i,command:(if $c=="" then [] else [$c] end)}}')
              fi ;;
    *)        fail "$what: 未知 FORM $form" ;;
  esac
  # 没给真值 → 占位（记录形态但显式失败）
  placeholder=0
  [ -z "${body:-}" ] && { placeholder=1; body=$(placeholder_body "$form" "$prefix" "$what"); }
  existing=$(curl -s -b "$COOKIE" "$BASE/api/agents" | jq -r --arg n "$name" '.[] | select(.name==$n) | .id')
  full=$(jq -n --arg n "$name" --argjson b "$body" '{name:$n} + $b')
  if [ -n "$existing" ]; then
    out=$(api PATCH "/api/agents/$existing" "$full")
    log "$name: 已存在，PATCH 更新（form=$form placeholder=$placeholder）"
  else
    out=$(api POST /api/agents "$full")
    log "$name: 注册（form=$form placeholder=$placeholder）"
  fi
  LAST_AGENT_JSON=$(printf '%s' "$out" | jq -c '{id,name,runner,config}')
  printf '%s' "$out" | jq -r '.id'
}

log "目标: $BASE（绑定落点: ${PROJECT:+项目 $PROJECT}${PROJECT:-全局默认}）"

ID_INF=$(register infCode INFCODE 'infCode 接入信息')
ID_TST=$(register testAgent TESTAGENT 'testAgent 接入信息')
ID_REV=$(register codeReviewAgent CODEREVIEW 'codeReviewAgent 接入信息')
log "agents: infCode=$ID_INF testAgent=$ID_TST codeReviewAgent=$ID_REV"

# 绑定（可绑定节点全集，角色映射）：
#   生成/设计/任务类: spec+code_gen → infCode；test_gen → testAgent
#   审查类: code_review+spec_conformance+code_quality → codeReviewAgent
bindings=$(jq -n --arg inf "$ID_INF" --arg tst "$ID_TST" --arg rev "$ID_REV" \
  '{bindings:{
    spec:$inf, code_gen:$inf,
    test_gen:$tst,
    code_review:$rev, spec_conformance:$rev, code_quality:$rev}}')

if [ -n "$PROJECT" ]; then
  api PUT "/api/projects/$PROJECT/pipeline" "$bindings" >/dev/null
else
  api PUT /api/pipeline "$bindings" >/dev/null
fi
log "绑定完成（6 节点：spec/code_gen→infCode，test_gen→testAgent，code_review/spec_conformance/code_quality→codeReviewAgent）"

# design/tasks 目前不在平台可绑定节点清单里（冒烟报告问题 P1）。探测性 PUT 用
# 「全量 6 节点 + 待探测节点」：校验先于落盘（SaveBindings 拒绝未知节点时不改任何
# 绑定），无副作用；返回写明原因——400 不可绑定 / 200 已支持（此时绑定真生效）。
probe_path=$([ -n "$PROJECT" ] && echo "/api/projects/$PROJECT/pipeline" || echo /api/pipeline)
for node in design tasks; do
  probe=$(curl -s -w '\n%{http_code}' -b "$COOKIE" -X PUT -H 'Content-Type: application/json' \
    -d "$(printf '%s' "$bindings" | jq --arg id "$ID_INF" --arg n "$node" \
      '.bindings + {$n:$id} | {bindings:.}')" \
    "$BASE$probe_path")
  pcode=$(printf '%s' "$probe" | tail -1)
  log "探测绑定 $node → HTTP $pcode: $(printf '%s' "$probe" | sed '$d' | jq -r .error 2>/dev/null)"
  log "  （400=平台暂不可绑定 design/tasks；200=已支持，绑定生效）"
done

# 证据输出：生效编排
if [ -n "$PROJECT" ]; then
  eff=$(api GET "/api/projects/$PROJECT/pipeline" '')
else
  eff=$(api GET /api/pipeline '')
fi
printf '\n[register] 生效编排（%s）：\n%s\n' "${PROJECT:+项目}${PROJECT:-全局}" \
  "$(printf '%s' "$eff" | jq -c '.effective // {bindings:.bindings,agents:[.agents[].name]}')"
log "完成。"
