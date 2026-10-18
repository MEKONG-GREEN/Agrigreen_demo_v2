-- pf/extension (`dom/extension`, phòng ban chan_nuoi_thuy_san) — W11.43, D4 CN3, BRD CNTS v3.7 FN-13, UR-TINH-08, UR-TINH-13,
-- Mục III.5 (quy tắc kiểm tra liên biểu, 12 cảnh báo tự động, vòng đời cảnh báo).
--
-- Cảnh báo = nhiệm vụ có SLA của năng lực `cap/task` đang cài ở pf/extension (hosts: [cap/task]; bên dùng thứ hai sau
-- nhiệm vụ hỗ trợ US-TASK-*, chưa đủ quy tắc ba để tách pf/task): sổ sự việc + bảng chiếu trạng thái hiện tại cập nhật
-- CÙNG giao dịch (R-21), như task_event / task_current (pf 000006). Sổ task_event đã có dữ liệu và CHECK riêng của nhiệm
-- vụ hỗ trợ → sổ riêng, không ADD COLUMN.
--
-- 1. cnts_alert_event — sổ chỉ ghi thêm (shared.apply_ledger), chuỗi 'alert:<alert_ref>'. Sự việc:
--      raised       hệ thống tạo (consumer quy tắc liên biểu, ổ dịch mới, job trễ liên tiếp) — Mới
--      assigned     Cấp Tỉnh giao người xử lý, hạn xử lý — Đã giao
--      started      người được giao nhận xử lý — Đang xử lý
--      closed       đóng, kết quả bắt buộc (≥ 10 ký tự) — Đã đóng
--      auto_closed  số liệu sửa hết vi phạm → tự đóng (ghi log) — Đã đóng
--      overdue      quá hạn xử lý → Quá hạn, báo Lãnh đạo phòng (job; một lần mỗi hạn)
-- 2. cnts_alert_current — bảng chiếu (cập nhật được), UNIQUE (tenant, dedup_key): "mỗi đối tượng, loại, kỳ chỉ một cảnh
--    báo" (Mục V, FN-13 BR-04 "không tạo trùng", AC-02).
-- 3. cnts_alert_config — sổ chỉ ghi thêm cấu hình theo phiên bản: nhắc nhở theo biểu (UR-TINH-08 — giờ nhắc trước hạn,
--    nhắc khi trễ, nhắc lặp, số kỳ trễ liên tiếp, kênh, nội dung mẫu) và xử lý cảnh báo theo loại (hạn xử lý, mức). Bản mới
--    nhất của chuỗi 'config:<scope>:<code>' là hiệu lực. Ngưỡng là tham số có phiên bản cnts.* (pf/masterdata, W11.41).
-- 4. cnts_reminder_sent — nhật ký vận hành của job nhắc nộp: một dòng mỗi (kỳ, đơn vị, lượt nhắc) — chống nhắc lặp khi
--    job chạy chồng / chạy lại. Không phải sổ nghiệp vụ.
-- Vai r_extension: SELECT, INSERT (+ UPDATE bảng chiếu). ag_ops_jobs (BYPASSRLS) chỉ đọc cột tìm tenant cho job.

CREATE TABLE pf_extension.cnts_alert_event (
  id                uuid        NOT NULL,                -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                -- 'alert:<alert_ref>'
  seq               bigint      NOT NULL,
  alert_ref         uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('raised', 'assigned', 'started', 'closed', 'auto_closed', 'overdue')),
  status_after      text        NOT NULL CHECK (status_after IN ('new', 'assigned', 'in_progress', 'closed')),
  department_code   text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,40}$'),
  kind              text        NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]{1,40}$'),
  level             text        NOT NULL CHECK (level IN ('urgent', 'attention')),
  dedup_key         text        NOT NULL CHECK (char_length(dedup_key) BETWEEN 1 AND 300),
  commune_code      text        NULL CHECK (commune_code IS NULL OR commune_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  province_code     text        NULL CHECK (province_code IS NULL OR province_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  subject_kind      text        NOT NULL CHECK (subject_kind IN ('commune', 'facility', 'household', 'site', 'outbreak')),
  subject_ref       text        NOT NULL CHECK (char_length(subject_ref) BETWEEN 1 AND 64),
  template_code     text        NULL CHECK (template_code IS NULL OR char_length(template_code) BETWEEN 1 AND 64),
  period_key        text        NULL CHECK (period_key IS NULL OR char_length(period_key) BETWEEN 1 AND 40),
  period_ref        uuid        NULL,                    -- kỳ native cap/forms
  source_ref        uuid        NULL,                    -- bài nộp / ổ dịch đã sinh cảnh báo
  title             text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
  detail            jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(detail) = 'object'),
  assignee_ref      uuid        NULL,
  due_at            timestamptz NULL,                    -- hạn xử lý sau sự việc
  note              text        NULL CHECK (note IS NULL OR char_length(note) <= 2000),
  recipient_refs    uuid[]      NOT NULL DEFAULT '{}' CHECK (array_position(recipient_refs, NULL) IS NULL),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  actor_ref         uuid        NOT NULL,
  on_behalf_of      uuid        NULL,
  support_reason    text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_alert_event_stream CHECK (stream_key = 'alert:' || alert_ref::text),
  -- Vòng đời (Mục V): đóng phải ghi kết quả; giao phải có người được giao.
  CONSTRAINT cnts_alert_event_result CHECK (action <> 'closed' OR char_length(btrim(note)) >= 10),
  CONSTRAINT cnts_alert_event_assignee CHECK (action <> 'assigned' OR assignee_ref IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_alert_event_ref ON pf_extension.cnts_alert_event (tenant_id, alert_ref, seq);
SELECT shared.apply_ledger('pf_extension.cnts_alert_event', 'pf_extension.cnts_alert_event');

CREATE TABLE pf_extension.cnts_alert_current (
  tenant_id         uuid        NOT NULL,
  alert_ref         uuid        NOT NULL,
  dedup_key         text        NOT NULL,
  department_code   text        NOT NULL,
  kind              text        NOT NULL,
  level             text        NOT NULL CHECK (level IN ('urgent', 'attention')),
  status            text        NOT NULL CHECK (status IN ('new', 'assigned', 'in_progress', 'closed')),
  commune_code      text        NULL,
  province_code     text        NULL,
  subject_kind      text        NOT NULL,
  subject_ref       text        NOT NULL,
  template_code     text        NULL,
  period_key        text        NULL,
  period_ref        uuid        NULL,
  source_ref        uuid        NULL,
  title             text        NOT NULL,
  detail            jsonb       NOT NULL DEFAULT '{}'::jsonb,
  recipient_refs    uuid[]      NOT NULL DEFAULT '{}',
  assignee_ref      uuid        NULL,
  raised_at         timestamptz NOT NULL,
  due_at            timestamptz NOT NULL,                -- hạn xử lý hiện hành (Cấp Tỉnh đặt khi giao; mặc định theo loại)
  overdue_at        timestamptz NULL,                    -- đã báo Quá hạn cho hạn hiện hành
  closed_at         timestamptz NULL,
  auto_closed       boolean     NOT NULL DEFAULT false,
  result            text        NULL,
  last_event_id     uuid        NOT NULL,
  last_seq          bigint      NOT NULL,
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, alert_ref),
  UNIQUE (tenant_id, dedup_key),
  CHECK ((status = 'closed') = (closed_at IS NOT NULL))
);
CREATE INDEX cnts_alert_current_list ON pf_extension.cnts_alert_current (tenant_id, status, raised_at DESC);
CREATE INDEX cnts_alert_current_commune ON pf_extension.cnts_alert_current (tenant_id, commune_code);
CREATE INDEX cnts_alert_current_subject ON pf_extension.cnts_alert_current (tenant_id, subject_ref, kind) WHERE status <> 'closed';
CREATE INDEX cnts_alert_current_overdue ON pf_extension.cnts_alert_current (due_at) WHERE status <> 'closed' AND overdue_at IS NULL;
SELECT shared.apply_tenant_rls('pf_extension.cnts_alert_current');

CREATE TABLE pf_extension.cnts_alert_config (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                -- 'config:<scope>:<code>'
  seq               bigint      NOT NULL,
  department_code   text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,40}$'),
  scope             text        NOT NULL CHECK (scope IN ('form', 'alert')),
  code              text        NOT NULL CHECK (char_length(code) BETWEEN 1 AND 64),
  settings          jsonb       NOT NULL CHECK (jsonb_typeof(settings) = 'object'),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  actor_ref         uuid        NOT NULL,
  on_behalf_of      uuid        NULL,
  support_reason    text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_alert_config_stream CHECK (stream_key = 'config:' || scope || ':' || code)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_alert_config_stream ON pf_extension.cnts_alert_config (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_extension.cnts_alert_config', 'pf_extension.cnts_alert_config');

CREATE TABLE pf_extension.cnts_reminder_sent (
  tenant_id         uuid        NOT NULL,
  period_ref        uuid        NOT NULL,
  unit_code         text        NOT NULL CHECK (char_length(unit_code) BETWEEN 1 AND 64),
  slot              text        NOT NULL CHECK (slot ~ '^(before|overdue|repeat:[0-9]{4}-[0-9]{2}-[0-9]{2})$'),
  template_code     text        NOT NULL,
  recipients        integer     NOT NULL CHECK (recipients >= 0),
  sent_at           timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, period_ref, unit_code, slot)
);
CREATE INDEX cnts_reminder_sent_day ON pf_extension.cnts_reminder_sent (tenant_id, sent_at);
SELECT shared.apply_tenant_rls('pf_extension.cnts_reminder_sent');

REVOKE ALL ON pf_extension.cnts_alert_event, pf_extension.cnts_alert_current, pf_extension.cnts_alert_config,
  pf_extension.cnts_reminder_sent FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_alert_event, pf_extension.cnts_alert_config, pf_extension.cnts_reminder_sent TO r_extension;
GRANT SELECT, INSERT, UPDATE ON pf_extension.cnts_alert_current TO r_extension;
-- Job extension_cnts_alert (worker) tìm tenant có cảnh báo quá hạn xử lý: chỉ bốn cột.
GRANT SELECT (tenant_id, status, due_at, overdue_at) ON pf_extension.cnts_alert_current TO ag_ops_jobs;
