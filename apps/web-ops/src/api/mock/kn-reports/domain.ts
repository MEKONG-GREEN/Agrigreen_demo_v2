// BẢN PORT TS (CHỈ CHO BACKEND GIẢ LẬP) của internal/platform/extension/reportcycle/domain (P4): chia kỳ, hạn nộp,
// trạng thái công việc, so khác lần tổng hợp, tỉ lệ/năng suất, lý do. Đối chiếu ca vàng do Go sinh
// (fixtures/rc-golden.json, TestWebReportCycleFixture) ở domain.test.ts — sửa ở Go trước, rồi sinh lại ca vàng.
import { Decimal } from "@/kn/grid/decimal";

export type PeriodType = "day" | "week" | "ksgm_week" | "month" | "quarter" | "season" | "year" | "adhoc";
export type ReportType = "weekly" | "monthly" | "seasonal" | "annual" | "adhoc";

export const REPORT_PERIOD_TYPE: Record<ReportType, PeriodType> = { weekly: "week", monthly: "month", seasonal: "season", annual: "year", adhoc: "adhoc" };

// Lịch kỳ hệ thống (W11.03) — quy tắc có phiên bản như domain.PeriodRule; tag ghi vào đợt (period_rule).
export const RULE_VERSION_V1 = "kn.period_rule/v1-tam";
export const RULE_VERSION_V2 = "kn.period_rule/v2";
export type WeekNumbering = "season" | "year" | "iso";
export type MonthAlign = "calendar" | "week_end";
export interface PeriodRule {
  version: string;
  week_start: number; // 0 = Chủ nhật … 6 = thứ Bảy
  week_numbering: WeekNumbering;
  month_align: MonthAlign;
}
export const LEGACY_RULE: PeriodRule = { version: RULE_VERSION_V1, week_start: 1, week_numbering: "season", month_align: "calendar" };
export const DEFAULT_RULE: PeriodRule = { version: RULE_VERSION_V2, week_start: 3, week_numbering: "year", month_align: "week_end" };
const WEEKDAY_CODE = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];

/** Ngày hiệu lực phiên bản tham số extension.report_period_rule nạp sẵn (masterdata seeddata/params.json). */
export const RULE_V2_EFFECTIVE_FROM = "2026-11-04";

/** Giả lập tra tham số tại ngày đầu đợt (Service.periodRule): trước ngày hiệu lực → v1, từ đó → v2. */
export const ruleAt = (fromDate: string): PeriodRule => (fromDate >= RULE_V2_EFFECTIVE_FROM ? { ...DEFAULT_RULE } : { ...LEGACY_RULE });

export function ruleTag(r: PeriodRule): string {
  if (r.version === RULE_VERSION_V1) return `${r.version};week=${r.week_numbering}`;
  return `${r.version};week_start=${WEEKDAY_CODE[r.week_start]};week=${r.week_numbering};month=${r.month_align}`;
}

/** Như domain.ParseRule ("" → DEFAULT_RULE; không kiểm tra giá trị như Validate — chỉ dùng cho ca vàng/giả lập). */
export function parseRule(tag: string): PeriodRule {
  if (!tag.trim()) return { ...DEFAULT_RULE };
  const parts = tag.trim().split(";");
  let r: PeriodRule;
  if (parts[0] === RULE_VERSION_V1) r = { ...LEGACY_RULE };
  else if (parts[0] === RULE_VERSION_V2) r = { ...DEFAULT_RULE };
  else throw new PeriodError("period_rule", `phiên bản quy tắc chia kỳ không hỗ trợ: ${parts[0]}`);
  for (const p of parts.slice(1)) {
    const [k, v = ""] = p.trim().split("=");
    if (k === "week") r.week_numbering = v as WeekNumbering;
    else if (k === "month") r.month_align = v as MonthAlign;
    else if (k === "week_start" && WEEKDAY_CODE.includes(v)) r.week_start = WEEKDAY_CODE.indexOf(v);
    else throw new PeriodError("period_rule", `khóa quy tắc chia kỳ lạ: ${p}`);
  }
  return r;
}

export interface Deadline {
  offset_days: number;
  time: string;
}

/** Như PeriodRule.DefaultDeadline; không truyền quy tắc → v1 (domain.DefaultDeadline). */
export function defaultDeadline(pt: string, r: PeriodRule = LEGACY_RULE): Deadline {
  if (r.version === RULE_VERSION_V1) {
    if (pt === "week") return { offset_days: 2, time: "17:00" };
    if (pt === "month") return { offset_days: 10, time: "17:00" };
    return { offset_days: 7, time: "17:00" };
  }
  if (pt === "week" || pt === "ksgm_week") return { offset_days: 1, time: "12:00" };
  if (pt === "day") return { offset_days: 1, time: "09:00" };
  if (pt === "month" || pt === "quarter" || pt === "year") return r.month_align === "week_end" ? { offset_days: 1, time: "10:00" } : { offset_days: 10, time: "17:00" };
  return { offset_days: 7, time: "17:00" };
}

export const DEFAULT_CLOSE_AFTER_DAYS = 3;
export const MAX_PERIODS = 60;
export const MAX_DAY_PERIODS = 366;
export const MIN_REASON = 10;

/** Giờ Việt Nam UTC+7 (không có giờ mùa hè). */
const VN_OFFSET_MS = 7 * 3_600_000;

export interface SplitPeriod {
  slot: number;
  key: string;
  label: string;
  range_label: string;
  iso_week: number;
  week_year: number;
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

/** Hạn nộp UTC (ms) của kỳ kết thúc ngày `to`: to + offset ngày lúc HH:MM giờ Việt Nam. */
export function dueAt(to: number, d: Deadline): number {
  const c = parseClock(d.time);
  if (!c) throw new PeriodError("deadline.time", "giờ phải dạng HH:MM");
  return to + d.offset_days * DAY + c[0] * 3_600_000 + c[1] * 60_000 - VN_OFFSET_MS;
}

interface Span {
  key: string;
  label: string;
  from: number;
  to: number;
  week_year: number;
  week_no: number;
}

const isV2 = (r: PeriodRule) => r.version !== RULE_VERSION_V1;
const ymdParts = (t: number) => {
  const d = new Date(t);
  return [d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()] as const;
};
function startOf(r: PeriodRule, t: number): number {
  const ws = isV2(r) ? r.week_start : 1;
  return t - ((new Date(t).getUTCDay() - ws + 7) % 7) * DAY;
}
const isoNumbering = (r: PeriodRule) => !isV2(r) || r.week_numbering === "iso";
const week1 = (r: PeriodRule, y: number) => startOf(r, Date.UTC(y, 0, isoNumbering(r) ? 4 : 1));

/** Như PeriodRule.WeekOf (t = ms UTC 00:00). */
export function weekOf(r: PeriodRule, t: number): { year: number; no: number; from: number; to: number } {
  const from = startOf(r, t);
  const to = from + 6 * DAY;
  const year = new Date(isoNumbering(r) ? from + 3 * DAY : to).getUTCFullYear();
  return { year, no: Math.round((from - week1(r, year)) / DAY / 7) + 1, from, to };
}

const ROMAN_Q = ["I", "II", "III", "IV"];
const PT_V1 = "week | month | season | year | adhoc";
const PT_V2 = "day | week | ksgm_week | month | quarter | season | year | adhoc";

/** Như PeriodRule.PeriodOf (tuần KSGM: chỉ ranh giới mặc định 1, 8, 15, 22). */
export function periodOf(r: PeriodRule, pt: string, t: number): Span {
  const aligned = isV2(r) && r.month_align === "week_end";
  const anchor = (x: number) => (aligned ? weekOf(r, x).to : x);
  const bound = (first: number) => (aligned ? weekOf(r, first).from : first);
  const span = (key: string, label: string, first: number, next: number): Span => ({ key, label, from: bound(first), to: bound(next) - DAY, week_year: 0, week_no: 0 });
  if (pt === "week") {
    const w = weekOf(r, t);
    return { key: `${w.year}-W${pad2(w.no)}`, label: `Tuần ${w.no}/${w.year}`, from: w.from, to: w.to, week_year: w.year, week_no: w.no };
  }
  if (pt === "month") {
    const [y, m] = ymdParts(anchor(t));
    return span(`${y}-${pad2(m + 1)}`, `Tháng ${m + 1}/${y}`, Date.UTC(y, m, 1), Date.UTC(y, m + 1, 1));
  }
  if (isV2(r)) {
    if (pt === "day") {
      const [y, m, d] = ymdParts(t);
      return { key: dateStr(t), label: `Ngày ${pad2(d)}/${pad2(m + 1)}/${y}`, from: t, to: t, week_year: 0, week_no: 0 };
    }
    if (pt === "quarter") {
      const [y, m] = ymdParts(anchor(t));
      const q = Math.floor(m / 3);
      return span(`${y}-Q${q + 1}`, `Quý ${ROMAN_Q[q]}/${y}`, Date.UTC(y, q * 3, 1), Date.UTC(y, q * 3 + 3, 1));
    }
    if (pt === "year") {
      const [y] = ymdParts(anchor(t));
      return span(String(y), `Năm ${y}`, Date.UTC(y, 0, 1), Date.UTC(y + 1, 0, 1));
    }
    if (pt === "ksgm_week") {
      const [y, m, d] = ymdParts(t);
      const starts = [1, 8, 15, 22];
      const i = starts.filter((s) => s <= d).length - 1;
      const end = i < 3 ? Date.UTC(y, m, starts[i + 1]!) - DAY : Date.UTC(y, m + 1, 1) - DAY;
      return { key: `${y}-${pad2(m + 1)}-K${i + 1}`, label: `Tuần KSGM ${i + 1} tháng ${m + 1}/${y}`, from: Date.UTC(y, m, starts[i]!), to: end, week_year: 0, week_no: 0 };
    }
  }
  throw new PeriodError("period_type", isV2(r) ? PT_V2 : PT_V1);
}

/** Như domain.Split. */
export function split(pt: string, fromS: string, toS: string, season: string, rule: PeriodRule, d: Deadline, closeAfter: number): SplitPeriod[] {
  const from = parseDate(fromS);
  const to = parseDate(toS);
  if (from === null) throw new PeriodError("from_date", "ngày phải dạng YYYY-MM-DD");
  if (to === null) throw new PeriodError("to_date", "ngày phải dạng YYYY-MM-DD");
  if (to < from) throw new PeriodError("to_date", "phải không trước from_date");
  if (closeAfter < 0 || closeAfter > 60) throw new PeriodError("close_after_days", "0–60 ngày");
  if (d.offset_days < 0 || d.offset_days > 60) throw new PeriodError("deadline.offset_days", "0–60 ngày");
  if (!parseClock(d.time)) throw new PeriodError("deadline.time", "giờ phải dạng HH:MM");
  const limit = pt === "day" ? MAX_DAY_PERIODS : MAX_PERIODS;
  const out: SplitPeriod[] = [];
  const add = (f: number, t: number, s: Pick<Span, "key" | "label" | "week_year" | "week_no">) => {
    if (out.length >= limit) throw new PeriodError("to_date", `tối đa ${limit} kỳ mỗi đợt`);
    const due = dueAt(t, d);
    out.push({
      slot: out.length + 1,
      key: s.key,
      label: s.label,
      range_label: rangeLabel(f, t),
      iso_week: s.week_no,
      week_year: s.week_year,
      from_date: dateStr(f),
      to_date: dateStr(t),
      due_at: rfc3339(due),
      auto_close_at: rfc3339(due + closeAfter * DAY),
    });
  };
  if (pt === "season" || pt === "adhoc" || (pt === "year" && !isV2(rule))) {
    add(from, to, { key: "", label: season || (pt === "year" ? `Năm ${new Date(from).getUTCFullYear()}` : ""), week_year: 0, week_no: 0 });
    return out;
  }
  periodOf(rule, pt, from);
  if ((to - from) / DAY > 3660) throw new PeriodError("to_date", `tối đa ${limit} kỳ mỗi đợt`);
  for (let t0 = from; t0 <= to; ) {
    const s = periodOf(rule, pt, t0);
    const t = Math.min(s.to, to);
    if (pt === "week" && rule.week_numbering === "season") s.label = `Tuần ${out.length + 1}`;
    else if (pt === "week" && !isV2(rule)) s.label = `Tuần ${s.week_no}`;
    add(t0, t, s);
    t0 = t + DAY;
  }
  return out;
}

// ---------------------------------------------------------------------------
// Trạng thái công việc (domain/task.go)
// ---------------------------------------------------------------------------
export type StoredStatus = "not_started" | "draft" | "submitted" | "adjustment_requested" | "confirmed";
export type DisplayStatus = StoredStatus | "overdue" | "closed";

// Vòng đời mở rộng (W11.05, CNTS v3.7 III.3.0) — chỉ phòng ban có ExtendedLifecycle (Go); Trồng trọt không dùng.
export type ExtendedStatus = "closed_unadjusted" | "missed" | "awaiting_reopen";

export const STATUS_LABEL: Record<DisplayStatus | ExtendedStatus, string> = {
  not_started: "Chưa nhập",
  draft: "Lưu nháp",
  submitted: "Đã nộp",
  overdue: "Trễ hạn",
  adjustment_requested: "Yêu cầu điều chỉnh",
  confirmed: "Đã xác nhận",
  closed: "Đã chốt",
  closed_unadjusted: "Đã chốt – chưa điều chỉnh",
  missed: "Không nộp",
  awaiting_reopen: "Chờ mở kỳ",
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
