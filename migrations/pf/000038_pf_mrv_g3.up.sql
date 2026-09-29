-- pf_mrv G3 (g3-conventions §2: pf 000038, vai mrv_app). Liên kết bằng chứng từ
-- nguồn ngoài module và ước tính CO2 TẠM (chờ khách ký phương pháp, C-1):
--
--   1. season_link          sổ tham chiếu vụ (từ pf.cropcycle.season.*): season_ref → thửa, xã, mã vụ
--   2. farm_log_link        sổ liên kết nhật ký HTX làm tham chiếu bằng chứng (pf.cropcycle.farm_log.*),
--                           ghim phiên bản danh mục htx_farm_log đã duyệt lúc liên kết
--   3. collection_evidence  sổ bằng chứng thu gom rơm qua van mrv_collection_evidence (khối lượng THEO BẬC)
--   4. log_comparison       sổ kết quả đối chiếu nhật ký HTX ↔ minh chứng cán bộ KN (KN US-MRV-02)
--   5. method_version       phương pháp ước tính có phê duyệt (R-56) — ghim phiên bản tham số
--   6. estimate             sổ ước tính CO2 theo thửa/vụ, có rule_version + phiên bản tham số
--
-- Mọi bảng mới là SỔ (chỉ ghi thêm) trừ method_version (quy trình duyệt như
-- catalog_version). Không có bảng chiếu: bản hiện hành đọc bằng DISTINCT ON trên
-- sổ, nên gói xuất dựng lại được tại as_of (mục 16).

-- ---------------------------------------------------------------------------
-- 1. Tham chiếu vụ
-- ---------------------------------------------------------------------------
CREATE TABLE pf_mrv.season_link (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,               -- 'season:<season_ref>'
  seq               bigint      NOT NULL,
  season_ref        uuid        NOT NULL,
  plot_ref          uuid        NOT NULL,
  commune_code      text        NOT NULL CHECK (commune_code ~ '^[0-9A-Z][0-9A-Z.-]{0,39}$'),
  season_code       text        NOT NULL CHECK (length(season_code) BETWEEN 1 AND 40),
  season_year       integer     NOT NULL,
  status            text        NOT NULL CHECK (status IN ('open', 'completed', 'cancelled')),
  sowing_date       date        NULL,
  source_event      text        NOT NULL,
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,               -- 'evt:<event_id>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX season_link_ref    ON pf_mrv.season_link (tenant_id, season_ref, event_time);
CREATE INDEX season_link_plot   ON pf_mrv.season_link (tenant_id, plot_ref, season_code);
CREATE INDEX season_link_idem   ON pf_mrv.season_link (tenant_id, idem_key);
CREATE INDEX season_link_stream ON pf_mrv.season_link (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_mrv.season_link', 'pf_mrv.season_link');

-- ---------------------------------------------------------------------------
-- 2. Liên kết nhật ký HTX (bằng chứng tham chiếu — nội dung nhật ký ở pf/cropcycle)
-- ---------------------------------------------------------------------------
-- kind = recorded: một phiên bản nhật ký (bản gốc hoặc bản ghi bù); kind = reviewed:
-- quyết định duyệt của HTX cho log_id. catalog_version: phiên bản danh mục
-- htx_farm_log ĐÃ DUYỆT lúc liên kết (NULL = chưa có danh mục duyệt → không tính).
CREATE TABLE pf_mrv.farm_log_link (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,               -- 'log:<log_ref>'
  seq               bigint      NOT NULL,
  kind              text        NOT NULL CHECK (kind IN ('recorded', 'reviewed')),
  log_id            uuid        NOT NULL,
  log_ref           uuid        NOT NULL,
  log_version       bigint      NULL,
  season_ref        uuid        NOT NULL,
  plot_ref          uuid        NULL,
  commune_code      text        NULL,
  activity_code     text        NULL,
  location_status   text        NULL,
  became_current    boolean     NULL,
  decision          text        NULL,
  author_ref        uuid        NOT NULL,
  catalog_version   integer     NULL CHECK (catalog_version IS NULL OR catalog_version > 0),
  log_event_time    timestamptz NOT NULL,               -- thời điểm của nhật ký / quyết định duyệt (nguồn)
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,               -- 'evt:<event_id>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (kind <> 'recorded' OR (plot_ref IS NOT NULL AND commune_code IS NOT NULL AND activity_code IS NOT NULL AND log_version IS NOT NULL)),
  CHECK (kind <> 'reviewed' OR decision IS NOT NULL)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX farm_log_link_season ON pf_mrv.farm_log_link (tenant_id, season_ref, log_ref);
CREATE INDEX farm_log_link_log    ON pf_mrv.farm_log_link (tenant_id, log_id);
CREATE INDEX farm_log_link_idem   ON pf_mrv.farm_log_link (tenant_id, idem_key);
CREATE INDEX farm_log_link_stream ON pf_mrv.farm_log_link (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_mrv.farm_log_link', 'pf_mrv.farm_log_link');

-- ---------------------------------------------------------------------------
-- 3. Bằng chứng thu gom qua van (mrv_collection_evidence v1, dự thảo C-1)
-- ---------------------------------------------------------------------------
-- Chỉ khối lượng THEO BẬC — không bao giờ có khối lượng cân chính xác (mkg.md §3).
-- idem_key 'rev:<evidence_ref>:<revision>': phát lại cùng revision (event_id khác) là trùng.
CREATE TABLE pf_mrv.collection_evidence (
  id                  uuid        NOT NULL,
  tenant_id           uuid        NOT NULL,
  stream_key          text        NOT NULL,             -- 'collection:<evidence_ref>'
  seq                 bigint      NOT NULL,
  evidence_ref        uuid        NOT NULL,
  revision            bigint      NOT NULL,
  plot_ref            uuid        NOT NULL,
  season_ref          uuid        NULL,
  collected_on        date        NOT NULL,
  collected_kg_bucket text        NOT NULL CHECK (collected_kg_bucket IN ('0-500', '500-1000', '1000-2000', '2000-5000', '5000-10000', '10000+')),
  bucket_rule_version text        NOT NULL CHECK (length(bucket_rule_version) BETWEEN 1 AND 60),
  source_event_id     uuid        NOT NULL,
  event_time          timestamptz NOT NULL,
  recorded_at         timestamptz NOT NULL DEFAULT now(),
  supersedes_id       uuid        NULL,
  correction_reason   text        NULL,
  actor_ref           uuid        NOT NULL,
  device_id           text        NULL,
  idem_key            text        NOT NULL,
  rule_version        text        NOT NULL,
  prev_hash           bytea       NOT NULL,
  row_hash            bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX collection_evidence_ref    ON pf_mrv.collection_evidence (tenant_id, evidence_ref, revision);
CREATE INDEX collection_evidence_season ON pf_mrv.collection_evidence (tenant_id, season_ref);
CREATE INDEX collection_evidence_plot   ON pf_mrv.collection_evidence (tenant_id, plot_ref);
CREATE INDEX collection_evidence_idem   ON pf_mrv.collection_evidence (tenant_id, idem_key);
CREATE INDEX collection_evidence_stream ON pf_mrv.collection_evidence (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_mrv.collection_evidence', 'pf_mrv.collection_evidence');

-- ---------------------------------------------------------------------------
-- 4. Đối chiếu nhật ký HTX ↔ minh chứng cán bộ KN (KN US-MRV-02)
-- ---------------------------------------------------------------------------
-- Gắn với bản ghi MRV tương ứng (evidence_id = đúng phiên bản minh chứng của cán bộ).
-- Sửa kết quả = bản ghi bù (supersedes_id + correction_reason).
CREATE TABLE pf_mrv.log_comparison (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,               -- 'unit:<admin_unit_code>'
  seq               bigint      NOT NULL,
  evidence_id       uuid        NOT NULL,
  evidence_root_id  uuid        NOT NULL,
  log_ref           uuid        NOT NULL,
  log_id            uuid        NOT NULL,
  season_ref        uuid        NOT NULL,
  plot_ref          uuid        NOT NULL,
  admin_unit_code   text        NOT NULL,
  result            text        NOT NULL CHECK (result IN ('match', 'mismatch')),
  note              text        NULL CHECK (note IS NULL OR length(note) <= 1000),
  file_refs         jsonb       NOT NULL CHECK (jsonb_typeof(file_refs) = 'array'),
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
  CHECK (result = 'match' OR (note IS NOT NULL AND length(note) >= 5))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX log_comparison_season ON pf_mrv.log_comparison (tenant_id, season_ref, log_ref);
CREATE INDEX log_comparison_id     ON pf_mrv.log_comparison (id);
CREATE INDEX log_comparison_idem   ON pf_mrv.log_comparison (tenant_id, idem_key);
CREATE INDEX log_comparison_stream ON pf_mrv.log_comparison (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_mrv.log_comparison', 'pf_mrv.log_comparison');

-- ---------------------------------------------------------------------------
-- 5. Phương pháp ước tính — quy trình duyệt (R-56), chờ khách ký
-- ---------------------------------------------------------------------------
-- params: [{key, version}] — phiên bản tham số masterdata mà phương pháp ghim.
-- Ước tính chỉ "chính thức" khi có phương pháp ĐÃ DUYỆT cùng rule_version và mọi
-- tham số đã dùng là bản đã duyệt đúng phiên bản được ghim.
CREATE TABLE pf_mrv.method_version (
  id            uuid        PRIMARY KEY,
  tenant_id     uuid        NOT NULL,
  rule_version  text        NOT NULL CHECK (rule_version ~ '^[a-z][a-z0-9_.]{2,59}$'),
  version       integer     NOT NULL CHECK (version > 0),
  status        text        NOT NULL CHECK (status IN ('submitted', 'approved', 'rejected')),
  params        jsonb       NOT NULL CHECK (jsonb_typeof(params) = 'array'),
  source_note   text        NOT NULL CHECK (length(source_note) BETWEEN 5 AND 1000),
  setter_ref    uuid        NOT NULL,
  approver_ref  uuid        NULL,
  decided_at    timestamptz NULL,
  decision_note text        NULL CHECK (decision_note IS NULL OR length(decision_note) <= 500),
  idem_key      text        NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, rule_version, version),
  UNIQUE (tenant_id, idem_key),
  CHECK (approver_ref IS NULL OR approver_ref <> setter_ref),
  CHECK ((status IN ('approved', 'rejected')) = (approver_ref IS NOT NULL AND decided_at IS NOT NULL))
);
SELECT shared.apply_tenant_rls('pf_mrv.method_version');

CREATE FUNCTION pf_mrv.fn_method_version_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status IN ('approved', 'rejected') THEN
    RAISE EXCEPTION 'method_version %: phiên bản đã % là bất biến', OLD.id, OLD.status
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id OR NEW.rule_version <> OLD.rule_version
     OR NEW.version <> OLD.version OR NEW.params <> OLD.params OR NEW.source_note <> OLD.source_note
     OR NEW.setter_ref <> OLD.setter_ref OR NEW.idem_key <> OLD.idem_key THEN
    RAISE EXCEPTION 'method_version %: nội dung đề xuất bất biến, chỉ đổi được trạng thái', OLD.id
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF NOT (OLD.status = 'submitted' AND NEW.status IN ('approved', 'rejected')) THEN
    RAISE EXCEPTION 'method_version %: chuyển trạng thái % → % không hợp lệ', OLD.id, OLD.status, NEW.status
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER trg_method_version_guard BEFORE UPDATE ON pf_mrv.method_version
  FOR EACH ROW EXECUTE FUNCTION pf_mrv.fn_method_version_guard();
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_mrv.method_version
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_mrv.method_version
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

-- ---------------------------------------------------------------------------
-- 6. Sổ ước tính CO2 (TẠM — chờ khách ký phương pháp)
-- ---------------------------------------------------------------------------
-- Mỗi lần đầu vào đổi (bằng chứng thu gom, kết quả duyệt, tham số) → một dòng mới
-- bù dòng trước của cùng thửa/vụ. input_sha256 = băm đầu vào chuẩn hóa: tính lại
-- với cùng đầu vào không ghi dòng mới. official = false cho tới khi phương pháp và
-- tham số được duyệt (method_version_id khác NULL).
CREATE TABLE pf_mrv.estimate (
  id                     uuid          NOT NULL,
  tenant_id              uuid          NOT NULL,
  stream_key             text          NOT NULL,        -- 'estimate:<season_ref>'
  seq                    bigint        NOT NULL,
  season_ref             uuid          NOT NULL,
  plot_ref               uuid          NOT NULL,
  commune_code           text          NOT NULL,
  season_code            text          NOT NULL,
  input_sha256           text          NOT NULL CHECK (input_sha256 ~ '^[0-9a-f]{64}$'),
  counted_tonnes         numeric(14,3) NOT NULL CHECK (counted_tonnes >= 0),
  avoided_burning_kgco2e numeric(18,3) NOT NULL CHECK (avoided_burning_kgco2e >= 0),
  transport_kgco2e       numeric(18,3) NULL CHECK (transport_kgco2e IS NULL OR transport_kgco2e >= 0),
  transport_status       text          NOT NULL CHECK (transport_status IN ('computed', 'no_leg_data')),
  inputs                 jsonb         NOT NULL CHECK (jsonb_typeof(inputs) = 'array'),
  params                 jsonb         NOT NULL CHECK (jsonb_typeof(params) = 'array'),
  official               boolean       NOT NULL,
  method_version_id      uuid          NULL,
  event_time             timestamptz   NOT NULL,
  recorded_at            timestamptz   NOT NULL DEFAULT now(),
  supersedes_id          uuid          NULL,
  correction_reason      text          NULL,
  actor_ref              uuid          NOT NULL,
  device_id              text          NULL,
  idem_key               text          NOT NULL,        -- 'estimate:<season_ref>:<input_sha256>'
  rule_version           text          NOT NULL,
  prev_hash              bytea         NOT NULL,
  row_hash               bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (official = (method_version_id IS NOT NULL))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX estimate_season ON pf_mrv.estimate (tenant_id, season_ref, seq);
CREATE INDEX estimate_unit   ON pf_mrv.estimate (tenant_id, commune_code, season_ref);
CREATE INDEX estimate_idem   ON pf_mrv.estimate (tenant_id, idem_key);
CREATE INDEX estimate_stream ON pf_mrv.estimate (tenant_id, stream_key, seq);
SELECT shared.apply_ledger('pf_mrv.estimate', 'pf_mrv.estimate');

-- ---------------------------------------------------------------------------
-- Quyền (R-05). Sổ: chỉ SELECT, INSERT.
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON pf_mrv.season_link, pf_mrv.farm_log_link, pf_mrv.collection_evidence, pf_mrv.log_comparison,
  pf_mrv.estimate TO mrv_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_mrv.season_link, pf_mrv.farm_log_link, pf_mrv.collection_evidence,
  pf_mrv.log_comparison, pf_mrv.estimate FROM mrv_app;
GRANT SELECT, INSERT, UPDATE ON pf_mrv.method_version TO mrv_app;
REVOKE DELETE, TRUNCATE ON pf_mrv.method_version FROM mrv_app;
