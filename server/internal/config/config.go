package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	Addr         string // HTTP 监听地址
	DatabaseURL  string
	Password     string // 单租户密码门
	MCPToken     string // MCP 服务专用 token（空 = /mcp 端点禁用）
	GitHubToken  string
	AgentImage   string // agent 容器镜像
	AgentCmd     string // agent 命令（可替换：claude / pi / ...）
	RepoWorkRoot string // workdir 根目录
	TestCmd      string // unit_test 命令（本地模式）

	// Multica 接入（空 = 未接入）。三项均不内置默认值：ServerURL 尤其不能回落
	// 到云端 api.multica.ai（本机默认 profile 指向云端是已实证的坑）——缺失交给
	// multica.New 显式报错，宁可不启不用错误地址。
	MulticaServerURL   string
	MulticaToken       string
	MulticaWorkspaceID string
	// MulticaProjectID 固定 project（FR-2：派发创建的 Multica 父 issue 固定
	// 归属的项目）。空 = 不启用固定项目语义。
	MulticaProjectID string
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
	return Config{
		Addr:         addr,
		DatabaseURL:  dbURL,
		Password:     os.Getenv("INFERA_PASSWORD"),
		MCPToken:     os.Getenv("INFERA_MCP_TOKEN"),
		GitHubToken:  os.Getenv("GITHUB_TOKEN"),
		AgentImage:   getenv("AGENT_IMAGE", "infera-agent"),
		AgentCmd:     getenv("AGENT_CMD", "claude"),
		RepoWorkRoot: getenv("REPO_WORK_ROOT", "/tmp/infera-workdirs"),
		TestCmd:      getenv("TEST_CMD", "true"),

		MulticaServerURL:   os.Getenv("MULTICA_SERVER_URL"),
		MulticaToken:       os.Getenv("MULTICA_TOKEN"),
		MulticaWorkspaceID: os.Getenv("MULTICA_WORKSPACE_ID"),
		MulticaProjectID:   os.Getenv("MULTICA_PROJECT_ID"),
	}, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
