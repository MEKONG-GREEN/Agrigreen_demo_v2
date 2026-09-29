-- pf_sync / pf_notify G2: giám sát đồng bộ tích hợp và cảnh báo Admin (GIS US-SYNC-01/02,
-- CGH FN-07 BR-04 / AC-02 / AC-03).
--
-- 1. pf_sync.integration_failure: mỗi lần một consumer (worker) xử lý sự kiện THẤT BẠI một
--    dòng — thời điểm, consumer (nguồn), loại sự kiện, mã lỗi đã chuẩn hóa (mã problem hoặc
--    SQLSTATE, KHÔNG có thông điệp tự do nên không lọt dữ liệu cá nhân — R-14), lần giao thứ
--    mấy và đã vào hàng chờ lỗi (DLQ) chưa. Lần xử lý THÀNH CÔNG đã có ở shared.inbox
--    (processed_at, consumer, event_type) nên không chép lại. Màn "Giám sát đồng bộ" gộp hai
--    nguồn theo consumer. Bảng chỉ ghi thêm (không phải sổ băm: dữ liệu vận hành).
--    Chỉ mục duy nhất một dòng dead_lettered cho (sự kiện, consumer): cảnh báo Admin
--    pf.sync.integration.dead_lettered phát đúng một lần.
-- 2. Loại thông báo mới: integration_dead_lettered (sự kiện vào DLQ → Admin nền tảng) và
--    area_feed_stale (dữ liệu diện tích vụ App HTX quá ngưỡng độ tươi → Admin CGH). Ràng
--    buộc kind được MỞ RỘNG từ định nghĩa hiện có (không liệt kê lại) để không làm mất loại
--    của migration khác chạy trước.

CREATE TABLE pf_sync.integration_failure (
  tenant_id      uuid        NOT NULL,
  id             uuid        NOT NULL,                   -- UUIDv7
  consumer       text        NOT NULL CHECK (consumer ~ '^[a-z0-9_.]{1,120}$'),
  event_id       uuid        NOT NULL,
  event_type     text        NOT NULL CHECK (event_type ~ '^(pf|mkg|vault|edge)\.[a-z_]+\.[a-z_]+\.[a-z_]+$'),
  error_code     text        NOT NULL CHECK (error_code ~ '^[a-z0-9_.:]{1,120}$'),
  delivery_count integer     NOT NULL CHECK (delivery_count >= 0),
  dead_lettered  boolean     NOT NULL,
  event_time     timestamptz NOT NULL,                   -- occurred_at của sự kiện
  recorded_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id)
);
CREATE INDEX integration_failure_recent ON pf_sync.integration_failure (tenant_id, consumer, recorded_at DESC);
CREATE UNIQUE INDEX integration_failure_dead_once ON pf_sync.integration_failure (tenant_id, event_id, consumer) WHERE dead_lettered;
SELECT shared.apply_tenant_rls('pf_sync.integration_failure');
CREATE TRIGGER trg_no_update BEFORE UPDATE ON pf_sync.integration_failure
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_sync.integration_failure
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_sync.integration_failure
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

GRANT SELECT, INSERT ON pf_sync.integration_failure TO sync_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_sync.integration_failure FROM sync_app;

DO $$
DECLARE
  def text;
BEGIN
  SELECT pg_get_constraintdef(c.oid) INTO def
    FROM pg_constraint c
   WHERE c.conrelid = 'pf_notify.notification'::regclass AND c.conname = 'notification_kind_check';
  IF def IS NULL THEN
    RAISE EXCEPTION 'thiếu ràng buộc pf_notify.notification.notification_kind_check';
  END IF;
  EXECUTE 'ALTER TABLE pf_notify.notification DROP CONSTRAINT notification_kind_check';
  EXECUTE format('ALTER TABLE pf_notify.notification ADD CONSTRAINT notification_kind_check CHECK ((%s) OR kind IN (%L, %L))',
                 substring(def FROM '^CHECK \((.*)\)$'), 'integration_dead_lettered', 'area_feed_stale');
END
$$;
