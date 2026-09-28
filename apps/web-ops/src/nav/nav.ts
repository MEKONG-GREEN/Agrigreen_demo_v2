import type { ScopeLevel } from "@/auth/principal";
import { hasRole, ROLES, type Principal } from "@/auth/principal";

export type NavIcon =
  | "dashboard"
  | "map"
  | "coop"
  | "task"
  | "library"
  | "catalog"
  | "param"
  | "org"
  | "tractor"
  | "megaphone"
  | "report"
  | "bell"
  | "sync"
  | "user"
  | "conflict"
  | "upload";

export interface NavItem {
  href: string;
  label: string;
  icon: NavIcon;
  /** Cấp phạm vi được thấy mục này. Bỏ trống = không lọc theo cấp. */
  levels?: readonly ScopeLevel[];
  /** Cần ít nhất một vai. Bỏ trống = không lọc theo vai. */
  roles?: readonly string[];
  /** Không vẽ trong menu (vd hộp thư mở từ chuông) nhưng vẫn dùng để kiểm quyền và hiện tiêu đề. */
  hidden?: boolean;
}

export interface NavGroup {
  /** Nhóm đánh số như prototype KN. */
  label: string;
  items: NavItem[];
}

const KN: readonly ScopeLevel[] = ["tw", "tinh", "xa"];

/**
 * Menu theo vai và phạm vi. Đây CHỈ là ẩn/hiện — backend vẫn kiểm quyền từng lời gọi (R-55).
 * Cục KTHT chỉ thấy cổng CGH và xuất báo cáo CGH (BRD CGH FN-10, FN-11 BR-01: không có đường tới khối vận hành).
 */
export const NAV_GROUPS: readonly NavGroup[] = [
  {
    label: "1. Điều hành",
    items: [
      { href: "/", label: "Tổng quan", icon: "dashboard", levels: KN },
      { href: "/ban-do", label: "Bản đồ khuyến nông", icon: "map", levels: KN },
      { href: "/nhiem-vu", label: "Nhiệm vụ hỗ trợ", icon: "task", levels: KN },
      { href: "/chi-dao", label: "Chỉ đạo khẩn", icon: "megaphone", levels: KN },
      { href: "/bao-cao", label: "Báo cáo & xuất dữ liệu", icon: "report", levels: [...KN, "cuc"] },
      {
        href: "/dong-bo",
        label: "Đồng bộ thiết bị",
        icon: "sync",
        roles: [ROLES.knProvinceAdmin, ROLES.knNationalAdmin, ROLES.deviceAdmin, ROLES.platformAdmin],
      },
      { href: "/thong-bao", label: "Thông báo", icon: "bell", hidden: true },
    ],
  },
  {
    label: "2. HTX & nông hộ",
    items: [{ href: "/htx", label: "Quản lý HTX & nông hộ", icon: "coop", levels: KN }],
  },
  {
    label: "3. Tri thức",
    items: [{ href: "/thu-vien", label: "Thư viện số", icon: "library", levels: KN }],
  },
  {
    label: "4. Cơ giới hoá",
    items: [
      {
        href: "/cgh",
        label: "Bản đồ CGH",
        icon: "tractor",
        levels: ["cuc"],
        // cgh_viewer (Cục) và platform_admin; vai được kiểm lại ở backend.
      },
    ],
  },
  {
    label: "5. Quản trị",
    items: [
      { href: "/quan-tri/tai-khoan", label: "Tài khoản cán bộ", icon: "user", roles: [ROLES.platformAdmin, ROLES.accountAdmin, ROLES.accountAdminProvince] },
      { href: "/quan-tri/danh-muc", label: "Danh mục dùng chung", icon: "catalog", roles: [ROLES.platformAdmin, ROLES.masterdataAdmin] },
      {
        href: "/quan-tri/tham-so",
        label: "Tham số & phê duyệt",
        icon: "param",
        roles: [ROLES.platformAdmin, ROLES.paramSetter, ROLES.paramApprover],
      },
      { href: "/quan-tri/to-chuc", label: "Cơ cấu tổ chức", icon: "org", roles: [ROLES.platformAdmin, ROLES.registryAdmin] },
      { href: "/quan-tri/nhap-nong-ho", label: "Nhập nông hộ từ tệp", icon: "coop", roles: [ROLES.platformAdmin, ROLES.registryAdmin] },
      { href: "/quan-tri/nhap-thua", label: "Nhập ranh giới thửa", icon: "upload", roles: [ROLES.platformAdmin, ROLES.geoAdmin] },
      { href: "/quan-tri/xung-dot", label: "Xung đột đồng bộ", icon: "conflict", roles: [ROLES.platformAdmin, ROLES.conflictResolver] },
      { href: "/quan-tri/mau-bao-cao", label: "Mẫu & lịch báo cáo", icon: "report", roles: [ROLES.platformAdmin, ROLES.reportsAdmin, ROLES.masterdataAdmin] },
    ],
  },
];

export function canSee(item: NavItem, p: Principal | null): boolean {
  if (!p) return false;
  const isAdmin = hasRole(p, ROLES.platformAdmin);
  if (item.roles && !hasRole(p, ...item.roles)) return false;
  if (item.levels) {
    // Quản trị nền tảng thấy mọi màn hình (phạm vi All theo ScopeResolver).
    if (isAdmin) return true;
    if (!p.level || !item.levels.includes(p.level)) return false;
  }
  return true;
}

export function visibleNav(p: Principal | null): NavGroup[] {
  return NAV_GROUPS.map((g) => ({ ...g, items: g.items.filter((i) => !i.hidden && canSee(i, p)) })).filter((g) => g.items.length > 0);
}

/** Trang đích sau đăng nhập: mục đầu tiên được thấy (Cục → /cgh). */
export function homeFor(p: Principal | null): string {
  if (p?.level === "cuc" && !hasRole(p, ROLES.platformAdmin)) return "/cgh";
  const first = visibleNav(p)[0]?.items[0];
  return first?.href ?? "/khong-co-quyen";
}

/** Mục menu ứng với một đường dẫn (khớp tiền tố dài nhất). */
export function findNavItem(pathname: string): NavItem | undefined {
  let best: NavItem | undefined;
  for (const g of NAV_GROUPS) {
    for (const i of g.items) {
      const match = i.href === "/" ? pathname === "/" : pathname === i.href || pathname.startsWith(`${i.href}/`);
      if (match && (!best || i.href.length > best.href.length)) best = i;
    }
  }
  return best;
}
