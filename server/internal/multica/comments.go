package multica

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// CommentCursor 是增量评论游标：(AfterID, Since) 二元组，见 ListCommentsSince。
type CommentCursor struct {
	// AfterID 是最后一条已交付评论的 id；空值 = 首轮（从头开始）。
	AfterID string
	// Since 是该评论 created_at 的值（服务端响应为秒级精度），仅用于收窄
	// 服务端返回窗口，不承担排序语义。
	Since time.Time
}

// PostComment 以服务身份代发评论（POST /api/issues/{id}/comments，请求体
// content——multica-src CreateComment 实证；201 回显完整评论对象）。
// 审批（approved / 驳回反馈）、决策回复、返工指令全部走这里：infera 是唯一
// 前台，用户不直接进 Multica 发评论。
//
// 空内容在客户端就地拒绝（服务端同样回 400 "content is required"），省一次
// 注定失败的往返。
func (c *Client) PostComment(ctx context.Context, issueID, content string) (Comment, error) {
	if content == "" {
		return Comment{}, fmt.Errorf("multica: PostComment content 不能为空（服务端同样会以 400 拒绝）")
	}
	body := struct {
		Content string `json:"content"`
	}{Content: content}
	var posted Comment
	if err := c.do(ctx, http.MethodPost, "/api/issues/"+issueID+"/comments", body, &posted); err != nil {
		return Comment{}, err
	}
	return posted, nil
}

// ListCommentsSince 增量拉取游标之后的新评论，返回 (新评论, 推进后的游标)。
// 零值游标（AfterID/Since 均零）= 首轮全量；推进规则内建：取本轮最末一条
// 评论构造 next 游标，无新评论时 next 原样返回——调用方只需存回 next。
//
// 为什么游标是 (AfterID, Since) 二元组而不是纯时间戳（本地冒烟实测踩中）：
// 服务端响应的 created_at 是秒级截断（multica-src util.TimestampToString
// 用 time.RFC3339 序列化），而 DB 存微秒。纯时间戳游标解析回整秒 .000000，
// 服务端按 DB 精度做"严格大于 since"过滤 → 边界秒内的旧评论每轮重复返回，
// 同秒内更晚的新评论又无法与旧评论区分。因此排序唯一可靠的信号是响应顺序。
//
// 协议（不漏不重）：
//   - 请求：GET /api/issues/{id}/comments?since=<cur.Since>（服务端实证语义：
//     只回 DB created_at 严格大于 since 的行；由于截断，边界秒的评论组会
//     整组返回）。cur.Since 为零 → 不带参数（全量）。
//   - 响应：服务端恒按时间升序（oldest → newest，multica-src 文档化不变量）。
//   - 切位：客户端在响应中定位 cur.AfterID，只交付其后的评论。
//     不重：AfterID 及其之前的评论（含同秒边界组里的旧评论）一律切掉。
//     不漏：晚于 AfterID 创建的任何评论必然排在其后（升序不变量）→ 必然交付。
//   - 兜底：AfterID 不在响应中（锚点评论被删除——平台有 DELETE
//     /api/comments/{id}，本流水线不删）→ 位置不可知，退化为交付 since 窗口
//     内全部评论，宁可重发不漏发；调用方按评论 id 幂等去重。
//
// 单次返回有平台上限（约 2000 条，超出经 X-Multica-Comments-Truncated 头
// 标记）；本流水线单个需求 issue 的评论量远低于该量级，客户端不处理翻页。
func (c *Client) ListCommentsSince(ctx context.Context, issueID string, cur CommentCursor) ([]Comment, CommentCursor, error) {
	path := "/api/issues/" + issueID + "/comments"
	if !cur.Since.IsZero() {
		q := url.Values{"since": {cur.Since.UTC().Format(time.RFC3339Nano)}}
		path += "?" + q.Encode()
	}
	var fetched []Comment
	if err := c.do(ctx, http.MethodGet, path, nil, &fetched); err != nil {
		return nil, cur, err
	}

	var out []Comment
	if cur.AfterID == "" {
		out = fetched
	} else {
		idx := -1
		for i, cm := range fetched {
			if cm.ID == cur.AfterID {
				idx = i
				break
			}
		}
		if idx < 0 {
			// 兜底：锚点不在响应中（被删）→ 窗口全量交付，见函数注释。
			out = fetched
		} else {
			out = fetched[idx+1:]
		}
	}

	next := cur
	if len(out) > 0 {
		last := out[len(out)-1]
		next = CommentCursor{AfterID: last.ID, Since: last.CreatedAt}
	}
	return out, next, nil
}
