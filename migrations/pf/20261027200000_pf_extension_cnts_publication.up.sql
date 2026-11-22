-- pf_extension: Phát hành và theo dõi biểu kỳ CNTS (BF-CN-PUBLISH #108 — CN-07, FN-07, UR-TINH-08, UR-XA-13). Chỉ đụng
-- schema pf_extension. Kỳ, công việc, nộp, chốt vẫn là kỳ native cap/forms profile kn_cnts (pf_forms) — ba thay đổi dưới
-- đây chỉ giữ phần riêng của KN:
--
--   1. cnts_publication — SỔ (append-only, shared.apply_ledger) mỗi lần Cấp Tỉnh phát hành một lô kỳ: biểu, cán bộ tỉnh
--      phụ trách, đối tượng (tất cả xã / khu vực / xã có nuôi thủy sản / xã có mô hình / chọn từng xã / cơ sở giết mổ) và
--      danh sách đơn vị đã giải, khoảng ngày, hạn nộp, giờ chốt (số giờ sau hạn), các kỳ native đã sinh, số nhiệm vụ.
--      Màn theo dõi đọc người phụ trách và giờ chốt từ đây; cảnh báo trễ liên tiếp gửi người phụ trách của kỳ.
--   2. cnts_period_draft — bảng chiếu bản nháp của cán bộ xã theo (kỳ native, đơn vị): "Lưu nháp" để Cấp Tỉnh thấy trạng
--      thái Nháp. Nháp cũ hơn lần gửi cuối được bỏ qua khi chiếu (không xóa).
--   3. cnts_reminder_sent nhận thêm lượt "manual:<YYYY-MM-DDTHH>" — Cấp Tỉnh bấm Nhắc trên màn theo dõi (tối đa một lượt
--      mỗi đơn vị mỗi giờ).

SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.cnts_publication (
  id                  uuid        NOT NULL,                 -- UUIDv7 (R-11) = mã lần phát hành
  tenant_id           uuid        NOT NULL,
  stream_key          text        NOT NULL,                 -- 'cnts_publication:<template_code>'
  seq                 bigint      NOT NULL,
  department_code     text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,40}$'),
  org_ref             uuid        NULL,                     -- Nút Cấp Tỉnh phát hành (phạm vi xem của màn theo dõi)
  template_code       text        NOT NULL CHECK (template_code ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'),
  owner_ref          uuid        NULL,                     -- cán bộ tỉnh phụ trách biểu (tham chiếu tài khoản)
  target_mode         text        NOT NULL CHECK (target_mode IN ('communes', 'province', 'regions', 'aquaculture', 'model', 'facilities')),
  regions             text[]      NOT NULL DEFAULT '{}' CHECK (cardinality(regions) <= 100),
  unit_kind           text        NOT NULL CHECK (unit_kind IN ('commune', 'facility')),
  units               text[]      NOT NULL CHECK (cardinality(units) BETWEEN 1 AND 5000),
  period_from         date        NOT NULL,
  period_to           date        NOT NULL,
  deadline_offset     integer     NOT NULL CHECK (deadline_offset BETWEEN -14 AND 60),
  deadline_time       text        NOT NULL CHECK (deadline_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
  close_after_hours   integer     NULL CHECK (close_after_hours IS NULL OR close_after_hours BETWEEN 0 AND 720),
  period_refs         uuid[]      NOT NULL CHECK (cardinality(period_refs) BETWEEN 1 AND 400),
  task_count          integer     NOT NULL CHECK (task_count >= 1),
  event_time          timestamptz NOT NULL,
  recorded_at         timestamptz NOT NULL DEFAULT now(),
  actor_ref           uuid        NOT NULL,
  on_behalf_of        uuid        NULL,
  support_reason      text        NULL,
  idem_key            text        NOT NULL,
  rule_version        text        NOT NULL,
  prev_hash           bytea       NOT NULL,
  row_hash            bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_publication_stream CHECK (stream_key = 'cnts_publication:' || template_code),
  CONSTRAINT cnts_publication_range CHECK (period_to >= period_from),
  CONSTRAINT cnts_publication_regions CHECK (target_mode = 'regions' OR cardinality(regions) = 0)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_publication_code    ON pf_extension.cnts_publication (tenant_id, template_code, event_time DESC);
CREATE INDEX cnts_publication_idem    ON pf_extension.cnts_publication (tenant_id, idem_key);
CREATE INDEX cnts_publication_periods ON pf_extension.cnts_publication USING gin (period_refs);
SELECT shared.apply_ledger('pf_extension.cnts_publication', 'pf_extension.cnts_publication');

CREATE TABLE pf_extension.cnts_period_draft (
  tenant_id         uuid        NOT NULL,
  period_ref        uuid        NOT NULL,                 -- kỳ native pf_forms.period (không khóa ngoại liên schema)
  unit_code         text        NOT NULL CHECK (char_length(unit_code) BETWEEN 1 AND 64),
  parent_code       text        NOT NULL CHECK (char_length(parent_code) BETWEEN 1 AND 64), -- xã của đơn vị (phạm vi)
  template_code     text        NOT NULL CHECK (char_length(template_code) BETWEEN 1 AND 64),
  draft_values      jsonb       NOT NULL CHECK (jsonb_typeof(draft_values) = 'object'),
  nil_report        boolean     NOT NULL DEFAULT false,
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 2000),
  saved_by          uuid        NOT NULL,
  saved_at          timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, period_ref, unit_code)
);
CREATE INDEX cnts_period_draft_parent ON pf_extension.cnts_period_draft (tenant_id, parent_code, saved_at DESC);
SELECT shared.apply_tenant_rls('pf_extension.cnts_period_draft');

ALTER TABLE pf_extension.cnts_reminder_sent DROP CONSTRAINT cnts_reminder_sent_slot_check;
ALTER TABLE pf_extension.cnts_reminder_sent ADD CONSTRAINT cnts_reminder_sent_slot_check
  CHECK (slot ~ '^(before|overdue|repeat:[0-9]{4}-[0-9]{2}-[0-9]{2}|manual:[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2})$') NOT VALID;
ALTER TABLE pf_extension.cnts_reminder_sent VALIDATE CONSTRAINT cnts_reminder_sent_slot_check;

REVOKE ALL ON pf_extension.cnts_publication, pf_extension.cnts_period_draft FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_publication TO r_extension;
GRANT SELECT, INSERT, UPDATE ON pf_extension.cnts_period_draft TO r_extension;
