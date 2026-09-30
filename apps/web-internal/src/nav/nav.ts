import { hasRole, ROLES, type Principal } from "@/auth/principal";
import type { IconName } from "@/components/Icon";

export interface NavItem {
  href: string;
  label: string;
  icon: IconName;
  /** Cần ít nhất một vai. Bỏ trống = mọi người dùng đã đăng nhập. */
  roles?: readonly string[];
  /**
   * Màn hình dùng bề mặt QUẢN TRỊ của api-internal (MFA bắt buộc). Phiên chưa qua OTP thấy hướng dẫn
   * đăng nhập lại thay cho nội dung; máy chủ vẫn từ chối (mfa_required).
   */
  admin?: boolean;
}

export interface NavGroup {
  label: string;
  items: NavItem[];
}

const R = ROLES;
const HUB = [R.hubOperator, R.hubApprover];
const MEAS = [R.scaleSupervisor, R.reconciliationOfficer, R.disputeOfficer, R.deviceApprover, R.toleranceApprover];
const FIELD = [R.fieldSupervisor, R.fieldCollector];
const TRIPS = [R.dispatcher, R.driver];
const SALES = [R.salesOfficer, R.salesApprover];
const COST = [R.costAccountant, R.periodCloser, R.chiefAccountant];
const BRIDGE = [R.accountingBridgeOperator, R.chiefAccountant];

/** Menu theo vai. Đây CHỈ là ẩn/hiện — api-internal kiểm quyền từng lời gọi (R-55). */
export const NAV_GROUPS: readonly NavGroup[] = [
  {
    label: "Tổng quan",
    items: [{ href: "/", label: "Tổng quan vận hành", icon: "dashboard" }],
  },
  {
    label: "1. Hub & kho",
    items: [
      { href: "/hub/co-so", label: "Cơ sở & phiên bản", icon: "warehouse", roles: HUB, admin: true },
      { href: "/hub/ton-kho", label: "Lô & tồn kho", icon: "layers", roles: HUB },
      { href: "/hub/xuat-hang", label: "Xuất theo rủi ro", icon: "upload", roles: HUB },
      { href: "/hub/nhan-hang", label: "Nhận hàng", icon: "download", roles: HUB },
      { href: "/hub/kiem-ke", label: "Kiểm kê", icon: "task", roles: HUB },
      { href: "/hub/chuyen-kho", label: "Chuyển kho", icon: "sync", roles: HUB },
      { href: "/hub/tham-so", label: "Tham số rủi ro v2", icon: "param", roles: HUB, admin: true },
      { href: "/hub/hao-hut", label: "Hao hụt", icon: "chart", roles: [...HUB, R.reconciliationOfficer] },
      { href: "/hub/van-cong-bo", label: "Van công bố", icon: "megaphone", roles: [...HUB, R.fieldSupervisor, R.reconciliationOfficer] },
    ],
  },
  {
    label: "2. Cân & đối soát",
    items: [
      { href: "/can/phien-can", label: "Phiên cân", icon: "clock", roles: [R.scaleSupervisor] },
      { href: "/can/phieu-can", label: "Phiếu cân theo lô", icon: "report", roles: MEAS },
      { href: "/can/nhap-so-khach", label: "Nhập số cân khách", icon: "upload", roles: [R.scaleSupervisor] },
      { href: "/can/tham-so", label: "Dung sai & định mức", icon: "param", roles: [R.reconciliationOfficer, R.toleranceApprover], admin: true },
      { href: "/can/doi-soat", label: "Đối soát lô", icon: "approve", roles: [R.reconciliationOfficer, R.disputeOfficer] },
      { href: "/can/tranh-chap", label: "Tranh chấp", icon: "conflict", roles: [R.disputeOfficer, R.reconciliationOfficer] },
      { href: "/can/thiet-bi", label: "Sổ thiết bị cân", icon: "lock", roles: [R.scaleSupervisor, R.deviceApprover], admin: true },
      { href: "/can/suc-khoe", label: "Sức khoẻ thiết bị", icon: "bell", roles: [R.scaleSupervisor, R.deviceApprover] },
    ],
  },
  {
    label: "3. Thu gom hiện trường",
    items: [
      { href: "/gom/lenh-gom", label: "Lệnh gom", icon: "tractor", roles: FIELD },
      { href: "/gom/dem-cuon", label: "Đếm cuộn & xác nhận HTX", icon: "harvest", roles: FIELD },
      { href: "/gom/doi-soat", label: "Đối soát chiều mua", icon: "approve", roles: FIELD },
      { href: "/gom/kg-cuon", label: "kg/cuộn theo HTX", icon: "chart", roles: [R.fieldSupervisor] },
    ],
  },
  {
    label: "4. Vận tải",
    items: [
      { href: "/van-tai/chuyen", label: "Chuyến & chặng", icon: "map", roles: TRIPS },
      { href: "/van-tai/phuong-tien", label: "Danh mục ghe / xe", icon: "water", roles: [R.dispatcher] },
      { href: "/van-tai/canh-bao", label: "Cảnh báo vận tải", icon: "bell", roles: [R.dispatcher] },
      { href: "/van-tai/tham-so", label: "Tham số cước", icon: "param", roles: [R.dispatcher, R.chiefAccountant] },
    ],
  },
  {
    label: "5. Đơn hàng",
    items: [
      { href: "/don-hang/hop-dong", label: "Hợp đồng mua rơm", icon: "journal", roles: SALES },
      { href: "/don-hang/don-mua", label: "Đơn mua & phiếu nhận", icon: "download", roles: SALES },
      { href: "/don-hang/don-ban", label: "Đơn bán & giao nhận", icon: "upload", roles: SALES },
      { href: "/don-hang/tham-so", label: "Tham số đơn hàng", icon: "param", roles: SALES },
    ],
  },
  {
    label: "6. Giá thành & quyết toán",
    items: [
      { href: "/gia-thanh/ky", label: "Kỳ giá thành", icon: "clock", roles: COST },
      { href: "/gia-thanh/lo", label: "Giá thành lô & phân bổ", icon: "chart", roles: COST },
      { href: "/gia-thanh/quyet-toan", label: "Quyết toán HTX", icon: "approve", roles: COST },
      { href: "/gia-thanh/carbon", label: "Chia doanh thu carbon", icon: "leaf", roles: [R.costAccountant] },
      { href: "/gia-thanh/tham-so", label: "Tham số giá thành", icon: "param", roles: [R.costAccountant, R.chiefAccountant], admin: true },
    ],
  },
  {
    label: "7. Kế toán",
    items: [
      { href: "/ke-toan/but-toan", label: "Bút toán tổng hợp", icon: "report", roles: BRIDGE },
      { href: "/ke-toan/anh-xa", label: "Ánh xạ tài khoản", icon: "catalog", roles: BRIDGE, admin: true },
    ],
  },
];

export function canSee(item: NavItem, p: Principal | null): boolean {
  if (!p) return false;
  return !item.roles || hasRole(p, ...item.roles);
}

export function visibleNav(p: Principal | null): NavGroup[] {
  return NAV_GROUPS.map((g) => ({ ...g, items: g.items.filter((i) => canSee(i, p)) })).filter((g) => g.items.length > 0);
}

/** Trang đích sau đăng nhập. */
export function homeFor(p: Principal | null): string {
  return p ? "/" : "/login";
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

/** Màn hình quản trị cần phiên đã qua MFA (hiển thị hướng dẫn thay nội dung). */
export function needsMfa(item: NavItem | undefined, p: Principal | null): boolean {
  return !!item?.admin && !!p && !p.mfa;
}
