# auth（认证）

## Purpose

管理登录与会话：口令登录、会话建立与校验、API/MCP 两条通道的鉴权要求与登录限速。单租户单密码（`INFERA_PASSWORD`），不引入用户/角色/多租户体系——访问控制即「持有有效会话即可访问全部业务资源」，无 per-project ACL；浏览器通道用 HttpOnly cookie 会话，MCP 通道用独立静态 Bearer token，两种凭证解耦、可独立轮换。（REST 面：`server/internal/api/auth.go`、`router.go`；MCP 面：`server/internal/mcp/server.go`；web 端：`features/auth`。）

## Requirement: 口令登录与会话建立

`POST /api/login` SHALL 以请求体 `{password}` 校验口令（与 `INFERA_PASSWORD` 常数时间比较，防时序侧信道）：正确 SHALL 建立服务端会话并以 HttpOnly cookie `infera_session` 下发（`SameSite=Lax`，`Secure` 属性按 HTTPS 部署开启，会话 TTL 7 天）；错误 SHALL 返回 401。会话保存于服务端内存（实现现状：重启即全部失效，需重新登录）。

#### Scenario: 登录成功建会话

- **WHEN** 以正确口令调用 `POST /api/login`
- **THEN** 返回 200 `{logged_in:true}`，并 `Set-Cookie`：HttpOnly、有效期 7 天

#### Scenario: 密码错误拒绝

- **WHEN** 以错误口令调用 `POST /api/login`
- **THEN** 返回 401，错误码 `unauthorized`

#### Scenario: 会话过期失效

- **WHEN** 会话超过 7 天 TTL 后携带原 cookie 访问业务端点
- **THEN** 按未登录处理，返回 401

## Requirement: 会话自省与登出

`GET /api/me`（公开端点）SHALL 返回 `{logged_in: bool}` 供前端探测登录态，未登录时 SHALL 返回 200 而非 401；`POST /api/logout` SHALL 撤销服务端会话并清除 cookie。

#### Scenario: 会话自省

- **WHEN** 已登录调用 `GET /api/me`
- **THEN** 返回 `{logged_in:true}`；未登录时返回 `{logged_in:false}`

#### Scenario: 登出撤销会话

- **WHEN** 已登录用户调用 `POST /api/logout`
- **THEN** 服务端会话被撤销、cookie 被清除；此后原 cookie 访问业务端点返回 401

## Requirement: API 通道鉴权要求

除公开端点（`/api/health`、`/api/login`、`/api/logout`、`/api/me`）外，全部 `/api/*` 业务端点与 `/ws` 事件流 SHALL 要求有效会话；未认证请求 SHALL 返回 401 并携带稳定机器可读错误码 `unauthorized`（客户端按错误码分支，不解析文案）。

#### Scenario: 未登录访问业务端点

- **WHEN** 无有效会话 cookie 调用 `GET /api/projects`
- **THEN** 返回 401，响应体含 `code: "unauthorized"`

#### Scenario: 健康检查公开

- **WHEN** 未登录调用 `GET /api/health`
- **THEN** 返回 200，不触发鉴权

#### Scenario: 事件流同守卫

- **WHEN** 未登录向 `/ws` 发起升级请求
- **THEN** 连接被拒绝，不得订阅任何交付的事件流

## Requirement: 登录限速（防在线爆破）

系统 SHALL 按来源 IP（取 `RemoteAddr` 主机部分，SHALL NOT 信任可伪造的 `X-Forwarded-For`）对登录失败限速：同一 IP 连续失败达 5 次即锁定 1 分钟，锁定期间的登录尝试 SHALL 返回 429（错误码 `rate_limited`）；每次失败响应 SHALL 施加延迟以拖慢爆破；成功登录 SHALL 清零该 IP 的失败计数。

#### Scenario: 连续失败锁定

- **WHEN** 同一 IP 连续第 5 次输错口令后再次尝试登录
- **THEN** 返回 429（`rate_limited`），提示稍后再试

#### Scenario: 锁定自然过期

- **WHEN** 锁定窗口（1 分钟）过后该 IP 再次登录
- **THEN** 锁与失败计数作废，可正常重试

#### Scenario: 成功登录清零计数

- **WHEN** 某 IP 偶发输错口令后登录成功
- **THEN** 该 IP 的失败计数清零，偶发错误不累积到锁定

## Requirement: MCP 通道鉴权（独立静态 token）

`/mcp` 端点 SHALL 用专用静态 Bearer token（`INFERA_MCP_TOKEN`，常数时间比较）鉴权，SHALL NOT 复用登录 cookie 会话；部署未设置 token 时整个端点 SHALL 以 503 禁用；token 缺失或错误 SHALL 返回 401 并带 `WWW-Authenticate: Bearer`。

#### Scenario: 未启用即禁用

- **WHEN** 部署未设置 `INFERA_MCP_TOKEN` 时请求 `/mcp`
- **THEN** 返回 503，端点整体禁用，不暴露攻击面

#### Scenario: 无凭证拒绝

- **WHEN** 不带或带错误的 `Authorization: Bearer` 头调用 `/mcp`
- **THEN** 返回 401，响应携带 `WWW-Authenticate: Bearer`

#### Scenario: 双凭证解耦

- **WHEN** 轮换 `INFERA_MCP_TOKEN` 或重置登录会话
- **THEN** 另一通道的凭证不受影响，可独立轮换

## Requirement: 前端会话过期与登录回跳

认证区内请求收到 401 时，前端 SHALL 将用户带回 `/sign-in` 并携带当前位置（`redirect` 查询参数）；在 `/sign-in` 登录成功后 SHALL 跳回该目标（无目标则回首页）；查询类请求的 401 SHALL 提示会话已过期。

#### Scenario: 会话过期回跳

- **WHEN** 认证区内某查询请求收到 401
- **THEN** 提示「登录已过期」并跳转 `/sign-in?redirect=<当前地址>`

#### Scenario: 登录后跳回原目标

- **WHEN** 用户在 `/sign-in?redirect=<目标>` 完成登录
- **THEN** 跳转回 `<目标>`，而非固定首页
