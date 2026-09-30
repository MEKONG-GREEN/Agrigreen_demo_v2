// Ngày nghiệp vụ theo giờ Việt Nam (UTC+7, không có giờ mùa hè — R-12).
const VN_OFFSET_MS = 7 * 3_600_000;
const DATE = /^\d{4}-\d{2}-\d{2}$/;

/** Ngày YYYY-MM-DD theo giờ VN của thời điểm `now`. */
export function vnToday(now: Date = new Date()): string {
  return new Date(now.getTime() + VN_OFFSET_MS).toISOString().slice(0, 10);
}

export function isIsoDate(s: string): boolean {
  if (!DATE.test(s)) return false;
  const t = Date.parse(`${s}T00:00:00Z`);
  return Number.isFinite(t) && new Date(t).toISOString().slice(0, 10) === s;
}

/** Cộng/trừ ngày trên chuỗi YYYY-MM-DD (lịch, không phụ thuộc múi giờ máy). */
export function addDays(date: string, days: number): string {
  return new Date(Date.parse(`${date}T00:00:00Z`) + days * 86_400_000).toISOString().slice(0, 10);
}

/**
 * Cutoff(D) = 00:00 giờ VN ngày D+1, dạng ISO UTC (vd 2026-09-29 → 2026-09-29T17:00:00.000Z).
 * Giá trị/ảnh chụp ngày D là "như hệ biết lúc hết ngày D": chỉ tính bản ghi recorded_at < Cutoff(D).
 */
export function cutoffOf(date: string): string {
  return new Date(Date.parse(`${addDays(date, 1)}T00:00:00Z`) - VN_OFFSET_MS).toISOString();
}

/** Số ngày từ `a` tới `b` (b − a). */
export function daysBetween(a: string, b: string): number {
  return Math.round((Date.parse(`${b}T00:00:00Z`) - Date.parse(`${a}T00:00:00Z`)) / 86_400_000);
}
