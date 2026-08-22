package flow

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GateKind 是闸门事件的类型（值即 gate_cards.kind 落库值）。
type GateKind string

const (
	GateApproval GateKind = "approval" // 审批卡（待批准：）
	GateDecision GateKind = "decision" // 决策卡（需要决策：）
	GateMerge    GateKind = "merge"    // 合并卡（verdict:）
	GateUpdate   GateKind = "update"   // 兜底"有新动态"卡
)

// Verdict 是合并事件的结论词。
type Verdict string

const (
	VerdictPass    Verdict = "PASS"
	VerdictFail    Verdict = "FAIL"
	VerdictUnknown Verdict = ""
)

// 闸门协议（冻结）：三类固定字面前缀，必须顶格、区分大小写，不做任何 trim：
//   - `待批准：`（全角冒号）→ 审批事件
//   - `需要决策：`（全角冒号）→ 决策事件
//   - `verdict:`（ASCII 冒号）→ 合并事件
//
// 半角/全角冒号互换等相近变体刻意不命中（防 agent 格式跑偏伪装成闸门），
// 它们与所有其他评论一起走兜底"有新动态"。
const (
	prefixApproval = "待批准："
	prefixDecision = "需要决策："
	prefixMerge    = "verdict:"
)

// CommentInput 是闸门解析器的最小输入面。tasksource.Comment 由下游
// （gatepoll）适配成它——flow 自身不 import tasksource，保持纯确定性。
type CommentInput struct {
	ID         string
	AuthorType string
	AuthorID   string
	Body       string
	CreatedAt  time.Time
}

// GateEvent 是一条评论解析出的闸门事件：Kind 说明卡的类型，溯源字段
// 供落库卡片回链与去重；PRURL 独立于事件类型，正文里出现即提取。
type GateEvent struct {
	Kind       GateKind
	CommentID  string
	AuthorType string
	AuthorID   string
	Body       string
	CreatedAt  time.Time
	Verdict    Verdict // 仅合并事件有值
	PRURL      string  // 正文提取到的 github PR URL，无则空
}

// ParseComment 把一条评论分类为闸门事件（增量评论流的单条语义）。
// 识别不了任何前缀的评论返回 GateUpdate——即兜底规则一"有新动态"。
func ParseComment(c CommentInput) GateEvent {
	ev := GateEvent{
		CommentID:  c.ID,
		AuthorType: c.AuthorType,
		AuthorID:   c.AuthorID,
		Body:       c.Body,
		CreatedAt:  c.CreatedAt,
	}
	switch {
	case strings.HasPrefix(c.Body, prefixApproval):
		ev.Kind = GateApproval
	case strings.HasPrefix(c.Body, prefixDecision):
		ev.Kind = GateDecision
	case strings.HasPrefix(c.Body, prefixMerge):
		ev.Kind = GateMerge
		ev.Verdict = ExtractVerdict(c.Body)
	default:
		ev.Kind = GateUpdate
	}
	ev.PRURL = ExtractPRURL(c.Body)
	return ev
}

// verdict 词匹配：全词、大小写敏感（PASSING 不算 PASS）。
var verdictRe = regexp.MustCompile(`\b(PASS|FAIL)\b`)

// ExtractVerdict 从 verdict 评论正文提取 PASS / FAIL 结论词，无则 Unknown。
// 首个命中者为准：协议要求评论以 `verdict:` 顶格开头，结论词先于补充说明。
func ExtractVerdict(body string) Verdict {
	m := verdictRe.FindStringSubmatch(body)
	if m == nil {
		return VerdictUnknown
	}
	return Verdict(m[1])
}

// github PR URL：https 限定；owner/repo 限 GitHub 合法字符集；捕获到 PR 号
// 为止，尾随路径/锚点自然剥掉；多条取首条。
var prURLRe = regexp.MustCompile(`https://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/pull/([0-9]+)`)

// ExtractPRURL 从任意评论正文提取第一个 github PR URL（规范形），无则空串。
func ExtractPRURL(body string) string {
	m := prURLRe.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[0]
}

// PRRef 是规范形 PR URL 解析出的 GitHub PR 定位（owner/repo/number）。
type PRRef struct {
	Owner  string
	Repo   string
	Number int
}

// prRefRe 是解析用的严格全串形（提取用的 prURLRe 不锚定，两者同字符集）。
var prRefRe = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/pull/([0-9]+)$`)

// ParsePRRef 把规范形 PR URL（ExtractPRURL 的产物形态）解析为 owner / repo /
// number。严格全串匹配：尾随路径/锚点、非 https、非纯数字 PR 号一律不认
// （PR 号 0 也不是合法 PR，GitHub 从 1 起）——数据来自本服务落库的规范提取，
// 走歪说明状态异常，由调用方按各自语义处理（reqservice 报状态冲突，
// gatepoll 防御性跳过）。这是 PR URL 解析的单一入口，下游禁止平行另造。
func ParsePRRef(u string) (PRRef, bool) {
	m := prRefRe.FindStringSubmatch(u)
	if m == nil {
		return PRRef{}, false
	}
	n, err := strconv.Atoi(m[3])
	if err != nil || n <= 0 {
		return PRRef{}, false
	}
	return PRRef{Owner: m[1], Repo: m[2], Number: n}, true
}

// InReviewWithoutVerdict 是兜底规则二：状态跃迁进入 in_review 但该需求
// 尚未见过任何 verdict 评论时，需要发中性"有新动态"卡。
// from 为空（首次轮询）视同跃入——宁可多弹一张中性卡，不漏合并闸门。
// 停留在 in_review（from==to）不算跃迁。
func InReviewWithoutVerdict(fromStatus, toStatus string, seenVerdict bool) bool {
	return toStatus == "in_review" && fromStatus != "in_review" && !seenVerdict
}
