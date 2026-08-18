// MCP 工具定义与实现。工具即驾驶面：
//   - get_context：交付全量上下文（需求 / 产物 / 仓库与 workdir / 本机停车节点的角色 prompt）；
//   - submit_stage_output：local 绑定节点的交回（engine.SubmitLocal 单入口）；
//   - get_gate / approve_gate / reject_gate：门禁查询与裁定（engine.Approve/Reject 单入口）。
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/tokfinity/infera/internal/engine"
	"github.com/tokfinity/infera/internal/store"
)

// argError 参数层错误（缺必填 / 类型不符）→ JSON-RPC -32602；区别于工具执行错误（isError 结果）。
type argError struct{ msg string }

func (e *argError) Error() string { return e.msg }

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func prop(description string, extra ...map[string]any) map[string]any {
	p := map[string]any{"type": "string", "description": description}
	for _, e := range extra {
		for k, v := range e {
			p[k] = v
		}
	}
	return p
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// toolDefs 工具清单（tools/list 的单一事实来源）。
func toolDefs() []toolDef {
	return []toolDef{
		{
			Name:        "get_context",
			Description: "获取交付的全量驾驶上下文：需求（标题/描述）、当前阶段与门禁、已有产物（规格/设计/任务清单/摘要等，每类最新一份全文）、仓库信息（repo、默认分支、workdir 路径与约定）、以及停在本机绑定节点时该节点的角色 prompt（pending_local）。驾驶流水线第一步总是它。",
			InputSchema: objectSchema(map[string]any{
				"delivery_id": prop("交付 ID（UUID）"),
			}, "delivery_id"),
		},
		{
			Name:        "get_gate",
			Description: "查看交付当前挂起的人工门禁详情：待审产物全文（spec/design/tasks 文档或前置审查意见）、PR 地址，以及门禁专属信息（spec_approval 附 AI 复杂度建议、design_approval 附 AI 拆分建议、tasks_approval 附可覆盖的任务清单）。",
			InputSchema: objectSchema(map[string]any{
				"delivery_id": prop("交付 ID（UUID）"),
			}, "delivery_id"),
		},
		{
			Name:        "approve_gate",
			Description: "批准交付当前挂起的人工门禁（引擎 Approve 单入口）。可选选项按门分发：spec_approval 带 complexity=small|large（缺省取 AI 建议）；design_approval 带 split=[{title,description,wave}] 表示「批准并拆分」；tasks_approval 带 tasks=[{title,detail}] 表示「批准并覆盖任务清单」。批准后流水线自动推进。",
			InputSchema: objectSchema(map[string]any{
				"delivery_id": prop("交付 ID（UUID）"),
				"complexity":  prop("需求复杂度裁定，仅 spec_approval 门可用", map[string]any{"enum": []string{"small", "large"}}),
				"split": map[string]any{"type": "array", "description": "拆分子需求清单，仅 design_approval 门可用（非空=批准并拆分）", "items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":       prop("子需求标题"),
						"description": prop("子需求范围"),
						"wave":        map[string]any{"type": "integer", "description": "批次号 1..N"},
					},
					"required": []string{"title", "description"},
				}},
				"tasks": map[string]any{"type": "array", "description": "覆盖后的任务清单，仅 tasks_approval 门可用（非空=批准并覆盖）", "items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":  prop("任务标题"),
						"detail": prop("任务详情（做什么、怎么验收）"),
					},
					"required": []string{"title"},
				}},
			}, "delivery_id"),
		},
		{
			Name:        "reject_gate",
			Description: "打回交付当前挂起的人工门禁（引擎 Reject 单入口）：流水线回退到该门禁的重跑阶段，reason 作为反馈注入重跑 prompt。spec/design/tasks 门禁回退到对应文档阶段，code_review 门禁回退到 code_gen。",
			InputSchema: objectSchema(map[string]any{
				"delivery_id": prop("交付 ID（UUID）"),
				"reason":      prop("打回原因（注入重跑反馈）"),
			}, "delivery_id"),
		},
		{
			Name:        "submit_stage_output",
			Description: "交回本机绑定节点（local 绑定）的阶段产出：把 output 按该节点的产物契约落盘（spec/design 为文档全文；tasks 需含 ```infera-tasks fenced block；code_gen 为改动摘要——代码改动直接在 workdir 里做），然后流水线自动推进到下一个停车点。交付挂在 code_review 门禁且审查节点为本机绑定时，交回内容作为预审意见（门禁仍需 approve/reject 裁定）。不是本机绑定节点会拒绝。",
			InputSchema: objectSchema(map[string]any{
				"delivery_id": prop("交付 ID（UUID）"),
				"output":      prop("阶段产出全文（契约同上）"),
			}, "delivery_id", "output"),
		},
	}
}

// --- 参数与公共读取 ---

// bookkeep 簿记动作用与请求脱钩的有界 ctx（客户端断连不能杀掉持久化；
// 与 api.withGateAction 同款 10s 上限），读路径仍走请求 ctx。
func bookkeep() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return &argError{msg: "arguments 不合法: " + err.Error()}
	}
	return nil
}

// delivery 取交付：畸形 UUID 与不存在统一按「交付不存在」报给客户端。
func (s *Server) delivery(ctx context.Context, id string) (*store.Delivery, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, errors.New("交付不存在")
	}
	d, err := s.st.GetDelivery(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errors.New("交付不存在")
	}
	return d, err
}

// snapshot 簿记后的最新状态视图（读失败退化为仅 id——推进已发生，读不影响结果）。
func (s *Server) snapshot(ctx context.Context, id string) map[string]any {
	out := map[string]any{"delivery_id": id}
	if d, err := s.st.GetDelivery(ctx, id); err == nil {
		out["status"], out["current_stage"], out["pending_gate"] = d.Status, d.CurrentStage, d.PendingGate
	}
	return out
}

// maxContextRunes get_context 里单产物内容的截断上限（test_output 可能极长；
// 文档类产物 spec/design/tasks 通常远小于此）。
const maxContextRunes = 8000

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n…（截断）"
}

// gateArtifactKind 门禁 → 待审产物 kind（与 api 门禁页同一映射；
// 未导出复用以避免 mcp→api 反向依赖，映射变化时两处同步）。
func gateArtifactKind(gate string) string {
	switch gate {
	case "spec_approval":
		return "spec"
	case "design_approval":
		return "design"
	case "tasks_approval":
		return "tasks"
	}
	return "agent_output"
}

// --- 工具实现 ---

func (s *Server) toolGetContext(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		DeliveryID string `json:"delivery_id"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.DeliveryID == "" {
		return nil, &argError{msg: "缺少 delivery_id"}
	}
	d, err := s.delivery(ctx, a.DeliveryID)
	if err != nil {
		return nil, err
	}
	proj, err := s.st.GetProject(ctx, d.ProjectID)
	if err != nil {
		return nil, errors.New("项目不存在")
	}
	arts, err := s.st.ListArtifacts(ctx, d.ID)
	if err != nil {
		return nil, errors.New("读取产物失败")
	}
	latest := map[string]map[string]string{} // kind → 最新一份（列表从旧到新，后写覆盖）
	for _, a := range arts {
		content := a.Content
		if a.Kind == "test_output" {
			content = truncateRunes(content, 2000)
		} else {
			content = truncateRunes(content, maxContextRunes)
		}
		latest[a.Kind] = map[string]string{"stage": a.Stage, "content": content}
	}

	out := map[string]any{
		"delivery": map[string]any{
			"id": d.ID, "title": d.Title, "description": d.Description,
			"status": d.Status, "current_stage": d.CurrentStage,
			"pending_gate": d.PendingGate, "complexity": d.Complexity,
			"split_mode": d.SplitMode, "fail_count": d.FailCount,
		},
		"project": map[string]any{
			"name": proj.Name, "repo_url": proj.RepoURL, "default_branch": proj.DefaultBranch,
		},
		"repo": map[string]any{
			"workdir":     s.workdir(d.ID),
			"base_commit": d.BaseCommit,
			// workdir 约定：clone 自 default_branch，各阶段共享同一目录；
			// 交付分支由引擎在 code_review 门禁固化时创建推送，勿手工改分支。
			"convention": "workdir 为该交付独占的仓库检出（clone 自 default_branch，各阶段共享）；改动直接提交在当前分支，交付分支/PR 由引擎在 code_review 门禁创建",
		},
		"artifacts": latest,
		"pending_gate": map[string]any{
			"gate": d.PendingGate,
			"hint": "门禁详情用 get_gate；裁定用 approve_gate / reject_gate",
		},
	}
	if role, prompt, err := s.eng.LocalPrompt(ctx, d.ID); err == nil && role != "" {
		out["pending_local"] = map[string]string{"node": role, "prompt": prompt}
	} else {
		out["pending_local"] = nil
	}
	return out, nil
}

func (s *Server) toolSubmitStageOutput(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		DeliveryID string `json:"delivery_id"`
		Output     string `json:"output"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.DeliveryID == "" {
		return nil, &argError{msg: "缺少 delivery_id"}
	}
	if strings.TrimSpace(a.Output) == "" {
		return nil, errors.New("output 不能为空")
	}
	if _, err := s.delivery(ctx, a.DeliveryID); err != nil {
		return nil, err
	}
	bctx, cancel := bookkeep()
	defer cancel()
	if err := s.act(a.DeliveryID, func() error {
		return s.eng.SubmitLocal(bctx, a.DeliveryID, a.Output)
	}); err != nil {
		return nil, err
	}
	return s.snapshot(ctx, a.DeliveryID), nil
}

func (s *Server) toolGetGate(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		DeliveryID string `json:"delivery_id"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.DeliveryID == "" {
		return nil, &argError{msg: "缺少 delivery_id"}
	}
	d, err := s.delivery(ctx, a.DeliveryID)
	if err != nil {
		return nil, err
	}
	if d.PendingGate == "" {
		return nil, errors.New("当前无挂起门禁（交付停在 " + d.CurrentStage + "）")
	}
	gate := d.PendingGate
	kind := gateArtifactKind(gate)
	resp := map[string]any{
		"delivery_id": d.ID,
		"gate":        gate,
		"agent_output": map[string]string{
			"agent": gate, "output": "",
		},
		"pr_url": "",
	}
	arts, err := s.st.ListArtifacts(ctx, d.ID)
	if err != nil {
		return nil, errors.New("读取产物失败")
	}
	output, prURL, diff := "", "", ""
	for _, a := range arts { // 列表从旧到新：后写覆盖 = 最新
		if a.Kind == kind {
			resp["agent_output"] = map[string]string{"agent": a.Stage, "output": a.Content}
			output = a.Content
		}
		if a.Kind == "pr" && prURL == "" {
			prURL = a.Content
			resp["pr_url"] = prURL
		}
		if a.Kind == "diff" {
			diff = a.Content
		}
	}
	// code_review 门禁的裁决材料是真 diff（Persist 在挂门前落盘）——
	// MCP 客户端没有前端那套 delivery 详情接口，diff 全文随门禁详情一起给。
	if diff != "" {
		resp["diff"] = truncateRunes(diff, maxContextRunes)
	}
	switch gate {
	case "spec_approval":
		resp["complexity_suggestion"] = engine.ParseComplexitySuggestion(output)
	case "design_approval":
		resp["split_plan"] = engine.ParseSplitPlan(output)
	case "tasks_approval":
		var tasks []store.TaskSpec
		if err := json.Unmarshal([]byte(output), &tasks); err != nil {
			tasks = nil
		}
		resp["tasks"] = tasks
	}
	return resp, nil
}

func (s *Server) toolApproveGate(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		DeliveryID string            `json:"delivery_id"`
		Complexity string            `json:"complexity"`
		Split      []store.ChildSpec `json:"split"`
		Tasks      []store.TaskSpec  `json:"tasks"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.DeliveryID == "" {
		return nil, &argError{msg: "缺少 delivery_id"}
	}
	if _, err := s.delivery(ctx, a.DeliveryID); err != nil {
		return nil, err
	}
	opts := store.ApproveOpts{Complexity: a.Complexity, Split: a.Split, Tasks: a.Tasks}
	bctx, cancel := bookkeep()
	defer cancel()
	if err := s.act(a.DeliveryID, func() error {
		_, err := s.eng.Approve(bctx, a.DeliveryID, opts) // 冻结单入口：选项按当前门校验
		return err
	}); err != nil {
		return nil, err
	}
	return s.snapshot(ctx, a.DeliveryID), nil
}

func (s *Server) toolRejectGate(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		DeliveryID string `json:"delivery_id"`
		Reason     string `json:"reason"`
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.DeliveryID == "" {
		return nil, &argError{msg: "缺少 delivery_id"}
	}
	if _, err := s.delivery(ctx, a.DeliveryID); err != nil {
		return nil, err
	}
	bctx, cancel := bookkeep()
	defer cancel()
	if err := s.act(a.DeliveryID, func() error {
		return s.eng.Reject(bctx, a.DeliveryID, a.Reason)
	}); err != nil {
		return nil, err
	}
	return s.snapshot(ctx, a.DeliveryID), nil
}
