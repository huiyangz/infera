package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoleForStage(t *testing.T) {
	cases := []struct {
		stage string
		role  Role
		ok    bool
	}{
		{"spec", RoleSpec, true},
		{"test_gen", RoleTest, true},
		{"code_gen", RoleCoder, true},
		{"code_review", RoleReviewer, true},
		{"intake", "", false},        // 人，无 agent
		{"spec_approval", "", false}, // 人 gate
		{"unit_test", "", false},     // 系统
		{"deploy", "", false},        // 系统
	}
	for _, c := range cases {
		role, ok := RoleForStage(c.stage)
		assert.Equal(t, c.ok, ok, "stage %q", c.stage)
		assert.Equal(t, c.role, role, "stage %q", c.stage)
	}
}
