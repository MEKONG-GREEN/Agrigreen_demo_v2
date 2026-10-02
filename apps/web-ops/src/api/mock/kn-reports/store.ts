// ============================================================================
// BACKEND GIẢ LẬP vòng đời báo cáo KN v7.7 (P4 — pf/extension reportcycle + pf/reports Chi cục). Quy tắc như Go
// (internal/platform/extension/reportcycle/{app,domain}, internal/platform/reports/app/dispatch.go); quy tắc thuần
// (chia kỳ, trạng thái, so khác) dùng bản port domain.ts đã đối chiếu ca vàng Go. Giá trị ô do engine mẫu giả lập
// (forms/evaluate.ts = bản port reports/form) tính. Mọi trạng thái trong bộ nhớ tab.
// Dữ liệu khởi đầu: một đợt "Thu Đông 2026" (T01, T04) 5 tuần quanh tuần hiện tại cho 7 xã An Giang (2 khu vực),
// các kỳ đã qua có bài nộp + lần tổng hợp của "Hệ thống" lúc tự chốt — đủ cho Dashboard, báo cáo TW, Chi cục.
// ============================================================================
import { uuidv7 } from "../../idempotency";
import { problem } from "../http";
import { KN_ACCOUNTS, findKnAccount } from "../kn-accounts";
import { stableUuid, WS_ROSTER } from "../kn-schedules";
import { knStoredContext, type StoredContext } from "../kn";
import { evaluateGrid } from "../forms/evaluate";
import { exportXlsx } from "../forms/xlsx";
import { layout } from "../forms/layout";
import { mockAssignedForms, mockFormVersion, type MockAssignedForm } from "../forms/routes";
import type { FormDefinition, FormEvalInput, FormEvalResult, FormReportType } from "@/kn/forms/types";
import type { KnRole } from "@/kn/contract";
import { Decimal } from "@/kn/grid/decimal";
import {
  DAY_MS,
  DEFAULT_CLOSE_AFTER_DAYS,
  PeriodError,
  REPORT_PERIOD_TYPE,
  STATUS_LABEL,
  dec,
  defaultDeadline,
  diff,
  mondayOf,
  ratio,
  reason as checkReason,
  rfc3339,
  ruleTag,
  split,
  taskDisplay,
  taskOpen,
  taskPending,
  vnClock,
  yieldOf,
  ymd,
  type ChangeKind,
  type Deadline,
  type DisplayStatus,
  type ReportType,
  type Row,
  type Source,
  type SplitPeriod,
  type StoredStatus,
} from "./domain";

// ---------------------------------------------------------------------------
// Địa bàn giả lập: TTKN An Giang (91), 2 khu vực (Q-04 tạm), 7 xã
// ---------------------------------------------------------------------------
export const AG_PROVINCE = "91";
export const AG_DEPARTMENT = "trong_trot";

export interface MockRegion {
  code: string;
  name: string;
  sort: number;
}
export const AG_REGIONS: readonly MockRegion[] = [
  { code: "LX", name: "Khu vực Long Xuyên", sort: 1 },
  { code: "CD", name: "Khu vực Châu Đốc", sort: 2 },
];

export interface MockCommune {
  code: string;
  name: string;
  region: string;
  sort: number;
}
export const AG_COMMUNES: readonly MockCommune[] = [
  { code: "91301", name: "Mỹ Hòa Hưng", region: "LX", sort: 1 },
  { code: "91302", name: "Bình Đức", region: "LX", sort: 2 },
  { code: "91303", name: "Long Xuyên", region: "LX", sort: 3 },
  { code: "91304", name: "Vĩnh Tế", region: "CD", sort: 4 },
  { code: "91305", name: "An Cư", region: "CD", sort: 5 },
  { code: "91306", name: "Núi Sam", region: "CD", sort: 6 },
  { code: "91307", name: "Châu Phong", region: "CD", sort: 7 },
];

export const communeName = (code: string) => AG_COMMUNES.find((c) => c.code === code)?.name ?? code;
export const regionOf = (code: string) => AG_COMMUNES.find((c) => c.code === code)?.region ?? "";
export const regionName = (code: string) => AG_REGIONS.find((r) => r.code === code)?.name ?? code;

/** Tác nhân "Hệ thống" của lần tổng hợp tự động (Go SystemActor). */
export const SYSTEM_ACTOR = "00000000-0000-7000-8000-00000000c004";

export interface Officer {
  actor_ref: string;
  display: string;
  account_id: string | null;
}

/** Cán bộ xã phụ trách xã cùng phòng ban: tài khoản KN (kể cả Admin vừa tạo) + danh bạ lịch công tác P5. */
export function officersOf(dept: string, commune: string): Officer[] {
  const out: Officer[] = [];
  for (const a of KN_ACCOUNTS) {
    if (a.assignments.some((x) => x.role === "kn_xa" && x.departments.some((d) => d.code === dept) && x.communes.some((c) => c.code === commune))) {
      out.push({ actor_ref: stableUuid(a.id), display: a.displayName, account_id: a.id });
    }
  }
  for (const o of WS_ROSTER) {
    if (o.account_id || o.level !== "commune" || o.department_code !== dept || !o.communes.includes(commune)) continue;
    if (!out.some((x) => x.actor_ref === o.actor_ref)) out.push({ actor_ref: o.actor_ref, display: o.display, account_id: null });
  }
  return out;
}

/** Tên hiển thị của tác nhân (tra lúc đọc — RB-3). */
export function actorDisplay(ref: string | null | undefined): string {
  if (!ref) return "";
  if (ref === SYSTEM_ACTOR) return "Hệ thống";
  const a = KN_ACCOUNTS.find((x) => stableUuid(x.id) === ref);
  if (a) return a.displayName;
  return WS_ROSTER.find((o) => o.actor_ref === ref)?.display ?? "";
}

// ---------------------------------------------------------------------------
// Người gọi theo ngữ cảnh phiên (X-AG-Context) — như knActor / writer của Go
// ---------------------------------------------------------------------------
export interface Caller {
  contextId: string;
  accountId: string;
  role: KnRole;
  dept: string | null;
  /** Tỉnh của ngữ cảnh; null = mọi tỉnh (Cấp Quốc gia, Admin). */
  province: string | null;
  communes: string[];
  actor: string;
  display: string;
  support: { sessionId: string; onBehalfOf: string | null; reason: string } | null;
}

export type Result<T> = T | Response;
export const isErr = (r: unknown): r is Response => r instanceof Response;

export function callerOf(request: Request): Result<Caller> {
  const id = request.headers.get("X-AG-Context");
  if (!id) return problem(428, "accounts.context_required", "Cần chọn vai trò và phòng ban (header X-AG-Context)");
  const st = knStoredContext(id);
  if (!st) return problem(403, "accounts.context_invalid", "Ngữ cảnh phiên không hợp lệ hoặc không thuộc người gọi");
  return callerFromStored(st, request.headers.get("X-AG-Reason"));
}

export function callerFromStored(st: StoredContext, reasonHeader: string | null = null): Caller {
  const role = st.ctx.role;
  let reasonText = "";
  try {
    reasonText = reasonHeader ? decodeURIComponent(reasonHeader).trim() : "";
  } catch {
    reasonText = reasonHeader ?? "";
  }
  return {
    contextId: st.ctx.context_id,
    accountId: st.accountId,
    role,
    dept: st.ctx.department_code,
    province: role === "kn_tinh" || role === "kn_xa" ? AG_PROVINCE : null,
    communes: st.ctx.communes.map((c) => c.code),
    actor: stableUuid(st.accountId),
    display: findKnAccount(st.accountId)?.displayName ?? st.accountId,
    support: st.support ? { sessionId: st.support.sessionId, onBehalfOf: st.support.onBehalfOf ? stableUuid(st.support.onBehalfOf) : null, reason: reasonText } : null,
  };
}

const forbidden = () => problem(403, "forbidden", "Forbidden", { detail: "vai trò của ngữ cảnh phiên không được thao tác này" });

export function reader(c: Caller, ...roles: KnRole[]): Response | null {
  return roles.length && !roles.includes(c.role) ? forbidden() : null;
}

export function writer(c: Caller, role: KnRole): Response | null {
  if (c.role === "kn_tw") return problem(403, "extension.read_only", "Cấp Quốc gia chỉ xem");
  return c.role === role ? null : forbidden();
}

export const fieldErr = (field: string, reason: string) => problem(422, "validation_failed", "Dữ liệu không hợp lệ", { errors: [{ field, reason }] });

// ---------------------------------------------------------------------------
// Trạng thái
// ---------------------------------------------------------------------------
export interface BatchForm {
  template_id: string;
  version_id: string;
  code: string;
  name: string;
  version: number;
  period_block: boolean;
}

export interface Batch {
  id: string;
  department_code: string;
  province_code: string;
  report_type: ReportType;
  period_type: string;
  season_label: string;
  from_date: string;
  to_date: string;
  period_rule: string;
  deadline_offset_days: number;
  deadline_time: string;
  close_after_days: number;
  note: string | null;
  forms: BatchForm[];
  communes: string[];
  status: "issued" | "withdrawn";
  period_count: number;
  task_count: number;
  issued_by: string;
  issued_at: string;
  withdrawn_by: string | null;
  withdrawn_at: string | null;
  withdraw_reason: string | null;
  updated_at: string;
}

export interface Period {
  id: string;
  batch_id: string;
  slot: number;
  label: string;
  range_label: string;
  iso_week: number | null;
  from_date: string;
  to_date: string;
  due_at: string;
  auto_close_at: string;
  status: "open" | "closed";
  closed_at: string | null;
  closed_by: string | null;
  close_mode: "manual" | "auto" | null;
}

export interface SubRow {
  key: string;
  indicator?: string;
  input: Record<string, string>;
  cells: Record<string, string>;
}

type Issue = FormEvalResult["issues"][number];

export interface Submission {
  id: string;
  task_id: string;
  version_no: number;
  form_version_id: string;
  kind: "draft" | "submitted";
  rows: SubRow[];
  issues: Issue[];
  adjustment_response: boolean;
  previous_id: string | null;
  on_behalf_of: string | null;
  support_session_id: string | null;
  support_reason: string | null;
  event_time: string;
  recorded_at: string;
  actor_ref: string;
}

export interface LogEntry {
  event: string;
  actor_ref: string;
  at: string;
  changes: { field: string; old: string | null; new: string | null }[];
  reason: string | null;
  on_behalf_of: string | null;
  detail?: unknown;
}

export interface Task {
  id: string;
  batch_id: string;
  period_id: string;
  template_id: string;
  version_id: string;
  form_code: string;
  department_code: string;
  province_code: string;
  commune_code: string;
  region_code: string;
  slot: number;
  period_block: boolean;
  stored: StoredStatus;
  assignees: string[];
  subs: Submission[];
  submitted_id: string | null;
  submitted_at: string | null;
  submitted_by: string | null;
  first_submitted_at: string | null;
  on_behalf_of: string | null;
  adjustment_reason: string | null;
  confirmed_at: string | null;
  last_reminded_at: string | null;
  reopened_until: string | null;
  updated_at: string;
  withdrawn: boolean;
  log: LogEntry[];
}

export interface Reminder {
  id: string;
  batch_id: string;
  period_id: string;
  template_id: string | null;
  mode: "manual" | "auto_before" | "auto_overdue";
  message: string;
  communes: string[];
  task_ids: string[];
  recipient_count: number;
  actor_ref: string;
  at: string;
}

export interface RegionRef {
  commune: string;
  commune_name: string;
  region: string;
  region_name: string;
  sort_order: number;
}

export interface Run {
  id: string;
  run_no: number;
  period_id: string;
  template_id: string;
  version_id: string;
  form_code: string;
  trigger: "manual" | "system";
  actor_ref: string;
  on_behalf_of: string | null;
  at: string;
  communes_total: number;
  communes_with_data: number;
  communes_changed: number;
  cells_changed: number;
  provisional: boolean;
  missing_communes: string[];
  regions: RegionRef[];
  previous_run_id: string | null;
}

export interface SnapRow {
  key: string;
  commune: string;
  commune_name: string;
  region: string;
  region_name: string;
  indicator?: string;
  cells: Record<string, string>;
}
export interface SnapTotal {
  key: string;
  kind: "region" | "province";
  region?: string;
  region_name?: string;
  indicator?: string;
  members: number;
  cells: Record<string, string>;
}

export interface Change {
  id: string;
  commune_code: string;
  row_key: string;
  letter: string;
  old: string | null;
  new: string | null;
  kind: ChangeKind;
  submission_id: string | null;
  changed_by: string | null;
  on_behalf_of: string | null;
  submitted_at: string | null;
}

interface Snapshot {
  rows: SnapRow[];
  totals: SnapTotal[];
  sources: Record<string, Source>;
}

export interface Recipient {
  id: string;
  province_code: string;
  department_code: string;
  label: string;
  address: string;
  kind: "to" | "cc";
  is_default: boolean;
  active: boolean;
  last_used_at: string | null;
}

export interface DispFile {
  id: string;
  format: "xlsx" | "pdf";
  form_code: string;
  file_name: string;
  sha256: string;
  size_bytes: number;
  created_at: string;
}

export interface Dispatch {
  id: string;
  department_code: string;
  province_code: string;
  period_id: string;
  period_label: string;
  run_ids: string[];
  forms: string[];
  formats: string[];
  provisional: boolean;
  missing_communes: string[];
  status: "exported" | "sending" | "sent" | "failed";
  recipient_ids: string[];
  cc_ids: string[];
  subject: string | null;
  body: string | null;
  link_only: boolean;
  attempts: number;
  last_error: string | null;
  sent_at: string | null;
  created_by: string;
  created_at: string;
  updated_at: string;
  files: DispFile[];
  log: { event: string; actor_ref: string; at: string; on_behalf_of: string | null; detail: Record<string, unknown> }[];
  /** Mock: gửi xong lúc này (worker SMTP giả). */
  deliver_at: number | null;
}

interface State {
  batches: Batch[];
  periods: Period[];
  tasks: Task[];
  provincial: Map<string, Record<string, Record<string, Record<string, string>>>>; // batch → template → xã → chữ cái → giá trị
  reminders: Reminder[];
  runs: Run[];
  snapshots: Map<string, Snapshot>;
  changes: Map<string, Change[]>;
  batchLog: Map<string, LogEntry[]>;
  periodLog: Map<string, LogEntry[]>;
  recipients: Recipient[];
  dispatches: Dispatch[];
  now: () => number;
}

let S: State = emptyState(Date.now);

function emptyState(now: () => number): State {
  return {
    batches: [],
    periods: [],
    tasks: [],
    provincial: new Map(),
    reminders: [],
    runs: [],
    snapshots: new Map(),
    changes: new Map(),
    batchLog: new Map(),
    periodLog: new Map(),
    recipients: [],
    dispatches: [],
    now,
  };
}

export const nowMs = () => S.now();

// ---------------------------------------------------------------------------
// Mẫu (engine giả lập)
// ---------------------------------------------------------------------------
interface FormInfo {
  def: FormDefinition;
  block: boolean;
  slotLetter: Map<number, string[]>;
  letterSlot: Map<string, number>;
  formula: Set<string>;
  hasInd: boolean;
}

const infoCache = new Map<string, FormInfo>();

export function formInfo(versionId: string): FormInfo | null {
  const hit = infoCache.get(versionId);
  if (hit) return hit;
  const v = mockFormVersion(versionId);
  if (!v) return null;
  const def = v.definition;
  const fi: FormInfo = { def, block: def.period_blocks.length > 0, slotLetter: new Map(), letterSlot: new Map(), formula: new Set(), hasInd: def.rows.indicators.length > 0 };
  for (const c of def.columns) {
    if (c.period_block && (c.period_slot ?? 0) > 0) {
      fi.slotLetter.set(c.period_slot!, [...(fi.slotLetter.get(c.period_slot!) ?? []), c.letter]);
      fi.letterSlot.set(c.letter, c.period_slot!);
    }
    if (c.kind === "formula") fi.formula.add(c.letter);
  }
  infoCache.set(versionId, fi);
  return fi;
}

const rowKey = (commune: string, indicator?: string) => (indicator ? `${commune}/${indicator}` : commune);

function systemCells(fi: FormInfo, commune: string, provincial: Record<string, string> | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  for (const c of fi.def.columns) {
    if (c.kind === "fixed" && c.fixed_source === "commune_name") out[c.letter] = communeName(commune);
    else if (c.kind === "fixed" && c.fixed_source === "region_name") out[c.letter] = regionName(regionOf(commune));
    else if (c.kind === "provincial" && provincial?.[c.letter] !== undefined) out[c.letter] = provincial[c.letter]!;
  }
  return out;
}

function evaluate(fi: FormInfo, input: FormEvalInput): FormEvalResult {
  return evaluateGrid(fi.def, input);
}

// ---------------------------------------------------------------------------
// Tra cứu & phạm vi
// ---------------------------------------------------------------------------
export const batchOf = (id: string) => S.batches.find((b) => b.id === id);
export const periodOf = (id: string) => S.periods.find((p) => p.id === id);
export const taskOf = (id: string) => S.tasks.find((t) => t.id === id);
export const periodsOf = (batchId: string) => S.periods.filter((p) => p.batch_id === batchId).sort((a, b) => a.slot - b.slot);

export function batchVisible(c: Caller, b: Batch): boolean {
  return (c.province === null || b.province_code === c.province) && (c.role === "admin" || b.department_code === c.dept);
}

export function taskVisible(c: Caller, t: Task): boolean {
  const b = batchOf(t.batch_id);
  return !!b && !t.withdrawn && batchVisible(c, b) && (c.role !== "kn_xa" || c.communes.includes(t.commune_code));
}

function taskState(t: Task) {
  const p = periodOf(t.period_id)!;
  return { status: t.stored, dueAt: Date.parse(p.due_at), periodClosed: p.status === "closed", reopenedUntil: t.reopened_until ? Date.parse(t.reopened_until) : null };
}

export const displayOf = (t: Task, now = nowMs()): DisplayStatus => taskDisplay(taskState(t), now);
export const openOf = (t: Task, now = nowMs()): boolean => taskOpen(taskState(t), now);

export function taskWire(t: Task, now = nowMs()) {
  const p = periodOf(t.period_id)!;
  const display = displayOf(t, now);
  return {
    id: t.id,
    batch_id: t.batch_id,
    period_id: t.period_id,
    template_id: t.template_id,
    version_id: t.version_id,
    form_code: t.form_code,
    department_code: t.department_code,
    province_code: t.province_code,
    commune_code: t.commune_code,
    region_code: t.region_code,
    slot: t.slot,
    period_block: t.period_block,
    period_label: p.label,
    period_range: p.range_label,
    due_at: p.due_at,
    status: display,
    status_label: STATUS_LABEL[display],
    stored_status: t.stored,
    open: openOf(t, now),
    assignees: t.assignees,
    latest_version: t.subs.length ? t.subs[t.subs.length - 1]!.version_no : 0,
    submitted_at: t.submitted_at,
    submitted_by: t.submitted_by,
    on_behalf_of: t.on_behalf_of,
    adjustment_reason: t.stored === "adjustment_requested" ? t.adjustment_reason : null,
    confirmed_at: t.confirmed_at,
    last_reminded_at: t.last_reminded_at,
    reopened_until: t.reopened_until,
    updated_at: t.updated_at,
  };
}

export const subWire = (s: Submission) => ({ ...s, actor_display: actorDisplay(s.actor_ref) });

export const logWire = (l: LogEntry) => ({ ...l, actor_display: actorDisplay(l.actor_ref) });

export const reminderWire = (r: Reminder) => ({ ...r, actor_display: actorDisplay(r.actor_ref) });

export const runWire = (r: Run) => ({ ...r, actor_display: actorDisplay(r.actor_ref) });

// ---------------------------------------------------------------------------
// Phát hành (FN-TI-02.02)
// ---------------------------------------------------------------------------
export interface BatchPlan {
  report_type: ReportType;
  period_type: string;
  season_label: string;
  from_date: string;
  to_date: string;
  deadline: Deadline;
  close_after_days: number;
  note: string | null;
  forms: MockAssignedForm[];
  communes: string[];
  scope: { mode: string; regions: string[]; communes: string[] };
  periods: SplitPeriod[];
  provincial: Record<string, Record<string, Record<string, string>>>;
  warnings: string[];
  without_officer: string[];
  slots: Map<string, number>;
}

const REPORT_TYPES: ReportType[] = ["weekly", "monthly", "seasonal", "annual", "adhoc"];

export function planBatch(c: Caller, body: unknown): Result<BatchPlan> {
  const b = (body ?? {}) as Record<string, unknown>;
  const rt = b.report_type as ReportType;
  if (!REPORT_TYPES.includes(rt)) return fieldErr("report_type", "weekly | monthly | seasonal | annual | adhoc");
  const pt = REPORT_PERIOD_TYPE[rt];
  if (b.period_type !== undefined && b.period_type !== pt) return fieldErr("period_type", `loại báo cáo ${rt} dùng kỳ ${pt}`);
  const tplIds = Array.isArray(b.template_ids) ? (b.template_ids as unknown[]).filter((x): x is string => typeof x === "string") : [];
  if (!tplIds.length) return fieldErr("template_ids", "chọn ít nhất một mẫu");
  const scope = (b.scope ?? {}) as { mode?: string; regions?: unknown; communes?: unknown };
  const mode = scope.mode || "province";
  if (!["province", "regions", "communes"].includes(mode)) return fieldErr("scope.mode", "province | regions | communes");
  const regions = Array.isArray(scope.regions) ? (scope.regions as string[]) : [];
  const picked = Array.isArray(scope.communes) ? (scope.communes as string[]) : [];
  if (mode === "regions" && !regions.length) return fieldErr("scope.regions", "chọn ít nhất một khu vực");
  const season = typeof b.season_label === "string" ? b.season_label.trim() : "";
  if (!season || [...season].length > 120) return fieldErr("season_label", "bắt buộc, tối đa 120 ký tự (vd Thu Đông 2026)");
  const note = typeof b.note === "string" && b.note.trim() ? b.note.trim() : null;
  if (note && [...note].length > 2000) return fieldErr("note", "tối đa 2000 ký tự");
  const dl = (b.deadline ?? {}) as Partial<Deadline>;
  const def = defaultDeadline(pt);
  const deadline: Deadline = { offset_days: typeof dl.offset_days === "number" ? dl.offset_days : def.offset_days, time: typeof dl.time === "string" ? dl.time : def.time };
  const closeAfter = typeof b.close_after_days === "number" ? b.close_after_days : DEFAULT_CLOSE_AFTER_DAYS;
  let periods: SplitPeriod[];
  try {
    periods = split(pt, String(b.from_date ?? ""), String(b.to_date ?? ""), season, "season", deadline, closeAfter);
  } catch (e) {
    if (e instanceof PeriodError) return fieldErr(e.field, e.msg);
    throw e;
  }
  const dept = c.dept ?? AG_DEPARTMENT;
  const assigned = mockAssignedForms(dept, "commune", rt as FormReportType, String(b.from_date));
  const forms: MockAssignedForm[] = [];
  for (const id of tplIds) {
    const f = assigned.find((x) => x.template.id === id);
    if (!f) return problem(422, "extension.form_not_assigned", "Mẫu chưa được gán cho loại báo cáo của phòng ban", { detail: id });
    if (!f.definition.columns.some((col) => col.kind === "input" || col.kind === "per_period")) return problem(422, "extension.form_no_structure", "Mẫu chưa có cấu trúc cột nhập liệu", { detail: f.template.code });
    forms.push(f);
  }
  let communes: string[];
  if (mode === "province") communes = AG_COMMUNES.map((x) => x.code);
  else if (mode === "regions") communes = AG_COMMUNES.filter((x) => regions.includes(x.region)).map((x) => x.code);
  else communes = AG_COMMUNES.filter((x) => picked.includes(x.code)).map((x) => x.code);
  if (!communes.length) return problem(422, "extension.no_communes", "Chưa chọn xã nhận hoặc xã không thuộc tỉnh");
  const warnings: string[] = [];
  const slots = new Map<string, number>();
  for (const f of forms) {
    const s = f.definition.period_blocks.length ? Math.min(...f.definition.period_blocks.map((x) => x.slots)) : 0;
    slots.set(f.template.id, s);
    if (s > 0 && s < periods.length) warnings.push(`${f.template.code} chỉ có ${s} khe kỳ trên một dòng, đợt có ${periods.length} kỳ (tạm theo mẫu seed, chờ khách Q-01)`);
  }
  const without = communes.filter((x) => officersOf(dept, x).length === 0);
  if (without.length) warnings.push(`${without.length} xã/phường chưa có cán bộ phụ trách cùng phòng ban`);
  // Giá trị Tỉnh giao: chỉ cột provincial, xã trong phạm vi, số không âm (chuẩn hóa).
  const provincial: BatchPlan["provincial"] = {};
  const pv = (b.provincial_values ?? {}) as Record<string, Record<string, Record<string, unknown>>>;
  for (const f of forms) {
    const cols = new Map(f.definition.columns.filter((x) => x.kind === "provincial").map((x) => [x.letter, x]));
    const byCommune = pv[f.template.id] ?? {};
    for (const [commune, cells] of Object.entries(byCommune)) {
      if (!communes.includes(commune)) continue;
      for (const [letter, raw] of Object.entries(cells ?? {})) {
        const col = cols.get(letter);
        if (!col || raw === "" || raw === null || raw === undefined) continue;
        let v = String(raw).trim();
        if (col.dtype === "number" || col.dtype === "integer") {
          const d = Decimal.parse(v.replace(",", "."));
          if (!d || d.sign() < 0) return fieldErr(`provincial_values.${f.template.code}.${commune}.${letter}`, "phải là số không âm");
          v = d.toString();
        }
        ((provincial[f.template.id] ??= {})[commune] ??= {})[letter] = v;
      }
    }
  }
  const copyFrom = Array.isArray(b.copy_provincial_from_previous) ? (b.copy_provincial_from_previous as string[]) : [];
  if (copyFrom.length) {
    const prev = [...S.batches].reverse().find((x) => x.status === "issued" && batchVisible(c, x));
    const prevPv = prev ? S.provincial.get(prev.id) : undefined;
    if (prevPv) {
      for (const f of forms) {
        if (!copyFrom.includes(f.template.id)) continue;
        const src = prevPv[f.template.id] ?? Object.values(prevPv).find(() => false);
        for (const commune of communes) {
          if (provincial[f.template.id]?.[commune]) continue;
          if (src?.[commune]) ((provincial[f.template.id] ??= {})[commune] = { ...src[commune] });
        }
      }
    }
  }
  return {
    report_type: rt,
    period_type: pt,
    season_label: season,
    from_date: String(b.from_date),
    to_date: String(b.to_date),
    deadline,
    close_after_days: closeAfter,
    note,
    forms,
    communes,
    scope: { mode, regions, communes: picked },
    periods,
    provincial,
    warnings,
    without_officer: without,
    slots,
  };
}

export function previewWire(p: BatchPlan) {
  return {
    report_type: p.report_type,
    period_type: p.period_type,
    period_rule: ruleTag("season"),
    deadline: p.deadline,
    close_after_days: p.close_after_days,
    forms: p.forms.map((f) => ({
      template_id: f.template.id,
      version_id: f.version.id,
      code: f.template.code,
      name: f.template.name,
      version: f.version.version,
      period_block: f.definition.period_blocks.length > 0,
      slots: p.slots.get(f.template.id) ?? 0,
    })),
    periods: p.periods,
    communes: p.communes.map((x) => ({ commune_code: x, region_code: regionOf(x), region_name: regionName(regionOf(x)), officer_count: officersOf(AG_DEPARTMENT, x).length })),
    communes_without_officer: p.without_officer,
    provincial_columns: p.forms.flatMap((f) =>
      f.definition.columns.filter((x) => x.kind === "provincial").map((x) => ({ template_id: f.template.id, form_code: f.template.code, letter: x.letter, title: x.title, unit: x.unit ?? "" })),
    ),
    form_count: p.forms.length,
    commune_count: p.communes.length,
    period_count: p.periods.length,
    task_count: p.forms.length * p.communes.length * p.periods.length,
    warnings: p.warnings,
  };
}

export function issueBatch(c: Caller, plan: BatchPlan, at = nowMs()): Result<{ batch: Batch; periods: Period[] }> {
  for (const f of plan.forms) {
    const s = plan.slots.get(f.template.id) ?? 0;
    if (s > 0 && s < plan.periods.length) return problem(422, "extension.period_slots_exceeded", "Số kỳ vượt số khe kỳ của mẫu", { detail: `${f.template.code}: ${s} khe, ${plan.periods.length} kỳ` });
  }
  const dept = c.dept ?? AG_DEPARTMENT;
  const iso = rfc3339(at);
  const batch: Batch = {
    id: uuidv7(at),
    department_code: dept,
    province_code: AG_PROVINCE,
    report_type: plan.report_type,
    period_type: plan.period_type,
    season_label: plan.season_label,
    from_date: plan.from_date,
    to_date: plan.to_date,
    period_rule: ruleTag("season"),
    deadline_offset_days: plan.deadline.offset_days,
    deadline_time: plan.deadline.time,
    close_after_days: plan.close_after_days,
    note: plan.note,
    forms: plan.forms.map((f) => ({ template_id: f.template.id, version_id: f.version.id, code: f.template.code, name: f.template.name, version: f.version.version, period_block: f.definition.period_blocks.length > 0 })),
    communes: plan.communes,
    status: "issued",
    period_count: plan.periods.length,
    task_count: plan.forms.length * plan.communes.length * plan.periods.length,
    issued_by: c.actor,
    issued_at: iso,
    withdrawn_by: null,
    withdrawn_at: null,
    withdraw_reason: null,
    updated_at: iso,
  };
  S.batches.push(batch);
  const periods = plan.periods.map(
    (p): Period => ({ id: uuidv7(at), batch_id: batch.id, slot: p.slot, label: p.label, range_label: p.range_label, iso_week: p.iso_week || null, from_date: p.from_date, to_date: p.to_date, due_at: p.due_at, auto_close_at: p.auto_close_at, status: "open", closed_at: null, closed_by: null, close_mode: null }),
  );
  S.periods.push(...periods);
  S.provincial.set(batch.id, plan.provincial);
  for (const f of batch.forms) {
    for (const commune of plan.communes) {
      const assignees = officersOf(dept, commune).map((o) => o.actor_ref);
      for (const p of periods) {
        S.tasks.push({
          id: uuidv7(at),
          batch_id: batch.id,
          period_id: p.id,
          template_id: f.template_id,
          version_id: f.version_id,
          form_code: f.code,
          department_code: dept,
          province_code: AG_PROVINCE,
          commune_code: commune,
          region_code: regionOf(commune),
          slot: p.slot,
          period_block: f.period_block,
          stored: "not_started",
          assignees,
          subs: [],
          submitted_id: null,
          submitted_at: null,
          submitted_by: null,
          first_submitted_at: null,
          on_behalf_of: null,
          adjustment_reason: null,
          confirmed_at: null,
          last_reminded_at: null,
          reopened_until: null,
          updated_at: iso,
          withdrawn: false,
          log: [],
        });
      }
    }
  }
  pushLog(S.batchLog, batch.id, {
    event: "batch.issued",
    actor_ref: c.actor,
    at: iso,
    changes: [],
    reason: c.support?.reason || null,
    on_behalf_of: c.support?.onBehalfOf ?? null,
    detail: {
      report_type: batch.report_type,
      period_type: batch.period_type,
      season_label: batch.season_label,
      from_date: batch.from_date,
      to_date: batch.to_date,
      forms: batch.forms.map((x) => x.code),
      scope: plan.scope,
      deadline: plan.deadline,
      close_after_days: plan.close_after_days,
      period_rule: batch.period_rule,
      task_count: batch.task_count,
      communes_no_officer: plan.without_officer,
      note: plan.note,
    },
  });
  return { batch, periods };
}

function pushLog(m: Map<string, LogEntry[]>, id: string, e: LogEntry) {
  m.set(id, [...(m.get(id) ?? []), e]);
}

export const batchLog = (id: string) => [...(S.batchLog.get(id) ?? [])].reverse();
export const periodLog = (id: string) => [...(S.periodLog.get(id) ?? [])].reverse();

export function taskLog(t: Task): LogEntry[] {
  const b = batchOf(t.batch_id)!;
  return [...t.log, { event: "batch.issued", actor_ref: b.issued_by, at: b.issued_at, changes: [], reason: null, on_behalf_of: null }].sort((x, y) => (x.at < y.at ? 1 : x.at > y.at ? -1 : 0));
}

export function listBatches(c: Caller, q: { province?: string | null; report_type?: string | null }) {
  return S.batches
    .filter((b) => batchVisible(c, b) && (!q.province || b.province_code === q.province) && (!q.report_type || b.report_type === q.report_type))
    .sort((a, b) => (a.id < b.id ? 1 : -1));
}

export function withdrawBatch(c: Caller, b: Batch, reasonText: string, at = nowMs()): Result<Batch> {
  if (b.status === "withdrawn") return problem(409, "extension.batch_withdrawn", "Đợt đã thu hồi");
  const tasks = S.tasks.filter((t) => t.batch_id === b.id);
  const n = tasks.filter((t) => t.subs.some((s) => s.kind === "submitted")).length;
  if (n) return problem(409, "extension.batch_has_submissions", "Đợt đã có xã nộp — không thu hồi được", { detail: `${n} công việc đã có bản gửi` });
  const iso = rfc3339(at);
  Object.assign(b, { status: "withdrawn", withdrawn_by: c.actor, withdrawn_at: iso, withdraw_reason: reasonText, updated_at: iso });
  for (const t of tasks) t.withdrawn = true;
  pushLog(S.batchLog, b.id, { event: "batch.withdrawn", actor_ref: c.actor, at: iso, changes: [], reason: reasonText, on_behalf_of: c.support?.onBehalfOf ?? null, detail: {} });
  return b;
}

export function changeDeadline(c: Caller, b: Batch, body: Record<string, unknown>, reasonText: string, at = nowMs()): Result<{ changes: Record<string, unknown>[] }> {
  if (b.status === "withdrawn") return problem(409, "extension.batch_withdrawn", "Đợt đã thu hồi");
  const hasDue = typeof body.due_at === "string";
  const hasRule = body.deadline !== undefined && body.deadline !== null;
  if (hasDue === hasRule) return fieldErr("due_at", "chọn một: due_at (hạn mới) hoặc deadline (quy tắc hạn)");
  const closeAfter = typeof body.close_after_days === "number" ? body.close_after_days : b.close_after_days;
  const ids = Array.isArray(body.period_ids) ? (body.period_ids as string[]) : null;
  const changes: Record<string, unknown>[] = [];
  for (const p of periodsOf(b.id)) {
    if ((ids && !ids.includes(p.id)) || p.status === "closed" || at >= Date.parse(p.due_at)) continue;
    let due: number;
    if (hasDue) due = Date.parse(String(body.due_at));
    else {
      const d = body.deadline as Deadline;
      due = Date.parse(p.to_date) + d.offset_days * DAY_MS + Number(d.time.slice(0, 2)) * 3_600_000 + Number(d.time.slice(3, 5)) * 60_000 - 7 * 3_600_000;
    }
    if (!Number.isFinite(due) || due <= at) return fieldErr("due_at", "hạn mới phải sau thời điểm hiện tại");
    const ch = { period_id: p.id, label: p.label, old_due_at: p.due_at, new_due_at: rfc3339(due), old_auto_close_at: p.auto_close_at, new_auto_close_at: rfc3339(due + closeAfter * DAY_MS) };
    p.due_at = ch.new_due_at;
    p.auto_close_at = ch.new_auto_close_at;
    changes.push(ch);
  }
  if (!changes.length) return problem(409, "extension.deadline_no_future_period", "Không còn kỳ chưa đến hạn để sửa hạn nộp");
  pushLog(S.batchLog, b.id, { event: "batch.deadline_changed", actor_ref: c.actor, at: rfc3339(at), changes: [], reason: reasonText, on_behalf_of: c.support?.onBehalfOf ?? null, detail: { changes } });
  return { changes };
}

export function provincialOf(batchId: string, templateId: string | null, communes: string[] | null) {
  const pv = S.provincial.get(batchId) ?? {};
  const items: { template_id: string; commune_code: string; cells: Record<string, string> }[] = [];
  for (const [tpl, byCommune] of Object.entries(pv)) {
    if (templateId && tpl !== templateId) continue;
    for (const [commune, cells] of Object.entries(byCommune)) if (!communes || communes.includes(commune)) items.push({ template_id: tpl, commune_code: commune, cells });
  }
  return items;
}

// ---------------------------------------------------------------------------
// Công việc & bài nộp (FN-XA-03.01, 03.02)
// ---------------------------------------------------------------------------
export interface TaskQuery {
  commune?: string | null;
  period_type?: string | null;
  status?: string | null;
  form_code?: string | null;
  batch_id?: string | null;
  period_id?: string | null;
}

export function listTasks(c: Caller, q: TaskQuery, now = nowMs()) {
  const base = S.tasks.filter((t) => {
    if (!taskVisible(c, t)) return false;
    const b = batchOf(t.batch_id)!;
    return (
      (!q.commune || t.commune_code === q.commune) &&
      (!q.period_type || b.period_type === q.period_type) &&
      (!q.form_code || t.form_code === q.form_code) &&
      (!q.batch_id || t.batch_id === q.batch_id) &&
      (!q.period_id || t.period_id === q.period_id)
    );
  });
  const counts = { todo: 0, overdue: 0, adjustment_requested: 0 };
  for (const t of base) {
    const d = displayOf(t, now);
    if (openOf(t, now) && taskPending({ status: t.stored })) counts.todo++;
    if (d === "overdue") counts.overdue++;
    if (d === "adjustment_requested") counts.adjustment_requested++;
  }
  const items = base
    .filter((t) => !q.status || displayOf(t, now) === q.status)
    .sort((a, b) => {
      const da = periodOf(a.period_id)!.due_at;
      const db = periodOf(b.period_id)!.due_at;
      return da < db ? -1 : da > db ? 1 : a.commune_code.localeCompare(b.commune_code) || a.form_code.localeCompare(b.form_code);
    });
  const reminderIds = new Set(base.map((t) => t.id));
  const last = [...S.reminders].reverse().find((r) => r.task_ids.some((id) => reminderIds.has(id))) ?? null;
  return { items, counts, last_reminder: last };
}

/** Giá trị các khe kỳ KHÁC (bản mới nhất, kể cả nháp) — khóa dòng → chữ cái → giá trị. */
function carriedOf(fi: FormInfo, t: Task, maxSlot = 0, submittedOnly = false): Record<string, Record<string, string>> {
  const out: Record<string, Record<string, string>> = {};
  if (!fi.block) return out;
  for (const sib of S.tasks) {
    if (sib.id === t.id || sib.batch_id !== t.batch_id || sib.template_id !== t.template_id || sib.commune_code !== t.commune_code) continue;
    if (maxSlot > 0 && sib.slot > maxSlot) continue;
    // Như Go latestRows(allowDraft): bản "Đã gửi" mới nhất, chưa có thì bản mới nhất.
    const pick = sib.subs.find((s) => s.id === sib.submitted_id) ?? (submittedOnly ? undefined : sib.subs.at(-1));
    if (!pick) continue;
    for (const r of pick.rows) {
      const k = rowKey(t.commune_code, r.indicator);
      for (const l of fi.slotLetter.get(sib.slot) ?? []) {
        const v = r.input[l];
        if (v) (out[k] ??= {})[l] = v;
      }
    }
  }
  return out;
}

export function taskDetail(t: Task, now = nowMs()) {
  const b = batchOf(t.batch_id)!;
  const p = periodOf(t.period_id)!;
  const fi = formInfo(t.version_id);
  const latest = t.subs.at(-1) ?? null;
  const latestSubmitted = t.subs.find((s) => s.id === t.submitted_id) ?? null;
  return {
    task: taskWire(t, now),
    batch: b,
    period: p,
    form_name: b.forms.find((f) => f.template_id === t.template_id)?.name ?? t.form_code,
    layout: { template_id: t.template_id, version_id: t.version_id, period_labels: periodsOf(b.id).map((x) => x.label), season: b.season_label, open_slots: openOf(t, now) ? [t.slot] : [] },
    provincial_values: S.provincial.get(b.id)?.[t.template_id]?.[t.commune_code] ?? {},
    carried: fi ? carriedOf(fi, t) : {},
    latest: latest ? subWire(latest) : null,
    latest_submitted: latestSubmitted ? subWire(latestSubmitted) : null,
  };
}

const COMPUTED_ISSUE = new Set(["reports.formula_error", "reports.rule_failed", "reports.rule_error"]);
const UUIDV7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export function submit(c: Caller, t: Task, body: unknown, at = nowMs()): Result<{ status: number; submission: Submission; issues: Issue[] }> {
  const b = (body ?? {}) as { id?: unknown; kind?: unknown; rows?: unknown; event_time?: unknown };
  if (b.kind !== "draft" && b.kind !== "submitted") return fieldErr("kind", "draft | submitted");
  const rowsIn = Array.isArray(b.rows) ? (b.rows as { indicator?: string; input?: Record<string, string> }[]) : [];
  if (rowsIn.length < 1 || rowsIn.length > 500) return fieldErr("rows", "1–500 dòng");
  if (typeof b.event_time === "string" && Date.parse(b.event_time) > at + 5 * 60_000) return fieldErr("event_time", "không được ở tương lai");
  if (typeof b.id === "string") {
    if (!UUIDV7.test(b.id)) return fieldErr("id", "phải là UUIDv7");
    const prev = t.subs.find((s) => s.id === b.id);
    if (prev) return { status: 200, submission: prev, issues: prev.issues };
    if (S.tasks.some((x) => x.subs.some((s) => s.id === b.id))) return problem(409, "conflict", "Xung đột dữ liệu", { errors: [{ field: "id", reason: "id đã dùng cho công việc khác" }] });
  }
  if (c.support && [...c.support.reason].length < 10) return problem(422, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do");
  if (!openOf(t, at)) return problem(409, "extension.period_closed", "Kỳ đã chốt — không gửi/sửa được nếu chưa mở lại kỳ");
  const fi = formInfo(t.version_id);
  if (!fi) return problem(503, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng");
  if (fi.hasInd && rowsIn.some((r) => !r.indicator)) return fieldErr("rows", "mẫu có dòng chỉ tiêu — mỗi dòng cần indicator");
  const mode = b.kind === "submitted" ? "submit" : "draft";
  const batch = batchOf(t.batch_id)!;
  const system = systemCells(fi, t.commune_code, S.provincial.get(batch.id)?.[t.template_id]?.[t.commune_code]);
  const carry = carriedOf(fi, t);
  const region = regionOf(t.commune_code);
  const mk = (strip: boolean) =>
    rowsIn.map((r) => {
      const k = rowKey(t.commune_code, r.indicator);
      const input: Record<string, string> = {};
      for (const [l0, v] of Object.entries(r.input ?? {})) {
        const l = l0.trim().toUpperCase();
        if (strip) {
          const slot = fi.letterSlot.get(l);
          if (slot !== undefined && slot !== t.slot && carry[k]?.[l] === v) continue;
        }
        input[l] = String(v);
      }
      return { key: k, commune: t.commune_code, region, ...(r.indicator ? { indicator: r.indicator } : {}), input, system };
    });
  let rows: SubRow[];
  let issues: Issue[];
  if (!fi.block) {
    const input = mk(false);
    const res = evaluate(fi, { mode, open_slots: null, rows: input });
    rows = res.rows.map((r, i) => ({ key: r.key, ...(r.indicator ? { indicator: r.indicator } : {}), input: input[i]!.input, cells: r.cells }));
    issues = res.issues;
  } else {
    const a = evaluate(fi, { mode, open_slots: [t.slot], rows: mk(true) });
    const merged = mk(true).map((r) => {
      const input: Record<string, string> = {};
      for (const [l, v] of Object.entries(r.input)) {
        const slot = fi.letterSlot.get(l);
        if (slot === undefined || slot === t.slot) input[l] = v;
      }
      Object.assign(input, carry[r.key] ?? {});
      return { ...r, input };
    });
    const bres = evaluate(fi, { mode: "draft", open_slots: null, rows: merged });
    issues = [...a.issues.filter((i) => !COMPUTED_ISSUE.has(i.code)), ...bres.issues.filter((i) => COMPUTED_ISSUE.has(i.code))];
    rows = bres.rows.map((r, i) => ({ key: r.key, ...(r.indicator ? { indicator: r.indicator } : {}), input: merged[i]!.input, cells: r.cells }));
  }
  if (mode === "submit" && issues.some((i) => i.severity === "error")) {
    return problem(422, "extension.submission_invalid", "Số liệu chưa đạt yêu cầu của mẫu", { issues });
  }
  const iso = rfc3339(at);
  const prev = t.subs.at(-1) ?? null;
  const sub: Submission = {
    id: typeof b.id === "string" ? b.id : uuidv7(at),
    task_id: t.id,
    version_no: (prev?.version_no ?? 0) + 1,
    form_version_id: t.version_id,
    kind: b.kind,
    rows,
    issues,
    adjustment_response: b.kind === "submitted" && t.stored === "adjustment_requested",
    previous_id: prev?.id ?? null,
    on_behalf_of: c.support?.onBehalfOf ?? null,
    support_session_id: c.support?.sessionId ?? null,
    support_reason: c.support?.reason ?? null,
    event_time: typeof b.event_time === "string" ? rfc3339(Date.parse(b.event_time)) : iso,
    recorded_at: iso,
    actor_ref: c.actor,
  };
  t.subs.push(sub);
  t.updated_at = iso;
  if (b.kind === "submitted") {
    Object.assign(t, { stored: "submitted", submitted_id: sub.id, submitted_at: sub.event_time, submitted_by: c.actor, on_behalf_of: sub.on_behalf_of, confirmed_at: null });
    t.first_submitted_at ??= sub.event_time;
  } else if (t.stored === "not_started") t.stored = "draft";
  t.log.push({
    event: b.kind === "submitted" ? "task.submitted" : "task.draft_saved",
    actor_ref: c.actor,
    at: iso,
    changes: [{ field: "submission_id", old: null, new: sub.id }],
    reason: c.support?.reason ?? null,
    on_behalf_of: sub.on_behalf_of,
  });
  return { status: 201, submission: sub, issues };
}

/** SoD (R-56) với chế độ hỗ trợ: không rà soát/xác nhận/chốt bản chính mình nhập thay. */
function selfActAs(c: Caller, tasks: Task[]): Response | null {
  const hit = tasks.some((t) => t.subs.some((s) => s.kind === "submitted" && s.on_behalf_of && s.actor_ref === c.actor));
  return hit ? problem(403, "self_approval_forbidden", "Không được phê duyệt thay đổi do chính mình đặt", { detail: "Không rà soát, xác nhận hoặc chốt báo cáo do chính mình nhập thay (chế độ hỗ trợ)" }) : null;
}

export function requestAdjustment(c: Caller, t: Task, body: unknown, at = nowMs()): Result<Task> {
  const [r, ok] = checkReason(String((body as { reason?: unknown } | undefined)?.reason ?? ""), 2000);
  if (!ok) return fieldErr("reason", "bắt buộc, 10–2000 ký tự");
  if (!t.subs.some((s) => s.kind === "submitted")) return problem(409, "extension.task_not_submitted", "Xã chưa gửi báo cáo");
  const sod = selfActAs(c, [t]);
  if (sod) return sod;
  if (!openOf(t, at)) return problem(409, "extension.adjustment_after_close", "Kỳ đã chốt — mở lại kỳ trước khi yêu cầu điều chỉnh");
  const iso = rfc3339(at);
  Object.assign(t, { stored: "adjustment_requested", adjustment_reason: r, confirmed_at: null, updated_at: iso });
  t.log.push({ event: "task.adjustment_requested", actor_ref: c.actor, at: iso, changes: [{ field: "submission_id", old: null, new: t.submitted_id }], reason: r, on_behalf_of: c.support?.onBehalfOf ?? null });
  return t;
}

export function confirmTask(c: Caller, t: Task, body: unknown, at = nowMs()): Result<Task> {
  const note = String((body as { note?: unknown } | undefined)?.note ?? "").trim();
  if (note && !checkReason(note, 2000)[1]) return fieldErr("note", "10–2000 ký tự nếu có");
  if (!t.subs.some((s) => s.kind === "submitted")) return problem(409, "extension.task_not_submitted", "Xã chưa gửi báo cáo");
  const sod = selfActAs(c, [t]);
  if (sod) return sod;
  if (t.stored !== "submitted" && t.stored !== "confirmed") return problem(409, "extension.task_not_submitted", "Xã chưa gửi báo cáo", { detail: "chỉ xác nhận bản đã gửi chưa có yêu cầu điều chỉnh" });
  const iso = rfc3339(at);
  Object.assign(t, { stored: "confirmed", confirmed_at: iso, updated_at: iso });
  t.log.push({ event: "task.confirmed", actor_ref: c.actor, at: iso, changes: [{ field: "submission_id", old: null, new: t.submitted_id }], reason: note || null, on_behalf_of: c.support?.onBehalfOf ?? null });
  return t;
}

// ---------------------------------------------------------------------------
// Trạng thái nộp, nhắc (FN-TI-02.03)
// ---------------------------------------------------------------------------
export function defaultPeriod(batchId: string, now = nowMs()): Period | undefined {
  const ps = periodsOf(batchId);
  return [...ps].reverse().find((p) => Date.parse(p.from_date) <= now) ?? ps[0];
}

export function statusBoard(b: Batch, p: Period, q: { template_id?: string | null; status?: string | null }, now = nowMs()) {
  const tasks = S.tasks.filter((t) => t.period_id === p.id && !t.withdrawn && (!q.template_id || t.template_id === q.template_id));
  const counts: Record<string, number> = { not_started: 0, draft: 0, submitted: 0, overdue: 0, adjustment_requested: 0, confirmed: 0, closed: 0, no_officer: 0 };
  const items = tasks
    .map((t) => {
      const d = displayOf(t, now);
      counts[d] = (counts[d] ?? 0) + 1;
      const noOfficer = t.assignees.length === 0;
      if (noOfficer) counts.no_officer = (counts.no_officer ?? 0) + 1;
      return {
        task_id: t.id,
        commune_code: t.commune_code,
        commune_name: communeName(t.commune_code),
        region_code: t.region_code,
        region_name: regionName(t.region_code),
        form_code: t.form_code,
        template_id: t.template_id,
        status: d,
        status_label: STATUS_LABEL[d],
        officers: t.assignees.map((a) => ({ actor_ref: a, display: actorDisplay(a) })),
        no_officer: noOfficer,
        submitted_at: t.submitted_at,
        last_reminded_at: t.last_reminded_at,
      };
    })
    .filter((x) => !q.status || (q.status === "no_officer" ? x.no_officer : x.status === q.status))
    .sort((a, b) => AG_COMMUNES.findIndex((x) => x.code === a.commune_code) - AG_COMMUNES.findIndex((x) => x.code === b.commune_code) || a.form_code.localeCompare(b.form_code));
  return { period: p, counts, items, batch: b };
}

export function remind(c: Caller, b: Batch, body: unknown, at = nowMs()): Result<{ reminder: Reminder; skipped_no_officer: string[]; skipped_done: string[] }> {
  const x = (body ?? {}) as { period_id?: string; template_id?: string | null; communes?: string[]; all_pending?: boolean; message?: string };
  const p = x.period_id ? periodOf(x.period_id) : undefined;
  if (!p || p.batch_id !== b.id) return problem(404, "extension.report_period_not_found", "Không tìm thấy kỳ báo cáo");
  const message = String(x.message ?? "").trim();
  if (!message || [...message].length > 1000) return fieldErr("message", "bắt buộc, tối đa 1000 ký tự");
  const communes = Array.isArray(x.communes) ? x.communes : [];
  if (!communes.length && !x.all_pending) return fieldErr("communes", "chọn xã hoặc all_pending = true");
  const tasks = S.tasks.filter((t) => t.period_id === p.id && !t.withdrawn && (!x.template_id || t.template_id === x.template_id) && (!communes.length || communes.includes(t.commune_code)));
  const skippedDone = new Set<string>();
  const skippedNo = new Set<string>();
  const picked: Task[] = [];
  for (const t of tasks) {
    if (!taskPending({ status: t.stored }) || !openOf(t, at)) skippedDone.add(t.commune_code);
    else if (!t.assignees.length) skippedNo.add(t.commune_code);
    else picked.push(t);
  }
  if (!picked.length) return fieldErr("communes", "không có xã nào cần nhắc (đã nộp hoặc chưa có cán bộ)");
  const iso = rfc3339(at);
  const pickedCommunes = [...new Set(picked.map((t) => t.commune_code))];
  for (const t of picked) {
    t.last_reminded_at = iso;
    t.log.push({ event: "task.reminded.manual", actor_ref: c.actor, at: iso, changes: [], reason: message, on_behalf_of: null });
  }
  const reminder: Reminder = {
    id: uuidv7(at),
    batch_id: b.id,
    period_id: p.id,
    template_id: x.template_id ?? null,
    mode: "manual",
    message,
    communes: pickedCommunes,
    task_ids: picked.map((t) => t.id),
    recipient_count: new Set(picked.flatMap((t) => t.assignees)).size,
    actor_ref: c.actor,
    at: iso,
  };
  S.reminders.push(reminder);
  return {
    reminder,
    skipped_no_officer: [...skippedNo].filter((x2) => !pickedCommunes.includes(x2)),
    skipped_done: [...skippedDone].filter((x2) => !pickedCommunes.includes(x2) && !skippedNo.has(x2)),
  };
}

export const remindersOf = (b: Batch, periodId: string | null) => S.reminders.filter((r) => r.batch_id === b.id && (!periodId || r.period_id === periodId)).reverse();

// ---------------------------------------------------------------------------
// Kỳ: chốt, mở lại, chốt lại (FN-TI-02.04)
// ---------------------------------------------------------------------------
export function periodView(c: Caller | null, p: Period, now = nowMs()) {
  const b = batchOf(p.batch_id)!;
  const tasks = S.tasks.filter((t) => t.period_id === p.id && !t.withdrawn && (!c || c.role !== "kn_xa" || c.communes.includes(t.commune_code)));
  const byCommune = new Map<string, Task[]>();
  for (const t of tasks) byCommune.set(t.commune_code, [...(byCommune.get(t.commune_code) ?? []), t]);
  const submitted = [...byCommune.values()].filter((ts) => ts.every((t) => t.submitted_id)).length;
  return {
    period: p,
    batch: b,
    submitted,
    total: byCommune.size,
    reopened: tasks.filter((t) => t.reopened_until && Date.parse(t.reopened_until) > now).length,
    runs: S.runs.filter((r) => r.period_id === p.id).sort((x, y) => (x.at < y.at ? 1 : -1)).map(runWire),
  };
}

export function closePeriod(c: Caller, p: Period, at = nowMs()): Result<null> {
  const b = batchOf(p.batch_id)!;
  if (b.status === "withdrawn") return problem(409, "extension.batch_withdrawn", "Đợt đã thu hồi");
  const tasks = S.tasks.filter((t) => t.period_id === p.id && !t.withdrawn);
  const sod = selfActAs(c, tasks);
  if (sod) return sod;
  const iso = rfc3339(at);
  if (p.status === "open") {
    Object.assign(p, { status: "closed", closed_at: iso, closed_by: c.actor, close_mode: "manual" });
    pushLog(S.periodLog, p.id, { event: "period.closed", actor_ref: c.actor, at: iso, changes: [], reason: c.support?.reason || null, on_behalf_of: c.support?.onBehalfOf ?? null });
    consolidate(p, b.forms.map((f) => f.template_id), "system", SYSTEM_ACTOR, null, at);
    return null;
  }
  const reopened = tasks.filter((t) => t.reopened_until);
  if (!reopened.length) return problem(409, "conflict", "Xung đột dữ liệu", { detail: "Kỳ đã chốt" });
  reclose(p, reopened, c.actor, "period.reclosed", at);
  return null;
}

function reclose(p: Period, tasks: Task[], actor: string, event: string, at: number) {
  const tpls = [...new Set(tasks.map((t) => t.template_id))];
  for (const t of tasks) t.reopened_until = null;
  pushLog(S.periodLog, p.id, { event, actor_ref: actor, at: rfc3339(at), changes: [{ field: "communes", old: null, new: [...new Set(tasks.map((t) => t.commune_code))].join(",") }], reason: null, on_behalf_of: null });
  consolidate(p, tpls, "system", SYSTEM_ACTOR, null, at);
}

export function reopenPeriod(c: Caller, p: Period, body: unknown, at = nowMs()): Result<{ task_ids: string[]; dispatch_changed: boolean }> {
  const x = (body ?? {}) as { communes?: string[]; template_ids?: string[]; reason?: string; until?: string };
  const [r, ok] = checkReason(String(x.reason ?? ""), 500);
  if (!ok) return fieldErr("reason", "bắt buộc, 10–500 ký tự");
  const until = Date.parse(String(x.until ?? ""));
  if (!Number.isFinite(until) || until <= at || until > at + 30 * DAY_MS) return fieldErr("until", "thời hạn mở phải sau hiện tại và không quá 30 ngày");
  if (p.status !== "closed") return problem(409, "extension.period_not_closed", "Kỳ chưa chốt");
  const communes = Array.isArray(x.communes) ? x.communes : [];
  const tpls = Array.isArray(x.template_ids) ? x.template_ids : [];
  const tasks = S.tasks.filter((t) => t.period_id === p.id && !t.withdrawn && (!communes.length || communes.includes(t.commune_code)) && (!tpls.length || tpls.includes(t.template_id)));
  if (!tasks.length) return fieldErr("communes", "không có công việc nào của kỳ khớp xã/biểu đã chọn");
  const iso = rfc3339(until);
  for (const t of tasks) t.reopened_until = iso;
  pushLog(S.periodLog, p.id, {
    event: "period.reopened",
    actor_ref: c.actor,
    at: rfc3339(at),
    changes: [
      { field: "reopen_until", old: null, new: iso },
      { field: "communes", old: null, new: [...new Set(tasks.map((t) => t.commune_code))].join(",") },
    ],
    reason: r,
    on_behalf_of: c.support?.onBehalfOf ?? null,
  });
  return { task_ids: tasks.map((t) => t.id), dispatch_changed: S.dispatches.some((d) => d.period_id === p.id && d.status === "sent") };
}

/** Việc nền (job): tự chốt kỳ quá giờ chốt, tự chốt lại xã/biểu hết hạn mở, worker gửi email. Chạy trước mỗi lời gọi. */
export function tick(now = nowMs()): void {
  for (const p of S.periods) {
    const b = batchOf(p.batch_id);
    if (!b || b.status !== "issued") continue;
    if (p.status === "open" && Date.parse(p.auto_close_at) <= now) {
      const at = Date.parse(p.auto_close_at);
      Object.assign(p, { status: "closed", closed_at: rfc3339(at), closed_by: SYSTEM_ACTOR, close_mode: "auto" });
      pushLog(S.periodLog, p.id, { event: "period.auto_closed", actor_ref: SYSTEM_ACTOR, at: rfc3339(at), changes: [], reason: null, on_behalf_of: null });
      consolidate(p, b.forms.map((f) => f.template_id), "system", SYSTEM_ACTOR, null, at);
    }
    if (p.status === "closed") {
      const expired = S.tasks.filter((t) => t.period_id === p.id && t.reopened_until && Date.parse(t.reopened_until) <= now);
      if (expired.length) reclose(p, expired, SYSTEM_ACTOR, "period.reclosed", now);
    }
  }
  for (const d of S.dispatches) {
    if (d.status === "sending" && d.deliver_at !== null && d.deliver_at <= now) {
      const size = d.files.reduce((s, f) => s + f.size_bytes, 0);
      Object.assign(d, { status: "sent", sent_at: rfc3339(d.deliver_at), link_only: size > 10 * 1024 * 1024, deliver_at: null, updated_at: rfc3339(d.deliver_at) });
      d.log.push({ event: "dispatch.sent", actor_ref: SYSTEM_ACTOR, at: d.sent_at!, on_behalf_of: null, detail: { attempt: d.attempts, recipients: d.recipient_ids.length, cc: d.cc_ids.length, attachments: d.files.length, link_only: d.link_only } });
    }
  }
}

// ---------------------------------------------------------------------------
// Tổng hợp các xã (FN-TI-02.05)
// ---------------------------------------------------------------------------
const latestRun = (periodId: string, templateId: string) =>
  S.runs.filter((r) => r.period_id === periodId && r.template_id === templateId).sort((a, b) => b.run_no - a.run_no)[0];

export function consolidate(p: Period, templateIds: string[], trigger: "manual" | "system", actor: string, obo: string | null, at = nowMs()): { runs: Run[]; up_to_date: string[] } {
  const b = batchOf(p.batch_id)!;
  const runs: Run[] = [];
  const upToDate: string[] = [];
  for (const f of b.forms) {
    if (templateIds.length && !templateIds.includes(f.template_id)) continue;
    const fi = formInfo(f.version_id);
    if (!fi) continue;
    const all = S.tasks.filter((t) => t.batch_id === b.id && t.template_id === f.template_id && !t.withdrawn);
    const subOf = (t: Task) => t.subs.find((s) => s.id === t.submitted_id);
    const slotVals: Record<string, Record<string, Record<string, string>>> = {};
    if (fi.block) {
      for (const t of all) {
        if (t.period_id === p.id || !t.submitted_id || t.slot > p.slot) continue;
        for (const r of subOf(t)!.rows) {
          const k = rowKey(t.commune_code, r.indicator);
          for (const l of fi.slotLetter.get(t.slot) ?? []) if (r.input[l]) ((slotVals[t.commune_code] ??= {})[k] ??= {})[l] = r.input[l]!;
        }
      }
    }
    const evalRows: FormEvalInput["rows"] = [];
    const sources: Record<string, Source> = {};
    const missing: string[] = [];
    const regions: RegionRef[] = [];
    let total = 0;
    for (const t of all.filter((x) => x.period_id === p.id)) {
      total++;
      const cm = AG_COMMUNES.find((x) => x.code === t.commune_code);
      regions.push({ commune: t.commune_code, commune_name: communeName(t.commune_code), region: t.region_code, region_name: regionName(t.region_code), sort_order: cm?.sort ?? 0 });
      const sub = subOf(t);
      if (!sub) {
        missing.push(t.commune_code);
        continue;
      }
      sources[t.commune_code] = { submission_id: sub.id, version_no: sub.version_no, submitted_at: sub.event_time, actor_ref: sub.actor_ref, on_behalf_of: sub.on_behalf_of, adjustment_response: sub.adjustment_response };
      const system = systemCells(fi, t.commune_code, S.provincial.get(b.id)?.[f.template_id]?.[t.commune_code]);
      for (const r of sub.rows) {
        const k = rowKey(t.commune_code, r.indicator);
        const input: Record<string, string> = {};
        for (const [l, v] of Object.entries(r.input)) {
          const slot = fi.letterSlot.get(l);
          if (slot === undefined || slot === t.slot) input[l] = v;
        }
        Object.assign(input, slotVals[t.commune_code]?.[k] ?? {});
        evalRows.push({ key: k, commune: t.commune_code, region: t.region_code, ...(r.indicator ? { indicator: r.indicator } : {}), input, system });
      }
    }
    regions.sort((x, y) => x.sort_order - y.sort_order || x.commune.localeCompare(y.commune));
    const res = evaluate(fi, { mode: "draft", open_slots: null, rows: evalRows });
    const rows: Row[] = res.rows.map((r) => ({ key: r.key, commune: r.commune, region: r.region ?? "", ...(r.indicator ? { indicator: r.indicator } : {}), cells: r.cells }));
    const prev = latestRun(p.id, f.template_id);
    const prevSnap = prev ? S.snapshots.get(prev.id) : undefined;
    const [changes, communesChanged] = diff(
      (prevSnap?.rows ?? []).map((r) => ({ key: r.key, commune: r.commune, region: r.region, ...(r.indicator ? { indicator: r.indicator } : {}), cells: r.cells })),
      rows,
      prevSnap?.sources ?? {},
      sources,
      fi.formula,
    );
    missing.sort();
    const provisional = p.status !== "closed" || missing.length > 0;
    const sameSrc = prevSnap && JSON.stringify(Object.keys(prevSnap.sources).sort().map((k) => prevSnap.sources[k]!.submission_id)) === JSON.stringify(Object.keys(sources).sort().map((k) => sources[k]!.submission_id));
    if (prev && !changes.length && prev.provisional === provisional && sameSrc) {
      upToDate.push(f.code);
      continue;
    }
    const run: Run = {
      id: uuidv7(at),
      run_no: (prev?.run_no ?? 0) + 1,
      period_id: p.id,
      template_id: f.template_id,
      version_id: f.version_id,
      form_code: f.code,
      trigger,
      actor_ref: actor,
      on_behalf_of: obo,
      at: rfc3339(at),
      communes_total: total,
      communes_with_data: Object.keys(sources).length,
      communes_changed: communesChanged,
      cells_changed: changes.length,
      provisional,
      missing_communes: missing,
      regions,
      previous_run_id: prev?.id ?? null,
    };
    S.runs.push(run);
    S.snapshots.set(run.id, {
      rows: res.rows.map((r) => ({ key: r.key, commune: r.commune, commune_name: communeName(r.commune), region: r.region ?? "", region_name: regionName(r.region ?? ""), ...(r.indicator ? { indicator: r.indicator } : {}), cells: r.cells })),
      totals: res.totals.map((t) => ({ key: t.key, kind: t.kind, ...(t.region ? { region: t.region, region_name: regionName(t.region) } : {}), ...(t.indicator ? { indicator: t.indicator } : {}), members: t.members, cells: t.cells })),
      sources,
    });
    S.changes.set(
      run.id,
      changes.map((ch) => ({
        id: uuidv7(at),
        commune_code: ch.commune,
        row_key: ch.row_key,
        letter: ch.letter,
        old: ch.old,
        new: ch.new,
        kind: ch.kind,
        submission_id: ch.source?.submission_id ?? null,
        changed_by: ch.source?.actor_ref ?? null,
        on_behalf_of: ch.source?.on_behalf_of ?? null,
        submitted_at: ch.source?.submitted_at ?? null,
      })),
    );
    runs.push(run);
  }
  return { runs, up_to_date: upToDate };
}

export function pendingOf(p: Period, templateId: string | null) {
  const items: { commune_code: string; form_code: string; task_id: string; submission_id: string; submitted_at: string | null; kind: "new" | "resubmit" | "adjusted" }[] = [];
  for (const t of S.tasks) {
    if (t.period_id !== p.id || t.withdrawn || !t.submitted_id || (templateId && t.template_id !== templateId)) continue;
    const run = latestRun(p.id, t.template_id);
    const src = run ? S.snapshots.get(run.id)?.sources[t.commune_code] : undefined;
    if (src?.submission_id === t.submitted_id) continue;
    const sub = t.subs.find((s) => s.id === t.submitted_id)!;
    items.push({ commune_code: t.commune_code, form_code: t.form_code, task_id: t.id, submission_id: sub.id, submitted_at: sub.event_time, kind: !src ? "new" : sub.adjustment_response ? "adjusted" : "resubmit" });
  }
  return { changes: items.length, communes: new Set(items.map((x) => x.commune_code)).size, items };
}

export function runsOf(periodId: string, templateId: string | null) {
  return S.runs.filter((r) => r.period_id === periodId && (!templateId || r.template_id === templateId)).sort((a, b) => (a.at < b.at ? 1 : a.at > b.at ? -1 : b.run_no - a.run_no));
}

export const runOf = (id: string) => S.runs.find((r) => r.id === id);
export const changesOf = (runId: string) => S.changes.get(runId) ?? [];
export const snapshotOf = (runId: string) => S.snapshots.get(runId);

export function consolidationView(p: Period, q: { template_id?: string | null; form_code?: string | null; run_id?: string | null; region?: string | null; compare?: boolean }): Result<Record<string, unknown>> {
  const b = batchOf(p.batch_id)!;
  const f = q.template_id ? b.forms.find((x) => x.template_id === q.template_id) : q.form_code ? b.forms.find((x) => x.code === q.form_code) : b.forms[0];
  if (!f) return problem(404, "extension.consolidation_not_found", "Kỳ chưa có dữ liệu tổng hợp");
  const run = q.run_id ? S.runs.find((r) => r.id === q.run_id && r.period_id === p.id && r.template_id === f.template_id) : latestRun(p.id, f.template_id);
  if (!run) return problem(404, "extension.consolidation_not_found", "Kỳ chưa có dữ liệu tổng hợp");
  const snap = S.snapshots.get(run.id)!;
  const region = q.region || "";
  const changes = S.changes.get(run.id) ?? [];
  let compare: Record<string, unknown> | null = null;
  if (q.compare) {
    const target = Date.parse(p.from_date) - 365 * DAY_MS;
    const cands = S.periods
      .filter((x) => {
        const xb = batchOf(x.batch_id)!;
        return x.id !== p.id && xb.province_code === b.province_code && xb.department_code === b.department_code && xb.report_type === b.report_type && Math.abs(Date.parse(x.from_date) - target) <= 10 * DAY_MS;
      })
      .sort((x, y) => Math.abs(Date.parse(x.from_date) - target) - Math.abs(Date.parse(y.from_date) - target));
    for (const cp of cands) {
      const cb = batchOf(cp.batch_id)!;
      const cf = cb.forms.find((x) => x.code === f.code);
      const cr = cf ? latestRun(cp.id, cf.template_id) : undefined;
      const tot = cr ? S.snapshots.get(cr.id)?.totals.find((t) => t.kind === "province" && !t.indicator) : undefined;
      if (tot) {
        compare = { label: `${cp.label} (${cp.range_label})`, total: tot };
        break;
      }
    }
  }
  return {
    period: p,
    batch: b,
    form_name: f.name,
    run: runWire(run),
    status_label: run.provisional ? "Tạm tính" : "Đã chốt",
    read_only: true,
    rows: snap.rows.filter((r) => !region || r.region === region),
    totals: snap.totals.filter((t) => t.kind === "province" || !region || t.region === region),
    missing: run.missing_communes.map((x) => ({ commune_code: x, commune_name: communeName(x), region_code: regionOf(x), region_name: regionName(regionOf(x)) })),
    changed_cells: changes.map((ch) => ({ row_key: ch.row_key, letter: ch.letter, old: ch.old, kind: ch.kind, changed_by: ch.changed_by, changed_by_display: ch.kind === "computed" ? "(công thức)" : actorDisplay(ch.changed_by), submitted_at: ch.submitted_at })),
    pending: pendingOf(p, f.template_id),
    compare,
  };
}

// ---------------------------------------------------------------------------
// Dashboard (FN-TI-02.01, FN-TW-01.01) — chỉ số từ lần tổng hợp gần nhất (bảng ánh xạ v1-tam như Go)
// ---------------------------------------------------------------------------
interface Snapshots {
  [code: string]: { run: Run; snap: Snapshot; fi: FormInfo } | undefined;
}

function latestSnaps(p: Period): Snapshots {
  const b = batchOf(p.batch_id)!;
  const out: Snapshots = {};
  for (const f of b.forms) {
    const run = latestRun(p.id, f.template_id);
    const fi = formInfo(f.version_id);
    if (run && fi) out[f.code] = { run, snap: S.snapshots.get(run.id)!, fi };
  }
  return out;
}

const colByKey = (fi: FormInfo, key: string) => fi.def.columns.find((c) => c.key === key)?.letter;

function sumCells(rows: SnapRow[], letter: string | undefined, region: string): Decimal {
  let s = Decimal.ZERO;
  if (!letter) return s;
  for (const r of rows) if (!region || r.region === region) s = s.add(dec(r.cells[letter]));
  return s;
}

export function dashboard(c: Caller, q: { province?: string | null; region?: string | null; period_id?: string | null; batch_id?: string | null }, now = nowMs()): Result<Record<string, unknown>> {
  const province = c.province ?? q.province ?? "";
  if (!province) return fieldErr("province", "bắt buộc");
  const region = q.region ?? "";
  const batches = S.batches.filter((b) => b.status === "issued" && b.province_code === province && (c.role === "admin" || b.department_code === c.dept) && (!q.batch_id || b.id === q.batch_id));
  const periods = S.periods.filter((p) => batches.some((b) => b.id === p.batch_id)).sort((a, b) => (a.from_date < b.from_date ? 1 : -1));
  let period: Period | undefined;
  if (q.period_id) {
    period = periods.find((p) => p.id === q.period_id);
    if (!period) return problem(404, "extension.report_period_not_found", "Không tìm thấy kỳ báo cáo");
  } else {
    const started = periods.filter((p) => Date.parse(p.from_date) <= now);
    period = started.find((p) => S.runs.some((r) => r.period_id === p.id)) ?? started[0];
  }
  const regionNames = Object.fromEntries(AG_REGIONS.map((r) => [r.code, r.name]));
  const metric = (v: Decimal | null, asOf: string | null) => ({ value: v === null ? null : v.toString(), as_of: v === null ? null : asOf });
  const empty = { value: null, as_of: null };
  const cards: Record<string, unknown> = {};
  for (const k of ["sown_area", "sown_plan", "sown_pct", "harvest_area", "harvest_output", "harvest_yield", "veg_sown", "veg_harvest_area", "veg_output", "pest_area", "pest_severe", "fruit_area"]) cards[k] = empty;
  const base = {
    province,
    region,
    period: period ?? null,
    batch: period ? batchOf(period.batch_id) : null,
    periods,
    region_names: regionNames,
    versions: { metrics: "kn.dashboard.metrics/v1-tam", alerts: "kn.dashboard.alerts/v1-tam" },
    read_only: true,
  };
  if (!period) return { ...base, updated_at: null, empty: true, cards: { ...cards, submitted: { communes_submitted: 0, communes_total: 0 } }, sown_by_week: [], tra: [], sown_by_region: [], pests_top: [], unsubmitted: [], by_commune: [], alerts: [] };
  const snaps = latestSnaps(period);
  const updatedAt = Object.values(snaps).reduce<string | null>((m, s) => (s && (!m || s.run.at > m) ? s.run.at : m), null);
  const t01 = snaps.T01;
  const t04 = snaps.T04;
  if (t01) {
    const area = sumCells(t01.snap.rows, colByKey(t01.fi, "luy_ke"), region);
    const plan = sumCells(t01.snap.rows, colByKey(t01.fi, "kh_tinh_giao"), region);
    cards.sown_area = metric(area, t01.run.at);
    cards.sown_plan = metric(plan, t01.run.at);
    cards.sown_pct = metric(ratio(area, plan), t01.run.at);
  }
  if (t04) {
    cards.veg_sown = metric(sumCells(t04.snap.rows, colByKey(t04.fi, "dt_xg"), region), t04.run.at);
    cards.veg_harvest_area = metric(sumCells(t04.snap.rows, colByKey(t04.fi, "dt_th"), region), t04.run.at);
    cards.veg_output = metric(sumCells(t04.snap.rows, colByKey(t04.fi, "san_luong"), region), t04.run.at);
  }
  // Nộp thời gian thực: xã không còn công việc phải gửi.
  const tasks = S.tasks.filter((t) => t.period_id === period!.id && !t.withdrawn && (!region || t.region_code === region));
  const communes = [...new Set(tasks.map((t) => t.commune_code))];
  const pendingBy = (cm: string) => tasks.filter((t) => t.commune_code === cm && taskPending({ status: t.stored }));
  const unsubmitted = communes
    .filter((cm) => pendingBy(cm).length)
    .map((cm) => {
      const off = officersOf(AG_DEPARTMENT, cm);
      return { commune_code: cm, commune_name: communeName(cm), region_code: regionOf(cm), pending_forms: [...new Set(pendingBy(cm).map((t) => t.form_code))].sort(), officers: off.map((o) => ({ actor_ref: o.actor_ref, display: o.display })), no_officer: !off.length };
    });
  cards.submitted = { communes_submitted: communes.length - unsubmitted.length, communes_total: communes.length };
  const sownByWeek: Record<string, unknown>[] = [];
  if (t01) {
    for (let slot = 1; slot <= period.slot; slot++) {
      const letter = t01.fi.slotLetter.get(slot)?.[0];
      const lp = periodsOf(period.batch_id).find((x) => x.slot === slot);
      sownByWeek.push({ key: String(slot), label: lp?.label ?? `Tuần ${slot}`, value: sumCells(t01.snap.rows, letter, region).toString(), plan: null, pct: null });
    }
  }
  const sownByRegion = t01
    ? AG_REGIONS.filter((r) => !region || r.code === region).map((r) => {
        const a = sumCells(t01.snap.rows, colByKey(t01.fi, "luy_ke"), r.code);
        const pl = sumCells(t01.snap.rows, colByKey(t01.fi, "kh_tinh_giao"), r.code);
        return { key: r.code, label: r.name, value: a.toString(), plan: pl.toString(), pct: ratio(a, pl).toString() };
      })
    : [];
  const byCommune = communes.map((cm) => {
    const row = t01?.snap.rows.find((r) => r.commune === cm);
    return {
      commune_code: cm,
      commune_name: communeName(cm),
      sown_area: row ? dec(row.cells[colByKey(t01!.fi, "luy_ke") ?? ""]).toString() : null,
      sown_plan: row ? dec(row.cells[colByKey(t01!.fi, "kh_tinh_giao") ?? ""]).toString() : null,
      pest_area: null,
      submitted: !pendingBy(cm).length,
    };
  });
  const alerts: { level: "urgent" | "warning" | "info"; code: string; message: string }[] = [];
  if (unsubmitted.length) alerts.push({ level: now >= Date.parse(period.due_at) ? "warning" : "info", code: "unsubmitted", message: `${unsubmitted.length} xã chưa nộp ${period.label}` });
  for (const r of sownByRegion) {
    if (dec(r.plan).gt(Decimal.ZERO) && dec(r.pct).lt(Decimal.fromInt(50))) alerts.push({ level: "warning", code: "low_progress", message: `Tiến độ xuống giống thấp: ${r.label} đạt ${r.pct} % kế hoạch` });
  }
  if (updatedAt) alerts.push({ level: "info", code: "consolidated", message: `Đã cập nhật tổng hợp lúc ${vnClock(Date.parse(updatedAt))}` });
  return {
    ...base,
    updated_at: updatedAt,
    empty: !updatedAt,
    cards,
    sown_by_week: sownByWeek,
    tra: [],
    sown_by_region: sownByRegion,
    pests_top: [],
    unsubmitted,
    by_commune: byCommune,
    alerts,
  };
}

export function harvestYield(output: string, area: string): string {
  return yieldOf(dec(output), dec(area)).toString();
}

// ---------------------------------------------------------------------------
// Số đếm menu, Báo cáo của xã (FN-XA-03.07)
// ---------------------------------------------------------------------------
export function badgesFor(c: Caller, now = nowMs()): Record<string, number> {
  const counts: Record<string, number> = {
    "extension.report_tasks.todo": 0,
    "extension.report_status.not_submitted": 0,
    "extension.submissions.pending_review": 0,
    "extension.report_tasks.ad_hoc_todo": 0,
    "extension.report_status.ad_hoc_pending": 0,
  };
  for (const t of S.tasks) {
    if (!taskVisible(c, t) || !openOf(t, now)) continue;
    const b = batchOf(t.batch_id)!;
    const p = periodOf(t.period_id)!;
    const adhoc = b.report_type === "adhoc";
    if (c.role === "kn_xa") {
      if (taskPending({ status: t.stored }) && Date.parse(p.from_date) <= now) counts[adhoc ? "extension.report_tasks.ad_hoc_todo" : "extension.report_tasks.todo"]!++;
    } else {
      if (taskPending({ status: t.stored }) && Date.parse(p.from_date) <= now) counts[adhoc ? "extension.report_status.ad_hoc_pending" : "extension.report_status.not_submitted"]!++;
      if (t.stored === "submitted") counts["extension.submissions.pending_review"]!++;
    }
  }
  return counts;
}

/** Số đếm cho /v1/me/menu-badges (kn.ts) theo ngữ cảnh. */
export function cycleBadges(contextId: string): Record<string, number> {
  const st = knStoredContext(contextId);
  if (!st || st.ctx.role === "kn_tw" || st.ctx.role === "admin") return {};
  ensureSeeded();
  tick();
  return badgesFor(callerFromStored(st));
}

export function communeStats(c: Caller, commune: string, batchId: string | null, now = nowMs()) {
  const tasks = S.tasks.filter((t) => taskVisible(c, t) && t.commune_code === commune && (!batchId || t.batch_id === batchId));
  const last = [...tasks].sort((a, b) => (periodOf(a.period_id)!.due_at < periodOf(b.period_id)!.due_at ? -1 : 1)).at(-1);
  const bid = batchId ?? last?.batch_id ?? null;
  const inBatch = tasks.filter((t) => t.batch_id === bid);
  const due = inBatch.filter((t) => Date.parse(periodOf(t.period_id)!.due_at) <= now);
  const submitted = inBatch.filter((t) => t.submitted_id);
  const onTime = inBatch.filter((t) => t.first_submitted_at && t.first_submitted_at <= periodOf(t.period_id)!.due_at);
  const latestT01 = inBatch.filter((t) => t.form_code === "T01" && t.submitted_id).sort((a, b) => b.slot - a.slot)[0];
  const sub = latestT01?.subs.find((s) => s.id === latestT01.submitted_id);
  const fi = latestT01 ? formInfo(latestT01.version_id) : null;
  const area = sub && fi ? dec(sub.rows[0]?.cells[colByKey(fi, "luy_ke") ?? ""]) : null;
  const plan = sub && fi ? dec(sub.rows[0]?.cells[colByKey(fi, "kh_tinh_giao") ?? ""] ?? S.provincial.get(latestT01!.batch_id)?.[latestT01!.template_id]?.[commune]?.D) : null;
  const m = (v: Decimal | null) => ({ value: v ? v.toString() : null, as_of: v ? sub!.event_time : null });
  return {
    commune_code: commune,
    batch: bid ? batchOf(bid) : null,
    sown_area: m(area),
    sown_plan: m(plan),
    sown_pct: m(area && plan ? ratio(area, plan) : null),
    pest_area: { value: null, as_of: null },
    tasks_total: inBatch.length,
    tasks_submitted: submitted.length,
    tasks_due: due.length,
    tasks_on_time: onTime.length,
  };
}

export function listPeriods(c: Caller, q: { province?: string | null; report_type?: string | null; batch_id?: string | null }) {
  return S.periods
    .filter((p) => {
      const b = batchOf(p.batch_id)!;
      return b.status === "issued" && batchVisible(c, b) && (!q.province || b.province_code === q.province) && (!q.report_type || b.report_type === q.report_type) && (!q.batch_id || b.id === q.batch_id);
    })
    .sort((a, b) => (a.from_date < b.from_date ? 1 : a.from_date > b.from_date ? -1 : 0));
}

// ---------------------------------------------------------------------------
// Báo cáo Chi cục (pf/reports, FN-TI-02.06) — Google Sheet TẮT mặc định (ADR-0026)
// ---------------------------------------------------------------------------
export const GSHEET_ENABLED = false;

export function recipientsOf(c: Caller, province: string) {
  return S.recipients.filter((r) => r.province_code === province && (c.role === "admin" || r.department_code === c.dept) && r.active);
}

export function maskAddress(a: string): string {
  const [u = "", d = ""] = a.split("@");
  return `${u.slice(0, 2)}•••@${d}`;
}

export const recipientWire = (r: Recipient, masked: boolean) => ({
  id: r.id,
  label: r.label,
  address: masked ? maskAddress(r.address) : r.address,
  masked,
  kind: r.kind,
  is_default: r.is_default,
  active: r.active,
  last_used_at: r.last_used_at,
});

export function addRecipient(c: Caller, body: unknown, at = nowMs()): Result<Recipient> {
  const x = (body ?? {}) as { label?: string; address?: string; kind?: string; is_default?: boolean };
  const address = String(x.address ?? "").trim().toLowerCase();
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(address)) return fieldErr("address", "email không hợp lệ");
  if (x.kind !== "to" && x.kind !== "cc") return fieldErr("kind", "to | cc");
  if (S.recipients.some((r) => r.active && r.address === address && r.province_code === AG_PROVINCE)) return problem(409, "reports.dispatch_recipient_duplicate", "Địa chỉ đã có trong danh bạ");
  const r: Recipient = { id: uuidv7(at), province_code: AG_PROVINCE, department_code: c.dept ?? AG_DEPARTMENT, label: String(x.label ?? address).trim() || address, address, kind: x.kind, is_default: x.is_default ?? true, active: true, last_used_at: null };
  S.recipients.push(r);
  return r;
}

export const recipientOf = (id: string) => S.recipients.find((r) => r.id === id);

const fileSafe = (s: string) => s.replace(/[\\/:*?"<>|]/g, "_");

export function createDispatch(c: Caller, body: unknown, at = nowMs()): Result<Dispatch> {
  const x = (body ?? {}) as { period_id?: string; form_codes?: string[]; formats?: string[] };
  const p = x.period_id ? periodOf(x.period_id) : undefined;
  const b = p ? batchOf(p.batch_id) : undefined;
  if (!p || !b || !batchVisible(c, b)) return problem(404, "extension.report_period_not_found", "Không tìm thấy kỳ báo cáo");
  const formats = Array.isArray(x.formats) && x.formats.length ? x.formats : ["xlsx"];
  if (formats.some((f) => !["xlsx", "pdf", "gsheet"].includes(f))) return fieldErr("formats", "xlsx | pdf | gsheet");
  if (formats.includes("gsheet") && !GSHEET_ENABLED) return problem(409, "reports.gsheet_disabled", "Tạo link Google Sheet đang tắt (ADR-0026 chưa được chấp nhận)");
  const forms = b.forms.filter((f) => !x.form_codes?.length || x.form_codes.includes(f.code));
  const runs = forms.map((f) => latestRun(p.id, f.template_id)).filter((r): r is Run => !!r);
  if (!runs.length) return problem(409, "reports.dispatch_no_data", "Kỳ chưa có dữ liệu tổng hợp — bấm Cập nhật ở Báo cáo tổng hợp các xã");
  const iso = rfc3339(at);
  const files: DispFile[] = [];
  for (const r of runs) {
    for (const fmt of formats.filter((f): f is "xlsx" | "pdf" => f !== "gsheet")) {
      const bytes = fileBytes(r, fmt);
      files.push({ id: uuidv7(at), format: fmt, form_code: r.form_code, file_name: fileSafe(`${r.form_code}_${p.label}.${fmt}`), sha256: hashHex(bytes), size_bytes: bytes.length, created_at: iso });
    }
  }
  const missing = [...new Set(runs.flatMap((r) => r.missing_communes))].map(communeName);
  const d: Dispatch = {
    id: uuidv7(at),
    department_code: b.department_code,
    province_code: b.province_code,
    period_id: p.id,
    period_label: p.label,
    run_ids: runs.map((r) => r.id),
    forms: runs.map((r) => r.form_code),
    formats,
    provisional: runs.some((r) => r.provisional),
    missing_communes: missing,
    status: "exported",
    recipient_ids: [],
    cc_ids: [],
    subject: null,
    body: null,
    link_only: false,
    attempts: 0,
    last_error: null,
    sent_at: null,
    created_by: c.actor,
    created_at: iso,
    updated_at: iso,
    files,
    log: [],
    deliver_at: null,
  };
  d.log.push({
    event: "dispatch.exported",
    actor_ref: c.actor,
    at: iso,
    on_behalf_of: c.support?.onBehalfOf ?? null,
    detail: { period_id: p.id, period_label: p.label, run_ids: d.run_ids, forms: d.forms, files: files.map((f) => ({ file_id: f.id, form_code: f.form_code, format: f.format, sha256: f.sha256, size_bytes: f.size_bytes })), gsheet_links: [], provisional: d.provisional },
  });
  S.dispatches.push(d);
  return d;
}

export function sendDispatch(c: Caller, d: Dispatch, body: unknown, at = nowMs()): Result<Dispatch> {
  if (d.status === "sending") return problem(409, "reports.dispatch_sending", "Đang gửi — chờ kết quả trước khi gửi lại");
  const x = (body ?? {}) as { recipient_ids?: string[]; cc_ids?: string[]; subject?: string; body?: string };
  let to = Array.isArray(x.recipient_ids) ? x.recipient_ids : [];
  let cc = Array.isArray(x.cc_ids) ? x.cc_ids : [];
  for (const id of [...to, ...cc]) if (!S.recipients.some((r) => r.id === id && r.active)) return problem(404, "reports.dispatch_recipient_not_found", "Không tìm thấy người nhận");
  if (!to.length && !cc.length) {
    to = recipientsOf(c, d.province_code).filter((r) => r.kind === "to" && r.is_default).map((r) => r.id);
    cc = recipientsOf(c, d.province_code).filter((r) => r.kind === "cc" && r.is_default).map((r) => r.id);
  }
  if (!to.length) return problem(422, "reports.dispatch_no_recipients", "Chưa chọn người nhận Chi cục");
  const subject = (x.subject ?? "").trim() || `Báo cáo Phòng Trồng trọt — ${d.period_label} — ${d.forms.join(", ")}${d.provisional ? " (Tạm tính)" : ""}`;
  if ([...subject].length > 300 || /\n/.test(subject)) return fieldErr("subject", "tối đa 300 ký tự, một dòng");
  if ((x.body ?? "").length > 5000) return fieldErr("body", "tối đa 5000 ký tự");
  const iso = rfc3339(at);
  Object.assign(d, { status: "sending", recipient_ids: to, cc_ids: cc, subject, body: x.body ?? null, attempts: d.attempts + 1, updated_at: iso, deliver_at: at + 1500 });
  for (const r of S.recipients) if ([...to, ...cc].includes(r.id)) Object.assign(r, { is_default: true, last_used_at: iso });
  d.log.push({ event: "dispatch.send_requested", actor_ref: c.actor, at: iso, on_behalf_of: c.support?.onBehalfOf ?? null, detail: { recipient_ids: to, cc_ids: cc, attempt: d.attempts, subject } });
  return d;
}

export const dispatchOf = (id: string) => S.dispatches.find((d) => d.id === id);
export const dispatchesOf = (c: Caller, q: { period_id?: string | null; province?: string | null }) =>
  S.dispatches.filter((d) => (c.province === null || d.province_code === c.province) && (c.role === "admin" || d.department_code === c.dept) && (!q.period_id || d.period_id === q.period_id) && (!q.province || d.province_code === q.province)).reverse();

export function dispatchWire(d: Dispatch) {
  const { files, log, ...rest } = d;
  const dispatch: Partial<Dispatch> = { ...rest };
  delete dispatch.deliver_at;
  return { dispatch, files, log };
}

function hashHex(bytes: Uint8Array): string {
  let out = "";
  for (let k = 0; k < 8; k++) {
    let h = 2166136261 ^ k;
    for (let i = 0; i < bytes.length; i++) h = Math.imul(h ^ bytes[i]!, 16777619) >>> 0;
    out += h.toString(16).padStart(8, "0");
  }
  return out;
}

/** Tệp xuất (giả lập): Excel đúng bố cục mẫu kèm số tổng hợp; PDF tối giản. */
export function fileBytes(r: Run, fmt: "xlsx" | "pdf"): Uint8Array {
  const fi = formInfo(r.version_id);
  const snap = S.snapshots.get(r.id);
  if (fmt === "xlsx" && fi) {
    const p = periodOf(r.period_id)!;
    const b = batchOf(p.batch_id)!;
    return exportXlsx(layout(fi.def, { periodLabels: periodsOf(b.id).map((x) => x.label), season: b.season_label }));
  }
  const total = snap?.totals.find((t) => t.kind === "province");
  const text = `%PDF-1.4\n% ${r.form_code} ${r.provisional ? "Tam tinh" : "Da chot"} ${JSON.stringify(total?.cells ?? {})}\n%%EOF\n`;
  return new TextEncoder().encode(text);
}

export function downloadUrl(d: Dispatch, f: DispFile): string {
  const r = S.runs.find((x) => d.run_ids.includes(x.id) && x.form_code === f.form_code);
  const bytes = r ? fileBytes(r, f.format) : new Uint8Array();
  let bin = "";
  for (const x of bytes) bin += String.fromCharCode(x);
  const type = f.format === "xlsx" ? "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" : "application/pdf";
  return `data:${type};base64,${btoa(bin)}`;
}

// ---------------------------------------------------------------------------
// Dữ liệu khởi đầu
// ---------------------------------------------------------------------------
const BINH = "kn-binh";

/** Số liệu mẫu theo xã: KH tỉnh giao (ha) và diện tích xuống giống tăng thêm mỗi tuần. */
const SEED_T01: Record<string, { plan: string; weeks: string[] }> = {
  "91301": { plan: "126", weeks: ["28", "38", "20", "12", "8"] },
  "91302": { plan: "290", weeks: ["30", "20", "45", "60", "40"] },
  "91303": { plan: "420", weeks: ["120", "160", "90", "30", "10"] },
  "91304": { plan: "1100", weeks: ["400", "380", "210", "60", "20"] },
  "91305": { plan: "980", weeks: ["300", "250", "200", "100", "50"] },
  "91306": { plan: "3600", weeks: ["900", "1100", "700", "400", "200"] },
  "91307": { plan: "500", weeks: ["100", "100", "100", "100", "50"] },
};

function seedCaller(accountId: string, role: KnRole, communes: string[] = []): Caller {
  return { contextId: "seed", accountId, role, dept: AG_DEPARTMENT, province: AG_PROVINCE, communes, actor: stableUuid(accountId), display: accountId, support: null };
}

function seedSubmitter(commune: string): Caller {
  const off = officersOf(AG_DEPARTMENT, commune)[0];
  return { ...seedCaller(off?.account_id ?? "seed", "kn_xa", [commune]), actor: off?.actor_ref ?? stableUuid("seed") };
}

export function seed(now = Date.now()): void {
  const mon = mondayOf(now);
  // Mẫu T01/T04 giả lập áp dụng từ 07/09/2026 (Tuần 37) — đợt bắt đầu không sớm hơn.
  const start = Math.max(mon - 28 * DAY_MS, Date.parse("2026-09-07"));
  const forms = mockAssignedForms(AG_DEPARTMENT, "commune", "weekly", ymd(start)).filter((f) => f.template.code === "T01" || f.template.code === "T04");
  if (!forms.length) return;
  const binh = seedCaller(BINH, "kn_tinh");
  const pv: Record<string, Record<string, Record<string, string>>> = {};
  const t01 = forms.find((f) => f.template.code === "T01");
  if (t01) pv[t01.template.id] = Object.fromEntries(Object.entries(SEED_T01).map(([c, v]) => [c, { D: v.plan }]));
  const plan = planBatch(binh, {
    report_type: "weekly",
    template_ids: forms.map((f) => f.template.id),
    scope: { mode: "province" },
    from_date: ymd(start),
    to_date: ymd(mon + 6 * DAY_MS),
    season_label: "Thu Đông 2026",
    note: "Nộp đủ biểu tuần",
    provincial_values: pv,
  });
  if (isErr(plan)) return;
  const issueAt = start - DAY_MS + 7 * 3_600_000;
  const issued = issueBatch(binh, plan, issueAt);
  if (isErr(issued)) return;
  const { batch, periods } = issued;
  for (const p of periods) {
    const slot = p.slot;
    const due = Date.parse(p.due_at);
    const current = slot === periods.length;
    for (const commune of batch.communes) {
      const author = seedSubmitter(commune);
      if (!officersOf(AG_DEPARTMENT, commune).length) continue; // 91307: chưa có cán bộ → không nộp
      const at = Math.min(due - 20 * 3_600_000, now - 3_600_000);
      for (const f of batch.forms) {
        const t = S.tasks.find((x) => x.period_id === p.id && x.template_id === f.template_id && x.commune_code === commune)!;
        // Tuần hiện tại: Mỹ Hòa Hưng, Bình Đức đã gửi T01; T04 Mỹ Hòa Hưng lưu nháp; còn lại chưa nhập. Tuần trước: Núi Sam chưa nộp.
        if (current && !(f.code === "T01" && ["91301", "91302", "91304"].includes(commune)) && !(f.code === "T04" && commune === "91301")) continue;
        if (slot === periods.length - 1 && commune === "91306") continue;
        const kind = current && f.code === "T04" ? "draft" : "submitted";
        let input: Record<string, string>;
        if (f.code === "T01") {
          const letter = formInfo(f.version_id)?.slotLetter.get(slot)?.[0] ?? "F";
          input = { E: SEED_T01[commune]!.plan, [letter]: SEED_T01[commune]!.weeks[slot - 1] ?? "0" };
        } else {
          const base = Number(SEED_T01[commune]!.weeks[0]) / 10;
          input = { K: String(base * slot), L: String(base * (slot - 1)), N: String(base * (slot - 1) * 20), O: String(base), P: "0", R: "0" };
        }
        tick(at);
        submit(author, t, { kind, rows: [{ input }] }, at);
      }
    }
    if (!current) {
      tick(Math.min(Date.parse(p.auto_close_at), now));
      if (p.status === "open") consolidate(p, [], "manual", stableUuid(BINH), null, Math.min(due + 2 * 3_600_000, now - 1800_000));
    } else {
      consolidate(p, [], "manual", stableUuid(BINH), null, now - 1800_000);
    }
  }
  S.recipients.push(
    { id: uuidv7(issueAt), province_code: AG_PROVINCE, department_code: AG_DEPARTMENT, label: "Chi cục Trồng trọt và BVTV An Giang", address: "trongtrot.chicuc@angiang.gov.vn", kind: "to", is_default: true, active: true, last_used_at: null },
    { id: uuidv7(issueAt), province_code: AG_PROVINCE, department_code: AG_DEPARTMENT, label: "Trần Thị Bình (Phòng Trồng trọt)", address: "binh.tt@angiang.gov.vn", kind: "cc", is_default: true, active: true, last_used_at: null },
  );
}

let seeded = false;
/** Trạng thái (tạo dữ liệu khởi đầu ở lần dùng đầu tiên — sau khi mẫu báo cáo giả lập đã nạp). */
export function ensureSeeded(): void {
  if (seeded) return;
  seeded = true;
  seed(S.now());
}

/** Cho kiểm thử: đồng hồ cố định và (tùy chọn) không có dữ liệu khởi đầu. */
export function resetReportCycle(now: () => number = Date.now, withSeed = true): void {
  S = emptyState(now);
  infoCache.clear();
  seeded = !withSeed;
  if (withSeed) ensureSeeded();
}

export const reportCycleState = () => S;
