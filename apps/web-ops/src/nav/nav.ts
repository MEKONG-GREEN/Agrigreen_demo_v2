import type { ScopeLevel } from "@/auth/principal";
import { hasRole, KN_ROLES, ROLES, type Principal } from "@/auth/principal";

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
  | "upload"
  | "phone"
  | "layers"
  | "water"
  | "warehouse"
  | "sim"
  | "season"
  | "journal"
  | "approve"
  | "harvest"
  | "chart"
  | "qr"
  | "support"
  | "sop"
  | "plot";

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
/** Ghi dữ liệu mùa vụ HTX: quản lý HTX (HTX mình) và quản trị nền tảng. */
const HTX_WRITE: readonly string[] = [ROLES.coopManager, ROLES.platformAdmin];
/** Xem dữ liệu mùa vụ HTX: thêm cán bộ KN (chỉ đọc theo phạm vi). */
const HTX_READ: readonly string[] = [...HTX_WRITE, ...KN_ROLES];

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
      { href: "/ban-do-gis", label: "Bản đồ GIS nhiều lớp", icon: "layers", levels: KN },
      { href: "/nhiem-vu", label: "Nhiệm vụ hỗ trợ", icon: "task", levels: KN },
      { href: "/chi-dao", label: "Chỉ đạo khẩn", icon: "megaphone", levels: KN },
      { href: "/danh-ba", label: "Danh bạ trực", icon: "phone", levels: KN },
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
    // App HTX (pf/cropcycle, pf/accounts): quản lý HTX ghi trong HTX mình; cán bộ KN chỉ xem trong phạm vi.
    items: [
      { href: "/htx", label: "Quản lý HTX & nông hộ", icon: "coop", levels: KN },
      { href: "/htx/dashboard", label: "Bảng điều hành sản xuất", icon: "chart", roles: HTX_READ },
      { href: "/htx/nong-ho", label: "Quản lý nông hộ", icon: "coop", roles: HTX_READ },
      { href: "/htx/lo-ruong", label: "Lô ruộng & ranh giới", icon: "plot", roles: HTX_READ },
      { href: "/htx/mua-vu", label: "Mùa vụ", icon: "season", roles: HTX_READ },
      { href: "/htx/nhat-ky", label: "Nhật ký canh tác", icon: "journal", roles: HTX_READ },
      { href: "/htx/phe-duyet", label: "Phê duyệt nhật ký", icon: "approve", roles: HTX_WRITE },
      { href: "/htx/san-luong", label: "Sản lượng thu hoạch", icon: "harvest", roles: HTX_READ },
      // HTX US-DASH-03: lịch trên Application API chỉ cho quản lý HTX (cán bộ/quản trị dùng /quan-tri/mau-bao-cao).
      { href: "/htx/lich-bao-cao", label: "Lịch gửi báo cáo", icon: "report", roles: [ROLES.coopManager] },
      { href: "/htx/tai-khoan-nong-dan", label: "Tài khoản nông dân", icon: "qr", roles: HTX_READ },
      { href: "/htx/yeu-cau-ho-tro", label: "Yêu cầu hỗ trợ", icon: "support", roles: HTX_READ },
    ],
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
    label: "5. Logistics & mô phỏng",
    items: [{ href: "/mo-phong", label: "Mô phỏng chọn Hub", icon: "sim", roles: [ROLES.simulationAnalyst, ROLES.platformAdmin] }],
  },
  {
    label: "6. Quản trị",
    items: [
      { href: "/quan-tri/tai-khoan", label: "Tài khoản cán bộ", icon: "user", roles: [ROLES.platformAdmin, ROLES.accountAdmin, ROLES.accountAdminProvince] },
      { href: "/quan-tri/danh-muc", label: "Danh mục dùng chung", icon: "catalog", roles: [ROLES.platformAdmin, ROLES.masterdataAdmin] },
      { href: "/quan-tri/quy-trinh", label: "Quy trình SOP", icon: "sop", roles: [ROLES.platformAdmin, ROLES.sopAdmin, ROLES.paramApprover] },
      {
        href: "/quan-tri/tham-so",
        label: "Tham số & phê duyệt",
        icon: "param",
        roles: [ROLES.platformAdmin, ROLES.paramSetter, ROLES.paramApprover],
      },
      {
        // GIS US-CFG-01/02: bảng màu và ngưỡng % bản đồ là tham số có phê duyệt (R-56).
        href: "/quan-tri/hien-thi",
        label: "Quy chuẩn hiển thị bản đồ",
        icon: "layers",
        roles: [ROLES.platformAdmin, ROLES.paramSetter, ROLES.paramApprover],
      },
      { href: "/quan-tri/to-chuc", label: "Cơ cấu tổ chức", icon: "org", roles: [ROLES.platformAdmin, ROLES.registryAdmin] },
      { href: "/quan-tri/nhap-nong-ho", label: "Nhập nông hộ từ tệp", icon: "coop", roles: [ROLES.platformAdmin, ROLES.registryAdmin] },
      { href: "/quan-tri/bai-viet", label: "Soạn bài viết", icon: "library", roles: [ROLES.contentEditorNational, ROLES.contentEditorProvince] },
      { href: "/quan-tri/nhap-thua", label: "Nhập ranh giới thửa", icon: "upload", roles: [ROLES.platformAdmin, ROLES.geoAdmin] },
      { href: "/quan-tri/ban-do", label: "Biên tập bản đồ", icon: "map", roles: [ROLES.platformAdmin, ROLES.geoAdmin] },
      { href: "/quan-tri/kho-hub", label: "Kho / Hub & dung lượng", icon: "warehouse", roles: [ROLES.platformAdmin, ROLES.geoAdmin] },
      {
        href: "/quan-tri/thuy-he",
        label: "Mạng đường thủy",
        icon: "water",
        roles: [ROLES.platformAdmin, ROLES.waterwayEditor, ROLES.waterwayPublisher],
      },
      { href: "/quan-tri/xung-dot", label: "Xung đột đồng bộ", icon: "conflict", roles: [ROLES.platformAdmin, ROLES.conflictResolver] },
      // GIS US-SYNC-01/02: tình trạng luồng đồng bộ tích hợp, hàng chờ lỗi (DLQ).
      { href: "/quan-tri/giam-sat-dong-bo", label: "Giám sát đồng bộ", icon: "sync", roles: [ROLES.platformAdmin, ROLES.deviceAdmin] },
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
