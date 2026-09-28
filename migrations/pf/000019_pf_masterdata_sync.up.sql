-- pf_masterdata: con trỏ đồng bộ ngoại tuyến (docs/dev/sync-contract.md) cho entity
-- chiều xuống "catalog_item" và "admin_unit".
--
-- sync_seq lấy từ một sequence chung, cấp mới mỗi lần dòng được ghi (thêm, sửa,
-- ngừng dùng/dùng lại mục danh mục; đóng hiệu lực đơn vị hành chính). pf/sync
-- đọc thay đổi có sync_seq > con trỏ theo thứ tự tăng.
--
-- ADD COLUMN ... DEFAULT nextval() cấp số cho các dòng đã có ngay khi thêm cột
-- (không cần UPDATE, R-25; không chạm trigger bảo vệ lịch sử địa giới).

CREATE SEQUENCE pf_masterdata.sync_seq;

ALTER TABLE pf_masterdata.catalog_item
  ADD COLUMN sync_seq bigint NOT NULL DEFAULT nextval('pf_masterdata.sync_seq');
ALTER TABLE pf_masterdata.admin_unit
  ADD COLUMN sync_seq bigint NOT NULL DEFAULT nextval('pf_masterdata.sync_seq');

CREATE UNIQUE INDEX catalog_item_sync ON pf_masterdata.catalog_item (tenant_id, sync_seq);
CREATE UNIQUE INDEX admin_unit_sync   ON pf_masterdata.admin_unit (tenant_id, sync_seq);

-- Mỗi UPDATE lấy số mới — ứng dụng không phải nhớ cập nhật cột này. Trigger chạy
-- sau trg_admin_unit_guard (thứ tự tên), trigger đó không so cột sync_seq.
CREATE FUNCTION pf_masterdata.fn_bump_sync_seq() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.sync_seq := nextval('pf_masterdata.sync_seq');
  RETURN NEW;
END $$;

CREATE TRIGGER trg_sync_seq BEFORE UPDATE ON pf_masterdata.catalog_item
  FOR EACH ROW EXECUTE FUNCTION pf_masterdata.fn_bump_sync_seq();
CREATE TRIGGER trg_sync_seq BEFORE UPDATE ON pf_masterdata.admin_unit
  FOR EACH ROW EXECUTE FUNCTION pf_masterdata.fn_bump_sync_seq();

GRANT USAGE ON SEQUENCE pf_masterdata.sync_seq TO masterdata_app;
