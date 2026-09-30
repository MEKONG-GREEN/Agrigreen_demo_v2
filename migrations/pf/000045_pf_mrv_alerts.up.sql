-- pf_mrv G3: cảnh báo MRV (vai mrv_app) + loại thông báo tương ứng của pf_notify.
--
--   1. alert  sổ cảnh báo chỉ ghi thêm, một dòng mỗi (quy tắc, chủ thể, mức):
--        pest_urgent            minh chứng sâu bệnh/thiên tai vượt ngưỡng → nhãn "Cảnh báo khẩn" (KN US-MRV-04);
--                               nhãn gắn với ĐÚNG phiên bản minh chứng (subject_id), bản ghi bù đánh giá lại;
--        awd_no_infrastructure  nhật ký App HTX ghi AWD nhưng thửa/xã chưa có minh chứng hạ tầng đạt (KN FR-08);
--        review_overdue         hồ sơ chờ duyệt quá hạn xử lý, level 1 = người duyệt, 2 = quản trị TTKN tỉnh
--                               (KN US-MRV-06). Idempotent theo idem_key 'overdue:<phiên bản>:<mức>'.
--      params: [{key, version, value, unit, source}] — tham số (có phiên bản) đã dùng khi đánh giá.
--   2. ag_outbox_publisher đọc HAI cột của evidence_current để job leo thang tìm tenant (mẫu pf 000006).
--   3. pf_notify.notification.kind thêm mrv_urgent, mrv_anomaly, mrv_overdue (mẫu pf 000041).

CREATE TABLE pf_mrv.alert (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,               -- 'unit:<admin_unit_code>'
  seq               bigint      NOT NULL,
  kind              text        NOT NULL CHECK (kind IN ('pest_urgent', 'awd_no_infrastructure', 'review_overdue')),
  level             smallint    NOT NULL CHECK (level BETWEEN 0 AND 2),
  severity          text        NOT NULL CHECK (severity IN ('urgent', 'normal')),
  admin_unit_code   text        NOT NULL CHECK (admin_unit_code ~ '^[0-9A-Z][0-9A-Z.-]{0,39}$'),
  subject_type      text        NOT NULL CHECK (subject_type IN ('evidence', 'farm_log')),
  subject_id        uuid        NOT NULL,
  subject_root_id   uuid        NOT NULL,
  plot_ref          uuid        NULL,
  season_ref        uuid        NULL,
  detail            jsonb       NOT NULL CHECK (jsonb_typeof(detail) = 'object'),
  params            jsonb       NOT NULL CHECK (jsonb_typeof(params) = 'array'),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK ((kind = 'review_overdue') = (level > 0)),
  CHECK (kind <> 'awd_no_infrastructure' OR subject_type = 'farm_log'),
  CHECK (kind = 'awd_no_infrastructure' OR subject_type = 'evidence')
) PARTITION BY RANGE (recorded_at);

CREATE INDEX alert_subject ON pf_mrv.alert (tenant_id, subject_id, kind, level);
CREATE INDEX alert_root    ON pf_mrv.alert (tenant_id, subject_root_id);
CREATE INDEX alert_unit    ON pf_mrv.alert (tenant_id, admin_unit_code, kind, id);
CREATE INDEX alert_idem    ON pf_mrv.alert (tenant_id, idem_key);
CREATE INDEX alert_stream  ON pf_mrv.alert (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_mrv.alert', 'pf_mrv.alert');

GRANT SELECT, INSERT ON pf_mrv.alert TO mrv_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_mrv.alert FROM mrv_app;

-- Job leo thang (worker, leader) cho MỌI tenant: ag_outbox_publisher chỉ đọc hai cột.
GRANT USAGE ON SCHEMA pf_mrv TO ag_outbox_publisher;
GRANT SELECT (tenant_id, review_status) ON pf_mrv.evidence_current TO ag_outbox_publisher;

-- Loại thông báo mới: giữ nguyên các loại đã có (đọc CHECK hiện hành rồi cộng thêm).
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
  EXECUTE format('ALTER TABLE pf_notify.notification ADD CONSTRAINT notification_kind_check CHECK ((%s) OR kind IN (%L, %L, %L))',
                 substring(def FROM '^CHECK \((.*)\)$'), 'mrv_urgent', 'mrv_anomaly', 'mrv_overdue');
END
$$;
