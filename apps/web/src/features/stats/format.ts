/**
 * 累计时长人话格式（数字卡片与 tooltip 共用口径）：
 * <1 分钟 → 秒；<1 小时 → 分钟（四舍五入）；≥1 小时 → 小时（1 位小数）。
 * 0/负数/非有限值 → 0 秒（空窗口不该出现负时长）。
 */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '0 秒'
  if (ms < 60_000) return `${Math.max(1, Math.round(ms / 1000))} 秒`
  if (ms < 3_600_000) return `${Math.round(ms / 60_000)} 分钟`
  return `${(ms / 3_600_000).toFixed(1)} 小时`
}
