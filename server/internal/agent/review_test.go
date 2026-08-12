package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseReviewApprove(t *testing.T) {
	d, err := ParseReview(`{"decision":"approve","reasons":["ok"]}`)
	assert.NoError(t, err)
	assert.Equal(t, "approve", d.Decision)
}

func TestParseReviewReject(t *testing.T) {
	d, err := ParseReview(`{"decision":"reject","reasons":["缺少错误处理"]}`)
	assert.NoError(t, err)
	assert.Equal(t, "reject", d.Decision)
	assert.Len(t, d.Reasons, 1)
}

func TestParseReviewFromWrappedText(t *testing.T) {
	// Agent 偶尔在 JSON 外裹一段话，要能抽出 JSON
	d, err := ParseReview("我的审核：\n{\"decision\":\"reject\",\"reasons\":[\"x\"]}\n完")
	assert.NoError(t, err)
	assert.Equal(t, "reject", d.Decision)
}
