// Tính tuần công tác (FN-XA-03.08, FN-TI-02.07): tuần bắt đầu Thứ Hai, theo giờ Việt Nam (Asia/Ho_Chi_Minh,
// UTC+7 cố định, không có giờ mùa hè). Ngày biểu diễn dạng "YYYY-MM-DD" (format: date của hợp đồng).
// Hàm thuần, dùng chung cho màn hình và backend giả lập.

const VN_OFFSET_MS = 7 * 3_600_000;
const DAY_MS = 86_400_000;

/** Hạn đăng ký mặc định: 00:00 Thứ Hai − 55 giờ = 17:00 Thứ Sáu tuần trước (ExtWorkScheduleSettings). */
export const DEFAULT_SUBMIT_DUE_OFFSET_HOURS = -55;
/** Hạn duyệt mặc định: 00:00 Thứ Hai của tuần. */
export const DEFAULT_REVIEW_DUE_OFFSET_HOURS = 0;

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

function parts(date: string): [number, number, number] {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  if (!m) throw new Error(`Ngày không hợp lệ: ${date}`);
  return [Number(m[1]), Number(m[2]), Number(m[3])];
}

export function isIsoDate(v: unknown): v is string {
  if (typeof v !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(v)) return false;
  const [y, mo, d] = parts(v);
  const t = new Date(Date.UTC(y, mo - 1, d));
  return t.getUTCFullYear() === y && t.getUTCMonth() === mo - 1 && t.getUTCDate() === d;
}

/** Ngày lịch Việt Nam của một thời điểm. */
export function vnDate(ms: number): string {
  const t = new Date(ms + VN_OFFSET_MS);
  return `${t.getUTCFullYear()}-${pad(t.getUTCMonth() + 1)}-${pad(t.getUTCDate())}`;
}

export function addDays(date: string, n: number): string {
  const [y, m, d] = parts(date);
  const t = new Date(Date.UTC(y, m - 1, d) + n * DAY_MS);
  return `${t.getUTCFullYear()}-${pad(t.getUTCMonth() + 1)}-${pad(t.getUTCDate())}`;
}

/** 0 = Thứ Hai … 6 = Chủ Nhật. */
export function weekdayIndex(date: string): number {
  const [y, m, d] = parts(date);
  return (new Date(Date.UTC(y, m - 1, d)).getUTCDay() + 6) % 7;
}

export function mondayOf(date: string): string {
  return addDays(date, -weekdayIndex(date));
}

export function isMonday(date: string): boolean {
  return isIsoDate(date) && weekdayIndex(date) === 0;
}

/** Thứ Hai của tuần hiện tại (giờ Việt Nam). */
export function currentWeekStart(now: number = Date.now()): string {
  return mondayOf(vnDate(now));
}

/** Tuần kế tiếp — mặc định của màn đăng ký (FN-XA-03.08) và danh sách duyệt. */
export function nextWeekStart(now: number = Date.now()): string {
  return addDays(currentWeekStart(now), 7);
}

export function weekEnd(weekStart: string): string {
  return addDays(weekStart, 6);
}

export function weekDays(weekStart: string): string[] {
  return Array.from({ length: 7 }, (_, i) => addDays(weekStart, i));
}

export function inWeek(weekStart: string, date: string): boolean {
  return isIsoDate(date) && mondayOf(date) === weekStart;
}

/** 00:00 giờ Việt Nam của một ngày, theo ms UTC. */
export function vnMidnight(date: string): number {
  const [y, m, d] = parts(date);
  return Date.UTC(y, m - 1, d) - VN_OFFSET_MS;
}

export function submitDueAt(weekStart: string, offsetHours = DEFAULT_SUBMIT_DUE_OFFSET_HOURS): string {
  return new Date(vnMidnight(weekStart) + offsetHours * 3_600_000).toISOString();
}

export function reviewDueAt(weekStart: string, offsetHours = DEFAULT_REVIEW_DUE_OFFSET_HOURS): string {
  return new Date(vnMidnight(weekStart) + offsetHours * 3_600_000).toISOString();
}

/** Tuần đã kết thúc (sau 24:00 Chủ Nhật giờ Việt Nam) → khóa, chỉ xem. */
export function isWeekLocked(weekStart: string, now: number = Date.now()): boolean {
  return now >= vnMidnight(addDays(weekStart, 7));
}

// ---------------------------------------------------------------------------
// Nhãn hiển thị
// ---------------------------------------------------------------------------
const SHORT_DAY = ["T2", "T3", "T4", "T5", "T6", "T7", "CN"];
const LONG_DAY = ["Thứ Hai", "Thứ Ba", "Thứ Tư", "Thứ Năm", "Thứ Sáu", "Thứ Bảy", "Chủ Nhật"];

/** "08/09" */
export function dayMonth(date: string): string {
  const [, m, d] = parts(date);
  return `${pad(d)}/${pad(m)}`;
}

/** "T2 08/09" — tiêu đề cột lưới tuần (Hình 12, 21). */
export function dayColumnLabel(date: string): string {
  return `${SHORT_DAY[weekdayIndex(date)]} ${dayMonth(date)}`;
}

export function longDayName(date: string): string {
  return LONG_DAY[weekdayIndex(date)] ?? "";
}

/** "08/09 – 14/09/2026" */
export function weekRangeLabel(weekStart: string): string {
  const end = weekEnd(weekStart);
  const [y] = parts(end);
  return `${dayMonth(weekStart)} – ${dayMonth(end)}/${y}`;
}

/** Thời điểm theo giờ Việt Nam: "Thứ Sáu 12/09 17:00" (hạn đăng ký, Hình 21). */
export function vnDeadlineLabel(iso: string): string {
  const ms = Date.parse(iso);
  if (!Number.isFinite(ms)) return "—";
  const date = vnDate(ms);
  const t = new Date(ms + VN_OFFSET_MS);
  return `${longDayName(date)} ${dayMonth(date)} ${pad(t.getUTCHours())}:${pad(t.getUTCMinutes())}`;
}
