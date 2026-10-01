-- pf_party KN v7.7 (gói P2): phòng ban theo đơn vị, khu vực theo tỉnh, gán khu vực cho Nút xã.
--
-- FN-AD-04.02 (cây 3 cấp TTKNQG → TTKN tỉnh → Xã, Khu vực là THUỘC TÍNH của Nút xã), FN-AD-04.03
-- (danh mục phòng ban, mỗi phòng ban là một module; trạng thái Đang áp dụng / Chưa mở / Ngưng hoạt
-- động điều khiển bước 2 đăng nhập và menu), UR-KT-02 (bật phòng ban bằng cấu hình, không phát hành
-- lại ứng dụng). docs/brd/kn-v7.7-plan.md §2 (pf 000062), §3 P2.
--
-- Mẫu như org_event (mục 8.1): mỗi thực thể một sổ chỉ ghi thêm mang ảnh chụp TRƯỚC (before) và SAU
-- (after) sự việc để màn Detail hiện "người, thời điểm, giá trị cũ → mới" (AC-CHUNG), cùng bảng chiếu
-- hiện hành cập nhật trong CÙNG giao dịch (R-21). Không xóa cứng: "xóa" phòng ban = sự kiện
-- 'retired' (chỉ khi chưa có tài khoản/dữ liệu — kiểm ở app/ qua port), khu vực = ngừng dùng.
--
-- Không có dữ liệu cá nhân (R-04): chỉ mã, tên đơn vị/khu vực và actor_ref.

-- ---------------------------------------------------------------------------
-- 1. Phòng ban (module) theo đơn vị — đơn vị là Nút cấp 1 (ttkn_national) hoặc cấp 2 (ttkn_province).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.department_event (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'dept:<department_id>'
  seq               bigint      NOT NULL,
  department_id     uuid        NOT NULL,
  org_ref           uuid        NOT NULL,                 -- đơn vị (Nút cấp 1/2)
  code              text        NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]{1,39}$'),
  action            text        NOT NULL CHECK (action IN ('created', 'updated', 'status_changed', 'retired')),
  status            text        NOT NULL CHECK (status IN ('active', 'not_open', 'suspended')),
  before            jsonb       NULL,                     -- ảnh chụp trước (NULL khi created)
  after             jsonb       NOT NULL,                 -- ảnh chụp sau
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
  -- Ngưng hoạt động và "xóa" phải có căn cứ.
  CHECK (action <> 'retired' OR char_length(btrim(coalesce(reason, ''))) >= 10),
  CHECK (action <> 'status_changed' OR status <> 'suspended' OR char_length(btrim(coalesce(reason, ''))) >= 10),
  CHECK (action = 'created' OR before IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX department_event_stream ON pf_party.department_event (tenant_id, stream_key, seq);
CREATE INDEX department_event_idem   ON pf_party.department_event (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_party.department_event', 'pf_party.department_event');

CREATE TABLE pf_party.department (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,
  org_ref       uuid        NOT NULL,
  code          text        NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]{1,39}$'),
  name          text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
  description   text        NULL CHECK (description IS NULL OR char_length(description) <= 1000),
  sort_order    integer     NOT NULL DEFAULT 0 CHECK (sort_order BETWEEN 0 AND 100000),
  status        text        NOT NULL CHECK (status IN ('active', 'not_open', 'suspended')),
  -- Bộ menu dựng sẵn trong mã (bảng tĩnh có phiên bản, party/domain/menu.go): 'trong_trot' hoặc
  -- 'generic' — phòng ban mới bật bằng cấu hình dùng 'generic' (UR-KT-02).
  menu_profile  text        NOT NULL DEFAULT 'generic' CHECK (menu_profile ~ '^[a-z][a-z0-9_]{1,39}$'),
  retired       boolean     NOT NULL DEFAULT false,
  last_event_id uuid        NOT NULL,
  last_seq      bigint      NOT NULL,
  updated_by    uuid        NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  -- FN-AD-04.03: trùng mã trong đơn vị → chặn (kể cả mã đã "xóa": mã không tái sử dụng).
  UNIQUE (tenant_id, org_ref, code),
  FOREIGN KEY (tenant_id, org_ref) REFERENCES pf_party.org (tenant_id, ref)
);
CREATE INDEX department_org ON pf_party.department (tenant_id, org_ref, sort_order, code);
SELECT shared.apply_tenant_rls('pf_party.department');

-- ---------------------------------------------------------------------------
-- 2. Khu vực theo tỉnh (nhóm xã theo địa bàn huyện cũ — để sắp xếp và cộng tổng trong báo cáo).
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.region_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'region:<region_id>'
  seq               bigint      NOT NULL,
  region_id         uuid        NOT NULL,
  province_code     text        NOT NULL,                 -- mã ĐVHC tỉnh (masterdata)
  code              text        NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$'),
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
CREATE INDEX region_event_stream ON pf_party.region_event (tenant_id, stream_key, seq);
CREATE INDEX region_event_idem   ON pf_party.region_event (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_party.region_event', 'pf_party.region_event');

CREATE TABLE pf_party.region (
  tenant_id           uuid        NOT NULL,
  id                  uuid        NOT NULL,
  province_code       text        NOT NULL,
  code                text        NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$'),
  name                text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
  sort_order          integer     NOT NULL DEFAULT 0 CHECK (sort_order BETWEEN 0 AND 100000),
  active              boolean     NOT NULL DEFAULT true,
  deactivation_reason text        NULL,
  last_event_id       uuid        NOT NULL,
  last_seq            bigint      NOT NULL,
  updated_by          uuid        NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, province_code, code),
  CHECK (active = (deactivation_reason IS NULL))
);
CREATE INDEX region_province ON pf_party.region (tenant_id, province_code, sort_order, code);
SELECT shared.apply_tenant_rls('pf_party.region');

-- ---------------------------------------------------------------------------
-- 3. Gán khu vực cho Nút xã (extension_team) — sổ + bảng chiếu. region_code NULL = bỏ gán.
-- Số liệu đã chốt giữ cấu trúc lúc chốt: P4 chụp khu vực vào consolidation_run, không đọc lại sổ này.
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.org_region_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'org-region:<org_ref>'
  seq               bigint      NOT NULL,
  org_ref           uuid        NOT NULL,
  commune_code      text        NOT NULL,                 -- mã ĐVHC xã của Nút lúc gán
  province_code     text        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('assigned', 'changed', 'cleared')),
  region_code       text        NULL,
  prev_region_code  text        NULL,
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
  CHECK ((action = 'cleared') = (region_code IS NULL)),
  CHECK (action = 'assigned' OR prev_region_code IS NOT NULL),
  -- Đổi hoặc bỏ khu vực của xã đã gán phải có lý do (FN-AD-04.02: mọi thay đổi lưu vết).
  CHECK (action = 'assigned' OR char_length(btrim(coalesce(reason, ''))) >= 10)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX org_region_event_stream ON pf_party.org_region_event (tenant_id, stream_key, seq);
CREATE INDEX org_region_event_idem   ON pf_party.org_region_event (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_party.org_region_event', 'pf_party.org_region_event');

CREATE TABLE pf_party.org_region (
  tenant_id     uuid        NOT NULL,
  org_ref       uuid        NOT NULL,
  commune_code  text        NOT NULL,
  province_code text        NOT NULL,
  region_code   text        NULL,
  last_event_id uuid        NOT NULL,
  last_seq      bigint      NOT NULL,
  updated_by    uuid        NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, org_ref),
  FOREIGN KEY (tenant_id, org_ref) REFERENCES pf_party.org (tenant_id, ref)
);
CREATE INDEX org_region_region ON pf_party.org_region (tenant_id, province_code, region_code);
SELECT shared.apply_tenant_rls('pf_party.org_region');

-- ---------------------------------------------------------------------------
-- 4. Quyền của party_app (R-05). Sổ: chỉ SELECT, INSERT. Bảng chiếu: không DELETE.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_party.department_event, pf_party.region_event, pf_party.org_region_event TO party_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.department_event, pf_party.region_event, pf_party.org_region_event FROM party_app;
GRANT SELECT, INSERT, UPDATE ON pf_party.department, pf_party.region, pf_party.org_region TO party_app;
REVOKE DELETE, TRUNCATE ON pf_party.department, pf_party.region, pf_party.org_region FROM party_app;
