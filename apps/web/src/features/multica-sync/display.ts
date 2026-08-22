/**
 * multica 同步数据的展示层翻译（T03 既定边界：assignee 是展示串
 * "type:id"（如 agent:<uuid>），姓名解析归前端展示层）。
 * 目前无姓名查询面，type 映射为角色词 + id 前 8 位保甄别度；
 * 后续出现姓名 API 时在此处升级，调用方无感。
 */
const ACTOR_LABELS: Record<string, string> = {
  agent: 'Agent',
  member: '成员',
  squad: 'Squad',
}

/** "agent:7bc775bc-…" → "Agent 7bc775bc"；空串/无冒点/空 id → 空串（不展示） */
export function assigneeLabel(assignee: string): string {
  const sep = assignee.indexOf(':')
  if (sep <= 0) return ''
  const typ = assignee.slice(0, sep)
  const id = assignee.slice(sep + 1)
  if (!id) return ''
  return `${ACTOR_LABELS[typ] ?? typ} ${id.slice(0, 8)}`
}
