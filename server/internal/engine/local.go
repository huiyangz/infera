// 本机交互通道的引擎侧落点（R3 / MCP submit_stage_output 的支撑）：
// local 绑定节点的停车（stepAgent 的 ErrLocalRunner 分支）在这里被交回——
// 按节点约定写 artifact + advance，或门禁前置审查的预审产物。
// 与 Approve 一样只做簿记不驱动，后续推进由调用方异步点火。
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/orchestration"
	"github.com/tokfinity/infera/internal/store"
)

// SubmitLocal 本机交互通道的交回入口。两种合法形态：
//  1. 交付停在本机绑定的 agent 节点（local 停车）：按节点约定落产物后 advance
//     （tasks 节点先解析 fenced block 为清单 JSON；code_gen 带清单时补齐剩余
//     task_done 标记再落 summary——与引擎驱动的产物契约完全一致）；
//  2. 交付挂在门禁且门禁前置审查（ReviewRole，如 code_review）绑定本机：
//     落 agent_output 预审产物，门禁不动（放行/打回仍走 Approve/Reject 单入口）。
//
// 其余情况（非 active / 节点未绑定本机 / 拆分父停在 code_gen 的合并等待语义）
// 一律报错且不改任何状态。后续推进不由本方法驱动——调用方照 approve 的约定
// 异步点火（api 层 RunDelivery 拿 per-delivery 锁驱动到下一个停车点）。
func (e *Engine) SubmitLocal(ctx context.Context, deliveryID, output string) error {
	d, err := e.st.GetDelivery(ctx, deliveryID)
	if err != nil {
		return err
	}
	if d.Status != StatusActive {
		return fmt.Errorf("engine: delivery %s is %s, not active", d.ID, d.Status)
	}
	if d.PendingGate != "" {
		return e.submitLocalReview(ctx, d, output)
	}
	return e.submitLocalStage(ctx, d, output)
}

// submitLocalStage 形态 1：本机停车节点的产出交回。
func (e *Engine) submitLocalStage(ctx context.Context, d *store.Delivery, output string) error {
	// 拆分父停在 code_gen 是「等子需求 / 合并」语义（run 路由 MaybeDriveParent），
	// 不是本机交互停车，交回通道必须拒绝。
	if d.SplitMode && d.CurrentStage == "code_gen" {
		return fmt.Errorf("engine: 拆分父 %s 停在 code_gen 是合并等待语义，不走本机交回", d.ID)
	}
	node, ok := Graph[d.CurrentStage]
	if !ok {
		return fmt.Errorf("engine: unknown stage %q", d.CurrentStage)
	}
	if node.Kind != KindAgent {
		return fmt.Errorf("engine: stage %q 不是 agent 节点，无本机交回语义", d.CurrentStage)
	}
	if _, err := e.runnerFor(ctx, d, node.Stage); !errors.Is(err, orchestration.ErrLocalRunner) {
		return fmt.Errorf("engine: 节点 %s 未绑定本机 runner（绑定的是可执行 agent，由引擎驱动）", node.Stage)
	}
	switch node.Stage {
	case "tasks":
		// 与 stepTasksAgent 同一契约：fenced block 解析为清单 JSON 落盘
		//（畸形块容错为空清单，人在门上可打回或覆盖）。
		tasks := ParseTasksBlock(output)
		if tasks == nil {
			tasks = []store.TaskSpec{}
		}
		content, err := json.Marshal(tasks)
		if err != nil {
			return err
		}
		if err := e.st.SaveArtifact(ctx, &store.Artifact{
			DeliveryID: d.ID, Stage: node.Stage, Kind: node.ArtifactKind, Content: string(content),
		}); err != nil {
			return err
		}
	case "code_gen":
		if err := e.submitLocalCodeGen(ctx, d, node, output); err != nil {
			return err
		}
	default:
		if err := e.st.SaveArtifact(ctx, &store.Artifact{
			DeliveryID: d.ID, Stage: node.Stage, Kind: node.ArtifactKind, Content: output,
		}); err != nil {
			return err
		}
	}
	if err := e.advance(ctx, d, node.Next); err != nil {
		return err
	}
	e.emit(ctx, d, node.Stage, "local_stage_submitted", map[string]string{"node": node.Stage})
	return nil
}

// submitLocalCodeGen code_gen 本机交回：本机执行者是「一口气在 workdir 里完成全部
// 剩余任务」的语义——补齐剩余 task_done 标记（unit_test 回环 / 恢复的进度契约），
// 交回内容作为 summary 落盘。无清单（老数据 / small 路径）时只落 summary。
func (e *Engine) submitLocalCodeGen(ctx context.Context, d *store.Delivery, node Node, output string) error {
	tasks, err := e.deliveryTasks(ctx, d.ID)
	if err != nil {
		return err
	}
	if len(tasks) > 0 {
		done, err := e.doneTasks(ctx, d.ID)
		if err != nil {
			return err
		}
		for i := range tasks {
			idx := i + 1
			if done[idx] {
				continue
			}
			if err := e.st.SaveArtifact(ctx, &store.Artifact{
				DeliveryID: d.ID, Stage: node.Stage, Kind: taskDoneKind, Content: strconv.Itoa(idx),
			}); err != nil {
				return err
			}
			e.emit(ctx, d, node.Stage, "task_done", map[string]any{"index": idx, "total": len(tasks), "title": tasks[i].Title})
		}
	}
	return e.saveSummary(ctx, d, node.Stage, output)
}

// submitLocalReview 形态 2：门禁前置审查（ReviewRole）绑定本机时的预审产物交回。
// 只落 agent_output 供门禁页 / get_gate 展示，门禁本身不动。
func (e *Engine) submitLocalReview(ctx context.Context, d *store.Delivery, output string) error {
	node, ok := Graph[d.PendingGate]
	if !ok {
		return fmt.Errorf("engine: unknown gate stage %q", d.PendingGate)
	}
	if node.ReviewRole == "" {
		return fmt.Errorf("engine: 门禁 %s 无前置审查角色，本机交回不适用（放行走 Approve）", d.PendingGate)
	}
	if _, err := e.runnerFor(ctx, d, node.ReviewRole); !errors.Is(err, orchestration.ErrLocalRunner) {
		return fmt.Errorf("engine: 审查节点 %s 未绑定本机 runner", node.ReviewRole)
	}
	return e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID, Stage: node.Stage, Kind: "agent_output", Content: output,
	})
}

// LocalPrompt 只读组装本机停车节点的角色 prompt（MCP get_context 的驾驶上下文），
// 返回 (角色名, prompt)。本机执行者拿到的指令与引擎发给绑定 agent 的完全同源
// （同一模板 / spec 注入 / 反馈）。反馈为只读镜像（人打回意见 / 上轮测试输出），
// 不消费——真正的消费发生在下一次引擎驱动。
// 非本机停车（非 active / 挂门禁且无本机审查 / 节点未绑定本机 / 拆分父）返回 ("", "")。
func (e *Engine) LocalPrompt(ctx context.Context, deliveryID string) (string, string, error) {
	d, err := e.st.GetDelivery(ctx, deliveryID)
	if err != nil {
		return "", "", err
	}
	if d.Status != StatusActive {
		return "", "", nil
	}
	role := ""
	if d.PendingGate != "" {
		if node, ok := Graph[d.PendingGate]; ok && node.ReviewRole != "" {
			role = node.ReviewRole
		}
	} else if !(d.SplitMode && d.CurrentStage == "code_gen") {
		if node, ok := Graph[d.CurrentStage]; ok && node.Kind == KindAgent {
			role = node.Stage
		}
	}
	if role == "" {
		return "", "", nil
	}
	if _, err := e.runnerFor(ctx, d, role); !errors.Is(err, orchestration.ErrLocalRunner) {
		return "", "", nil
	}
	spec, err := e.latestSpec(ctx, d.ID)
	if err != nil {
		return "", "", err
	}
	prompt := agent.BuildPrompt(role, d.Description, spec, e.readonlyFeedback(d, role))
	addenda, err := e.promptAddenda(ctx, d, role)
	if err != nil {
		return "", "", err
	}
	return role, prompt + addenda, nil
}

// readonlyFeedback stageFeedback 的只读镜像（不消费 RejectReason）：
// spec/design/tasks 看人打回意见；code_gen 额外带上轮 unit_test 失败输出。
func (e *Engine) readonlyFeedback(d *store.Delivery, stage string) string {
	fb := ""
	if d.RejectReason != "" {
		fb = "人打回：" + d.RejectReason
	}
	if stage != "code_gen" || d.FailCount == 0 {
		return fb
	}
	out, err := e.latestTestOutput(context.Background(), d.ID)
	if err != nil || out == "" {
		return fb
	}
	if fb != "" {
		fb += "\n"
	}
	return fb + "上一轮 unit_test 未过：" + out
}

// splitPlanRe 从 spec/design 全文里提取 ```infera-split fenced block（AI 的拆分建议）。
var splitPlanRe = regexp.MustCompile("```infera-split\\n([\\s\\S]*?)\\n```")

// ParseSplitPlan 解析拆分建议；无 block / 解析失败返回 nil（按无建议处理）。
// （与 api 门禁页的展示解析同源；engine 导出供 MCP get_gate 复用。）
func ParseSplitPlan(spec string) []store.ChildSpec {
	m := splitPlanRe.FindStringSubmatch(spec)
	if m == nil {
		return nil
	}
	var plan []store.ChildSpec
	if err := json.Unmarshal([]byte(m[1]), &plan); err != nil {
		return nil
	}
	return plan
}
