-- pf_extension: nghiệp vụ khuyến nông (ADR-0011) — nhiệm vụ hỗ trợ có SLA và
-- leo thang, chỉ đạo khẩn có xác nhận đã đọc, báo cáo hiện trường có phiên bản,
-- danh bạ trực.
--
-- Nguồn: BRD App Khuyến nông v1.0 (FR-01, FR-02, FR-05, FR-17/18, FR-19),
-- Backlog KN v1.0 (US-TASK-01..04, US-DASH-01..03, US-DIR-01..03).
--
-- KHÔNG có dữ liệu cá nhân của nông hộ ở đây (R-04): nông hộ, HTX, thửa chỉ là
-- *_ref. Ngoại lệ có chủ đích duy nhất là staff.display_name (xem chú thích bảng).

CREATE SCHEMA pf_extension;
REVOKE ALL ON SCHEMA pf_extension FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_extension TO extension_app;

-- Con trỏ đồng bộ chiều xuống (sync port.Source): mỗi lần bảng hiện tại đổi thì
-- lấy số mới từ sequence này.
CREATE SEQUENCE pf_extension.sync_seq;

-- ---------------------------------------------------------------------------
-- Sổ nhiệm vụ hỗ trợ (mẫu mục 8.1). stream_key = 'task:<task_id>'.
-- Mỗi dòng là một sự việc: created, assigned, accepted, progress, resolved,
-- escalated, reopened — kèm trạng thái SAU sự việc để dựng lại task_current (R-22).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.task_event (
  id                   uuid        NOT NULL,
  tenant_id            uuid        NOT NULL,
  stream_key           text        NOT NULL,
  seq                  bigint      NOT NULL,
  task_id              uuid        NOT NULL,
  action               text        NOT NULL CHECK (action IN ('created', 'assigned', 'accepted', 'progress', 'resolved', 'escalated', 'reopened')),
  status_after         text        NOT NULL CHECK (status_after IN ('new', 'assigned', 'accepted', 'in_progress', 'resolved')),
  assignee_ref         uuid        NULL,
  due_at               timestamptz NOT NULL,
  note                 text        NULL CHECK (note IS NULL OR char_length(note) <= 2000),
  escalated_unit       text        NULL,                     -- mã tỉnh nhận leo thang
  escalation_org_ref   uuid        NULL,                     -- TTKN tỉnh (pf_party, tham chiếu thuần R-06)
  snapshot             jsonb       NULL,                     -- action = created: toàn bộ thuộc tính nhiệm vụ (không PII)
  event_time           timestamptz NOT NULL,                 -- R-13
  recorded_at          timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
  supersedes_id        uuid        NULL,
  correction_reason    text        NULL,
  actor_ref            uuid        NOT NULL,
  device_id            text        NULL,
  idem_key             text        NOT NULL,
  rule_version         text        NOT NULL,
  prev_hash            bytea       NOT NULL,
  row_hash             bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  -- US-TASK-02 AC-1: chỉ đóng khi có kết quả xử lý cụ thể.
  CHECK (action <> 'resolved' OR char_length(btrim(note)) >= 10),
  CHECK (action <> 'reopened' OR char_length(btrim(note)) >= 10)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX task_event_idem   ON pf_extension.task_event (tenant_id, idem_key);
CREATE INDEX task_event_stream ON pf_extension.task_event (tenant_id, stream_key, seq);
CREATE INDEX task_event_task   ON pf_extension.task_event (tenant_id, task_id, seq);

SELECT shared.apply_ledger('pf_extension.task_event', 'pf_extension.task_event');

-- Bảng chiếu trạng thái hiện tại của nhiệm vụ (R-21), cập nhật trong CÙNG giao dịch.
CREATE TABLE pf_extension.task_current (
  tenant_id          uuid          NOT NULL,
  task_id            uuid          NOT NULL,
  commune_code       text          NOT NULL,
  province_code      text          NOT NULL,
  cooperative_ref    uuid          NULL,
  household_ref      uuid          NULL,
  plot_ref           uuid          NULL,
  category           text          NOT NULL CHECK (category IN ('sau_benh', 'ky_thuat', 'co_gioi', 'khac')),
  description        text          NOT NULL CHECK (char_length(description) BETWEEN 10 AND 2000),
  priority           text          NOT NULL CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
  source             text          NOT NULL CHECK (source IN ('officer', 'coop_manager', 'htx_app')),
  status             text          NOT NULL CHECK (status IN ('new', 'assigned', 'accepted', 'in_progress', 'resolved')),
  assignee_ref       uuid          NULL,
  created_by         uuid          NOT NULL,
  created_at         timestamptz   NOT NULL,
  sla_hours          numeric(14,4) NOT NULL CHECK (sla_hours > 0),
  due_at             timestamptz   NOT NULL,
  escalated_at       timestamptz   NULL,
  escalated_unit     text          NULL,
  escalation_org_ref uuid          NULL,
  resolved_at        timestamptz   NULL,
  last_note          text          NULL,
  last_event_id      uuid          NOT NULL,
  last_seq           bigint        NOT NULL,
  sync_seq           bigint        NOT NULL DEFAULT nextval('pf_extension.sync_seq'),
  updated_at         timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, task_id)
);
CREATE INDEX task_current_commune  ON pf_extension.task_current (tenant_id, commune_code, status);
CREATE INDEX task_current_province ON pf_extension.task_current (tenant_id, province_code, status);
CREATE INDEX task_current_assignee ON pf_extension.task_current (tenant_id, assignee_ref);
CREATE INDEX task_current_sync     ON pf_extension.task_current (tenant_id, sync_seq);
CREATE INDEX task_current_overdue  ON pf_extension.task_current (due_at) WHERE status <> 'resolved' AND escalated_at IS NULL;
SELECT shared.apply_tenant_rls('pf_extension.task_current');

-- ---------------------------------------------------------------------------
-- Chỉ đạo khẩn (FR-19, US-TASK-03): ban hành một lần, không sửa. Đối tượng nhận
-- chụp lại lúc ban hành; xác nhận đã đọc là bản ghi thêm (không UPDATE).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.directive (
  tenant_id      uuid        NOT NULL,
  directive_id   uuid        NOT NULL,
  title          text        NOT NULL CHECK (char_length(title) BETWEEN 5 AND 200),
  body           text        NOT NULL CHECK (char_length(body) BETWEEN 10 AND 5000),
  severity       text        NOT NULL CHECK (severity IN ('urgent', 'normal')),
  audience_units text[]      NOT NULL CHECK (cardinality(audience_units) BETWEEN 1 AND 500),
  province_codes text[]      NOT NULL,                     -- tỉnh chứa các đơn vị nhận (lọc phạm vi)
  issuer_ref     uuid        NOT NULL,
  issued_at      timestamptz NOT NULL,
  expires_at     timestamptz NULL,
  target_count   integer     NOT NULL CHECK (target_count >= 0),
  idem_key       text        NOT NULL,
  sync_seq       bigint      NOT NULL DEFAULT nextval('pf_extension.sync_seq'),
  recorded_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, directive_id),
  UNIQUE (tenant_id, idem_key),
  CHECK (expires_at IS NULL OR expires_at > issued_at)
);
CREATE INDEX directive_sync ON pf_extension.directive (tenant_id, sync_seq);
CREATE INDEX directive_audience ON pf_extension.directive USING gin (audience_units);
SELECT shared.apply_tenant_rls('pf_extension.directive');

CREATE TABLE pf_extension.directive_target (
  tenant_id     uuid        NOT NULL,
  directive_id  uuid        NOT NULL,
  actor_ref     uuid        NOT NULL,
  unit_code     text        NOT NULL,
  province_code text        NOT NULL,
  issued_at     timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, directive_id, actor_ref),
  FOREIGN KEY (tenant_id, directive_id) REFERENCES pf_extension.directive (tenant_id, directive_id)
);
CREATE INDEX directive_target_actor ON pf_extension.directive_target (tenant_id, actor_ref);
CREATE INDEX directive_target_unit  ON pf_extension.directive_target (tenant_id, province_code, unit_code);
SELECT shared.apply_tenant_rls('pf_extension.directive_target');

CREATE TABLE pf_extension.directive_ack (
  tenant_id    uuid        NOT NULL,
  directive_id uuid        NOT NULL,
  actor_ref    uuid        NOT NULL,
  acked_at     timestamptz NOT NULL,
  device_id    text        NULL,
  recorded_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, directive_id, actor_ref),
  FOREIGN KEY (tenant_id, directive_id, actor_ref) REFERENCES pf_extension.directive_target (tenant_id, directive_id, actor_ref)
);
SELECT shared.apply_tenant_rls('pf_extension.directive_ack');

-- ---------------------------------------------------------------------------
-- Sổ báo cáo hiện trường (FR-01 gieo sạ, FR-02 sinh trưởng/sâu bệnh).
-- stream_key = 'report:<version_key>'. Kỳ còn mở: gửi lại cùng khóa tạo phiên
-- bản mới (previous_id). Kỳ đã chốt: chỉ bản ghi bù có lý do (US-DASH-02 AC-2).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.field_report (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,
  seq               bigint        NOT NULL,
  version_key       text          NOT NULL,
  report_kind       text          NOT NULL CHECK (report_kind IN ('sowing_progress', 'growth_pest')),
  commune_code      text          NOT NULL,
  province_code     text          NOT NULL,
  cooperative_ref   uuid          NULL,
  period            text          NOT NULL CHECK (period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
  season_code       text          NULL,
  variety_code      text          NULL,
  sown_area_ha      numeric(14,4) NULL CHECK (sown_area_ha >= 0),
  sown_on           date          NULL,
  growth_stage      text          NULL,
  pest_code         text          NULL,
  severity          text          NULL CHECK (severity IN ('nhe', 'trung_binh', 'nang')),
  affected_area_ha  numeric(14,4) NULL CHECK (affected_area_ha >= 0),
  urgent            boolean       NOT NULL DEFAULT false,
  note              text          NULL CHECK (note IS NULL OR char_length(note) <= 2000),
  version           integer       NOT NULL CHECK (version >= 1),
  previous_id       uuid          NULL,                    -- phiên bản trước (gửi lại khi kỳ còn mở hoặc bản ghi bù)
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (report_kind <> 'sowing_progress' OR (sown_area_ha IS NOT NULL AND variety_code IS NOT NULL AND season_code IS NOT NULL)),
  CHECK (report_kind <> 'growth_pest' OR (growth_stage IS NOT NULL AND pest_code IS NOT NULL AND severity IS NOT NULL))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX field_report_idem   ON pf_extension.field_report (tenant_id, idem_key);
CREATE INDEX field_report_stream ON pf_extension.field_report (tenant_id, stream_key, seq);
CREATE INDEX field_report_id     ON pf_extension.field_report (id);

SELECT shared.apply_ledger('pf_extension.field_report', 'pf_extension.field_report');

-- Phiên bản hiện hành theo khóa phiên bản (R-21) — nguồn của dashboard.
CREATE TABLE pf_extension.field_report_current (
  tenant_id        uuid          NOT NULL,
  version_key      text          NOT NULL,
  report_id        uuid          NOT NULL,
  report_kind      text          NOT NULL,
  commune_code     text          NOT NULL,
  province_code    text          NOT NULL,
  cooperative_ref  uuid          NULL,
  period           text          NOT NULL,
  sown_area_ha     numeric(14,4) NULL,
  affected_area_ha numeric(14,4) NULL,
  version          integer       NOT NULL,
  updated_at       timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, version_key)
);
CREATE INDEX field_report_current_period ON pf_extension.field_report_current (tenant_id, period, province_code, commune_code);
SELECT shared.apply_tenant_rls('pf_extension.field_report_current');

-- Chốt kỳ báo cáo theo tỉnh (FR-05). Chỉ ghi thêm — không mở lại kỳ ở G1.
CREATE TABLE pf_extension.period_close (
  tenant_id     uuid        NOT NULL,
  period        text        NOT NULL CHECK (period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
  province_code text        NOT NULL,
  closed_by     uuid        NOT NULL,
  closed_at     timestamptz NOT NULL,
  note          text        NULL,
  recorded_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, period, province_code)
);
SELECT shared.apply_tenant_rls('pf_extension.period_close');

-- ---------------------------------------------------------------------------
-- Danh bạ trực (FN-15, US-DIR-01..03).
--
-- display_name là tên cán bộ khuyến nông — dữ liệu danh bạ công vụ, KHÔNG phải
-- dữ liệu cá nhân của nông hộ (mục 10.4), nhưng vẫn là thông tin cá nhân. Tài
-- khoản cán bộ nằm ở Keycloak, không có chủ thể trong kho định danh nên không gọi
-- được vault Display. Giữ tối thiểu một cột tên hiển thị ở đây (ghi chú ADR-0009):
-- struct Go gắn pii:"true", không bao giờ ghi log. SĐT không lưu ở G1.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_extension.staff (
  tenant_id     uuid        NOT NULL,
  actor_ref     uuid        NOT NULL,                      -- = sub của token cán bộ
  unit_code     text        NOT NULL,                      -- xã (hoặc tỉnh với cán bộ TTKN tỉnh)
  province_code text        NOT NULL,
  role_title    text        NOT NULL CHECK (char_length(role_title) BETWEEN 2 AND 100),
  display_name  text        NOT NULL CHECK (char_length(display_name) BETWEEN 2 AND 120),
  active        boolean     NOT NULL DEFAULT true,
  updated_by    uuid        NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, actor_ref)
);
CREATE INDEX staff_unit ON pf_extension.staff (tenant_id, province_code, unit_code);
SELECT shared.apply_tenant_rls('pf_extension.staff');

-- Trạng thái trực tự khai (US-DIR-01): chỉ ghi thêm; bản mới nhất còn hạn là hiện hành.
CREATE TABLE pf_extension.duty_status (
  tenant_id   uuid        NOT NULL,
  id          uuid        NOT NULL,
  actor_ref   uuid        NOT NULL,
  status      text        NOT NULL CHECK (status IN ('on_duty', 'off_duty')),
  set_at      timestamptz NOT NULL,
  valid_until timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  CHECK (valid_until > set_at)
);
CREATE INDEX duty_status_actor ON pf_extension.duty_status (tenant_id, actor_ref, set_at DESC);
SELECT shared.apply_tenant_rls('pf_extension.duty_status');

-- Lịch phân ca trực (US-DIR-03): chỉ ghi thêm.
CREATE TABLE pf_extension.duty_shift (
  tenant_id   uuid        NOT NULL,
  id          uuid        NOT NULL,
  actor_ref   uuid        NOT NULL,
  starts_at   timestamptz NOT NULL,
  ends_at     timestamptz NOT NULL,
  created_by  uuid        NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  CHECK (ends_at > starts_at)
);
CREATE INDEX duty_shift_actor ON pf_extension.duty_shift (tenant_id, actor_ref, starts_at);
SELECT shared.apply_tenant_rls('pf_extension.duty_shift');

-- ---------------------------------------------------------------------------
-- Quyền của extension_app. Sổ: chỉ SELECT, INSERT. Bảng chỉ ghi thêm khác:
-- SELECT, INSERT. Bảng hiện tại (task_current, field_report_current, staff):
-- thêm UPDATE. Không DELETE ở đâu cả.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_extension.task_event, pf_extension.field_report TO extension_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_extension.task_event, pf_extension.field_report FROM extension_app;
GRANT SELECT, INSERT ON pf_extension.directive, pf_extension.directive_target, pf_extension.directive_ack,
  pf_extension.period_close, pf_extension.duty_status, pf_extension.duty_shift TO extension_app;
GRANT SELECT, INSERT, UPDATE ON pf_extension.task_current, pf_extension.field_report_current, pf_extension.staff TO extension_app;
GRANT USAGE ON SEQUENCE pf_extension.sync_seq TO extension_app;

-- Job leo thang chạy ở worker (leader) cho MỌI tenant: vai ag_outbox_publisher
-- (BYPASSRLS) chỉ được đọc bốn cột để biết tenant nào có nhiệm vụ quá hạn; việc
-- leo thang thật chạy bằng extension_app trong ngữ cảnh từng tenant (R-05).
GRANT USAGE ON SCHEMA pf_extension TO ag_outbox_publisher;
GRANT SELECT (tenant_id, status, escalated_at, due_at) ON pf_extension.task_current TO ag_outbox_publisher;
