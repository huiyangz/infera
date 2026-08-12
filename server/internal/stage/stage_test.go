package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNextOfEachStage(t *testing.T) {
	cases := []struct{ from, want string }{
		{"intake", "spec"},
		{"spec", "spec_approval"},
		{"spec_approval", "test_gen"},
		{"test_gen", "code_gen"},
		{"code_gen", "unit_test"},
		{"unit_test", "code_review"},
		{"code_review", "deploy"},
	}
	for _, c := range cases {
		got, ok := Next(c.from)
		assert.True(t, ok, "stage %q should have a next", c.from)
		assert.Equal(t, c.want, got)
	}
}

func TestNextOfDeployIsEmpty(t *testing.T) {
	_, ok := Next("deploy")
	assert.False(t, ok, "deploy has no next")
}

func TestIsGate(t *testing.T) {
	assert.True(t, IsGate("spec_approval"))
	assert.True(t, IsGate("code_review"))
	assert.False(t, IsGate("code_gen"))
}
