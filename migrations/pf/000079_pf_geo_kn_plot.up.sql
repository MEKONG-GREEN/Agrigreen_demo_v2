-- pf_geo KN v7.7 P7 (FN-XA-03.05 Vẽ ranh giới thửa ruộng — Cán bộ xã trên web/field-app; docs/brd/kn-v7.7-plan.md §2
-- pf 000079). Thuộc tính thửa v7.7 trên bảng chiếu và trên hai sổ (ảnh chụp sau sự việc, chỉ ghi thêm):
--
--   farmer_ref        mã tham chiếu chủ thể "người" trong kho định danh (tên nông dân — RB-3/R-04: KHÔNG lưu tên ở đây)
--   lot_name          tên lô do cán bộ xã đặt
--   note              ghi chú (app kiểm không chứa SĐT/CCCD)
--   declared_area_ha  diện tích khai báo để đối chiếu với diện tích tự tính (diện tích luôn tính trong PostGIS)
--   shape             cách vẽ: polygon | circle | square (máy đổi hình tròn/vuông thành đa giác trước khi gửi)
--   department_code   phòng ban của ngữ cảnh phiên KN đã tạo thửa; NULL = thửa có trước v7.7 / không qua KN
--   on_behalf_of, support_reason   chế độ hỗ trợ của Admin (FN-AD-04.07)
--
-- Sửa thuộc tính = sự kiện 'attributes_changed' mới trên plot_event (ảnh chụp đủ thuộc tính sau sự việc) — Log là
-- hiệu giữa hai ảnh chụp liên tiếp. Sửa ranh giới vẫn là phiên bản mới trên plot_boundary (supersedes_id, RB-1).
-- Cột mới đều NULL được nên dòng đã có không cần cập nhật (R-25).

-- ---------------------------------------------------------------------------
-- 1. Bảng chiếu
-- ---------------------------------------------------------------------------
ALTER TABLE pf_geo.plot
  ADD COLUMN farmer_ref       uuid          NULL,
  ADD COLUMN lot_name         text          NULL CHECK (lot_name IS NULL OR char_length(lot_name) BETWEEN 1 AND 200),
  ADD COLUMN note             text          NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  ADD COLUMN declared_area_ha numeric(14,4) NULL CHECK (declared_area_ha IS NULL OR declared_area_ha > 0),
  ADD COLUMN shape            text          NULL CHECK (shape IS NULL OR shape IN ('polygon', 'circle', 'square')),
  ADD COLUMN department_code  text          NULL CHECK (department_code IS NULL OR department_code ~ '^[a-z][a-z0-9_]{1,39}$');
CREATE INDEX plot_kn_list ON pf_geo.plot (tenant_id, commune_code, ref);

-- ---------------------------------------------------------------------------
-- 2. Sổ phiên bản ranh giới
-- ---------------------------------------------------------------------------
ALTER TABLE pf_geo.plot_boundary
  ADD COLUMN shape            text          NULL CHECK (shape IS NULL OR shape IN ('polygon', 'circle', 'square')),
  ADD COLUMN declared_area_ha numeric(14,4) NULL CHECK (declared_area_ha IS NULL OR declared_area_ha > 0),
  ADD COLUMN on_behalf_of     uuid          NULL,
  ADD COLUMN support_reason   text          NULL CHECK (support_reason IS NULL OR char_length(support_reason) BETWEEN 10 AND 500);

-- ---------------------------------------------------------------------------
-- 3. Sổ biến động thuộc tính
-- ---------------------------------------------------------------------------
ALTER TABLE pf_geo.plot_event
  ADD COLUMN farmer_ref       uuid          NULL,
  ADD COLUMN lot_name         text          NULL CHECK (lot_name IS NULL OR char_length(lot_name) BETWEEN 1 AND 200),
  ADD COLUMN note             text          NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  ADD COLUMN declared_area_ha numeric(14,4) NULL CHECK (declared_area_ha IS NULL OR declared_area_ha > 0),
  ADD COLUMN shape            text          NULL CHECK (shape IS NULL OR shape IN ('polygon', 'circle', 'square')),
  ADD COLUMN department_code  text          NULL CHECK (department_code IS NULL OR department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  ADD COLUMN on_behalf_of     uuid          NULL,
  ADD COLUMN support_reason   text          NULL CHECK (support_reason IS NULL OR char_length(support_reason) BETWEEN 10 AND 500);

ALTER TABLE pf_geo.plot_event DROP CONSTRAINT plot_event_action_check;
ALTER TABLE pf_geo.plot_event ADD CONSTRAINT plot_event_action_check
  CHECK (action IN ('created', 'assigned', 'retired', 'restored', 'attributes_changed'));
