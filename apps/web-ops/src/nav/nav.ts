import type { ScopeLevel } from "@/auth/principal";
import { API_CLIENT_ROLES, hasRole, KN_ROLES, METERING_READ_ROLES, MRV_ROLES, ROLES, type Principal } from "@/auth/principal";

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
  | "plot"
  // Bề mặt KN v7.7 (Design Sample): menu theo cấu hình (src/kn/contract.ts).
  | "calendar"
  | "sprout"
  | "send"
  | "database"
  | "building"
  | "package"
  | "table"
  | "key"
  | "users"
  | "zap"
  | "file"
  | "check"
  | "shield";

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
  /**
   * Module per_tenant sá»Ÿ há»¯u mÃ n (tÃªn repo, module.yaml). W1.4 (R-MOD-08): module Ä‘Ã£ táº¯t cho Ä‘Æ¡n vá»‹
   * (GET /v1/me/modules) â†’ khÃ´ng hiá»‡n. Bá» trá»‘ng = mÃ n lÃµi, luÃ´n hiá»‡n.
   */
  module?: string;
}

/** Táº­p module Ä‘Ã£ táº¯t cho Ä‘Æ¡n vá»‹ cá»§a ngÆ°á»i dÃ¹ng (GET /v1/me/modules, enabled = false). */
export type DisabledModules = ReadonlySet<string>;

const NONE: DisabledModules = new Set<string>();

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
      { href: "/ban-do", module: "pf/geo", label: "Bản đồ khuyến nông", icon: "map", levels: KN },
      { href: "/ban-do-gis", module: "pf/geo", label: "Bản đồ GIS nhiều lớp", icon: "layers", levels: KN },
      { href: "/nhiem-vu", module: "pf/extension", label: "Nhiệm vụ hỗ trợ", icon: "task", levels: KN },
      { href: "/chi-dao", label: "Chỉ đạo khẩn", icon: "megaphone", levels: KN },
      { href: "/danh-ba", label: "Danh bạ trực", icon: "phone", levels: KN },
      { href: "/bao-cao", module: "pf/reports", label: "Báo cáo & xuất dữ liệu", icon: "report", levels: [...KN, "cuc"] },
      // pf/mrv G3: đủ hồ sơ, đối chiếu HTX ↔ KN (KN US-MRV-02), bất thường FR-08, ước tính CO2 tạm.
      { href: "/mrv", module: "pf/mrv", label: "MRV: hồ sơ & đối chiếu", icon: "approve", roles: [...MRV_ROLES, ROLES.platformAdmin] },
      {
        href: "/dong-bo", module: "pf/sync",
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
      { href: "/htx/dashboard", module: "pf/cropcycle", label: "Bảng điều hành sản xuất", icon: "chart", roles: HTX_READ },
      { href: "/htx/nong-ho", label: "Quản lý nông hộ", icon: "coop", roles: HTX_READ },
      { href: "/htx/lo-ruong", module: "pf/geo", label: "Lô ruộng & ranh giới", icon: "plot", roles: HTX_READ },
      { href: "/htx/mua-vu", module: "pf/cropcycle", label: "Mùa vụ", icon: "season", roles: HTX_READ },
      { href: "/htx/nhat-ky", module: "pf/cropcycle", label: "Nhật ký canh tác", icon: "journal", roles: HTX_READ },
      { href: "/htx/phe-duyet", module: "pf/cropcycle", label: "Phê duyệt nhật ký", icon: "approve", roles: HTX_WRITE },
      { href: "/htx/san-luong", module: "pf/cropcycle", label: "Sản lượng thu hoạch", icon: "harvest", roles: HTX_READ },
      // HTX US-DASH-03: lịch trên Application API chỉ cho quản lý HTX (cán bộ/quản trị dùng /quan-tri/mau-bao-cao).
      { href: "/htx/lich-bao-cao", module: "pf/reports", label: "Lịch gửi báo cáo", icon: "report", roles: [ROLES.coopManager] },
      { href: "/htx/tai-khoan-nong-dan", label: "Tài khoản nông dân", icon: "qr", roles: HTX_READ },
      { href: "/htx/yeu-cau-ho-tro", label: "Yêu cầu hỗ trợ", icon: "support", roles: HTX_READ },
      { href: "/htx/thu-gom", module: "pf/cropcycle", label: "Thu gom rơm MKG", icon: "harvest", roles: HTX_WRITE },
    ],
  },
  {
    label: "3. Tri thức",
    items: [{ href: "/thu-vien", module: "pf/content", label: "Thư viện số", icon: "library", levels: KN }],
  },
  {
    label: "4. Cơ giới hoá",
    items: [
      {
        href: "/cgh", module: "pf/mechanization",
        label: "Bản đồ CGH",
        icon: "tractor",
        levels: ["cuc"],
        // cgh_viewer (Cục) và platform_admin; vai được kiểm lại ở backend.
      },
    ],
  },
  {
    label: "5. Logistics & mô phỏng",
    items: [{ href: "/mo-phong", module: "pf/simulation", label: "Mô phỏng chọn Hub", icon: "sim", roles: [ROLES.simulationAnalyst, ROLES.platformAdmin] }],
  },
  {
    label: "6. Quản trị",
    items: [
      { href: "/quan-tri/tai-khoan", label: "Tài khoản cán bộ", icon: "user", roles: [ROLES.platformAdmin, ROLES.accountAdmin, ROLES.accountAdminProvince] },
      { href: "/quan-tri/danh-muc", module: "pf/masterdata", label: "Danh mục dùng chung", icon: "catalog", roles: [ROLES.platformAdmin, ROLES.masterdataAdmin] },
      { href: "/quan-tri/quy-trinh", module: "pf/cropcycle", label: "Quy trình SOP", icon: "sop", roles: [ROLES.platformAdmin, ROLES.sopAdmin, ROLES.paramApprover] },
      {
        href: "/quan-tri/tham-so", module: "pf/masterdata",
        label: "Tham số & phê duyệt",
        icon: "param",
        roles: [ROLES.platformAdmin, ROLES.paramSetter, ROLES.paramApprover],
      },
      {
        // GIS US-CFG-01/02: bảng màu và ngưỡng % bản đồ là tham số có phê duyệt (R-56).
        href: "/quan-tri/hien-thi", module: "pf/masterdata",
        label: "Quy chuẩn hiển thị bản đồ",
        icon: "layers",
        roles: [ROLES.platformAdmin, ROLES.paramSetter, ROLES.paramApprover],
      },
      { href: "/quan-tri/to-chuc", label: "Cơ cấu tổ chức", icon: "org", roles: [ROLES.platformAdmin, ROLES.registryAdmin] },
      { href: "/quan-tri/nhap-nong-ho", label: "Nhập nông hộ từ tệp", icon: "coop", roles: [ROLES.platformAdmin, ROLES.registryAdmin] },
      {
        // D2 (ADR-0052): neo HTX theo MST — HTX tạm (Chưa xác nhận), gắn MST, nhận quyền có chạy thử.
        href: "/quan-tri/neo-htx", module: "pf/cooperative", label: "Neo HTX theo mã số thuế", icon: "coop",
        roles: [ROLES.platformAdmin, ROLES.registryAdmin],
      },
      { href: "/quan-tri/bai-viet", module: "pf/content", label: "Soạn bài viết", icon: "library", roles: [ROLES.contentEditorNational, ROLES.contentEditorProvince] },
      { href: "/quan-tri/nhap-thua", module: "pf/geo", label: "Nhập ranh giới thửa", icon: "upload", roles: [ROLES.platformAdmin, ROLES.geoAdmin] },
      { href: "/quan-tri/ban-do", module: "pf/geo", label: "Biên tập bản đồ", icon: "map", roles: [ROLES.platformAdmin, ROLES.geoAdmin] },
      { href: "/quan-tri/kho-hub", module: "pf/geo", label: "Kho / Hub & dung lượng", icon: "warehouse", roles: [ROLES.platformAdmin, ROLES.geoAdmin] },
      {
        href: "/quan-tri/thuy-he", module: "pf/waterway",
        label: "Mạng đường thủy",
        icon: "water",
        roles: [ROLES.platformAdmin, ROLES.waterwayEditor, ROLES.waterwayPublisher],
      },
      // pf/metrics: định nghĩa chỉ số có phiên bản — chỉ xem.
      { href: "/quan-tri/chi-so", module: "pf/metrics", label: "Định nghĩa chỉ số", icon: "chart", roles: [ROLES.platformAdmin, ROLES.metricsViewer] },
      // pf/mrv: phương pháp ước tính CO2 — đề xuất/duyệt khác người (R-56), chờ khách ký (C-1).
      { href: "/quan-tri/mrv-phuong-phap", module: "pf/mrv", label: "Phương pháp ước tính MRV", icon: "param", roles: [ROLES.platformAdmin, ROLES.paramSetter, ROLES.paramApprover] },
      { href: "/quan-tri/xung-dot", module: "pf/sync", label: "Xung đột đồng bộ", icon: "conflict", roles: [ROLES.platformAdmin, ROLES.conflictResolver] },
      // GIS US-SYNC-01/02: tình trạng luồng đồng bộ tích hợp, hàng chờ lỗi (DLQ).
      { href: "/quan-tri/giam-sat-dong-bo", module: "pf/sync", label: "Giám sát đồng bộ", icon: "sync", roles: [ROLES.platformAdmin, ROLES.deviceAdmin] },
      { href: "/quan-tri/mau-bao-cao", module: "pf/reports", label: "Mẫu & lịch báo cáo", icon: "report", roles: [ROLES.platformAdmin, ROLES.reportsAdmin, ROLES.masterdataAdmin] },
      // G5 (ADR-0022/0023/0024): Public API cho bên thứ ba — tài khoản tenant đơn vị vận hành, Admin API (MFA).
      { href: "/quan-tri/khach-hang-api", label: "Khách hàng Public API", icon: "key", roles: API_CLIENT_ROLES },
      { href: "/quan-tri/su-dung-api", module: "pf/metering", label: "Sử dụng Public API", icon: "chart", roles: METERING_READ_ROLES },
      { href: "/quan-tri/bang-ke", module: "pf/metering", label: "Bảng kê sử dụng", icon: "report", roles: METERING_READ_ROLES },
      // metering_viewer của mọi tenant chủ dữ liệu (chỉ thấy tenant mình) và platform_admin.
      { href: "/quan-tri/nhat-ky-xuat", module: "pf/metering", label: "Nhật ký xuất dữ liệu", icon: "shield", roles: [ROLES.meteringViewer, ROLES.platformAdmin] },
    ],
  },
];

export function canSee(item: NavItem, p: Principal | null, disabled: DisabledModules = NONE): boolean {
  if (!p) return false;
  // W1.4 (R-MOD-08): mÃ n cá»§a module chÆ°a báº­t cho Ä‘Æ¡n vá»‹ khÃ´ng hiá»‡n (backend tráº£ 403 module_not_enabled).
  if (item.module && disabled.has(item.module)) return false;
  const isAdmin = hasRole(p, ROLES.platformAdmin);
  if (item.roles && !hasRole(p, ...item.roles)) return false;
  if (item.levels) {
    // Quản trị nền tảng thấy mọi màn hình (phạm vi All theo ScopeResolver).
    if (isAdmin) return true;
    if (!p.level || !item.levels.includes(p.level)) return false;
  }
  return true;
}

export function visibleNav(p: Principal | null, disabled: DisabledModules = NONE): NavGroup[] {
  return NAV_GROUPS.map((g) => ({ ...g, items: g.items.filter((i) => !i.hidden && canSee(i, p, disabled)) })).filter((g) => g.items.length > 0);
}

/** Táº­p module Ä‘Ã£ táº¯t tá»« GET /v1/me/modules. */
export function disabledModules(items: readonly { module: string; enabled: boolean }[] | null | undefined): DisabledModules {
  return new Set((items ?? []).filter((m) => !m.enabled).map((m) => m.module));
}

/** Trang đích sau đăng nhập: mục đầu tiên được thấy (Cục → /cgh). */
export function homeFor(p: Principal | null, disabled: DisabledModules = NONE): string {
  if (p?.level === "cuc" && !hasRole(p, ROLES.platformAdmin) && !disabled.has("pf/mechanization")) return "/cgh";
  const first = visibleNav(p, disabled)[0]?.items[0];
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
