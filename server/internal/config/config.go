package config

import "os"

type Config struct {
	DatabaseURL    string
	Port           string
	GitHubToken    string // PAT
	AgentImage     string // infera-agent
	RepoWorkRoot   string // 本地 clone 根目录，如 /tmp/infera-repos
	InferaPassword string // 单用户登录密码
}

func Load() Config {
	return Config{
		DatabaseURL:    getenv("DATABASE_URL", "postgres://infera:infera@localhost:5433/infera?sslmode=disable"),
		Port:           getenv("PORT", "8080"),
		GitHubToken:    getenv("GITHUB_TOKEN", ""),
		AgentImage:     getenv("AGENT_IMAGE", "infera-agent"),
		RepoWorkRoot:   getenv("REPO_WORK_ROOT", "/tmp/infera-repos"),
		InferaPassword: getenv("INFERA_PASSWORD", ""),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
