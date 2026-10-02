// BẢN PORT TS (CHỈ CHO BACKEND GIẢ LẬP) của internal/platform/extension/reportcycle/domain (P4): chia kỳ, hạn nộp,
// trạng thái công việc, so khác lần tổng hợp, tỉ lệ/năng suất, lý do. Đối chiếu ca vàng do Go sinh
// (fixtures/rc-golden.json, TestWebReportCycleFixture) ở domain.test.ts — sửa ở Go trước, rồi sinh lại ca vàng.
import { Decimal } from "@/kn/grid/decimal";

export type PeriodType = "week" | "month" | "season" | "year" | "adhoc";
export type ReportType = "weekly" | "monthly" | "seasonal" | "annual" | "adhoc";

export const REPORT_PERIOD_TYPE: Record<ReportType, PeriodType> = { weekly: "week", monthly: "month", seasonal: "season", annual: "year", adhoc: "adhoc" };

export const RULE_VERSION = "kn.period_rule/v1-tam";
export type WeekNumbering = "season" | "iso";
export const ruleTag = (numbering: WeekNumbering) => `${RULE_VERSION};week=${numbering}`;

export interface Deadline {
  offset_days: number;
  time: string;
}

export function defaultDeadline(pt: string): Deadline {
  if (pt === "week") return { offset_days: 2, time: "17:00" };
  if (pt === "month") return { offset_days: 10, time: "17:00" };
  return { offset_days: 7, time: "17:00" };
}

export const DEFAULT_CLOSE_AFTER_DAYS = 3;
export const MAX_PERIODS = 60;
export const MIN_REASON = 10;

/** Giờ Việt Nam UTC+7 (không có giờ mùa hè). */
const VN_OFFSET_MS = 7 * 3_600_000;

export interface SplitPeriod {
  slot: number;
  label: string;
  range_label: string;
  iso_week: number;
  from_date: string;
  to_date: string;
  due_at: string;
  auto_close_at: string;
}

export class PeriodError extends Error {
  constructor(
    readonly field: string,
    readonly msg: string,
  ) {
    super(`${field}: ${msg}`);
  }
}

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

/** "YYYY-MM-DD" → ms UTC 00:00; sai → null. */
export function parseDate(s: string): number | null {
  const v = s.trim();
  if (!DATE_RE.test(v)) return null;
  const [y, m, d] = v.split("-").map(Number) as [number, number, number];
  const t = Date.UTC(y, m - 1, d);
  const dt = new Date(t);
  return dt.getUTCFullYear() === y && dt.getUTCMonth() === m - 1 && dt.getUTCDate() === d ? t : null;
}

export function parseClock(s: string): [number, number] | null {
  const m = /^(\d{2}):(\d{2})$/.exec(s);
  if (!m) return null;
  const hh = Number(m[1]);
  const mm = Number(m[2]);
  return hh <= 23 && mm <= 59 ? [hh, mm] : null;
}

const DAY = 86_400_000;
const dateStr = (t: number) => new Date(t).toISOString().slice(0, 10);
const pad2 = (n: number) => String(n).padStart(2, "0");
/** RFC3339 UTC không phần lẻ giây (như Go reportcycle). */
export const rfc3339 = (t: number) => new Date(t).toISOString().replace(/\.\d{3}Z$/, "Z");

function ddmm(t: number): string {
  const d = new Date(t);
  return `${pad2(d.getUTCDate())}/${pad2(d.getUTCMonth() + 1)}`;
}

export function rangeLabel(f: number, t: number): string {
  const fy = new Date(f).getUTCFullYear();
  const ty = new Date(t).getUTCFullYear();
  return fy === ty ? `${ddmm(f)}–${ddmm(t)}/${ty}` : `${ddmm(f)}/${fy}–${ddmm(t)}/${ty}`;
}

/** Tuần ISO 8601 của ngày (ms UTC 00:00). */
export function isoWeek(t: number): number {
  const d = new Date(t);
  const wd = (d.getUTCDay() + 6) % 7; // thứ Hai = 0
  const thursday = t - wd * DAY + 3 * DAY;
  const y = new Date(thursday).getUTCFullYear();
  const jan4 = Date.UTC(y, 0, 4);
  const jan4wd = (new Date(jan4).getUTCDay() + 6) % 7;
  const week1Mon = jan4 - jan4wd * DAY;
  return Math.floor((thursday - week1Mon) / (7 * DAY)) + 1;
}

/** Hạn nộp UTC (ms) của kỳ kết thúc ngày `to`: to + offset ngày lúc HH:MM giờ Việt Nam. */
export function dueAt(to: number, d: Deadline): number {
  const c = parseClock(d.time);
  if (!c) throw new PeriodError("deadline.time", "giờ phải dạng HH:MM");
  return to + d.offset_days * DAY + c[0] * 3_600_000 + c[1] * 60_000 - VN_OFFSET_MS;
}

/** Như domain.Split. */
export function split(pt: string, fromS: string, toS: string, season: string, numbering: WeekNumbering, d: Deadline, closeAfter: number): SplitPeriod[] {
  const from = parseDate(fromS);
  const to = parseDate(toS);
  if (from === null) throw new PeriodError("from_date", "ngày phải dạng YYYY-MM-DD");
  if (to === null) throw new PeriodError("to_date", "ngày phải dạng YYYY-MM-DD");
  if (to < from) throw new PeriodError("to_date", "phải không trước from_date");
  if (closeAfter < 0 || closeAfter > 60) throw new PeriodError("close_after_days", "0–60 ngày");
  if (d.offset_days < 0 || d.offset_days > 60) throw new PeriodError("deadline.offset_days", "0–60 ngày");
  if (!parseClock(d.time)) throw new PeriodError("deadline.time", "giờ phải dạng HH:MM");
  const out: SplitPeriod[] = [];
  const add = (f: number, t: number, label: string, iso: number) => {
    if (out.length >= MAX_PERIODS) throw new PeriodError("to_date", `tối đa ${MAX_PERIODS} kỳ mỗi đợt`);
    const due = dueAt(t, d);
    out.push({
      slot: out.length + 1,
      label,
      range_label: rangeLabel(f, t),
      iso_week: iso,
      from_date: dateStr(f),
      to_date: dateStr(t),
      due_at: rfc3339(due),
      auto_close_at: rfc3339(due + closeAfter * DAY),
    });
  };
  switch (pt) {
    case "week": {
      const start = from - ((new Date(from).getUTCDay() + 6) % 7) * DAY;
      for (let ws = start; ws <= to; ws += 7 * DAY) {
        const f = Math.max(ws, from);
        const t = Math.min(ws + 6 * DAY, to);
        const iso = isoWeek(f);
        add(f, t, `Tuần ${numbering === "iso" ? iso : out.length + 1}`, iso);
      }
      break;
    }
    case "month": {
      const fd = new Date(from);
      for (let ms = Date.UTC(fd.getUTCFullYear(), fd.getUTCMonth(), 1); ms <= to; ) {
        const md = new Date(ms);
        const next = Date.UTC(md.getUTCFullYear(), md.getUTCMonth() + 1, 1);
        add(Math.max(ms, from), Math.min(next - DAY, to), `Tháng ${md.getUTCMonth() + 1}/${md.getUTCFullYear()}`, 0);
        ms = next;
      }
      break;
    }
    case "season":
    case "adhoc":
      add(from, to, season, 0);
      break;
    case "year":
      add(from, to, season || `Năm ${new Date(from).getUTCFullYear()}`, 0);
      break;
    default:
      throw new PeriodError("period_type", "week | month | season | year | adhoc");
  }
  return out;
}

// ---------------------------------------------------------------------------
// Trạng thái công việc (domain/task.go)
// ---------------------------------------------------------------------------
export type StoredStatus = "not_started" | "draft" | "submitted" | "adjustment_requested" | "confirmed";
export type DisplayStatus = StoredStatus | "overdue" | "closed";

export const STATUS_LABEL: Record<DisplayStatus, string> = {
  not_started: "Chưa nhập",
  draft: "Lưu nháp",
  submitted: "Đã nộp",
  overdue: "Trễ hạn",
  adjustment_requested: "Yêu cầu điều chỉnh",
  confirmed: "Đã xác nhận",
  closed: "Đã chốt",
};

export const DISPLAY_STATUSES: DisplayStatus[] = ["not_started", "draft", "submitted", "overdue", "adjustment_requested", "confirmed", "closed"];

export interface TaskState {
  status: StoredStatus;
  dueAt: number;
  periodClosed: boolean;
  reopenedUntil: number | null;
}

export function taskOpen(s: TaskState, now: number): boolean {
  return !s.periodClosed || (s.reopenedUntil !== null && now < s.reopenedUntil);
}

export function taskDisplay(s: TaskState, now: number): DisplayStatus {
  if (!taskOpen(s, now)) return "closed";
  if ((s.status === "not_started" || s.status === "draft") && now >= s.dueAt) return "overdue";
  return s.status;
}

export function taskPending(s: Pick<TaskState, "status">): boolean {
  return s.status === "not_started" || s.status === "draft" || s.status === "adjustment_requested";
}

/** Lý do bắt buộc 10–max ký tự (đếm theo ký tự Unicode, sau khi bỏ khoảng trắng hai đầu). */
export function reason(s: string, max: number): [string, boolean] {
  const v = s.trim();
  const n = [...v].length;
  return [v, n >= MIN_REASON && n <= max];
}

// ---------------------------------------------------------------------------
// So khác lần tổng hợp (domain/consolidate.go)
// ---------------------------------------------------------------------------
export type ChangeKind = "new" | "resubmit" | "adjusted" | "computed";

export interface Source {
  submission_id: string;
  version_no: number;
  submitted_at: string;
  actor_ref: string;
  on_behalf_of?: string | null;
  adjustment_response: boolean;
}

export interface Row {
  key: string;
  commune: string;
  region: string;
  indicator?: string;
  cells: Record<string, string>;
}

export interface CellChange {
  commune: string;
  row_key: string;
  letter: string;
  old: string | null;
  new: string | null;
  kind: ChangeKind;
  source: Source | null;
}

export function letterLess(a: string, b: string): number {
  if (a.length !== b.length) return a.length - b.length;
  return a < b ? -1 : a > b ? 1 : 0;
}

export function sameValue(a: string | null, b: string | null): boolean {
  if (a === null || b === null) return a === null && b === null;
  if (a === b) return true;
  const da = Decimal.parse(a);
  const db = Decimal.parse(b);
  return !!da && !!db && da.eq(db);
}

const goLess = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);

export function diff(prev: Row[], next: Row[], prevSrc: Record<string, Source>, nextSrc: Record<string, Source>, formula: ReadonlySet<string>): [CellChange[], number] {
  const pm = new Map(prev.map((r) => [r.key, r]));
  const nm = new Map(next.map((r) => [r.key, r]));
  const keys = [...new Set([...pm.keys(), ...nm.keys()])].sort(goLess);
  const out: CellChange[] = [];
  const communes = new Set<string>();
  for (const k of keys) {
    const p = pm.get(k);
    const n = nm.get(k);
    const commune = n ? n.commune : p!.commune;
    const hadSource = commune in prevSrc;
    const src = nextSrc[commune];
    const letters = [...new Set([...Object.keys(p?.cells ?? {}), ...Object.keys(n?.cells ?? {})])].sort(letterLess);
    for (const l of letters) {
      const ov = p?.cells[l] ? p.cells[l]! : null;
      const nv = n?.cells[l] ? n.cells[l]! : null;
      if (sameValue(ov, nv)) continue;
      let kind: ChangeKind = "resubmit";
      if (formula.has(l)) kind = "computed";
      else if (!hadSource) kind = "new";
      else if (src?.adjustment_response) kind = "adjusted";
      out.push({ commune, row_key: k, letter: l, old: ov, new: nv, kind, source: src ?? null });
      communes.add(commune);
    }
  }
  return [out, communes.size];
}

/** a / b × 100 làm tròn 1 chữ số (0 nếu b = 0). */
export function ratio(a: Decimal, b: Decimal): Decimal {
  if (b.isZero()) return Decimal.ZERO;
  return a.mul(Decimal.fromInt(100)).divRound(b, 1);
}

/** Sản lượng / diện tích làm tròn 2 chữ số (bình quân gia quyền). */
export function yieldOf(output: Decimal, area: Decimal): Decimal {
  if (area.isZero()) return Decimal.ZERO;
  return output.divRound(area, 2);
}

export const dec = (s: string | null | undefined): Decimal => (s ? (Decimal.parse(s.trim()) ?? Decimal.ZERO) : Decimal.ZERO);

/** "HH:MM dd/MM/yyyy" giờ Việt Nam. */
export function vnClock(t: number): string {
  const d = new Date(t + VN_OFFSET_MS);
  return `${pad2(d.getUTCHours())}:${pad2(d.getUTCMinutes())} ${pad2(d.getUTCDate())}/${pad2(d.getUTCMonth() + 1)}/${d.getUTCFullYear()}`;
}

/** Thứ Hai (ms UTC 00:00) của tuần chứa thời điểm t theo giờ Việt Nam. */
export function mondayOf(t: number): number {
  const d = new Date(t + VN_OFFSET_MS);
  const day = Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate());
  return day - ((new Date(day).getUTCDay() + 6) % 7) * DAY;
}

export const ymd = dateStr;
export const DAY_MS = DAY;
