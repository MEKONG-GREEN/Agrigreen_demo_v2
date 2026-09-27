// ĐANG CHỜ HỢP ĐỒNG — module pf/mechanization (Bản đồ CGH, cổng Cục KTHT — chỉ xem).
import type { DateTime, Decimal, PageQuery, Uuid } from "./common";

/** Nguồn dữ liệu theo QT-03: ưu tiên App HTX, dự phòng nhập tay. */
export type DataSource = "app_htx" | "manual" | "none";

/** Ngưỡng cảnh báo cung–cầu (%), tham số có hiệu lực theo vụ (FN-02). null = chưa cấu hình. */
export interface CoverageThresholds {
  /** Đủ: ≥ sufficient_pct (mặc định 85). */
  sufficient_pct: Decimal;
  /** Cần chú ý: ≥ attention_pct và < sufficient_pct (mặc định 60). Dưới là Thiếu. */
  attention_pct: Decimal;
  /** Thừa: ≥ surplus_pct (đề xuất 120). */
  surplus_pct: Decimal;
}

export interface CoverageItem {
  admin_unit_code: string;
  admin_unit_name: string;
  /** Số máy cần = diện tích ÷ định mức có hiệu lực tại vụ. null = chưa đủ dữ liệu. */
  demand_machines: number | null;
  /** Số máy "Hoạt động". */
  capacity_machines: number | null;
  /** Mức đáp ứng %, null = "Chưa có dữ liệu". */
  coverage_pct: Decimal | null;
  source: DataSource;
  updated_at: DateTime | null;
}

/** GET /v1/cgh/coverage?season=&stage=&machine_type=&level=province */
export interface CoverageReport {
  season: string;
  stage: string;
  machine_type: string | null;
  thresholds: CoverageThresholds | null;
  as_of: DateTime;
  items: CoverageItem[];
}

export interface CoverageQuery {
  season?: string;
  stage?: string;
  machine_type?: string;
  level?: "province" | "commune";
}

/** GET /v1/cgh/dashboard?season= — 4 thẻ chỉ số FN-11. null → hiện "—" + chú thích. */
export interface CghDashboard {
  season: string;
  as_of: DateTime;
  total_machines: number | null;
  total_cooperatives: number | null;
  cooperatives_sufficient: number | null;
  cooperatives_short: number | null;
  machines_by_type: { machine_type: string; name: string; count: number }[];
}

/** GET /v1/cgh/priority-cooperatives?season=&stage= — bảng "HTX cần ưu tiên". */
export interface PriorityCooperative {
  cooperative_id: Uuid;
  code: string;
  name: string;
  province_name: string;
  commune_name: string;
  stage: string;
  machine_type: string;
  demand_machines: number;
  capacity_machines: number;
  coverage_pct: Decimal | null;
  /** capacity − demand (âm = thiếu). */
  gap: number;
  source: DataSource;
}

export interface PriorityQuery extends PageQuery {
  season?: string;
  stage?: string;
}
