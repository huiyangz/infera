package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFakeBackendReturnsCannedOutput(t *testing.T) {
	b := NewFakeBackend()
	b.Stub("spec", "这是 spec 产出")

	res, err := b.Execute(context.Background(), ExecInput{
		Role:   "spec",
		Prompt: "写 spec",
	})
	assert.NoError(t, err)
	assert.Equal(t, "这是 spec 产出", res.Output)
	assert.NotEmpty(t, res.SessionID)
}
