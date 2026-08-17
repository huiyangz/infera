// Package orchestration 实现 Agent 编排配置：可绑定节点清单（单一事实来源）、
// 绑定解析（项目覆盖 ?? 全局默认）、runner 工厂（按 agent.runner 构造执行器）。
package orchestration

import (
	"context"
	"errors"
	"fmt"
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
func RunnerFor(a store.Agent) (agent.Runner, error) {
	switch a.Runner {
	case "cli":
		argv, err := strSlice(a.Config, "command")
		if err != nil {
			return nil, err
		}
		if len(argv) == 0 {
			return nil, fmt.Errorf("agent %s: cli runner 需要 config.command", a.Name)
		}
		return agent.NewLocal(argv), nil
	case "docker":
		image, _ := a.Config["image"].(string)
		if image == "" {
			return nil, fmt.Errorf("agent %s: docker runner 需要 config.image", a.Name)
		}
		argv, err := strSlice(a.Config, "command")
		if err != nil {
			return nil, err
		}
		return agent.NewDocker(image, argv), nil
	case "http":
		url, _ := a.Config["url"].(string)
		if url == "" {
			return nil, fmt.Errorf("agent %s: http runner 需要 config.url", a.Name)
		}
		return agent.NewHTTP(url), nil
	case "local":
		return nil, ErrLocalRunner
	default:
		return nil, fmt.Errorf("agent %s: 未知 runner %q", a.Name, a.Runner)
	}
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
