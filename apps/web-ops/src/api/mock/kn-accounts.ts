// DỮ LIỆU GIẢ LẬP — tài khoản thử bề mặt KN v7.7 (NEXT_PUBLIC_API_MOCK=1). Không phải người thật.
// Mỗi tài khoản có nhiều lần gán vai trò (P1 role_assignment) như Hình 3b/3c/3d/3e/3f và 25b.
// Đăng nhập bằng email hoặc SĐT + MẬT KHẨU THỬ của chế độ giả lập (MOCK_KN_PASSWORD) — chỉ có trong
// bản build mock; bản thật đăng nhập qua Keycloak (OIDC + PKCE), web không bao giờ nhận mật khẩu.
import type { Me } from "../types";
import type { DepartmentRef, KnRole, RoleAssignment } from "@/kn/contract";
import { MKG_REF, NATIONAL_REF, provinceOrgRef } from "./data";
import type { MockPersona } from "./personas";

/** Mật khẩu thử của mọi tài khoản KN giả lập (giá trị kiểm thử, không phải bí mật). */
export const MOCK_KN_PASSWORD = "KhuyenNong@2026";

const TENANT = "01927b3e-0000-7000-8000-00000000a001";

/**
 * 6 phòng ban của TTKN An Giang (FN-AD-04.03) = dữ liệu nền Go internal/platform/party/seeddata/departments_an_giang.json
 * (kiểm bởi src/api/mock/kn-menu.contract.test.ts qua ca vàng kn-menu.json). Q-02: mới biết 4 tên trên wireframe —
 * 2 phòng còn lại là tên TẠM, chờ khách. Chỉ Phòng Trồng trọt "Đang áp dụng".
 */
export const KN_DEPARTMENTS: readonly DepartmentRef[] = [
  { code: "trong_trot", name: "Phòng Trồng trọt", description: "Báo cáo tuần/tháng/vụ cây trồng; thu thập HTX, máy móc, thửa ruộng, vật tư", status: "active" },
  { code: "chan_nuoi_thuy_san", name: "Phòng Chăn nuôi - Thủy sản", description: "Báo cáo Chăn nuôi, Nuôi trồng & Đánh bắt Thủy sản", status: "not_open" },
  { code: "ke_hoach_tai_chinh", name: "Phòng Kế hoạch - Tài chính", description: "Tổng hợp báo cáo liên phòng; Dự án khuyến nông", status: "not_open" },
  { code: "thong_tin_truyen_thong", name: "Phòng Thông tin - Truyền thông", description: "Thư viện kỹ thuật & tin tức; Quản trị đào tạo; Giá cả thị trường", status: "not_open" },
  { code: "phong_ban_5", name: "Phòng ban 5 (tạm, chờ khách Q-02)", description: "Tên chưa có trong BRD v7.7", status: "not_open" },
  { code: "phong_ban_6", name: "Phòng ban 6 (tạm, chờ khách Q-02)", description: "Tên chưa có trong BRD v7.7", status: "not_open" },
];

/** Bộ menu của phòng ban (P2 menu_profile): chỉ Trồng trọt có bộ riêng. */
export const KN_MENU_PROFILE: Readonly<Record<string, "trong_trot" | "generic">> = { trong_trot: "trong_trot" };

/** Tên ngắn (Hình 3c) — định nghĩa ở src/kn/menuAdapter.ts, giữ tên xuất cũ. */
export { SHORT_DEPT_NAME } from "@/kn/menuAdapter";

const dept = (...codes: string[]) => KN_DEPARTMENTS.filter((d) => codes.includes(d.code));

const AG_PROVINCE_REF = provinceOrgRef(1); // An Giang (mã 91) trong cây giả lập
const AG_ORG_NAME = "TTKN tỉnh An Giang";

export interface MockKnAccount {
  id: string;
  displayName: string;
  /** Email và SĐT dùng để đăng nhập (FN-CH-00.01: email hoặc SĐT). */
  identifiers: string[];
  assignments: RoleAssignment[];
}

function a(id: string, role: KnRole, extra: Partial<RoleAssignment> = {}): RoleAssignment {
  const base: Record<KnRole, Pick<RoleAssignment, "org_ref" | "org_name">> = {
    kn_tw: { org_ref: NATIONAL_REF, org_name: "Trung tâm Khuyến nông Quốc gia" },
    kn_tinh: { org_ref: AG_PROVINCE_REF, org_name: AG_ORG_NAME },
    kn_xa: { org_ref: AG_PROVINCE_REF, org_name: AG_ORG_NAME },
    admin: { org_ref: MKG_REF, org_name: "Toàn hệ thống" },
  };
  return {
    assignment_id: id,
    role,
    ...base[role],
    mgmt_level: role === "admin" ? null : "Chuyên viên",
    communes: [],
    departments: role === "admin" ? [] : dept("trong_trot"),
    ...extra,
  };
}

export const KN_ACCOUNTS: readonly MockKnAccount[] = [
  {
    // Hình 3b: 3 vai trò — Cấp Xã (2 xã, 3 phòng ban), Cấp Tỉnh, Admin.
    id: "kn-an",
    displayName: "Nguyễn Văn An",
    identifiers: ["an.nv@angiang.gov.vn", "0912345601"],
    assignments: [
      a("asg-an-xa", "kn_xa", {
        communes: [
          { code: "91301", name: "Mỹ Hòa Hưng" },
          { code: "91302", name: "Bình Đức" },
        ],
        departments: dept("trong_trot", "chan_nuoi_thuy_san", "thong_tin_truyen_thong"),
      }),
      a("asg-an-tinh", "kn_tinh"),
      a("asg-an-admin", "admin"),
    ],
  },
  {
    // Hình 3e: Cấp Tỉnh, đổi được sang Cấp Xã.
    id: "kn-binh",
    displayName: "Trần Thị Bình",
    identifiers: ["binh.tt@angiang.gov.vn", "0912345602"],
    assignments: [
      a("asg-binh-tinh", "kn_tinh", { mgmt_level: "Trưởng phòng" }),
      a("asg-binh-xa", "kn_xa", { communes: [{ code: "91303", name: "Long Xuyên" }] }),
    ],
  },
  {
    // Hình 3d: chỉ 1 vai trò + 1 phòng ban → vào thẳng Dashboard.
    id: "kn-quan",
    displayName: "Lê Minh Quân",
    identifiers: ["quan.lm@ttknqg.gov.vn", "0912345603"],
    assignments: [a("asg-quan-tw", "kn_tw")],
  },
  {
    // Hình 3c: Admin → vào thẳng Tổng quan hệ thống.
    id: "kn-admin",
    displayName: "admin.mekong",
    identifiers: ["admin.mekong@mekonggreen.vn", "0912345604"],
    assignments: [a("asg-admin", "admin")],
  },
  {
    // Không có phòng ban đang áp dụng → thông báo liên hệ Admin (FN-CH-00.01 luồng phụ).
    id: "kn-loc",
    displayName: "Phạm Văn Lộc",
    identifiers: ["loc.pv@angiang.gov.vn", "0912345605"],
    assignments: [a("asg-loc-xa", "kn_xa", { departments: dept("chan_nuoi_thuy_san") })],
  },
];

export function findKnAccount(id: string | null | undefined): MockKnAccount | undefined {
  return KN_ACCOUNTS.find((x) => x.id === id);
}

export function findKnAccountByIdentifier(identifier: string): MockKnAccount | undefined {
  const raw = identifier.trim().toLowerCase();
  const digits = raw.replace(/[\s.-]/g, "").replace(/^\+84/, "0");
  return KN_ACCOUNTS.find((x) => x.identifiers.some((i) => i === raw || i === digits));
}

/** Vai realm cho bề mặt Admin khi đang ở ngữ cảnh Admin (để các màn quản trị hiện có mở được). */
const ADMIN_REALM_ROLES = [
  "platform_admin",
  "masterdata_admin",
  "registry_admin",
  "param_setter",
  "account_admin",
  "reports_admin",
  "sync_conflict_resolver",
  "device_admin",
  "geo_admin",
];

const LEGACY_ROLE: Record<KnRole, string[]> = {
  kn_tw: ["kn_national_admin"],
  kn_tinh: ["kn_province_admin"],
  kn_xa: ["kn_officer"],
  admin: ADMIN_REALM_ROLES,
};

/**
 * Persona mà backend giả lập dùng cho một tài khoản KN. Không có ngữ cảnh: chỉ vai cổng `kn_user`
 * (bản vá realm P1). Có ngữ cảnh (header X-AG-Context): vai + phạm vi của lần gán đã chọn —
 * mô phỏng middleware context của P1 dựng lại Principal.
 */
export function knPersona(account: MockKnAccount, assignment?: RoleAssignment): MockPersona {
  const roles = ["kn_user", ...(assignment ? LEGACY_ROLE[assignment.role] : [])];
  const isAdmin = assignment?.role === "admin";
  let org: Me["org"] = null;
  let scope: Me["scope"] = { all: false, level: "none", units: [], unit_names: {} };
  if (assignment) {
    switch (assignment.role) {
      case "kn_tw":
        org = { ref: NATIONAL_REF, kind: "ttkn_national", name: assignment.org_name, admin_unit_code: null, active: true };
        scope = { all: true, level: "national", units: [], unit_names: {} };
        break;
      case "kn_tinh":
        org = { ref: AG_PROVINCE_REF, kind: "ttkn_province", name: assignment.org_name, admin_unit_code: "91", active: true };
        scope = { all: false, level: "province", units: ["91"], unit_names: { "91": "An Giang" } };
        break;
      case "kn_xa":
        org = { ref: AG_PROVINCE_REF, kind: "ttkn_province", name: assignment.org_name, admin_unit_code: "91", active: true };
        scope = {
          all: false,
          level: "commune",
          units: assignment.communes.map((c) => c.code),
          unit_names: Object.fromEntries(assignment.communes.map((c) => [c.code, c.name])),
        };
        break;
      case "admin":
        org = { ref: MKG_REF, kind: "mkg", name: "Mekong Green", admin_unit_code: null, active: true };
        scope = { all: true, level: "national", units: [], unit_names: {} };
        break;
    }
  }
  return {
    id: account.id,
    title: account.displayName,
    description: "Tài khoản KN v7.7 (giả lập)",
    displayName: account.displayName,
    // Admin bắt buộc MFA (NFR Bảo mật): tài khoản Admin giả lập coi như đã qua OTP.
    amr: isAdmin || account.assignments.some((x) => x.role === "admin") ? ["pwd", "otp"] : ["pwd"],
    me: { actor_ref: `mock-${account.id}`, tenant_id: TENANT, roles, mfa: isAdmin, org, scope },
  };
}

export const KN_PERSONAS: readonly MockPersona[] = KN_ACCOUNTS.map((x) => knPersona(x));
