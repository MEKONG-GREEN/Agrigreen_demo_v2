-- W11.07 — Phân quyền trong vai + đơn vị nội bộ (BRD KN 10/2026: D4 X3, D3 H0 + C3–C5; Q-059, Q-067, Q-068).
--
-- 1. pf_accounts.assignment_attr_event/_current: SỔ PHỤ thuộc tính vai cạnh sổ gán vai trò (không thêm cột
--    vào role_assignment_event — bài học W4.1): nhóm Cấp Tỉnh (5 nhóm CNTS), cờ nhiệm vụ (QTNV báo giá,
--    người phụ trách biểu), đơn vị nội bộ (Cấp Xã: đơn vị quản lý HS-37), phạm vi Cấp Xã theo cơ sở/hộ.
-- 2. pf_accounts.delegation_event/_current: ủy quyền CÓ HẠN (vd lãnh đạo phòng giao "mở lại kỳ" cho cán bộ
--    tổng hợp). valid_to bắt buộc; hết hạn tự mất hiệu lực, không cần job.
-- 3. pf_accounts.internal_unit_event/internal_unit: danh mục đơn vị nội bộ của TTKN tỉnh (BGĐ, văn phòng,
--    phòng, trạm, trại, tăng cường) — nơi chứa người (đơn vị công tác, đơn vị quản lý cán bộ xã HS-37). Phòng
--    (kind = 'phong') trỏ mã phòng ban P2 của pf/party (department_code, kiểm ở tầng ứng dụng qua cổng —
--    không FK chéo schema). Đặt ở pf_accounts (một migration, một module — archlint schema); không chèn
--    trạm/trại vào pf_party.department (menu, ngữ cảnh đăng nhập không đổi). org_ref = Nút cấp 1/2 (pf/party).
--
-- Không có dữ liệu cá nhân (R-04): chỉ mã tham chiếu, mã danh mục. Danh mục mã (nhóm, cờ, quyền, loại đơn
-- vị) là tham số có phiên bản trong mã (accounts.kn-roles/2, accounts.internal-unit/1) — DB chỉ kiểm dạng mã.

-- ---------------------------------------------------------------------------
-- 1. Thuộc tính vai. stream_key = 'asgattr:<assignment_id>'.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_accounts.assignment_attr_event (
  id                 uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,
  seq                bigint      NOT NULL,
  assignment_id      uuid        NOT NULL,
  user_ref           uuid        NOT NULL,
  role               text        NOT NULL CHECK (role IN ('kn_tw', 'kn_tinh', 'kn_xa', 'admin')),
  province_group     text        NULL CHECK (province_group IS NULL OR province_group ~ '^[a-z][a-z0-9_]{1,39}$'),
  duties             text[]      NOT NULL DEFAULT '{}',
  internal_unit_code text        NULL CHECK (internal_unit_code IS NULL OR internal_unit_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  facility_refs      uuid[]      NOT NULL DEFAULT '{}',
  household_refs     uuid[]      NOT NULL DEFAULT '{}',
  before             jsonb       NULL,
  after              jsonb       NOT NULL,
  reason             text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
  event_time         timestamptz NOT NULL,                 -- R-13
  recorded_at        timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (array_to_string(duties, ',') ~ '^([a-z][a-z0-9_]{1,39}(,[a-z][a-z0-9_]{1,39})*)?$'),
  -- Nhóm và cờ nhiệm vụ chỉ của Cấp Tỉnh; giới hạn cơ sở/hộ chỉ của Cấp Xã (D4 X3).
  CHECK (role = 'kn_tinh' OR (province_group IS NULL AND cardinality(duties) = 0)),
  CHECK (role = 'kn_xa' OR (cardinality(facility_refs) = 0 AND cardinality(household_refs) = 0))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX assignment_attr_event_stream ON pf_accounts.assignment_attr_event (tenant_id, stream_key, seq);
CREATE INDEX assignment_attr_event_idem   ON pf_accounts.assignment_attr_event (tenant_id, idem_key);
CREATE INDEX assignment_attr_event_user   ON pf_accounts.assignment_attr_event (tenant_id, user_ref, seq);
SELECT shared.apply_ledger('pf_accounts.assignment_attr_event', 'pf_accounts.assignment_attr_event');

-- Bảng chiếu hiện hành (R-21), cập nhật trong CÙNG giao dịch với dòng sổ.
CREATE TABLE pf_accounts.assignment_attr_current (
  tenant_id          uuid        NOT NULL,
  assignment_id      uuid        NOT NULL,
  user_ref           uuid        NOT NULL,
  role               text        NOT NULL,
  province_group     text        NULL,
  duties             text[]      NOT NULL DEFAULT '{}',
  internal_unit_code text        NULL,
  facility_refs      uuid[]      NOT NULL DEFAULT '{}',
  household_refs     uuid[]      NOT NULL DEFAULT '{}',
  last_event_id      uuid        NOT NULL,
  last_seq           bigint      NOT NULL,
  updated_by         uuid        NOT NULL,
  updated_at         timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, assignment_id)
);
CREATE INDEX assignment_attr_current_user ON pf_accounts.assignment_attr_current (tenant_id, user_ref);
CREATE INDEX assignment_attr_current_unit ON pf_accounts.assignment_attr_current (tenant_id, internal_unit_code);
CREATE INDEX assignment_attr_current_duty ON pf_accounts.assignment_attr_current USING gin (duties);
SELECT shared.apply_tenant_rls('pf_accounts.assignment_attr_current');

-- ---------------------------------------------------------------------------
-- 2. Ủy quyền có hạn. stream_key = 'deleg:<delegation_id>'.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_accounts.delegation_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  delegation_id     uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('granted', 'revoked')),
  grantor_ref       uuid        NOT NULL,                 -- người giao (lãnh đạo phòng)
  grantee_ref       uuid        NOT NULL,                 -- người nhận
  unit_ref          uuid        NOT NULL,                 -- đơn vị sở hữu phòng ban (Nút cấp 1/2)
  department_code   text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  power             text        NOT NULL CHECK (power ~ '^[a-z][a-z0-9_]{1,39}$'),
  valid_from        timestamptz NOT NULL,
  valid_to          timestamptz NOT NULL,
  reason            text        NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 500),
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (grantor_ref <> grantee_ref),
  -- Có hạn: kết thúc sau bắt đầu, trần cứng 1 năm (trần nghiệp vụ là tham số trong mã, mặc định 30 ngày).
  CHECK (valid_to > valid_from AND valid_to <= valid_from + interval '366 days')
) PARTITION BY RANGE (recorded_at);
CREATE INDEX delegation_event_stream ON pf_accounts.delegation_event (tenant_id, stream_key, seq);
CREATE INDEX delegation_event_idem   ON pf_accounts.delegation_event (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_accounts.delegation_event', 'pf_accounts.delegation_event');

CREATE TABLE pf_accounts.delegation_current (
  tenant_id       uuid        NOT NULL,
  delegation_id   uuid        NOT NULL,
  grantor_ref     uuid        NOT NULL,
  grantee_ref     uuid        NOT NULL,
  unit_ref        uuid        NOT NULL,
  department_code text        NOT NULL,
  power           text        NOT NULL,
  valid_from      timestamptz NOT NULL,
  valid_to        timestamptz NOT NULL,
  revoked         boolean     NOT NULL DEFAULT false,
  reason          text        NOT NULL,
  last_event_id   uuid        NOT NULL,
  last_seq        bigint      NOT NULL,
  created_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL,
  updated_at      timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, delegation_id),
  CHECK (valid_to > valid_from)
);
CREATE INDEX delegation_current_grantee ON pf_accounts.delegation_current (tenant_id, grantee_ref, department_code, valid_to);
CREATE INDEX delegation_current_grantor ON pf_accounts.delegation_current (tenant_id, grantor_ref, valid_to);
SELECT shared.apply_tenant_rls('pf_accounts.delegation_current');

REVOKE ALL ON pf_accounts.assignment_attr_event, pf_accounts.assignment_attr_current,
  pf_accounts.delegation_event, pf_accounts.delegation_current FROM PUBLIC;
GRANT SELECT, INSERT ON pf_accounts.assignment_attr_event, pf_accounts.delegation_event TO r_accounts;
GRANT SELECT, INSERT, UPDATE ON pf_accounts.assignment_attr_current, pf_accounts.delegation_current TO r_accounts;

-- ---------------------------------------------------------------------------
-- 3. Đơn vị nội bộ. stream_key = 'iunit:<unit_id>'.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_accounts.internal_unit_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  unit_id           uuid        NOT NULL,
  org_ref           uuid        NOT NULL,                 -- đơn vị (Nút cấp 1/2)
  code              text        NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]{1,39}$'),
  kind              text        NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]{1,39}$'),
  action            text        NOT NULL CHECK (action IN ('created', 'updated', 'deactivated', 'reactivated')),
  before            jsonb       NULL,
  after             jsonb       NOT NULL,
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
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (action <> 'deactivated' OR char_length(btrim(coalesce(reason, ''))) >= 10),
  CHECK (action = 'created' OR before IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX internal_unit_event_stream ON pf_accounts.internal_unit_event (tenant_id, stream_key, seq);
CREATE INDEX internal_unit_event_idem   ON pf_accounts.internal_unit_event (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_accounts.internal_unit_event', 'pf_accounts.internal_unit_event');

CREATE TABLE pf_accounts.internal_unit (
  tenant_id       uuid        NOT NULL,
  id              uuid        NOT NULL,
  org_ref         uuid        NOT NULL,
  code            text        NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]{1,39}$'),
  kind            text        NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_]{1,39}$'),
  title           text        NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200), -- tên đơn vị (cột không mang tên "name": cổng R-04 TestNoPIIColumns)
  department_code text        NULL CHECK (department_code IS NULL OR department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  sort_order      integer     NOT NULL DEFAULT 0 CHECK (sort_order BETWEEN 0 AND 100000),
  active          boolean     NOT NULL DEFAULT true,
  last_event_id   uuid        NOT NULL,
  last_seq        bigint      NOT NULL,
  updated_by      uuid        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  -- Mã không tái sử dụng trong đơn vị (kể cả đơn vị đã ngừng).
  UNIQUE (tenant_id, org_ref, code)
);
CREATE INDEX internal_unit_org ON pf_accounts.internal_unit (tenant_id, org_ref, sort_order, code);
SELECT shared.apply_tenant_rls('pf_accounts.internal_unit');

REVOKE ALL ON pf_accounts.internal_unit_event, pf_accounts.internal_unit FROM PUBLIC;
GRANT SELECT, INSERT ON pf_accounts.internal_unit_event TO r_accounts;
GRANT SELECT, INSERT, UPDATE ON pf_accounts.internal_unit TO r_accounts;
