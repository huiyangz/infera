package testrunner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFakeRunnerPassAndFail(t *testing.T) {
	pass := NewFakeRunner(true)
	r, err := pass.Run(context.Background(), "/work")
	assert.NoError(t, err)
	assert.True(t, r.Pass)

	fail := NewFakeRunner(false)
	r, err = fail.Run(context.Background(), "/work")
	assert.NoError(t, err)
	assert.False(t, r.Pass)
	assert.NotEmpty(t, r.Detail)
}
