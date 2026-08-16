package config

import (
	"os"
	"strings"
)

type Config struct {
	Addr         string // HTTP 监听地址
	DatabaseURL  string
	Password     string // 单租户密码门
	GitHubToken  string
	AgentImage   string // agent 容器镜像
	AgentCmd     string // agent 命令（可替换：claude / pi / ...）
	RepoWorkRoot string // workdir 根目录
	TestCmd      string // unit_test 命令（本地模式）
}

func Load() Config {
	addr := getenv("PORT", ":8080")
	if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr // 容错：PORT=8080 与 PORT=:8080 等价
	}
	return Config{
		Addr:         addr,
		DatabaseURL:  getenv("DATABASE_URL", "postgres://infera:infera@localhost:5433/infera_v2?sslmode=disable"),
		Password:     os.Getenv("INFERA_PASSWORD"),
		GitHubToken:  os.Getenv("GITHUB_TOKEN"),
		AgentImage:   getenv("AGENT_IMAGE", "infera-agent"),
		AgentCmd:     getenv("AGENT_CMD", "claude"),
		RepoWorkRoot: getenv("REPO_WORK_ROOT", "/tmp/infera-workdirs"),
		TestCmd:      getenv("TEST_CMD", "true"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
