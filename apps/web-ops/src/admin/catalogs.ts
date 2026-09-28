// Danh mục dùng chung (pf/masterdata). Danh sách danh mục, tên tiếng Việt và thuộc tính lấy từ
// Admin GET /v1/masterdata/catalogs (G-06); ở đây chỉ còn nhãn hiển thị cho khóa thuộc tính hay gặp.

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
