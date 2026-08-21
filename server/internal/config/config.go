package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Addr         string // HTTP 监听地址
	DatabaseURL  string
	Password     string // 单租户密码门
	MCPToken     string // MCP 服务专用 token（空 = /mcp 端点禁用）
	GitHubToken  string
	GitHubAPIURL string // GitHub API 入口覆盖（空 = 官方 api.github.com；GH Enterprise 用）
	AgentImage   string // agent 容器镜像
	AgentCmd     string // agent 命令（可替换：claude / pi / ...）
	RepoWorkRoot string // workdir 根目录
	TestCmd      string // unit_test 命令（本地模式）

	// 需求流转（flow 契约，INFERA-11 T01）：闸门轮询间隔与派发目标项目。
	GatePollInterval time.Duration // 默认 30s，上限 60s（AC-3：状态变化 2 分钟内反映）
	MulticaProjectID string        // 派发目标 Multica 项目（reqservice 装配期必填）

	// 装配期定位（T07）：reqservice.Options 的派发指派与深链段。
	MulticaTechLeadAgentID string // 派发指派的 Tech Lead agent id（装配期必填）
	MulticaWorkspaceSlug   string // 深链工作区段，如 infera（装配期必填）

	// Multica 接入（空 = 未接入）。三项均不内置默认值：ServerURL 尤其不能回落
	// 到云端 api.multica.ai（本机默认 profile 指向云端是已实证的坑）——缺失交给
	// multica.New 显式报错，宁可不启不用错误地址。
	MulticaServerURL   string
	MulticaToken       string
	MulticaWorkspaceID string
}

// devDatabaseURL 开发回落值：docker-compose 的本地 postgres（127.0.0.1:5433）。
// 仅开发模式使用——生产（INFERA_ENV=production）必须显式设置 DATABASE_URL。
const devDatabaseURL = "postgres://infera:infera@localhost:5433/infera_v2?sslmode=disable"

// Load 装配配置。INFERA_ENV=production 时启用严格模式：缺失关键项直接报错
// （内置默认值连上错误的库比启动失败更危险）。
func Load() (Config, error) {
	addr := getenv("PORT", ":8080")
	if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr // 容错：PORT=8080 与 PORT=:8080 等价
	}
	production := os.Getenv("INFERA_ENV") == "production"
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		if production {
			return Config{}, errors.New("INFERA_ENV=production 要求显式设置 DATABASE_URL（不内置默认连接串）")
		}
		dbURL = devDatabaseURL
	}
	gatePoll, err := loadGatePollInterval()
	if err != nil {
		return Config{}, err
	}
	return Config{
		Addr:         addr,
		DatabaseURL:  dbURL,
		Password:     os.Getenv("INFERA_PASSWORD"),
		MCPToken:     os.Getenv("INFERA_MCP_TOKEN"),
		GitHubToken:  os.Getenv("GITHUB_TOKEN"),
		GitHubAPIURL: os.Getenv("GITHUB_API_URL"),
		AgentImage:   getenv("AGENT_IMAGE", "infera-agent"),
		AgentCmd:     getenv("AGENT_CMD", "claude"),
		RepoWorkRoot: getenv("REPO_WORK_ROOT", "/tmp/infera-workdirs"),
		TestCmd:      getenv("TEST_CMD", "true"),

		MulticaServerURL:   os.Getenv("MULTICA_SERVER_URL"),
		MulticaToken:       os.Getenv("MULTICA_TOKEN"),
		MulticaWorkspaceID: os.Getenv("MULTICA_WORKSPACE_ID"),

		GatePollInterval: gatePoll,
		MulticaProjectID: os.Getenv("MULTICA_PROJECT_ID"),

		MulticaTechLeadAgentID: os.Getenv("MULTICA_TECH_LEAD_AGENT_ID"),
		MulticaWorkspaceSlug:   os.Getenv("MULTICA_WORKSPACE_SLUG"),
	}, nil
}

// loadGatePollInterval 解析 GATE_POLL_INTERVAL：默认 30s；(0, 60s] 之外或
// 非法 duration 直接报错——超过 60s 的轮询间隔无法满足"状态变化 2 分钟内
// 反映"（AC-3），静默接受只会把违约推迟到运行期。
func loadGatePollInterval() (time.Duration, error) {
	raw := os.Getenv("GATE_POLL_INTERVAL")
	if raw == "" {
		return 30 * time.Second, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("GATE_POLL_INTERVAL %q 不是合法 duration: %w", raw, err)
	}
	if d <= 0 || d > 60*time.Second {
		return 0, fmt.Errorf("GATE_POLL_INTERVAL %s 超出 (0, 60s]", d)
	}
	return d, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
