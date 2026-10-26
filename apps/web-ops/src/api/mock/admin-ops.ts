// GIẢ LẬP các route Admin API (MFA bắt buộc — kiểm ở mockFetch) của đợt hoàn thiện G1, theo contracts/openapi/admin.yaml:
// danh sách danh mục (G-06), sổ tham số (G-07), liệt kê/xuất/nhập cây tổ chức (G-08, US-ADM-08), thiết bị, đối soát xung đột
// (US-SYNC-03), chạy dự báo CGH (FN-09), đồng bộ ngay, tài khoản cán bộ (pf/accounts), mẫu + lịch báo cáo (pf/reports),
// nhập ranh giới thửa (GeoJSON/KML/Shapefile .zip). Tài khoản là DỮ LIỆU TỔNG HỢP, không phải người thật.
import { uuidv7 } from "../idempotency";
import type { AccountsEvent, AccountsUser, CatalogName, Org, ReportSchedule, ReportTemplate } from "../types";
import { CATALOG_META, CATALOGS, COMMUNES, COOPERATIVES, ORGS, PARAM_REGISTRY, PARAM_VERSIONS } from "./data";
import { SYNC_CONFLICTS_STORE, slaStatusOf } from "./conflicts";
import { hasRole, isZip, iso, json, nowIso, omit, opaque, paginate, problem, problemOf, reasonOf, scopeUnits, validation, type Route } from "./http";
import { computeForecast, FORECASTS, latestTemplate, MOCK_DEVICES, nudge, REPORT_TEMPLATES, templateKeys } from "./ops";
import type { MockPersona } from "./personas";

const admin = (p: MockPersona, ...roles: string[]) => hasRole(p, "platform_admin", ...roles);

/** Tách CSV đơn giản (có ngoặc kép). Đủ cho tệp mẫu; backend dùng bộ đọc CSV đầy đủ. */
export function parseCsv(text: string): string[][] {
  const rows: string[][] = [];
  for (const line of text.replace(/^﻿/, "").split(/\r?\n/)) {
    if (!line.trim()) continue;
    const cells: string[] = [];
    let cur = "";
    let quoted = false;
    for (let i = 0; i < line.length; i++) {
      const ch = line[i];
      if (quoted) {
        if (ch === '"' && line[i + 1] === '"') {
          cur += '"';
          i++;
        } else if (ch === '"') quoted = false;
        else cur += ch;
      } else if (ch === '"') quoted = true;
      else if (ch === ",") {
        cells.push(cur.trim());
        cur = "";
      } else cur += ch;
    }
    cells.push(cur.trim());
    rows.push(cells);
  }
  return rows;
}

/** Tên cột không dấu, chữ thường, khoảng trắng → "_" ("Họ tên" = ho_ten). */
export function headerKey(h: string): string {
  return h
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/đ/g, "d")
    .replace(/Đ/g, "D")
    .trim()
    .toLowerCase()
    .replace(/\s+/g, "_");
}

// --------------------------------------------------------------------------
// Phạm vi tổ chức cho quản trị (tỉnh: tổ chức trong tỉnh; toàn quốc: tất cả)
// --------------------------------------------------------------------------
function orgUnit(o: Org): string | null {
  return o.admin_unit_code;
}

function orgInScope(p: MockPersona, o: Org | undefined): boolean {
  if (!o) return false;
  const s = scopeUnits(p);
  if (!s) return true;
  const u = orgUnit(o);
  return !!u && (s.has(u) || s.has(u.slice(0, 2)));
}

// --------------------------------------------------------------------------
// pf/accounts
// --------------------------------------------------------------------------
const MANAGED_ROLES = [
  "kn_officer", "kn_province_admin", "kn_national_admin", "coop_manager", "cgh_viewer", "cgh_admin", "masterdata_admin", "registry_admin",
  "param_setter", "param_approver", "reports_admin", "sync_conflict_resolver", "device_admin", "account_admin", "account_admin_province",
  "vault_eraser", "platform_admin",
];
const MFA_ROLES = ["platform_admin", "account_admin", "account_admin_province", "param_setter", "param_approver", "masterdata_admin", "registry_admin", "reports_admin", "cgh_admin", "vault_eraser"];
const ROLE_ORG_KINDS: Record<string, string[]> = {
  kn_officer: ["extension_team"],
  kn_province_admin: ["ttkn_province"],
  kn_national_admin: ["ttkn_national"],
  coop_manager: ["cooperative"],
  cgh_viewer: ["dcrd"],
  cgh_admin: ["dcrd", "mkg"],
  account_admin_province: ["ttkn_province"],
};

function grantable(p: MockPersona): string[] {
  if (hasRole(p, "platform_admin")) return MANAGED_ROLES;
  if (hasRole(p, "account_admin")) return MANAGED_ROLES.filter((r) => !["account_admin", "param_approver", "vault_eraser", "platform_admin"].includes(r));
  if (hasRole(p, "account_admin_province")) return ["kn_officer", "coop_manager"];
  return [];
}

interface MockAccount {
  user: AccountsUser;
  events: AccountsEvent[];
}

const GIVEN = ["An", "Bình", "Chi", "Dũng", "Giang", "Hạnh", "Khoa", "Lan", "Minh", "Nga", "Phúc", "Quân"];
const FAMILY = ["Nguyễn", "Trần", "Lê", "Phạm", "Võ", "Huỳnh"];

function accountSnapshot(u: AccountsUser): AccountsEvent["account"] {
  return { user_ref: u.id, org_ref: u.org_ref ?? "", roles: u.roles ?? [], enabled: u.enabled, lock_cause: u.lock_cause };
}

function pushEvent(a: MockAccount, action: AccountsEvent["action"], actor: string, reason: string | null, details?: Record<string, unknown>) {
  const now = nowIso();
  a.events.push({ seq: a.events.length + 1, action, event_time: now, recorded_at: now, actor_ref: actor, reason, account: accountSnapshot(a.user), details: details ? opaque(details) : undefined });
  return a.events.length;
}

const accountOrgs: Org[] = [
  ...ORGS.filter((o) => o.kind === "ttkn_province").slice(0, 3),
  ...ORGS.filter((o) => o.kind === "extension_team").slice(0, 6),
  ...COOPERATIVES.slice(0, 3).map((c) => ORGS.find((o) => o.ref === c.api.ref)!),
];

export const ACCOUNTS: MockAccount[] = accountOrgs.map((org, i) => {
  const role = org.kind === "ttkn_province" ? "kn_province_admin" : org.kind === "cooperative" ? "coop_manager" : "kn_officer";
  const first = GIVEN[i % GIVEN.length]!;
  const last = `${FAMILY[i % FAMILY.length]} Văn`;
  const locked = i === 4;
  const pending = i === 2 || i === 7;
  const user: AccountsUser = {
    id: `01927b3e-acc0-7000-8000-${String(i + 1).padStart(12, "0")}`,
    username: `canbo${String(i + 1).padStart(2, "0")}`,
    email: `canbo${String(i + 1).padStart(2, "0")}@example.test`,
    first_name: first,
    last_name: last,
    phone: `0999${String(100000 + i * 7).padStart(6, "0")}`,
    enabled: !locked,
    email_verified: !pending,
    org_ref: org.ref,
    roles: [role],
    lock_cause: locked ? "manual" : null,
    required_actions: pending ? ["VERIFY_EMAIL", "UPDATE_PASSWORD"] : [],
    created_at: iso(120 - i * 5),
    tracked: true,
  };
  const a: MockAccount = { user, events: [] };
  a.events.push({ seq: 1, action: "created", event_time: user.created_at!, recorded_at: user.created_at!, actor_ref: "mock-admin-setter", reason: null, account: accountSnapshot(user) });
  a.events.push({ seq: 2, action: "actions_sent", event_time: user.created_at!, recorded_at: user.created_at!, actor_ref: "mock-admin-setter", reason: null, account: accountSnapshot(user), details: opaque({ actions: ["VERIFY_EMAIL", "UPDATE_PASSWORD"] }) });
  if (locked) a.events.push({ seq: 3, action: "locked", event_time: iso(3), recorded_at: iso(3), actor_ref: "mock-admin-setter", reason: "Cán bộ nghỉ phép dài hạn theo quyết định của đơn vị", account: accountSnapshot(user) });
  return a;
});

// ADMIN-UNLOCK (#42): khóa đăng nhập do sai mật khẩu (Keycloak attack-detection) — tách khỏi enabled. Từ P0-VPS-OPS
// (#131) là khóa TẠM 15 phút rồi tự mở; Admin mở sớm qua /unlock. Giá trị = mốc tự mở (ms). Máy chủ chỉ trả
// login_locked (+ login_locked_until) ở GET chi tiết; danh sách/tạo bỏ field.
export const LOGIN_LOCK_MS = 15 * 60_000;
const LOGIN_LOCKS = new Map<string, number>();

/** Giả lập Keycloak khóa tạm sau 5 lần sai; `until` mặc định = bây giờ + 15 phút (truyền mốc đã qua = khóa đã tự hết). */
export function lockAccountByBruteForce(id: string, until = Date.now() + LOGIN_LOCK_MS): void {
  LOGIN_LOCKS.set(id, until);
}

/** Cho kiểm thử: bỏ mọi khóa đăng nhập sai (kể cả tài khoản mẫu). */
export function resetAccountLoginLocks(): void {
  LOGIN_LOCKS.clear();
}

function loginLockedUntil(id: string): number | null {
  const until = LOGIN_LOCKS.get(id);
  return until !== undefined && until > Date.now() ? until : null;
}

// Dữ liệu mẫu cho demo: một cán bộ tổ khuyến nông đang hoạt động nhưng vừa bị khóa tạm do nhập sai mật khẩu 5 lần.
{
  const sample = ACCOUNTS.filter((a) => a.user.enabled && a.user.required_actions.length === 0)[3];
  if (sample) lockAccountByBruteForce(sample.user.id);
}

function accountDetail(u: AccountsUser): AccountsUser {
  const until = loginLockedUntil(u.id);
  return { ...u, mfa: mfaOf(u), login_locked: until !== null, ...(until !== null ? { login_locked_until: new Date(until).toISOString() } : {}) };
}

function accountVisible(p: MockPersona, a: MockAccount): boolean {
  return orgInScope(p, ORGS.find((o) => o.ref === a.user.org_ref));
}

function findAccount(p: MockPersona, id: string | undefined): MockAccount | Response {
  const a = ACCOUNTS.find((x) => x.user.id === id);
  if (!a) return problem(404, "accounts.user_not_found", "Not found");
  if (!accountVisible(p, a)) return problem(403, "accounts.out_of_scope", "Out of scope");
  return a;
}

function checkRoles(p: MockPersona, roles: string[], org: Org): Response | null {
  const g = grantable(p);
  if (roles.some((r) => !MANAGED_ROLES.includes(r) || !g.includes(r))) return problem(403, "accounts.role_not_grantable", "Role not grantable");
  for (const r of roles) {
    const kinds = ROLE_ORG_KINDS[r];
    if (kinds && !kinds.includes(org.kind)) {
      return validation([{ field: "org_ref", reason: r === "coop_manager" ? "Vui lòng chọn HTX liên kết." : `Vai ${r} không dùng cho loại tổ chức này.` }]);
    }
  }
  return null;
}

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function normPhone(v: string): string {
  const s = v.replace(/\s+/g, "");
  return s.startsWith("+84") ? `0${s.slice(3)}` : s;
}

function createAccount(p: MockPersona, input: Record<string, unknown>): { user?: AccountsUser; error?: Response; reason?: string } {
  const email = typeof input.email === "string" ? input.email.trim() : "";
  const orgRef = typeof input.org_ref === "string" ? input.org_ref : "";
  const roles = Array.isArray(input.roles) ? input.roles.filter((r): r is string => typeof r === "string") : [];
  const phone = typeof input.phone === "string" && input.phone.trim() ? normPhone(input.phone) : null;
  const full = typeof input.full_name === "string" ? input.full_name.trim() : "";
  const errors: { field: string; reason: string }[] = [];
  if (!email) errors.push({ field: "email", reason: "Thiếu Email" });
  else if (!EMAIL.test(email)) errors.push({ field: "email", reason: "Sai định dạng Email" });
  if (roles.length === 0) errors.push({ field: "roles", reason: "Chọn ít nhất một vai" });
  if (phone && !/^0\d{9}$/.test(phone)) errors.push({ field: "phone", reason: "SĐT gồm 10 chữ số bắt đầu bằng 0" });
  if (errors.length) return { error: validation(errors), reason: errors[0]!.reason };
  const org = ORGS.find((o) => o.ref === orgRef);
  if (!org) return { error: problem(422, "accounts.org_invalid", "Org invalid"), reason: "Không có tổ chức này" };
  if (!orgInScope(p, org)) return { error: problem(403, "accounts.out_of_scope", "Out of scope"), reason: "Tổ chức ngoài phạm vi phụ trách" };
  if (!org.active) return { error: problem(409, "accounts.org_inactive", "Org inactive"), reason: "Tổ chức đang vô hiệu hóa" };
  const bad = checkRoles(p, roles, org);
  if (bad) return { error: bad, reason: bad.status === 403 ? "Vai trò vượt quyền cấp của bạn" : "Sai định dạng vai trò" };
  const username = typeof input.username === "string" && input.username.trim() ? input.username.trim() : email;
  if (ACCOUNTS.some((a) => a.user.email.toLowerCase() === email.toLowerCase() || a.user.username === username)) {
    return { error: problem(409, "accounts.user_exists", "User exists"), reason: "Tên đăng nhập hoặc Email đã tồn tại" };
  }
  if (phone && ACCOUNTS.some((a) => a.user.phone === phone)) return { error: problem(409, "accounts.phone_taken", "Phone taken"), reason: "SĐT đã tồn tại" };
  const parts = full.split(/\s+/).filter(Boolean);
  const user: AccountsUser = {
    id: uuidv7(),
    username,
    email,
    first_name: typeof input.first_name === "string" && input.first_name ? input.first_name : (parts.pop() ?? ""),
    last_name: typeof input.last_name === "string" && input.last_name ? input.last_name : parts.join(" "),
    phone,
    enabled: true,
    email_verified: false,
    org_ref: org.ref,
    roles,
    lock_cause: null,
    required_actions: ["VERIFY_EMAIL", "UPDATE_PASSWORD", ...(roles.some((r) => MFA_ROLES.includes(r)) ? ["CONFIGURE_TOTP"] : [])],
    created_at: nowIso(),
    tracked: true,
  };
  const a: MockAccount = { user, events: [] };
  pushEvent(a, "created", p.me.actor_ref, null);
  pushEvent(a, "actions_sent", p.me.actor_ref, null, { actions: user.required_actions });
  ACCOUNTS.unshift(a);
  return { user };
}

function importAccounts(p: MockPersona, body: unknown, request: Request): Response {
  const type = request.headers.get("Content-Type") ?? "";
  if (isZip(body)) {
    // Giả lập không giải nén .xlsx: trả kết quả minh hoạ cho trang tính đầu (1 dòng nhận, 1 dòng loại).
    const org = ORGS.find((o) => o.kind === "extension_team" && orgInScope(p, o))!;
    const res = createAccount(p, { email: `nhap.xlsx.${Date.now()}@example.test`, full_name: "Cán bộ Nhập Tệp", org_ref: org.ref, roles: ["kn_officer"] });
    return json({
      created: res.user ? 1 : 0,
      rejected: res.user ? 1 : 2,
      rows: [
        { row: 2, status: res.user ? "created" : "rejected", reason: res.user ? null : (res.reason ?? null), user_id: res.user?.id ?? null, activation_email_sent: !!res.user },
        { row: 3, status: "rejected", reason: "Sai định dạng Email", user_id: null, activation_email_sent: false },
      ],
    });
  }
  if (!type.startsWith("text/csv") || typeof body !== "string") return problem(422, "accounts.import_invalid", "Expected CSV or XLSX");
  const rows = parseCsv(body);
  const header = (rows[0] ?? []).map(headerKey);
  const col = (...names: string[]) => header.findIndex((h) => names.includes(h));
  const iEmail = col("email");
  const iName = col("full_name", "ho_ten");
  const iOrg = col("org_ref", "ma_to_chuc");
  const iRoles = col("roles", "vai_tro");
  const iPhone = col("phone", "so_dien_thoai", "sdt");
  if (iEmail < 0 || iName < 0 || iOrg < 0 || iRoles < 0) return problem(422, "accounts.import_invalid", "Missing required columns");
  const data = rows.slice(1);
  if (data.length === 0 || data.length > 200) return problem(422, "accounts.import_invalid", "Row count out of range");
  const seen = new Map<string, number>();
  const out = data.map((r, i) => {
    const row = i + 2;
    const email = (r[iEmail] ?? "").trim().toLowerCase();
    const prev = email ? seen.get(email) : undefined;
    if (prev) return { row, status: "rejected" as const, reason: `Trùng với dòng ${prev} trong tệp`, user_id: null, activation_email_sent: false };
    if (email) seen.set(email, row);
    const roles = (r[iRoles] ?? "").split(/[,;|]/).map((x) => x.trim()).filter(Boolean);
    if (roles.length === 0 || roles.some((x) => !/^[a-z_]+$/.test(x))) return { row, status: "rejected" as const, reason: "Sai định dạng vai trò", user_id: null, activation_email_sent: false };
    const res = createAccount(p, { email: r[iEmail] ?? "", full_name: r[iName] ?? "", org_ref: r[iOrg] ?? "", roles, phone: iPhone >= 0 ? r[iPhone] : undefined });
    if (!res.user) return { row, status: "rejected" as const, reason: res.reason ?? "Không hợp lệ", user_id: null, activation_email_sent: false };
    return { row, status: "created" as const, reason: null, user_id: res.user.id, activation_email_sent: true };
  });
  return json({ created: out.filter((x) => x.status === "created").length, rejected: out.filter((x) => x.status === "rejected").length, rows: out });
}

function lockUnlock(p: MockPersona, id: string | undefined, body: unknown, lock: boolean): Response {
  const found = findAccount(p, id);
  if (found instanceof Response) return found;
  const reason = reasonOf(body);
  if (reason.length < 20) return validation([{ field: "reason", reason: "Lý do tối thiểu 20 ký tự" }]);
  if (found.user.id === p.me.actor_ref) return problem(403, "accounts.self_action", "Self action");
  const bad = (found.user.roles ?? []).find((r) => !grantable(p).includes(r));
  if (bad) return problem(403, "accounts.role_not_grantable", "Role not grantable");
  // Như máy chủ: mở khóa khi bị Admin khóa HOẶC đang khóa do đăng nhập sai (enabled vẫn true); không còn gì → 409.
  const bruteForce = loginLockedUntil(found.user.id) !== null;
  if (lock ? !found.user.enabled : found.user.enabled && !bruteForce) return problem(409, "accounts.state_conflict", "State conflict");
  if (!lock && !ORGS.find((o) => o.ref === found.user.org_ref)?.active) return problem(409, "accounts.org_inactive", "Org inactive");
  const prevCause = found.user.lock_cause ?? (!lock && bruteForce ? "brute_force" : null);
  if (!lock) LOGIN_LOCKS.delete(found.user.id);
  found.user.enabled = !lock;
  found.user.lock_cause = lock ? "manual" : null;
  const seq = pushEvent(found, lock ? "locked" : "unlocked", p.me.actor_ref, reason, lock ? undefined : { prev_cause: prevCause });
  return json({ account: accountSnapshot(found.user), seq, replayed: false });
}

// D24: vai bắt TOTP khi đăng nhập (cán bộ KN + vai [MFA]) — accounts/port.LoginTOTPRoles.
const LOGIN_TOTP_ROLES = ["kn_officer", "kn_province_admin", "kn_national_admin", "kn_user", ...MFA_ROLES, "geo_admin", "device_admin", "sync_conflict_resolver"];
/** Tài khoản đã gỡ TOTP (đặt lại) trong phiên mock. */
const TOTP_REMOVED = new Set<string>();

function mfaOf(u: AccountsUser): NonNullable<AccountsUser["mfa"]> {
  const required = (u.roles ?? []).some((r) => LOGIN_TOTP_ROLES.includes(r));
  const pending = u.required_actions.includes("CONFIGURE_TOTP");
  return { required, configured: required && !pending && !TOTP_REMOVED.has(u.id) && u.required_actions.length === 0, enrollment_pending: pending };
}

function resetTotp(p: MockPersona, id: string | undefined, body: unknown): Response {
  const found = findAccount(p, id);
  if (found instanceof Response) return found;
  const reason = reasonOf(body);
  if (reason.length < 20) return validation([{ field: "reason", reason: "Lý do tối thiểu 20 ký tự" }]);
  if (found.user.id === p.me.actor_ref) return problem(403, "accounts.self_action", "Self action");
  const bad = (found.user.roles ?? []).find((r) => !grantable(p).includes(r));
  if (bad) return problem(403, "accounts.role_not_grantable", "Role not grantable");
  if (!found.user.enabled) return problem(409, "accounts.state_conflict", "Locked");
  const m = mfaOf(found.user);
  if (!m.configured && !m.enrollment_pending && !m.required) return problem(409, "accounts.state_conflict", "No TOTP");
  TOTP_REMOVED.add(found.user.id);
  if (!found.user.required_actions.includes("CONFIGURE_TOTP")) found.user.required_actions = [...found.user.required_actions, "CONFIGURE_TOTP"];
  const seq = pushEvent(found, "totp_reset", p.me.actor_ref, reason, { otp_removed: m.configured ? 1 : 0, sessions_revoked: true });
  return json({ account: accountSnapshot(found.user), seq, replayed: false });
}

function resendActions(p: MockPersona, id: string | undefined, body: unknown): Response {
  const a = findAccount(p, id);
  if (a instanceof Response) return a;
  if (!a.user.enabled) return problem(409, "accounts.state_conflict", "Locked");
  const req = (body as { actions?: unknown } | undefined)?.actions;
  const actions = Array.isArray(req) && req.length ? req : a.user.required_actions.length ? a.user.required_actions : ["UPDATE_PASSWORD"];
  pushEvent(a, "actions_sent", p.me.actor_ref, null, { actions });
  return json({ account: accountSnapshot(a.user), activation_email_sent: true, replayed: false });
}

// --------------------------------------------------------------------------
// pf/reports — lịch gửi
// --------------------------------------------------------------------------
export const SCHEDULES: ReportSchedule[] = [
  {
    id: "01927b3e-5ced-7000-8000-000000000001",
    template_key: "kn.sowing_progress",
    format: "xlsx",
    unit_code: "82",
    params: opaque({ level: "commune" }),
    frequency: "weekly",
    weekday: 1,
    day_of_month: null,
    hour: 7,
    recipients: [ACCOUNTS[0]!.user.id],
    owner_ref: "mock-admin-setter",
    active: true,
    deactivated_reason: null,
    created_at: iso(30),
    updated_at: iso(30),
  },
];

// --------------------------------------------------------------------------
// Bảng định tuyến Admin
// --------------------------------------------------------------------------
export const adminOpsRoutes: Route[] = [
  // --- pf/masterdata: G-06, G-07 ---
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/catalogs$/,
    handler: ({ persona }) => {
      if (!admin(persona, "masterdata_admin")) return problem(403, "forbidden", "Forbidden");
      return json({
        items: (Object.keys(CATALOGS) as CatalogName[]).map((k) => ({
          catalog: k,
          name: CATALOG_META[k].name,
          item_count: CATALOGS[k].length,
          inactive_count: CATALOGS[k].filter((i) => !i.active).length,
          attrs_schema: CATALOG_META[k].attrs_schema,
        })),
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/masterdata\/params$/,
    handler: ({ persona, url }) => {
      if (!admin(persona, "param_setter", "param_approver")) return problem(403, "forbidden", "Forbidden");
      const prefix = url.searchParams.get("prefix") ?? "";
      const now = Date.now();
      const items = PARAM_REGISTRY.filter((d) => d.key.startsWith(prefix)).map((d) => {
        const versions = PARAM_VERSIONS.filter((v) => v.key === d.key);
        const current = versions
          .filter((v) => v.status === "approved" && Date.parse(v.effective_from) <= now)
          .sort((a, b) => Date.parse(b.effective_from) - Date.parse(a.effective_from) || b.version - a.version)[0];
        return {
          ...d,
          current: current ? { version: current.version, value: current.value, text: current.text, effective_from: current.effective_from } : null,
          submitted_count: versions.filter((v) => v.status === "submitted").length,
        };
      });
      return json(paginate(items, url));
    },
  },

  // --- pf/party: G-08, xuất/nhập cây ---
  {
    method: "GET",
    pattern: /^\/v1\/orgs$/,
    handler: ({ persona, url }) => {
      if (!admin(persona, "registry_admin", "account_admin", "account_admin_province", "reports_admin")) return problem(403, "forbidden", "Forbidden");
      const kind = url.searchParams.get("kind");
      const parent = url.searchParams.get("parent_ref");
      const inactive = url.searchParams.get("include_inactive") !== "false";
      const list = ORGS.filter(
        (o) => (!kind || o.kind === kind) && (!parent || o.parent_ref === parent) && (inactive || o.active) && orgInScope(persona, o),
      );
      return json(paginate(list, url, 50, 200));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/orgs\/export\.xlsx$/,
    handler: ({ persona }) => {
      if (!admin(persona, "registry_admin")) return problem(403, "forbidden", "Forbidden");
      // Giả lập: CSV UTF-8 (BOM) cùng cột với tệp .xlsx thật; tên tệp ghi rõ "gia-lap".
      const head = "Mã tổ chức,Loại,Tên,Mã đơn vị hành chính,Mã tổ chức cấp trên,Trạng thái";
      const code = (ref: string | null) => ORGS.find((o) => o.ref === ref)?.code ?? "";
      const lines = ORGS.map((o) => [o.code, o.kind, `"${o.name}"`, o.admin_unit_code ?? "", code(o.parent_ref), o.active ? "Hoạt động" : "Ngừng hoạt động"].join(","));
      return new Response(`﻿${head}\r\n${lines.join("\r\n")}\r\n`, {
        status: 200,
        headers: { "Content-Type": "text/csv; charset=utf-8", "Content-Disposition": 'attachment; filename="co-cau-to-chuc-gia-lap.csv"' },
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/orgs\/import$/,
    handler: ({ persona, body }) => {
      if (!admin(persona, "registry_admin")) return problem(403, "forbidden", "Forbidden");
      if (isZip(body)) {
        return json({ created: 0, updated: 1, unchanged: 2, rejected: 0, rows: [
          { row: 2, status: "unchanged", code: "TTKNQG", org_ref: ORGS[0]!.ref, changes: [], reason: null },
          { row: 3, status: "updated", code: ORGS[3]!.code, org_ref: ORGS[3]!.ref, changes: ["renamed"], reason: null },
          { row: 4, status: "unchanged", code: ORGS[4]!.code, org_ref: ORGS[4]!.ref, changes: [], reason: null },
        ] });
      }
      const rows = parseCsv(typeof body === "string" ? body : "");
      const header = (rows[0] ?? []).map(headerKey);
      const iCode = header.findIndex((h) => ["ma_to_chuc", "code"].includes(h));
      const iKind = header.findIndex((h) => ["loai", "kind"].includes(h));
      const iName = header.findIndex((h) => ["ten", "name"].includes(h));
      if (iCode < 0 || iKind < 0 || iName < 0) return problem(422, "party.import_invalid", "Missing required columns");
      const out = rows.slice(1).map((r, i) => {
        const o = ORGS.find((x) => x.code === (r[iCode] ?? "").trim());
        if (!o) return { row: i + 2, status: "rejected" as const, code: r[iCode] ?? null, org_ref: null, changes: [], reason: "Không tạo mới trong giả lập" };
        const name = (r[iName] ?? "").trim();
        if (name && name !== o.name) return { row: i + 2, status: "updated" as const, code: o.code, org_ref: o.ref, changes: ["renamed" as const], reason: null };
        return { row: i + 2, status: "unchanged" as const, code: o.code, org_ref: o.ref, changes: [], reason: null };
      });
      const count = (s: string) => out.filter((x) => x.status === s).length;
      return json({ created: count("created"), updated: count("updated"), unchanged: count("unchanged"), rejected: count("rejected"), rows: out });
    },
  },

  // --- pf/sync: thiết bị, xung đột ---
  {
    method: "GET",
    pattern: /^\/v1\/devices$/,
    handler: ({ persona, url }) => {
      if (!admin(persona, "device_admin")) return problem(403, "forbidden", "Forbidden");
      return json(paginate(MOCK_DEVICES.map((d) => omit(d, "commune_code")), url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/sync\/conflicts$/,
    handler: ({ persona, url }) => {
      if (!admin(persona, "sync_conflict_resolver")) return problem(403, "forbidden", "Forbidden");
      const status = url.searchParams.get("status");
      const entity = url.searchParams.get("entity");
      const sla = url.searchParams.get("sla");
      const overdue = url.searchParams.get("overdue") === "true";
      const list = SYNC_CONFLICTS_STORE.map((x) => ({ ...x.c, overdue: x.c.status === "open" && Date.parse(x.c.due_at) < Date.now(), sla_status: slaStatusOf(x.c) })).filter(
        (c) => (!status || c.status === status) && (!entity || c.entity === entity) && (!sla || c.sla_status === sla) && (!overdue || c.overdue),
      );
      return json(paginate(list, url));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/sync\/conflicts\/([\w-]+)$/,
    handler: ({ persona, params }) => {
      if (!admin(persona, "sync_conflict_resolver")) return problem(403, "forbidden", "Forbidden");
      const x = SYNC_CONFLICTS_STORE.find((c) => c.c.conflict_id === params[0]);
      if (!x) return problem(404, "sync.conflict_not_found", "Not found");
      return json({ ...x.c, sla_status: slaStatusOf(x.c), resolution: x.resolution, versions_available: x.versions.length > 0, records: x.records, versions: x.versions });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/sync\/conflicts\/([\w-]+)\/resolve$/,
    handler: ({ persona, params, body }) => {
      if (!admin(persona, "sync_conflict_resolver")) return problem(403, "forbidden", "Forbidden");
      const x = SYNC_CONFLICTS_STORE.find((c) => c.c.conflict_id === params[0]);
      if (!x) return problem(404, "sync.conflict_not_found", "Not found");
      const note = reasonOf(body, "note");
      if (note.length < 10) return validation([{ field: "note", reason: "Ghi chú xử lý tối thiểu 10 ký tự" }]);
      if (x.c.status === "resolved") return problem(409, "sync.conflict_state", "Already resolved");
      const input = (body ?? {}) as { resolution?: unknown; resolved_record_id?: unknown };
      const rid = typeof input.resolved_record_id === "string" ? input.resolved_record_id : null;
      const own = x.records.some((r) => r.record_id === rid);
      if (input.resolution === "keep_version" && !own) return problem(422, "sync.conflict_resolution_invalid", "Invalid resolution");
      if (input.resolution === "correction" && own) return problem(422, "sync.conflict_resolution_invalid", "Invalid resolution");
      Object.assign(x.c, { status: "resolved", overdue: false, resolved_at: nowIso(), resolved_by: persona.me.actor_ref, resolution_note: note, resolved_record_id: rid });
      x.resolution = input.resolution === "keep_version" || input.resolution === "correction" ? input.resolution : null;
      return json({ ...x.c, sla_status: slaStatusOf(x.c) });
    },
  },
  { method: "POST", pattern: /^\/v1\/sync\/nudge$/, handler: ({ persona, body }) => nudge(persona, body) },

  // --- pf/mechanization: chạy dự báo (cgh_admin) ---
  {
    method: "POST",
    pattern: /^\/v1\/mechanization\/forecasts$/,
    handler: ({ persona, body }) => {
      if (!hasRole(persona, "cgh_admin")) return problem(403, "forbidden", "Forbidden");
      const input = (body ?? {}) as { season_code?: unknown; season_year?: unknown };
      const code = typeof input.season_code === "string" ? input.season_code.toUpperCase() : "";
      const year = typeof input.season_year === "number" ? input.season_year : NaN;
      const errors: { field: string; reason: string }[] = [];
      if (!["DX", "HT", "TD"].includes(code)) errors.push({ field: "season_code", reason: "Mã vụ DX, HT hoặc TD" });
      if (!Number.isInteger(year) || year < 2020 || year > 2100) errors.push({ field: "season_year", reason: "Năm bắt đầu vụ không hợp lệ" });
      if (errors.length) return validation(errors);
      const seq = FORECASTS.filter((f) => f.run.season_code === code && f.run.season_year === year).length + 1;
      const f = computeForecast(code, year, seq + 1);
      f.run.computed_at = nowIso();
      FORECASTS.unshift(f);
      return json({ run: f.run }, 201);
    },
  },

  // --- pf/accounts ---
  {
    method: "GET",
    pattern: /^\/v1\/accounts\/grants$/,
    handler: ({ persona }) => {
      if (!admin(persona, "account_admin", "account_admin_province")) return problem(403, "forbidden", "Forbidden");
      return json({ grantable: grantable(persona), mfa_roles: MFA_ROLES, role_org_kinds: ROLE_ORG_KINDS });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/accounts\/users$/,
    handler: ({ persona, url }) => {
      if (!admin(persona, "account_admin", "account_admin_province")) return problem(403, "forbidden", "Forbidden");
      const org = url.searchParams.get("org_ref");
      return json(paginate(ACCOUNTS.filter((a) => accountVisible(persona, a) && (!org || a.user.org_ref === org)).map((a) => a.user), url, 50, 100));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/accounts\/users$/,
    handler: ({ persona, body }) => {
      if (!admin(persona, "account_admin", "account_admin_province")) return problem(403, "forbidden", "Forbidden");
      const res = createAccount(persona, (body ?? {}) as Record<string, unknown>);
      if (res.error) return res.error;
      return json({ user: res.user, seq: 2, activation_email_sent: true }, 201, { Location: `/v1/accounts/users/${res.user!.id}` });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/accounts\/users\/import$/,
    handler: ({ persona, body, request }) => {
      if (!admin(persona, "account_admin", "account_admin_province")) return problem(403, "forbidden", "Forbidden");
      return importAccounts(persona, body, request);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/accounts\/users\/([\w-]+)$/,
    handler: ({ persona, params }) => {
      const a = findAccount(persona, params[0]);
      return a instanceof Response ? a : json(accountDetail(a.user));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/accounts\/users\/([\w-]+)\/events$/,
    handler: ({ persona, params }) => {
      const a = findAccount(persona, params[0]);
      return a instanceof Response ? a : json({ items: a.events });
    },
  },
  { method: "POST", pattern: /^\/v1\/accounts\/users\/([\w-]+)\/lock$/, handler: ({ persona, params, body }) => lockUnlock(persona, params[0], body, true) },
  { method: "POST", pattern: /^\/v1\/accounts\/users\/([\w-]+)\/unlock$/, handler: ({ persona, params, body }) => lockUnlock(persona, params[0], body, false) },
  { method: "POST", pattern: /^\/v1\/accounts\/users\/([\w-]+)\/reset-totp$/, handler: ({ persona, params, body }) => resetTotp(persona, params[0], body) },
  { method: "POST", pattern: /^\/v1\/accounts\/users\/([\w-]+)\/resend-actions$/, handler: ({ persona, params, body }) => resendActions(persona, params[0], body) },
  {
    // Thao tác loạt (US-ADM-03): tối đa 100 tài khoản, kết quả theo từng tài khoản.
    method: "POST",
    pattern: /^\/v1\/accounts\/users\/bulk$/,
    handler: ({ persona, body }) => {
      const b = (body ?? {}) as { action?: unknown; user_ids?: unknown; reason?: unknown };
      const ids = Array.isArray(b.user_ids) ? b.user_ids.filter((x): x is string => typeof x === "string") : [];
      if (b.action !== "lock" && b.action !== "unlock" && b.action !== "resend") return validation([{ field: "action", reason: "lock, unlock hoặc resend" }]);
      if (ids.length === 0 || ids.length > 100) return validation([{ field: "user_ids", reason: "1–100 tài khoản" }]);
      if (b.action !== "resend" && reasonOf(b).length < 20) return validation([{ field: "reason", reason: "Lý do tối thiểu 20 ký tự" }]);
      const out = ids.map((id) => {
        const r = b.action === "resend" ? resendActions(persona, id, {}) : lockUnlock(persona, id, { reason: b.reason }, b.action === "lock");
        const p = r.ok ? null : problemOf(r);
        return { user_id: id, ok: r.ok, replayed: false, code: p?.code ?? null, detail: p?.title ?? null };
      });
      return json({ succeeded: out.filter((o) => o.ok).length, failed: out.filter((o) => !o.ok).length, items: out });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/accounts\/users\/([\w-]+)\/assignment$/,
    handler: ({ persona, params, body }) => {
      const a = findAccount(persona, params[0]);
      if (a instanceof Response) return a;
      if (a.user.id === persona.me.actor_ref) return problem(403, "accounts.self_action", "Self action");
      const input = (body ?? {}) as { org_ref?: unknown; roles?: unknown };
      const orgRef = typeof input.org_ref === "string" ? input.org_ref : a.user.org_ref;
      const roles = Array.isArray(input.roles) ? input.roles.filter((r): r is string => typeof r === "string") : (a.user.roles ?? []);
      if (roles.length === 0) return validation([{ field: "roles", reason: "Chọn ít nhất một vai" }]);
      const org = ORGS.find((o) => o.ref === orgRef);
      if (!org) return problem(422, "accounts.org_invalid", "Org invalid");
      if (!orgInScope(persona, org)) return problem(403, "accounts.out_of_scope", "Out of scope");
      if (!org.active) return problem(409, "accounts.org_inactive", "Org inactive");
      if ((a.user.roles ?? []).some((r) => !grantable(persona).includes(r))) return problem(403, "accounts.role_not_grantable", "Role not grantable");
      const bad = checkRoles(persona, roles, org);
      if (bad) return bad;
      const prev = a.user.org_ref;
      a.user.org_ref = org.ref;
      a.user.roles = roles;
      const seq = pushEvent(a, "assignment_changed", persona.me.actor_ref, null, { prev_org_ref: prev });
      return json({ account: accountSnapshot(a.user), seq, replayed: false });
    },
  },

  // --- pf/reports: mẫu, lịch gửi ---
  {
    method: "GET",
    pattern: /^\/v1\/reports\/templates$/,
    handler: ({ persona }) => {
      if (!admin(persona, "reports_admin", "masterdata_admin")) return problem(403, "forbidden", "Forbidden");
      return json({ items: templateKeys().map((k) => latestTemplate(k)!) });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/reports\/templates$/,
    handler: ({ persona, body }) => {
      if (!admin(persona, "reports_admin", "masterdata_admin")) return problem(403, "forbidden", "Forbidden");
      const input = (body ?? {}) as { key?: unknown; definition?: ReportTemplate["definition"] };
      const key = typeof input.key === "string" ? input.key : "";
      const def = input.definition;
      const errors: { field: string; reason: string }[] = [];
      if (!/^[a-z][a-z0-9_.]{2,63}$/.test(key)) errors.push({ field: "key", reason: "Khóa mẫu a-z, 0-9, dấu chấm, 3–64 ký tự" });
      if (!def || typeof def.title !== "string" || def.title.trim().length < 5) errors.push({ field: "definition.title", reason: "Tiêu đề từ 5 ký tự" });
      if (!def || !Array.isArray(def.formats) || def.formats.length === 0) errors.push({ field: "definition.formats", reason: "Chọn ít nhất một định dạng" });
      if (!def || !Array.isArray(def.columns) || def.columns.length === 0) errors.push({ field: "definition.columns", reason: "Cần ít nhất một cột" });
      if (errors.length) return validation(errors);
      const prev = latestTemplate(key);
      const t: ReportTemplate = { key, version: (prev?.version ?? 0) + 1, builtin: false, status: "active", definition: def!, reason: null, created_by: persona.me.actor_ref, created_at: nowIso() };
      REPORT_TEMPLATES.push(t);
      return json(t, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/reports\/templates\/([\w.]+)\/versions$/,
    handler: ({ params }) => {
      const list = REPORT_TEMPLATES.filter((t) => t.key === params[0]).sort((a, b) => b.version - a.version);
      return list.length ? json({ items: list }) : problem(404, "reports.template_not_found", "Not found");
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/reports\/templates\/([\w.]+)\/retire$/,
    handler: ({ persona, params, body }) => {
      if (!admin(persona, "reports_admin", "masterdata_admin")) return problem(403, "forbidden", "Forbidden");
      const prev = latestTemplate(params[0] ?? "");
      if (!prev) return problem(404, "reports.template_not_found", "Not found");
      const reason = reasonOf(body);
      if (reason.length < 10) return validation([{ field: "reason", reason: "Lý do tối thiểu 10 ký tự" }]);
      if (prev.status === "retired") return problem(409, "reports.template_retired", "Already retired");
      const t: ReportTemplate = { ...prev, version: prev.version + 1, builtin: false, status: "retired", reason, created_by: persona.me.actor_ref, created_at: nowIso() };
      REPORT_TEMPLATES.push(t);
      return json(t, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/reports\/schedules$/,
    handler: ({ persona }) => {
      if (!admin(persona, "reports_admin", "kn_province_admin", "kn_national_admin")) return problem(403, "forbidden", "Forbidden");
      return json({ items: SCHEDULES });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/reports\/schedules$/,
    handler: ({ persona, body }) => {
      if (!admin(persona, "reports_admin", "kn_province_admin", "kn_national_admin")) return problem(403, "forbidden", "Forbidden");
      const input = (body ?? {}) as Record<string, unknown>;
      const key = typeof input.template_key === "string" ? input.template_key : "";
      const t = latestTemplate(key);
      if (!t) return problem(404, "reports.template_not_found", "Not found");
      if (t.status === "retired") return problem(409, "reports.template_retired", "Retired");
      const format = input.format === "pdf" || input.format === "xlsx" ? input.format : null;
      const freq = input.frequency === "weekly" || input.frequency === "monthly" ? input.frequency : null;
      const hour = typeof input.hour === "number" ? input.hour : -1;
      const recipients = Array.isArray(input.recipients) ? input.recipients.filter((r): r is string => typeof r === "string") : [];
      const errors: { field: string; reason: string }[] = [];
      if (!format) errors.push({ field: "format", reason: "Chọn xlsx hoặc pdf" });
      else if (!t.definition.formats.includes(format)) return problem(422, "reports.format_unsupported", "Format unsupported");
      if (!freq) errors.push({ field: "frequency", reason: "Chọn hằng tuần hoặc hằng tháng" });
      if (freq === "weekly" && !(typeof input.weekday === "number" && input.weekday >= 1 && input.weekday <= 7)) errors.push({ field: "weekday", reason: "Chọn thứ trong tuần" });
      if (freq === "monthly" && !(typeof input.day_of_month === "number" && input.day_of_month >= 1 && input.day_of_month <= 28)) errors.push({ field: "day_of_month", reason: "Ngày 1–28" });
      if (hour < 0 || hour > 23) errors.push({ field: "hour", reason: "Giờ 0–23" });
      if (recipients.length === 0) errors.push({ field: "recipients", reason: "Chọn ít nhất một người nhận" });
      if (errors.length) return validation(errors);
      const now = nowIso();
      const s: ReportSchedule = {
        id: uuidv7(), template_key: key, format: format!, unit_code: typeof input.unit_code === "string" && input.unit_code ? input.unit_code : null,
        params: opaque((input.params as Record<string, unknown>) ?? {}), frequency: freq!, weekday: freq === "weekly" ? (input.weekday as number) : null,
        day_of_month: freq === "monthly" ? (input.day_of_month as number) : null, hour, recipients, owner_ref: persona.me.actor_ref, active: true,
        deactivated_reason: null, created_at: now, updated_at: now,
      };
      SCHEDULES.unshift(s);
      return json(s, 201);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/reports\/schedules\/([\w-]+)\/deactivate$/,
    handler: ({ params, body }) => {
      const s = SCHEDULES.find((x) => x.id === params[0]);
      if (!s) return problem(404, "reports.schedule_not_found", "Not found");
      const reason = reasonOf(body);
      if (reason.length < 10) return validation([{ field: "reason", reason: "Lý do tối thiểu 10 ký tự" }]);
      Object.assign(s, { active: false, deactivated_reason: reason, updated_at: nowIso() });
      return json(s);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/reports\/schedules\/([\w-]+)\/runs$/,
    handler: ({ params }) => {
      const s = SCHEDULES.find((x) => x.id === params[0]);
      if (!s) return problem(404, "reports.schedule_not_found", "Not found");
      const age = (Date.now() - Date.parse(s.created_at)) / 86_400_000;
      const runs = age < 7 ? [] : [
        { slot_key: "2026-W38", slot_at: iso(8), status: "queued" as const, skip_reason: null, job_id: uuidv7(), fired_at: iso(8), delivery_status: "sent" as const, delivery_note: null, sent_count: s.recipients.length, delivered_at: iso(8) },
        { slot_key: "2026-W39", slot_at: iso(1), status: "queued" as const, skip_reason: null, job_id: uuidv7(), fired_at: iso(1), delivery_status: "sent" as const, delivery_note: null, sent_count: s.recipients.length, delivered_at: iso(1) },
      ];
      return json({ items: runs });
    },
  },

  // --- pf/geo: nhập ranh giới thửa ---
  {
    method: "POST",
    pattern: /^\/v1\/geo\/plots\/import$/,
    handler: ({ persona, body, url, request }) => {
      if (!admin(persona, "geo_admin")) return problem(403, "forbidden", "Forbidden");
      const type = request.headers.get("Content-Type") ?? "";
      const confirm = url.searchParams.get("confirm_overlap") === "true";
      const commune = url.searchParams.get("commune_code") ?? COMMUNES[0]!.code;
      let format: "geojson" | "kml" | "shapefile";
      let names: (string | null)[] = [];
      let bad = new Set<number>();
      if (isZip(body)) {
        format = "shapefile";
        // Tên tệp trong gói ZIP nằm ở dạng chữ thường trong thư mục trung tâm.
        const text = new TextDecoder("latin1").decode(body);
        if (!/\.shp/i.test(text) || !/\.dbf/i.test(text)) return problem(422, "geo.format_invalid", "Shapefile .zip must contain .shp and .dbf");
        if (!/\.prj/i.test(text)) return problem(422, "geo.crs_unsupported", "Missing .prj");
        names = ["Tờ 12 thửa 45", "Tờ 12 thửa 46", "Tờ 13 thửa 3"];
      } else if (type.includes("kml")) {
        format = "kml";
        const text = typeof body === "string" ? body : "";
        const marks = text.match(/<Placemark[\s>]/g) ?? [];
        if (marks.length === 0) return problem(422, "geo.format_invalid", "No features");
        names = marks.map((_, i) => `Thửa KML ${i + 1}`);
      } else if (type.includes("json")) {
        format = "geojson";
        const fc = body as { type?: string; features?: { properties?: { name?: string }; geometry?: { type?: string; coordinates?: number[][][] } }[]; crs?: unknown };
        if (!fc || (fc.type !== "FeatureCollection" && fc.type !== "Feature")) return problem(422, "geo.format_invalid", "Not GeoJSON");
        if (fc.crs) return problem(422, "geo.crs_unsupported", "Only WGS84");
        const feats = fc.type === "Feature" ? [fc as unknown as NonNullable<typeof fc.features>[number]] : (fc.features ?? []);
        if (feats.length === 0 || feats.length > 200) return problem(422, "geo.format_invalid", "Feature count out of range");
        names = feats.map((f) => f.properties?.name ?? null);
        bad = new Set(feats.map((f, i) => (f.geometry?.type === "Polygon" && (f.geometry.coordinates?.[0]?.length ?? 0) >= 5 ? -1 : i)).filter((i) => i >= 0));
      } else {
        return problem(422, "geo.format_unsupported", "Unsupported format");
      }
      const features = names.map((name, index) => {
        if (bad.has(index)) return { index, name, status: "rejected" as const, code: "geo.too_few_points", reason: "Dưới 3 điểm phân biệt hoặc thẳng hàng", plot_ref: null, plot_code: null, area_ha: null, flagged_overlaps: [] };
        const overlap = index === 1;
        if (overlap && !confirm) return { index, name, status: "rejected" as const, code: "geo.overlap_needs_confirm", reason: "Chồng lấn thửa nháp cùng xã — nhập lại với “Nhận cả đối tượng chồng lấn”", plot_ref: null, plot_code: null, area_ha: null, flagged_overlaps: [] };
        const ref = uuidv7();
        return {
          index, name, status: overlap ? ("flagged" as const) : ("accepted" as const), code: null, reason: null, plot_ref: ref,
          plot_code: `TH${commune}${String(index + 1).padStart(3, "0")}`, area_ha: `${(0.8 + index * 0.35).toFixed(4)}`,
          flagged_overlaps: overlap ? [{ plot_code: "TH0000123", overlap_pct: "2.40" }] : [],
        };
      });
      const n = (s: string) => features.filter((f) => f.status === s).length;
      return json({ format, accepted: n("accepted"), flagged: n("flagged"), duplicate: 0, rejected: n("rejected"), features });
    },
  },
];
