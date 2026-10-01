-- pf_masterdata KN v7.7 (gói P2): danh mục nông nghiệp FN-AD-04.05 — Giống lúa, Vật tư, Cây trồng,
-- Sâu bệnh dịch hại (gắn cây trồng bị ảnh hưởng), Loại máy móc, Mô hình sản xuất — và loại báo cáo.
-- docs/brd/kn-v7.7-plan.md §2 (pf 000063).
--
-- Danh mục mới (crop, material, production_model, report_type) và thuộc tính mới của pest
-- (affected_crops, severity_thresholds) dùng lại bảng chiếu catalog_item + sổ catalog_change sẵn có
-- (000002): tên danh mục chỉ ràng buộc bằng biểu thức chính quy, thuộc tính là jsonb kiểm ở domain/
-- (attrSpecs) — không cần đổi cấu trúc bảng, không sửa dòng đã ghi (RB-1, R-25).
--
-- Migration này chỉ thêm chỉ mục cho các truy vấn mới:
--   * (trùng tên trong một danh mục — "trùng mã/tên → chặn", FN-AD-04.05 — kiểm ở app/ với so khớp
--     hoa/thường tiếng Việt trong Go; không UNIQUE vì dữ liệu G1/G2 có thể đã có tên trùng;)
--   * cây "nhóm › cây" của danh mục cây trồng (attrs.parent_code);
--   * sâu bệnh theo cây trồng bị ảnh hưởng (attrs.affected_crops, mảng mã dạng chuỗi ngăn cách dấu phẩy
--     → chỉ mục GIN trên mảng tách ra).
CREATE INDEX catalog_item_parent ON pf_masterdata.catalog_item (tenant_id, catalog, (attrs->>'parent_code'))
  WHERE attrs ? 'parent_code';
CREATE INDEX catalog_item_affected_crops ON pf_masterdata.catalog_item
  USING gin (string_to_array(attrs->>'affected_crops', ','))
  WHERE catalog = 'pest' AND attrs ? 'affected_crops';
