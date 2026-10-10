// ============================================================================
// HỢP ĐỒNG cho bề mặt KN v7.7.
//   P1 (danh tính, phiên nhiều vai trò): GET /v1/me/contexts, POST /v1/me/context, header X-AG-Context — ĐÃ CÓ trong
//     contracts/openapi/application.yaml (KNAssignment, KNSessionContext); kn.test.ts kiểm kiểu sinh gán được vào kiểu dưới đây.
//   P2 (menu theo cấu hình):            GET /v1/menu (KHÔNG tham số; vai + phòng ban lấy từ X-AG-Context) → KNMenu
//     (kiểu sinh). Hợp đồng đường dẫn (KN-fix): route = trang web-ops "/kn/<màn>" (vai + phòng ban ở ngữ cảnh,
//     không ở URL); màn Admin theo phòng ban "/kn/phong-ban/{mã}/<màn>"; màn cũ ngoài /kn giữ đường dẫn.
//     Bản giả lập (src/api/mock/kn.ts) = bảng Go qua ca vàng src/api/mock/fixtures/kn-menu.json.
//     `GET /v1/departments/{code}/modules?audience=` (cây một phòng ban) giữ cho Admin/màn "Menu các cấp".
//   P4 (số đếm menu):                    GET /v1/me/menu-badges (khóa = badge_source của KNMenu)
// Xem docs/brd/kn-v7.7-plan.md §3 P1, P2, P6.
// ============================================================================
import type { components } from "@/api/generated/application";
import type { NavIcon } from "@/nav/nav";

/** Menu trên dây (P2, kiểu sinh từ OpenAPI). Khung vẽ MenuTree dựng bằng fromKnMenu (src/kn/menuAdapter.ts). */
export type KnMenu = components["schemas"]["KNMenu"];
export type KnMenuGroup = components["schemas"]["KNMenuGroup"];
export type KnMenuItem = components["schemas"]["KNMenuItem"];

/** Vai nghiệp vụ KN v7.7 (BRD §1.3) — nằm trong role_assignment của P1, KHÔNG phải vai realm. */
export type KnRole = "kn_tw" | "kn_tinh" | "kn_xa" | "admin";

export const KN_ROLE_LABEL: Record<KnRole, string> = {
  kn_tw: "Cấp Quốc gia",
  kn_tinh: "Cấp Tỉnh",
  kn_xa: "Cấp Xã",
  admin: "Admin",
};

/** Trạng thái áp dụng của phòng ban (P2 `department.status`). */
export type DepartmentStatus = "active" | "not_open" | "suspended";

export interface DepartmentRef {
  code: string;
  name: string;
  /** Mô tả ngắn hiện trên thẻ phòng ban (Hình 3b). */
  description: string | null;
  status: DepartmentStatus;
}

export interface CommuneRef {
  code: string;
  name: string;
}

/** Một lần gán vai trò của tài khoản (P1 `role_assignment_current`). */
export interface RoleAssignment {
  assignment_id: string;
  role: KnRole;
  /** Nút của vai trò; null với Admin không gắn Nút ("Toàn hệ thống"). */
  org_ref: string | null;
  /** Tên Nút tổ chức, vd "TTKN tỉnh An Giang". */
  org_name: string;
  /** Cấp quản lý (Q-05) — nhãn, vd "Chuyên viên", "Trưởng phòng". */
  mgmt_level: string | null;
  /** Mã cấp quản lý (danh mục tạm của P1). */
  mgmt_level_code?: string | null;
  /** Xã/phường phụ trách (chỉ Cấp Xã). */
  communes: CommuneRef[];
  /** Phòng ban của vai trò. Admin: rỗng (không chọn phòng ban). */
  departments: DepartmentRef[];
}

/** GET /v1/me/contexts */
export interface MeContexts {
  assignments: RoleAssignment[];
}

/** POST /v1/me/context — thân yêu cầu. Admin gửi department_code = null. */
export interface SelectContextRequest {
  assignment_id: string;
  department_code: string | null;
}

/** POST /v1/me/context — phản hồi; `context_id` gửi lại qua header X-AG-Context. */
export interface SessionContext {
  context_id: string;
  /** null: ngữ cảnh hỗ trợ của Admin không gắn tài khoản (FN-AD-04.07). */
  assignment_id: string | null;
  role: KnRole;
  department_code: string | null;
  department_name: string | null;
  org_ref: string | null;
  org_name: string;
  communes: CommuneRef[];
  /** Trang mặc định theo vai trò (P1 mục 3). */
  default_route: string;
  expires_at: string;
  /** Khác null: Admin đang hỗ trợ (FN-AD-04.07) — hiện dải cảnh báo vàng; mọi thao tác ghi gửi kèm X-AG-Reason. */
  support_session_id?: string | null;
  /** Tài khoản được hỗ trợ. */
  on_behalf_of?: string | null;
}

/** Header mang ngữ cảnh phiên (P1 gap §4.5 phương án A). */
export const CONTEXT_HEADER = "X-AG-Context";

/** Lý do thao tác ghi trong chế độ hỗ trợ (UTF-8, encodeURIComponent; ≥ 10 ký tự). */
export const REASON_HEADER = "X-AG-Reason";

// ---------------------------------------------------------------------------
// Cây menu của khung (dựng từ KnMenu bằng fromKnMenu; menu cũ nav.ts cũng đổi sang dạng này)
// ---------------------------------------------------------------------------

/** Một mục menu. Có `children` → mục cha mở/đóng; không có → liên kết `href`. */
export interface MenuNode {
  key: string;
  label: string;
  icon: NavIcon;
  href?: string;
  children?: MenuNode[];
  /** Khóa nguồn số đếm (vd "extension.report_tasks.todo"); chỉ đặt cho việc cần xử lý. */
  badge_source?: string;
  /**
   * "not_open": phòng ban chưa mở — hiện mờ, nhãn "Chưa mở", không bấm được.
   * "coming_soon": mục của module chưa có (kn.menu/v3) — hiện mờ, nhãn "Sắp có", không bấm được.
   */
  status?: "active" | "not_open" | "coming_soon";
}

export interface MenuGroup {
  key: string;
  label: string;
  items: MenuNode[];
}

export interface MenuTree {
  /** Phiên bản bảng menu tĩnh (P2 định nghĩa trong mã, có phiên bản). */
  version: string;
  role: KnRole | null;
  department_code: string | null;
  /** Trang mặc định của đối tượng (KNMenu.default_route). */
  default_route?: string;
  groups: MenuGroup[];
}

/** GET /v1/me/menu-badges — số đếm theo `badge_source`. Thiếu khóa = 0. */
export interface MenuBadges {
  counts: Record<string, number>;
}

/** Cổng gọi API KN (thật hoặc giả lập). Lỗi → ném ApiProblem. 404 ở listContexts → backend chưa có P1. */
export interface KnApi {
  listContexts(): Promise<MeContexts>;
  selectContext(req: SelectContextRequest): Promise<SessionContext>;
  /** GET /v1/menu theo ngữ cảnh phiên (header X-AG-Context), đã dựng thành cây của khung. */
  menu(): Promise<MenuTree>;
  badges(): Promise<MenuBadges>;
}
