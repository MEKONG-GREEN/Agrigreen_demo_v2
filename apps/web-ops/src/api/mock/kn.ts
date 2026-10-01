// ============================================================================
// BACKEND GIẢ LẬP cho hợp đồng tạm KN v7.7 (src/kn/contract.ts):
//   GET  /v1/me/contexts                 (P1) — các lần gán vai trò của người gọi
//   POST /v1/me/context                  (P1) — chọn vai trò + phòng ban → context_id
//   GET  /v1/departments/{code}/modules  (P2) — cây menu theo vai trò (Admin: code "_all")
//   GET  /v1/me/menu-badges              (P4) — số đếm việc cần xử lý
// Bảng menu Trồng trọt dựng theo BRD v7.7 §6 (Hình 3c–3f). Đăng nhập mật khẩu giả lập ở `mockKnLogin`.
// ============================================================================
import { uuidv7 } from "../idempotency";
import type { DepartmentRef, KnRole, MenuGroup, MenuNode, MenuTree, RoleAssignment, SessionContext } from "@/kn/contract";
import { json, problem, type Route } from "./http";
import { findKnAccount, findKnAccountByIdentifier, KN_DEPARTMENTS, knPersona, MOCK_KN_PASSWORD, SHORT_DEPT_NAME, type MockKnAccount } from "./kn-accounts";
import type { MockPersona } from "./personas";

export const MENU_VERSION = "kn-menu.v7.7-2026-10-01";

/** Trang mặc định theo vai trò (FN-CH-00.01). */
export const DEFAULT_ROUTE: Record<KnRole, string> = {
  kn_tw: "/kn/dashboard",
  kn_tinh: "/kn/dashboard",
  kn_xa: "/kn/nhiem-vu/cong-viec",
  admin: "/kn/tong-quan",
};

/** Cờ "Báo cáo đột xuất": chỉ hiện khi phòng ban đã có mẫu loại này (FN-AD-04.06). Mặc định tắt như Hình 3e/3f. */
const ADHOC_FORMS: Record<string, boolean> = { trong_trot: false };

const BAO_CAO_KY: MenuNode[] = [
  { key: "bc-tuan", label: "Báo cáo tuần", icon: "file", href: "/kn/bao-cao/tuan" },
  { key: "bc-thang", label: "Báo cáo tháng", icon: "calendar", href: "/kn/bao-cao/thang" },
  { key: "bc-vu", label: "Báo cáo hàng vụ", icon: "sprout", href: "/kn/bao-cao/vu" },
];

function adhoc(dept: string): MenuNode[] {
  return ADHOC_FORMS[dept]
    ? [{ key: "bc-dot-xuat", label: "Báo cáo đột xuất", icon: "zap", href: "/kn/bao-cao/dot-xuat", badge_source: "adhoc_reports.todo" }]
    : [];
}

function deptGroups(role: Exclude<KnRole, "admin">, dept: string): MenuGroup[] {
  switch (role) {
    case "kn_tw":
      return [
        { key: "dieu-hanh", label: "Điều hành", items: [{ key: "dashboard", label: "Dashboard", icon: "dashboard", href: "/kn/dashboard" }] },
        {
          key: "bao-cao",
          label: "Báo cáo",
          items: [...BAO_CAO_KY, { key: "bc-nam", label: "Báo cáo năm", icon: "chart", href: "/kn/bao-cao/nam" }],
        },
      ];
    case "kn_tinh":
      return [
        {
          key: "dieu-hanh",
          label: "Điều hành",
          items: [
            { key: "dashboard", label: "Dashboard", icon: "dashboard", href: "/kn/dashboard" },
            {
              key: "nhiem-vu",
              label: "Nhiệm vụ",
              icon: "task",
              children: [
                { key: "phat-hanh", label: "Phát hành biểu mẫu", icon: "send", href: "/kn/nhiem-vu/phat-hanh" },
                { key: "trang-thai-nop", label: "Trạng thái nộp", icon: "bell", href: "/kn/nhiem-vu/trang-thai-nop", badge_source: "submissions.missing" },
                { key: "ra-soat", label: "Rà soát & chốt kỳ", icon: "check", href: "/kn/nhiem-vu/ra-soat", badge_source: "reviews.pending" },
                { key: "lich-cong-tac", label: "Lịch công tác", icon: "calendar", href: "/kn/nhiem-vu/lich-cong-tac", badge_source: "schedules.pending" },
              ],
            },
          ],
        },
        {
          key: "bao-cao",
          label: "Báo cáo",
          items: [
            { key: "tong-hop", label: "Báo cáo tổng hợp các xã", icon: "layers", href: "/kn/bao-cao/tong-hop" },
            { key: "chi-cuc", label: "Báo cáo Chi cục", icon: "file", href: "/kn/bao-cao/chi-cuc" },
            ...adhoc(dept),
          ],
        },
      ];
    case "kn_xa":
      return [
        {
          key: "dieu-hanh",
          label: "Điều hành",
          items: [
            {
              key: "nhiem-vu",
              label: "Nhiệm vụ",
              icon: "task",
              children: [
                { key: "cong-viec", label: "Công việc cần làm", icon: "bell", href: "/kn/nhiem-vu/cong-viec", badge_source: "report_tasks.todo" },
                { key: "lich-cong-tac", label: "Lịch công tác", icon: "calendar", href: "/kn/nhiem-vu/lich-cong-tac" },
              ],
            },
          ],
        },
        {
          key: "bao-cao",
          label: "Báo cáo",
          items: [...BAO_CAO_KY, ...adhoc(dept), { key: "bc-cua-xa", label: "Báo cáo của xã", icon: "chart", href: "/kn/bao-cao/cua-xa" }],
        },
        {
          key: "du-lieu",
          label: "Dữ liệu",
          items: [
            {
              key: "thu-thap",
              label: "Thu thập dữ liệu",
              icon: "database",
              children: [
                { key: "htx", label: "Danh sách HTX", icon: "coop", href: "/kn/du-lieu/htx" },
                { key: "may-moc", label: "Máy móc HTX", icon: "tractor", href: "/kn/du-lieu/may-moc" },
                { key: "ve-thua", label: "Vẽ thửa ruộng", icon: "map", href: "/kn/du-lieu/ve-thua" },
                { key: "vat-tu", label: "Vật tư", icon: "package", href: "/kn/du-lieu/vat-tu" },
              ],
            },
          ],
        },
      ];
  }
}

/** Menu Admin: nhóm Phòng ban (từng phòng ban) + Quản trị hệ thống (FN-CH-00.01 quy tắc, Hình 3c). */
function adminGroups(departments: readonly DepartmentRef[]): MenuGroup[] {
  const deptNodes: MenuNode[] = [];
  const closed: DepartmentRef[] = [];
  for (const d of departments) {
    if (d.status === "active") {
      const base = `/kn/phong-ban/${d.code}`;
      deptNodes.push({
        key: `pb-${d.code}`,
        label: d.name,
        icon: "sprout",
        children: [
          { key: `${d.code}-theo-doi`, label: "Theo dõi nộp báo cáo", icon: "bell", href: `${base}/theo-doi-nop` },
          { key: `${d.code}-tong-hop`, label: "Báo cáo tổng hợp các xã", icon: "layers", href: `${base}/tong-hop` },
          { key: `${d.code}-du-lieu`, label: "Dữ liệu báo cáo các xã", icon: "file", href: `${base}/du-lieu-bao-cao` },
          { key: `${d.code}-thu-thap`, label: "Thu thập dữ liệu", icon: "database", href: `${base}/thu-thap` },
          // Màn hiện có (G1): danh mục dùng chung, mẫu & lịch báo cáo — giữ tới khi P3/P6 dựng màn mới.
          { key: `${d.code}-danh-muc`, label: "Danh mục nông nghiệp", icon: "package", href: "/quan-tri/danh-muc" },
          { key: `${d.code}-mau`, label: "Mẫu báo cáo", icon: "table", href: "/quan-tri/mau-bao-cao" },
        ],
      });
    } else closed.push(d);
  }
  // Hình 3c: hai phòng chưa mở đầu tiên hiện riêng, phần còn lại gộp "n phòng khác".
  for (const d of closed.slice(0, 2)) {
    deptNodes.push({ key: `pb-${d.code}`, label: SHORT_DEPT_NAME[d.code] ?? d.name, icon: "building", status: "not_open" });
  }
  if (closed.length > 2) deptNodes.push({ key: "pb-khac", label: `${closed.length - 2} phòng khác`, icon: "building", status: "not_open" });

  return [
    { key: "dieu-hanh", label: "Điều hành", items: [{ key: "tong-quan", label: "Tổng quan hệ thống", icon: "dashboard", href: "/kn/tong-quan" }] },
    { key: "phong-ban", label: "Phòng ban", items: deptNodes },
    {
      key: "quan-tri",
      label: "Quản trị hệ thống",
      items: [
        { key: "dvhc", label: "Đơn vị hành chính", icon: "map", href: "/kn/quan-tri/don-vi-hanh-chinh" },
        { key: "co-cau", label: "Cơ cấu tổ chức 3 cấp", icon: "org", href: "/quan-tri/to-chuc" },
        { key: "phong-ban-dm", label: "Phòng ban", icon: "building", href: "/kn/quan-tri/phong-ban" },
        { key: "nguoi-dung", label: "Người dùng", icon: "users", href: "/quan-tri/tai-khoan" },
        { key: "menu-cac-cap", label: "Menu các cấp", icon: "key", href: "/kn/quan-tri/menu-cac-cap" },
        {
          // Q-13 (tạm, chờ khách): công cụ quản trị nền tảng đã có đặt trong nhóm Quản trị hệ thống.
          key: "cong-cu",
          label: "Công cụ nền tảng (tạm)",
          icon: "param",
          children: [
            { key: "tham-so", label: "Tham số & phê duyệt", icon: "param", href: "/quan-tri/tham-so" },
            { key: "xung-dot", label: "Xung đột đồng bộ", icon: "conflict", href: "/quan-tri/xung-dot" },
            { key: "giam-sat", label: "Giám sát đồng bộ", icon: "sync", href: "/quan-tri/giam-sat-dong-bo" },
            { key: "thiet-bi", label: "Đồng bộ thiết bị", icon: "sync", href: "/dong-bo" },
          ],
        },
      ],
    },
  ];
}

export function knMenuFor(role: KnRole, departmentCode: string | null): MenuTree {
  return {
    version: MENU_VERSION,
    role,
    department_code: role === "admin" ? null : departmentCode,
    groups: role === "admin" ? adminGroups(KN_DEPARTMENTS) : deptGroups(role, departmentCode ?? "trong_trot"),
  };
}

/** Số đếm giả lập theo vai trò (Hình 3e: 15 / 3 / 2; Hình 3f: 6). 0 → menu ẩn số đếm. */
const BADGES: Record<KnRole, Record<string, number>> = {
  kn_tw: {},
  kn_tinh: { "submissions.missing": 15, "reviews.pending": 3, "schedules.pending": 2, "adhoc_reports.todo": 0 },
  kn_xa: { "report_tasks.todo": 6, "adhoc_reports.todo": 1 },
  admin: {},
};

// --------------------------------------------------------------------------
// Ngữ cảnh phiên (giữ trong bộ nhớ tab, như mọi trạng thái giả lập khác)
// --------------------------------------------------------------------------
interface StoredContext {
  accountId: string;
  assignment: RoleAssignment;
  ctx: SessionContext;
}
const CONTEXTS = new Map<string, StoredContext>();

function accountOf(p: MockPersona): MockKnAccount | undefined {
  return findKnAccount(p.id);
}

/**
 * Persona theo header X-AG-Context (mô phỏng middleware P1): chỉ nhận context của chính tài khoản
 * trong token. Context lạ/của người khác → giữ persona gốc (backend thật trả 403).
 */
export function personaForContext(base: MockPersona, contextId: string | null): MockPersona {
  if (!contextId) return base;
  const stored = CONTEXTS.get(contextId);
  const account = accountOf(base);
  if (!stored || !account || stored.accountId !== account.id) return base;
  return knPersona(account, stored.assignment);
}

/** Context đã lưu (cho kiểm thử và khôi phục sau tải lại trong cùng tab). */
export function knContext(contextId: string): SessionContext | undefined {
  return CONTEXTS.get(contextId)?.ctx;
}

export const knAppRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/me\/contexts$/,
    // Vai thử cũ (không phải tài khoản KN) → danh sách rỗng: giữ bề mặt menu hiện có.
    handler: ({ persona }) => json({ assignments: accountOf(persona)?.assignments ?? [] }),
  },
  {
    method: "POST",
    pattern: /^\/v1\/me\/context$/,
    handler: ({ persona, body }) => {
      const account = accountOf(persona);
      const req = (body ?? {}) as { assignment_id?: unknown; department_code?: unknown };
      const asg = account?.assignments.find((x) => x.assignment_id === req.assignment_id);
      if (!account || !asg) return problem(404, "accounts.assignment_not_found", "Assignment not found");
      let dept: DepartmentRef | null = null;
      if (asg.role !== "admin") {
        dept = asg.departments.find((d) => d.code === req.department_code) ?? null;
        if (!dept) return problem(422, "accounts.department_required", "Department required");
        if (dept.status !== "active") return problem(409, "accounts.department_not_open", "Department not open");
      }
      const ctx: SessionContext = {
        context_id: uuidv7(),
        assignment_id: asg.assignment_id,
        role: asg.role,
        department_code: dept?.code ?? null,
        department_name: dept?.name ?? null,
        org_ref: asg.org_ref,
        org_name: asg.org_name,
        communes: asg.communes,
        default_route: DEFAULT_ROUTE[asg.role],
        expires_at: new Date(Date.now() + 4 * 3_600_000).toISOString(),
      };
      CONTEXTS.set(ctx.context_id, { accountId: account.id, assignment: asg, ctx });
      return json(ctx, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/departments\/([\w-]+)\/modules$/,
    handler: ({ params, url }) => {
      const role = url.searchParams.get("role") as KnRole | null;
      if (!role || !(role in DEFAULT_ROUTE)) return problem(422, "validation_failed", "role required");
      const code = params[0] ?? "";
      if (role !== "admin") {
        const d = KN_DEPARTMENTS.find((x) => x.code === code);
        if (!d) return problem(404, "party.department_not_found", "Department not found");
        if (d.status !== "active") return problem(409, "party.department_not_open", "Department not open");
      }
      return json(knMenuFor(role, role === "admin" ? null : code));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/me\/menu-badges$/,
    handler: ({ request }) => {
      const stored = CONTEXTS.get(request.headers.get("X-AG-Context") ?? "");
      return json({ counts: stored ? BADGES[stored.assignment.role] : {} });
    },
  },
];

// --------------------------------------------------------------------------
// Đăng nhập mật khẩu giả lập (FN-CH-00.01): sai → còn n lần; sai 5 lần → khóa tạm 15 phút.
// Bản thật: Keycloak brute-force detection (bản vá realm P1: waitIncrementSeconds 900).
// --------------------------------------------------------------------------
export const MAX_ATTEMPTS = 5;
export const LOCK_MINUTES = 15;
const ATTEMPTS = new Map<string, { failed: number; lockedUntil: number | null }>();

export type MockLoginResult =
  | { ok: true; personaId: string }
  | { ok: false; error: "invalid_credentials"; remaining: number }
  | { ok: false; error: "locked"; lockedUntil: string };

export function mockKnLogin(identifier: string, password: string, now = Date.now()): MockLoginResult {
  const key = identifier.trim().toLowerCase();
  const st = ATTEMPTS.get(key) ?? { failed: 0, lockedUntil: null };
  if (st.lockedUntil && st.lockedUntil > now) return { ok: false, error: "locked", lockedUntil: new Date(st.lockedUntil).toISOString() };
  if (st.lockedUntil) {
    st.failed = 0;
    st.lockedUntil = null;
  }
  const account = findKnAccountByIdentifier(identifier);
  // Cùng một thông báo cho tài khoản không tồn tại và sai mật khẩu (không lộ tài khoản nào có thật).
  if (!account || password !== MOCK_KN_PASSWORD) {
    st.failed += 1;
    if (st.failed >= MAX_ATTEMPTS) {
      st.lockedUntil = now + LOCK_MINUTES * 60_000;
      ATTEMPTS.set(key, st);
      return { ok: false, error: "locked", lockedUntil: new Date(st.lockedUntil).toISOString() };
    }
    ATTEMPTS.set(key, st);
    return { ok: false, error: "invalid_credentials", remaining: MAX_ATTEMPTS - st.failed };
  }
  ATTEMPTS.delete(key);
  return { ok: true, personaId: account.id };
}

/** Cho kiểm thử: xóa bộ đếm sai mật khẩu. */
export function resetMockLoginAttempts(): void {
  ATTEMPTS.clear();
}
