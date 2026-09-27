-- pf_mrv: nền MRV của G1 (mục 16, Phụ lục A G1: "danh mục trường MRV đều có dù
-- chỉ một tenant"). Danh mục thương mại các trường bằng chứng CHƯA chốt (câu hỏi
-- mở C-1) — vì vậy cấu trúc được thiết kế để thêm/đổi trường chỉ bằng một PHIÊN
-- BẢN DANH MỤC mới có phê duyệt, không cần migration:
--
--   1. catalog_version  quy trình duyệt phiên bản danh mục (R-56: người duyệt ≠ người đặt)
--   2. field_def        sổ định nghĩa trường của từng phiên bản (bất biến)
--   3. evidence         sổ minh chứng; mỗi bản ghi GHIM phiên bản danh mục đã duyệt
--   4. evidence_current bảng chiếu bản hiện hành của mỗi chuỗi sửa sai (R-21)
--   5. evidence_review  sổ quyết định duyệt của TTKN tỉnh
--   6. export_job       job xuất gói (mẫu bất đồng bộ mục 12)
--   7. export_package   sổ gói đã xuất + băm ảnh chụp dữ liệu (bất biến)

CREATE SCHEMA pf_mrv;
REVOKE ALL ON SCHEMA pf_mrv FROM PUBLIC;
GRANT USAGE ON SCHEMA pf_mrv TO mrv_app;

-- ---------------------------------------------------------------------------
-- 1. Phiên bản danh mục trường — quy trình duyệt
-- ---------------------------------------------------------------------------
-- Được UPDATE các cột trạng thái (draft → submitted → approved/rejected); nội
-- dung đề xuất bất biến, phiên bản đã quyết định bất biến (trigger bên dưới).
CREATE TABLE pf_mrv.catalog_version (
  id            uuid        PRIMARY KEY,                -- UUIDv7
  tenant_id     uuid        NOT NULL,
  evidence_type text        NOT NULL CHECK (evidence_type ~ '^[a-z][a-z0-9_]{2,39}$'),
  version       integer     NOT NULL CHECK (version > 0),
  status        text        NOT NULL CHECK (status IN ('draft', 'submitted', 'approved', 'rejected')),
  -- provisional = true: danh mục tạm, chờ chốt C-1. Dữ liệu vẫn thu được, gói xuất ghi rõ cờ này.
  provisional   boolean     NOT NULL,
  source_note   text        NOT NULL CHECK (length(source_note) BETWEEN 1 AND 500),
  field_count   integer     NOT NULL CHECK (field_count BETWEEN 1 AND 100),
  setter_ref    uuid        NOT NULL,
  approver_ref  uuid        NULL,
  submitted_at  timestamptz NULL,
  decided_at    timestamptz NULL,
  decision_note text        NULL CHECK (decision_note IS NULL OR length(decision_note) <= 500),
  idem_key      text        NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, evidence_type, version),
  UNIQUE (tenant_id, idem_key),
  -- R-56 lớp DB: người phê duyệt/từ chối không bao giờ là người đặt.
  CHECK (approver_ref IS NULL OR approver_ref <> setter_ref),
  CHECK ((status IN ('approved', 'rejected')) = (approver_ref IS NOT NULL AND decided_at IS NOT NULL))
);
SELECT shared.apply_tenant_rls('pf_mrv.catalog_version');

CREATE FUNCTION pf_mrv.fn_catalog_version_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status IN ('approved', 'rejected') THEN
    RAISE EXCEPTION 'catalog_version %: phiên bản đã % là bất biến', OLD.id, OLD.status
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id OR NEW.evidence_type <> OLD.evidence_type
     OR NEW.version <> OLD.version OR NEW.provisional <> OLD.provisional OR NEW.source_note <> OLD.source_note
     OR NEW.field_count <> OLD.field_count OR NEW.setter_ref <> OLD.setter_ref OR NEW.idem_key <> OLD.idem_key THEN
    RAISE EXCEPTION 'catalog_version %: nội dung đề xuất bất biến, chỉ đổi được trạng thái', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NOT ((OLD.status = 'draft' AND NEW.status = 'submitted')
       OR (OLD.status = 'submitted' AND NEW.status IN ('approved', 'rejected'))) THEN
    RAISE EXCEPTION 'catalog_version %: chuyển trạng thái % → % không hợp lệ', OLD.id, OLD.status, NEW.status
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_catalog_version_guard BEFORE UPDATE ON pf_mrv.catalog_version
  FOR EACH ROW EXECUTE FUNCTION pf_mrv.fn_catalog_version_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_mrv.catalog_version
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_mrv.catalog_version
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 2. Sổ định nghĩa trường — mỗi dòng một trường của một phiên bản danh mục
-- ---------------------------------------------------------------------------
-- data_type là tập đóng; mọi thứ khác (khóa, nhãn, đơn vị, bắt buộc, min/max,
-- giá trị enum) là DỮ LIỆU, nên danh mục C-1 chốt sau không cần migration.
CREATE TABLE pf_mrv.field_def (
  id                 uuid        NOT NULL,
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,              -- 'catalog:<evidence_type>'
  seq                bigint      NOT NULL,
  catalog_version_id uuid        NOT NULL,
  evidence_type      text        NOT NULL,
  catalog_version    integer     NOT NULL,
  position           integer     NOT NULL CHECK (position BETWEEN 1 AND 100),
  field_key          text        NOT NULL CHECK (field_key ~ '^[a-z][a-z0-9_]{1,62}$'),
  label_vi           text        NOT NULL CHECK (length(label_vi) BETWEEN 1 AND 200),
  data_type          text        NOT NULL CHECK (data_type IN ('decimal', 'integer', 'boolean', 'enum', 'date', 'photo', 'geo_point', 'text')),
  unit               text        NULL CHECK (unit IS NULL OR length(unit) BETWEEN 1 AND 20),
  required           boolean     NOT NULL,
  min_value          numeric     NULL,                  -- decimal/integer: giá trị; photo: số ảnh; text: độ dài
  max_value          numeric     NULL,
  max_scale          integer     NULL CHECK (max_scale IS NULL OR max_scale BETWEEN 0 AND 8),
  enum_values        jsonb       NULL CHECK (enum_values IS NULL OR jsonb_typeof(enum_values) = 'array'),
  help_vi            text        NULL CHECK (help_vi IS NULL OR length(help_vi) <= 500),
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
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
  CHECK (min_value IS NULL OR max_value IS NULL OR min_value <= max_value)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX field_def_version ON pf_mrv.field_def (tenant_id, evidence_type, catalog_version, position);
CREATE INDEX field_def_stream  ON pf_mrv.field_def (tenant_id, stream_key, seq);
CREATE INDEX field_def_idem    ON pf_mrv.field_def (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_mrv.field_def', 'pf_mrv.field_def');

-- ---------------------------------------------------------------------------
-- 3. Sổ minh chứng
-- ---------------------------------------------------------------------------
-- values: giá trị theo khóa trường của phiên bản danh mục đã ghim; số thập phân
-- là CHUỖI (R-10). file_refs: [{file_ref, sha256}] — ảnh đã mã hóa phong bì do
-- module tệp lưu (ADR-0004); ở đây chỉ tham chiếu. gps là dữ liệu cá nhân khi gắn
-- với hộ (envelope-format.md) — không bao giờ ghi log.
CREATE TABLE pf_mrv.evidence (
  id                uuid        NOT NULL,               -- UUIDv7, có thể sinh trên máy (R-11)
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,               -- 'unit:<admin_unit_code>'
  seq               bigint      NOT NULL,
  root_id           uuid        NOT NULL,               -- bản gốc của chuỗi sửa sai (= id nếu là bản gốc)
  chain_version     integer     NOT NULL CHECK (chain_version > 0),
  evidence_type     text        NOT NULL CHECK (evidence_type ~ '^[a-z][a-z0-9_]{2,39}$'),
  catalog_version   integer     NOT NULL CHECK (catalog_version > 0),
  admin_unit_code   text        NOT NULL CHECK (admin_unit_code ~ '^[0-9A-Z][0-9A-Z.-]{0,39}$'),
  plot_ref          uuid        NULL,
  household_ref     uuid        NULL,                   -- mã tham chiếu hộ (R-41), không phải định danh
  season_code       text        NULL CHECK (season_code IS NULL OR length(season_code) BETWEEN 1 AND 40),
  field_values      jsonb       NOT NULL CHECK (jsonb_typeof(field_values) = 'object'),
  file_refs         jsonb       NOT NULL CHECK (jsonb_typeof(file_refs) = 'array'),
  gps               geometry(Point, 4326) NULL,
  gps_accuracy_m    numeric(8,2) NULL CHECK (gps_accuracy_m IS NULL OR gps_accuracy_m >= 0),
  event_time        timestamptz NOT NULL,               -- R-13: lúc thu tại hiện trường
  recorded_at       timestamptz NOT NULL DEFAULT now(), -- R-13, khóa phân vùng
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,               -- người thu (captured_by)
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK ((supersedes_id IS NULL) = (chain_version = 1 AND root_id = id))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX evidence_id     ON pf_mrv.evidence (id);
CREATE INDEX evidence_root   ON pf_mrv.evidence (tenant_id, root_id, chain_version);
CREATE INDEX evidence_idem   ON pf_mrv.evidence (tenant_id, idem_key);
CREATE INDEX evidence_stream ON pf_mrv.evidence (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_mrv.evidence', 'pf_mrv.evidence');

-- ---------------------------------------------------------------------------
-- 4. Bảng chiếu bản hiện hành của mỗi chuỗi (R-21, dựng lại được từ sổ — R-22)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_mrv.evidence_current (
  tenant_id       uuid        NOT NULL,
  root_id         uuid        NOT NULL,
  head_id         uuid        NOT NULL,
  chain_version   integer     NOT NULL,
  evidence_type   text        NOT NULL,
  catalog_version integer     NOT NULL,
  admin_unit_code text        NOT NULL,
  season_code     text        NULL,
  event_time      timestamptz NOT NULL,
  captured_by     uuid        NOT NULL,
  review_status   text        NOT NULL CHECK (review_status IN ('pending', 'accepted', 'rejected')),
  review_id       uuid        NULL,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, root_id)
);
CREATE INDEX evidence_current_unit ON pf_mrv.evidence_current (tenant_id, admin_unit_code, root_id);
SELECT shared.apply_tenant_rls('pf_mrv.evidence_current');

-- ---------------------------------------------------------------------------
-- 5. Sổ quyết định duyệt (KN US-MRV-05, G1 tối thiểu)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_mrv.evidence_review (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,               -- 'unit:<admin_unit_code>'
  seq               bigint      NOT NULL,
  evidence_id       uuid        NOT NULL,               -- đúng phiên bản được duyệt
  root_id           uuid        NOT NULL,
  decision          text        NOT NULL CHECK (decision IN ('accepted', 'rejected')),
  reason            text        NOT NULL CHECK (length(reason) BETWEEN 5 AND 1000),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,               -- người duyệt ≠ người thu (kiểm ở app/)
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX evidence_review_evidence ON pf_mrv.evidence_review (tenant_id, evidence_id);
CREATE INDEX evidence_review_idem     ON pf_mrv.evidence_review (tenant_id, idem_key);
CREATE INDEX evidence_review_stream   ON pf_mrv.evidence_review (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_mrv.evidence_review', 'pf_mrv.evidence_review');

-- ---------------------------------------------------------------------------
-- 6. Job xuất gói (mục 12). Bảng vận hành: được UPDATE cột trạng thái.
-- ---------------------------------------------------------------------------
-- filters là ảnh chụp bộ lọc ĐÃ GIẢI phạm vi (danh sách xã cụ thể) lúc yêu cầu:
-- worker không cần biết phạm vi của người gọi, và dựng lại được đúng gói.
CREATE TABLE pf_mrv.export_job (
  id              uuid        PRIMARY KEY,              -- UUIDv7 = export_id
  tenant_id       uuid        NOT NULL,
  status          text        NOT NULL CHECK (status IN ('queued', 'completed', 'failed')),
  filters         jsonb       NOT NULL CHECK (jsonb_typeof(filters) = 'object'),
  requested_by    uuid        NOT NULL,
  requested_at    timestamptz NOT NULL DEFAULT now(),   -- mốc ảnh chụp dữ liệu (as_of)
  idem_key        text        NOT NULL,
  object_key      text        NULL,
  package_sha256  text        NULL CHECK (package_sha256 IS NULL OR package_sha256 ~ '^[0-9a-f]{64}$'),
  snapshot_sha256 text        NULL CHECK (snapshot_sha256 IS NULL OR snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  evidence_count  integer     NULL,
  error_code      text        NULL,
  completed_at    timestamptz NULL,
  attempts        integer     NOT NULL DEFAULT 0,
  UNIQUE (tenant_id, idem_key),
  CHECK ((status = 'completed') = (package_sha256 IS NOT NULL AND completed_at IS NOT NULL))
);
SELECT shared.apply_tenant_rls('pf_mrv.export_job');

CREATE FUNCTION pf_mrv.fn_export_job_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status IN ('completed', 'failed') THEN
    RAISE EXCEPTION 'export_job %: job đã % là bất biến', OLD.id, OLD.status
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id OR NEW.filters <> OLD.filters
     OR NEW.requested_by <> OLD.requested_by OR NEW.requested_at <> OLD.requested_at OR NEW.idem_key <> OLD.idem_key THEN
    RAISE EXCEPTION 'export_job %: yêu cầu xuất bất biến, chỉ đổi được trạng thái', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_export_job_guard BEFORE UPDATE ON pf_mrv.export_job
  FOR EACH ROW EXECUTE FUNCTION pf_mrv.fn_export_job_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_mrv.export_job
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_mrv.export_job
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 7. Sổ gói đã xuất (mục 16: lưu lại package đã xuất kèm băm ảnh chụp dữ liệu)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_mrv.export_package (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,               -- 'exports'
  seq               bigint      NOT NULL,
  export_id         uuid        NOT NULL,
  object_key        text        NOT NULL,               -- packages/<tenant>/<export_id>.json
  package_sha256    text        NOT NULL CHECK (package_sha256 ~ '^[0-9a-f]{64}$'),
  snapshot_sha256   text        NOT NULL CHECK (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  size_bytes        bigint      NOT NULL CHECK (size_bytes > 0),
  evidence_count    integer     NOT NULL CHECK (evidence_count >= 0),
  as_of             timestamptz NOT NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,               -- người yêu cầu xuất
  device_id         text        NULL,
  idem_key          text        NOT NULL,               -- 'export:<export_id>' — một gói cho mỗi yêu cầu
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX export_package_export ON pf_mrv.export_package (tenant_id, export_id);
CREATE INDEX export_package_stream ON pf_mrv.export_package (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_mrv.export_package', 'pf_mrv.export_package');

-- ---------------------------------------------------------------------------
-- Quyền (R-05). Sổ: chỉ SELECT, INSERT.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_mrv.field_def, pf_mrv.evidence, pf_mrv.evidence_review, pf_mrv.export_package TO mrv_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_mrv.field_def, pf_mrv.evidence, pf_mrv.evidence_review, pf_mrv.export_package FROM mrv_app;
GRANT SELECT, INSERT, UPDATE ON pf_mrv.catalog_version, pf_mrv.export_job TO mrv_app;
REVOKE DELETE, TRUNCATE ON pf_mrv.catalog_version, pf_mrv.export_job FROM mrv_app;
-- DELETE trên bảng chiếu chỉ để dựng lại từ sổ (R-22); bảng chiếu không phải sổ.
GRANT SELECT, INSERT, UPDATE, DELETE ON pf_mrv.evidence_current TO mrv_app;
