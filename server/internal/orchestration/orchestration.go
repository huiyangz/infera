// Package orchestration 实现 Agent 编排配置：可绑定节点清单（单一事实来源）、
// 绑定解析（项目覆盖 ?? 全局默认）、runner 工厂（按 agent.runner 构造执行器）。
package orchestration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/store"
)

// BindableNodes 当前图上可绑定 agent 的节点（单一事实来源；design/tasks 在后续批次加入）。
var BindableNodes = []string{"spec", "test_gen", "code_gen", "code_review"}

// ErrLocalRunner runner=local 的哨兵：本机交互通道（批 B 实装），本批语义 = 交付停在该阶段。
var ErrLocalRunner = errors.New("orchestration: local runner（本机交互，批 B 实装；本批停在该阶段等待）")

// ErrIncompleteBindings 有效绑定不全的哨兵错误；Missing 列出缺绑定的节点。
type ErrIncompleteBindings struct{ Missing []string }

func (e *ErrIncompleteBindings) Error() string {
	return fmt.Sprintf("orchestration: 节点缺少有效 agent 绑定: %s", strings.Join(e.Missing, ", "))
}

// Effective 某节点的有效绑定：AgentID + 来源（default=全局默认 / project=项目覆盖）。
type Effective struct {
	Node    string `json:"node"`
	AgentID string `json:"agent_id"`
	From    string `json:"from"` // default|project
}

// Resolve 解析某项目的有效编排：agents 按节点给生效的 agent，eff 给来源。
// 任一可绑定节点缺绑定（或指向不存在的 agent）→ *ErrIncompleteBindings。
func Resolve(ctx context.Context, st store.Store, projectID string) (map[string]store.Agent, map[string]Effective, error) {
	defaults, err := st.ListBindings(ctx, "")
	if err != nil {
		return nil, nil, err
	}
	overrides, err := st.ListBindings(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	all, err := st.ListAgents(ctx)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]store.Agent, len(all))
	for _, a := range all {
		byID[a.ID] = a
	}

	defByNode := make(map[string]store.PipelineBinding, len(defaults))
	for _, b := range defaults {
		defByNode[b.Node] = b
	}
	ovByNode := make(map[string]store.PipelineBinding, len(overrides))
	for _, b := range overrides {
		ovByNode[b.Node] = b
	}

	agents := make(map[string]store.Agent, len(BindableNodes))
	eff := make(map[string]Effective, len(BindableNodes))
	var missing []string
	for _, node := range BindableNodes {
		b, from := ovByNode[node], "project"
		if b.AgentID == "" {
			b, from = defByNode[node], "default"
		}
		a, ok := byID[b.AgentID]
		if !ok {
			missing = append(missing, node)
			continue
		}
		agents[node] = a
		eff[node] = Effective{Node: node, AgentID: a.ID, From: from}
	}
	if len(missing) > 0 {
		return nil, nil, &ErrIncompleteBindings{Missing: missing}
	}
	return agents, eff, nil
}

// ValidateComplete 校验有效绑定覆盖全部可绑定节点（缺 → *ErrIncompleteBindings）。
func ValidateComplete(effective map[string]Effective) error {
	var missing []string
	for _, node := range BindableNodes {
		if _, ok := effective[node]; !ok {
			missing = append(missing, node)
		}
	}
	if len(missing) > 0 {
		return &ErrIncompleteBindings{Missing: missing}
	}
	return nil
}

// RunnerFor 按 agent.runner 构造执行器：
// cli → LocalRunner（config.command 为 argv；prompt 经 env/stdin 传入）；
// docker → DockerRunner（config.image + config.command）；
// http → HTTPRunner（config.url）；
// local → (nil, ErrLocalRunner)，引擎据此停车等待本机交互。
// 配置要求与 ValidateConfig 一致（同一份校验，两个消费时机）。
func RunnerFor(a store.Agent) (agent.Runner, error) {
	if err := ValidateConfig(a.Runner, a.Config); err != nil {
		return nil, fmt.Errorf("agent %s: %w", a.Name, err)
	}
	switch a.Runner {
	case "cli":
		argv, _ := strSlice(a.Config, "command")
		return agent.NewLocal(argv), nil
	case "docker":
		image, _ := a.Config["image"].(string)
		argv, _ := strSlice(a.Config, "command")
		return agent.NewDocker(image, argv), nil
	case "http":
		url, _ := a.Config["url"].(string)
		return agent.NewHTTP(url), nil
	case "local":
		return nil, ErrLocalRunner
	default:
		return nil, fmt.Errorf("agent %s: 未知 runner %q", a.Name, a.Runner)
	}
}

// ErrInvalidBinding 绑定保存校验失败（api 映射 400）；Message 可直接展示给用户。
type ErrInvalidBinding struct{ Message string }

func (e *ErrInvalidBinding) Error() string { return e.Message }

// ValidateConfig 校验 runner 与 config 匹配（agent 保存预校验，交付前暴露）：
// cli 必有非空 command、http 必有非空 url、docker 必有非空 image
// （command 可选但类型须合法）、local 无额外要求。
// 错误信息带字段名（如 "config.command 不能为空"）。
func ValidateConfig(runner string, config map[string]any) error {
	switch runner {
	case "cli":
		argv, err := strSlice(config, "command")
		if err != nil {
			return err
		}
		if len(argv) == 0 {
			return fmt.Errorf("config.command 不能为空（cli runner 需要命令数组）")
		}
	case "http":
		if url, _ := config["url"].(string); url == "" {
			return fmt.Errorf("config.url 不能为空（http runner 需要 URL）")
		}
	case "docker":
		if image, _ := config["image"].(string); image == "" {
			return fmt.Errorf("config.image 不能为空（docker runner 需要镜像）")
		}
		if _, err := strSlice(config, "command"); err != nil {
			return err
		}
	case "local":
		// 无额外要求
	default:
		return fmt.Errorf("未知 runner %q", runner)
	}
	return nil
}

// SaveBindings 校验并原子保存一组绑定（默认与项目级共用；projectID 空 = 全局默认）：
// 节点必须可绑定、agent 必须存在且配置合法——把交付期才会暴露的 blocked 提前到保存时。
// 校验失败 *ErrInvalidBinding；通过后走 store 单事务替换，任一步失败整体回滚（无半写）。
func SaveBindings(ctx context.Context, st store.Store, projectID string, bindings map[string]string) error {
	for node := range bindings {
		if !slices.Contains(BindableNodes, node) {
			return &ErrInvalidBinding{Message: "不可绑定的节点: " + node}
		}
	}
	agents, err := st.ListAgents(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]store.Agent, len(agents))
	for _, a := range agents {
		byID[a.ID] = a
	}
	for node, id := range bindings {
		a, ok := byID[id]
		if !ok {
			return &ErrInvalidBinding{Message: "节点 " + node + " 引用了不存在的 agent"}
		}
		if err := ValidateConfig(a.Runner, a.Config); err != nil {
			return &ErrInvalidBinding{Message: fmt.Sprintf("节点 %s 的 agent「%s」配置不合法: %v", node, a.Name, err)}
		}
	}
	return st.ReplaceBindings(ctx, projectID, bindings)
}

// strSlice 从 config 取字符串数组（JSON 反序列化出来是 []any）。
func strSlice(cfg map[string]any, key string) ([]string, error) {
	raw, ok := cfg[key]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("config.%s 应为字符串数组", key)
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			return nil, fmt.Errorf("config.%s 应为字符串数组", key)
		}
		out = append(out, s)
	}
	return out, nil
}
