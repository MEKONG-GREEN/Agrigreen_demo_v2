-- pf_reports KN v7.7 P3: mẫu báo cáo dạng lưới Excel có phiên bản (FN-AD-04.06,
-- FN-XA-03.02; docs/brd/kn-v7.7-plan.md P3). Khác pf_reports.template_version (000013 —
-- bố cục XUẤT báo cáo G1): đây là mẫu NHẬP LIỆU của phòng ban.
--
--   1. form_template          mẫu (mã T01…, phòng ban, cấp nhập liệu) — bảng hiện hành, ngừng dùng có lý do
--   2. form_template_version  phiên bản: nháp → đề nghị → phát hành (BẤT BIẾN) → ngừng dùng.
--                             Nháp là bảng có updated_by/updated_at (gap §4.1: chưa có số liệu dựa
--                             vào); từ "proposed" định nghĩa bị khóa; phát hành rồi không đổi gì
--                             ngoài chuyển sang retired. Người phát hành ≠ người đề nghị (R-56)
--                             kiểm ở app/ VÀ bằng CHECK + trigger dưới đây.
--   3. form_assignment_event  SỔ gán mẫu vào loại báo cáo của phòng ban × cấp (assigned/revoked)
--      form_assignment        bảng chiếu hiện hành (dựng lại được từ sổ, R-22)
--   4. form_template_event    SỔ nhật ký thiết kế (tạo mẫu, nháp, sửa lưới, đề nghị, rút, phát hành,
--                             ngừng dùng, nạp Excel, gán) — nguồn API Log của màn Detail.
--
-- department_code là chuỗi (P2 chưa xong; không FK xuyên schema, R-06). Không PII (R-04).

CREATE TABLE pf_reports.form_template (
  tenant_id        uuid        NOT NULL,
  id               uuid        NOT NULL,                      -- UUIDv7
  code             text        NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9_.-]{1,31}$'),
  name             text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 300),
  department_code  text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,63}$'),
  entry_level      text        NOT NULL CHECK (entry_level IN ('commune', 'province')),
  status           text        NOT NULL CHECK (status IN ('active', 'retired')),
  retired_reason   text        NULL CHECK (retired_reason IS NULL OR char_length(retired_reason) BETWEEN 10 AND 500),
  created_by       uuid        NOT NULL,
  created_at       timestamptz NOT NULL,
  updated_at       timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, department_code, code),                  -- mẫu thuộc đúng một phòng ban
  CHECK ((status = 'retired') = (retired_reason IS NOT NULL))
);
SELECT shared.apply_tenant_rls('pf_reports.form_template');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_reports.form_template
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_reports.form_template
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

CREATE TABLE pf_reports.form_template_version (
  tenant_id          uuid        NOT NULL,
  id                 uuid        NOT NULL,                    -- UUIDv7; P4 ghim id này vào công việc
  template_id        uuid        NOT NULL,
  version            integer     NOT NULL CHECK (version >= 1),
  status             text        NOT NULL CHECK (status IN ('draft', 'proposed', 'published', 'retired')),
  definition         jsonb       NOT NULL,                    -- contracts/schemas/form-template.v1.json
  definition_sha256  text        NOT NULL CHECK (definition_sha256 ~ '^[0-9a-f]{64}$'),
  effective_from     date        NULL,                        -- "áp dụng từ kỳ": ngày bắt đầu kỳ
  effective_label    text        NULL CHECK (effective_label IS NULL OR char_length(effective_label) BETWEEN 1 AND 60),
  origin             text        NOT NULL CHECK (origin IN ('blank', 'clone', 'restore', 'import', 'seed')),
  origin_version_id  uuid        NULL,
  import_id          uuid        NULL,
  created_by         uuid        NOT NULL,
  created_at         timestamptz NOT NULL,
  updated_by         uuid        NOT NULL,
  updated_at         timestamptz NOT NULL,
  proposed_by        uuid        NULL,
  proposed_at        timestamptz NULL,
  published_by       uuid        NULL,
  published_at       timestamptz NULL,
  retired_by         uuid        NULL,
  retired_at         timestamptz NULL,
  retire_reason      text        NULL CHECK (retire_reason IS NULL OR char_length(retire_reason) BETWEEN 10 AND 500),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, template_id, version),
  FOREIGN KEY (tenant_id, template_id) REFERENCES pf_reports.form_template (tenant_id, id),
  CHECK (published_by IS NULL OR published_by <> proposed_by),           -- R-56: không tự phát hành
  CHECK (published_by IS NULL OR published_by <> updated_by),            -- R-56: người sửa cuối không tự phát hành
  CHECK (status <> 'proposed' OR (proposed_by IS NOT NULL AND proposed_at IS NOT NULL AND effective_from IS NOT NULL)),
  CHECK (status NOT IN ('published', 'retired') OR (published_by IS NOT NULL AND published_at IS NOT NULL AND proposed_by IS NOT NULL AND effective_from IS NOT NULL)),
  CHECK ((status = 'retired') = (retired_at IS NOT NULL AND retired_by IS NOT NULL AND retire_reason IS NOT NULL)),
  CHECK ((origin IN ('clone', 'restore')) = (origin_version_id IS NOT NULL)),
  CHECK ((origin = 'import') = (import_id IS NOT NULL))
);
-- Mỗi mẫu tối đa một phiên bản đang soạn (nháp hoặc chờ phát hành).
CREATE UNIQUE INDEX form_version_one_open ON pf_reports.form_template_version (tenant_id, template_id)
  WHERE status IN ('draft', 'proposed');
CREATE INDEX form_version_applicable ON pf_reports.form_template_version (tenant_id, template_id, effective_from DESC, version DESC)
  WHERE status = 'published';
SELECT shared.apply_tenant_rls('pf_reports.form_template_version');

-- Bất biến của phiên bản (lớp 2 sau app/, áp cả owner):
--   draft     → đổi định nghĩa/áp dụng từ kỳ; → proposed
--   proposed  → định nghĩa KHÓA; → draft (rút đề nghị) hoặc → published (người khác)
--   published → chỉ được → retired (kèm người, lúc, lý do); mọi cột khác giữ nguyên
--   retired   → bất biến hoàn toàn
CREATE FUNCTION pf_reports.fn_form_version_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id OR NEW.template_id <> OLD.template_id
     OR NEW.version <> OLD.version OR NEW.origin <> OLD.origin
     OR NEW.origin_version_id IS DISTINCT FROM OLD.origin_version_id OR NEW.import_id IS DISTINCT FROM OLD.import_id
     OR NEW.created_by <> OLD.created_by OR NEW.created_at <> OLD.created_at THEN
    RAISE EXCEPTION 'form_template_version %: định danh và nguồn của phiên bản là bất biến', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF OLD.status = 'retired' THEN
    RAISE EXCEPTION 'form_template_version %: phiên bản đã ngừng dùng là bất biến', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF OLD.status = 'published' THEN
    IF NEW.status <> 'retired' OR NEW.definition <> OLD.definition OR NEW.definition_sha256 <> OLD.definition_sha256
       OR NEW.effective_from IS DISTINCT FROM OLD.effective_from OR NEW.effective_label IS DISTINCT FROM OLD.effective_label
       OR NEW.updated_by <> OLD.updated_by OR NEW.updated_at <> OLD.updated_at
       OR NEW.proposed_by IS DISTINCT FROM OLD.proposed_by OR NEW.proposed_at IS DISTINCT FROM OLD.proposed_at
       OR NEW.published_by IS DISTINCT FROM OLD.published_by OR NEW.published_at IS DISTINCT FROM OLD.published_at THEN
      RAISE EXCEPTION 'form_template_version %: phiên bản đã phát hành là bất biến (chỉ được ngừng dùng)', OLD.id
        USING ERRCODE = 'insufficient_privilege';
    END IF;
    RETURN NEW;
  END IF;
  IF OLD.status = 'proposed' THEN
    IF NEW.definition <> OLD.definition OR NEW.definition_sha256 <> OLD.definition_sha256
       OR NEW.effective_from IS DISTINCT FROM OLD.effective_from OR NEW.updated_by <> OLD.updated_by THEN
      RAISE EXCEPTION 'form_template_version %: phiên bản đã đề nghị phát hành bị khóa nội dung — rút đề nghị để sửa', OLD.id
        USING ERRCODE = 'insufficient_privilege';
    END IF;
    IF NEW.status NOT IN ('proposed', 'draft', 'published') THEN
      RAISE EXCEPTION 'form_template_version %: chuyển trạng thái % → % không hợp lệ', OLD.id, OLD.status, NEW.status
        USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;
  -- draft
  IF NEW.status NOT IN ('draft', 'proposed') THEN
    RAISE EXCEPTION 'form_template_version %: nháp phải được đề nghị trước khi phát hành', OLD.id
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_form_version_guard BEFORE UPDATE ON pf_reports.form_template_version
  FOR EACH ROW EXECUTE FUNCTION pf_reports.fn_form_version_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_reports.form_template_version
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_reports.form_template_version
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 3. Gán mẫu vào loại báo cáo (sổ + bảng chiếu)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_reports.form_assignment_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                     -- 'assignment:<assignment_id>'
  seq               bigint      NOT NULL,
  assignment_id     uuid        NOT NULL,
  action            text        NOT NULL CHECK (action IN ('assigned', 'revoked')),
  template_id       uuid        NOT NULL,
  department_code   text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,63}$'),
  entry_level       text        NOT NULL CHECK (entry_level IN ('commune', 'province')),
  report_type       text        NOT NULL CHECK (report_type IN ('weekly', 'monthly', 'seasonal', 'annual', 'adhoc', 'dispatch')),
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 500),
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
  CHECK (action <> 'revoked' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX form_assignment_event_stream ON pf_reports.form_assignment_event (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_reports.form_assignment_event', 'pf_reports.form_assignment_event');

CREATE TABLE pf_reports.form_assignment (
  tenant_id        uuid        NOT NULL,
  id               uuid        NOT NULL,
  template_id      uuid        NOT NULL,
  department_code  text        NOT NULL,
  entry_level      text        NOT NULL,
  report_type      text        NOT NULL,
  status           text        NOT NULL CHECK (status IN ('active', 'revoked')),
  assigned_by      uuid        NOT NULL,
  assigned_at      timestamptz NOT NULL,
  revoked_by       uuid        NULL,
  revoked_at       timestamptz NULL,
  revoke_reason    text        NULL,
  last_event_id    uuid        NOT NULL,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, template_id) REFERENCES pf_reports.form_template (tenant_id, id),
  CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);
CREATE UNIQUE INDEX form_assignment_active ON pf_reports.form_assignment (tenant_id, template_id, report_type) WHERE status = 'active';
CREATE INDEX form_assignment_lookup ON pf_reports.form_assignment (tenant_id, department_code, entry_level, report_type) WHERE status = 'active';
SELECT shared.apply_tenant_rls('pf_reports.form_assignment');

-- ---------------------------------------------------------------------------
-- 4. Nhật ký thiết kế mẫu (sổ)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_reports.form_template_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                     -- 'form:<template_id>'
  seq               bigint      NOT NULL,
  template_id       uuid        NOT NULL,
  version_id        uuid        NULL,
  version           integer     NULL,
  action            text        NOT NULL CHECK (action IN (
                      'template_created', 'template_updated', 'template_retired',
                      'draft_created', 'draft_saved', 'draft_ops_applied', 'imported', 'seeded',
                      'proposed', 'proposal_withdrawn', 'published', 'version_retired',
                      'assigned', 'assignment_revoked')),
  changes           jsonb       NOT NULL DEFAULT '[]'::jsonb, -- [{field, old, new}] hoặc lô thao tác lưới
  reason            text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 500),
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
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX form_template_event_stream ON pf_reports.form_template_event (tenant_id, template_id, id);
SELECT shared.apply_ledger('pf_reports.form_template_event', 'pf_reports.form_template_event');

-- ---------------------------------------------------------------------------
-- Quyền của reports_app (R-05). Sổ: SELECT, INSERT. Bảng hiện hành: thêm UPDATE. Không DELETE.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_reports.form_assignment_event, pf_reports.form_template_event TO reports_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_reports.form_assignment_event, pf_reports.form_template_event FROM reports_app;
GRANT SELECT, INSERT, UPDATE ON pf_reports.form_template, pf_reports.form_template_version, pf_reports.form_assignment TO reports_app;
REVOKE DELETE, TRUNCATE ON pf_reports.form_template, pf_reports.form_template_version, pf_reports.form_assignment FROM reports_app;
