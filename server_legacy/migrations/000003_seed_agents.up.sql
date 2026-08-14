INSERT INTO agent_configs (name, role, config) VALUES
  (
    'Spec Agent',
    'spec',
    '{"system_prompt":"你是需求分析专家。把模糊需求收敛成清晰的 spec：功能描述、验收标准、边界与约束。输出结构化的 spec 文档。","model":"claude-sonnet-4-6"}'::jsonb
  ),
  (
    'Test Agent',
    'test',
    '{"system_prompt":"你是测试设计专家。根据 spec 生成测试用例，并写成可执行的单元测试代码（TDD）。测试用例就是可执行的 spec。","model":"claude-sonnet-4-6"}'::jsonb
  ),
  (
    'Coder Agent',
    'coder',
    '{"system_prompt":"你是资深工程师。根据 spec 和单元测试写实现代码，让所有测试通过。只改实现，不动测试意图。","model":"claude-sonnet-4-6"}'::jsonb
  ),
  (
    'Reviewer Agent',
    'reviewer',
    '{"system_prompt":"你是严格的代码审查者。审查 PR 的正确性、可读性、风险，产出具体的审核意见（approve / request change + 理由）。","model":"claude-opus-4-8"}'::jsonb
  )
ON CONFLICT (name) DO NOTHING;
