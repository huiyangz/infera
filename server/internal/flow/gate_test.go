package flow

import (
	"testing"
	"time"
)

func comment(body string) CommentInput {
	return CommentInput{
		ID:         "c1",
		AuthorType: "agent",
		Body:       body,
		CreatedAt:  time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
	}
}

// TestParseCommentPrefixes：三类闸门的字面前缀识别。
//
// 协议（冻结）：`待批准：`/`需要决策：` 用全角冒号，`verdict:` 用 ASCII 冒号，
// 一律区分大小写、必须顶格（不做 trim、不认前缀出现在句中）。
// 半角/全角冒号互换、大小写变化等相近变体一律不命中——防的是 agent 格式
// 跑偏伪装成闸门；这类评论走兜底"有新动态"。
func TestParseCommentPrefixes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want GateKind
	}{
		// 命中
		{"审批卡", "待批准：INFERA-31 分批执行计划\n\n正文……", GateApproval},
		{"决策卡", "需要决策：依赖的 API 不稳定，重试还是跳过？", GateDecision},
		{"合并卡", "verdict: PASS\n\n行级评审摘要……", GateMerge},
		{"verdict 后无空格", "verdict:FAIL", GateMerge},
		// 干扰反例：冒号全半角互换 → 不命中，落兜底
		{"审批前缀半角冒号", "待批准: 计划正文", GateUpdate},
		{"决策前缀半角冒号", "需要决策: 怎么办", GateUpdate},
		{"verdict 全角冒号", "verdict：PASS", GateUpdate},
		// 干扰反例：大小写 / 前缀不在句首 / 句中包含
		{"verdict 大写 V", "Verdict: PASS", GateUpdate},
		{"前缀不在句首", "经过评审，verdict: PASS", GateUpdate},
		{"句中包含待批准", "这个方案亟待批准：请尽快", GateUpdate},
		{"前导空格不 trim", " 待批准：计划", GateUpdate},
		// 普通评论 → 兜底
		{"普通评论", "进度过半，接口已通", GateUpdate},
		{"空评论", "", GateUpdate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := ParseComment(comment(tc.body))
			if ev.Kind != tc.want {
				t.Fatalf("ParseComment(%q).Kind = %s, want %s", tc.body, ev.Kind, tc.want)
			}
		})
	}
}

// TestParseCommentCarries：事件携带溯源字段（评论 id / 作者 / 时间 / 全文）。
func TestParseCommentCarries(t *testing.T) {
	in := CommentInput{
		ID:         "c-42",
		AuthorType: "agent",
		AuthorID:   "agent-1",
		Body:       "待批准：计划正文",
		CreatedAt:  time.Date(2026, 8, 21, 13, 0, 0, 0, time.UTC),
	}
	ev := ParseComment(in)
	if ev.CommentID != "c-42" || ev.AuthorType != "agent" || ev.AuthorID != "agent-1" {
		t.Fatalf("事件溯源字段缺失: %+v", ev)
	}
	if ev.Body != "待批准：计划正文" || !ev.CreatedAt.Equal(in.CreatedAt) {
		t.Fatalf("事件正文/时间不符: %+v", ev)
	}
}

// TestExtractVerdict：从 verdict 评论正文提取 PASS / FAIL 结论词。
// 冻结：全词匹配（PASSING 不算）、大小写敏感、首个命中者为准
// （评论以 verdict: 顶格开头，结论词必然在补充说明之前）。
func TestExtractVerdict(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Verdict
	}{
		{"同行 PASS", "verdict: PASS", VerdictPass},
		{"同行 FAIL", "verdict: FAIL\n两处阻塞……", VerdictFail},
		{"中文标点邻接", "verdict: PASS。行级评审通过", VerdictPass},
		{"首个命中优先", "verdict: PASS（上一轮 FAIL 已修复）", VerdictPass},
		{"全词匹配", "verdict: PASSING", VerdictUnknown},
		{"小写不认", "verdict: pass", VerdictUnknown},
		{"无结论词", "verdict:\n详见时间线", VerdictUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractVerdict(tc.body); got != tc.want {
				t.Fatalf("ExtractVerdict(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

// TestParseCommentVerdict：只有合并事件带结论词；其余事件为 Unknown。
func TestParseCommentVerdict(t *testing.T) {
	if got := ParseComment(comment("verdict: PASS")).Verdict; got != VerdictPass {
		t.Fatalf("合并事件 Verdict = %q, want PASS", got)
	}
	if got := ParseComment(comment("待批准：计划")).Verdict; got != VerdictUnknown {
		t.Fatalf("审批事件 Verdict = %q, want Unknown", got)
	}
}

// TestExtractPRURL：从任意评论正文提取 github PR URL。
// 冻结：仅 https；owner/repo 限 GitHub 合法字符集；返回到 PR 号为止的
// 规范形（尾随路径/锚点剥掉）；多条取首条。
func TestExtractPRURL(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"裸 URL", "PR 已开：https://github.com/huiyangz/infera/pull/7 请评审", "https://github.com/huiyangz/infera/pull/7"},
		{"Markdown 链接", "见 [PR](https://github.com/foo/bar/pull/123)", "https://github.com/foo/bar/pull/123"},
		{"尾随文件路径", "https://github.com/foo/bar/pull/9/files#diff-abc", "https://github.com/foo/bar/pull/9"},
		{"多条取首条", "https://github.com/a/b/pull/1 和 https://github.com/c/d/pull/2", "https://github.com/a/b/pull/1"},
		{"仅 https", "http://github.com/foo/bar/pull/3 不认", ""},
		{"PR 号必须纯数字", "https://github.com/foo/bar/pull/x1", ""},
		{"无 URL", "纯文本评论", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractPRURL(tc.body); got != tc.want {
				t.Fatalf("ExtractPRURL(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

// TestParsePRRef：规范形 PR URL（ExtractPRURL 的产物形态）→ owner/repo/number。
// 这是解析的单一入口（此前 reqservice 与 gatepoll 各持一份私有同构实现，
// 违反本包"禁止平行另造入口"的冻结纪律）。
// 冻结：严格全串匹配——尾随路径/锚点、非 https、非纯数字 PR 号一律不认；
// PR 号 0 不是合法 PR（GitHub 从 1 起）。
func TestParsePRRef(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want PRRef
		ok   bool
	}{
		{"规范形", "https://github.com/huiyangz/infera/pull/7", PRRef{Owner: "huiyangz", Repo: "infera", Number: 7}, true},
		{"owner/repo 含点划线", "https://github.com/foo.bar/baz-qux/pull/128", PRRef{Owner: "foo.bar", Repo: "baz-qux", Number: 128}, true},
		{"尾随文件路径", "https://github.com/foo/bar/pull/9/files#diff-abc", PRRef{}, false},
		{"http 不认", "http://github.com/foo/bar/pull/3", PRRef{}, false},
		{"PR 号 0 非法", "https://github.com/foo/bar/pull/0", PRRef{}, false},
		{"PR 号非纯数字", "https://github.com/foo/bar/pull/x1", PRRef{}, false},
		{"缺段", "https://github.com/foo/pull/3", PRRef{}, false},
		{"空串", "", PRRef{}, false},
		{"纯文本", "详见时间线", PRRef{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParsePRRef(tc.url)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("ParsePRRef(%q) = (%+v, %v), want (%+v, %v)", tc.url, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestParseCommentPRURL：任意类型事件的正文里出现 PR URL 都随事件携带
// （审批/决策评论里贴 PR 链接同样有效，供深链与合并卡引用）。
func TestParseCommentPRURL(t *testing.T) {
	ev := ParseComment(comment("待批准：计划见 https://github.com/huiyangz/infera/pull/7"))
	if ev.PRURL != "https://github.com/huiyangz/infera/pull/7" {
		t.Fatalf("PRURL = %q, want 提取到的 PR 链接", ev.PRURL)
	}
	ev = ParseComment(comment("verdict: PASS https://github.com/huiyangz/infera/pull/7"))
	if ev.PRURL != "https://github.com/huiyangz/infera/pull/7" {
		t.Fatalf("合并事件 PRURL = %q, want 提取到的 PR 链接", ev.PRURL)
	}
}

// TestInReviewWithoutVerdict：兜底规则二——状态跃迁进入 in_review 但该需求
// 尚未见过任何 verdict 评论 → 需要"有新动态"卡（防 Reviewer 格式跑偏漏掉合并闸门）。
// 首次轮询就见到 in_review（from 为空）同样算跃入；已在 in_review 停留不算。
func TestInReviewWithoutVerdict(t *testing.T) {
	cases := []struct {
		name        string
		from, to    string
		seenVerdict bool
		want        bool
	}{
		{"跃入且无 verdict", "in_progress", "in_review", false, true},
		{"跃入但已见过 verdict", "in_progress", "in_review", true, false},
		{"首次轮询即 in_review", "", "in_review", false, true},
		{"停留在 in_review 不算跃迁", "in_review", "in_review", false, false},
		{"非 in_review 跃迁", "todo", "in_progress", false, false},
		{"离开 in_review", "in_review", "done", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InReviewWithoutVerdict(tc.from, tc.to, tc.seenVerdict); got != tc.want {
				t.Fatalf("InReviewWithoutVerdict(%q, %q, %v) = %v, want %v",
					tc.from, tc.to, tc.seenVerdict, got, tc.want)
			}
		})
	}
}
