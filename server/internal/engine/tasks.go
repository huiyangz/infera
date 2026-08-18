// 任务层（R8）：tasks agent 产出 fenced block 清单 → 引擎解析存 JSON artifact →
// tasks_approval 门可覆盖 → code_gen 按任务逐条实现（task_done 持久进度，
// unit_test 失败回环只重跑剩余任务）。
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/store"
)

// taskDoneKind 已完成任务的持久标记（append-only artifact，content = 1-based 任务序号；
// unit_test 回环 / 重启恢复后据此跳过已实现任务，进度不丢）。
const taskDoneKind = "task_done"

// tasksRe 从 agent 输出里提取 ```infera-tasks fenced block（JSON 数组）。
var tasksRe = regexp.MustCompile("```infera-tasks\\n([\\s\\S]*?)\\n```")

// ParseTasksBlock 解析 agent 输出里的任务清单 fenced block；容错约定：
// 无块 / 坏 JSON / 空数组 / 全空标题 → nil（调用方按无可执行清单处理），
// 空标题条目被过滤、其余保留。
func ParseTasksBlock(output string) []store.TaskSpec {
	m := tasksRe.FindStringSubmatch(output)
	if m == nil {
		return nil
	}
	var raw []store.TaskSpec
	if err := json.Unmarshal([]byte(m[1]), &raw); err != nil {
		return nil
	}
	out := make([]store.TaskSpec, 0, len(raw))
	for _, t := range raw {
		if strings.TrimSpace(t.Title) == "" {
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stepTasksAgent tasks 节点的产出处理：解析 fenced block 后以 JSON 落盘
// （kind=tasks 的 content 恒为清单 JSON，门禁页与 code_gen 消费同一份契约）。
// 畸形块容错为空清单：照常进门，人在门上可打回重列或直接覆盖清单。
func (e *Engine) stepTasksAgent(ctx context.Context, d *store.Delivery, res agent.Result) error {
	tasks := ParseTasksBlock(res.Output)
	if tasks == nil {
		tasks = []store.TaskSpec{}
	}
	content, err := json.Marshal(tasks)
	if err != nil {
		return err
	}
	return e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID,
		Stage:      "tasks",
		Kind:       "tasks",
		Content:    string(content),
	})
}

// stepCodeGen code_gen 节点执行：有非空 tasks artifact → 逐任务循环实现——
// 每个未完成任务一次 agent 调用（prompt = code_gen 基底 + 「当前任务 i/N」+ 反馈），
// 成功落 task_done 标记与事件；全部完成后存 kind=summary 摘要再前进。
// 无 tasks artifact（老数据）/ 空清单 → 单次整体实现，行为不变。
// 任务全部完成但带失败反馈（unit_test 回环 / 审查打回）→ 单次修复调用承载反馈，
// 避免「全完成 → unit_test 失败 → 无任务可跑」空转到 blocked。
func (e *Engine) stepCodeGen(ctx context.Context, d *store.Delivery, node Node, run *store.StageRun, ar agent.Runner, spec, feedback string) error {
	base := agent.BuildPrompt("code_gen", d.Description, spec, feedback)
	tasks, err := e.deliveryTasks(ctx, d.ID)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		// 老数据 / 空清单：单次整体实现（旧路径）。
		res, err := runAgent(ctx, ar, agent.Request{Role: node.Stage, Prompt: base, Workdir: e.ws.Path(d.ID)})
		if err != nil {
			return e.agentFailed(ctx, d, node, run, err)
		}
		if err := e.saveSummary(ctx, d, node.Stage, res.Output); err != nil {
			return err
		}
		e.finishStageRun(ctx, run.ID, "done")
		return e.advance(ctx, d, node.Next)
	}
	done, err := e.doneTasks(ctx, d.ID)
	if err != nil {
		return err
	}
	ran := 0
	for i, t := range tasks {
		idx := i + 1
		if done[idx] {
			continue // task_done 持久：回环 / 恢复只重跑剩余任务
		}
		prompt := fmt.Sprintf("%s\n\n当前任务 %d/%d：%s\n任务详情：%s", base, idx, len(tasks), t.Title, t.Detail)
		// 任务级输出不落盘；真 diff 由 code_review 门禁 Persist 产出。
		if _, err := runAgent(ctx, ar, agent.Request{Role: node.Stage, Prompt: prompt, Workdir: e.ws.Path(d.ID)}); err != nil {
			return e.agentFailed(ctx, d, node, run, err)
		}
		if err := e.st.SaveArtifact(ctx, &store.Artifact{
			DeliveryID: d.ID,
			Stage:      node.Stage,
			Kind:       taskDoneKind,
			Content:    strconv.Itoa(idx),
		}); err != nil {
			return err
		}
		e.emit(ctx, d, node.Stage, "task_done", map[string]any{"index": idx, "total": len(tasks), "title": t.Title})
		ran++
	}
	if ran == 0 && feedback != "" {
		// 无剩余任务但有失败反馈：单次修复调用（反馈已在 base prompt 里），
		// 摘要维持任务清单合成结果不变。
		if _, err := runAgent(ctx, ar, agent.Request{Role: node.Stage, Prompt: base, Workdir: e.ws.Path(d.ID)}); err != nil {
			return e.agentFailed(ctx, d, node, run, err)
		}
		e.finishStageRun(ctx, run.ID, "done")
		return e.advance(ctx, d, node.Next)
	}
	titles := make([]string, 0, len(tasks))
	for _, t := range tasks {
		titles = append(titles, t.Title)
	}
	if err := e.saveSummary(ctx, d, node.Stage, fmt.Sprintf("按任务清单完成 %d 项实现：%s", len(tasks), strings.Join(titles, "、"))); err != nil {
		return err
	}
	e.finishStageRun(ctx, run.ID, "done")
	return e.advance(ctx, d, node.Next)
}

// approveTasksOverride 「批准并覆盖任务清单」（Approve 单入口在 opts.Tasks 非空时路由到此；
// 仅 tasks_approval）：覆盖清单落为新 tasks artifact（append-only，最新生效）+ tasks_overridden
// 事件，随后按普通批准放行（同 split 编辑器模式：编辑即批准）。
func (e *Engine) approveTasksOverride(ctx context.Context, d *store.Delivery, tasks []store.TaskSpec) error {
	if d.PendingGate != "tasks_approval" {
		return fmt.Errorf("engine: tasks override is only allowed at tasks_approval, not %q", d.PendingGate)
	}
	normalized, err := normalizeTasks(tasks)
	if err != nil {
		return err
	}
	content, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	if err := e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID,
		Stage:      "tasks",
		Kind:       "tasks",
		Content:    string(content),
	}); err != nil {
		return err
	}
	e.emit(ctx, d, d.PendingGate, "tasks_overridden", map[string]int{"count": len(normalized)})
	return e.approveGate(ctx, d, Graph["tasks_approval"].Next)
}

// normalizeTasks 校验并规范化任务清单：标题去空白且非空。
func normalizeTasks(tasks []store.TaskSpec) ([]store.TaskSpec, error) {
	out := make([]store.TaskSpec, 0, len(tasks))
	for i, t := range tasks {
		t.Title = strings.TrimSpace(t.Title)
		if t.Title == "" {
			return nil, fmt.Errorf("engine: task %d has empty title", i)
		}
		out = append(out, t)
	}
	return out, nil
}

// saveSummary 落 code_gen 改动摘要 artifact。
func (e *Engine) saveSummary(ctx context.Context, d *store.Delivery, stage, content string) error {
	return e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID,
		Stage:      stage,
		Kind:       "summary",
		Content:    content,
	})
}

// deliveryTasks 交付的任务清单（最新 kind=tasks artifact，content 为 JSON）。
// 兼容旧格式（原始 agent 输出）：JSON 解析失败时回落 fenced block 解析。无 artifact → nil。
func (e *Engine) deliveryTasks(ctx context.Context, deliveryID string) ([]store.TaskSpec, error) {
	a, err := e.st.LatestArtifact(ctx, deliveryID, "tasks")
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []store.TaskSpec
	if err := json.Unmarshal([]byte(a.Content), &tasks); err != nil {
		tasks = ParseTasksBlock(a.Content)
	}
	return tasks, nil
}

// doneTasks 已完成任务序号集合（kind=task_done，content=1-based 序号）。
func (e *Engine) doneTasks(ctx context.Context, deliveryID string) (map[int]bool, error) {
	arts, err := e.st.ListArtifacts(ctx, deliveryID)
	if err != nil {
		return nil, err
	}
	out := map[int]bool{}
	for _, a := range arts {
		if a.Kind != taskDoneKind {
			continue
		}
		if i, err := strconv.Atoi(strings.TrimSpace(a.Content)); err == nil && i > 0 {
			out[i] = true
		}
	}
	return out, nil
}
