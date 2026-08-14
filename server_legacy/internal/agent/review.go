package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ReviewDecision 是 Reviewer Agent 的结构化审核结论。
type ReviewDecision struct {
	Decision string   `json:"decision"` // "approve" | "reject"
	Reasons  []string `json:"reasons"`
}

var jsonObjectRe = regexp.MustCompile(`(?s)\{.*\}`)

// ParseReview 从 Reviewer Agent 的输出解析 decision。
// 支持纯 JSON，也支持 JSON 被文字包裹的情况。
func ParseReview(output string) (ReviewDecision, error) {
	var d ReviewDecision
	out := strings.TrimSpace(output)
	if err := json.Unmarshal([]byte(out), &d); err == nil {
		return d, nil
	}
	// 抽取第一个 JSON 对象
	if m := jsonObjectRe.FindString(out); m != "" {
		if err := json.Unmarshal([]byte(m), &d); err == nil {
			return d, nil
		}
	}
	return d, fmt.Errorf("cannot parse review decision from: %s", output)
}
