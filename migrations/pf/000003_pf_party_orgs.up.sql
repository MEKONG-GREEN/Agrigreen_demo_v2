-- pf_party: tổ chức, cây phân cấp TW → Tỉnh → Xã, HTX/THT và nông hộ (ADR-0009).
--
-- KN FN-03/FN-04 (US-ADM-07/08, US-HTX-01..03), HTX FN-04/05/15 (US-HTX-01,
-- US-HH-01/02, US-HTXSTATUS-01..03), CGH FN-03.
--
-- Mẫu: sổ chỉ ghi thêm org_event (mục 8.1) cho mọi biến động của tổ chức — tạo,
-- di chuyển (sáp nhập), đổi tên, vô hiệu hóa, kích hoạt lại, kèm lý do — và bảng
-- trạng thái hiện tại org/cooperative cập nhật trong CÙNG giao dịch (R-21).
--
-- KHÔNG có dữ liệu cá nhân ở đây (R-04): người đại diện HTX và chủ hộ nằm ở kho
-- định danh, chỉ giữ *_ref.

-- PostGIS: extension không "trusted" nên ag_migrator (không phải superuser) không
-- tự tạo được. Quản trị cụm tạo trước trong database ag_pf (bootstrap); câu lệnh
-- dưới đây khi đó chỉ là no-op.
CREATE EXTENSION IF NOT EXISTS postgis;

-- ---------------------------------------------------------------------------
-- Sổ biến động tổ chức. stream_key = 'org:<org_ref>'. Mỗi dòng mang ảnh chụp
-- đầy đủ trạng thái SAU sự việc để dựng lại bảng hiện tại từ sổ (R-22).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.org_event (
  id                 uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,
  seq                bigint      NOT NULL,
  org_ref            uuid        NOT NULL,
  action             text        NOT NULL CHECK (action IN ('created', 'moved', 'renamed', 'deactivated', 'reactivated')),
  kind               text        NOT NULL CHECK (kind IN ('ttkn_national', 'ttkn_province', 'extension_team', 'dcrd', 'cooperative', 'mkg', 'partner')),
  code               text        NOT NULL,
  name               text        NOT NULL,
  admin_unit_code    text        NULL,
  parent_ref         uuid        NULL,
  prev_parent_ref    uuid        NULL,                     -- trước khi di chuyển
  prev_admin_unit    text        NULL,                     -- mã đơn vị cũ (sáp nhập)
  representative_ref uuid        NULL,                     -- mã tham chiếu vault
  active             boolean     NOT NULL,
  reason             text        NULL,
  details            jsonb       NULL,                     -- hồ sơ HTX (không PII)
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
  -- US-HTXSTATUS-01 AC-3: lý do vô hiệu hóa ít nhất 20 ký tự.
  CHECK (action <> 'deactivated' OR char_length(btrim(reason)) >= 20),
  CHECK (action <> 'moved' OR char_length(btrim(reason)) >= 10)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX org_event_idem   ON pf_party.org_event (tenant_id, idem_key);
CREATE INDEX org_event_stream ON pf_party.org_event (tenant_id, stream_key, seq);
CREATE INDEX org_event_org    ON pf_party.org_event (tenant_id, org_ref, seq);

SELECT shared.apply_ledger('pf_party.org_event', 'pf_party.org_event');

-- ---------------------------------------------------------------------------
-- Trạng thái hiện tại của tổ chức (bảng chiếu của org_event, R-21).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.org (
  tenant_id          uuid        NOT NULL,
  ref                uuid        NOT NULL,
  kind               text        NOT NULL CHECK (kind IN ('ttkn_national', 'ttkn_province', 'extension_team', 'dcrd', 'cooperative', 'mkg', 'partner')),
  code               text        NOT NULL CHECK (length(code) BETWEEN 1 AND 40),
  name               text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  admin_unit_code    text        NULL,
  parent_ref         uuid        NULL,
  representative_ref uuid        NULL,
  active             boolean     NOT NULL DEFAULT true,
  status_reason      text        NULL,
  last_event_id      uuid        NOT NULL,
  last_seq           bigint      NOT NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, ref),
  UNIQUE (tenant_id, code),
  FOREIGN KEY (tenant_id, parent_ref) REFERENCES pf_party.org (tenant_id, ref),
  CHECK (parent_ref IS NULL OR parent_ref <> ref)
);
CREATE INDEX org_parent ON pf_party.org (tenant_id, parent_ref);
CREATE INDEX org_unit   ON pf_party.org (tenant_id, admin_unit_code);
SELECT shared.apply_tenant_rls('pf_party.org');

-- Mã HTX do hệ thống tự sinh (CGH BR-01): HTX000001, HTX000002…
CREATE SEQUENCE pf_party.cooperative_code_seq;

-- Hồ sơ HTX/THT (CGH FN-03, HTX US-HTX-01). Xã = org.admin_unit_code.
CREATE TABLE pf_party.cooperative (
  tenant_id      uuid                  NOT NULL,
  org_ref        uuid                  NOT NULL,
  tax_code       text                  NULL CHECK (tax_code ~ '^[0-9]{10}(-[0-9]{3})?$'),
  area_ha        numeric(14,4)         NULL CHECK (area_ha >= 0),   -- diện tích đăng ký hành chính
  established_on date                  NULL,
  location       geometry(Point, 4326) NULL,                        -- NULL → hiển thị tâm xã, nhãn "vị trí tạm"
  updated_at     timestamptz           NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, org_ref),
  FOREIGN KEY (tenant_id, org_ref) REFERENCES pf_party.org (tenant_id, ref)
);
-- Mã số thuế duy nhất trong tenant (NULL được lặp).
CREATE UNIQUE INDEX cooperative_tax_code ON pf_party.cooperative (tenant_id, tax_code);
CREATE INDEX cooperative_location ON pf_party.cooperative USING gist (location);
SELECT shared.apply_tenant_rls('pf_party.cooperative');

-- ---------------------------------------------------------------------------
-- Nông hộ: CHỈ mã tham chiếu vault và thuộc tính không định danh.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.household (
  tenant_id          uuid          NOT NULL,
  household_ref      uuid          NOT NULL,   -- = Created.Ref của kho định danh (R-41)
  commune_code       text          NOT NULL,
  cultivated_area_ha numeric(14,4) NULL CHECK (cultivated_area_ha >= 0),
  active             boolean       NOT NULL DEFAULT true,
  created_by         uuid          NOT NULL,
  created_at         timestamptz   NOT NULL DEFAULT now(),
  updated_at         timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, household_ref)
);
CREATE INDEX household_commune ON pf_party.household (tenant_id, commune_code);
SELECT shared.apply_tenant_rls('pf_party.household');

-- Thành viên HTX theo thời gian. KN US-HTX-02: một hộ gắn đúng một HTX tại một thời điểm.
CREATE TABLE pf_party.household_membership (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,
  household_ref uuid        NOT NULL,
  org_ref       uuid        NOT NULL,
  since_date    date        NOT NULL,
  until_date    date        NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, household_ref) REFERENCES pf_party.household (tenant_id, household_ref),
  FOREIGN KEY (tenant_id, org_ref) REFERENCES pf_party.org (tenant_id, ref),
  CHECK (until_date IS NULL OR until_date >= since_date)
);
CREATE UNIQUE INDEX household_membership_open ON pf_party.household_membership (tenant_id, household_ref) WHERE until_date IS NULL;
CREATE INDEX household_membership_org ON pf_party.household_membership (tenant_id, org_ref, household_ref);
SELECT shared.apply_tenant_rls('pf_party.household_membership');

-- ---------------------------------------------------------------------------
-- Quyền của party_app. Sổ: chỉ SELECT, INSERT. Bảng hiện tại: không DELETE
-- (không xóa cứng dữ liệu nghiệp vụ — vô hiệu hóa là trạng thái).
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_party.org_event TO party_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.org_event FROM party_app;
GRANT SELECT, INSERT, UPDATE ON pf_party.org, pf_party.cooperative, pf_party.household, pf_party.household_membership TO party_app;
GRANT USAGE ON SEQUENCE pf_party.cooperative_code_seq TO party_app;
