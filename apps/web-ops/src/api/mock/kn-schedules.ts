// ============================================================================
// BACKEND GIẢ LẬP — Lịch công tác tuần KN v7.7 P5 (FN-XA-03.08 đăng ký, FN-TI-02.07 duyệt).
// Đường dẫn, tham số, hình dạng phản hồi theo ĐÚNG contracts/openapi/application.yaml (/v1/extension/work-schedules*).
// Quy tắc mô phỏng:
//   - Ngữ cảnh KN lấy từ X-AG-Context (P1); không có → suy từ vai realm (kn_officer → kn_xa, kn_province_admin →
//     kn_tinh, platform_admin → admin chỉ đọc). Cấp Quốc gia (kn_tw) → 403.
//   - Cán bộ xã chỉ thấy lịch của mình; Cấp Tỉnh thấy lịch cùng phòng ban, cùng tỉnh; Admin xem mọi lịch.
//   - Nháp → Chờ duyệt → Đã duyệt / Yêu cầu chỉnh sửa (lý do ≥ 10 ký tự). Gửi lại lịch đã duyệt = điều chỉnh
//     (amendment, bản đã duyệt giữ hiệu lực, mục khác bản đã duyệt có changed = true).
//   - Mỗi lần lưu = một phiên bản mới (sổ chỉ ghi thêm, RB-1); base_version lệch → 409 extension.schedule_version_conflict.
//   - Tuần đã kết thúc → 409 extension.week_locked; tự duyệt → 403 self_approval_forbidden (R-56);
//     chỉ cấp quản lý "Trưởng phòng" duyệt (Q-05, tạm). Gọi lặp cùng Idempotency-Key: lớp chung của handler.ts.
// Dữ liệu là GIẢ LẬP — không phải người thật. Trạng thái nằm trong bộ nhớ tab.
// ============================================================================
import { uuidv7 } from "../idempotency";
import type { components } from "../generated/application";
import { COOPERATIVES } from "./data";
import { json, problem, type Ctx, type Route } from "./http";
import { findKnAccount } from "./kn-accounts";
import { knContext } from "./kn";
import type { MockPersona } from "./personas";
import {
  conflictIndexes,
  diffItems,
  ITEM_FIELD_LABEL,
  itemSummary,
  looksLikePersonalNumber,
  SESSION_LABEL,
  STATUS_LABEL,
  timeError,
  type KnLogEntry,
  type WsCounts,
  type WsDetail,
  type WsItem,
  type WsItemInput,
  type WsListItem,
  type WsMine,
  type WsSchedule,
  type WsSettings,
  type WsStatus,
  type WsUnregistered,
  type WsWarning,
} from "@/kn/schedule/model";
import {
  addDays,
  currentWeekStart,
  dayColumnLabel,
  DEFAULT_REVIEW_DUE_OFFSET_HOURS,
  DEFAULT_SUBMIT_DUE_OFFSET_HOURS,
  inWeek,
  isIsoDate,
  isMonday,
  isWeekLocked,
  nextWeekStart,
  reviewDueAt,
  submitDueAt,
  vnMidnight,
  weekEnd,
  weekRangeLabel,
} from "@/kn/schedule/week";

type Cooperative = components["schemas"]["Cooperative"];
type AdminUnit = components["schemas"]["AdminUnit"];

// ---------------------------------------------------------------------------
// Danh bạ giả lập: xã, HTX, cán bộ Phòng Trồng trọt TTKN An Giang
// ---------------------------------------------------------------------------
export const WS_PROVINCE = "91";
export const WS_DEPARTMENT = "trong_trot";

/** Xã/phường của các tài khoản KN giả lập (kn-accounts.ts dùng mã 913xx). */
export const KN_COMMUNES: readonly { code: string; name: string }[] = [
  { code: "91301", name: "Mỹ Hòa Hưng" },
  { code: "91302", name: "Bình Đức" },
  { code: "91303", name: "Long Xuyên" },
  { code: "91304", name: "Vĩnh Tế" },
  { code: "91305", name: "An Cư" },
  { code: "91306", name: "Núi Sam" },
  // P6s: xã chưa có cán bộ phụ trách Phòng Trồng trọt (cảnh báo khi phát hành biểu).
  { code: "91307", name: "Châu Phong" },
];

const communeName = (code: string | null | undefined) => KN_COMMUNES.find((c) => c.code === code)?.name ?? code ?? "";

/** HTX giả lập của các xã KN (địa điểm "HTX" của mục công tác). */
const KN_COOP_NAMES: readonly [string, string][] = [
  ["91301", "HTX Mỹ Hòa"],
  ["91301", "HTX Nông nghiệp Mỹ Hòa Hưng"],
  ["91302", "HTX Bình Đức"],
  ["91303", "HTX Long Xuyên"],
  ["91304", "HTX Vĩnh Tế"],
  ["91305", "HTX An Cư"],
  ["91306", "HTX Núi Sam"],
];

export const KN_COOPS: readonly Cooperative[] = KN_COOP_NAMES.map(([code, name], i) => {
  const base = COOPERATIVES[0]!.api;
  return {
    ...base,
    ref: `01927b3e-0000-7000-8000-0000000c${String(i + 1).padStart(4, "0")}`,
    code: `HTX-AG-${String(i + 1).padStart(3, "0")}`,
    name,
    admin_unit_code: code,
    province_code: WS_PROVINCE,
    commune_name: communeName(code),
    province_name: "An Giang",
    active: true,
  };
});

export interface WsOfficer {
  actor_ref: string;
  /** Tài khoản đăng nhập được (kn-accounts.ts); null = chỉ có trong danh bạ giả lập. */
  account_id: string | null;
  display: string;
  level: "commune" | "province";
  province_code: string;
  department_code: string;
  communes: string[];
  mgmt_level: string;
}

/** UUID ổn định từ một chuỗi (actor_ref giả lập của tài khoản). */
export function stableUuid(seed: string): string {
  let h1 = 0x811c9dc5;
  let h2 = 0x01000193;
  for (let i = 0; i < seed.length; i++) {
    const c = seed.charCodeAt(i);
    h1 = Math.imul(h1 ^ c, 16777619) >>> 0;
    h2 = Math.imul(h2 ^ c, 2246822519) >>> 0;
  }
  const hex = (h1.toString(16).padStart(8, "0") + h2.toString(16).padStart(8, "0")).repeat(2);
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-7${hex.slice(13, 16)}-8${hex.slice(17, 20)}-${hex.slice(20, 32)}`;
}

const officer = (key: string, display: string, level: WsOfficer["level"], communes: string[], mgmt: string, account: string | null = null): WsOfficer => ({
  actor_ref: stableUuid(account ?? `ws-${key}`),
  account_id: account,
  display,
  level,
  province_code: WS_PROVINCE,
  department_code: WS_DEPARTMENT,
  communes,
  mgmt_level: mgmt,
});

export const WS_ROSTER: readonly WsOfficer[] = [
  officer("an", "Nguyễn Văn An", "commune", ["91301", "91302"], "Chuyên viên", "kn-an"),
  officer("hoa", "Lý Thị Hoa", "commune", ["91304"], "Chuyên viên"),
  officer("phuc", "Huỳnh Văn Phúc", "commune", ["91305"], "Chuyên viên"),
  officer("thu", "Đặng Thị Thu", "commune", ["91306"], "Chuyên viên"),
  officer("binh", "Trần Thị Bình", "province", [], "Trưởng phòng", "kn-binh"),
  officer("mai", "Lê Thị Mai", "province", [], "Chuyên viên"),
];

/** Cấp quản lý được duyệt (Q-05, tạm chờ khách). */
export const APPROVER_LEVELS = ["Trưởng phòng"];

// ---------------------------------------------------------------------------
// Người gọi
// ---------------------------------------------------------------------------
export interface WsCaller {
  role: "kn_xa" | "kn_tinh" | "admin";
  actor_ref: string;
  display: string;
  department_code: string;
  province_code: string | null;
  communes: string[];
  level: "commune" | "province";
  approver: boolean;
}

type Fail = { ok: false; status: number; code: string; title: string; extra?: Record<string, unknown> };
type Ok<T> = { ok: true; status: number; body: T };
export type Result<T> = Ok<T> | Fail;

const fail = (status: number, code: string, title: string, extra?: Record<string, unknown>): Fail => ({ ok: false, status, code, title, extra });
const ok = <T>(body: T, status = 200): Ok<T> => ({ ok: true, status, body });
const invalid = (errors: { field: string; reason: string }[]): Fail => fail(422, "validation_failed", "Validation failed", { errors });

/** Ngữ cảnh KN của người gọi (P1 X-AG-Context) hoặc suy từ vai realm khi chưa có P1. */
export function callerOf(persona: MockPersona, contextId: string | null): Result<WsCaller> {
  const account = findKnAccount(persona.id);
  const ctx = contextId ? knContext(contextId) : undefined;
  const asg = ctx && account ? account.assignments.find((a) => a.assignment_id === ctx.assignment_id) : undefined;
  if (ctx && asg && account) {
    if (ctx.role === "kn_tw") return fail(403, "forbidden", "National level has no access to work schedules");
    const actor = stableUuid(account.id);
    return ok({
      role: ctx.role,
      actor_ref: actor,
      display: account.displayName,
      department_code: ctx.department_code ?? WS_DEPARTMENT,
      province_code: ctx.role === "admin" ? null : WS_PROVINCE,
      communes: ctx.communes.map((c) => c.code),
      level: ctx.role === "kn_xa" ? "commune" : "province",
      approver: ctx.role === "kn_tinh" && APPROVER_LEVELS.includes(asg.mgmt_level ?? ""),
    });
  }
  const roles = persona.me.roles;
  const actor = stableUuid(persona.id);
  const province = persona.me.org?.admin_unit_code ?? null;
  if (roles.includes("platform_admin")) {
    return ok({ role: "admin", actor_ref: actor, display: persona.displayName, department_code: WS_DEPARTMENT, province_code: null, communes: [], level: "province", approver: false });
  }
  if (roles.includes("kn_province_admin")) {
    // Chưa có P1: không biết cấp quản lý → coi như được duyệt (tạm).
    return ok({ role: "kn_tinh", actor_ref: actor, display: persona.displayName, department_code: WS_DEPARTMENT, province_code: province, communes: [], level: "province", approver: true });
  }
  if (roles.includes("kn_officer")) {
    const communes = persona.me.scope.level === "commune" ? persona.me.scope.units : [];
    const prov = province && province.length === 2 ? province : (communes[0]?.slice(0, 2) ?? null);
    return ok({ role: "kn_xa", actor_ref: actor, display: persona.displayName, department_code: WS_DEPARTMENT, province_code: prov, communes, level: "commune", approver: false });
  }
  return fail(403, "forbidden", "No extension role");
}

// ---------------------------------------------------------------------------
// Sổ lịch
// ---------------------------------------------------------------------------
type StoredItem = WsItemInput & { id: string };

interface Version {
  version: number;
  action: "draft_saved" | "submitted" | "amended";
  at: string;
  actor_ref: string;
  actor_display: string;
  items: StoredItem[];
}

interface Rec {
  id: string;
  owner_ref: string;
  owner_display: string;
  owner_level: "commune" | "province";
  department_code: string;
  province_code: string;
  commune_codes: string[];
  week_start: string;
  versions: Version[];
  status: WsStatus;
  approved_version: number | null;
  late: boolean;
  first_submitted_at: string | null;
  submitted_at: string | null;
  reviewed_at: string | null;
  reviewer_ref: string | null;
  reviewer_display: string | null;
  last_reason: string | null;
  created_at: string;
  updated_at: string;
  updated_by: string;
  locked_logged: boolean;
}

interface LogRow extends KnLogEntry {
  schedule_id: string;
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function cleanStr(v: unknown): string | null {
  if (typeof v !== "string") return null;
  const t = v.trim();
  return t ? t : null;
}

export class WorkScheduleStore {
  recs = new Map<string, Rec>();
  logs: LogRow[] = [];
  private seq = 0;
  /** Cán bộ đã từng đăng ký (ngoài danh bạ) — để tra tên hiển thị. */
  private directory = new Map<string, string>();

  constructor(readonly now: () => number = () => Date.now()) {
    for (const o of WS_ROSTER) this.directory.set(o.actor_ref, o.display);
  }

  private nowIso(): string {
    return new Date(this.now()).toISOString();
  }

  settings(provinceCode: string | null): WsSettings {
    return {
      province_code: provinceCode ?? WS_PROVINCE,
      submit_due_offset_hours: DEFAULT_SUBMIT_DUE_OFFSET_HOURS,
      review_due_offset_hours: DEFAULT_REVIEW_DUE_OFFSET_HOURS,
      remind_before_hours: 24,
      approver_levels: ["truong_phong"],
      province_approval: "peer",
      provisional: true,
    };
  }

  // --- tiện ích ----------------------------------------------------------
  private current(r: Rec): Version {
    return r.versions[r.versions.length - 1]!;
  }

  private versionOf(r: Rec, v: number | null): Version | undefined {
    return v === null ? undefined : r.versions.find((x) => x.version === v);
  }

  private find(owner: string, dept: string, week: string): Rec | undefined {
    for (const r of this.recs.values()) if (r.owner_ref === owner && r.department_code === dept && r.week_start === week) return r;
    return undefined;
  }

  private log(r: Rec, event: string, actor: { ref: string; display: string }, at: string, changes: KnLogEntry["changes"] = [], reason: string | null = null): void {
    this.seq += 1;
    this.logs.push({ schedule_id: r.id, seq: this.seq, event, actor_ref: actor.ref, actor_display: actor.display, at, recorded_at: at, changes, reason });
  }

  /** Tuần đã kết thúc: ghi sự kiện "locked" một lần (mô phỏng việc khóa định kỳ của backend). */
  private touch(r: Rec): void {
    if (r.locked_logged || !isWeekLocked(r.week_start, this.now())) return;
    r.locked_logged = true;
    const at = new Date(vnMidnight(addDays(r.week_start, 7))).toISOString();
    this.log(r, "locked", { ref: "00000000-0000-7000-8000-000000000000", display: "Hệ thống" }, at, [{ field: "Khóa", old: "Mở", new: "Đã khóa" }]);
  }

  private visible(c: WsCaller, r: Rec): boolean {
    if (c.role === "admin") return true;
    if (c.role === "kn_xa") return r.owner_ref === c.actor_ref;
    return r.owner_ref === c.actor_ref || (r.department_code === c.department_code && r.province_code === c.province_code);
  }

  private canReview(c: WsCaller, r: Rec): boolean {
    return (
      c.role === "kn_tinh" &&
      c.approver &&
      r.owner_ref !== c.actor_ref &&
      r.department_code === c.department_code &&
      r.province_code === c.province_code &&
      r.status === "pending" &&
      !isWeekLocked(r.week_start, this.now())
    );
  }

  private decorate(items: readonly StoredItem[], approved: readonly StoredItem[] | null): WsItem[] {
    const conflicts = conflictIndexes(items);
    const diff = approved ? diffItems(approved, items) : null;
    const changed = new Set<string>([...(diff?.added ?? []), ...(diff?.modified.map((m) => m.after) ?? [])].map((x) => x.id));
    return items.map((it, i) => ({ ...it, changed: changed.has(it.id), conflict: conflicts.has(i) }));
  }

  private dto(r: Rec): WsSchedule {
    this.touch(r);
    const cur = this.current(r);
    const due = submitDueAt(r.week_start);
    const reviewDue = reviewDueAt(r.week_start);
    return {
      id: r.id,
      owner_ref: r.owner_ref,
      owner_display: this.directory.get(r.owner_ref) ?? r.owner_display,
      owner_level: r.owner_level,
      department_code: r.department_code,
      province_code: r.province_code,
      commune_codes: [...r.commune_codes],
      week_start: r.week_start,
      week_end: weekEnd(r.week_start),
      status: r.status,
      version: cur.version,
      approved_version: r.approved_version,
      amendment: r.approved_version !== null && r.status !== "approved",
      late: r.late,
      locked: isWeekLocked(r.week_start, this.now()),
      submit_due_at: due,
      review_due_at: reviewDue,
      review_overdue: r.status === "pending" && this.now() > Date.parse(reviewDue),
      submitted_at: r.submitted_at,
      reviewed_at: r.reviewed_at,
      reviewer_ref: r.reviewer_ref,
      reviewer_display: r.reviewer_display,
      last_reason: r.last_reason,
      item_count: cur.items.length,
      conflict_count: conflictIndexes(cur.items).size,
      created_at: r.created_at,
      updated_at: r.updated_at,
      updated_by: r.updated_by,
    };
  }

  private approvedItems(r: Rec, upTo: Version): StoredItem[] | null {
    if (r.approved_version === null || r.approved_version >= upTo.version) return null;
    return this.versionOf(r, r.approved_version)?.items ?? null;
  }

  private listItem(r: Rec): WsListItem {
    const cur = this.current(r);
    return { ...this.dto(r), items: this.decorate(cur.items, this.approvedItems(r, cur)) };
  }

  detail(c: WsCaller, r: Rec, version?: number): WsDetail {
    const schedule = this.dto(r);
    const view = version ? (this.versionOf(r, version) ?? this.current(r)) : this.current(r);
    const approved = this.approvedItems(r, view);
    const items = this.decorate(view.items, approved);
    const warnings: WsWarning[] = [];
    const conflictIds = items.filter((x) => x.conflict).map((x) => x.id!);
    if (conflictIds.length) warnings.push({ code: "time_overlap", message: "Có mục công tác trùng thời gian.", item_ids: conflictIds });
    if (r.late) warnings.push({ code: "late_registration", message: "Đăng ký muộn (gửi sau hạn đăng ký).", item_ids: [] });
    const own = r.owner_ref === c.actor_ref && c.role !== "admin";
    const editable = own && !schedule.locked;
    return {
      schedule,
      viewing_version: view.version,
      items,
      approved_items: approved ? this.decorate(approved, null) : [],
      warnings,
      versions: r.versions.map((v) => ({ version: v.version, action: v.action, at: v.at, actor_ref: v.actor_ref, actor_display: v.actor_display, item_count: v.items.length })),
      can: {
        edit: editable,
        submit: editable && this.current(r).items.length > 0,
        approve: this.canReview(c, r),
        request_changes: this.canReview(c, r),
      },
    };
  }

  private getVisible(c: WsCaller, id: string): Rec | Fail {
    const r = this.recs.get(id);
    if (!r || !this.visible(c, r)) return fail(404, "extension.schedule_not_found", "Work schedule not found");
    return r;
  }

  // --- đọc ---------------------------------------------------------------
  mine(c: WsCaller, weekStart: string | null): Result<WsMine> {
    const week = weekStart ?? nextWeekStart(this.now());
    if (!isMonday(week)) return invalid([{ field: "week_start", reason: "Phải là Thứ Hai đầu tuần" }]);
    const r = this.find(c.actor_ref, c.department_code, week);
    const prev = addDays(week, -7);
    const prevRec = this.find(c.actor_ref, c.department_code, prev);
    const due = submitDueAt(week);
    return ok({
      week_start: week,
      week_end: weekEnd(week),
      submit_due_at: due,
      review_due_at: reviewDueAt(week),
      locked: isWeekLocked(week, this.now()),
      late_now: this.now() > Date.parse(due),
      schedule: r ? this.detail(c, r) : null,
      previous_week_start: prev,
      previous_available: !!prevRec && this.current(prevRec).items.length > 0,
      owner_level: c.level,
      commune_codes: c.level === "commune" ? [...c.communes] : [],
    });
  }

  get(c: WsCaller, id: string, version?: number): Result<WsDetail> {
    const r = this.getVisible(c, id);
    if ("ok" in r) return r;
    if (version !== undefined && !this.versionOf(r, version)) return fail(404, "extension.schedule_version_not_found", "Version not found");
    return ok(this.detail(c, r, version));
  }

  logPage(c: WsCaller, id: string, cursor: number | null, limit: number): Result<{ items: KnLogEntry[]; next_cursor: number | null }> {
    const r = this.getVisible(c, id);
    if ("ok" in r) return r;
    this.touch(r);
    const all = this.logs
      .filter((l) => l.schedule_id === id && (cursor === null || l.seq < cursor))
      .sort((a, b) => b.seq - a.seq);
    const page = all.slice(0, limit);
    return ok({
      items: page.map((l): KnLogEntry => ({ seq: l.seq, event: l.event, actor_ref: l.actor_ref, actor_display: l.actor_display, at: l.at, recorded_at: l.recorded_at, changes: l.changes, reason: l.reason })),
      next_cursor: all.length > limit ? (page[page.length - 1]?.seq ?? null) : null,
    });
  }

  private inWeekVisible(c: WsCaller, week: string): Rec[] {
    return [...this.recs.values()].filter((r) => r.week_start === week && this.visible(c, r));
  }

  list(c: WsCaller, q: { week_start: string | null; status: string | null; commune_code: string | null; owner_level: string | null }): Result<{ week_start: string; items: WsListItem[] }> {
    const week = q.week_start ?? nextWeekStart(this.now());
    if (!isMonday(week)) return invalid([{ field: "week_start", reason: "Phải là Thứ Hai đầu tuần" }]);
    if (q.status && !(q.status in STATUS_LABEL)) return invalid([{ field: "status", reason: "Trạng thái không hợp lệ" }]);
    const items = this.inWeekVisible(c, week)
      .filter((r) => (!q.status || r.status === q.status) && (!q.commune_code || r.commune_codes.includes(q.commune_code)) && (!q.owner_level || r.owner_level === q.owner_level))
      .sort((a, b) => (a.owner_level === b.owner_level ? (this.directory.get(a.owner_ref) ?? "").localeCompare(this.directory.get(b.owner_ref) ?? "", "vi") : a.owner_level === "commune" ? -1 : 1))
      .map((r) => this.listItem(r));
    return ok({ week_start: week, items });
  }

  unregistered(c: WsCaller, weekStart: string | null, communeCode: string | null): Result<{ week_start: string; items: WsUnregistered[] }> {
    if (c.role === "kn_xa") return fail(403, "forbidden", "Commune officers cannot list other officers");
    const week = weekStart ?? nextWeekStart(this.now());
    if (!isMonday(week)) return invalid([{ field: "week_start", reason: "Phải là Thứ Hai đầu tuần" }]);
    const items = WS_ROSTER.filter(
      (o) => (c.role === "admin" || (o.province_code === c.province_code && o.department_code === c.department_code)) && (!communeCode || o.communes.includes(communeCode)),
    )
      .map((o): WsUnregistered | null => {
        const r = this.find(o.actor_ref, o.department_code, week);
        if (r && (r.status !== "draft" || r.first_submitted_at)) return null;
        return {
          actor_ref: o.actor_ref,
          actor_display: o.display,
          owner_level: o.level,
          province_code: o.province_code,
          commune_codes: [...o.communes],
          status: r ? "draft" : "none",
          schedule_id: r?.id ?? null,
        };
      })
      .filter((x): x is WsUnregistered => x !== null);
    return ok({ week_start: week, items });
  }

  counts(c: WsCaller, weekStart: string | null): Result<WsCounts> {
    const week = weekStart ?? nextWeekStart(this.now());
    if (!isMonday(week)) return invalid([{ field: "week_start", reason: "Phải là Thứ Hai đầu tuần" }]);
    const recs = this.inWeekVisible(c, week);
    const by = (s: WsStatus) => recs.filter((r) => r.status === s).length;
    const unreg = c.role === "kn_xa" ? 0 : (this.unregistered(c, week, null) as Ok<{ items: WsUnregistered[] }>).body.items.length;
    return ok({
      week_start: week,
      draft: by("draft"),
      pending: by("pending"),
      approved: by("approved"),
      changes_requested: by("changes_requested"),
      unregistered: unreg,
      pending_open_weeks: this.pendingBadge(c),
    });
  }

  /** Số đếm menu extension.work_schedules.pending: lịch chờ duyệt mọi tuần chưa khóa mà người gọi duyệt được. */
  pendingBadge(c: WsCaller): number {
    return [...this.recs.values()].filter((r) => this.canReview(c, r)).length;
  }

  // --- ghi ---------------------------------------------------------------
  private validateItems(c: WsCaller, week: string, raw: unknown): StoredItem[] | Fail {
    if (!Array.isArray(raw)) return invalid([{ field: "items", reason: "Bắt buộc" }]);
    if (raw.length > 100) return invalid([{ field: "items", reason: "Tối đa 100 mục" }]);
    const errors: { field: string; reason: string }[] = [];
    const ids = new Set<string>();
    const items = raw.map((x: Record<string, unknown>, i): StoredItem => {
      const f = (name: string) => `items[${i}].${name}`;
      const day = typeof x?.day === "string" ? x.day : "";
      if (!isIsoDate(day)) errors.push({ field: f("day"), reason: "Ngày không hợp lệ" });
      else if (!inWeek(week, day)) errors.push({ field: f("day"), reason: "Ngày phải thuộc tuần đã chọn" });
      const session = x?.session === "morning" || x?.session === "afternoon" ? x.session : null;
      if (!session) errors.push({ field: f("session"), reason: "Chọn buổi Sáng hoặc Chiều" });
      const content = cleanStr(x?.content) ?? "";
      if (content.length < 3 || content.length > 500) errors.push({ field: f("content"), reason: "Nội dung 3–500 ký tự" });
      const start_time = cleanStr(x?.start_time);
      const end_time = cleanStr(x?.end_time);
      const te = timeError({ start_time, end_time });
      if (te) errors.push({ field: f("start_time"), reason: te });
      const commune_code = cleanStr(x?.commune_code);
      if (commune_code && c.level === "commune" && !c.communes.includes(commune_code)) errors.push({ field: f("commune_code"), reason: "Xã không thuộc phạm vi phụ trách" });
      const cooperative_ref = cleanStr(x?.cooperative_ref);
      if (cooperative_ref && !UUID_RE.test(cooperative_ref)) errors.push({ field: f("cooperative_ref"), reason: "Mã HTX không hợp lệ" });
      // Log bug TT-05/CN-04 (như máy chủ): cán bộ xã bắt buộc Xã/phường; Địa điểm bắt buộc (chữ hoặc vị trí).
      if (!commune_code && c.level === "commune") errors.push({ field: f("commune_code"), reason: "Xã/phường bắt buộc" });
      const location = cleanStr(x?.location);
      const hasPlace = typeof x?.place === "object" && x.place !== null;
      if (!location && !hasPlace) errors.push({ field: f("location"), reason: "Địa điểm bắt buộc (nhập địa điểm hoặc chọn vị trí)" });
      if (location && location.length > 200) errors.push({ field: f("location"), reason: "Tối đa 200 ký tự" });
      const participants = cleanStr(x?.participants);
      if (participants && participants.length > 500) errors.push({ field: f("participants"), reason: "Tối đa 500 ký tự" });
      if (looksLikePersonalNumber(participants)) errors.push({ field: f("participants"), reason: "Không ghi số điện thoại, số CCCD (RB-3)" });
      const note = cleanStr(x?.note);
      if (note && note.length > 1000) errors.push({ field: f("note"), reason: "Tối đa 1000 ký tự" });
      if (looksLikePersonalNumber(note)) errors.push({ field: f("note"), reason: "Không ghi số điện thoại, số CCCD (RB-3)" });
      let id = typeof x?.id === "string" && UUID_RE.test(x.id) ? x.id : uuidv7();
      if (ids.has(id)) id = uuidv7();
      ids.add(id);
      return { id, day, session: session ?? "morning", start_time, end_time, content, commune_code, cooperative_ref, location, participants, note };
    });
    return errors.length ? invalid(errors) : items;
  }

  private itemChanges(before: readonly StoredItem[], after: readonly StoredItem[]): KnLogEntry["changes"] {
    const d = diffItems(before, after);
    const label = (it: StoredItem) => `${dayColumnLabel(it.day)} ${SESSION_LABEL[it.session]}`;
    const sum = (it: StoredItem) => itemSummary(it, dayColumnLabel);
    const list: KnLogEntry["changes"] = [
      ...d.added.map((it) => ({ field: "Thêm mục", old: null, new: sum(it) })),
      ...d.removed.map((it) => ({ field: "Bỏ mục", old: sum(it), new: null })),
      ...d.modified.flatMap((m) =>
        m.fields.map((fld) => ({
          field: `Mục ${label(m.after)} · ${ITEM_FIELD_LABEL[fld]}`,
          old: fld === "session" ? SESSION_LABEL[m.before.session] : ((m.before[fld] as string | null | undefined) ?? null),
          new: fld === "session" ? SESSION_LABEL[m.after.session] : ((m.after[fld] as string | null | undefined) ?? null),
        })),
      ),
    ];
    // Tên trường không trùng trong một dòng Log ("Thêm mục", "Thêm mục (2)"…) — khung Log dùng làm khóa.
    const seen = new Map<string, number>();
    return list.map((c) => {
      const n = (seen.get(c.field) ?? 0) + 1;
      seen.set(c.field, n);
      return n > 1 ? { ...c, field: `${c.field} (${n})` } : c;
    });
  }

  private statusChange(from: WsStatus | null, to: WsStatus): KnLogEntry["changes"] {
    return from === to ? [] : [{ field: "Trạng thái", old: from ? STATUS_LABEL[from] : null, new: STATUS_LABEL[to] }];
  }

  private newRec(c: WsCaller, week: string, id: unknown): Rec {
    const at = this.nowIso();
    const rid = typeof id === "string" && UUID_RE.test(id) && !this.recs.has(id) ? id : uuidv7();
    this.directory.set(c.actor_ref, c.display);
    const r: Rec = {
      id: rid,
      owner_ref: c.actor_ref,
      owner_display: c.display,
      owner_level: c.level,
      department_code: c.department_code,
      province_code: c.province_code ?? WS_PROVINCE,
      commune_codes: [...c.communes],
      week_start: week,
      versions: [],
      status: "draft",
      approved_version: null,
      late: false,
      first_submitted_at: null,
      submitted_at: null,
      reviewed_at: null,
      reviewer_ref: null,
      reviewer_display: null,
      last_reason: null,
      created_at: at,
      updated_at: at,
      updated_by: c.actor_ref,
      locked_logged: false,
    };
    this.recs.set(r.id, r);
    return r;
  }

  private pushVersion(c: WsCaller, r: Rec, action: "draft" | "submit", items: StoredItem[], event?: string, extraChanges: KnLogEntry["changes"] = []): void {
    const at = this.nowIso();
    const prev = r.versions.length ? this.current(r) : null;
    const prevStatus = prev ? r.status : null;
    const vAction: Version["action"] = action === "draft" ? "draft_saved" : r.approved_version !== null ? "amended" : "submitted";
    r.versions.push({ version: (prev?.version ?? 0) + 1, action: vAction, at, actor_ref: c.actor_ref, actor_display: c.display, items });
    if (action === "submit") {
      r.status = "pending";
      r.submitted_at = at;
      if (!r.first_submitted_at) {
        r.first_submitted_at = at;
        r.late = this.now() > Date.parse(submitDueAt(r.week_start));
      }
    } else r.status = "draft";
    r.commune_codes = [...c.communes];
    r.updated_at = at;
    r.updated_by = c.actor_ref;
    this.log(r, event ?? vAction, { ref: c.actor_ref, display: c.display }, at, [...extraChanges, ...this.statusChange(prevStatus, r.status), ...this.itemChanges(prev?.items ?? [], items)]);
  }

  save(c: WsCaller, body: unknown): Result<WsDetail> {
    if (c.role === "admin") return fail(403, "forbidden", "Admin is read-only for work schedules");
    const b = (body ?? {}) as Record<string, unknown>;
    const week = typeof b.week_start === "string" ? b.week_start : "";
    if (!isMonday(week)) return invalid([{ field: "week_start", reason: "Phải là Thứ Hai đầu tuần" }]);
    const action = b.action === "draft" || b.action === "submit" ? b.action : null;
    if (!action) return invalid([{ field: "action", reason: "draft hoặc submit" }]);
    if (isWeekLocked(week, this.now())) return fail(409, "extension.week_locked", "Week has ended");
    const items = this.validateItems(c, week, b.items);
    if ("ok" in items) return items;
    let r = this.find(c.actor_ref, c.department_code, week);
    const base = typeof b.base_version === "number" ? b.base_version : null;
    if (r ? base !== this.current(r).version : base !== null) {
      return fail(409, "extension.schedule_version_conflict", "Base version differs from current version", { current_version: r ? this.current(r).version : null });
    }
    if (action === "draft" && r && (r.status === "pending" || r.status === "approved")) {
      return fail(409, "extension.schedule_state_conflict", "Draft not allowed after submission — submit an amendment instead");
    }
    if (action === "submit" && items.length === 0) return invalid([{ field: "items", reason: "Cần ít nhất một mục công tác để gửi duyệt" }]);
    r ??= this.newRec(c, week, b.id);
    this.pushVersion(c, r, action, items);
    return ok(this.detail(c, r), 201);
  }

  copyPrevious(c: WsCaller, body: unknown): Result<WsDetail> {
    if (c.role === "admin") return fail(403, "forbidden", "Admin is read-only for work schedules");
    const week = typeof (body as Record<string, unknown> | undefined)?.week_start === "string" ? ((body as Record<string, unknown>).week_start as string) : "";
    if (!isMonday(week)) return invalid([{ field: "week_start", reason: "Phải là Thứ Hai đầu tuần" }]);
    if (isWeekLocked(week, this.now())) return fail(409, "extension.week_locked", "Week has ended");
    const prevWeek = addDays(week, -7);
    const prev = this.find(c.actor_ref, c.department_code, prevWeek);
    // Bản sao lấy bản đã duyệt nếu có, không thì phiên bản hiện hành.
    const src = prev ? (this.versionOf(prev, prev.approved_version) ?? this.current(prev)) : undefined;
    if (!prev || !src || src.items.length === 0) return fail(404, "extension.schedule_not_found", "No schedule in previous week");
    let r = this.find(c.actor_ref, c.department_code, week);
    if (r && r.status !== "draft" && r.status !== "changes_requested") return fail(409, "extension.schedule_state_conflict", "Schedule already submitted");
    const items: StoredItem[] = src.items
      .filter((it) => c.level !== "commune" || !it.commune_code || c.communes.includes(it.commune_code))
      .map((it) => ({ ...it, id: uuidv7(), day: addDays(it.day, 7) }));
    r ??= this.newRec(c, week, undefined);
    this.pushVersion(c, r, "draft", items, "draft_saved", [{ field: "Nguồn", old: null, new: `Sao chép tuần ${weekRangeLabel(prevWeek)}` }]);
    return ok(this.detail(c, r), 201);
  }

  private reviewGuard(c: WsCaller, r: Rec, version: unknown): Fail | null {
    if (c.role !== "kn_tinh") return fail(403, "forbidden", "Only province level may review work schedules");
    if (r.owner_ref === c.actor_ref) return fail(403, "self_approval_forbidden", "Approver cannot review own schedule");
    if (!c.approver) return fail(403, "extension.not_schedule_approver", "Management level not allowed to approve (Q-05)");
    if (r.department_code !== c.department_code || r.province_code !== c.province_code) return fail(404, "extension.schedule_not_found", "Work schedule not found");
    if (isWeekLocked(r.week_start, this.now())) return fail(409, "extension.week_locked", "Week has ended");
    if (r.status !== "pending") return fail(409, "extension.schedule_state_conflict", "Schedule is not pending");
    if (typeof version !== "number" || version < 1) return invalid([{ field: "version", reason: "Bắt buộc" }]);
    if (version !== this.current(r).version) return fail(409, "extension.schedule_version_conflict", "Schedule changed since viewed", { current_version: this.current(r).version });
    return null;
  }

  approve(c: WsCaller, id: string, body: unknown): Result<WsDetail> {
    const r = this.getVisible(c, id);
    if ("ok" in r) return r;
    const b = (body ?? {}) as Record<string, unknown>;
    const g = this.reviewGuard(c, r, b.version);
    if (g) return g;
    const note = cleanStr(b.note);
    if (note && note.length > 1000) return invalid([{ field: "note", reason: "Tối đa 1000 ký tự" }]);
    const at = this.nowIso();
    const wasAmendment = r.approved_version !== null;
    const changes: KnLogEntry["changes"] = [...this.statusChange(r.status, "approved")];
    if (wasAmendment) changes.push({ field: "Phiên bản hiệu lực", old: `v${r.approved_version}`, new: `v${this.current(r).version}` });
    if (note) changes.push({ field: "Ghi chú duyệt", old: null, new: note });
    r.status = "approved";
    r.approved_version = this.current(r).version;
    r.reviewed_at = at;
    r.reviewer_ref = c.actor_ref;
    r.reviewer_display = c.display;
    r.last_reason = null;
    r.updated_at = at;
    r.updated_by = c.actor_ref;
    this.log(r, "approved", { ref: c.actor_ref, display: c.display }, at, changes);
    return ok(this.detail(c, r));
  }

  requestChanges(c: WsCaller, id: string, body: unknown): Result<WsDetail> {
    const r = this.getVisible(c, id);
    if ("ok" in r) return r;
    const b = (body ?? {}) as Record<string, unknown>;
    const reason = typeof b.reason === "string" ? b.reason.trim() : "";
    const g = this.reviewGuard(c, r, b.version);
    if (g) return g;
    if (reason.length < 10 || reason.length > 1000) return invalid([{ field: "reason", reason: "Lý do bắt buộc, từ 10 đến 1000 ký tự" }]);
    const at = this.nowIso();
    const changes = this.statusChange(r.status, "changes_requested");
    r.status = "changes_requested";
    r.reviewed_at = at;
    r.reviewer_ref = c.actor_ref;
    r.reviewer_display = c.display;
    r.last_reason = reason;
    r.updated_at = at;
    r.updated_by = c.actor_ref;
    this.log(r, "change_requested", { ref: c.actor_ref, display: c.display }, at, changes, reason);
    return ok(this.detail(c, r));
  }

  bulkApprove(c: WsCaller, body: unknown): Result<{ approved: string[]; skipped: { id: string; code: string }[] }> {
    if (c.role !== "kn_tinh") return fail(403, "forbidden", "Only province level may review work schedules");
    const ids = (body as Record<string, unknown> | undefined)?.ids;
    if (!Array.isArray(ids) || ids.length < 1 || ids.length > 200 || ids.some((x) => typeof x !== "string")) return invalid([{ field: "ids", reason: "1–200 mã lịch" }]);
    const approved: string[] = [];
    const skipped: { id: string; code: string }[] = [];
    for (const id of new Set(ids as string[])) {
      const r = this.recs.get(id);
      const res = r && this.visible(c, r) ? this.approve(c, id, { version: this.current(r).version }) : fail(404, "extension.schedule_not_found", "");
      if (res.ok) approved.push(id);
      else skipped.push({ id, code: res.code });
    }
    return ok({ approved, skipped });
  }

  /** Dữ liệu xuất (CSV/PDF giả lập) cho lưới tuần. */
  exportRows(c: WsCaller, q: { week_start: string | null; status: string | null; commune_code: string | null }): Result<{ week: string; rows: string[][] }> {
    const l = this.list(c, { ...q, owner_level: null });
    if (!l.ok) return l;
    const rows = l.body.items.flatMap((s) =>
      s.items.map((it) => [
        s.owner_display ?? "",
        s.commune_codes.map(communeName).join(", "),
        STATUS_LABEL[s.status],
        dayColumnLabel(it.day),
        SESSION_LABEL[it.session],
        [it.start_time, it.end_time].filter(Boolean).join("–"),
        it.content,
        [communeName(it.commune_code), KN_COOPS.find((x) => x.ref === it.cooperative_ref)?.name, it.location].filter(Boolean).join(" · "),
        it.participants ?? "",
        it.conflict ? "Trùng giờ" : "",
      ]),
    );
    return ok({ week: l.body.week_start, rows });
  }

  // --- dữ liệu mẫu --------------------------------------------------------
  /** Gieo một lịch với các bước (thời điểm tính lùi từ hiện tại, giờ). */
  seedSchedule(o: WsOfficer, week: string, items: Omit<StoredItem, "id">[], steps: ({ kind: "draft" | "submit"; hoursAgo: number } | { kind: "approve" | "changes"; hoursAgo: number; by: WsOfficer; reason?: string })[]): Rec {
    const caller: WsCaller = {
      role: o.level === "commune" ? "kn_xa" : "kn_tinh",
      actor_ref: o.actor_ref,
      display: o.display,
      department_code: o.department_code,
      province_code: o.province_code,
      communes: o.communes,
      level: o.level,
      approver: APPROVER_LEVELS.includes(o.mgmt_level),
    };
    const realNow = this.now;
    let r: Rec | undefined;
    const stored = items.map((it) => ({ ...it, id: uuidv7() }));
    for (const s of steps) {
      const at = realNow() - s.hoursAgo * 3_600_000;
      (this as { now: () => number }).now = () => at;
      try {
        if (s.kind === "draft" || s.kind === "submit") {
          r ??= this.newRec(caller, week, undefined);
          this.pushVersion(caller, r, s.kind, stored.map((x) => ({ ...x })));
        } else if (r && "by" in s) {
          const reviewer: WsCaller = { ...caller, role: "kn_tinh", actor_ref: s.by.actor_ref, display: s.by.display, level: "province", communes: [], approver: true };
          const iso = new Date(at).toISOString();
          if (s.kind === "approve") {
            this.log(r, "approved", { ref: reviewer.actor_ref, display: reviewer.display }, iso, this.statusChange(r.status, "approved"));
            r.status = "approved";
            r.approved_version = this.current(r).version;
            r.last_reason = null;
          } else {
            this.log(r, "change_requested", { ref: reviewer.actor_ref, display: reviewer.display }, iso, this.statusChange(r.status, "changes_requested"), s.reason ?? null);
            r.status = "changes_requested";
            r.last_reason = s.reason ?? null;
          }
          r.reviewed_at = iso;
          r.reviewer_ref = reviewer.actor_ref;
          r.reviewer_display = reviewer.display;
          r.updated_at = iso;
          r.updated_by = reviewer.actor_ref;
        }
      } finally {
        (this as { now: () => number }).now = realNow;
      }
    }
    return r!;
  }
}

const it = (day: string, session: "morning" | "afternoon", start: string | null, end: string | null, content: string, extra: Partial<WsItemInput> = {}): Omit<StoredItem, "id"> => ({
  day,
  session,
  start_time: start,
  end_time: end,
  content,
  commune_code: null,
  cooperative_ref: null,
  location: "Trụ sở UBND xã",
  participants: null,
  note: null,
  ...extra,
});

const roster = (key: string) => WS_ROSTER.find((o) => o.actor_ref === stableUuid(key === "an" ? "kn-an" : key === "binh" ? "kn-binh" : `ws-${key}`))!;

/** Dữ liệu mẫu: tuần trước (đã khóa), tuần này, tuần sau (mặc định màn đăng ký/duyệt) — theo Hình 12, 21. */
export function seedWorkSchedules(store: WorkScheduleStore): void {
  const now = store.now();
  const cur = currentWeekStart(now);
  const next = addDays(cur, 7);
  const prev = addDays(cur, -7);
  const d = (week: string, i: number) => addDays(week, i);
  const an = roster("an");
  const hoa = roster("hoa");
  const phuc = roster("phuc");
  const thu = roster("thu");
  const binh = roster("binh");
  const mai = roster("mai");
  const coop = (name: string) => KN_COOPS.find((c) => c.name === name)!.ref;
  const daysSinceMonday = (now - vnMidnight(cur)) / 3_600_000;
  const before = (h: number) => daysSinceMonday + h; // giờ trước 00:00 Thứ Hai tuần này

  // Tuần trước — đã khóa.
  store.seedSchedule(
    an,
    prev,
    [
      it(d(prev, 0), "morning", "07:30", "11:00", "Kiểm tra đồng ruộng sau sạ", { commune_code: "91301" }),
      it(d(prev, 2), "afternoon", "13:30", "16:30", "Họp tổ khuyến nông", { commune_code: "91302" }),
      it(d(prev, 4), "morning", null, null, "Nhập báo cáo tuần", { commune_code: "91301" }),
    ],
    [
      { kind: "submit", hoursAgo: before(80) },
      { kind: "approve", hoursAgo: before(60), by: binh },
    ],
  );
  store.seedSchedule(hoa, prev, [it(d(prev, 1), "morning", "08:00", "11:00", "Điều tra sâu bệnh", { commune_code: "91304" })], [
    { kind: "submit", hoursAgo: before(78) },
    { kind: "approve", hoursAgo: before(58), by: binh },
  ]);

  // Tuần này — đã duyệt (Hình 21: các mục của Nguyễn Văn An).
  store.seedSchedule(
    an,
    cur,
    [
      it(d(cur, 0), "morning", "07:30", "11:00", "Kiểm tra rầy nâu HTX Mỹ Hòa", { commune_code: "91301", cooperative_ref: coop("HTX Mỹ Hòa") }),
      it(d(cur, 1), "morning", "08:00", "10:30", "Họp tổ khuyến nông", { commune_code: "91301" }),
      it(d(cur, 1), "afternoon", "13:30", "16:30", "Điều tra sâu bệnh ấp 3", { commune_code: "91302", location: "Ấp 3" }),
      it(d(cur, 2), "afternoon", null, null, "Nhập báo cáo tuần", { commune_code: "91301" }),
      it(d(cur, 3), "morning", "07:30", "11:00", "Hướng dẫn 1P5G · Bình Đức", { commune_code: "91302", cooperative_ref: coop("HTX Bình Đức"), participants: "Cán bộ HTX, BVTV" }),
    ],
    [
      { kind: "draft", hoursAgo: before(60) },
      { kind: "submit", hoursAgo: before(58) },
      { kind: "approve", hoursAgo: before(40), by: binh },
    ],
  );
  store.seedSchedule(hoa, cur, [it(d(cur, 2), "morning", "07:30", "11:00", "Thăm đồng lúa Thu Đông", { commune_code: "91304" })], [
    { kind: "submit", hoursAgo: before(62) },
    { kind: "approve", hoursAgo: before(40), by: binh },
  ]);
  store.seedSchedule(phuc, cur, [it(d(cur, 0), "afternoon", "13:30", "16:00", "Tập huấn IPM", { commune_code: "91305" })], [
    { kind: "submit", hoursAgo: before(10) },
    { kind: "approve", hoursAgo: before(5), by: binh },
  ]);
  store.seedSchedule(mai, cur, [it(d(cur, 1), "morning", "08:00", "11:00", "Kiểm tra mô hình cánh đồng lớn", { commune_code: "91302" })], [
    { kind: "submit", hoursAgo: before(70) },
    { kind: "approve", hoursAgo: before(45), by: binh },
  ]);
  // Lịch của chính Trưởng phòng: chờ người duyệt khác (không tự duyệt, R-56).
  store.seedSchedule(binh, cur, [it(d(cur, 3), "morning", "08:00", "11:30", "Họp giao ban Phòng Trồng trọt")], [{ kind: "submit", hoursAgo: before(66) }]);

  // Tuần sau — Chờ duyệt (2): Lý Thị Hoa (trùng giờ T3), Lê Thị Mai; Đặng Thị Thu mới lưu nháp; Nguyễn Văn An, Huỳnh Văn Phúc chưa đăng ký.
  const recentHours = Math.min(6, Math.max(1, daysSinceMonday / 2));
  store.seedSchedule(
    hoa,
    next,
    [
      it(d(next, 1), "morning", "07:30", "10:00", "Thăm đồng", { commune_code: "91304", location: "Cánh đồng ấp Vĩnh Đông" }),
      it(d(next, 1), "morning", "09:00", "11:00", "Tập huấn", { commune_code: "91304", cooperative_ref: coop("HTX Vĩnh Tế"), participants: "Tổ khuyến nông cộng đồng" }),
      it(d(next, 4), "morning", "08:00", "11:00", "Điều tra sâu bệnh", { commune_code: "91304" }),
    ],
    [
      { kind: "draft", hoursAgo: recentHours + 2 },
      { kind: "submit", hoursAgo: recentHours },
    ],
  );
  store.seedSchedule(
    mai,
    next,
    [
      it(d(next, 0), "afternoon", "13:30", "16:30", "Kiểm tra tiến độ xuống giống", { commune_code: "91301" }),
      it(d(next, 3), "morning", "08:00", "11:00", "Làm việc với HTX Long Xuyên", { commune_code: "91303", cooperative_ref: coop("HTX Long Xuyên") }),
    ],
    [{ kind: "submit", hoursAgo: recentHours + 1 }],
  );
  store.seedSchedule(thu, next, [it(d(next, 2), "morning", null, null, "Thăm đồng Núi Sam", { commune_code: "91306" })], [{ kind: "draft", hoursAgo: recentHours }]);
}

// ---------------------------------------------------------------------------
// Kho dùng chung của chế độ giả lập + route
// ---------------------------------------------------------------------------
let STORE = new WorkScheduleStore();
seedWorkSchedules(STORE);

export function workScheduleStore(): WorkScheduleStore {
  return STORE;
}

/** Cho kiểm thử: dựng lại kho với đồng hồ cho trước. */
export function resetWorkSchedules(now?: () => number, seed = true): WorkScheduleStore {
  STORE = new WorkScheduleStore(now);
  if (seed) seedWorkSchedules(STORE);
  return STORE;
}

/** Số đếm menu cho người gọi (dùng ở GET /v1/me/menu-badges). */
export function workSchedulePendingBadge(persona: MockPersona, contextId: string | null): number {
  const c = callerOf(persona, contextId);
  return c.ok ? STORE.pendingBadge(c.body) : 0;
}

function respond<T>(r: Result<T>): Response {
  return r.ok ? json(r.body, r.status) : problem(r.status, r.code, r.title, r.extra ?? {});
}

function withCaller<T>(ctx: Ctx, fn: (c: WsCaller) => Result<T>): Response {
  const c = callerOf(ctx.persona, ctx.request.headers.get("X-AG-Context"));
  return c.ok ? respond(fn(c.body)) : respond(c);
}

const q = (url: URL, k: string) => url.searchParams.get(k) || null;

function pdfOf(lines: string[]): string {
  // PDF tối giản (ASCII): tiêu đề + số dòng — bản thật dựng khổ ngang có tiếng Việt.
  const ascii = (s: string) => s.normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/đ/g, "d").replace(/Đ/g, "D").replace(/[^\x20-\x7e]/g, "?").replace(/[()\\]/g, "");
  const text = lines.slice(0, 40).map((l, i) => `BT /F1 9 Tf 40 ${560 - i * 13} Td (${ascii(l)}) Tj ET`).join("\n");
  const objs = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 842 595] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
    `<< /Length ${text.length} >>\nstream\n${text}\nendstream`,
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
  ];
  let pdf = "%PDF-1.4\n";
  const offsets: number[] = [];
  objs.forEach((o, i) => {
    offsets.push(pdf.length);
    pdf += `${i + 1} 0 obj\n${o}\nendobj\n`;
  });
  const xref = pdf.length;
  pdf += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n${offsets.map((o) => `${String(o).padStart(10, "0")} 00000 n \n`).join("")}`;
  pdf += `trailer\n<< /Size ${objs.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF`;
  return pdf;
}

const HEADERS = ["Cán bộ", "Xã phụ trách", "Trạng thái", "Ngày", "Buổi", "Giờ", "Nội dung", "Địa điểm", "Thành phần phối hợp", "Ghi chú"];

export const workScheduleAppRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/work-schedules\/mine$/,
    handler: (ctx) => withCaller(ctx, (c) => STORE.mine(c, q(ctx.url, "week_start"))),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/work-schedules\/counts$/,
    handler: (ctx) => withCaller(ctx, (c) => STORE.counts(c, q(ctx.url, "week_start"))),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/work-schedules\/unregistered$/,
    handler: (ctx) => withCaller(ctx, (c) => STORE.unregistered(c, q(ctx.url, "week_start"), q(ctx.url, "commune_code"))),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/work-schedules\/settings$/,
    handler: (ctx) => withCaller(ctx, (c) => ok(STORE.settings(q(ctx.url, "province_code") ?? c.province_code))),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/work-schedules\/export$/,
    handler: (ctx) => {
      const c = callerOf(ctx.persona, ctx.request.headers.get("X-AG-Context"));
      if (!c.ok) return respond(c);
      const format = q(ctx.url, "format") ?? "xlsx";
      if (format !== "xlsx" && format !== "pdf") return problem(422, "validation_failed", "Validation failed", { errors: [{ field: "format", reason: "xlsx hoặc pdf" }] });
      const r = STORE.exportRows(c.body, { week_start: q(ctx.url, "week_start"), status: q(ctx.url, "status"), commune_code: q(ctx.url, "commune_code") });
      if (!r.ok) return respond(r);
      const title = `Lịch công tác tuần ${weekRangeLabel(r.body.week)} (giả lập)`;
      if (format === "pdf") {
        const name = `lich-cong-tac-${r.body.week}-gia-lap.pdf`;
        return new Response(pdfOf([title, HEADERS.join(" | "), ...r.body.rows.map((x) => x.join(" | "))]), {
          status: 200,
          headers: { "Content-Type": "application/pdf", "Content-Disposition": `attachment; filename="${name}"` },
        });
      }
      // Giả lập không đóng gói .xlsx (cần thư viện zip) — trả CSV UTF-8 có BOM, tên tệp ghi rõ.
      const cell = (s: string) => (/[",\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
      const csv = `﻿${cell(title)}\r\n${[HEADERS, ...r.body.rows].map((row) => row.map(cell).join(",")).join("\r\n")}\r\n`;
      const name = `lich-cong-tac-${r.body.week}-gia-lap.csv`;
      return new Response(csv, { status: 200, headers: { "Content-Type": "text/csv; charset=utf-8", "Content-Disposition": `attachment; filename="${name}"` } });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/work-schedules\/copy-previous$/,
    handler: (ctx) => withCaller(ctx, (c) => STORE.copyPrevious(c, ctx.body)),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/work-schedules\/bulk-approve$/,
    handler: (ctx) => withCaller(ctx, (c) => STORE.bulkApprove(c, ctx.body)),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/work-schedules$/,
    handler: (ctx) => withCaller(ctx, (c) => STORE.save(c, ctx.body)),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/work-schedules$/,
    handler: (ctx) =>
      withCaller(ctx, (c) => {
        const ownerLevel = q(ctx.url, "owner_level");
        const r = STORE.list(c, { week_start: q(ctx.url, "week_start"), status: q(ctx.url, "status"), commune_code: q(ctx.url, "commune_code"), owner_level: ownerLevel });
        if (!r.ok) return r;
        const limit = Math.min(Math.max(Number(q(ctx.url, "limit") ?? 50) || 50, 1), 200);
        const cursor = q(ctx.url, "cursor");
        const offset = cursor ? Number.parseInt(atob(cursor), 10) || 0 : 0;
        const slice = r.body.items.slice(offset, offset + limit);
        return ok({ week_start: r.body.week_start, items: slice, next_cursor: offset + limit < r.body.items.length ? btoa(String(offset + limit)) : null });
      }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/work-schedules\/([0-9a-f-]{36})\/log$/,
    handler: (ctx) =>
      withCaller(ctx, (c) => {
        const cursor = q(ctx.url, "cursor");
        const limit = Math.min(Math.max(Number(q(ctx.url, "limit") ?? 50) || 50, 1), 200);
        return STORE.logPage(c, ctx.params[0]!, cursor ? Number(cursor) : null, limit);
      }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/work-schedules\/([0-9a-f-]{36})$/,
    handler: (ctx) =>
      withCaller(ctx, (c) => {
        const v = q(ctx.url, "version");
        return STORE.get(c, ctx.params[0]!, v ? Number(v) : undefined);
      }),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/work-schedules\/([0-9a-f-]{36})\/approve$/,
    handler: (ctx) => withCaller(ctx, (c) => STORE.approve(c, ctx.params[0]!, ctx.body)),
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/work-schedules\/([0-9a-f-]{36})\/request-changes$/,
    handler: (ctx) => withCaller(ctx, (c) => STORE.requestChanges(c, ctx.params[0]!, ctx.body)),
  },
  // Danh bạ phụ trợ cho màn lịch: tên xã 913xx của tài khoản KN giả lập và HTX của các xã đó.
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/admin-units\/(913\d\d)$/,
    handler: ({ params }) => {
      const c = KN_COMMUNES.find((x) => x.code === params[0]);
      if (!c) return problem(404, "masterdata.admin_unit_not_found", "Admin unit not found");
      const unit: AdminUnit = { code: c.code, name: c.name, level: "commune", parent_code: WS_PROVINCE, valid_from: "2025-07-01", valid_to: null, successor_codes: [] };
      return json(unit);
    },
  },
  {
    // P6s (FN-XA-03.03): hồ sơ HTX của các xã KN giả lập.
    method: "GET",
    pattern: /^\/v1\/cooperatives\/([0-9a-f-]{36})$/,
    match: (url) => KN_COOPS.some((c) => url.pathname.endsWith(c.ref)),
    handler: ({ params }) => json(KN_COOPS.find((c) => c.ref === params[0])!),
  },
  {
    method: "GET",
    pattern: /^\/v1\/cooperatives$/,
    match: (url) => /^913\d\d$/.test(url.searchParams.get("admin_unit") ?? ""),
    handler: ({ url }) => json({ items: KN_COOPS.filter((c) => c.admin_unit_code === url.searchParams.get("admin_unit")), next_cursor: null }),
  },
];
