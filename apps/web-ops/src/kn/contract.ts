// ============================================================================
// HỢP ĐỒNG cho bề mặt KN v7.7.
//   P1 (danh tính, phiên nhiều vai trò): GET /v1/me/contexts, POST /v1/me/context, header X-AG-Context — ĐÃ CÓ trong
//     contracts/openapi/application.yaml (KNAssignment, KNSessionContext); kn.test.ts kiểm kiểu sinh gán được vào kiểu dưới đây.
//   P2 (menu theo cấu hình):            GET /v1/departments/{code}/modules?role=…  (Admin: code "_all").
//   P4 (số đếm menu):                    GET /v1/me/menu-badges
// Khi P1/P2 nộp OpenAPI, thay các kiểu dưới đây bằng kiểu sinh (src/api/generated) — giữ tên trường
// snake_case như hợp đồng để việc thay là đổi import. Xem docs/brd/kn-v7.7-plan.md §3 P1, P2, P6.
// ============================================================================
import type { NavIcon } from "@/nav/nav";

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
// Menu theo cấu hình (P2 `GET /v1/departments/{code}/modules`)
// ---------------------------------------------------------------------------

/** Một mục menu. Có `children` → mục cha mở/đóng; không có → liên kết `href`. */
export interface MenuNode {
  key: string;
  label: string;
  icon: NavIcon;
  href?: string;
  children?: MenuNode[];
  /** Khóa nguồn số đếm (vd "report_tasks.todo"); chỉ đặt cho việc cần xử lý. */
  badge_source?: string;
  /** "not_open": phòng ban chưa mở — hiện mờ, nhãn "Chưa mở", không bấm được. */
  status?: "active" | "not_open";
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
  menu(ctx: Pick<SessionContext, "role" | "department_code">): Promise<MenuTree>;
  badges(): Promise<MenuBadges>;
}
