-- mkg_fieldops — bản sao cục bộ danh mục HTX/thửa của nền tảng và nơi gác phản hồi HTX lỗi vĩnh viễn
-- (kiến trúc v4.1 §39 rủi ro 1, §41.4, §43; R-INT-02: không gọi đồng bộ nền tảng trong đường găng).
--
--   directory_htx_replica    bản sao HTX (cooperative_directory, ADR-0020) theo tenant MKG: mã xã, còn hoạt động.
--   directory_plot_replica   bản sao thửa (plot_directory, ADR-0020): HTX sở hữu, mã xã, trạng thái.
--                            Hai bảng là BẢNG ĐỆM dựng lại được (không phải nguồn sự thật): job
--                            fieldops_directory làm mới qua bộ điều hợp PlatformDirectory; lập/sửa lệnh gom
--                            đối chiếu với bản sao, không gọi nền tảng cho mục đã có (app/directory.go).
--                            present = false: nền tảng trả không có / chưa chia sẻ (403/404) tại as_of.
--                            as_of = thời điểm MKG nhận ảnh chụp từ nền tảng (nền tảng chưa trả mốc riêng;
--                            W5.5 thay bằng as_of của feed); quá max_age (cấu hình) → gắn cờ dữ liệu cũ.
--   htx_feed_parked          phản hồi HTX (luồng /v1/partner/htx-collection/responses) ghi lỗi VĨNH VIỄN
--                            (dữ liệu sai lược đồ / ràng buộc): gác lại kèm lỗi để vận hành xử lý, con trỏ
--                            vẫn tiến — một mục hỏng không chặn cả luồng. Chỉ ghi thêm.
--
-- Số 000033 (bản review ban đầu dùng 000023): 000022 đã dùng ở W2.5 (k_ref), 000023…000032 dành cho các gói
-- kế hoạch v4.1 (W2.6 …) — cấp lại số theo thứ tự gộp (docs/kien-truc-v4.1-ke-hoach.md §1.6).
-- Không có dữ liệu cá nhân (R-04).

-- ---------------------------------------------------------------------------
-- 1. Bản sao danh mục HTX / thửa
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.directory_htx_replica (
  tenant_id     uuid        NOT NULL,
  htx_ref       uuid        NOT NULL,
  present       boolean     NOT NULL,
  commune_code  text        NULL CHECK (commune_code IS NULL OR char_length(commune_code) <= 16),  -- kiểm 5 chữ số ở app/
  active        boolean     NOT NULL DEFAULT false,
  as_of         timestamptz NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, htx_ref),
  CHECK (present OR (commune_code IS NULL AND NOT active))
);
CREATE INDEX directory_htx_replica_as_of ON mkg_fieldops.directory_htx_replica (tenant_id, as_of);
SELECT shared.apply_tenant_rls('mkg_fieldops.directory_htx_replica');

CREATE TABLE mkg_fieldops.directory_plot_replica (
  tenant_id       uuid        NOT NULL,
  plot_ref        uuid        NOT NULL,
  present         boolean     NOT NULL,
  cooperative_ref uuid        NULL,                   -- nil = thửa nháp chưa gán HTX
  commune_code    text        NULL CHECK (commune_code IS NULL OR char_length(commune_code) <= 16),
  status          text        NULL CHECK (status IS NULL OR char_length(status) BETWEEN 1 AND 32),  -- draft | active | retired
  as_of           timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, plot_ref),
  CHECK (present OR (cooperative_ref IS NULL AND commune_code IS NULL AND status IS NULL))
);
CREATE INDEX directory_plot_replica_htx   ON mkg_fieldops.directory_plot_replica (tenant_id, cooperative_ref);
CREATE INDEX directory_plot_replica_as_of ON mkg_fieldops.directory_plot_replica (tenant_id, as_of);
SELECT shared.apply_tenant_rls('mkg_fieldops.directory_plot_replica');

-- ---------------------------------------------------------------------------
-- 2. Phản hồi HTX bị gác (lỗi vĩnh viễn)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_fieldops.htx_feed_parked (
  tenant_id     uuid        NOT NULL,
  feed_cursor   bigint      NOT NULL CHECK (feed_cursor >= 0),
  response_id   uuid        NOT NULL,
  statement_ref uuid        NOT NULL,
  error_code    text        NOT NULL CHECK (char_length(error_code) BETWEEN 1 AND 100),
  error_detail  text        NOT NULL CHECK (char_length(error_detail) <= 1000),
  payload       jsonb       NOT NULL,                 -- phản hồi như bộ điều hợp đọc (đã bỏ ký tự NUL)
  parked_at     timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, feed_cursor, response_id)
);
CREATE INDEX htx_feed_parked_at ON mkg_fieldops.htx_feed_parked (tenant_id, parked_at);
SELECT shared.apply_tenant_rls('mkg_fieldops.htx_feed_parked');

-- ---------------------------------------------------------------------------
-- Quyền
-- ---------------------------------------------------------------------------
-- Bảng đệm: được UPDATE (làm mới); DELETE chỉ để dựng lại.
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_fieldops.directory_htx_replica, mkg_fieldops.directory_plot_replica TO fieldops_app;
-- Gác lỗi: chỉ ghi thêm.
GRANT SELECT, INSERT ON mkg_fieldops.htx_feed_parked TO fieldops_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_fieldops.htx_feed_parked FROM fieldops_app;
