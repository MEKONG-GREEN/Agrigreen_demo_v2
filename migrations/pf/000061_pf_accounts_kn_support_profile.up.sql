-- pf_accounts KN v7.7 (gói P1): chế độ hỗ trợ của Admin (FN-AD-04.07 "Menu các cấp") và sổ chỉnh sửa
-- thông tin tài khoản (FN-AD-04.04 Log chỉnh sửa). docs/brd/kn-v7.7-plan.md §2 (pf 000061), §3 P1 mục 4–5.
--
-- Nhật ký hỗ trợ lưu thời điểm, Admin (actor_ref thật của token), thay cho ai (on_behalf_of), chức năng
-- (route), nội dung (tham số đường dẫn, mã kết quả — không có thân request), lý do; KHÔNG xóa (sổ chỉ ghi
-- thêm). Sổ chỉnh sửa tài khoản chỉ ghi TÊN TRƯỜNG đã đổi (họ tên, email, SĐT nằm ở Keycloak/kho định danh,
-- RB-3) — không có giá trị cũ/mới thô.

-- ---------------------------------------------------------------------------
-- 1. Phiên hỗ trợ: sổ (started, ended) + bảng chiếu. stream_key = 'support:<support_session_id>'.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_accounts.support_session_event (
  id                 uuid        NOT NULL,
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,
  seq                bigint      NOT NULL,
  support_session_id uuid        NOT NULL,
  action             text        NOT NULL CHECK (action IN ('started', 'ended')),
  acted_role         text        NOT NULL CHECK (acted_role IN ('kn_tw', 'kn_tinh', 'kn_xa')),
  department_code    text        NOT NULL,
  org_ref            uuid        NOT NULL,
  commune_codes      text[]      NOT NULL DEFAULT '{}',
  on_behalf_of       uuid        NULL,                  -- tài khoản được hỗ trợ (NULL: chỉ chọn vai + xã)
  target_assignment  uuid        NULL,
  reason             text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
  expires_at         timestamptz NOT NULL,
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  supersedes_id      uuid        NULL,
  correction_reason  text        NULL,
  actor_ref          uuid        NOT NULL,              -- Admin thật
  device_id          text        NULL,
  idem_key           text        NOT NULL,
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (action <> 'started' OR char_length(btrim(coalesce(reason, ''))) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX support_session_event_stream ON pf_accounts.support_session_event (tenant_id, stream_key, seq);
CREATE INDEX support_session_event_idem   ON pf_accounts.support_session_event (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_accounts.support_session_event', 'pf_accounts.support_session_event');

CREATE TABLE pf_accounts.support_session (
  tenant_id          uuid        NOT NULL,
  support_session_id uuid        NOT NULL,
  admin_ref          uuid        NOT NULL,
  acted_role         text        NOT NULL,
  department_code    text        NOT NULL,
  org_ref            uuid        NOT NULL,
  commune_codes      text[]      NOT NULL DEFAULT '{}',
  on_behalf_of       uuid        NULL,
  target_assignment  uuid        NULL,
  context_id         uuid        NOT NULL,
  started_at         timestamptz NOT NULL,
  expires_at         timestamptz NOT NULL,
  ended_at           timestamptz NULL,
  last_event_id      uuid        NOT NULL,
  last_seq           bigint      NOT NULL,
  PRIMARY KEY (tenant_id, support_session_id)
);
CREATE INDEX support_session_admin ON pf_accounts.support_session (tenant_id, admin_ref, started_at);
SELECT shared.apply_tenant_rls('pf_accounts.support_session');

-- ---------------------------------------------------------------------------
-- 2. Nhật ký thao tác hỗ trợ: mỗi request ghi trong ngữ cảnh hỗ trợ có hai dòng — attempted (TRƯỚC khi
-- chạy, bảo đảm có vết kể cả khi thao tác lỗi giữa chừng) và completed (mã kết quả HTTP).
-- stream_key = 'support:<support_session_id>'.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_accounts.support_action_log (
  id                 uuid        NOT NULL,
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,
  seq                bigint      NOT NULL,
  support_session_id uuid        NOT NULL,
  context_id         uuid        NOT NULL,
  request_id         uuid        NOT NULL,              -- nối attempted ↔ completed
  phase              text        NOT NULL CHECK (phase IN ('attempted', 'completed')),
  on_behalf_of       uuid        NULL,
  acted_role         text        NOT NULL,
  department_code    text        NOT NULL,
  function           text        NOT NULL,              -- "POST /v1/extension/report-tasks/:id/submissions"
  content            jsonb       NOT NULL,              -- tham số đường dẫn, query đã lọc; không thân request
  status_code        integer     NULL,
  reason             text        NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 500),
  trace_id           text        NULL,
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  supersedes_id      uuid        NULL,
  correction_reason  text        NULL,
  actor_ref          uuid        NOT NULL,              -- Admin thật
  device_id          text        NULL,
  idem_key           text        NOT NULL,
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK ((phase = 'completed') = (status_code IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX support_action_log_stream ON pf_accounts.support_action_log (tenant_id, stream_key, seq);
CREATE INDEX support_action_log_idem   ON pf_accounts.support_action_log (tenant_id, idem_key);
CREATE INDEX support_action_log_time   ON pf_accounts.support_action_log (tenant_id, event_time);
SELECT shared.apply_ledger('pf_accounts.support_action_log', 'pf_accounts.support_action_log');

-- ---------------------------------------------------------------------------
-- 3. Sổ chỉnh sửa thông tin tài khoản (FN-AD-04.04): trường đã đổi, không giá trị thô (RB-3).
-- stream_key = 'profile:<user_ref>'.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_accounts.account_profile_change (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  user_ref          uuid        NOT NULL,
  fields            text[]      NOT NULL CHECK (cardinality(fields) > 0
                                  AND fields <@ ARRAY['full_name', 'email', 'phone', 'username']::text[]),
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX account_profile_change_stream ON pf_accounts.account_profile_change (tenant_id, stream_key, seq);
CREATE INDEX account_profile_change_idem   ON pf_accounts.account_profile_change (tenant_id, idem_key);
CREATE INDEX account_profile_change_user   ON pf_accounts.account_profile_change (tenant_id, user_ref, seq);
SELECT shared.apply_ledger('pf_accounts.account_profile_change', 'pf_accounts.account_profile_change');

-- ---------------------------------------------------------------------------
-- 4. Quyền của accounts_app (R-05).
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_accounts.support_session_event, pf_accounts.support_action_log, pf_accounts.account_profile_change TO accounts_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_accounts.support_session_event, pf_accounts.support_action_log, pf_accounts.account_profile_change FROM accounts_app;
GRANT SELECT, INSERT, UPDATE ON pf_accounts.support_session TO accounts_app;
REVOKE DELETE, TRUNCATE ON pf_accounts.support_session FROM accounts_app;
