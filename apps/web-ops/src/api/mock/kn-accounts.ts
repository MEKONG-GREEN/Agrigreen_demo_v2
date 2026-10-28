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
 * 5 phòng ban của TTKN An Giang (FN-AD-04.03, W11.06) = dữ liệu nền Go internal/platform/party/seeddata/departments_an_giang.json
 * (kiểm bởi src/api/mock/kn-menu.contract.test.ts qua ca vàng kn-menu.json). Q-051 (chờ khách): tên "Thông tin Tuyên truyền"
 * theo BRD Báo giá/KHTC, mã giữ từ v7.7; mã tạm phong_ban_5 → hanh_chinh_tong_hop, phong_ban_6 bỏ (LEGACY_DEPT_ALIAS).
 * Chỉ Phòng Trồng trọt "Đang áp dụng". Mã: docs/kn/phong-ban-seed.md.
 */
export const KN_DEPARTMENTS: readonly DepartmentRef[] = [
  { code: "trong_trot", name: "Phòng Trồng trọt", description: "Báo cáo tuần/tháng/vụ cây trồng; thu thập HTX, máy móc, thửa ruộng, vật tư", status: "active" },
  { code: "chan_nuoi_thuy_san", name: "Phòng Chăn nuôi - Thủy sản", description: "Báo cáo Chăn nuôi, Nuôi trồng & Đánh bắt Thủy sản; KSGM, dịch bệnh, dự án KNQG", status: "not_open" },
  { code: "ke_hoach_tai_chinh", name: "Phòng Kế hoạch - Tài chính", description: "Kế hoạch năm, tổng hợp báo cáo liên phòng; dự án khuyến nông", status: "not_open" },
  { code: "thong_tin_truyen_thong", name: "Phòng Thông tin Tuyên truyền", description: "Báo giá nông sản; thư viện kỹ thuật & tin tức; quản trị đào tạo", status: "not_open" },
  { code: "hanh_chinh_tong_hop", name: "Phòng Hành chính - Tổng hợp", description: "Hồ sơ nhân sự, đánh giá xếp loại, kho tài liệu nhân sự; Contact dùng chung", status: "not_open" },
];

/** Bộ menu của phòng ban (P2 menu_profile, kn.menu/v3): mỗi phòng ban seed có khuôn riêng; phòng ban khác "generic". */
export type KnMenuProfile = "trong_trot" | "generic" | "chan_nuoi_thuy_san" | "ke_hoach_tai_chinh" | "thong_tin_truyen_thong" | "hanh_chinh_tong_hop";
export const KN_MENU_PROFILE: Readonly<Record<string, KnMenuProfile>> = {
  trong_trot: "trong_trot",
  chan_nuoi_thuy_san: "chan_nuoi_thuy_san",
  ke_hoach_tai_chinh: "ke_hoach_tai_chinh",
  thong_tin_truyen_thong: "thong_tin_truyen_thong",
  hanh_chinh_tong_hop: "hanh_chinh_tong_hop",
};

/** Mã tạm cũ → mã kế nhiệm (Go port.CanonicalDepartmentCode); "" = bỏ. */
export const LEGACY_DEPT_ALIAS: Readonly<Record<string, string>> = { phong_ban_5: "hanh_chinh_tong_hop", phong_ban_6: "" };

/** Phòng ban theo mã; mã tạm cũ không còn → phòng ban kế nhiệm. */
export function findKnDepartment(code: string): DepartmentRef | undefined {
  return KN_DEPARTMENTS.find((d) => d.code === code) ?? KN_DEPARTMENTS.find((d) => d.code === LEGACY_DEPT_ALIAS[code]);
}

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

/** Danh sách sống: Admin tạo tài khoản ở FN-AD-04.04 (kn-admin.ts) thêm vào đây — đăng nhập được ngay. */
export const KN_ACCOUNTS: MockKnAccount[] = [
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
  {
    // W11.29: Cấp Tỉnh – HCTH, Trưởng phòng (prototype HCTH `hung.tq`). Phòng HCTH mặc định "Chưa mở": Admin bật ở
    // Quản trị › Phòng ban rồi mới vào được (tests/e2e/hc-nhan-su.spec.ts).
    id: "kn-hung",
    displayName: "Trần Quốc Hưng",
    identifiers: ["hung.tq@angiang.gov.vn", "0912345606"],
    assignments: [a("asg-hung-tinh", "kn_tinh", { mgmt_level: "Trưởng phòng", departments: dept("hanh_chinh_tong_hop") })],
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

/** Persona của tài khoản KN theo id (kể cả tài khoản Admin vừa tạo — không có trong KN_PERSONAS lúc nạp). */
export function knPersonaById(id: string | null | undefined): MockPersona | undefined {
  const a = findKnAccount(id);
  return a ? knPersona(a) : undefined;
}

const KN_SEED_COUNT = KN_ACCOUNTS.length;

/** Cho kiểm thử: bỏ tài khoản tạo thêm, khôi phục lần gán đã sửa của tài khoản gốc. */
export function resetKnAccounts(original: readonly MockKnAccount[]): void {
  KN_ACCOUNTS.splice(KN_SEED_COUNT);
  for (const o of original) {
    const a = KN_ACCOUNTS.find((x) => x.id === o.id);
    if (a) a.assignments = o.assignments.map((x) => ({ ...x }));
  }
}
