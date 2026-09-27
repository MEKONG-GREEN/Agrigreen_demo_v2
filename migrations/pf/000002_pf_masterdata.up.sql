-- pf_masterdata: dữ liệu chủ dùng chung (mục 16) — đơn vị hành chính có hiệu lực
-- theo thời gian, danh mục (giống lúa, khâu, loại máy, vụ, dịch hại) và tham số có
-- phiên bản + phê duyệt hai người (R-56).
--
-- Mọi bảng có tenant_id + RLS (ADR-0009: dữ liệu chủ thuộc tenant sở hữu; ĐTK 1 chỉ
-- có một tenant). Không có bảng nào được DELETE: đóng hiệu lực, ngừng dùng hoặc
-- bản ghi bù thay cho xóa.

CREATE SCHEMA pf_masterdata;
REVOKE ALL ON SCHEMA pf_masterdata FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_masterdata TO masterdata_app;

-- ---------------------------------------------------------------------------
-- 1. Đơn vị hành chính có hiệu lực theo thời gian
-- ---------------------------------------------------------------------------
-- Một dòng = một phiên bản hiệu lực của một mã. Cùng một mã có thể được dùng lại
-- cho đơn vị khác sau sắp xếp (vd mã 91: Kiên Giang trước 01/07/2025, An Giang mới
-- sau đó), nên khóa là (mã, ngày bắt đầu hiệu lực). Khoảng hiệu lực [valid_from,
-- valid_to) theo ngày giờ Việt Nam; valid_to không bao gồm.
CREATE TABLE pf_masterdata.admin_unit (
  tenant_id       uuid        NOT NULL,
  code            text        NOT NULL CHECK (code ~ '^[0-9A-Z][0-9A-Z.-]{0,39}$'),
  valid_from      date        NOT NULL,
  valid_to        date        NULL,                    -- NULL = còn hiệu lực
  name            text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  -- district chỉ còn trong dữ liệu lịch sử trước sắp xếp 2025 (hai cấp tỉnh → xã).
  level           text        NOT NULL CHECK (level IN ('province', 'district', 'commune')),
  parent_code     text        NULL,
  -- Đơn vị kế thừa khi bị sáp nhập/chia tách; chỉ có khi đã đóng hiệu lực.
  successor_codes text[]      NOT NULL DEFAULT '{}',
  source          text        NOT NULL CHECK (length(source) BETWEEN 1 AND 300),
  actor_ref       uuid        NOT NULL,
  recorded_at     timestamptz NOT NULL DEFAULT now(),  -- R-13
  closed_by       uuid        NULL,
  closed_at       timestamptz NULL,
  PRIMARY KEY (tenant_id, code, valid_from),
  CHECK (valid_to IS NULL OR valid_to > valid_from),
  CHECK ((level = 'province') = (parent_code IS NULL)),
  CHECK (valid_to IS NOT NULL OR cardinality(successor_codes) = 0),
  CHECK (closed_at IS NULL OR valid_to IS NOT NULL),
  CHECK ((closed_at IS NULL) = (closed_by IS NULL))
);
CREATE INDEX admin_unit_parent ON pf_masterdata.admin_unit (tenant_id, parent_code, valid_from);
SELECT shared.apply_tenant_rls('pf_masterdata.admin_unit');

-- Bảo vệ lịch sử địa giới:
--   - không xóa (trigger dưới), chỉ đóng hiệu lực;
--   - UPDATE chỉ được đóng một phiên bản đang mở (đặt valid_to, successor_codes,
--     closed_by, closed_at) — mọi cột khác bất biến, phiên bản đã đóng bất biến;
--   - không có hai phiên bản chồng lấp hiệu lực cho cùng một mã. Khóa tư vấn theo
--     (tenant, mã) tuần tự hóa các lần ghi cùng mã nên kiểm tra không bị đua.
CREATE FUNCTION pf_masterdata.fn_admin_unit_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' THEN
    IF OLD.valid_to IS NOT NULL THEN
      RAISE EXCEPTION 'admin_unit %/%: phiên bản đã đóng hiệu lực là bất biến', OLD.code, OLD.valid_from
        USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.tenant_id <> OLD.tenant_id OR NEW.code <> OLD.code OR NEW.valid_from <> OLD.valid_from
       OR NEW.name <> OLD.name OR NEW.level <> OLD.level OR NEW.parent_code IS DISTINCT FROM OLD.parent_code
       OR NEW.source <> OLD.source OR NEW.actor_ref <> OLD.actor_ref OR NEW.recorded_at <> OLD.recorded_at THEN
      RAISE EXCEPTION 'admin_unit %/%: chỉ được đóng hiệu lực, không được sửa nội dung', OLD.code, OLD.valid_from
        USING ERRCODE = 'insufficient_privilege';
    END IF;
  END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended('pf_masterdata.admin_unit:' || NEW.tenant_id::text || ':' || NEW.code, 0));
  IF EXISTS (
    SELECT 1 FROM pf_masterdata.admin_unit u
     WHERE u.tenant_id = NEW.tenant_id AND u.code = NEW.code AND u.valid_from <> NEW.valid_from
       AND daterange(u.valid_from, u.valid_to) && daterange(NEW.valid_from, NEW.valid_to)) THEN
    RAISE EXCEPTION 'admin_unit %: hiệu lực chồng lấp với phiên bản khác của cùng mã', NEW.code
      USING ERRCODE = 'exclusion_violation';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_admin_unit_guard BEFORE INSERT OR UPDATE ON pf_masterdata.admin_unit
  FOR EACH ROW EXECUTE FUNCTION pf_masterdata.fn_admin_unit_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_masterdata.admin_unit
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_masterdata.admin_unit
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 2. Danh mục dùng chung
-- ---------------------------------------------------------------------------
-- Bảng hiện trạng (bảng chiếu, R-21): cập nhật cùng giao dịch với sổ catalog_change.
-- Không xóa: ngừng dùng kèm lý do.
CREATE TABLE pf_masterdata.catalog_item (
  tenant_id           uuid        NOT NULL,
  catalog             text        NOT NULL CHECK (catalog ~ '^[a-z][a-z_]{1,39}$'),
  code                text        NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$'),
  name                text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  attrs               jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(attrs) = 'object'),
  active              boolean     NOT NULL DEFAULT true,
  deactivation_reason text        NULL,
  revision            integer     NOT NULL DEFAULT 1 CHECK (revision > 0),
  last_change_id      uuid        NOT NULL,             -- dòng sổ catalog_change gần nhất
  updated_by          uuid        NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, catalog, code),
  CHECK (active = (deactivation_reason IS NULL))
);
SELECT shared.apply_tenant_rls('pf_masterdata.catalog_item');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_masterdata.catalog_item
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_masterdata.catalog_item
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- Sổ lịch sử thay đổi danh mục theo mẫu mục 8.1 (BRD CGH FN-02 B4: "Lịch sử thay
-- đổi" thời điểm, người thực hiện, giá trị trước → sau). Mỗi mục một chuỗi băm.
CREATE TABLE pf_masterdata.catalog_change (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'catalog:<catalog>:<code>'
  seq               bigint      NOT NULL,
  catalog           text        NOT NULL,
  code              text        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('created', 'updated', 'deactivated', 'reactivated')),
  before            jsonb       NULL,
  after             jsonb       NOT NULL,
  reason            text        NULL,
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
  CHECK (action NOT IN ('deactivated') OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX catalog_change_stream ON pf_masterdata.catalog_change (tenant_id, stream_key, seq);
CREATE INDEX catalog_change_idem   ON pf_masterdata.catalog_change (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_masterdata.catalog_change', 'pf_masterdata.catalog_change');

-- ---------------------------------------------------------------------------
-- 3. Tham số có phiên bản và phê duyệt (R-56, CGH QT-01/QT-02)
-- ---------------------------------------------------------------------------
-- Bảng quy trình: draft → submitted → approved | rejected. Được UPDATE các cột
-- trạng thái trong lúc chờ duyệt; khi đã approved/rejected thì bất biến (trigger).
CREATE TABLE pf_masterdata.param_version (
  id                uuid        PRIMARY KEY,              -- UUIDv7
  tenant_id         uuid        NOT NULL,
  param_key         text        NOT NULL CHECK (param_key ~ '^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+(:[A-Za-z0-9_.-]+)*$' AND length(param_key) <= 200),
  version           integer     NOT NULL CHECK (version > 0),
  value_num         numeric(24,8) NULL,
  value_text        text        NULL CHECK (value_text IS NULL OR length(value_text) BETWEEN 1 AND 1000),
  unit              text        NOT NULL DEFAULT '' CHECK (length(unit) <= 30),
  effective_from    timestamptz NOT NULL,
  source_doc        text        NOT NULL CHECK (length(source_doc) BETWEEN 1 AND 300),
  correction_reason text        NULL CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  status            text        NOT NULL CHECK (status IN ('draft', 'submitted', 'approved', 'rejected')),
  setter_ref        uuid        NOT NULL,
  submitted_at      timestamptz NULL,
  approver_ref      uuid        NULL,
  decided_at        timestamptz NULL,
  decision_note     text        NULL,
  idem_key          text        NOT NULL,
  created_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, param_key, version),
  UNIQUE (tenant_id, idem_key),
  CHECK ((value_num IS NULL) <> (value_text IS NULL)),
  -- R-56 lớp DB: người phê duyệt/từ chối không bao giờ là người đặt.
  CHECK (approver_ref IS NULL OR approver_ref <> setter_ref),
  CHECK ((status IN ('approved', 'rejected')) = (approver_ref IS NOT NULL AND decided_at IS NOT NULL)),
  CHECK (status = 'draft' OR submitted_at IS NOT NULL),
  CHECK (status <> 'rejected' OR decision_note IS NOT NULL)
);
CREATE INDEX param_version_key ON pf_masterdata.param_version (tenant_id, param_key, version);
SELECT shared.apply_tenant_rls('pf_masterdata.param_version');

CREATE FUNCTION pf_masterdata.fn_param_version_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status IN ('approved', 'rejected') THEN
    RAISE EXCEPTION 'param_version %: phiên bản đã % là bất biến', OLD.id, OLD.status
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id OR NEW.param_key <> OLD.param_key OR NEW.version <> OLD.version
     OR NEW.value_num IS DISTINCT FROM OLD.value_num OR NEW.value_text IS DISTINCT FROM OLD.value_text
     OR NEW.unit <> OLD.unit OR NEW.effective_from <> OLD.effective_from OR NEW.source_doc <> OLD.source_doc
     OR NEW.correction_reason IS DISTINCT FROM OLD.correction_reason OR NEW.setter_ref <> OLD.setter_ref
     OR NEW.idem_key <> OLD.idem_key OR NEW.created_at <> OLD.created_at THEN
    RAISE EXCEPTION 'param_version %: nội dung đề xuất bất biến, chỉ đổi được trạng thái', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NOT ((OLD.status = 'draft' AND NEW.status = 'submitted')
       OR (OLD.status = 'submitted' AND NEW.status IN ('approved', 'rejected'))) THEN
    RAISE EXCEPTION 'param_version %: chuyển trạng thái % → % không hợp lệ', OLD.id, OLD.status, NEW.status
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_param_version_guard BEFORE UPDATE ON pf_masterdata.param_version
  FOR EACH ROW EXECUTE FUNCTION pf_masterdata.fn_param_version_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_masterdata.param_version
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_masterdata.param_version
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- Sổ phiên bản ĐÃ PHÊ DUYỆT — nguồn duy nhất của Params.At.
--
-- Vì sao dùng mẫu sổ mục 8.1 + shared.apply_ledger thay vì một bảng thường có
-- trigger chặn sửa: định mức và ngưỡng quyết định kết quả cân đối của từng vụ
-- (CGH AC-02: vụ cũ tính bằng định mức hiệu lực lúc đó). Sổ cho ba lớp bảo vệ
-- (quyền, trigger chặn cả owner, chuỗi băm theo từng khóa tham số) và được cổng
-- v_missing_immutability kiểm; sửa sai là bản ghi bù (supersedes_id +
-- correction_reason) chứ không phải UPDATE. Bảng param_version ở trên chỉ là
-- quy trình duyệt; nó cũng bất biến khi đã có quyết định.
CREATE TABLE pf_masterdata.param_approved (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'param:<param_key>'
  seq               bigint      NOT NULL,
  param_key         text        NOT NULL,
  version           integer     NOT NULL,
  param_version_id  uuid        NOT NULL,
  value_num         numeric(24,8) NULL,
  value_text        text        NULL,
  unit              text        NOT NULL,
  effective_from    timestamptz NOT NULL,
  source_doc        text        NOT NULL,
  setter_ref        uuid        NOT NULL,
  event_time        timestamptz NOT NULL,                 -- lúc phê duyệt
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,                     -- bản duyệt trước cùng effective_from
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người phê duyệt
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK ((value_num IS NULL) <> (value_text IS NULL)),
  CHECK (actor_ref <> setter_ref)                         -- R-56
) PARTITION BY RANGE (recorded_at);
CREATE INDEX param_approved_at     ON pf_masterdata.param_approved (tenant_id, param_key, effective_from DESC, seq DESC);
CREATE INDEX param_approved_stream ON pf_masterdata.param_approved (tenant_id, stream_key, seq);
CREATE INDEX param_approved_idem   ON pf_masterdata.param_approved (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_masterdata.param_approved', 'pf_masterdata.param_approved');

-- ---------------------------------------------------------------------------
-- 4. Quyền (R-05). Không vai ứng dụng nào có DELETE/TRUNCATE trong schema này.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT, UPDATE ON pf_masterdata.admin_unit, pf_masterdata.catalog_item, pf_masterdata.param_version TO masterdata_app;
GRANT SELECT, INSERT ON pf_masterdata.catalog_change, pf_masterdata.param_approved TO masterdata_app;
REVOKE DELETE, TRUNCATE ON pf_masterdata.admin_unit, pf_masterdata.catalog_item, pf_masterdata.param_version FROM masterdata_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_masterdata.catalog_change, pf_masterdata.param_approved FROM masterdata_app;
