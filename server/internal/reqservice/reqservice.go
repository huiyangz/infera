// Package reqservice 是需求编排服务（INFERA-11 T05）：infera 侧需求聚合根的
// 派发、读取、代理动作、审计与项目级合并策略设置。
package reqservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tokfinity/infera/internal/flow"
	"github.com/tokfinity/infera/internal/github"
	"github.com/tokfinity/infera/internal/multica"
)

// MulticaClient 是 reqservice 对 Multica 薄 client 的窄接口（Go 鸭型，
// *multica.Client 天然满足；单测用 fake，不碰真服务）。
type MulticaClient interface {
	CreateIssue(ctx context.Context, in multica.CreateIssueInput) (multica.Issue, error)
	AssignAgent(ctx context.Context, issueID, agentID string) error
	SetStatus(ctx context.Context, issueID, status string, suppressRun bool) error
	PostComment(ctx context.Context, issueID, content string) (multica.Comment, error)
}

// GitHubClient 是合并动作对 github client 的窄接口。
type GitHubClient interface {
	MergePullRequest(ctx context.Context, owner, repo string, number int, in github.MergeInput) (github.MergeResult, error)
}

// Options 是装配期输入：派发目标与深链拼装所需的 Multica 侧定位。
type Options struct {
	MulticaProjectID     string // 派发目标 Multica 项目（必填）
	TechLeadAgentID      string // 派发指派的 Tech Lead agent（必填）
	MulticaServerURL     string // 深链前缀，如 http://localhost:8088（必填）
	MulticaWorkspaceSlug string // 深链工作区段，如 infera（必填）
}

// Service 是需求编排服务。线程安全（pgx 连接池 + 无状态 client）。
type Service struct {
	pool *pgxpool.Pool
	mc   MulticaClient
	gh   GitHubClient
	opts Options
}

// New 构造 Service。装配期显式校验（必填性在 reqservice 决定——缺失
// 只会变成运行期难排查的派发失败，构造期报错）。
func New(pool *pgxpool.Pool, mc MulticaClient, gh GitHubClient, opts Options) (*Service, error) {
	if pool == nil {
		return nil, errors.New("reqservice: 连接池缺失")
	}
	if mc == nil {
		return nil, errors.New("reqservice: multica client 缺失")
	}
	if gh == nil {
		return nil, errors.New("reqservice: github client 缺失")
	}
	if opts.MulticaProjectID == "" {
		return nil, errors.New("reqservice: MulticaProjectID 必填（派发固定项目，FR-2）")
	}
	if opts.TechLeadAgentID == "" {
		return nil, errors.New("reqservice: TechLeadAgentID 必填（派发指派 Tech Lead）")
	}
	if opts.MulticaServerURL == "" || opts.MulticaWorkspaceSlug == "" {
		return nil, errors.New("reqservice: MulticaServerURL 与 MulticaWorkspaceSlug 必填（深链逃生口 FR-8）")
	}
	opts.MulticaServerURL = strings.TrimSuffix(opts.MulticaServerURL, "/")
	return &Service{pool: pool, mc: mc, gh: gh, opts: opts}, nil
}

// CreateInput 是发起需求的输入面。业务元数据只存 infera，不下发 Multica。
type CreateInput struct {
	Title              string
	Description        string
	AcceptanceCriteria string
	Source             string
	Priority           string
	Acceptors          []string
}

// Requirement 是 API 响应形态的需求行（含深链）。
type Requirement struct {
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	Description        string    `json:"description"`
	AcceptanceCriteria string    `json:"acceptance_criteria"`
	Source             string    `json:"source"`
	Priority           string    `json:"priority"`
	Acceptors          []string  `json:"acceptors"`
	MulticaIssueID     string    `json:"multica_issue_id"`
	MulticaIssueKey    string    `json:"multica_issue_key"`
	MulticaIssueURL    string    `json:"multica_issue_url"`
	PRURL              string    `json:"pr_url"`
	Node               flow.Node `json:"node"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// ErrInvalid 输入校验失败（标题 / 反馈文本 / 决策选项等）。
var ErrInvalid = errors.New("reqservice: 输入不合法")

// ErrConflict 动作与当前状态冲突（卡已处理 / 卡类型与动作不匹配 / 缺 PR 关联）。
var ErrConflict = errors.New("reqservice: 状态冲突")

// ActorUser 是人工代理动作的固定 actor（单租户单密码门，无用户体系）。
// gatepoll 的自动动作用 actor=system（T04 语义），与本值区分。
const ActorUser = "user"

// GateCard 是闸门卡的 API 响应形态。
type GateCard struct {
	ID            string          `json:"id"`
	RequirementID string          `json:"requirement_id"`
	Kind          flow.GateKind   `json:"kind"`
	Status        flow.CardStatus `json:"status"`
	Payload       string          `json:"payload"`
	CommentID     string          `json:"comment_id"`
	CreatedAt     time.Time       `json:"created_at"`
	ResolvedAt    *time.Time      `json:"resolved_at"` // null = 未处理
}

// RequirementDetail 是需求详情：需求行 + 待处理卡。
type RequirementDetail struct {
	Requirement
	PendingCards []GateCard `json:"pending_cards"`
}

// RequirementListItem 是列表行：需求行 + 待处理卡计数。
type RequirementListItem struct {
	Requirement
	PendingCardCount int `json:"pending_card_count"`
}

// Get 需求详情：需求行（含深链）+ 待处理闸门卡（已处理卡不返回——前端
// 只渲染待办面，历史卡走审计时间线）。
func (s *Service) Get(ctx context.Context, id string) (*RequirementDetail, error) {
	r, err := s.getRequirement(ctx, id)
	if err != nil {
		return nil, err
	}
	cards, err := s.listPendingCards(ctx, id)
	if err != nil {
		return nil, err
	}
	if cards == nil {
		cards = []GateCard{}
	}
	d := &RequirementDetail{Requirement: s.toRequirement(r), PendingCards: cards}
	return d, nil
}

// List 需求列表（新 → 旧），附各需求的待处理卡计数。
func (s *Service) List(ctx context.Context) ([]RequirementListItem, error) {
	reqs, err := s.listRequirements(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取需求列表失败: %w", err)
	}
	counts, err := s.pendingCardCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RequirementListItem, len(reqs))
	for i := range reqs {
		out[i] = RequirementListItem{Requirement: s.toRequirement(&reqs[i])}
		out[i].PendingCardCount = counts[reqs[i].ID]
	}
	return out, nil
}

// Create 发起需求并派发：Multica 固定项目建父 issue（backlog 起步，不触发
// run）→ 指派 Tech Lead（置 todo 唤醒 agent）→ infera 落库（大节点=已派发）。
// 需求描述、验收标准与业务元数据只存 infera，不下发 Multica（FR-2）。
// Multica 侧失败则不落库（宁可不派发，不留无 issue 的本地孤儿行）。
func (s *Service) Create(ctx context.Context, in CreateInput) (*Requirement, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("%w: 标题不能为空", ErrInvalid)
	}
	issue, err := s.mc.CreateIssue(ctx, multica.CreateIssueInput{
		Title:     in.Title,
		Status:    "backlog",
		ProjectID: s.opts.MulticaProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("multica 建卡失败: %w", err)
	}
	if err := s.mc.AssignAgent(ctx, issue.ID, s.opts.TechLeadAgentID); err != nil {
		// 指派失败：尽力把已建的 issue 停回 backlog（suppressRun 防误唤醒），
		// 本地不落库。清理失败只记录进错误链，不掩盖原始失败。
		if serr := s.mc.SetStatus(ctx, issue.ID, "backlog", true); serr != nil {
			err = fmt.Errorf("%w（回收 issue 亦失败: %v）", err, serr)
		}
		return nil, fmt.Errorf("multica 指派 Tech Lead 失败: %w", err)
	}
	r := &flow.Requirement{
		ID:                 newID(),
		Title:              in.Title,
		Description:        in.Description,
		AcceptanceCriteria: in.AcceptanceCriteria,
		Source:             in.Source,
		Priority:           in.Priority,
		Acceptors:          in.Acceptors,
		MulticaIssueID:     issue.ID,
		MulticaIssueKey:    issue.Identifier,
		Node:               flow.NodeDispatched,
	}
	if r.Acceptors == nil {
		r.Acceptors = []string{} // acceptors TEXT[] NOT NULL
	}
	if err := s.insertRequirement(ctx, r); err != nil {
		return nil, fmt.Errorf("需求落库失败: %w", err)
	}
	out := s.toRequirement(r)
	return &out, nil
}

// issueURL 拼装 Multica issue 深链（Web 路由 /{slug}/issues/{id}，FR-8）。
func (s *Service) issueURL(r *flow.Requirement) string {
	if r.MulticaIssueID == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/issues/%s", s.opts.MulticaServerURL, s.opts.MulticaWorkspaceSlug, r.MulticaIssueID)
}

// toRequirement 领域行 → API 响应形态（含深链）。
func (s *Service) toRequirement(r *flow.Requirement) Requirement {
	acc := r.Acceptors
	if acc == nil {
		acc = []string{}
	}
	return Requirement{
		ID:                 r.ID,
		Title:              r.Title,
		Description:        r.Description,
		AcceptanceCriteria: r.AcceptanceCriteria,
		Source:             r.Source,
		Priority:           r.Priority,
		Acceptors:          acc,
		MulticaIssueID:     r.MulticaIssueID,
		MulticaIssueKey:    r.MulticaIssueKey,
		MulticaIssueURL:    s.issueURL(r),
		PRURL:              r.PRURL,
		Node:               r.Node,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}
