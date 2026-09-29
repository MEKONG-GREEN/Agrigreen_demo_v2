-- pf_cropcycle (1/2): danh mục quy trình SOP có phiên bản và gắn giống lúa ↔ SOP
-- mặc định (App HTX FN-03: HTX US-CAT-01, US-CAT-03; HTX US-SEASON-01 AC-1).
--
-- Mẫu:
--   - sop_template: bảng nghiệp vụ có phiên bản — mã, tên DUY NHẤT (HTX US-CAT-03
--     AC-3), cây trồng, phạm vi hệ thống | htx (+ cooperative_ref). Trạng thái
--     draft → published → retired. KHÔNG bao giờ xóa (RB-1): ngừng dùng = retired
--     kèm lý do; vụ đã ghim phiên bản vẫn đọc được.
--   - sop_version: nội dung bước của một phiên bản. Nháp sửa được; ĐÃ PHÁT HÀNH là
--     bất biến (trigger). Bước có seq liên tục 1..n (HTX US-CAT-01 AC-6) — kiểm ở Go.
--     Nhân bản (HTX US-CAT-03) = mẫu mới + phiên bản 1 trỏ source_version_ref về
--     phiên bản nguồn; sửa bản sao không đổi nguồn (dòng khác).
--     Phát hành: sop_admin soạn, param_approver phát hành, người phát hành ≠ người
--     soạn (R-56) — kiểm ở Go (authz.EnsureDistinctApprover) và bằng CHECK.
--   - variety_sop_binding: giống lúa → mẫu SOP mặc định để tự gán khi mở vụ. Đổi
--     gắn = đóng dòng cũ (superseded_at) + dòng mới; không xóa.
-- Không có dữ liệu cá nhân ở đây (R-04).

CREATE SCHEMA pf_cropcycle;
REVOKE ALL ON SCHEMA pf_cropcycle FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_cropcycle TO cropcycle_app;

-- Con trỏ chiều xuống của đồng bộ cho entity sop_version (docs/dev/sync-contract.md).
CREATE SEQUENCE pf_cropcycle.sop_sync_seq;

-- ---------------------------------------------------------------------------
-- 1. Mẫu quy trình
-- ---------------------------------------------------------------------------
CREATE TABLE pf_cropcycle.sop_template (
  tenant_id           uuid        NOT NULL,
  ref                 uuid        NOT NULL,                                 -- UUIDv7
  code                text        NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9_.-]{1,39}$'),
  name                text        NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
  name_key            text        NOT NULL CHECK (char_length(name_key) BETWEEN 1 AND 200), -- tên chuẩn hóa (chữ thường, gộp khoảng trắng) để chặn trùng
  crop                text        NOT NULL DEFAULT 'lua' CHECK (crop ~ '^[a-z][a-z0-9_]{1,39}$'),
  scope               text        NOT NULL CHECK (scope IN ('system', 'htx')),
  cooperative_ref     uuid        NULL,
  standard            text        NULL CHECK (standard IS NULL OR standard ~ '^[a-z][a-z0-9_]{1,39}$'),
  description         text        NULL CHECK (description IS NULL OR char_length(description) <= 2000),
  status              text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'retired')),
  status_reason       text        NULL,
  source_template_ref uuid        NULL,                                     -- mẫu nguồn khi nhân bản
  latest_version      integer     NOT NULL DEFAULT 1 CHECK (latest_version >= 1),
  published_version   integer     NULL CHECK (published_version IS NULL OR published_version >= 1),
  created_by          uuid        NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, ref),
  UNIQUE (tenant_id, code),
  UNIQUE (tenant_id, name_key),
  CHECK ((scope = 'htx') = (cooperative_ref IS NOT NULL)),
  CHECK (status <> 'retired' OR char_length(btrim(status_reason)) >= 10),
  CHECK (status <> 'published' OR published_version IS NOT NULL)
);
CREATE INDEX sop_template_coop ON pf_cropcycle.sop_template (tenant_id, cooperative_ref);
SELECT shared.apply_tenant_rls('pf_cropcycle.sop_template');

CREATE FUNCTION pf_cropcycle.fn_sop_template_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status = 'retired' THEN
    RAISE EXCEPTION 'sop_template %: mẫu đã ngừng dùng là bất biến', OLD.ref
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.tenant_id <> OLD.tenant_id OR NEW.ref <> OLD.ref OR NEW.code <> OLD.code OR NEW.scope <> OLD.scope
     OR NEW.cooperative_ref IS DISTINCT FROM OLD.cooperative_ref OR NEW.crop <> OLD.crop
     OR NEW.source_template_ref IS DISTINCT FROM OLD.source_template_ref
     OR NEW.created_by <> OLD.created_by OR NEW.created_at <> OLD.created_at
     OR NEW.latest_version < OLD.latest_version
     OR (OLD.published_version IS NOT NULL AND (NEW.published_version IS NULL OR NEW.published_version < OLD.published_version)) THEN
    RAISE EXCEPTION 'sop_template %: mã, phạm vi, nguồn và lịch sử phiên bản không đổi được', OLD.ref
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_sop_template_guard BEFORE UPDATE ON pf_cropcycle.sop_template
  FOR EACH ROW EXECUTE FUNCTION pf_cropcycle.fn_sop_template_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_cropcycle.sop_template
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_cropcycle.sop_template
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 2. Phiên bản quy trình (nội dung bước)
-- ---------------------------------------------------------------------------
-- steps: mảng JSON [{seq, name, stage_code, activity_code, day_offset, window_days,
-- mandatory, evidence_kinds[], phi_days?, instruction?}] — Go kiểm cấu trúc và seq liên tục.
CREATE TABLE pf_cropcycle.sop_version (
  tenant_id           uuid        NOT NULL,
  id                  uuid        NOT NULL,                                 -- UUIDv7
  template_ref        uuid        NOT NULL,
  version_no          integer     NOT NULL CHECK (version_no >= 1),
  status              text        NOT NULL CHECK (status IN ('draft', 'published')),
  steps               jsonb       NOT NULL CHECK (jsonb_typeof(steps) = 'array'),
  step_count          integer     NOT NULL CHECK (step_count >= 0 AND step_count <= 100),
  harvest_day_offset  integer     NULL,                                     -- bước thu hoạch đầu tiên (cổng khai sản lượng)
  harvest_window_days integer     NULL CHECK (harvest_window_days IS NULL OR harvest_window_days >= 0),
  source_version_ref  uuid        NULL,                                     -- phiên bản nguồn khi nhân bản / tạo phiên bản mới
  change_note         text        NULL CHECK (change_note IS NULL OR char_length(change_note) <= 1000),
  authored_by         uuid        NOT NULL,
  authored_at         timestamptz NOT NULL,
  updated_at          timestamptz NOT NULL DEFAULT now(),
  published_by        uuid        NULL,
  published_at        timestamptz NULL,
  sync_seq            bigint      NOT NULL DEFAULT nextval('pf_cropcycle.sop_sync_seq'),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, template_ref, version_no),
  FOREIGN KEY (tenant_id, template_ref) REFERENCES pf_cropcycle.sop_template (tenant_id, ref),
  CHECK ((status = 'published') = (published_by IS NOT NULL AND published_at IS NOT NULL)),
  CHECK (status <> 'published' OR (step_count >= 1 AND harvest_day_offset IS NOT NULL AND harvest_window_days IS NOT NULL)),
  -- R-56: người phát hành không phải người soạn (kiểm cả ở Go).
  CHECK (published_by IS NULL OR published_by <> authored_by)
);
-- Mỗi mẫu tối đa một bản nháp đang soạn.
CREATE UNIQUE INDEX sop_version_one_draft ON pf_cropcycle.sop_version (tenant_id, template_ref) WHERE status = 'draft';
CREATE INDEX sop_version_sync ON pf_cropcycle.sop_version (tenant_id, sync_seq);
SELECT shared.apply_tenant_rls('pf_cropcycle.sop_version');

CREATE FUNCTION pf_cropcycle.fn_sop_version_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status = 'published' THEN
    RAISE EXCEPTION 'sop_version %: phiên bản đã phát hành là bất biến — tạo phiên bản mới', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.tenant_id <> OLD.tenant_id OR NEW.id <> OLD.id OR NEW.template_ref <> OLD.template_ref
     OR NEW.version_no <> OLD.version_no OR NEW.source_version_ref IS DISTINCT FROM OLD.source_version_ref
     OR NEW.authored_by <> OLD.authored_by OR NEW.authored_at <> OLD.authored_at THEN
    RAISE EXCEPTION 'sop_version %: định danh, nguồn và người soạn không đổi được', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_sop_version_guard BEFORE UPDATE ON pf_cropcycle.sop_version
  FOR EACH ROW EXECUTE FUNCTION pf_cropcycle.fn_sop_version_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_cropcycle.sop_version
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_cropcycle.sop_version
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 3. Gắn giống lúa ↔ mẫu SOP mặc định (HTX US-SEASON-01 AC-1)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_cropcycle.variety_sop_binding (
  tenant_id     uuid        NOT NULL,
  id            uuid        NOT NULL,
  variety_code  text        NOT NULL CHECK (variety_code ~ '^[A-Za-z0-9_.-]{1,40}$'),
  template_ref  uuid        NOT NULL,
  note          text        NULL CHECK (note IS NULL OR char_length(note) <= 500),
  bound_by      uuid        NOT NULL,
  bound_at      timestamptz NOT NULL DEFAULT now(),
  superseded_at timestamptz NULL,
  superseded_by uuid        NULL,
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, template_ref) REFERENCES pf_cropcycle.sop_template (tenant_id, ref),
  CHECK ((superseded_at IS NULL) = (superseded_by IS NULL))
);
CREATE UNIQUE INDEX variety_sop_binding_current ON pf_cropcycle.variety_sop_binding (tenant_id, variety_code) WHERE superseded_at IS NULL;
SELECT shared.apply_tenant_rls('pf_cropcycle.variety_sop_binding');

CREATE FUNCTION pf_cropcycle.fn_variety_binding_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.superseded_at IS NOT NULL THEN
    RAISE EXCEPTION 'variety_sop_binding %: dòng đã bị thay là bất biến', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.tenant_id <> OLD.tenant_id OR NEW.id <> OLD.id OR NEW.variety_code <> OLD.variety_code
     OR NEW.template_ref <> OLD.template_ref OR NEW.note IS DISTINCT FROM OLD.note
     OR NEW.bound_by <> OLD.bound_by OR NEW.bound_at <> OLD.bound_at THEN
    RAISE EXCEPTION 'variety_sop_binding %: chỉ được đóng dòng, không sửa nội dung', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_variety_binding_guard BEFORE UPDATE ON pf_cropcycle.variety_sop_binding
  FOR EACH ROW EXECUTE FUNCTION pf_cropcycle.fn_variety_binding_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_cropcycle.variety_sop_binding
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_cropcycle.variety_sop_binding
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 4. Quyền của cropcycle_app (R-05). Không DELETE ở đâu cả.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT, UPDATE ON pf_cropcycle.sop_template, pf_cropcycle.sop_version, pf_cropcycle.variety_sop_binding TO cropcycle_app;
REVOKE DELETE, TRUNCATE ON pf_cropcycle.sop_template, pf_cropcycle.sop_version, pf_cropcycle.variety_sop_binding FROM cropcycle_app;
GRANT USAGE ON SEQUENCE pf_cropcycle.sop_sync_seq TO cropcycle_app;
