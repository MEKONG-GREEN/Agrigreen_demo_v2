-- pf_accounts: cầu nối quản trị tài khoản cán bộ với Keycloak (KN US-ADM-01..03,
-- HTX US-ACC-01/02, US-HTXSTATUS-01..03, CGH FN-01; ADR-0010).
--
-- Họ tên, email, SĐT, tên đăng nhập của cán bộ CHỈ nằm trong Keycloak (R-04 áp
-- cho nhân sự). Ở đây chỉ giữ mã người dùng Keycloak (user_ref), đơn vị công tác
-- (org_ref) và ảnh chụp vai — không có cột dữ liệu cá nhân nào.
--
-- Mẫu: sổ chỉ ghi thêm account_event (mục 8.1) cho mọi thao tác quản trị — tạo,
-- khóa, mở khóa, đổi đơn vị/vai, gửi lại liên kết kích hoạt — và bảng hiện tại
-- account cập nhật trong CÙNG giao dịch (R-21). account.lock_cause cho phép kích
-- hoạt lại tổ chức chỉ mở đúng các tài khoản bị khóa vì tổ chức bị vô hiệu hóa
-- (HTX US-HTXSTATUS-02 AC-2).

CREATE SCHEMA pf_accounts;
REVOKE ALL ON SCHEMA pf_accounts FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_accounts TO accounts_app;

-- ---------------------------------------------------------------------------
-- Sổ thao tác tài khoản. stream_key = 'user:<user_ref>'. Mỗi dòng mang ảnh chụp
-- trạng thái SAU thao tác (org_ref, roles, enabled, lock_cause) để dựng lại bảng
-- hiện tại từ sổ (R-22).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_accounts.account_event (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  user_ref          uuid        NOT NULL,                 -- id người dùng trong Keycloak
  action            text        NOT NULL CHECK (action IN ('created', 'locked', 'unlocked', 'assignment_changed', 'actions_sent')),
  org_ref           uuid        NOT NULL,
  roles             text[]      NOT NULL,                 -- vai realm do module quản lý, sau thao tác
  enabled           boolean     NOT NULL,
  lock_cause        text        NULL CHECK (lock_cause IN ('manual', 'org_deactivated')),
  reason            text        NULL,                     -- lý do do quản trị viên nhập; không chứa dữ liệu cá nhân
  details           jsonb       NULL,                     -- vd required actions, event_id nguồn (không PII)
  event_time        timestamptz NOT NULL,                 -- R-13
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  -- Khóa và mở khóa bắt buộc lý do ít nhất 20 ký tự (HTX US-HTXSTATUS-01 AC-3).
  CHECK (action NOT IN ('locked', 'unlocked') OR char_length(btrim(reason)) >= 20),
  CHECK (enabled = (lock_cause IS NULL)),
  CHECK (action <> 'locked' OR NOT enabled),
  CHECK (action <> 'unlocked' OR enabled)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX account_event_idem   ON pf_accounts.account_event (tenant_id, idem_key);
CREATE INDEX account_event_stream ON pf_accounts.account_event (tenant_id, stream_key, seq);
CREATE INDEX account_event_user   ON pf_accounts.account_event (tenant_id, user_ref, seq);

SELECT shared.apply_ledger('pf_accounts.account_event', 'pf_accounts.account_event');

-- ---------------------------------------------------------------------------
-- Trạng thái hiện tại của tài khoản (bảng chiếu của account_event, R-21).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_accounts.account (
  tenant_id     uuid        NOT NULL,
  user_ref      uuid        NOT NULL,
  org_ref       uuid        NOT NULL,
  roles         text[]      NOT NULL,
  enabled       boolean     NOT NULL,
  lock_cause    text        NULL CHECK (lock_cause IN ('manual', 'org_deactivated')),
  last_event_id uuid        NOT NULL,
  last_seq      bigint      NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, user_ref),
  CHECK (enabled = (lock_cause IS NULL))
);
CREATE INDEX account_org ON pf_accounts.account (tenant_id, org_ref, lock_cause);
SELECT shared.apply_tenant_rls('pf_accounts.account');

-- ---------------------------------------------------------------------------
-- Quyền của accounts_app. Sổ: chỉ SELECT, INSERT. Bảng hiện tại: không DELETE.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_accounts.account_event TO accounts_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_accounts.account_event FROM accounts_app;
GRANT SELECT, INSERT, UPDATE ON pf_accounts.account TO accounts_app;
