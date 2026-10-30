-- W11.26: sổ đánh giá HCTH FN15–18/21. Mẫu/phiếu, đợt và dòng đánh giá đều là phiên bản bất biến.
-- Không lưu họ tên, ngày sinh, email, SĐT; chỉ giữ mã hồ sơ/chủ thể và bản chụp nghiệp vụ.
-- Mỗi đối tượng có chuỗi băm riêng; phân quyền vai/đơn vị/chủ thể ở ứng dụng, RLS theo tenant ở DB.
CREATE TABLE pf_hrm.appraisal_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  entity_kind       text        NOT NULL CHECK (entity_kind IN ('summary', 'form', 'period', 'entry', 'export', 'access')),
  entity_key        text        NOT NULL CHECK (char_length(entity_key) BETWEEN 1 AND 120),
  period_ref        uuid        NULL,
  staff_ref         uuid        NULL,
  action            text        NOT NULL CHECK (action IN ('draft', 'publish', 'retire', 'open', 'extend', 'lock_self',
                          'lock_manager', 'unlock_self', 'unlock_manager', 'self_draft', 'self_submit', 'proxy_self',
                          'manager_draft', 'manager_submit', 'send', 'return', 'official', 'close', 'reopen',
                          'attach_signed', 'export', 'download', 'remind', 'view')),
  payload           jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 2097152),
  reason            text        NOT NULL DEFAULT '' CHECK (char_length(reason) <= 2000),
  request_hash      text        NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
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
  CHECK (stream_key = entity_kind || ':' || entity_key),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR char_length(correction_reason) >= 10),
  CHECK (entity_kind <> 'entry' OR (period_ref IS NOT NULL AND staff_ref IS NOT NULL)),
  CHECK (entity_kind NOT IN ('period', 'export') OR (period_ref IS NOT NULL AND staff_ref IS NULL)),
  CHECK (entity_kind NOT IN ('summary', 'form') OR (period_ref IS NULL AND staff_ref IS NULL)),
  CHECK (action NOT IN ('return', 'reopen') OR char_length(btrim(reason)) >= 10)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX appraisal_entity ON pf_hrm.appraisal_event (tenant_id, entity_kind, entity_key, seq DESC);
CREATE INDEX appraisal_period ON pf_hrm.appraisal_event (tenant_id, period_ref, entity_kind, staff_ref, seq DESC);
CREATE INDEX appraisal_request ON pf_hrm.appraisal_event (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_hrm.appraisal_event', 'pf_hrm.appraisal_event');
REVOKE ALL ON pf_hrm.appraisal_event FROM PUBLIC;
GRANT SELECT, INSERT ON pf_hrm.appraisal_event TO r_hrm;
