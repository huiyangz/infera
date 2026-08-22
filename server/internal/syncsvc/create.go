// 需求创建编排（L202608230412-1-T01）：项目任务列表/详情页「新建需求」的
// 后端面。链路：infera 项目 → 上游项目映射 → 上游建卡（缺省智能体=Tech
// Lead、状态两档 backlog|todo、优先级透传、autoMerge→auto label）→ 触发
// 既有 SyncNow 回流 → 按 ExternalIssueID 读回同步后的行作响应。
// 不新造同步通道：回流就是 syncsvc.Service.SyncNow 本体。
package syncsvc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/tasksource"
)

// IssueCreator 是创建面对 tasksource 薄 client 的最小依赖（*tasksource.Client
// 天然满足；Fetcher 刻意保持只读面，写入面独立成窄接口，Go 鸭型）。
type IssueCreator interface {
	CreateIssue(ctx context.Context, in tasksource.CreateIssueInput) (tasksource.Issue, error)
	ListLabels(ctx context.Context) ([]tasksource.Label, error)
	AddIssueLabel(ctx context.Context, issueID, labelID string) error
}

// SyncTrigger 是回流触发面的最小依赖（*Service 天然满足）。
type SyncTrigger interface {
	SyncNow(ctx context.Context) (Result, error)
}

// autoLabelName 是 autoMerge 映射的上游标签名（计划冻结：自动合并=auto
// label 开关）。标签本体归 workspace 治理，这里只按名解析、不代建。
const autoLabelName = "auto"

// ErrInvalid 输入校验失败（空标题 / 状态超出 backlog|todo 两档）。
var ErrInvalid = errors.New("syncsvc: 输入不合法")

// ErrProjectNotMapped infera 项目无上游映射（从未同步/纯本地建项），无处建卡。
var ErrProjectNotMapped = errors.New("syncsvc: 项目未绑定上游映射")

// CreatorOptions 是装配期输入：缺省智能体定位（reqservice.Options 同款
// 模式，main 从 config.TaskSyncTechLeadAgentID 注入）。
type CreatorOptions struct {
	TechLeadAgentID string // 请求未显式指定智能体时的缺省（必填）
}

// Creator 需求创建编排器。一次构造多处复用；无自有并发状态（上游调用
// 串行于单请求内，回流借 Service.SyncNow 的既有互斥）。
type Creator struct {
	cr     IssueCreator
	syncer SyncTrigger
	st     store.Store
	opts   CreatorOptions
}

// NewCreator 构造编排器。TechLeadAgentID 必填在构造期报错（缺项漏到
// 运行期只会变成难排查的派发失败）。
func NewCreator(cr IssueCreator, syncer SyncTrigger, st store.Store, opts CreatorOptions) (*Creator, error) {
	if cr == nil {
		return nil, errors.New("syncsvc: tasksource client 缺失")
	}
	if syncer == nil {
		return nil, errors.New("syncsvc: 同步服务缺失（回流触发面）")
	}
	if st == nil {
		return nil, errors.New("syncsvc: store 缺失")
	}
	if opts.TechLeadAgentID == "" {
		return nil, errors.New("syncsvc: TechLeadAgentID 必填（缺省智能体解析）")
	}
	return &Creator{cr: cr, syncer: syncer, st: st, opts: opts}, nil
}

// CreateRequirementInput 是创建需求的输入面。状态两档：backlog 待规划
// （不触发 agent run）/ todo 待办（指派即唤醒 agent）；空 = backlog。
type CreateRequirementInput struct {
	Title       string
	Description string
	Status      string // backlog|todo；空 = backlog
	Priority    string // 上游词表透传（urgent/high/medium/low/none）
	AgentID     string // 空 = Tech Lead（装配期定位）
	AutoMerge   bool   // true → 上游 auto label
}

// CreateProjectRequirement 创建需求：校验 → 项目映射解析 → （autoMerge 时
// fail-fast 解析 auto label id）→ 上游建卡（内联指派+状态，官方 CLI 同款
// 载荷）→ 打标 → 触发一轮同步回流 → 读回同步后的 Delivery。
//
// 回流是尽力而为：创建已成功，同步占用/失败不转为创建错误（否则前端按
// 失败重试会建出重复卡）；读回失败时退化为按上游回包 + 同步侧词表拼装的
// 行（无 infera 侧 id，锚点齐全），下一轮自动同步补齐。
func (s *Creator) CreateProjectRequirement(ctx context.Context, projectID string, in CreateRequirementInput) (store.Delivery, error) {
	if strings.TrimSpace(in.Title) == "" {
		return store.Delivery{}, fmt.Errorf("%w: 标题不能为空", ErrInvalid)
	}
	if in.Status == "" {
		in.Status = "backlog"
	}
	if in.Status != "backlog" && in.Status != "todo" {
		return store.Delivery{}, fmt.Errorf("%w: 状态只支持 backlog|todo 两档，got %q", ErrInvalid, in.Status)
	}

	p, err := s.st.GetProject(ctx, projectID)
	if err != nil {
		return store.Delivery{}, fmt.Errorf("读取项目失败: %w", err)
	}
	if p.ExternalProjectID == "" {
		return store.Delivery{}, fmt.Errorf("%w: 项目 %s 从未与上游同步，无映射可建卡", ErrProjectNotMapped, projectID)
	}

	agent := in.AgentID
	if agent == "" {
		agent = s.opts.TechLeadAgentID
	}

	// autoMerge 先解析标签 id 再建卡：解析不出时上游根本没这个标签，
	// 建了卡也缺核心语义——fail-fast 好过留半成品。
	var autoLabelID string
	if in.AutoMerge {
		autoLabelID, err = s.resolveAutoLabel(ctx)
		if err != nil {
			return store.Delivery{}, err
		}
	}

	issue, err := s.cr.CreateIssue(ctx, tasksource.CreateIssueInput{
		Title:        in.Title,
		Description:  in.Description,
		Status:       in.Status,
		Priority:     in.Priority,
		ProjectID:    p.ExternalProjectID,
		AssigneeType: "agent",
		AssigneeID:   agent,
	})
	if err != nil {
		return store.Delivery{}, fmt.Errorf("上游建卡失败: %w", err)
	}
	if in.AutoMerge {
		if err := s.cr.AddIssueLabel(ctx, issue.ID, autoLabelID); err != nil {
			// 卡已建成、标没打上：如实报错并带上卡键，调用方不盲目重试。
			return store.Delivery{}, fmt.Errorf("上游建卡成功（%s）但打 auto 标失败: %w", issue.Identifier, err)
		}
	}

	// 回流：借用既有 SyncNow。ErrSyncRunning / 同步失败都不阻断响应——
	// 创建已成功（同步状态可经 GET /api/task-sync/status 观察）。
	_, _ = s.syncer.SyncNow(ctx)

	return s.readBack(ctx, p.ID, issue, agent, in)
}

// resolveAutoLabel 按名字解析 auto label 的 id。workspace 无此标签 = 无法
// 兑现 autoMerge 语义，报错（不代建——标签是 workspace 治理对象）。
func (s *Creator) resolveAutoLabel(ctx context.Context) (string, error) {
	labels, err := s.cr.ListLabels(ctx)
	if err != nil {
		return "", fmt.Errorf("拉取上游标签失败: %w", err)
	}
	for _, l := range labels {
		if l.Name == autoLabelName {
			return l.ID, nil
		}
	}
	return "", fmt.Errorf("上游 workspace 无 %q 标签，autoMerge 无法生效（请先在上游建此标签）", autoLabelName)
}

// readBack 按 ExternalIssueID 读回同步落库的行（响应=同步侧真实形状）；
// 读不回（同步占用/失败/尚未轮到）退化为按上游回包拼装：状态按同步侧
// 词表预翻（非终态→queued）、负责人按 actorDisplay 同款 "type:id"。
func (s *Creator) readBack(ctx context.Context, projectID string, issue tasksource.Issue, agent string, in CreateRequirementInput) (store.Delivery, error) {
	ds, err := s.st.ListProjectDeliveries(ctx, projectID)
	if err == nil {
		for _, d := range ds {
			if d.ExternalIssueID == issue.ID {
				return d, nil
			}
		}
	}
	return store.Delivery{
		ProjectID:        projectID,
		Title:            issue.Title,
		Description:      in.Description,
		Status:           translateStatus(issue.Status),
		ExternalIssueID:  issue.ID,
		ExternalIssueKey: issue.Identifier,
		Assignee:         "agent:" + agent,
		Priority:         issue.Priority,
		CreatedAt:        issue.UpdatedAt,
		UpdatedAt:        issue.UpdatedAt,
	}, nil
}
