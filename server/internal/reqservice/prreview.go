// PR 评审读取面（INFERA-11 FR-4/FR-7）：合并卡的行级评审评论与 diff 概要
// 数据源。只读——不落闸门卡、不落审计、不动大节点；PR 定位复用需求的
// pr_url（flow.ParsePRRef 单源解析）。
package reqservice

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PRReviewComment 是行级评审评论的 API 响应形态（github.ReviewComment 的
// 渲染面裁剪：author 平铺、diff 上下文片段不下发）。删除行上的评论 Line
// 为 0、行号在 OriginalLine（GitHub original_* 语义）。
type PRReviewComment struct {
	ID           int64     `json:"id"`
	Path         string    `json:"path"`
	Line         int       `json:"line"`
	OriginalLine int       `json:"original_line"`
	Side         string    `json:"side"` // RIGHT | LEFT
	Body         string    `json:"body"`
	Author       string    `json:"author"`
	InReplyToID  int64     `json:"in_reply_to_id"` // 0 = 顶层评论，非 0 = 回复
	CreatedAt    time.Time `json:"created_at"`
}

// PRDiffStats 是 diff 概要的 API 响应形态（文件数与 +/- 行数）。
type PRDiffStats struct {
	Files     int `json:"files"`
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Changes   int `json:"changes"`
}

// PRReview 是合并卡渲染面：行级评审评论 + diff 概要 + PR 深链。
type PRReview struct {
	PRURL    string            `json:"pr_url"`
	Comments []PRReviewComment `json:"comments"`
	Diff     PRDiffStats       `json:"diff"`
}

// GetPRReview 拉取需求关联 PR 的行级评审评论与 diff 概要（经 gh API，
// FR-7——评审内容在卡内渲染，用户不访问 GitHub 页面）。
// 需求缺 PR 关联按冲突返回（与 Merge 的无 PR 语义一致）；github 故障
// 原样上抛（API 层 502）。
func (s *Service) GetPRReview(ctx context.Context, requirementID string) (*PRReview, error) {
	r, err := s.getRequirement(ctx, requirementID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.PRURL) == "" {
		return nil, fmt.Errorf("%w: 需求尚未关联 GitHub PR（等轮询器从评论提取）", ErrConflict)
	}
	ref, err := parsePRRef(r.PRURL)
	if err != nil {
		return nil, err
	}
	comments, err := s.gh.ListReviewComments(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return nil, fmt.Errorf("拉取评审评论失败: %w", err)
	}
	stats, err := s.gh.GetDiffStats(ctx, ref.Owner, ref.Repo, ref.Number)
	if err != nil {
		return nil, fmt.Errorf("拉取 diff 统计失败: %w", err)
	}
	out := &PRReview{PRURL: r.PRURL, Comments: make([]PRReviewComment, 0, len(comments))}
	for _, c := range comments {
		out.Comments = append(out.Comments, PRReviewComment{
			ID: c.ID, Path: c.Path, Line: c.Line, OriginalLine: c.OriginalLine,
			Side: c.Side, Body: c.Body, Author: c.User.Login,
			InReplyToID: c.InReplyToID, CreatedAt: c.CreatedAt,
		})
	}
	out.Diff = PRDiffStats{
		Files: stats.Files, Additions: stats.Additions,
		Deletions: stats.Deletions, Changes: stats.Changes,
	}
	return out, nil
}
