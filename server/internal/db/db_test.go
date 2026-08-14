package db

import (
	"os"
	"testing"
)

func TestMigrate(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	if err := Migrate(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 幂等
	if err := Migrate(url); err != nil {
		t.Fatalf("migrate twice: %v", err)
	}
}
