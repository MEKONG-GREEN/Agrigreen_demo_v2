-- pf_accounts KN v7.7 (gói P1): gán vai trò nhiều vai × Nút × phòng ban × xã phụ trách, ngữ cảnh phiên.
--
-- FN-CH-00.01 (đăng nhập, chọn vai trò & phòng ban, đổi không đăng nhập lại), FN-AD-04.04 (tài khoản và
-- phân quyền: một tài khoản nhiều vai trò, mỗi vai trò có Nút, phòng ban, cấp quản lý, xã phụ trách
-- riêng; Log chỉnh sửa người/thời điểm/cũ → mới). docs/brd/kn-v7.7-plan.md §2 (pf 000060), §3 P1;
-- docs/brd/kn-v7.7-gap.md §4.5 phương án A: Keycloak chỉ xác thực, vai KN nằm ở đây.
--
-- Không có dữ liệu cá nhân (R-04, RB-3): chỉ user_ref (id Keycloak), mã Nút, mã phòng ban, mã xã, cấp
-- quản lý (danh mục, Q-05) và actor_ref. Tên hiển thị trong Log tra lúc đọc.

-- ---------------------------------------------------------------------------
-- 1. Sổ gán vai trò. stream_key = 'asg:<assignment_id>'. Mỗi dòng mang ảnh chụp TRƯỚC/SAU (before/after,
-- không PII) để khung Log hiện cũ → mới, và các cột trạng thái SAU sự việc để dựng lại bảng chiếu (R-22).
-- "Xóa vai trò" = revoked (không xóa dữ liệu đã nhập bằng vai trò đó — FN-AD-04.04).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_accounts.role_assignment_event (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  assignment_id     uuid        NOT NULL,
  user_ref          uuid        NOT NULL,                 -- id người dùng Keycloak
  action            text        NOT NULL CHECK (action IN ('granted', 'changed', 'revoked')),
  role              text        NOT NULL CHECK (role IN ('kn_tw', 'kn_tinh', 'kn_xa', 'admin')),
  org_ref           uuid        NULL,                     -- Nút; NULL chỉ với admin (toàn hệ thống)
  unit_ref          uuid        NULL,                     -- đơn vị sở hữu phòng ban (Nút cấp 1/2)
  department_codes  text[]      NOT NULL DEFAULT '{}',
  mgmt_level        text        NULL CHECK (mgmt_level IS NULL OR mgmt_level ~ '^[a-z][a-z0-9_]{1,39}$'),
  commune_codes     text[]      NOT NULL DEFAULT '{}',
  valid_from        timestamptz NOT NULL,
  valid_to          timestamptz NULL,
  before            jsonb       NULL,
  after             jsonb       NOT NULL,
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
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
  CHECK (valid_to IS NULL OR valid_to > valid_from),
  -- Admin: không phòng ban, không xã; vai khác: có Nút và ít nhất một phòng ban (UR-AD-04).
  CHECK ((role = 'admin') = (cardinality(department_codes) = 0)),
  CHECK (role = 'admin' OR org_ref IS NOT NULL),
  CHECK (role = 'kn_xa' OR cardinality(commune_codes) = 0),
  -- Thu hồi phải có căn cứ.
  CHECK (action <> 'revoked' OR char_length(btrim(coalesce(reason, ''))) >= 10),
  CHECK (action = 'granted' OR before IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX role_assignment_event_stream ON pf_accounts.role_assignment_event (tenant_id, stream_key, seq);
CREATE INDEX role_assignment_event_idem   ON pf_accounts.role_assignment_event (tenant_id, idem_key);
CREATE INDEX role_assignment_event_user   ON pf_accounts.role_assignment_event (tenant_id, user_ref, seq);
SELECT shared.apply_ledger('pf_accounts.role_assignment_event', 'pf_accounts.role_assignment_event');

-- Bảng chiếu hiện hành (R-21), cập nhật trong CÙNG giao dịch với dòng sổ.
CREATE TABLE pf_accounts.role_assignment_current (
  tenant_id        uuid        NOT NULL,
  assignment_id    uuid        NOT NULL,
  user_ref         uuid        NOT NULL,
  role             text        NOT NULL CHECK (role IN ('kn_tw', 'kn_tinh', 'kn_xa', 'admin')),
  org_ref          uuid        NULL,
  unit_ref         uuid        NULL,
  department_codes text[]      NOT NULL DEFAULT '{}',
  mgmt_level       text        NULL,
  commune_codes    text[]      NOT NULL DEFAULT '{}',
  valid_from       timestamptz NOT NULL,
  valid_to         timestamptz NULL,
  active           boolean     NOT NULL,                 -- false sau revoked
  last_event_id    uuid        NOT NULL,
  last_seq         bigint      NOT NULL,
  created_by       uuid        NOT NULL,
  created_at       timestamptz NOT NULL,
  updated_by       uuid        NOT NULL,
  updated_at       timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, assignment_id)
);
CREATE INDEX role_assignment_current_user ON pf_accounts.role_assignment_current (tenant_id, user_ref, active);
CREATE INDEX role_assignment_current_unit ON pf_accounts.role_assignment_current (tenant_id, unit_ref, active);
CREATE INDEX role_assignment_current_org  ON pf_accounts.role_assignment_current (tenant_id, org_ref, active);
CREATE INDEX role_assignment_current_dept ON pf_accounts.role_assignment_current USING gin (department_codes);
CREATE INDEX role_assignment_current_comm ON pf_accounts.role_assignment_current USING gin (commune_codes);
SELECT shared.apply_tenant_rls('pf_accounts.role_assignment_current');

-- ---------------------------------------------------------------------------
-- 2. Ngữ cảnh phiên (gap §4.5): một dòng mỗi lần chọn vai trò + phòng ban. Bất biến — đổi ngữ cảnh tạo
-- dòng mới; hết hạn theo expires_at. Mọi request mang X-AG-Context được kiểm lại với bảng chiếu gán vai
-- trò, trạng thái tài khoản và phòng ban HIỆN HÀNH (thu hồi có hiệu lực ở request kế tiếp).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_accounts.session_context (
  tenant_id          uuid        NOT NULL,
  context_id         uuid        NOT NULL,              -- UUIDv7, gửi qua header X-AG-Context
  user_ref           uuid        NOT NULL,              -- = sub của token đã chọn
  assignment_id      uuid        NULL,                  -- NULL: ngữ cảnh hỗ trợ không gắn tài khoản
  role               text        NOT NULL CHECK (role IN ('kn_tw', 'kn_tinh', 'kn_xa', 'admin')),
  department_code    text        NULL,
  org_ref            uuid        NULL,
  unit_ref           uuid        NULL,
  commune_codes      text[]      NOT NULL DEFAULT '{}',
  support_session_id uuid        NULL,                  -- FN-AD-04.07 (000061)
  on_behalf_of       uuid        NULL,
  idem_key           text        NOT NULL,
  created_at         timestamptz NOT NULL,
  expires_at         timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, context_id),
  CHECK (expires_at > created_at),
  CHECK ((role = 'admin') = (department_code IS NULL))
);
CREATE INDEX session_context_user ON pf_accounts.session_context (tenant_id, user_ref, created_at);
CREATE INDEX session_context_idem ON pf_accounts.session_context (tenant_id, user_ref, idem_key);
SELECT shared.apply_tenant_rls('pf_accounts.session_context');

-- ---------------------------------------------------------------------------
-- 3. Quyền của accounts_app (R-05). Sổ: chỉ SELECT, INSERT. Bảng chiếu: không DELETE. Ngữ cảnh: bất biến.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_accounts.role_assignment_event TO accounts_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_accounts.role_assignment_event FROM accounts_app;
GRANT SELECT, INSERT, UPDATE ON pf_accounts.role_assignment_current TO accounts_app;
REVOKE DELETE, TRUNCATE ON pf_accounts.role_assignment_current FROM accounts_app;
GRANT SELECT, INSERT ON pf_accounts.session_context TO accounts_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_accounts.session_context FROM accounts_app;
