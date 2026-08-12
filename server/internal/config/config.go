package config

import "os"

type Config struct {
	DatabaseURL string
	Port        string
}

func Load() Config {
	return Config{
		DatabaseURL: getenv("DATABASE_URL", "postgres://infera:infera@localhost:5433/infera?sslmode=disable"),
		Port:        getenv("PORT", "8080"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
