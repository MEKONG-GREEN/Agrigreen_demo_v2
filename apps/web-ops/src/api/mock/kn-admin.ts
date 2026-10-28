// ============================================================================
// BACKEND GIẢ LẬP quản trị KN v7.7 (admin.yaml mục pf/accounts P1, pf/party P2) cho màn Admin:
//   FN-AD-04.04 tài khoản nhiều vai trò: GET /v1/accounts/users (khi có ngữ cảnh Admin KN), /users/{id},
//     POST /v1/accounts/kn-users, /users/{id}/assignments (GET/POST), PUT …/{aid}, POST …/{aid}/revoke,
//     PATCH /users/{id}/profile, GET /users/{id}/log, POST lock/unlock/resend-actions, GET /v1/accounts/mgmt-levels
//   FN-AD-04.07 Menu các cấp: POST /v1/accounts/support-sessions, /{id}/end, GET /v1/accounts/support-log;
//     mọi thao tác ghi trong ngữ cảnh hỗ trợ bắt buộc X-AG-Reason ≥ 10 ký tự, ghi hai dòng nhật ký (attempted, completed)
//   FN-AD-04.03 phòng ban: GET /v1/departments, POST /{id}/status, GET /{id}/log
//   FN-AD-04.02 cơ cấu: GET /v1/orgs/tree, /v1/regions, POST /v1/orgs/{ref}/region, GET /v1/orgs/{ref}/impact
// Quy tắc như P1/P2 (accounts/app/kn*.go, party/app/departments.go): tiên quyết Nút + phòng ban, trùng SĐT/email,
// Cấp Xã chưa gán xã → cảnh báo, Admin chỉ gán qua platform_admin, lý do ≥ 10 ký tự, sổ chỉ ghi thêm.
// ============================================================================
import { uuidv7 } from "../idempotency";
import type { DepartmentRef, KnRole, RoleAssignment, SessionContext } from "@/kn/contract";
import { NATIONAL_REF, provinceOrgRef } from "./data";
import { hasRole, json, problem, problemOf, type Ctx, type Route } from "./http";
import { findKnAccount, KN_ACCOUNTS, KN_DEPARTMENTS, KN_MENU_PROFILE, type MockKnAccount } from "./kn-accounts";
import { clearMockKnLoginLock, dropSupportContexts, knStoredContext, mockKnLoginLockedUntil, registerKnContext, knMenu } from "./kn";
import { stableUuid } from "./kn-schedules";
import { AG_COMMUNES, AG_PROVINCE, AG_REGIONS, communeName, regionName } from "./kn-reports/store";

const AG_REF = provinceOrgRef(1);
const T0 = "2026-08-20T01:30:00Z";

/** Danh mục cấp quản lý (accounts/port.MgmtLevels, accounts.kn-roles/2 — W11.07, Q-059). */
export const MGMT_LEVELS = [
  { code: "chuyen_vien", label: "Chuyên viên - Cán bộ", manager: false, reads_all_departments: false },
  { code: "phu_trach", label: "Phụ trách", manager: true, reads_all_departments: false },
  { code: "pho_truong_phong", label: "Phó trưởng phòng", manager: true, reads_all_departments: false },
  { code: "truong_phong", label: "Trưởng phòng", manager: true, reads_all_departments: false },
  { code: "lanh_dao_don_vi", label: "Lãnh đạo đơn vị", manager: true, reads_all_departments: true },
  { code: "pho_giam_doc", label: "Phó Giám đốc", manager: true, reads_all_departments: false },
  { code: "giam_doc", label: "Giám đốc", manager: true, reads_all_departments: false },
];
/** W11.07: nhóm Cấp Tỉnh, cờ nhiệm vụ, quyền ủy quyền (cùng danh mục với Go). */
export const PROVINCE_GROUPS = [
  { code: "vien_chuc", label: "Viên chức" },
  { code: "tong_hop", label: "Cán bộ tổng hợp" },
  { code: "chu_nhiem", label: "Chủ nhiệm chương trình/dự án" },
  { code: "lanh_dao_phong", label: "Lãnh đạo phòng" },
  { code: "ban_giam_doc", label: "Ban Giám đốc (chỉ xem)" },
];
export const DUTIES = [
  { code: "qtnv_bao_gia", label: "Quản trị nghiệp vụ báo giá (QTNV)" },
  { code: "phu_trach_bieu", label: "Người phụ trách biểu" },
];
export const POWERS = [
  { code: "mo_lai_ky", label: "Mở lại kỳ" },
  { code: "duyet_bao_cao", label: "Duyệt, xác thực báo cáo" },
];
// Nhãn cũ "Chuyên viên" (dữ liệu giả lập trước W11.07) ↔ chuyen_vien.
const LEGACY_LABEL: Record<string, string> = { "Chuyên viên": "chuyen_vien" };
const mgmtLabel = (code: string | null | undefined) => MGMT_LEVELS.find((m) => m.code === code || m.label === code)?.label ?? (code && LEGACY_LABEL[code] ? code : null);
const mgmtCode = (label: string | null | undefined) => MGMT_LEVELS.find((m) => m.label === label || m.code === label)?.code ?? (label ? (LEGACY_LABEL[label] ?? null) : null);

// ---------------------------------------------------------------------------
// W11.07: thuộc tính vai, ủy quyền có hạn, đơn vị nội bộ (giả lập trong bộ nhớ)
// ---------------------------------------------------------------------------
interface MockAttrs {
  province_group: string | null;
  duties: string[];
  internal_unit_code: string | null;
  facility_refs: string[];
  household_refs: string[];
}
const ATTRS = new Map<string, MockAttrs>();
const emptyAttrs = (): MockAttrs => ({ province_group: null, duties: [], internal_unit_code: null, facility_refs: [], household_refs: [] });
const attrsWire = (aid: string) => {
  const a = ATTRS.get(aid) ?? emptyAttrs();
  return { ...a, province_group_label: PROVINCE_GROUPS.find((g) => g.code === a.province_group)?.label ?? null };
};
interface MockUnit {
  id: string;
  org_ref: string;
  code: string;
  kind: string;
  kind_label: string;
  name: string;
  department_code: string | null;
  sort_order: number;
  active: boolean;
  updated_at: string;
}
export const UNIT_KINDS = [
  { code: "ban_giam_doc", label: "Ban Giám đốc" },
  { code: "van_phong", label: "Văn phòng" },
  { code: "phong", label: "Phòng" },
  { code: "tram", label: "Trạm" },
  { code: "trai", label: "Trại" },
  { code: "tang_cuong", label: "Tăng cường" },
];
const UNITS: MockUnit[] = [];
interface MockDelegation {
  delegation_id: string;
  grantor_ref: string;
  grantee_ref: string;
  department_code: string;
  power: string;
  power_label: string;
  valid_from: string;
  valid_to: string;
  revoked: boolean;
  status: "active" | "scheduled" | "expired" | "revoked";
  reason: string;
  created_at: string;
}
const DELEGATIONS: MockDelegation[] = [];

interface Profile {
  email: string;
  phone: string | null;
  enabled: boolean;
  created_at: string;
  created_by: string;
  activated_at: string | null;
  last_login: string | null;
  revoked: Map<string, { at: string; by: string }>;
}

interface AcctLog {
  seq: number;
  event: string;
  entity_ref: string | null;
  actor_ref: string;
  at: string;
  changes: { field: string; old: string | null; new: string | null }[];
  reason: string | null;
}

interface SupportLog {
  id: string;
  support_session_id: string;
  context_id: string;
  request_id: string;
  phase: "attempted" | "completed";
  actor_ref: string;
  actor_display: string | null;
  on_behalf_of: string | null;
  on_behalf_of_display: string | null;
  acted_role: "kn_tw" | "kn_tinh" | "kn_xa";
  department_code: string;
  function: string;
  content: Record<string, unknown>;
  status_code: number | null;
  reason: string;
  trace_id: string | null;
  at: string;
}

interface DeptLog {
  seq: number;
  event: string;
  actor_ref: string;
  at: string;
  changes: { field: string; old: string | null; new: string | null }[];
  reason: string | null;
}

const ADMIN_REF = stableUuid("kn-admin");
const PROFILES = new Map<string, Profile>();
const LOGS = new Map<string, AcctLog[]>();
const SUPPORT_LOG: SupportLog[] = [];
const DEPT_LOG = new Map<string, DeptLog[]>();
let seq = 0;

const userId = (a: MockKnAccount) => stableUuid(a.id);
const accountByUserId = (id: string) => KN_ACCOUNTS.find((a) => userId(a) === id);
const now = () => new Date().toISOString().replace(/\.\d{3}Z$/, "Z");

function profileOf(a: MockKnAccount): Profile {
  let p = PROFILES.get(a.id);
  if (!p) {
    p = {
      email: a.identifiers.find((i) => i.includes("@")) ?? "",
      phone: a.identifiers.find((i) => /^0\d{9}$/.test(i)) ?? null,
      enabled: true,
      created_at: T0,
      created_by: ADMIN_REF,
      activated_at: "2026-08-20T02:10:00Z",
      last_login: "2026-09-08T00:45:00Z",
      revoked: new Map(),
    };
    PROFILES.set(a.id, p);
    LOGS.set(a.id, [
      {
        seq: ++seq,
        event: "account.created",
        entity_ref: null,
        actor_ref: ADMIN_REF,
        at: T0,
        changes: [{ field: "roles", old: null, new: a.assignments.map((x) => roleText(x.role)).join(", ") }],
        reason: null,
      },
    ]);
  }
  return p;
}

const ROLE_TEXT: Record<KnRole, string> = { kn_tw: "Cấp Quốc gia", kn_tinh: "Cấp Tỉnh", kn_xa: "Cấp Xã", admin: "Admin" };
const roleText = (r: KnRole) => ROLE_TEXT[r];

function log(a: MockKnAccount, e: Omit<AcctLog, "seq" | "at"> & { at?: string }) {
  profileOf(a);
  LOGS.get(a.id)!.push({ ...e, seq: ++seq, at: e.at ?? now() });
}

/** D24: tài khoản KN đã được Admin đặt lại TOTP trong phiên mock (chờ đăng ký lại). */
const KN_TOTP_RESET = new Set<string>();

function userWire(a: MockKnAccount) {
  const p = profileOf(a);
  const parts = a.displayName.split(" ");
  const totpPending = !p.activated_at || KN_TOTP_RESET.has(userId(a));
  return {
    id: userId(a),
    username: p.phone ?? p.email,
    email: p.email,
    first_name: parts.length > 1 ? parts[parts.length - 1]! : a.displayName,
    last_name: parts.length > 1 ? parts.slice(0, -1).join(" ") : "",
    phone: p.phone,
    enabled: p.enabled,
    email_verified: true,
    org_ref: a.assignments[0]?.org_ref ?? null,
    roles: ["kn_user"],
    lock_cause: p.enabled ? null : "manual",
    required_actions: [...(p.activated_at ? [] : ["UPDATE_PASSWORD", "VERIFY_EMAIL"]), ...(totpPending ? ["CONFIGURE_TOTP"] : [])],
    created_at: p.created_at,
    tracked: true,
    // D24: mọi cán bộ KN bắt TOTP khi đăng nhập.
    mfa: { required: true, configured: !totpPending, enrollment_pending: totpPending },
  };
}

/**
 * ADMIN-UNLOCK (#42/#61): GET chi tiết thêm login_locked (+ login_locked_until) đọc từ bộ đếm đăng nhập giả lập
 * (mockKnLogin: sai 5 lần → khóa tạm 15 phút). Danh sách/tạo dùng userWire — bỏ field như máy chủ (không N+1).
 */
function userDetailWire(a: MockKnAccount) {
  const until = mockKnLoginLockedUntil(a.identifiers);
  return { ...userWire(a), login_locked: until !== null, ...(until ? { login_locked_until: until } : {}) };
}

function assignmentWire(a: MockKnAccount, x: RoleAssignment) {
  const p = profileOf(a);
  const rv = p.revoked.get(x.assignment_id);
  return {
    assignment_id: x.assignment_id.length === 36 ? x.assignment_id : stableUuid(x.assignment_id),
    user_ref: userId(a),
    role: x.role,
    org_ref: x.role === "admin" ? null : x.org_ref,
    org_name: x.org_name,
    mgmt_level: x.mgmt_level,
    mgmt_level_code: x.mgmt_level_code ?? mgmtCode(x.mgmt_level),
    communes: x.communes,
    departments: x.departments,
    valid_from: T0,
    valid_to: rv?.at ?? null,
    active: !rv,
    created_by: ADMIN_REF,
    created_at: T0,
    updated_by: ADMIN_REF,
    updated_at: T0,
    attributes: attrsWire(x.assignment_id.length === 36 ? x.assignment_id : stableUuid(x.assignment_id)),
  };
}

const wireAid = (x: RoleAssignment) => (x.assignment_id.length === 36 ? x.assignment_id : stableUuid(x.assignment_id));

/** Admin KN (ngữ cảnh Admin hoặc vai realm quản trị tài khoản). */
function isKnAdmin(ctx: Ctx): boolean {
  return hasRole(ctx.persona, "platform_admin", "account_admin");
}
const deny = () => problem(403, "forbidden", "Forbidden");

/** Có ngữ cảnh Admin KN (X-AG-Context) — để GET /v1/accounts/users trả tài khoản KN thay danh sách cũ. */
function knAdminContext(_url: URL, request: Request): boolean {
  return knStoredContext(request.headers.get("X-AG-Context"))?.ctx.role === "admin";
}

const errField = (field: string, reason: string) => problem(422, "validation_failed", "Dữ liệu không hợp lệ", { errors: [{ field, reason }] });

function normPhone(p: string): string {
  return p.replace(/[\s.-]/g, "").replace(/^\+84/, "0");
}

/** Kiểm một lần gán (KNAssignmentInput) → RoleAssignment. */
function buildAssignment(input: Record<string, unknown>, id: string): RoleAssignment | Response {
  const role = input.role as KnRole;
  if (!["kn_tw", "kn_tinh", "kn_xa", "admin"].includes(role)) return errField("role", "kn_tw | kn_tinh | kn_xa | admin");
  if (role === "admin") return problem(403, "accounts.kn_admin_grant", "Vai trò Admin chỉ platform_admin gán cho tài khoản platform_admin");
  const org = String(input.org_ref ?? "");
  if (role === "kn_tw" ? org !== NATIONAL_REF : org !== AG_REF) return problem(409, "accounts.prerequisite_missing", "Nút không hợp lệ cho vai trò hoặc đơn vị chưa khai báo phòng ban");
  const codes = Array.isArray(input.department_codes) ? (input.department_codes as string[]) : [];
  if (!codes.length) return errField("department_codes", "chọn ít nhất một phòng ban");
  const depts: DepartmentRef[] = [];
  for (const c of codes) {
    const d = KN_DEPARTMENTS.find((x) => x.code === c);
    if (!d) return problem(422, "accounts.department_invalid", "Phòng ban không thuộc đơn vị của Nút", { errors: [{ field: "department_codes", reason: c }] });
    depts.push(d);
  }
  const communeCodes = role === "kn_xa" && Array.isArray(input.commune_codes) ? (input.commune_codes as string[]) : [];
  for (const c of communeCodes) if (!AG_COMMUNES.some((x) => x.code === c)) return problem(422, "accounts.commune_invalid", "Xã phụ trách không thuộc Nút tỉnh của vai trò Cấp Xã");
  return {
    assignment_id: id,
    role,
    org_ref: org,
    org_name: role === "kn_tw" ? "Trung tâm Khuyến nông Quốc gia" : "TTKN tỉnh An Giang",
    mgmt_level: mgmtLabel(input.mgmt_level as string) ?? "Chuyên viên",
    mgmt_level_code: mgmtCode(input.mgmt_level as string) ?? "chuyen_vien",
    communes: communeCodes.map((c) => ({ code: c, name: communeName(c) })),
    departments: depts,
  };
}

const warningsOf = (x: RoleAssignment) => (x.role === "kn_xa" && !x.communes.length ? [{ code: "commune_unassigned", message: "Cấp Xã chưa gán xã/phường — chưa xem được dữ liệu nghiệp vụ" }] : []);

function scopeText(x: RoleAssignment): string {
  return `${roleText(x.role)} · ${x.departments.map((d) => d.name.replace(/^Phòng /, "")).join(", ")}${x.communes.length ? ` · ${x.communes.map((c) => c.name).join(", ")}` : ""}`;
}

// ---------------------------------------------------------------------------
// Chế độ hỗ trợ (FN-AD-04.07)
// ---------------------------------------------------------------------------
const SUPPORT_OPEN = new Map<string, { admin: string; ended: string | null }>();

/** Gọi ở fetch giả lập trước khi định tuyến: ghi trong ngữ cảnh hỗ trợ bắt buộc lý do. null = cho qua. */
export function supportGuard(request: Request, url: URL): Response | null {
  const st = knStoredContext(request.headers.get("X-AG-Context"));
  if (!st?.support || request.method === "GET") return null;
  if (url.pathname === "/v1/me/context" || url.pathname.startsWith("/v1/accounts/support-sessions")) return null;
  let reason = "";
  try {
    reason = decodeURIComponent(request.headers.get("X-AG-Reason") ?? "").trim();
  } catch {
    reason = "";
  }
  if ([...reason].length < 10 || [...reason].length > 500) return problem(422, "accounts.support_reason_required", "Thao tác trong chế độ hỗ trợ phải nhập lý do (ít nhất 10 ký tự)");
  return null;
}

/** Hai dòng nhật ký hỗ trợ (attempted trước, completed sau) cho mỗi thao tác ghi. */
export function supportRecord(request: Request, url: URL, status: number | null, phase: "attempted" | "completed", requestId: string): void {
  const st = knStoredContext(request.headers.get("X-AG-Context"));
  if (!st?.support || request.method === "GET") return;
  const admin = findKnAccount(st.accountId);
  const target = st.support.onBehalfOf ? findKnAccount(st.support.onBehalfOf) : undefined;
  const fn = `${request.method} ${url.pathname.replace(/[0-9a-f]{8}-[0-9a-f-]{27}/g, ":id")}`;
  SUPPORT_LOG.push({
    id: uuidv7(),
    support_session_id: st.support.sessionId,
    context_id: st.ctx.context_id,
    request_id: requestId,
    phase,
    actor_ref: stableUuid(st.accountId),
    actor_display: admin?.displayName ?? null,
    on_behalf_of: target ? stableUuid(target.id) : null,
    on_behalf_of_display: target ? `${target.displayName}${st.ctx.communes.length ? ` · ${st.ctx.communes.map((c) => c.name).join(", ")}` : ""}` : st.ctx.communes.map((c) => c.name).join(", ") || null,
    acted_role: st.ctx.role as "kn_tw" | "kn_tinh" | "kn_xa",
    department_code: st.ctx.department_code ?? "",
    function: fn,
    content: { path_params: (url.pathname.match(/[0-9a-f]{8}-[0-9a-f-]{27}/g) ?? []) },
    status_code: status,
    reason: decodeURIComponent(request.headers.get("X-AG-Reason") ?? ""),
    trace_id: null,
    at: now(),
  });
}

function startSupport(ctx: Ctx): Response {
  if (!hasRole(ctx.persona, "platform_admin") || !ctx.persona.amr.includes("otp")) return problem(403, "forbidden", "Chỉ Admin nền tảng đã xác thực nhiều yếu tố");
  const b = (ctx.body ?? {}) as { target_assignment_id?: string; role?: KnRole; org_ref?: string; commune_codes?: string[]; department_code?: string; reason?: string };
  const reason = String(b.reason ?? "").trim();
  if ([...reason].length < 10) return errField("reason", "bắt buộc, ít nhất 10 ký tự");
  const dept = KN_DEPARTMENTS.find((d) => d.code === b.department_code);
  if (!dept) return problem(422, "accounts.department_required", "Thiếu phòng ban hoặc phòng ban không thuộc vai trò");
  if (dept.status !== "active") return problem(409, "accounts.department_not_open", "Phòng ban chưa Đang áp dụng");
  const adminAcct = KN_ACCOUNTS.find((a) => `mock-${a.id}` === ctx.persona.me.actor_ref) ?? findKnAccount(ctx.persona.id);
  if (!adminAcct) return deny();
  let target: MockKnAccount | undefined;
  let asg: RoleAssignment;
  if (b.target_assignment_id) {
    target = KN_ACCOUNTS.find((a) => a.assignments.some((x) => wireAid(x) === b.target_assignment_id));
    const x = target?.assignments.find((y) => wireAid(y) === b.target_assignment_id);
    if (!target || !x || x.role === "admin") return problem(404, "accounts.assignment_not_found", "Không tìm thấy lần gán vai trò");
    if (!x.departments.some((d) => d.code === dept.code)) return problem(422, "accounts.department_required", "Phòng ban không thuộc vai trò");
    asg = { ...x };
  } else {
    if (!b.role || !["kn_tw", "kn_tinh", "kn_xa"].includes(b.role)) return errField("role", "kn_tw | kn_tinh | kn_xa");
    const communes = (b.commune_codes ?? []).map((c) => ({ code: c, name: communeName(c) }));
    asg = { assignment_id: uuidv7(), role: b.role, org_ref: b.role === "kn_tw" ? NATIONAL_REF : AG_REF, org_name: b.role === "kn_tw" ? "Trung tâm Khuyến nông Quốc gia" : "TTKN tỉnh An Giang", mgmt_level: null, communes, departments: [dept] };
  }
  const sessionId = uuidv7();
  const sc: SessionContext = {
    context_id: uuidv7(),
    assignment_id: target ? wireAid(asg) : null,
    role: asg.role,
    department_code: dept.code,
    department_name: dept.name,
    org_ref: asg.org_ref,
    org_name: asg.org_name,
    communes: asg.communes,
    default_route: knMenu(asg.role, dept.code, { orgRef: asg.org_ref }).default_route,
    expires_at: new Date(Date.now() + 4 * 3_600_000).toISOString(),
    support_session_id: sessionId,
    on_behalf_of: target ? userId(target) : null,
  };
  registerKnContext({ accountId: adminAcct.id, assignment: asg, ctx: sc, support: { sessionId, onBehalfOf: target?.id ?? null, onBehalfOfName: target?.displayName ?? null, reason, startedAt: now(), endedAt: null } });
  SUPPORT_OPEN.set(sessionId, { admin: adminAcct.id, ended: null });
  return json(sc, 201);
}

// ---------------------------------------------------------------------------
// Phòng ban (FN-AD-04.03)
// ---------------------------------------------------------------------------
const deptId = (code: string) => stableUuid(`dept-${code}`);
const STATUS_LABEL: Record<DepartmentRef["status"], string> = { active: "Đang áp dụng", not_open: "Chưa mở", suspended: "Ngưng hoạt động" };

function deptWire(d: DepartmentRef, i: number) {
  const accounts = KN_ACCOUNTS.filter((a) => a.assignments.some((x) => x.departments.some((y) => y.code === d.code))).length;
  return {
    id: deptId(d.code),
    org_ref: AG_REF,
    code: d.code,
    name: d.name,
    description: d.description,
    sort_order: i + 1,
    status: d.status,
    status_label: STATUS_LABEL[d.status],
    menu_profile: KN_MENU_PROFILE[d.code] ?? "generic",
    retired: false,
    selectable: d.status === "active",
    account_count: accounts,
    record_count: d.code === "trong_trot" ? 1 : 0,
    updated_by: ADMIN_REF,
    updated_at: T0,
  };
}

// ---------------------------------------------------------------------------
// Cây tổ chức (FN-AD-04.02)
// ---------------------------------------------------------------------------
const communeRef = (code: string) => stableUuid(`org-${code}`);

/** Giả lập FN-AD-04.02 B3: Nút xã đã chuyển sang TTKN tỉnh thứ hai (chỉ để màn cây thể hiện; dữ liệu khác giữ nguyên). */
const OTHER_PROVINCE_REF = provinceOrgRef(2);
const MOVED = new Set<string>();

function orgTree() {
  const regionNode = (r: { code: string; name: string } | null) => {
    const members = AG_COMMUNES.filter((c) => !MOVED.has(c.code) && (r ? c.region === r.code : !c.region));
    return {
      ref: null,
      kind: r ? "region" : "region_unassigned",
      code: r?.code ?? "",
      name: r?.name ?? "Chưa gán khu vực",
      admin_unit_code: null,
      level: 0,
      active: true,
      region: null,
      commune_count: members.length,
      children: members.map((c) => ({
        ref: communeRef(c.code),
        kind: "extension_team",
        code: `AG-${c.code}`,
        name: c.name,
        admin_unit_code: c.code,
        level: 3,
        active: true,
        region: c.region ? { code: c.region, name: regionName(c.region) } : null,
        commune_count: 0,
        children: [],
      })),
    };
  };
  const groups = [...AG_REGIONS.map((r) => regionNode(r)), ...(AG_COMMUNES.some((c) => !c.region) ? [regionNode(null)] : [])].filter((g) => g.commune_count > 0 || g.kind === "region");
  return {
    items: [
      {
        ref: NATIONAL_REF,
        kind: "ttkn_national",
        code: "TTKNQG",
        name: "Trung tâm Khuyến nông Quốc gia",
        admin_unit_code: null,
        level: 1,
        active: true,
        region: null,
        commune_count: AG_COMMUNES.length,
        children: [
          { ref: AG_REF, kind: "ttkn_province", code: "TTKN-AG", name: "TTKN tỉnh An Giang", admin_unit_code: AG_PROVINCE, level: 2, active: true, region: null, commune_count: AG_COMMUNES.length - MOVED.size, children: groups },
          {
            ref: OTHER_PROVINCE_REF,
            kind: "ttkn_province",
            code: "TTKN-CT",
            name: "TTKN tỉnh Cần Thơ",
            admin_unit_code: "92",
            level: 2,
            active: true,
            region: null,
            commune_count: MOVED.size,
            children: AG_COMMUNES.filter((c) => MOVED.has(c.code)).map((c) => ({ ref: communeRef(c.code), kind: "extension_team", code: `AG-${c.code}`, name: c.name, admin_unit_code: c.code, level: 3, active: true, region: null, commune_count: 0, children: [] })),
          },
        ],
      },
    ],
  };
}

// ---------------------------------------------------------------------------
// Định tuyến (Admin API)
// ---------------------------------------------------------------------------
const UUID = "([0-9a-f-]{36})";
const re = (s: string) => new RegExp(`^${s.replaceAll("{id}", UUID)}$`);

function withAccount(ctx: Ctx, h: (a: MockKnAccount) => Response): Response {
  if (!isKnAdmin(ctx)) return deny();
  const a = accountByUserId(ctx.params[0] ?? "");
  return a ? h(a) : problem(404, "accounts.user_not_found", "Không tìm thấy tài khoản");
}

const reasonOk = (b: unknown, min: number) => {
  const r = String((b as { reason?: unknown } | undefined)?.reason ?? "").trim();
  return [...r].length >= min ? r : null;
};

export const knAdminRoutes: Route[] = [
  {
    method: "GET",
    pattern: re("/v1/accounts/mgmt-levels"),
    handler: (ctx) =>
      isKnAdmin(ctx)
        ? json({ items: MGMT_LEVELS, provisional: true, version: "accounts.kn-roles/2", province_groups: PROVINCE_GROUPS, duties: DUTIES, powers: POWERS, delegation_max_days: 30 })
        : deny(),
  },
  // W11.07: thuộc tính phân quyền trong vai
  {
    method: "PUT",
    pattern: re("/v1/accounts/users/{id}/assignments/{id}/attributes"),
    handler: (ctx) =>
      withAccount(ctx, (a) => {
        const x = a.assignments.find((y) => wireAid(y) === ctx.params[1]);
        if (!x) return problem(404, "accounts.assignment_not_found", "Không tìm thấy lần gán vai trò");
        const b = (ctx.body ?? {}) as Partial<MockAttrs> & { internal_unit_code?: string };
        const group = b.province_group || null;
        const duties = [...new Set(b.duties ?? [])].sort();
        const fac = b.facility_refs ?? [];
        const hh = b.household_refs ?? [];
        if (group && !PROVINCE_GROUPS.some((g) => g.code === group)) return errField("province_group", "nhóm Cấp Tỉnh không có trong danh mục");
        if (duties.some((d) => !DUTIES.some((x2) => x2.code === d))) return errField("duties", "cờ nhiệm vụ không có trong danh mục");
        if (x.role !== "kn_tinh" && (group || duties.length)) return errField("province_group", "nhóm Cấp Tỉnh và cờ nhiệm vụ chỉ dành cho vai Cấp Tỉnh");
        if (x.role !== "kn_xa" && (fac.length || hh.length)) return errField("facility_refs", "giới hạn cơ sở/hộ chỉ dành cho vai Cấp Xã");
        const unit = b.internal_unit_code || null;
        if (unit && !UNITS.some((u) => u.code === unit && u.active)) return problem(422, "accounts.internal_unit_invalid", "Đơn vị nội bộ không thuộc đơn vị của lần gán hoặc đã ngừng");
        const aid = wireAid(x);
        const before = attrsWire(aid);
        ATTRS.set(aid, { province_group: group, duties, internal_unit_code: unit, facility_refs: fac, household_refs: hh });
        const after = attrsWire(aid);
        const changes = (["province_group", "duties", "internal_unit_code"] as const)
          .map((k) => ({ field: k, old: [before[k]].flat().join(",") || null, new: [after[k]].flat().join(",") || null }))
          .filter((c) => c.old !== c.new);
        if (changes.length) log(a, { event: "assignment.attributes_changed", entity_ref: aid, actor_ref: ADMIN_REF, changes, reason: null });
        return json({ assignment_id: aid, attributes: after, seq: changes.length ? seq : 0 });
      }),
  },
  // W11.07: ủy quyền có hạn (Admin xem, thu hồi)
  {
    method: "GET",
    pattern: re("/v1/accounts/users/{id}/delegations"),
    handler: (ctx) => withAccount(ctx, (a) => json({ items: DELEGATIONS.filter((d) => d.grantor_ref === userId(a) || d.grantee_ref === userId(a)) })),
  },
  {
    method: "POST",
    pattern: re("/v1/accounts/delegations/{id}/revoke"),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return deny();
      if (!reasonOk(ctx.body, 10)) return errField("reason", "bắt buộc, ít nhất 10 ký tự");
      const d = DELEGATIONS.find((x) => x.delegation_id === ctx.params[0]);
      if (!d) return problem(404, "accounts.delegation_not_found", "Không tìm thấy ủy quyền");
      if (d.revoked || d.status === "expired") return problem(409, "accounts.delegation_state", "Ủy quyền đã thu hồi hoặc đã hết hạn");
      d.revoked = true;
      d.status = "revoked";
      return json(d);
    },
  },
  // W11.07: đơn vị nội bộ (Q-068)
  {
    method: "GET",
    pattern: re("/v1/accounts/internal-units"),
    handler: (ctx) =>
      isKnAdmin(ctx)
        ? json({ org_ref: AG_REF, items: UNITS.filter((u) => ctx.url.searchParams.get("include_inactive") === "true" || u.active), kinds: UNIT_KINDS })
        : deny(),
  },
  {
    method: "POST",
    pattern: re("/v1/accounts/internal-units/seed"),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return deny();
      const seed: [string, string, string, string | null, number][] = [
        ["ban_giam_doc", "ban_giam_doc", "Ban Giám đốc", null, 0],
        ...KN_DEPARTMENTS.map((d, i) => [d.code, "phong", d.name, d.code, (i + 1) * 10] as [string, string, string, string | null, number]),
      ];
      const created: MockUnit[] = [];
      let existing = 0;
      for (const [code, kind, name, dept, sort] of seed) {
        if (UNITS.some((u) => u.code === code)) {
          existing++;
          continue;
        }
        const u: MockUnit = { id: uuidv7(), org_ref: AG_REF, code, kind, kind_label: UNIT_KINDS.find((k) => k.code === kind)!.label, name, department_code: dept, sort_order: sort, active: true, updated_at: now() };
        UNITS.push(u);
        created.push(u);
      }
      return json({ created, existing, status: "tạm, chờ khách (Q-068)" });
    },
  },
  {
    method: "POST",
    pattern: re("/v1/accounts/internal-units"),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return deny();
      const b = (ctx.body ?? {}) as { code?: string; kind?: string; name?: string; department_code?: string; sort_order?: number };
      const code = String(b.code ?? "").trim();
      if (!/^[a-z][a-z0-9_]{1,39}$/.test(code)) return errField("code", "mã 2–40 ký tự [a-z0-9_]");
      const kind = UNIT_KINDS.find((k) => k.code === b.kind);
      if (!kind) return errField("kind", "loại đơn vị nội bộ không có trong danh mục");
      if (!String(b.name ?? "").trim()) return errField("name", "bắt buộc");
      if (b.department_code && kind.code !== "phong") return errField("department_code", "chỉ đơn vị loại phòng mới gắn phòng ban");
      if (UNITS.some((u) => u.code === code)) return problem(409, "accounts.internal_unit_code_taken", "Mã đơn vị nội bộ đã có trong đơn vị");
      const u: MockUnit = { id: uuidv7(), org_ref: AG_REF, code, kind: kind.code, kind_label: kind.label, name: String(b.name).trim(), department_code: b.department_code || null, sort_order: b.sort_order ?? 0, active: true, updated_at: now() };
      UNITS.push(u);
      UNITS.sort((x, y) => x.sort_order - y.sort_order || x.code.localeCompare(y.code));
      return json(u, 201);
    },
  },
  {
    method: "POST",
    pattern: new RegExp(`^/v1/accounts/internal-units/${UUID}/(deactivate|reactivate)$`),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return deny();
      const u = UNITS.find((x) => x.id === ctx.params[0]);
      if (!u) return problem(404, "accounts.internal_unit_not_found", "Không tìm thấy đơn vị nội bộ");
      const active = ctx.params[1] === "reactivate";
      if (!active && !reasonOk(ctx.body, 10)) return errField("reason", "lý do ngừng ít nhất 10 ký tự");
      if (u.active === active) return problem(409, "accounts.internal_unit_state", "Đơn vị nội bộ đã ở trạng thái này");
      u.active = active;
      u.updated_at = now();
      return json(u);
    },
  },
  {
    method: "GET",
    pattern: re("/v1/accounts/users"),
    match: knAdminContext,
    handler: (ctx) => (isKnAdmin(ctx) ? json({ items: KN_ACCOUNTS.map(userWire), next_cursor: null }) : deny()),
  },
  { method: "GET", pattern: re("/v1/accounts/users/{id}"), match: knAdminContext, handler: (ctx) => withAccount(ctx, (a) => json(userDetailWire(a))) },
  {
    method: "POST",
    pattern: re("/v1/accounts/kn-users"),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return problem(403, "accounts.kn_admin_grant", "Chỉ Admin quản lý tài khoản");
      const b = (ctx.body ?? {}) as { full_name?: string; email?: string; phone?: string; assignments?: Record<string, unknown>[] };
      const name = String(b.full_name ?? "").trim();
      if (!name) return errField("full_name", "bắt buộc");
      const email = String(b.email ?? "").trim().toLowerCase();
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) return errField("email", "email không hợp lệ");
      const phone = b.phone ? normPhone(b.phone) : null;
      if (phone && !/^0\d{9}$/.test(phone)) return errField("phone", "10 chữ số bắt đầu bằng 0");
      const ids = [email, ...(phone ? [phone] : [])];
      if (KN_ACCOUNTS.some((a) => a.identifiers.some((i) => ids.includes(i)))) return problem(409, "accounts.duplicate_identity", "SĐT hoặc email đã là của tài khoản khác");
      if (!Array.isArray(b.assignments) || !b.assignments.length) return errField("assignments", "ít nhất một vai trò");
      const built: RoleAssignment[] = [];
      for (const input of b.assignments) {
        const x = buildAssignment(input, uuidv7());
        if (x instanceof Response) return x;
        if (built.some((y) => y.role === x.role && y.org_ref === x.org_ref)) return problem(409, "accounts.assignment_duplicate", "Tài khoản đã có vai trò này trên cùng Nút");
        built.push(x);
      }
      const acct: MockKnAccount = { id: `kn-${uuidv7().slice(-12)}`, displayName: name, identifiers: ids, assignments: built };
      KN_ACCOUNTS.push(acct);
      PROFILES.set(acct.id, { email, phone, enabled: true, created_at: now(), created_by: ADMIN_REF, activated_at: null, last_login: null, revoked: new Map() });
      LOGS.set(acct.id, []);
      log(acct, { event: "account.created", entity_ref: null, actor_ref: ADMIN_REF, changes: [{ field: "roles", old: null, new: built.map(scopeText).join("; ") }], reason: null });
      log(acct, { event: "account.activation_sent", entity_ref: null, actor_ref: ADMIN_REF, changes: [], reason: null });
      return json({ user: userWire(acct), assignments: built.map((x) => assignmentWire(acct, x)), activation_email_sent: true, warnings: built.flatMap(warningsOf) }, 201);
    },
  },
  {
    method: "POST",
    pattern: re("/v1/accounts/kn-users/import"),
    handler: (ctx) => importKnUsers(ctx),
  },
  {
    method: "GET",
    pattern: re("/v1/accounts/users/{id}/sessions"),
    handler: (ctx) =>
      withAccount(ctx, (a) => {
        const p = profileOf(a);
        const at = p.last_login ?? T0;
        return json({
          available: true,
          active: p.enabled ? [{ id: `s-${a.id}`, ip_address: "10.20.0.15", started_at: at, last_access_at: at, clients: ["agrigreen-web-ops"] }] : [],
          history: [
            { at, type: "LOGIN", ip_address: "10.20.0.15", client: "agrigreen-web-ops", error: null },
            { at: T0, type: "LOGIN_ERROR", ip_address: "10.20.0.15", client: "agrigreen-web-ops", error: "invalid_user_credentials" },
          ],
        });
      }),
  },
  {
    method: "GET",
    pattern: re("/v1/accounts/users/{id}/assignments"),
    handler: (ctx) => withAccount(ctx, (a) => json({ items: a.assignments.map((x) => assignmentWire(a, x)).filter((x) => ctx.url.searchParams.get("include_revoked") === "true" || x.active) })),
  },
  {
    method: "POST",
    pattern: re("/v1/accounts/users/{id}/assignments"),
    handler: (ctx) =>
      withAccount(ctx, (a) => {
        const x = buildAssignment((ctx.body ?? {}) as Record<string, unknown>, uuidv7());
        if (x instanceof Response) return x;
        if (a.assignments.some((y) => y.role === x.role && y.org_ref === x.org_ref && !profileOf(a).revoked.has(y.assignment_id))) return problem(409, "accounts.assignment_duplicate", "Tài khoản đã có vai trò này trên cùng Nút");
        a.assignments.push(x);
        log(a, { event: "assignment.granted", entity_ref: x.assignment_id, actor_ref: ADMIN_REF, changes: [{ field: "role", old: null, new: scopeText(x) }], reason: null });
        return json({ assignment: assignmentWire(a, x), seq, replayed: false, warnings: warningsOf(x) }, 201);
      }),
  },
  {
    method: "PUT",
    pattern: re("/v1/accounts/users/{id}/assignments/{id}"),
    handler: (ctx) =>
      withAccount(ctx, (a) => {
        const i = a.assignments.findIndex((y) => wireAid(y) === ctx.params[1]);
        if (i < 0) return problem(404, "accounts.assignment_not_found", "Không tìm thấy lần gán vai trò");
        const old = a.assignments[i]!;
        const x = buildAssignment({ ...(ctx.body as Record<string, unknown>), role: old.role }, old.assignment_id);
        if (x instanceof Response) return x;
        if (scopeText(x) === scopeText(old) && x.mgmt_level === old.mgmt_level) return problem(409, "accounts.assignment_state", "Không có thay đổi");
        a.assignments[i] = x;
        const changes = [];
        if (old.communes.map((c) => c.name).join(", ") !== x.communes.map((c) => c.name).join(", "))
          changes.push({ field: "Xã phụ trách", old: old.communes.map((c) => c.name).join(", ") || null, new: x.communes.map((c) => c.name).join(", ") || null });
        if (old.mgmt_level !== x.mgmt_level) changes.push({ field: "Cấp quản lý", old: old.mgmt_level, new: x.mgmt_level });
        if (old.departments.map((d) => d.code).join() !== x.departments.map((d) => d.code).join())
          changes.push({ field: "Phòng ban", old: old.departments.map((d) => d.name).join(", "), new: x.departments.map((d) => d.name).join(", ") });
        log(a, { event: "assignment.changed", entity_ref: wireAid(x), actor_ref: ADMIN_REF, changes, reason: null });
        return json({ assignment: assignmentWire(a, x), seq, replayed: false, warnings: warningsOf(x) });
      }),
  },
  {
    method: "POST",
    pattern: re("/v1/accounts/users/{id}/assignments/{id}/revoke"),
    handler: (ctx) =>
      withAccount(ctx, (a) => {
        const r = reasonOk(ctx.body, 10);
        if (!r) return errField("reason", "bắt buộc, ít nhất 10 ký tự");
        const x = a.assignments.find((y) => wireAid(y) === ctx.params[1]);
        if (!x) return problem(404, "accounts.assignment_not_found", "Không tìm thấy lần gán vai trò");
        const p = profileOf(a);
        if (p.revoked.has(x.assignment_id)) return problem(409, "accounts.assignment_state", "Lần gán đã thu hồi");
        p.revoked.set(x.assignment_id, { at: now(), by: ADMIN_REF });
        a.assignments = a.assignments.filter((y) => y !== x);
        log(a, { event: "assignment.revoked", entity_ref: wireAid(x), actor_ref: ADMIN_REF, changes: [{ field: "role", old: scopeText(x), new: null }], reason: r });
        return json({ assignment: { ...assignmentWire(a, x), active: false }, seq, replayed: false, warnings: [] });
      }),
  },
  {
    method: "PATCH",
    pattern: re("/v1/accounts/users/{id}/profile"),
    handler: (ctx) =>
      withAccount(ctx, (a) => {
        const b = (ctx.body ?? {}) as { full_name?: string; email?: string; phone?: string };
        const p = profileOf(a);
        const changed: string[] = [];
        if (b.full_name && b.full_name.trim() !== a.displayName) {
          a.displayName = b.full_name.trim();
          changed.push("full_name");
        }
        if (b.email && b.email.trim().toLowerCase() !== p.email) {
          const e = b.email.trim().toLowerCase();
          if (KN_ACCOUNTS.some((o) => o !== a && o.identifiers.includes(e))) return problem(409, "accounts.duplicate_identity", "SĐT hoặc email đã là của tài khoản khác");
          a.identifiers = [...a.identifiers.filter((i) => i !== p.email), e];
          p.email = e;
          changed.push("email");
        }
        if (b.phone !== undefined && normPhone(b.phone) !== (p.phone ?? "")) {
          const ph = normPhone(b.phone);
          if (ph && !/^0\d{9}$/.test(ph)) return errField("phone", "10 chữ số bắt đầu bằng 0");
          if (ph && KN_ACCOUNTS.some((o) => o !== a && o.identifiers.includes(ph))) return problem(409, "accounts.duplicate_identity", "SĐT hoặc email đã là của tài khoản khác");
          a.identifiers = [...a.identifiers.filter((i) => i !== p.phone), ...(ph ? [ph] : [])];
          p.phone = ph || null;
          changed.push("phone");
        }
        if (!changed.length) return problem(409, "accounts.state_conflict", "Không có thay đổi");
        log(a, { event: "account.profile_updated", entity_ref: null, actor_ref: ADMIN_REF, changes: changed.map((f) => ({ field: f, old: null, new: null })), reason: null });
        return json({ user: userWire(a), changed_fields: changed, seq, replayed: false });
      }),
  },
  {
    method: "GET",
    pattern: re("/v1/accounts/users/{id}/log"),
    handler: (ctx) =>
      withAccount(ctx, (a) => {
        profileOf(a);
        return json({ items: [...LOGS.get(a.id)!].reverse().map((l) => ({ ...l, actor_display: l.actor_ref === ADMIN_REF ? "admin.mekong" : null, recorded_at: l.at })), next_cursor: null });
      }),
  },
  ...(["lock", "unlock"] as const).map(
    (op): Route => ({
      method: "POST",
      pattern: re(`/v1/accounts/users/{id}/${op}`),
      match: knAdminContext,
      handler: (ctx) =>
        withAccount(ctx, (a) => {
          const r = reasonOk(ctx.body, 20);
          if (!r) return errField("reason", "bắt buộc, ít nhất 20 ký tự");
          const p = profileOf(a);
          // Như máy chủ: mở khóa cả khi enabled=true mà đang khóa do đăng nhập sai; không còn gì để mở → 409.
          const bruteForce = op === "unlock" && mockKnLoginLockedUntil(a.identifiers) !== null;
          if (p.enabled === (op === "unlock") && !bruteForce) return problem(409, "accounts.state_conflict", "Trạng thái tài khoản không đổi");
          if (op === "unlock") clearMockKnLoginLock(a.identifiers);
          const was = op === "lock" ? "Hoạt động" : p.enabled ? "Khóa do đăng nhập sai" : "Đang khóa";
          p.enabled = op === "unlock";
          log(a, { event: op === "lock" ? "account.locked" : "account.unlocked", entity_ref: null, actor_ref: ADMIN_REF, changes: [{ field: "Trạng thái", old: was, new: op === "lock" ? "Đang khóa" : "Hoạt động" }], reason: r });
          return json({ user_ref: userId(a), org_ref: userWire(a).org_ref, roles: ["kn_user"], enabled: p.enabled, lock_cause: p.enabled ? null : "manual" });
        }),
    }),
  ),
  {
    method: "POST",
    pattern: re("/v1/accounts/users/{id}/reset-totp"),
    match: knAdminContext,
    handler: (ctx) =>
      withAccount(ctx, (a) => {
        const r = reasonOk(ctx.body, 20);
        if (!r) return errField("reason", "bắt buộc, ít nhất 20 ký tự");
        const p = profileOf(a);
        if (!p.enabled) return problem(409, "accounts.state_conflict", "Tài khoản đang bị khóa");
        KN_TOTP_RESET.add(userId(a));
        log(a, { event: "account.totp_reset", entity_ref: null, actor_ref: ADMIN_REF, changes: [{ field: "TOTP", old: "Đã đăng ký", new: "Chờ đăng ký lại" }], reason: r });
        return json({ account: { user_ref: userId(a), org_ref: userWire(a).org_ref, roles: ["kn_user"], enabled: true, lock_cause: null }, seq: 1, replayed: false });
      }),
  },
  {
    method: "POST",
    pattern: re("/v1/accounts/users/{id}/resend-actions"),
    match: knAdminContext,
    handler: (ctx) =>
      withAccount(ctx, (a) => {
        log(a, { event: "account.activation_sent", entity_ref: null, actor_ref: ADMIN_REF, changes: [], reason: null });
        return json({ account: { user_ref: userId(a), org_ref: userWire(a).org_ref, roles: ["kn_user"], enabled: profileOf(a).enabled, lock_cause: null }, activation_email_sent: true, replayed: false });
      }),
  },
  { method: "POST", pattern: re("/v1/accounts/support-sessions"), handler: startSupport },
  {
    method: "POST",
    pattern: re("/v1/accounts/support-sessions/{id}/end"),
    handler: (ctx) => {
      const s = SUPPORT_OPEN.get(ctx.params[0] ?? "");
      if (!s) return problem(404, "accounts.support_session_not_found", "Không tìm thấy phiên hỗ trợ");
      if (s.ended) return problem(409, "accounts.support_session_ended", "Phiên hỗ trợ đã kết thúc");
      s.ended = now();
      dropSupportContexts(ctx.params[0]!);
      return json({ support_session_id: ctx.params[0], ended_at: s.ended });
    },
  },
  {
    method: "GET",
    pattern: re("/v1/accounts/support-log"),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return deny();
      const sid = ctx.url.searchParams.get("support_session_id");
      return json({ items: SUPPORT_LOG.filter((l) => !sid || l.support_session_id === sid).reverse(), next_cursor: null });
    },
  },
  // --- pf/party: phòng ban, khu vực, cây tổ chức ---
  { method: "GET", pattern: re("/v1/departments"), handler: (ctx) => (isKnAdmin(ctx) ? json({ org_ref: AG_REF, items: KN_DEPARTMENTS.map(deptWire) }) : deny()) },
  {
    method: "POST",
    pattern: re("/v1/departments/{id}/status"),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return deny();
      const i = KN_DEPARTMENTS.findIndex((d) => deptId(d.code) === ctx.params[0]);
      const d = KN_DEPARTMENTS[i];
      if (!d) return problem(404, "party.department_not_found", "Không tìm thấy phòng ban");
      const b = (ctx.body ?? {}) as { status?: DepartmentRef["status"]; reason?: string };
      if (!b.status || !(b.status in STATUS_LABEL)) return errField("status", "active | not_open | suspended");
      if (b.status === d.status) return problem(409, "party.department_state", "Phòng ban đã ở trạng thái yêu cầu");
      const r = String(b.reason ?? "").trim();
      if (b.status === "suspended" && [...r].length < 10) return errField("reason", "bắt buộc khi ngưng, ít nhất 10 ký tự");
      const old = d.status;
      d.status = b.status;
      DEPT_LOG.set(d.code, [...(DEPT_LOG.get(d.code) ?? []), { seq: ++seq, event: "department.status_changed", actor_ref: ADMIN_REF, at: now(), changes: [{ field: "status", old: STATUS_LABEL[old], new: STATUS_LABEL[d.status] }], reason: r || null }]);
      const w = deptWire(d, i);
      const warnings = b.status !== "active" && (w.account_count ?? 0) > 0 ? [`${w.account_count} tài khoản thuộc phòng ban — dữ liệu cũ giữ nguyên, không chọn được ở bước 2 đăng nhập`] : [];
      return json({ department: w, warnings }, 201);
    },
  },
  {
    method: "GET",
    pattern: re("/v1/departments/{id}/log"),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return deny();
      const d = KN_DEPARTMENTS.find((x) => deptId(x.code) === ctx.params[0]);
      if (!d) return problem(404, "party.department_not_found", "Không tìm thấy phòng ban");
      const items = [...(DEPT_LOG.get(d.code) ?? []), { seq: 0, event: "department.created", actor_ref: ADMIN_REF, at: T0, changes: [{ field: "code", old: null, new: d.code }], reason: null }]
        .sort((x, y) => y.seq - x.seq)
        .map((l) => ({ ...l, actor_display: "admin.mekong", recorded_at: l.at }));
      return json({ items, next_cursor: null });
    },
  },
  {
    method: "GET",
    pattern: re("/v1/regions"),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return deny();
      const items = AG_REGIONS.map((r) => ({ id: stableUuid(`region-${r.code}`), province_code: AG_PROVINCE, code: r.code, name: r.name, sort_order: r.sort, active: true, deactivation_reason: null, commune_count: AG_COMMUNES.filter((c) => c.region === r.code).length, updated_by: ADMIN_REF, updated_at: T0 }));
      return json({ province_code: AG_PROVINCE, items, assigned_communes: items.reduce((s, x) => s + x.commune_count, 0) });
    },
  },
  { method: "GET", pattern: re("/v1/orgs/tree"), handler: (ctx) => (isKnAdmin(ctx) ? json(orgTree()) : deny()) },
  {
    method: "POST",
    pattern: re("/v1/orgs/{id}/region"),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return deny();
      const c = AG_COMMUNES.find((x) => communeRef(x.code) === ctx.params[0]) as { code: string; region: string } | undefined;
      if (!c) return problem(422, "party.region_node_invalid", "Chỉ gán khu vực cho Nút xã đang hoạt động");
      const b = (ctx.body ?? {}) as { region_code?: string | null; reason?: string };
      const target = b.region_code ?? "";
      if (target && !AG_REGIONS.some((r) => r.code === target)) return problem(422, "party.region_node_invalid", "Khu vực không đang dùng / không thuộc tỉnh");
      if (target === c.region) return problem(409, "party.region_state", "Xã đã thuộc khu vực này");
      if (c.region && [...String(b.reason ?? "").trim()].length < 10) return errField("reason", "đổi/bỏ khu vực cần lý do ≥ 10 ký tự");
      const action = !c.region ? "assigned" : target ? "changed" : "cleared";
      c.region = target;
      return json({ org_ref: ctx.params[0], commune_code: c.code, province_code: AG_PROVINCE, region_code: target || null, action }, 201);
    },
  },
  {
    method: "POST",
    pattern: re("/v1/orgs/{id}/move"),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return deny();
      const c = AG_COMMUNES.find((x) => communeRef(x.code) === ctx.params[0]);
      if (!c) return problem(422, "party.invalid_hierarchy", "Không di chuyển Nút cấp 1, cấp 2");
      const b = (ctx.body ?? {}) as { new_parent_ref?: string; reason?: string };
      if ([...String(b.reason ?? "").trim()].length < 10) return errField("reason", "bắt buộc, ≥ 10 ký tự");
      if (b.new_parent_ref !== AG_REF && b.new_parent_ref !== OTHER_PROVINCE_REF) return errField("new_parent_ref", "Nút cha phải là TTKN tỉnh");
      const moved = b.new_parent_ref === OTHER_PROVINCE_REF;
      if (moved === MOVED.has(c.code)) return problem(409, "party.org_state_conflict", "Nút xã đã thuộc Nút tỉnh này");
      if (moved) MOVED.add(c.code);
      else MOVED.delete(c.code);
      return json({ org: { ref: ctx.params[0], kind: "extension_team", name: c.name, parent_ref: b.new_parent_ref, admin_unit_code: c.code } });
    },
  },
  {
    method: "GET",
    pattern: re("/v1/orgs/{id}/impact"),
    handler: (ctx) => {
      if (!isKnAdmin(ctx)) return deny();
      const c = AG_COMMUNES.find((x) => communeRef(x.code) === ctx.params[0]);
      const isProvince = ctx.params[0] === AG_REF || ctx.params[0] === NATIONAL_REF;
      if (!c && !isProvince) return problem(404, "party.org_not_found", "Không có tổ chức");
      const codes = c ? [c.code] : AG_COMMUNES.map((x) => x.code);
      const accounts = KN_ACCOUNTS.filter((a) => a.assignments.some((x) => x.communes.some((y) => codes.includes(y.code)))).length;
      return json({
        org: { ref: ctx.params[0], name: c?.name ?? "TTKN tỉnh An Giang" },
        communes: codes,
        rows: [
          { kind: "accounts", label: "Tài khoản", count: accounts, available: true },
          { kind: "cooperatives", label: "HTX", count: c ? 1 : AG_COMMUNES.length, available: true },
          { kind: "plots", label: "Thửa ruộng", count: null, available: false },
          { kind: "open_periods", label: "Kỳ chưa chốt", count: 2, available: true },
        ],
        warnings: c ? ["Xã có kỳ báo cáo chưa chốt — số liệu đã chốt giữ theo cấu trúc tại thời điểm chốt"] : [],
        movable: !!c,
      });
    },
  },
];

/** Giả lập POST /v1/accounts/kn-users/import (W11.02): CSV, mỗi dòng một vai trò, cùng Email = một tài khoản (gọi lại route tạo tài khoản). */
const MOCK_ROLE: Record<string, string> = { kn_tw: "kn_tw", "cấp quốc gia": "kn_tw", kn_tinh: "kn_tinh", "cấp tỉnh": "kn_tinh", kn_xa: "kn_xa", "cấp xã": "kn_xa" };
function importKnUsers(ctx: Ctx): Response {
  if (!isKnAdmin(ctx)) return deny();
  if (typeof ctx.body !== "string") return problem(422, "accounts.import_invalid", "Tệp nhập không đúng mẫu quy định", { detail: "Giả lập chỉ đọc CSV UTF-8" });
  const lines = ctx.body.replace(/^\ufeff/, "").split(/\r?\n/).filter((l) => l.trim());
  const head = (lines[0] ?? "").split(",").map((h) => h.trim().toLowerCase());
  const col = (names: string[]) => head.findIndex((h) => names.includes(h));
  const ci = { name: col(["họ tên", "ho_ten"]), email: col(["email"]), phone: col(["sđt", "so_dien_thoai"]), role: col(["vai trò", "vai_tro"]), org: col(["mã nút", "ma_nut"]), dept: col(["phòng ban", "phong_ban"]), mgmt: col(["cấp quản lý", "cap_quan_ly"]), communes: col(["xã phụ trách", "xa_phu_trach"]) };
  if (ci.name < 0 || ci.email < 0 || ci.role < 0 || ci.org < 0) return problem(422, "accounts.import_invalid", "Tệp nhập không đúng mẫu quy định", { detail: "Tệp thiếu cột bắt buộc: Họ tên, Email, Vai trò, Mã Nút" });
  const create = knAdminRoutes.find((r) => r.method === "POST" && r.pattern.source === re("/v1/accounts/kn-users").source)!;
  const groups = new Map<string, { lines: number[]; body: { full_name: string; email: string; phone?: string; assignments: Record<string, unknown>[] }; bad?: string }>();
  lines.slice(1).forEach((l, i) => {
    const c = l.split(",").map((x) => x.trim());
    const email = (c[ci.email] ?? "").toLowerCase();
    const key = email || `#${i}`;
    const g = groups.get(key) ?? { lines: [], body: { full_name: c[ci.name] ?? "", email, ...(c[ci.phone] ? { phone: c[ci.phone] } : {}), assignments: [] } };
    groups.set(key, g);
    g.lines.push(i + 2);
    const role = MOCK_ROLE[(c[ci.role] ?? "").toLowerCase()];
    if (!email) g.bad = "Thiếu email";
    else if (!role) g.bad = "Vai trò không hợp lệ (Cấp Quốc gia, Cấp Tỉnh, Cấp Xã)";
    const org = c[ci.org] === "TTKNQG" ? NATIONAL_REF : c[ci.org] === "TTKN-AG" ? AG_REF : c[ci.org];
    const list = (v?: string) => (v ?? "").split(";").map((x) => x.trim()).filter(Boolean);
    g.body.assignments.push({ role, org_ref: org, department_codes: list(c[ci.dept]), commune_codes: list(c[ci.communes]), mgmt_level: c[ci.mgmt] ? "chuyen_vien" : null });
  });
  const rows: { row: number; status: "created" | "rejected"; reason: string | null; user_id: string | null; activation_email_sent: boolean }[] = [];
  let accounts = 0;
  for (const g of groups.values()) {
    let reason = g.bad ?? null;
    let uid: string | null = null;
    if (!reason) {
      const r = create.handler({ ...ctx, body: g.body });
      if (r.status === 201) {
        accounts++;
        const acct = accountByEmail(g.body.email);
        uid = acct ? userId(acct) : null;
      } else reason = problemOf(r)?.title ?? "Không tạo được tài khoản";
    }
    for (const row of g.lines) rows.push(reason ? { row, status: "rejected", reason, user_id: null, activation_email_sent: false } : { row, status: "created", reason: null, user_id: uid, activation_email_sent: true });
  }
  rows.sort((x, y) => x.row - y.row);
  const created = rows.filter((r) => r.status === "created").length;
  return json({ accounts_created: accounts, created, rejected: rows.length - created, rows });
}

function accountByEmail(email: string): MockKnAccount | undefined {
  return KN_ACCOUNTS.find((a) => a.identifiers.includes(email));
}

/** Cho kiểm thử: xóa nhật ký và hồ sơ giả lập. */
export function resetKnAdmin(): void {
  MOVED.clear();
  ATTRS.clear();
  UNITS.length = 0;
  DELEGATIONS.length = 0;
  PROFILES.clear();
  LOGS.clear();
  SUPPORT_LOG.length = 0;
  DEPT_LOG.clear();
  SUPPORT_OPEN.clear();
}

// BEGIN GENERATED MOCK ROUTES
import type { RouteRegistration } from "./registry";
import { knAppRoutes as route0 } from "./kn";
import { cntsAggregateRoutes as route2 } from "./kn-cnts-aggregate";
import { cntsAlertRoutes as route3 } from "./kn-cnts-alert";
import { cntsBm11Routes as route4 } from "./kn-cnts-bm11";
import { cntsCalRoutes as route5 } from "./kn-cnts-cal";
import { cntsCampaignRoutes as route6 } from "./kn-cnts-campaign";
import { cntsCatalogAppRoutes as route7 } from "./kn-cnts-catalogs";
import { cntsCatalogAdminRoutes as route8 } from "./kn-cnts-catalogs";
import { cntsDiaryRoutes as route9 } from "./kn-cnts-diary";
import { cntsKnqgRoutes as route10 } from "./kn-cnts-knqg";
import { cntsMapRoutes as route11 } from "./kn-cnts-map";
import { cntsModelRoutes as route12 } from "./kn-cnts-model";
import { cntsOutbreakRoutes as route13 } from "./kn-cnts-outbreak";
import { cntsPeriodRoutes as route14 } from "./kn-cnts-periods";
import { knCollectRoutes as route15 } from "./kn-collect";
import { contactRoutes as route16 } from "./kn-contact";
import { deptHomeRoutes as route17 } from "./kn-dept-home";
import { hrmRoutes as route18 } from "./kn-hrm";
import { hrmDocRoutes as route19 } from "./kn-hrm-docs";
import { hrmPlanRoutes as route20 } from "./kn-hrm-plans";
import { hrmPromotionRoutes as route21 } from "./kn-hrm-promotion";
import { hrmReportRoutes as route22 } from "./kn-hrm-reports";
import { hrmStaffRoutes as route23 } from "./kn-hrm-staff";
import { khtcRoutes as route24 } from "./kn-khtc";
import { khtcFormRoutes as route25 } from "./kn-khtc-forms";
import { khtcPeriodRoutes as route26 } from "./kn-khtc-periods";
import { khtcReportRoutes as route27 } from "./kn-khtc-reports";
import { khtcSourceRoutes as route28 } from "./kn-khtc-sources";
import { khtcWordRoutes as route29 } from "./kn-khtc-words";
import { appRoutes28 as route30 } from "./kn-legacy-extension";
import { appRoutes29 as route31 } from "./kn-legacy-extension";
import { priceConfigRoutes as route32 } from "./kn-price";
import { priceBoardRoutes as route33 } from "./kn-price-boards";
import { priceItemRoutes as route34 } from "./kn-price-items";
import { pricePointRoutes as route35 } from "./kn-price-points";
import { priceReportRoutes as route36 } from "./kn-price-reports";
import { priceSurveyRoutes as route37 } from "./kn-price-surveys";
import { priceTicketRoutes as route38 } from "./kn-price-tickets";
import { reportCycleAppRoutes as route39 } from "./kn-reports/routes";
import { workScheduleAppRoutes as route40 } from "./kn-schedules";

export function registeredKnRoutes(): RouteRegistration[] {
  return [
  { surface: "app", order: 2, routes: route0 },
  { surface: "admin", order: 0, routes: knAdminRoutes },
  { surface: "app", order: 15, routes: route2 },
  { surface: "app", order: 21, routes: route3 },
  { surface: "app", order: 1145, routes: route4 },
  { surface: "app", order: 17, routes: route5 },
  { surface: "app", order: 61, routes: route6 },
  { surface: "app", order: 10, routes: route7 },
  { surface: "admin", order: 2, routes: route8 },
  { surface: "app", order: 1048, routes: route9 },
  { surface: "app", order: 1153, routes: route10 },
  { surface: "app", order: 1049, routes: route11 },
  { surface: "app", order: 1047, routes: route12 },
  { surface: "app", order: 18, routes: route13 },
  { surface: "app", order: 14, routes: route14 },
  { surface: "app", order: 7, routes: route15 },
  { surface: "app", order: 23, routes: route16 },
  { surface: "app", order: 58, routes: route17 },
  { surface: "app", order: 11, routes: route18 },
  { surface: "app", order: 25, routes: route19 },
  { surface: "app", order: 60, routes: route20 },
  { surface: "app", order: 125, routes: route21 },
  { surface: "app", order: 24, routes: route22 },
  { surface: "app", order: 22, routes: route23 },
  { surface: "app", order: 12, routes: route24 },
  { surface: "app", order: 20, routes: route25 },
  { surface: "app", order: 16, routes: route26 },
  { surface: "app", order: 1000, routes: route27 },
  { surface: "app", order: 19, routes: route28 },
  { surface: "app", order: 1002, routes: route29 },
  { surface: "app", order: 28, routes: route30 },
  { surface: "app", order: 29, routes: route31 },
  { surface: "app", order: 5, routes: route32 },
  { surface: "app", order: 26, routes: route33 },
  { surface: "app", order: 8, routes: route34 },
  { surface: "app", order: 9, routes: route35 },
  { surface: "app", order: 27, routes: route36 },
  { surface: "app", order: 78, routes: route37 },
  { surface: "app", order: 13, routes: route38 },
  { surface: "app", order: 6, routes: route39 },
  { surface: "app", order: 4, routes: route40 },
  ];
}
// END GENERATED MOCK ROUTES
