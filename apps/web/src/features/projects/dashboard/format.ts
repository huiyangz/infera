/**
 * dashboard 专用展示格式化：执行耗时与时序横轴刻度。
 * 中文紧凑口径，与 DESIGN.md 的中性文案一致（不引入单位符号混排）。
 */

/** 毫秒 → 中文紧凑时长；null/非有限/负数（未收尾、无样本）给「—」占位 */
export function formatDuration(
  ms: number | null | undefined
): string {
  if (ms == null || !Number.isFinite(ms) || ms < 0) return '—'
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s} 秒`
  const m = Math.floor(s / 60)
  if (m < 60) {
    const rest = s % 60
    return rest ? `${m} 分 ${rest} 秒` : `${m} 分`
  }
  const h = Math.floor(m / 60)
  if (h < 24) {
    const rest = m % 60
    return rest ? `${h} 时 ${rest} 分` : `${h} 时`
  }
  const d = Math.floor(h / 24)
  const restH = h % 24
  return restH ? `${d} 天 ${restH} 时` : `${d} 天`
}

/** 时序横轴刻度：跨度 ≤ 24h 只显 HH:mm，跨天显 MM-DD HH:mm（本地时区） */
export function axisTime(ts: number, spanMs: number): string {
  const d = new Date(ts)
  const pad = (n: number) => String(n).padStart(2, '0')
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  if (spanMs <= 24 * 3600_000) return hm
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${hm}`
}
