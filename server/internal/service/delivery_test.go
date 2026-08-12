package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/dbtest"
)

func TestCreateDelivery(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries")

	svc := New(pool)
	d, err := svc.Create(context.Background(), CreateInput{
		Title:       "忘记密码功能",
		Description: "登录页加重置流程",
		RepoURL:     "https://github.com/acme/web",
	})
	assert.NoError(t, err)
	assert.Equal(t, "忘记密码功能", d.Title)
	assert.Equal(t, "intake", d.CurrentStage)
	assert.Equal(t, "active", string(d.Status))
}
