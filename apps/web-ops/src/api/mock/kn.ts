// ============================================================================
// BACKEND GIẢ LẬP cho bề mặt KN v7.7 (kiểu trên dây = kiểu sinh từ OpenAPI, src/kn/contract.ts):
//   GET  /v1/me/contexts                 (P1) — các lần gán vai trò của người gọi
//   POST /v1/me/context                  (P1) — chọn vai trò + phòng ban → context_id
//   GET  /v1/menu                        (P2) — KNMenu theo ngữ cảnh phiên (X-AG-Context) hoặc ?audience=&department=
//   GET  /v1/departments/{code}/modules  (P2) — cây module của một phòng ban (?audience=; Admin: nhóm của phòng ban)
//   GET  /v1/me/menu-badges              (P4) — số đếm việc cần xử lý
// Bảng menu = bảng Go internal/platform/party/domain/menu.go (BRD v7.7 §6, Hình 3c–3f): Vitest
// src/api/mock/kn-menu.contract.test.ts so từng ca với ca vàng fixtures/kn-menu.json do Go sinh
// (AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/party/handler -run TestWebMenuFixture).
// Đăng nhập mật khẩu giả lập ở `mockKnLogin`.
// ============================================================================
import { uuidv7 } from "../idempotency";
import type { DepartmentRef, KnMenu, KnMenuGroup, KnMenuItem, KnRole, MenuTree, RoleAssignment, SessionContext } from "@/kn/contract";
import { fromKnMenu } from "@/kn/menuAdapter";
import { json, problem, type Route } from "./http";
import { findKnAccount, findKnAccountByIdentifier, findKnDepartment, KN_DEPARTMENTS, KN_MENU_PROFILE, knPersona, MOCK_KN_PASSWORD, type KnMenuProfile, type MockKnAccount } from "./kn-accounts";
import type { MockPersona } from "./personas";
import { workSchedulePendingBadge } from "./kn-schedules";
import { cycleBadges } from "./kn-reports/store";

/** domain.MenuVersion. */
export const MENU_VERSION = "kn.menu/v3";

/** Màn Lịch công tác (P5): một đường dẫn cho Cấp Xã (đăng ký) và Cấp Tỉnh/Admin (duyệt) — trang tự chọn theo vai trò. */
export const WORK_SCHEDULE_ROUTE = "/kn/nhiem-vu/lich-cong-tac";
/** Nguồn số đếm menu theo hợp đồng P5 (ExtWorkScheduleCounts.pending_open_weeks) và menu P2 (domain.BadgeScheduleApprovals). */
export const WORK_SCHEDULE_BADGE = "extension.work_schedules.pending";

/** Trang mặc định theo vai trò (FN-CH-00.01) — domain.DefaultRoute (P2) = accounts DefaultRoute (P1). */
export const DEFAULT_ROUTE: Record<KnRole, string> = {
  kn_tw: "/kn/dashboard",
  kn_tinh: "/kn/dashboard",
  kn_xa: "/kn/nhiem-vu/cong-viec",
  admin: "/kn/tong-quan",
};

/** Cờ "Báo cáo đột xuất": chỉ hiện khi phòng ban đã có mẫu loại này (FN-AD-04.06). Mặc định tắt như Hình 3e/3f. */
const ADHOC_FORMS: Record<string, boolean> = { trong_trot: false };

// Khóa số đếm (domain.Badge*).
const B = {
  tasksTodo: "extension.report_tasks.todo",
  notSubmitted: "extension.report_status.not_submitted",
  reviewPending: "extension.submissions.pending_review",
  schedules: WORK_SCHEDULE_BADGE,
  adHocTodo: "extension.report_tasks.ad_hoc_todo",
  adHocStatus: "extension.report_status.ad_hoc_pending",
} as const;

interface Item {
  key: string;
  label: string;
  route?: string;
  icon: string;
  badge?: string;
  /** Chỉ hiện khi có mẫu "đột xuất" (MenuRequirement). */
  adHoc?: boolean;
  /** Module chưa có: hiện mờ "Sắp có" (domain.MenuItem.ComingSoon, kn.menu/v3). */
  soon?: boolean;
  children?: Item[];
}
interface Group {
  key: string;
  label: string;
  items: Item[];
}

const BC_KY = (p: "tw" | "xa"): Item[] => [
  { key: `${p}.bao_cao.tuan`, label: "Báo cáo tuần", route: "/kn/bao-cao/tuan", icon: "file" },
  { key: `${p}.bao_cao.thang`, label: "Báo cáo tháng", route: "/kn/bao-cao/thang", icon: "calendar" },
  { key: `${p}.bao_cao.hang_vu`, label: "Báo cáo hàng vụ", route: "/kn/bao-cao/vu", icon: "sprout" },
];

/** Bộ menu Phòng Trồng trọt (domain.trongTrotMenus). */
const TRONG_TROT: Record<Exclude<KnRole, "admin">, Group[]> = {
  kn_tw: [
    { key: "dieu_hanh", label: "Điều hành", items: [{ key: "tw.dashboard", label: "Dashboard", route: "/kn/dashboard", icon: "dashboard" }] },
    { key: "bao_cao", label: "Báo cáo", items: [...BC_KY("tw"), { key: "tw.bao_cao.nam", label: "Báo cáo năm", route: "/kn/bao-cao/nam", icon: "chart" }] },
  ],
  kn_tinh: [
    {
      key: "dieu_hanh",
      label: "Điều hành",
      items: [
        { key: "tinh.dashboard", label: "Dashboard", route: "/kn/dashboard", icon: "dashboard" },
        {
          key: "tinh.nhiem_vu",
          label: "Nhiệm vụ",
          icon: "task",
          children: [
            { key: "tinh.nhiem_vu.phat_hanh", label: "Phát hành biểu mẫu", route: "/kn/nhiem-vu/phat-hanh", icon: "send" },
            { key: "tinh.nhiem_vu.trang_thai_nop", label: "Trạng thái nộp", route: "/kn/nhiem-vu/trang-thai-nop", icon: "bell", badge: B.notSubmitted },
            { key: "tinh.nhiem_vu.ra_soat", label: "Rà soát & chốt kỳ", route: "/kn/nhiem-vu/ra-soat", icon: "check", badge: B.reviewPending },
            { key: "tinh.nhiem_vu.lich_cong_tac", label: "Lịch công tác", route: WORK_SCHEDULE_ROUTE, icon: "calendar", badge: B.schedules },
          ],
        },
      ],
    },
    {
      key: "bao_cao",
      label: "Báo cáo",
      items: [
        { key: "tinh.bao_cao.tong_hop", label: "Báo cáo tổng hợp các xã", route: "/kn/bao-cao/tong-hop", icon: "layers" },
        { key: "tinh.bao_cao.chi_cuc", label: "Báo cáo Chi cục", route: "/kn/bao-cao/chi-cuc", icon: "file" },
        { key: "tinh.bao_cao.dot_xuat", label: "Báo cáo đột xuất", route: "/kn/bao-cao/dot-xuat", icon: "zap", badge: B.adHocStatus, adHoc: true },
      ],
    },
  ],
  kn_xa: [
    {
      key: "dieu_hanh",
      label: "Điều hành",
      items: [
        {
          key: "xa.nhiem_vu",
          label: "Nhiệm vụ",
          icon: "task",
          children: [
            { key: "xa.nhiem_vu.cong_viec", label: "Công việc cần làm", route: "/kn/nhiem-vu/cong-viec", icon: "bell", badge: B.tasksTodo },
            { key: "xa.nhiem_vu.lich_cong_tac", label: "Lịch công tác", route: WORK_SCHEDULE_ROUTE, icon: "calendar" },
          ],
        },
      ],
    },
    {
      key: "bao_cao",
      label: "Báo cáo",
      items: [
        ...BC_KY("xa"),
        { key: "xa.bao_cao.cua_xa", label: "Báo cáo của xã", route: "/kn/bao-cao/cua-xa", icon: "chart" },
        { key: "xa.bao_cao.dot_xuat", label: "Báo cáo đột xuất", route: "/kn/bao-cao/dot-xuat", icon: "zap", badge: B.adHocTodo, adHoc: true },
      ],
    },
    {
      key: "du_lieu",
      label: "Dữ liệu",
      items: [
        {
          key: "xa.thu_thap",
          label: "Thu thập dữ liệu",
          icon: "database",
          children: [
            { key: "xa.thu_thap.htx", label: "Danh sách HTX", route: "/kn/du-lieu/htx", icon: "coop" },
            { key: "xa.thu_thap.may_moc", label: "Máy móc HTX", route: "/kn/du-lieu/may-moc", icon: "tractor" },
            { key: "xa.thu_thap.thua", label: "Vẽ thửa ruộng", route: "/kn/du-lieu/ve-thua", icon: "map" },
            { key: "xa.thu_thap.vat_tu", label: "Vật tư", route: "/kn/du-lieu/vat-tu", icon: "package" },
          ],
        },
      ],
    },
  ],
};

/** Bộ menu chung cho phòng ban mở bằng cấu hình (domain.genericMenus). */
const GENERIC: Record<Exclude<KnRole, "admin">, Group[]> = {
  kn_tw: TRONG_TROT.kn_tw,
  kn_tinh: [
    {
      key: "dieu_hanh",
      label: "Điều hành",
      items: [
        { key: "tinh.dashboard", label: "Dashboard", route: "/kn/dashboard", icon: "dashboard" },
        {
          key: "tinh.nhiem_vu",
          label: "Nhiệm vụ",
          icon: "task",
          children: [
            { key: "tinh.nhiem_vu.phat_hanh", label: "Phát hành biểu mẫu", route: "/kn/nhiem-vu/phat-hanh", icon: "send" },
            { key: "tinh.nhiem_vu.trang_thai_nop", label: "Trạng thái nộp", route: "/kn/nhiem-vu/trang-thai-nop", icon: "bell", badge: B.notSubmitted },
            { key: "tinh.nhiem_vu.ra_soat", label: "Rà soát & chốt kỳ", route: "/kn/nhiem-vu/ra-soat", icon: "check", badge: B.reviewPending },
          ],
        },
      ],
    },
    {
      key: "bao_cao",
      label: "Báo cáo",
      items: [
        { key: "tinh.bao_cao.tong_hop", label: "Báo cáo tổng hợp các xã", route: "/kn/bao-cao/tong-hop", icon: "layers" },
        { key: "tinh.bao_cao.dot_xuat", label: "Báo cáo đột xuất", route: "/kn/bao-cao/dot-xuat", icon: "zap", badge: B.adHocStatus, adHoc: true },
      ],
    },
  ],
  kn_xa: [
    {
      key: "dieu_hanh",
      label: "Điều hành",
      items: [
        {
          key: "xa.nhiem_vu",
          label: "Nhiệm vụ",
          icon: "task",
          children: [{ key: "xa.nhiem_vu.cong_viec", label: "Công việc cần làm", route: "/kn/nhiem-vu/cong-viec", icon: "bell", badge: B.tasksTodo }],
        },
      ],
    },
    {
      key: "bao_cao",
      label: "Báo cáo",
      items: [
        ...BC_KY("xa"),
        { key: "xa.bao_cao.cua_xa", label: "Báo cáo của xã", route: "/kn/bao-cao/cua-xa", icon: "chart" },
        { key: "xa.bao_cao.dot_xuat", label: "Báo cáo đột xuất", route: "/kn/bao-cao/dot-xuat", icon: "zap", badge: B.adHocTodo, adHoc: true },
      ],
    },
  ],
};

const soon = (key: string, label: string, route: string, icon: string): Item => ({ key, label, route, icon, soon: true });

/** Nhóm "Dùng chung" cuối menu mọi đối tượng (domain.SharedGroup, HCTH FN-13). */
const SHARED: Group = { key: "dung_chung", label: "Dùng chung", items: [soon("chung.contact", "Contact", "/kn/dung-chung/contact", "phone")] };
/** W11.07 (Q-067): mục "Dùng chung" thêm theo đối tượng — server chỉ trả cho lãnh đạo (Needs manager); bản giả lập trả đầy đủ như ca vàng. */
const SHARED_BY_AUDIENCE: Partial<Record<KnRole, Item[]>> = {
  // W11.22 (pf/hrm): đã mở — hồ sơ trong phạm vi "đơn vị mình".
  kn_tinh: [{ key: "tinh.chung.nhan_su_phong", label: "Nhân sự của phòng", route: "/kn/nhan-su/cua-phong", icon: "users" }],
};

/** Nhóm "Nghiệp vụ" theo khuôn × đối tượng (domain.deptBusiness) — mục "Sắp có" trừ mục gói đã làm (W11.21). */
const BUSINESS: Partial<Record<KnMenuProfile, Partial<Record<Exclude<KnRole, "admin">, Item[]>>>> = {
  chan_nuoi_thuy_san: {
    kn_tw: [soon("tw.cnts.du_an_knqg", "Dự án KNQG", "/kn/cnts/du-an-knqg", "report")],
    kn_tinh: [
      soon("tinh.cnts.canh_bao", "Cảnh báo", "/kn/cnts/canh-bao", "zap"),
      soon("tinh.cnts.duyet_bao_cao", "Duyệt báo cáo", "/kn/cnts/duyet-bao-cao", "approve"),
      { key: "tinh.cnts.bieu_mau", label: "Biểu mẫu & cấp quyền", route: "/kn/cnts/bieu-mau", icon: "table" }, // W11.42
      soon("tinh.cnts.lich", "Lịch trực KSGM & lịch tiêm", "/kn/cnts/lich", "calendar"),
      soon("tinh.cnts.chien_dich", "Chiến dịch điều tra", "/kn/cnts/chien-dich", "megaphone"),
      soon("tinh.cnts.ban_do_dich", "Bản đồ vùng dịch", "/kn/cnts/ban-do-dich", "map"),
      soon("tinh.cnts.du_an_knqg", "Dự án KNQG / Điểm trình diễn", "/kn/cnts/du-an-knqg", "report"),
      soon("tinh.cnts.quy_trinh", "Quy trình, văn bản", "/kn/cnts/quy-trinh", "sop"),
      { key: "tinh.cnts.danh_muc", label: "Danh mục CNTS", route: "/kn/cnts/danh-muc", icon: "catalog" }, // W11.41
    ],
    kn_xa: [
      { key: "xa.cnts.bao_cao_ky", label: "Báo cáo kỳ CNTS", route: "/kn/cnts/bao-cao-ky", icon: "task" }, // W11.42
      soon("xa.cnts.lich", "Lịch trực của tôi", "/kn/cnts/lich", "calendar"),
      soon("xa.cnts.nhat_ky", "Hộ mô hình & nhật ký", "/kn/cnts/nhat-ky", "journal"),
      soon("xa.cnts.ban_do_dich", "Bản đồ vùng dịch", "/kn/cnts/ban-do-dich", "map"),
      soon("xa.cnts.diem_trinh_dien", "Điểm trình diễn", "/kn/cnts/du-an-knqg", "report"),
      soon("xa.cnts.quy_trinh", "Quy trình, văn bản", "/kn/cnts/quy-trinh", "sop"),
    ],
  },
  ke_hoach_tai_chinh: {
    kn_tw: [soon("tw.khtc.bao_cao_tinh", "Báo cáo Tỉnh (KHTC)", "/kn/khtc/bao-cao-da-xuat", "report")],
    kn_tinh: [
      { key: "tinh.khtc.ky_bao_cao", label: "Kỳ báo cáo & phiếu các phòng", route: "/kn/khtc/ky-bao-cao", icon: "task" }, // W11.62
      { key: "tinh.khtc.ke_hoach_nam", label: "Kế hoạch năm", route: "/kn/khtc/ke-hoach-nam", icon: "calendar" }, // W11.61
      soon("tinh.khtc.so_lieu_ngoai", "Số liệu nguồn ngoài", "/kn/khtc/so-lieu-ngoai", "database"),
      soon("tinh.khtc.mau_bao_cao", "Mẫu báo cáo KHTC", "/kn/khtc/mau-bao-cao", "table"),
      soon("tinh.khtc.tien_do", "Tiến độ các chương trình", "/kn/khtc/tien-do", "chart"),
      soon("tinh.khtc.da_xuat", "Báo cáo đã xuất", "/kn/khtc/bao-cao-da-xuat", "report"),
    ],
  },
  thong_tin_truyen_thong: {
    kn_tw: [
      soon("tw.bao_gia.bang_gia", "Bảng giá tổng hợp", "/kn/bao-gia/bang-gia", "chart"),
      soon("tw.bao_gia.bien_dong", "Báo cáo biến động giá", "/kn/bao-gia/bien-dong", "layers"),
      soon("tw.bao_gia.gia_thanh", "Giá thành sản xuất", "/kn/bao-gia/gia-thanh", "report"),
    ],
    kn_tinh: [
      soon("tinh.bao_gia.tien_do", "Tiến độ nhập giá", "/kn/bao-gia/tien-do", "bell"),
      { key: "tinh.bao_gia.kiem_duyet", label: "Kiểm duyệt & công bố", route: "/kn/bao-gia/kiem-duyet", icon: "approve" }, // W11.75
      soon("tinh.bao_gia.bang_gia", "Bảng giá tổng hợp", "/kn/bao-gia/bang-gia", "chart"),
      soon("tinh.bao_gia.bien_dong", "Báo cáo biến động giá", "/kn/bao-gia/bien-dong", "layers"),
      soon("tinh.bao_gia.dieu_tra", "Điều tra giá thành", "/kn/bao-gia/dieu-tra", "season"),
      soon("tinh.bao_gia.mat_hang", "Mặt hàng", "/kn/bao-gia/mat-hang", "package"),
      // W11.07: chỉ QTNV ở server; W11.72: đã mở (màn lịch báo giá W11.74).
      { key: "tinh.bao_gia.cau_hinh", label: "Cấu hình báo giá", route: "/kn/bao-gia/cau-hinh", icon: "param" },
    ],
    kn_xa: [
      // W11.75: nhập báo giá, yêu cầu xác minh, lịch sử báo giá đã mở.
      { key: "xa.bao_gia.nhap", label: "Nhập báo giá", route: "/kn/bao-gia/nhap", icon: "upload" },
      { key: "xa.bao_gia.xac_minh", label: "Yêu cầu xác minh", route: "/kn/bao-gia/xac-minh", icon: "check" },
      { key: "xa.bao_gia.lich_su", label: "Lịch sử báo giá", route: "/kn/bao-gia/lich-su", icon: "journal" },
      soon("xa.bao_gia.bang_gia", "Bảng giá tổng hợp", "/kn/bao-gia/bang-gia", "chart"),
    ],
  },
  hanh_chinh_tong_hop: {
    kn_tinh: [
      { key: "tinh.hcth.ho_so", label: "Hồ sơ nhân sự", route: "/kn/nhan-su/ho-so", icon: "users" }, // W11.22 (pf/hrm)
      soon("tinh.hcth.kho_tai_lieu", "Hồ sơ dữ liệu", "/kn/nhan-su/kho-tai-lieu", "library"),
      soon("tinh.hcth.danh_gia", "Đánh giá", "/kn/nhan-su/danh-gia", "approve"),
      { key: "tinh.hcth.danh_muc", label: "Danh mục nhân sự", route: "/kn/nhan-su/danh-muc", icon: "catalog" }, // W11.21 (pf/hrm)
    ],
    kn_xa: [soon("xa.hcth.danh_gia", "Đánh giá của tôi", "/kn/nhan-su/danh-gia-cua-toi", "approve")],
  },
};

/** Mục "Sắp có" thêm vào mục con phòng ban của Admin (domain.adminDeptBusiness). */
const ADMIN_BUSINESS: Partial<Record<KnMenuProfile, (base: string) => Item[]>> = {
  chan_nuoi_thuy_san: (b) => [
    { key: "admin.pb.cnts_danh_muc", label: "Danh mục CNTS", route: `${b}/danh-muc-cnts`, icon: "catalog" }, // W11.41
    { key: "admin.pb.cnts_bieu_ky", label: "Biểu kỳ Cấp Xã", route: `${b}/bieu-ky-cnts`, icon: "table" }, // W11.42
  ],
  ke_hoach_tai_chinh: (b) => [
    { key: "admin.pb.khtc_ke_hoach", label: "Kế hoạch năm", route: `${b}/ke-hoach-nam`, icon: "calendar" }, // W11.61
    { key: "admin.pb.khtc_danh_muc", label: "Danh mục KHTC", route: `${b}/danh-muc-khtc`, icon: "catalog" }, // W11.61
  ],
  // W11.72: Xã báo giá + Kỳ báo giá & giờ khóa đã mở.
  thong_tin_truyen_thong: (b) => [
    { key: "admin.pb.bao_gia_xa", label: "Xã báo giá", route: "/kn/bao-gia/xa-bao-gia", icon: "map" },
    { key: "admin.pb.bao_gia_lich", label: "Kỳ báo giá & giờ khóa", route: `${b}/bao-gia-lich`, icon: "calendar" },
    soon("admin.pb.bao_gia_mat_hang", "Mặt hàng", "/kn/bao-gia/mat-hang", "package"),
  ],
  hanh_chinh_tong_hop: (b) => [
    { key: "admin.pb.hcth_danh_muc", label: "Danh mục nhân sự", route: `${b}/danh-muc-nhan-su`, icon: "catalog" }, // W11.21 (pf/hrm)
    { key: "admin.pb.hcth_phan_quyen", label: "Phân quyền hồ sơ", route: `${b}/phan-quyen-ho-so`, icon: "shield" }, // W11.22 (pf/hrm)
    soon("admin.pb.hcth_thu_muc", "Thư mục tài liệu", `${b}/thu-muc-tai-lieu`, "library"),
  ],
};

/** Nhóm của khuôn × đối tượng (domain.DepartmentMenu): Trồng trọt; khuôn riêng = chung + "Nghiệp vụ" sau "Điều hành". */
function profileGroups(profile: KnMenuProfile, audience: Exclude<KnRole, "admin">): Group[] {
  if (profile === "trong_trot") return TRONG_TROT[audience];
  const biz = BUSINESS[profile]?.[audience] ?? [];
  return GENERIC[audience].flatMap((g) => (g.key === "dieu_hanh" && biz.length ? [g, { key: "nghiep_vu", label: "Nghiệp vụ", items: biz }] : [g]));
}

/** Mục con của phòng ban trong nhóm "Phòng ban" của Admin (domain.adminDeptItems + adminDeptBusiness). */
function adminDeptItems(code: string, profile: KnMenuProfile): Item[] {
  const base = `/kn/phong-ban/${code}`;
  return [
    { key: "admin.pb.theo_doi_nop", label: "Theo dõi nộp báo cáo", route: `${base}/theo-doi-nop`, icon: "bell" },
    { key: "admin.pb.tong_hop", label: "Báo cáo tổng hợp các xã", route: `${base}/tong-hop`, icon: "layers" },
    { key: "admin.pb.du_lieu_bao_cao", label: "Dữ liệu báo cáo các xã", route: `${base}/du-lieu-bao-cao`, icon: "file" },
    ...(profile === "trong_trot"
      ? [
          { key: "admin.pb.thu_thap", label: "Thu thập dữ liệu", route: `${base}/thu-thap`, icon: "database" },
          // FN-TI-02.07 (P5): Admin xem lịch công tác (chỉ đọc) — cùng màn của Cấp Tỉnh, chưa tách theo phòng ban.
          { key: "admin.pb.lich_cong_tac", label: "Lịch công tác", route: WORK_SCHEDULE_ROUTE, icon: "calendar" },
        ]
      : []),
    { key: "admin.pb.danh_muc", label: "Danh mục nông nghiệp", route: `${base}/danh-muc`, icon: "package" },
    // P3b: trình thiết kế mẫu báo cáo dạng lưới Excel (FN-AD-04.06, Hình 27).
    { key: "admin.pb.mau_bao_cao", label: "Mẫu báo cáo", route: `${base}/mau-bao-cao`, icon: "table" },
    ...(ADMIN_BUSINESS[profile]?.(base) ?? []),
  ];
}

const ADMIN_OVERVIEW: Group = {
  key: "dieu_hanh",
  label: "Điều hành",
  items: [{ key: "admin.tong_quan", label: "Tổng quan hệ thống", route: "/kn/tong-quan", icon: "dashboard" }],
};
const ADMIN_SYSTEM: Group = {
  key: "quan_tri",
  label: "Quản trị hệ thống",
  items: [
    { key: "admin.qt.dvhc", label: "Đơn vị hành chính", route: "/kn/quan-tri/don-vi-hanh-chinh", icon: "map" },
    { key: "admin.qt.co_cau", label: "Cơ cấu tổ chức 3 cấp", route: "/kn/quan-tri/co-cau-to-chuc", icon: "org" },
    { key: "admin.qt.phong_ban", label: "Phòng ban", route: "/kn/quan-tri/phong-ban", icon: "building" },
    { key: "admin.qt.nguoi_dung", label: "Người dùng", route: "/kn/quan-tri/nguoi-dung", icon: "users" },
    { key: "admin.qt.menu_cac_cap", label: "Menu các cấp", route: "/kn/quan-tri/menu-cac-cap", icon: "key" },
    {
      // Q-13 (tạm, chờ khách): công cụ quản trị nền tảng đã có đặt trong nhóm Quản trị hệ thống.
      key: "admin.qt.cong_cu",
      label: "Công cụ nền tảng (tạm)",
      icon: "param",
      children: [
        { key: "admin.qt.cong_cu.tham_so", label: "Tham số & phê duyệt", route: "/quan-tri/tham-so", icon: "param" },
        { key: "admin.qt.cong_cu.tai_khoan", label: "Tài khoản cán bộ (G1)", route: "/quan-tri/tai-khoan", icon: "user" },
        { key: "admin.qt.cong_cu.to_chuc", label: "Cây tổ chức (G1)", route: "/quan-tri/to-chuc", icon: "org" },
        // Màn G1 (mẫu xuất có phiên bản + lịch gửi email) — giữ nguyên, không xóa tính năng.
        { key: "admin.qt.cong_cu.mau_xuat", label: "Mẫu xuất & lịch gửi", route: "/quan-tri/mau-bao-cao", icon: "report" },
        { key: "admin.qt.cong_cu.xung_dot", label: "Xung đột đồng bộ", route: "/quan-tri/xung-dot", icon: "conflict" },
        { key: "admin.qt.cong_cu.giam_sat", label: "Giám sát đồng bộ", route: "/quan-tri/giam-sat-dong-bo", icon: "sync" },
        { key: "admin.qt.cong_cu.thiet_bi", label: "Đồng bộ thiết bị", route: "/dong-bo", icon: "sync" },
        // G5: màn Public API / đo lường / nhật ký xuất (tự kiểm vai).
        { key: "admin.qt.cong_cu.khach_hang_api", label: "Khách hàng Public API", route: "/quan-tri/khach-hang-api", icon: "key" },
        { key: "admin.qt.cong_cu.su_dung_api", label: "Sử dụng Public API", route: "/quan-tri/su-dung-api", icon: "chart" },
        { key: "admin.qt.cong_cu.bang_ke", label: "Bảng kê sử dụng", route: "/quan-tri/bang-ke", icon: "report" },
        { key: "admin.qt.cong_cu.nhat_ky_xuat", label: "Nhật ký xuất dữ liệu", route: "/quan-tri/nhat-ky-xuat", icon: "shield" },
      ],
    },
  ],
};

const STATUS_LABEL: Record<DepartmentRef["status"], string> = { active: "Đang áp dụng", not_open: "Chưa mở", suspended: "Ngưng hoạt động" };

function wire(items: readonly Item[], enabled: boolean, adHoc: boolean): KnMenuItem[] {
  const out: KnMenuItem[] = [];
  for (const it of items) {
    if (it.adHoc && !adHoc) continue;
    const children = wire(it.children ?? [], enabled, adHoc);
    if (it.children?.length && !children.length) continue;
    const base: KnMenuItem = { key: it.key, label: it.label, route: it.route ?? null, icon: it.icon, badge_source: it.badge ?? null, enabled, children };
    out.push(it.soon ? { ...base, enabled: false, status: "coming_soon", status_label: "Sắp có" } : base);
  }
  return out;
}

const profileOf = (code: string): KnMenuProfile => KN_MENU_PROFILE[code] ?? "generic";
const deptWire = (d: DepartmentRef): NonNullable<KnMenu["department"]> => ({ code: d.code, name: d.name, status: d.status, menu_profile: profileOf(d.code) });

function adminDeptItem(d: DepartmentRef): KnMenuItem {
  const enabled = d.status === "active";
  return {
    key: `phong_ban.${d.code}`,
    label: d.name,
    route: null,
    icon: profileOf(d.code) === "trong_trot" ? "sprout" : "building",
    badge_source: null,
    enabled,
    status: d.status,
    status_label: STATUS_LABEL[d.status],
    children: wire(adminDeptItems(d.code, profileOf(d.code)), enabled, false),
  };
}

/**
 * KNMenu trên dây (= Go app.DepartmentMenu / app.AdminMenu). Admin: Điều hành / Phòng ban (mọi phòng ban, chưa mở
 * enabled = false) / Quản trị hệ thống / Dùng chung (kn.menu/v3). Phòng ban chưa mở → 409 ở handler.
 */
export function knMenu(audience: KnRole, departmentCode: string | null, opts: { adHoc?: boolean; orgRef?: string | null } = {}): KnMenu {
  if (audience === "admin") {
    const groups: KnMenuGroup[] = [
      { key: ADMIN_OVERVIEW.key, label: ADMIN_OVERVIEW.label, items: wire(ADMIN_OVERVIEW.items, true, false) },
      { key: "phong_ban", label: "Phòng ban", items: KN_DEPARTMENTS.map(adminDeptItem) },
      { key: ADMIN_SYSTEM.key, label: ADMIN_SYSTEM.label, items: wire(ADMIN_SYSTEM.items, true, false) },
      { key: SHARED.key, label: SHARED.label, items: wire(SHARED.items, true, false) },
    ];
    return { menu_version: MENU_VERSION, audience, org_ref: opts.orgRef ?? null, department: null, default_route: DEFAULT_ROUTE.admin, groups };
  }
  const d = KN_DEPARTMENTS.find((x) => x.code === (departmentCode ?? "trong_trot")) ?? KN_DEPARTMENTS[0]!;
  const adHoc = opts.adHoc ?? ADHOC_FORMS[d.code] ?? false;
  const shared: Group = { ...SHARED, items: [...SHARED.items, ...(SHARED_BY_AUDIENCE[audience] ?? [])] };
  const groups = [...profileGroups(profileOf(d.code), audience), shared]
    .map((g) => ({ key: g.key, label: g.label, items: wire(g.items, true, adHoc) }))
    .filter((g) => g.items.length > 0);
  return { menu_version: MENU_VERSION, audience, org_ref: opts.orgRef ?? null, department: deptWire(d), default_route: DEFAULT_ROUTE[audience], groups };
}

/** Cây menu của khung (cho kiểm thử khung/Sidebar). */
export function knMenuFor(role: KnRole, departmentCode: string | null): MenuTree {
  return fromKnMenu(knMenu(role, departmentCode));
}

/** Số đếm giả lập theo vai trò (Hình 3e: 15 / 3 / 2; Hình 3f: 6). 0 → menu ẩn số đếm. */
const BADGES: Record<KnRole, Record<string, number>> = {
  kn_tw: {},
  // Lịch chờ duyệt (B.schedules) tính từ sổ lịch giả lập P5 ở handler menu-badges.
  // Số đếm vòng đời báo cáo tính từ sổ giả lập P4 (kn-reports/store.ts cycleBadges).
  kn_tinh: {},
  kn_xa: {},
  admin: {},
};

// --------------------------------------------------------------------------
// Ngữ cảnh phiên (giữ trong bộ nhớ tab, như mọi trạng thái giả lập khác)
// --------------------------------------------------------------------------
/** Phiên hỗ trợ của Admin (FN-AD-04.07): thay cho ai, vai trò, lý do mở phiên. */
export interface KnSupportInfo {
  sessionId: string;
  /** Tài khoản được hỗ trợ (id tài khoản giả lập) — null: hỗ trợ theo vai trò/xã, không gắn tài khoản. */
  onBehalfOf: string | null;
  onBehalfOfName: string | null;
  reason: string;
  startedAt: string;
  endedAt: string | null;
}
export interface StoredContext {
  accountId: string;
  assignment: RoleAssignment;
  ctx: SessionContext;
  support?: KnSupportInfo;
}
const CONTEXTS = new Map<string, StoredContext>();

/** Ngữ cảnh đã lưu (backend giả lập P4/FN-AD-04.07 đọc vai trò, phòng ban, xã, phiên hỗ trợ). */
export function knStoredContext(contextId: string | null | undefined): StoredContext | undefined {
  return contextId ? CONTEXTS.get(contextId) : undefined;
}

/** Lưu ngữ cảnh hỗ trợ do POST /v1/accounts/support-sessions tạo (kn-admin.ts). */
export function registerKnContext(stored: StoredContext): void {
  CONTEXTS.set(stored.ctx.context_id, stored);
}

/** Thoát hỗ trợ: ngữ cảnh của phiên hết hiệu lực ngay. */
export function dropSupportContexts(sessionId: string): void {
  for (const [id, st] of CONTEXTS) if (st.support?.sessionId === sessionId) CONTEXTS.delete(id);
}

function accountOf(p: MockPersona): MockKnAccount | undefined {
  return findKnAccount(p.id);
}

/**
 * Persona theo header X-AG-Context (mô phỏng middleware P1): chỉ nhận context của chính tài khoản
 * trong token. Context lạ/của người khác → giữ persona gốc; handler trả 403 trước đó (contextProblem).
 */
export function personaForContext(base: MockPersona, contextId: string | null): MockPersona {
  if (!contextId) return base;
  const stored = CONTEXTS.get(contextId);
  const account = accountOf(base);
  if (!stored || !account || stored.accountId !== account.id) return base;
  return knPersona(account, stored.assignment);
}

/**
 * Như middleware P1 thật: header X-AG-Context lạ, không phải UUID hoặc của người khác → 403
 * accounts.context_invalid (không bao giờ âm thầm bỏ qua header). null = hợp lệ hoặc không có header.
 */
export function contextProblem(base: MockPersona, contextId: string | null): Response | null {
  if (!contextId) return null;
  const stored = CONTEXTS.get(contextId);
  if (!stored || stored.accountId !== accountOf(base)?.id) {
    return problem(403, "accounts.context_invalid", "Ngữ cảnh phiên không hợp lệ hoặc không thuộc người gọi");
  }
  return null;
}

/** Context đã lưu (cho kiểm thử và khôi phục sau tải lại trong cùng tab). */
export function knContext(contextId: string): SessionContext | undefined {
  return CONTEXTS.get(contextId)?.ctx;
}

const NO_STORE = { "Cache-Control": "no-store" };

/**
 * Đối tượng + phòng ban của lời gọi menu như app.resolveAudience (P2): ngữ cảnh phiên (X-AG-Context) thắng;
 * ?audience= (tên cũ ?role=) khác ngữ cảnh → 403 party.menu_audience_forbidden (trừ Admin hỗ trợ mọi cấp).
 * Không có ngữ cảnh: bắt buộc ?audience=.
 */
function menuQuery(
  request: Request,
  url: URL,
  pathDepartment?: string,
): Response | { audience: KnRole; department: string | null; orgRef: string | null } {
  const asked = (url.searchParams.get("audience") ?? url.searchParams.get("role")) as KnRole | null;
  if (asked && !(asked in DEFAULT_ROUTE)) return problem(422, "validation_failed", "audience: kn_tw | kn_tinh | kn_xa | admin");
  let department = pathDepartment ?? url.searchParams.get("department");
  const stored = CONTEXTS.get(request.headers.get("X-AG-Context") ?? "");
  if (stored) {
    const role = stored.ctx.role;
    if (role !== "admin" && ((asked && asked !== role) || (department && department !== stored.ctx.department_code))) {
      return problem(403, "party.menu_audience_forbidden", "chỉ trả menu của ngữ cảnh phiên đang dùng");
    }
    const audience = asked ?? role;
    if (!department && audience === role) department = stored.ctx.department_code;
    return { audience, department, orgRef: stored.ctx.org_ref };
  }
  if (!asked) return problem(422, "validation_failed", "audience: bắt buộc khi không có ngữ cảnh phiên");
  return { audience: asked, department, orgRef: null };
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
        // Server: 24 giờ hoặc tới khi lần gán hết hiệu lực (P1 domain.ContextTTL).
        expires_at: new Date(Date.now() + 24 * 3_600_000).toISOString(),
        support_session_id: null,
        on_behalf_of: null,
      };
      CONTEXTS.set(ctx.context_id, { accountId: account.id, assignment: asg, ctx });
      return json(ctx, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/menu$/,
    handler: ({ request, url }) => {
      const q = menuQuery(request, url);
      if (q instanceof Response) return q;
      if (q.audience === "admin") return json(knMenu("admin", null, { orgRef: q.orgRef }), 200, NO_STORE);
      const d = q.department ? findKnDepartment(q.department) : KN_DEPARTMENTS.find((x) => x.status === "active");
      if (!d) return problem(404, "party.department_not_found", "Department not found");
      if (d.status !== "active") return problem(409, "party.department_not_open", "Department not open");
      return json(knMenu(q.audience, d.code, { orgRef: q.orgRef }), 200, NO_STORE);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/departments\/([\w-]+)\/modules$/,
    handler: ({ params, request, url }) => {
      const q = menuQuery(request, url, params[0] ?? "");
      if (q instanceof Response) return q;
      const d = findKnDepartment(params[0] ?? "");
      if (!d) return problem(404, "party.department_not_found", "Department not found");
      const code = d.code;
      if (q.audience === "admin") {
        // Admin: nhóm của MỘT phòng ban (kể cả chưa mở — mục con enabled = false), như app.DepartmentModules.
        const item = knMenu("admin", null).groups.find((g) => g.key === "phong_ban")!.items.find((i) => i.key === `phong_ban.${code}`)!;
        return json(
          { menu_version: MENU_VERSION, audience: "admin", org_ref: q.orgRef, department: { code: d.code, name: d.name, status: d.status, menu_profile: profileOf(d.code) },
            default_route: DEFAULT_ROUTE.admin, groups: [{ key: item.key, label: d.name, items: item.children }] } satisfies KnMenu,
          200,
          NO_STORE,
        );
      }
      if (d.status !== "active") return problem(409, "party.department_not_open", "Department not open");
      return json(knMenu(q.audience, d.code, { orgRef: q.orgRef }), 200, NO_STORE);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/me\/menu-badges$/,
    handler: ({ request, persona }) => {
      const contextId = request.headers.get("X-AG-Context") ?? "";
      const stored = CONTEXTS.get(contextId);
      if (!stored) return json({ counts: {} });
      const counts: Record<string, number> = { ...BADGES[stored.assignment.role], ...cycleBadges(contextId) };
      // Lịch chờ duyệt: tính từ sổ lịch giả lập (P5), không cố định.
      if (stored.assignment.role === "kn_tinh") counts[WORK_SCHEDULE_BADGE] = workSchedulePendingBadge(persona, contextId);
      return json({ counts });
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
