// Danh mục dùng chung (pf/masterdata). Tên danh mục là enum cố định trong hợp đồng
// (components.parameters.Catalog); backend chưa có route liệt kê danh mục kèm số mục (G-06).
import type { CatalogName } from "@/api/types";

export const CATALOGS: readonly { catalog: CatalogName; name: string; hint: string }[] = [
  { catalog: "rice_variety", name: "Giống lúa", hint: "growth_days, expected_yield_t_per_ha" },
  { catalog: "farm_stage", name: "Khâu sản xuất", hint: "pilot (thí điểm CGH), description" },
  { catalog: "machine_type", name: "Chủng loại máy", hint: "farm_stage (khâu đang dùng)" },
  { catalog: "season", name: "Mùa vụ", hint: "start_month, end_month" },
  { catalog: "pest", name: "Sâu bệnh hại", hint: "kind" },
];

/** Nhãn tiếng Việt cho khóa thuộc tính hay gặp. */
export const ATTR_LABEL: Record<string, string> = {
  growth_days: "Thời gian sinh trưởng (ngày)",
  expected_yield_t_per_ha: "Năng suất kỳ vọng (tấn/ha)",
  pilot: "Thí điểm CGH",
  description: "Mô tả",
  farm_stage: "Khâu",
  start_month: "Tháng bắt đầu",
  end_month: "Tháng kết thúc",
  kind: "Nhóm",
};
