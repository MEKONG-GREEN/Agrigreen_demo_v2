export type SlaTone = "ok" | "warn" | "bad" | "done";

export interface SlaState {
  overdue: boolean;
  remainingMs: number;
  label: string;
  tone: SlaTone;
}

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

/** "2 ngày 3 giờ", "5 giờ 12 phút", "8 phút". */
export function formatDuration(ms: number): string {
  const abs = Math.abs(ms);
  const d = Math.floor(abs / DAY);
  const h = Math.floor((abs % DAY) / HOUR);
  const m = Math.floor((abs % HOUR) / MIN);
  if (d > 0) return h > 0 ? `${d} ngày ${h} giờ` : `${d} ngày`;
  if (h > 0) return m > 0 ? `${h} giờ ${m} phút` : `${h} giờ`;
  return `${Math.max(m, abs > 0 ? 1 : 0)} phút`;
}

/**
 * Trạng thái SLA của nhiệm vụ. Cảnh báo (warn) khi còn dưới `warnMs` (mặc định 4 giờ).
 * Nhiệm vụ đã xử lý: so thời điểm xử lý với hạn.
 */
export function slaState(dueAt: string, now: number, resolvedAt?: string | null, warnMs = 4 * HOUR): SlaState {
  const due = Date.parse(dueAt);
  if (resolvedAt) {
    const done = Date.parse(resolvedAt);
    const late = done > due;
    return {
      overdue: late,
      remainingMs: due - done,
      label: late ? `Xử lý trễ ${formatDuration(done - due)}` : "Đúng hạn",
      tone: "done",
    };
  }
  const remaining = due - now;
  if (remaining < 0) {
    return { overdue: true, remainingMs: remaining, label: `Quá hạn ${formatDuration(remaining)}`, tone: "bad" };
  }
  return {
    overdue: false,
    remainingMs: remaining,
    label: `Còn ${formatDuration(remaining)}`,
    tone: remaining <= warnMs ? "warn" : "ok",
  };
}
