// ĐANG CHỜ HỢP ĐỒNG — module pf/extension (tổng quan, nhiệm vụ hỗ trợ).
import type { DateTime, Decimal, PageQuery, ScopeLevel, Uuid } from "./common";

/** GET /v1/dashboard/kpis?season= — số liệu gom theo phạm vi người gọi. */
export interface DashboardKpis {
  scope: { level: ScopeLevel | null; unit_name: string | null };
  season: string;
  as_of: DateTime;
  /** false khi còn đơn vị chưa gửi số liệu → hiện huy hiệu "Số liệu tạm tính". */
  complete: boolean;
  missing_units: number;
  total_units: number;
  kpis: {
    deployed_area_ha: Decimal | null;
    cooperatives: number | null;
    digitized_plots: number | null;
    overdue_tasks: number | null;
  };
  /** Tiến độ theo đơn vị con trực tiếp (tỉnh với TW, xã với tỉnh/xã). */
  breakdown: {
    unit_code: string;
    unit_name: string;
    deployed_area_ha: Decimal;
    target_area_ha: Decimal;
    reported: boolean;
  }[];
}

export type TaskKind = "hoi_dap" | "ho_tro_ky_thuat" | "phan_anh_khan" | "hau_kiem";
export type TaskStatus = "open" | "in_progress" | "resolved" | "closed";
export type TaskPriority = "low" | "normal" | "high" | "urgent";

/** GET /v1/tasks — nhiệm vụ hỗ trợ (hỏi đáp HTX → KNV, phản ánh khẩn…). */
export interface SupportTask {
  id: Uuid;
  code: string;
  title: string;
  kind: TaskKind;
  status: TaskStatus;
  priority: TaskPriority;
  province_name: string;
  commune_code: string;
  commune_name: string;
  cooperative_id: Uuid | null;
  cooperative_name: string | null;
  /** Tên cán bộ được giao (không phải nông hộ). */
  assignee: string | null;
  created_at: DateTime;
  /** Hạn SLA. */
  due_at: DateTime;
  resolved_at: DateTime | null;
}

export interface TaskQuery extends PageQuery {
  status?: TaskStatus;
  kind?: TaskKind;
  overdue?: boolean;
}
