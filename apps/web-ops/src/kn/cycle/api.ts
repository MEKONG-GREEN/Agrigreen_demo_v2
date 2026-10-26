// Client vòng đời báo cáo KN v7.7 (P4 — application.yaml /v1/extension/report-*, kn-periods, consolidations,
// kn-dashboard, commune-reports; /v1/reports/dispatch*) và quản trị KN (admin.yaml /v1/accounts/kn-users,
// assignments, log, support-sessions, /v1/departments, /v1/regions, /v1/orgs/tree). Kiểu = kiểu sinh từ OpenAPI.
// fetch(Request) thuần (như src/kn/client.ts): Bearer, X-AG-Context, Idempotency-Key, và X-AG-Reason ở chế độ hỗ trợ
// (FN-AD-04.07: mọi thao tác ghi bắt buộc lý do ≥ 10 ký tự, mã hóa phần trăm).
import type { components as AppC } from "@/api/generated/application";
import type { components as AdmC } from "@/api/generated/admin";
import { beginAttempt, currentIdempotentRun, requestFingerprint, type IdempotencyAttempt } from "@/api/idempotency";
import { ApiProblem, networkProblem, toProblem } from "@/api/problem";
import { CONTEXT_HEADER, REASON_HEADER } from "../contract";

type A = AppC["schemas"];
type D = AdmC["schemas"];

export type RcBatch = A["RcBatch"];
export type RcPeriod = A["RcPeriod"];
export type RcTask = A["RcTask"];
export type RcTaskList = A["RcTaskList"];
export type RcTaskDetail = A["RcTaskDetail"];
export type RcSubmission = A["RcSubmission"];
export type RcSubmissionResult = A["RcSubmissionResult"];
export type RcLogEntry = A["RcLogEntry"];
export type RcReminder = A["RcReminder"];
export type RcRun = A["RcRun"];
export type RcPeriodView = A["RcPeriodView"];
export type RcReopenResult = A["RcReopenResult"];
export type RcConsolidationView = A["RcConsolidationView"];
export type RcConsolidateResult = A["RcConsolidateResult"];
export type RcChange = A["RcChange"];
export type RcPending = A["RcPending"];
export type RcDashboard = A["RcDashboard"];
export type RcCommuneStats = A["RcCommuneStats"];
export type RcBatchInput = A["RcBatchInput"];
export type RcCellIssue = A["RcCellIssue"];
export type DispRecipient = A["DispRecipient"];
export type DispView = A["DispView"];
export type Dispatch = A["Dispatch"];

/** Kết quả nạp Tỉnh giao từ tệp (POST /v1/extension/report-batches/provincial-import). */
export interface RcProvincialImport {
  provincial_source: "excel";
  provincial_values: Record<string, Record<string, Record<string, string>>>;
  columns: { header: string; template_id: string; form_code: string; letter: string }[];
  ignored_columns: string[];
  accepted: number;
  rejected: number;
  rows: { row: number; commune_code: string; status: "accepted" | "rejected"; reason: string | null }[];
}

/** RcPreview với các phần tử đã khai (Go handler/dto.go). */
export interface RcPreview {
  report_type: string;
  period_type: string;
  period_rule: string;
  /** D8: lịch tuần của phòng — mã phòng, phiên bản (1 v1, 2 Thứ Tư–Thứ Ba, 3 CNTS Thứ Ba–Thứ Hai), nguồn. */
  calendar_id: string;
  calendar_version: number;
  calendar_source: "department_param" | "builtin" | "global_param" | "legacy";
  deadline: { offset_days: number; time: string };
  close_after_days: number;
  forms: { template_id: string; version_id: string; code: string; name: string; version: number; period_block: boolean; slots: number }[];
  periods: {
    slot: number;
    label: string;
    range_label: string;
    from_date: string;
    to_date: string;
    due_at: string;
    auto_close_at: string;
    /** Khóa kỳ theo lịch kỳ hệ thống (W11.05b) — null với vụ/đột xuất. */
    period_key: string | null;
    iso_week: number | null;
    week_year: number | null;
  }[];
  communes: { commune_code: string; region_code: string; region_name: string; officer_count: number }[];
  communes_without_officer: string[];
  provincial_columns: { template_id: string; form_code: string; letter: string; title: string; unit: string }[];
  form_count: number;
  commune_count: number;
  period_count: number;
  task_count: number;
  warnings: string[];
}

export interface RcFormItem {
  template_id: string;
  version_id: string;
  code: string;
  name: string;
  version: number;
  report_types: string[];
  period_block: boolean;
  completeness: string;
  provincial_columns: { letter: string; title: string; unit: string }[];
}

export interface RcStatusItem {
  task_id: string;
  commune_code: string;
  commune_name?: string;
  region_code: string;
  region_name?: string;
  form_code: string;
  template_id: string;
  status: RcTask["status"];
  status_label: string;
  officers: { actor_ref: string; display: string }[];
  no_officer: boolean;
  submitted_at: string | null;
  last_reminded_at: string | null;
}
export interface RcStatusBoard {
  period: RcPeriod;
  counts: Record<string, number>;
  items: RcStatusItem[];
}

export interface SnapRow {
  key: string;
  commune: string;
  commune_name?: string;
  region?: string;
  region_name?: string;
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
export interface ChangedCell {
  row_key: string;
  letter: string;
  old: string | null;
  kind: RcChange["kind"];
  changed_by: string | null;
  changed_by_display: string;
  submitted_at: string | null;
}
export interface PendingItem {
  commune_code: string;
  form_code: string;
  task_id: string;
  submission_id: string;
  submitted_at: string | null;
  kind: "new" | "resubmit" | "adjusted";
}

export type KnAssignment = D["KNAssignment"];
export type KnAssignmentInput = D["KNAssignmentInput"];
export type KnAccountLog = D["KNAccountLogEntry"];
export type KnSupportLog = D["KNSupportLogEntry"];
export type KnSessionContext = D["KNSessionContext"];
export type AccountsUser = D["AccountsUser"];
export type KnDepartment = D["KNDepartment"];
export type KnRegion = D["KNRegion"];
export type KnOrgTreeNode = D["KNOrgTreeNode"];
export type KnLogEntry = D["KNLogEntry"];

// ---------------------------------------------------------------------------
// Lý do ở chế độ hỗ trợ (dải vàng) — giữ trong bộ nhớ như context_id
// ---------------------------------------------------------------------------
let supportReason: string | null = null;
export const supportReasonStore = {
  get: (): string | null => supportReason,
  set: (v: string | null): void => {
    supportReason = v;
  },
};

export interface KnHttpOptions {
  baseUrl: string;
  getAccessToken: () => string | null;
  getContextId: () => string | null;
  fetch?: (input: Request) => Promise<Response>;
}

type Q = Record<string, string | number | boolean | null | undefined | string[]>;

function qs(q?: Q): string {
  if (!q) return "";
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(q)) {
    if (v === undefined || v === null || v === "") continue;
    if (Array.isArray(v)) v.forEach((x) => p.append(k, x));
    else p.set(k, String(v));
  }
  const s = p.toString();
  return s ? `?${s}` : "";
}

/** Thân request là tệp nguyên văn (CSV/.xlsx) thay cho JSON — các đường nạp tệp (shared/tabular). */
export class RawFile {
  constructor(
    readonly data: Blob | ArrayBuffer | Uint8Array | string,
    readonly contentType: string,
  ) {}
}

/** Dấu vân tay thân yêu cầu (đồng bộ): JSON/chuỗi nguyên văn; tệp nhị phân theo độ dài + băm FNV-1a; Blob theo loại + cỡ. */
function bodyFingerprint(b: unknown): string {
  if (b === undefined) return "";
  if (typeof b === "string") return b;
  const bytes = b instanceof Uint8Array ? b : b instanceof ArrayBuffer ? new Uint8Array(b) : null;
  if (bytes) {
    let h = 2166136261;
    for (let i = 0; i < bytes.length; i++) h = Math.imul(h ^ (bytes[i] ?? 0), 16777619) >>> 0;
    return `bin:${bytes.length}:${h.toString(16)}`;
  }
  if (typeof Blob !== "undefined" && b instanceof Blob) return `blob:${b.type}:${b.size}`;
  return String(b);
}

export function knHttp(opts: KnHttpOptions) {
  const base = opts.baseUrl.replace(/\/+$/, "");
  const doFetch = opts.fetch ?? ((r: Request) => fetch(r));
  // idem: khóa tường minh của nơi gọi. Không truyền → khóa của phiên thao tác hiện hành (useAction/useIdempotentAction,
  // P0-WEB-IDEMKEY #129: bấm lại cùng dữ liệu sau lỗi mạng/5xx dùng lại khóa) → ngoài phiên mới sinh khóa mới.
  return async function call<T>(method: "GET" | "POST" | "PUT" | "PATCH", path: string, body?: unknown, query?: Q, idem?: string): Promise<T> {
    const op = currentIdempotentRun(); // đọc đồng bộ, trước mọi await
    const headers = new Headers({ Accept: "application/json, application/problem+json" });
    const token = opts.getAccessToken();
    if (token) headers.set("Authorization", `Bearer ${token}`);
    const ctx = opts.getContextId();
    if (ctx) headers.set(CONTEXT_HEADER, ctx);
    const url = `${base}${path}${qs(query)}`;
    const payload = method === "GET" ? undefined : body instanceof RawFile ? body.data : JSON.stringify(body ?? {});
    let attempt: IdempotencyAttempt | null = null;
    if (method !== "GET") {
      headers.set("Content-Type", body instanceof RawFile ? body.contentType : "application/json");
      attempt = beginAttempt(op, requestFingerprint(method, url, ctx, bodyFingerprint(payload)), idem);
      headers.set("Idempotency-Key", attempt.key);
      const r = supportReason?.trim();
      if (r) headers.set(REASON_HEADER, encodeURIComponent(r));
    }
    const req = new Request(url, { method, headers, body: payload as BodyInit | undefined });
    let res: Response;
    try {
      res = await doFetch(req);
    } catch {
      attempt?.settle("network");
      throw networkProblem();
    }
    const text = await res.text();
    let data: unknown;
    try {
      data = text ? JSON.parse(text) : undefined;
    } catch {
      data = undefined;
    }
    attempt?.settle({ status: res.status, code: (data as { code?: string } | undefined)?.code ?? null });
    if (!res.ok) throw new ApiProblem(toProblem(data, res));
    return data as T;
  };
}

export type CycleApi = ReturnType<typeof createCycleApi>;

export function createCycleApi(opts: KnHttpOptions) {
  const call = knHttp(opts);
  const X = "/v1/extension";
  return {
    reportForms: (report_type: string, from_date?: string) => call<{ items: RcFormItem[] }>("GET", `${X}/report-forms`, undefined, { report_type, from_date }),
    preview: (b: RcBatchInput) => call<RcPreview>("POST", `${X}/report-batches/preview`, b),
    /** UR-TI-02: đọc tệp Tỉnh giao (CSV UTF-8/.xlsx) — không ghi; kết quả đưa vào provincial_values khi phát hành. */
    provincialImport: (q: { report_type: string; from_date?: string; template_ids: string[] }, file: RawFile) =>
      call<RcProvincialImport>("POST", `${X}/report-batches/provincial-import`, file, { report_type: q.report_type, from_date: q.from_date, template_ids: q.template_ids.join(",") }),
    issue: (b: RcBatchInput, idem?: string) => call<{ batch: RcBatch; periods: RcPeriod[]; warnings: string[]; communes_without_officer: string[] }>("POST", `${X}/report-batches`, b, undefined, idem),
    batches: (q: { report_type?: string; province?: string } = {}) => call<{ items: RcBatch[]; next_cursor: string | null }>("GET", `${X}/report-batches`, undefined, q),
    batch: (id: string) => call<{ batch: RcBatch; periods: RcPeriod[] }>("GET", `${X}/report-batches/${id}`),
    batchLog: (id: string) => call<{ items: RcLogEntry[] }>("GET", `${X}/report-batches/${id}/log`),
    withdraw: (id: string, reason: string) => call<RcBatch>("POST", `${X}/report-batches/${id}/withdraw`, { reason }),
    deadline: (id: string, body: { due_at?: string; deadline?: { offset_days: number; time: string }; period_ids?: string[]; reason: string }) =>
      call<{ changes: Record<string, unknown>[] }>("POST", `${X}/report-batches/${id}/deadline`, body),
    provincialValues: (id: string, template_id?: string) => call<{ items: { template_id: string; commune_code: string; cells: Record<string, string> }[] }>("GET", `${X}/report-batches/${id}/provincial-values`, undefined, { template_id }),
    status: (id: string, q: { period_id?: string; template_id?: string; status?: string } = {}) => call<RcStatusBoard>("GET", `${X}/report-batches/${id}/status`, undefined, q),
    remind: (id: string, body: { period_id: string; template_id?: string | null; communes?: string[]; all_pending?: boolean; message: string }) =>
      call<{ reminder: RcReminder; skipped_no_officer: string[]; skipped_done: string[] }>("POST", `${X}/report-batches/${id}/reminders`, body),
    reminders: (id: string, period_id?: string) => call<{ items: RcReminder[] }>("GET", `${X}/report-batches/${id}/reminders`, undefined, { period_id }),
    tasks: (q: { commune?: string; period_type?: string; status?: string; form_code?: string; batch_id?: string; period_id?: string } = {}) => call<RcTaskList>("GET", `${X}/report-tasks`, undefined, q),
    communeReports: (q: { commune?: string; period_type?: string; status?: string; form_code?: string; batch_id?: string } = {}) => call<RcTaskList>("GET", `${X}/commune-reports`, undefined, q),
    communeStats: (commune: string, batch_id?: string) => call<RcCommuneStats>("GET", `${X}/commune-reports/stats`, undefined, { commune, batch_id }),
    task: (id: string) => call<RcTaskDetail>("GET", `${X}/report-tasks/${id}`),
    submit: (id: string, body: { id?: string; kind: "draft" | "submitted"; rows: { indicator?: string; input: Record<string, string> }[] }, idem?: string) =>
      call<RcSubmissionResult>("POST", `${X}/report-tasks/${id}/submissions`, body, undefined, idem),
    history: (id: string) => call<{ items: RcSubmission[] }>("GET", `${X}/report-tasks/${id}/history`),
    taskLog: (id: string) => call<{ items: RcLogEntry[] }>("GET", `${X}/report-tasks/${id}/log`),
    requestAdjustment: (id: string, reason: string) => call<{ task: RcTask }>("POST", `${X}/report-tasks/${id}/adjustment-request`, { reason }),
    confirm: (id: string, note?: string) => call<{ task: RcTask }>("POST", `${X}/report-tasks/${id}/confirm`, note ? { note } : {}),
    periods: (q: { report_type?: string; batch_id?: string; province?: string } = {}) => call<{ items: RcPeriod[] }>("GET", `${X}/kn-periods`, undefined, q),
    period: (id: string) => call<RcPeriodView>("GET", `${X}/report-periods/${id}`),
    periodLog: (id: string) => call<{ items: RcLogEntry[] }>("GET", `${X}/report-periods/${id}/log`),
    close: (id: string) => call<RcPeriodView>("POST", `${X}/report-periods/${id}/close`),
    reopen: (id: string, body: { communes?: string[]; template_ids?: string[]; reason: string; until: string }) => call<RcReopenResult>("POST", `${X}/report-periods/${id}/reopen`, body),
    consolidation: (q: { period_id: string; template_id?: string; form_code?: string; run_id?: string; region?: string; compare?: "previous_year" }) => call<RcConsolidationView>("GET", `${X}/consolidations`, undefined, q),
    consolidate: (period_id: string, template_ids?: string[]) => call<RcConsolidateResult>("POST", `${X}/consolidations`, { period_id, ...(template_ids ? { template_ids } : {}) }),
    pending: (period_id: string) => call<RcPending>("GET", `${X}/consolidations/pending-changes`, undefined, { period_id }),
    runs: (period_id: string, template_id?: string) => call<{ items: RcRun[] }>("GET", `${X}/consolidations/runs`, undefined, { period_id, template_id }),
    runChanges: (run: string) => call<{ run: RcRun; items: RcChange[] }>("GET", `${X}/consolidations/${run}/changes`, undefined, { limit: 1000 }),
    dashboard: (q: { province?: string; region?: string; period_id?: string; batch_id?: string } = {}) => call<RcDashboard>("GET", `${X}/kn-dashboard`, undefined, q),
    badges: () => call<{ counts: Record<string, number> }>("GET", `${X}/report-badges`),
    recipients: (province?: string) => call<{ items: DispRecipient[] }>("GET", "/v1/reports/dispatch-recipients", undefined, { province }),
    addRecipient: (body: { label: string; address: string; kind: "to" | "cc"; is_default?: boolean }) => call<DispRecipient>("POST", "/v1/reports/dispatch-recipients", body),
    exportDispatch: (body: { period_id: string; form_codes?: string[]; formats?: ("xlsx" | "pdf" | "gsheet")[] }) => call<DispView>("POST", "/v1/reports/dispatches", body),
    dispatches: (q: { period_id?: string; province?: string } = {}) => call<{ items: Dispatch[] }>("GET", "/v1/reports/dispatches", undefined, q),
    dispatch: (id: string) => call<DispView>("GET", `/v1/reports/dispatches/${id}`),
    send: (id: string, body: { recipient_ids?: string[]; cc_ids?: string[]; subject?: string; body?: string }) => call<DispView>("POST", `/v1/reports/dispatches/${id}/send`, body),
    download: (id: string, file: string) => call<{ url: string; expires_in_seconds: number }>("GET", `/v1/reports/dispatches/${id}/files/${file}/download`),
  };
}

export type KnAdminApi = ReturnType<typeof createKnAdminApi>;

// W11.07 — phân quyền trong vai + đơn vị nội bộ (admin.yaml KNAssignmentAttrs, KNDelegation, InternalUnit).
export type KnAssignmentAttrs = D["KNAssignmentAttrs"];
export type KnDelegation = D["KNDelegation"];
export type InternalUnit = D["InternalUnit"];
export type KnCatalogItem = D["KNCatalogItem"];
export interface KnAttrsInput {
  province_group?: string;
  duties?: string[];
  internal_unit_code?: string;
  facility_refs?: string[];
  household_refs?: string[];
  reason?: string;
}

/** Quản trị KN (Admin API, MFA). */
/** Kết quả POST /v1/accounts/kn-users/import. */
export interface KnUserImport {
  accounts_created: number;
  created: number;
  rejected: number;
  rows: { row: number; status: "created" | "rejected"; reason: string | null; user_id: string | null; activation_email_sent: boolean }[];
}

/** GET /v1/accounts/users/{id}/sessions. */
export interface KnUserSessions {
  available: boolean;
  active: { id: string; ip_address: string; started_at: string | null; last_access_at: string | null; clients: string[] }[];
  history: { at: string | null; type: string; ip_address: string; client: string; error: string | null }[];
}

export function createKnAdminApi(opts: KnHttpOptions) {
  const call = knHttp(opts);
  return {
    users: () => call<{ items: AccountsUser[]; next_cursor: string | null }>("GET", "/v1/accounts/users", undefined, { limit: 100 }),
    /** W11.02 UR-AD-04: nạp Excel tài khoản nhiều vai trò (mỗi dòng một lần gán, cùng Email = một tài khoản). */
    importKnUsers: (file: RawFile, idem?: string) => call<KnUserImport>("POST", "/v1/accounts/kn-users/import", file, undefined, idem),
    /** W11.02 FN-AD-04.04: tab Phiên đang hoạt động / Lịch sử đăng nhập (Keycloak Admin REST). */
    sessions: (id: string) => call<KnUserSessions>("GET", `/v1/accounts/users/${id}/sessions`, undefined, { limit: 50 }),
    user: (id: string) => call<AccountsUser>("GET", `/v1/accounts/users/${id}`),
    mgmtLevels: () =>
      call<{
        items: { code: string; label: string; manager?: boolean; reads_all_departments?: boolean }[];
        provisional: boolean;
        province_groups?: KnCatalogItem[];
        duties?: KnCatalogItem[];
        powers?: KnCatalogItem[];
        delegation_max_days?: number;
      }>("GET", "/v1/accounts/mgmt-levels"),
    // W11.07: phân quyền trong vai, ủy quyền có hạn, đơn vị nội bộ.
    setAttrs: (id: string, aid: string, body: KnAttrsInput) =>
      call<{ assignment_id: string; attributes: KnAssignmentAttrs; seq: number }>("PUT", `/v1/accounts/users/${id}/assignments/${aid}/attributes`, body),
    delegations: (id: string) => call<{ items: KnDelegation[] }>("GET", `/v1/accounts/users/${id}/delegations`),
    revokeDelegation: (did: string, reason: string) => call<KnDelegation>("POST", `/v1/accounts/delegations/${did}/revoke`, { reason }),
    internalUnits: (includeInactive = false) => call<{ org_ref: string; items: InternalUnit[]; kinds: KnCatalogItem[] }>("GET", "/v1/accounts/internal-units", undefined, includeInactive ? { include_inactive: "true" } : {}),
    createInternalUnit: (body: { org_ref: string; code: string; kind: string; name: string; department_code?: string; sort_order?: number }) =>
      call<InternalUnit>("POST", "/v1/accounts/internal-units", body),
    seedInternalUnits: (org_ref: string) => call<{ created: InternalUnit[]; existing: number; status: string }>("POST", "/v1/accounts/internal-units/seed", { org_ref }),
    setInternalUnitActive: (uid: string, active: boolean, reason?: string) =>
      call<InternalUnit>("POST", `/v1/accounts/internal-units/${uid}/${active ? "reactivate" : "deactivate"}`, reason ? { reason } : {}),
    createKnUser: (body: { full_name: string; email: string; phone?: string; assignments: KnAssignmentInput[] }) =>
      call<{ user: AccountsUser; assignments: KnAssignment[]; activation_email_sent: boolean; warnings: { code: string; message: string }[] }>("POST", "/v1/accounts/kn-users", body),
    assignments: (id: string) => call<{ items: KnAssignment[] }>("GET", `/v1/accounts/users/${id}/assignments`),
    grant: (id: string, body: KnAssignmentInput) => call<{ assignment: KnAssignment; warnings: { code: string; message: string }[] }>("POST", `/v1/accounts/users/${id}/assignments`, body),
    changeAssignment: (id: string, aid: string, body: KnAssignmentInput) => call<{ assignment: KnAssignment }>("PUT", `/v1/accounts/users/${id}/assignments/${aid}`, body),
    revoke: (id: string, aid: string, reason: string) => call<{ assignment: KnAssignment }>("POST", `/v1/accounts/users/${id}/assignments/${aid}/revoke`, { reason }),
    profile: (id: string, body: { full_name?: string; email?: string; phone?: string }) => call<{ user: AccountsUser; changed_fields: string[] }>("PATCH", `/v1/accounts/users/${id}/profile`, body),
    log: (id: string) => call<{ items: KnAccountLog[]; next_cursor: number | null }>("GET", `/v1/accounts/users/${id}/log`, undefined, { limit: 200 }),
    lock: (id: string, reason: string) => call<unknown>("POST", `/v1/accounts/users/${id}/lock`, { reason }),
    unlock: (id: string, reason: string) => call<unknown>("POST", `/v1/accounts/users/${id}/unlock`, { reason }),
    resend: (id: string) => call<unknown>("POST", `/v1/accounts/users/${id}/resend-actions`, {}),
    // D24: đặt lại TOTP (mất/đổi điện thoại) — lý do ≥ 20 ký tự.
    resetTotp: (id: string, reason: string) => call<unknown>("POST", `/v1/accounts/users/${id}/reset-totp`, { reason }),
    startSupport: (body: { target_assignment_id?: string; role?: "kn_tw" | "kn_tinh" | "kn_xa"; org_ref?: string; commune_codes?: string[]; department_code: string; reason: string }) =>
      call<KnSessionContext>("POST", "/v1/accounts/support-sessions", body),
    endSupport: (id: string) => call<{ support_session_id: string; ended_at: string | null }>("POST", `/v1/accounts/support-sessions/${id}/end`),
    supportLog: () => call<{ items: KnSupportLog[]; next_cursor: string | null }>("GET", "/v1/accounts/support-log", undefined, { limit: 200 }),
    departments: () => call<{ org_ref: string; items: KnDepartment[] }>("GET", "/v1/departments"),
    departmentStatus: (id: string, status: KnDepartment["status"], reason?: string) =>
      call<{ department: KnDepartment; warnings: string[] }>("POST", `/v1/departments/${id}/status`, { status, ...(reason ? { reason } : {}) }),
    departmentLog: (id: string) => call<{ items: KnLogEntry[] }>("GET", `/v1/departments/${id}/log`),
    regions: (province_code: string) => call<{ province_code: string; items: KnRegion[]; assigned_communes: number }>("GET", "/v1/regions", undefined, { province_code }),
    orgTree: () => call<{ items: KnOrgTreeNode[] }>("GET", "/v1/orgs/tree"),
    assignRegion: (ref: string, region_code: string | null, reason?: string) => call<unknown>("POST", `/v1/orgs/${ref}/region`, { region_code, ...(reason ? { reason } : {}) }),
    /** FN-AD-04.02 B3: di chuyển Nút xã sang Nút tỉnh khác (Admin API /v1/orgs/{ref}/move, lý do bắt buộc, giữ lịch sử). */
    moveOrg: (ref: string, new_parent_ref: string, reason: string, new_admin_unit_code?: string) =>
      call<unknown>("POST", `/v1/orgs/${ref}/move`, { new_parent_ref, reason, ...(new_admin_unit_code ? { new_admin_unit_code } : {}) }),
    impact: (ref: string) => call<{ org: { name: string }; communes: string[]; rows: { key?: string; label?: string; count?: number }[]; warnings: string[]; movable: boolean }>("GET", `/v1/orgs/${ref}/impact`),
  };
}

// ---------------------------------------------------------------------------
// Thu thập dữ liệu Cấp Xã (P7 — application.yaml /v1/kn/*)
// ---------------------------------------------------------------------------
export type KnCooperative = A["KNCooperative"];
export type KnHousehold = A["KNHousehold"];
export type KnCoopMachines = A["KNCoopMachines"];
export type KnMachineOverview = A["KNMachineOverview"];
export type KnPlot = A["KNPlot"];
export type MaterialRecord = A["MaterialRecord"];

export type CollectApi = ReturnType<typeof createCollectApi>;

export function createCollectApi(opts: KnHttpOptions) {
  const call = knHttp(opts);
  return {
    cooperatives: (q: { commune?: string; status?: "active" | "inactive" | "all"; q?: string } = {}) => call<A["KNCooperativeList"]>("GET", "/v1/kn/cooperatives", undefined, { ...q, limit: 200 }),
    createCooperative: (b: A["KNCooperativeCreate"], idem?: string) => call<KnCooperative>("POST", "/v1/kn/cooperatives", b, undefined, idem),
    cooperative: (ref: string) => call<KnCooperative>("GET", `/v1/kn/cooperatives/${ref}`),
    deactivate: (ref: string, reason: string) => call<A["KNCooperativeDeactivated"]>("POST", `/v1/kn/cooperatives/${ref}/deactivate`, { reason }),
    households: (ref: string) => call<A["KNHouseholdList"]>("GET", `/v1/kn/cooperatives/${ref}/households`),
    addHousehold: (ref: string, b: { holder: { full_name: string; phone: string }; cultivated_area_ha?: string }, idem?: string) =>
      call<KnHousehold>("POST", `/v1/kn/cooperatives/${ref}/households`, b, undefined, idem),
    machines: (commune?: string) => call<KnMachineOverview>("GET", "/v1/kn/machine-counts", undefined, { commune }),
    coopMachines: (ref: string) => call<KnCoopMachines>("GET", `/v1/kn/machine-counts/${ref}`),
    putMachines: (ref: string, lines: { machine_type: string; quantity: number; note?: string | null }[], idem?: string) =>
      call<KnCoopMachines>("PUT", `/v1/kn/machine-counts/${ref}`, { lines }, undefined, idem),
    plots: (q: { commune?: string; cooperative_ref?: string } = {}) => call<{ items: KnPlot[]; next_cursor: string | null }>("GET", "/v1/kn/plots", undefined, q),
    materials: (commune?: string) => call<{ items: MaterialRecord[]; next_cursor: string | null }>("GET", "/v1/kn/materials", undefined, { commune }),
    recordMaterials: (b: { commune_code: string; recorded_on: string; lines: { material_code: string; quantity: string; note?: string }[] }, idem?: string) =>
      call<{ items: MaterialRecord[] }>("POST", "/v1/kn/materials", b, undefined, idem),
    materialSummary: (commune?: string) =>
      call<{ items: { commune_code: string; material_code: string; material_name: string; unit: string; quantity: string; records: number }[] }>("GET", "/v1/kn/materials/summary", undefined, { commune }),
  };
}
