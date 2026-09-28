-- pf_notify: thông báo trong app, token đẩy, đồng bộ theo yêu cầu, email tóm tắt
-- (US-LIB-06, US-SYNC-02, KN US-TASK-02/03; ADR-0015).
--
--   notification      : hộp thư trong app. Hai dạng người nhận:
--                       * cá nhân   — recipient_ref = actor_ref (đã biết chính xác người nhận);
--                       * chủ đề    — recipient_ref NULL, người nhận là ai có phạm vi/đơn vị/vai
--                                     khớp audience_* lúc ĐỌC (khi không có danh sách người nhận).
--                       Tiêu đề và nội dung KHÔNG chứa dữ liệu cá nhân (R-14).
--   receipt           : đã đọc của thông báo chủ đề (một dòng mỗi người đọc).
--   push_token        : token đẩy của thiết bị pf_sync (một token mỗi thiết bị), kèm ảnh
--                       chụp phạm vi/vai lúc gắn để chọn thiết bị cho thông báo chủ đề.
--   push_delivery     : từng lần gửi thông điệp dữ liệu tới một thiết bị (kênh đẩy chỉ
--                       mang notification_id + kind — RB-7, ADR-0015).
--   nudge             : yêu cầu "đồng bộ ngay" (US-SYNC-02), dùng để giới hạn tần suất.
--   email_digest_item : chỉ đạo khẩn chờ đưa vào email tóm tắt.
--
-- Tham chiếu sang module khác là UUID thuần (R-06). Không có xóa cứng: token ngừng
-- dùng bằng disabled_at + lý do.

CREATE SCHEMA pf_notify;
REVOKE ALL ON SCHEMA pf_notify FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_notify TO notify_app;

-- Số tăng dần cho con trỏ đồng bộ entity "notification": cấp lại khi thông báo
-- (hoặc biên nhận đã đọc) đổi.
CREATE SEQUENCE pf_notify.sync_seq;

-- ---------------------------------------------------------------------------
-- Thông báo. Bảng trạng thái (không phải sổ): chỉ read_at và sync_seq được UPDATE.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_notify.notification (
  tenant_id         uuid        NOT NULL,
  id                uuid        NOT NULL,                  -- UUIDv7 (R-11)
  recipient_ref     uuid        NULL,                      -- NULL = thông báo chủ đề
  audience_national boolean     NOT NULL DEFAULT false,    -- chủ đề toàn quốc
  audience_units    text[]      NOT NULL DEFAULT '{}',     -- chủ đề theo đơn vị (tỉnh đã mở rộng ra xã)
  audience_org_ref  uuid        NULL,                      -- chủ đề theo tổ chức
  audience_roles    text[]      NOT NULL DEFAULT '{}',     -- rỗng = mọi vai
  kind              text        NOT NULL CHECK (kind IN ('directive', 'alert', 'document', 'task_escalated',
                                                         'sync_conflict', 'org_deactivated')),
  severity          text        NOT NULL DEFAULT 'normal' CHECK (severity IN ('urgent', 'normal')),
  title             text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
  body              text        NOT NULL CHECK (char_length(body) <= 2000),
  deep_link         text        NOT NULL CHECK (deep_link ~ '^/[A-Za-z0-9/_.-]{1,200}$'),
  subject_ref       uuid        NULL,                      -- chỉ đạo, tài liệu, nhiệm vụ… được nhắc tới
  source_event_id   uuid        NOT NULL,
  source_event_type text        NOT NULL CHECK (char_length(source_event_type) <= 120),
  created_at        timestamptz NOT NULL,                  -- R-13: lúc sự việc xảy ra
  recorded_at       timestamptz NOT NULL DEFAULT now(),    -- R-13: lúc ghi nhận
  read_at           timestamptz NULL,                      -- chỉ thông báo cá nhân
  sync_seq          bigint      NOT NULL DEFAULT nextval('pf_notify.sync_seq'),
  PRIMARY KEY (tenant_id, id),
  CHECK (recipient_ref IS NOT NULL OR read_at IS NULL),
  CHECK (recipient_ref IS NOT NULL OR audience_national OR cardinality(audience_units) > 0 OR audience_org_ref IS NOT NULL)
);
-- Chống trùng khi sự kiện được giao lặp (ngoài shared.inbox): một thông báo mỗi
-- (sự kiện, người nhận); thông báo chủ đề dùng người nhận rỗng.
CREATE UNIQUE INDEX notification_source ON pf_notify.notification
  (tenant_id, source_event_id, coalesce(recipient_ref, '00000000-0000-0000-0000-000000000000'::uuid));
CREATE INDEX notification_recipient ON pf_notify.notification (tenant_id, recipient_ref, id) WHERE recipient_ref IS NOT NULL;
CREATE INDEX notification_topic     ON pf_notify.notification (tenant_id, id) WHERE recipient_ref IS NULL;
CREATE INDEX notification_units     ON pf_notify.notification USING gin (audience_units) WHERE recipient_ref IS NULL;
CREATE INDEX notification_sync      ON pf_notify.notification (tenant_id, sync_seq);
SELECT shared.apply_tenant_rls('pf_notify.notification');

-- ---------------------------------------------------------------------------
-- Đã đọc của thông báo chủ đề. Chỉ ghi thêm (đọc rồi không "bỏ đọc").
-- ---------------------------------------------------------------------------
CREATE TABLE pf_notify.receipt (
  tenant_id       uuid        NOT NULL,
  notification_id uuid        NOT NULL,
  actor_ref       uuid        NOT NULL,
  read_at         timestamptz NOT NULL,
  recorded_at     timestamptz NOT NULL DEFAULT now(),
  sync_seq        bigint      NOT NULL DEFAULT nextval('pf_notify.sync_seq'),
  PRIMARY KEY (tenant_id, notification_id, actor_ref)
);
CREATE INDEX receipt_actor ON pf_notify.receipt (tenant_id, actor_ref, sync_seq);
SELECT shared.apply_tenant_rls('pf_notify.receipt');

-- ---------------------------------------------------------------------------
-- Token đẩy. Một token mỗi thiết bị pf_sync; token là định danh thiết bị của nhà
-- cung cấp đẩy — không ghi log, không đưa ra API đọc.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_notify.push_token (
  tenant_id       uuid        NOT NULL,
  device_id       uuid        NOT NULL,                    -- pf_sync.device (R-06: UUID thuần)
  actor_ref       uuid        NOT NULL,
  org_ref         uuid        NULL,
  provider        text        NOT NULL CHECK (provider IN ('fcm')),
  token           text        NOT NULL CHECK (char_length(token) BETWEEN 20 AND 4096),
  platform        text        NOT NULL CHECK (platform IN ('android', 'ios')),
  scope_all       boolean     NOT NULL DEFAULT false,      -- ảnh chụp phạm vi lúc gắn token
  scope_units     text[]      NOT NULL DEFAULT '{}',
  roles           text[]      NOT NULL DEFAULT '{}',
  registered_at   timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  disabled_at     timestamptz NULL,
  disabled_reason text        NULL CHECK (disabled_reason IN ('device_revoked', 'token_invalid', 'org_deactivated', 'replaced')),
  PRIMARY KEY (tenant_id, device_id),
  CHECK ((disabled_at IS NULL) = (disabled_reason IS NULL))
);
CREATE INDEX push_token_actor ON pf_notify.push_token (tenant_id, actor_ref) WHERE disabled_at IS NULL;
CREATE INDEX push_token_org   ON pf_notify.push_token (tenant_id, org_ref) WHERE disabled_at IS NULL;
CREATE INDEX push_token_value ON pf_notify.push_token (tenant_id, token);
CREATE INDEX push_token_units ON pf_notify.push_token USING gin (scope_units) WHERE disabled_at IS NULL;
SELECT shared.apply_tenant_rls('pf_notify.push_token');

-- ---------------------------------------------------------------------------
-- Lần gửi đẩy. kind = kind của thông báo hoặc 'sync_now' (nudge).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_notify.push_delivery (
  tenant_id       uuid        NOT NULL,
  id              uuid        NOT NULL,
  batch_id        uuid        NOT NULL,                    -- = batch_id của pf.notify.push.requested
  device_id       uuid        NOT NULL,
  notification_id uuid        NULL,                        -- NULL với sync_now
  kind            text        NOT NULL CHECK (kind ~ '^[a-z_]{1,40}$'),
  urgent          boolean     NOT NULL DEFAULT false,
  status          text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed', 'dropped')),
  attempts        integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  last_error      text        NULL CHECK (char_length(last_error) <= 200),   -- chỉ mã lỗi, không token
  created_at      timestamptz NOT NULL,
  sent_at         timestamptz NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, batch_id, device_id, notification_id)
);
CREATE INDEX push_delivery_batch ON pf_notify.push_delivery (tenant_id, batch_id);
SELECT shared.apply_tenant_rls('pf_notify.push_delivery');

-- ---------------------------------------------------------------------------
-- Yêu cầu đồng bộ ngay (US-SYNC-02). Chỉ ghi thêm.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_notify.nudge (
  tenant_id   uuid        NOT NULL,
  id          uuid        NOT NULL,
  actor_ref   uuid        NOT NULL,                        -- người yêu cầu
  target_kind text        NOT NULL CHECK (target_kind IN ('device', 'unit')),
  target      text        NOT NULL CHECK (char_length(target) BETWEEN 1 AND 40),
  devices     integer     NOT NULL CHECK (devices >= 0),
  created_at  timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id)
);
CREATE INDEX nudge_target ON pf_notify.nudge (tenant_id, target_kind, target, created_at);
CREATE INDEX nudge_actor  ON pf_notify.nudge (tenant_id, actor_ref, created_at);
SELECT shared.apply_tenant_rls('pf_notify.nudge');

-- ---------------------------------------------------------------------------
-- Chỉ đạo khẩn chờ gửi email tóm tắt. Chỉ mã tham chiếu, đơn vị và thời điểm.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_notify.email_digest_item (
  tenant_id       uuid        NOT NULL,
  directive_id    uuid        NOT NULL,
  source_event_id uuid        NOT NULL,
  audience_units  text[]      NOT NULL,
  issued_at       timestamptz NOT NULL,
  queued_at       timestamptz NOT NULL,
  digest_id       uuid        NULL,
  sent_at         timestamptz NULL,
  PRIMARY KEY (tenant_id, directive_id),
  CHECK ((digest_id IS NULL) = (sent_at IS NULL))
);
CREATE INDEX email_digest_pending ON pf_notify.email_digest_item (tenant_id, issued_at) WHERE sent_at IS NULL;
SELECT shared.apply_tenant_rls('pf_notify.email_digest_item');

-- ---------------------------------------------------------------------------
-- Quyền của notify_app. Không DELETE ở bảng nào.
-- ---------------------------------------------------------------------------
GRANT USAGE ON SEQUENCE pf_notify.sync_seq TO notify_app;
GRANT SELECT, INSERT ON pf_notify.notification, pf_notify.receipt, pf_notify.nudge TO notify_app;
GRANT UPDATE (read_at, sync_seq) ON pf_notify.notification TO notify_app;
GRANT SELECT, INSERT, UPDATE ON pf_notify.push_token, pf_notify.push_delivery TO notify_app;
GRANT SELECT, INSERT ON pf_notify.email_digest_item TO notify_app;
GRANT UPDATE (digest_id, sent_at) ON pf_notify.email_digest_item TO notify_app;

-- Job email tóm tắt chạy ở worker (leader) cho MỌI tenant: vai ag_outbox_publisher
-- (BYPASSRLS) chỉ đọc hai cột để tìm tenant có mục chờ gửi; ghi bằng notify_app.
GRANT USAGE ON SCHEMA pf_notify TO ag_outbox_publisher;
GRANT SELECT (tenant_id, sent_at) ON pf_notify.email_digest_item TO ag_outbox_publisher;
