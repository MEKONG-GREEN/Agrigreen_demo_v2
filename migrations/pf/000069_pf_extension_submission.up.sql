-- pf_extension KN v7.7 P4 (2/4): bài nộp có phiên bản, rà soát, nhắc nhở (FN-XA-03.02, FN-TI-02.03,
-- FN-TI-02.04; docs/brd/kn-v7.7-plan.md §2 pf 000069, gap §4.1 RB-1).
--
--   1. submission_version  SỔ: mỗi lần Lưu nháp / Gửi / Gửi lại là một phiên bản BẤT BIẾN (giá trị ô theo
--                          chữ cái cột Excel, phiên bản mẫu, người nhập, on_behalf_of khi Admin nhập thay).
--                          "Sửa" = phiên bản mới (previous_id); lịch sử sửa = chuỗi phiên bản.
--   2. review_event        SỔ rà soát của Cấp Tỉnh: adjustment_requested {lý do bắt buộc} / confirmed
--   3. reminder_event      SỔ nhắc nhở: thủ công (chọn xã / tất cả xã chưa nộp) hoặc tự động trước hạn / khi trễ
--
-- Không PII: rows chứa số liệu tổng hợp cấp xã theo mẫu; người là actor_ref (tên tra lúc đọc, RB-3).

CREATE TABLE pf_extension.submission_version (
  id                 uuid        NOT NULL,                 -- UUIDv7; client ngoại tuyến được tự sinh (R-11)
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,                 -- 'sub:<task_id>'
  seq                bigint      NOT NULL,
  task_id            uuid        NOT NULL,
  version_no         integer     NOT NULL CHECK (version_no >= 1),
  form_version_id    uuid        NOT NULL,
  kind               text        NOT NULL CHECK (kind IN ('draft', 'submitted')),
  rows               jsonb       NOT NULL,                 -- [{key, indicator, input{chữ cái: giá trị}, cells{…đã tính}}]
  issues             jsonb       NOT NULL DEFAULT '[]'::jsonb,
  adjustment_response boolean    NOT NULL DEFAULT false,   -- gửi lại theo "Yêu cầu điều chỉnh"
  previous_id        uuid        NULL,
  commune_code       text        NOT NULL,
  department_code    text        NOT NULL,
  on_behalf_of       uuid        NULL,                     -- Admin nhập thay (FN-AD-04.07)
  support_session_id uuid        NULL,
  support_reason     text        NULL CHECK (support_reason IS NULL OR char_length(support_reason) BETWEEN 10 AND 500),
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  supersedes_id      uuid        NULL,
  correction_reason  text        NULL,
  actor_ref          uuid        NOT NULL,
  device_id          text        NULL,
  idem_key           text        NOT NULL,
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK ((support_session_id IS NULL) = (support_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX submission_version_task ON pf_extension.submission_version (tenant_id, task_id, version_no DESC);
CREATE INDEX submission_version_id ON pf_extension.submission_version (tenant_id, id);
SELECT shared.apply_ledger('pf_extension.submission_version', 'pf_extension.submission_version');

CREATE TABLE pf_extension.review_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'review:<task_id>'
  seq               bigint      NOT NULL,
  task_id           uuid        NOT NULL,
  submission_id     uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('adjustment_requested', 'confirmed')),
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 2000),
  department_code   text        NOT NULL,
  on_behalf_of      uuid        NULL,
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
  CHECK (action <> 'adjustment_requested' OR reason IS NOT NULL)  -- FN-TI-02.04: bắt buộc lý do
) PARTITION BY RANGE (recorded_at);
CREATE INDEX review_event_task ON pf_extension.review_event (tenant_id, task_id, id);
SELECT shared.apply_ledger('pf_extension.review_event', 'pf_extension.review_event');

CREATE TABLE pf_extension.reminder_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'reminder:<batch_id>'
  seq               bigint      NOT NULL,
  batch_id          uuid        NOT NULL,
  period_id         uuid        NOT NULL,
  template_id       uuid        NULL,                     -- NULL = mọi biểu của kỳ
  mode              text        NOT NULL CHECK (mode IN ('manual', 'auto_before', 'auto_overdue')),
  message           text        NOT NULL CHECK (char_length(message) BETWEEN 1 AND 1000),
  task_ids          uuid[]      NOT NULL,
  communes          text[]      NOT NULL,
  recipient_refs    uuid[]      NOT NULL,
  department_code   text        NOT NULL,
  on_behalf_of      uuid        NULL,
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
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX reminder_event_batch ON pf_extension.reminder_event (tenant_id, batch_id, id);
SELECT shared.apply_ledger('pf_extension.reminder_event', 'pf_extension.reminder_event');

GRANT SELECT, INSERT ON pf_extension.submission_version, pf_extension.review_event, pf_extension.reminder_event TO extension_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_extension.submission_version, pf_extension.review_event, pf_extension.reminder_event FROM extension_app;
