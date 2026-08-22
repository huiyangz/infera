package tasksource

import "time"

// ActorRef 是多态负责人引用（成员/agent/squad 三形一）。零值 = 无负责人。
// project 的 lead 与 issue 的 assignee 共用此形。
type ActorRef struct {
	Type string // member | agent | squad；空 = 无负责人
	ID   string // 对应类型的实体 id
}

// ProjectSnapshot 是 上游项目 → infera 的映射产物（纯数据，不落库）。
// ExternalID 是外部实体的唯一锚点（幂等 upsert 以它定位，归 T02/T03 消费）；
// 可空字段已归一（未填描述 = ""，无负责人 = 零值 ActorRef），消费方免 nil 防御。
type ProjectSnapshot struct {
	ExternalID  string    // 上游 project id（uuid）
	Title       string    // 标题
	Description string    // 描述（未填归一为空串）
	Status      string    // 状态：保留上游原词表透传
	Priority    string    // 优先级：保留上游原词表透传
	Lead        ActorRef  // 负责人（lead_type/lead_id）
	UpdatedAt   time.Time // 同步新鲜度（幂等 upsert 的比较面）
}

// IssueSnapshot 是 上游 issue → infera 的映射产物（纯数据，不落库）。
// ProjectSnapshot 同款归一约定；父子关系以 ParentExternalID 表达
// （子. ParentExternalID == 父.ExternalID，顶层为空串）。
type IssueSnapshot struct {
	ExternalID        string    // 上游 issue id（uuid）
	Identifier        string    // 人读键，如 INFERA-78
	Title             string    // 标题
	Description       string    // 描述（未填归一为空串）
	Status            string    // 状态：保留上游原词表透传
	Priority          string    // 优先级：保留上游原词表透传
	Assignee          ActorRef  // 负责人（assignee_type/assignee_id）
	ParentExternalID  string    // 父子关系：父 issue 的上游 id；空 = 顶层
	ProjectExternalID string    // 项目归属：所属项目的上游 id；空 = 未挂项目
	Stage             int       // 子任务所属阶段（上游 stage 1..N 原值透传；顶层/未带 = 0，兜底归消费方）
	UpdatedAt         time.Time // 同步新鲜度（幂等 upsert 的比较面）
}

// MapProject 把拉取面的 上游项目映射为快照。纯函数：不落库、不写
// store、不改入参；状态/优先级保留上游原词表（向 infera 词表的翻译
// 语义归消费方，本层不发明对照表）。
func MapProject(p Project) ProjectSnapshot {
	return ProjectSnapshot{
		ExternalID:  p.ID,
		Title:       p.Title,
		Description: derefOrEmpty(p.Description),
		Status:      p.Status,
		Priority:    p.Priority,
		Lead:        actorRef(p.LeadType, p.LeadID),
		UpdatedAt:   p.UpdatedAt,
	}
}

// MapIssue 把拉取面的 上游 issue 映射为快照。纯函数，约定同 MapProject。
func MapIssue(i Issue) IssueSnapshot {
	return IssueSnapshot{
		ExternalID:        i.ID,
		Identifier:        i.Identifier,
		Title:             i.Title,
		Description:       derefOrEmpty(i.Description),
		Status:            i.Status,
		Priority:          i.Priority,
		Assignee:          actorRef(i.AssigneeType, i.AssigneeID),
		ParentExternalID:  derefOrEmpty(i.ParentIssueID),
		ProjectExternalID: derefOrEmpty(i.ProjectID),
		Stage:             i.Stage,
		UpdatedAt:         i.UpdatedAt,
	}
}

// derefOrEmpty 解引用可空字符串：nil/未填 → ""。
func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// actorRef 组装负责人引用：类型与 id 任一缺失视为无负责人（零值）——
// 半截引用（有 id 无类型）不是合法状态，归零而不是带病透传。
func actorRef(typ, id *string) ActorRef {
	if typ == nil || id == nil || *typ == "" || *id == "" {
		return ActorRef{}
	}
	return ActorRef{Type: *typ, ID: *id}
}
