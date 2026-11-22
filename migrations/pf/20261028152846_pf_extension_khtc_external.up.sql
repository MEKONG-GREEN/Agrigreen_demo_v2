-- W11.67 (D4 KH7, BRD KHTC v2.2 FN-TI-02.11, BR-DL-19, BR-DL-04): Số liệu nguồn ngoài của KHTC. Chỉ pf_extension.
--
-- 1. khtc_ext_indicator — bảng CHỈ GHI THÊM (shared.apply_append_only): danh mục chỉ tiêu nguồn ngoài (tên, ĐVT, nhóm);
--    KHTC thêm được chỉ tiêu mới ngay trên màn. Mã, tên (chuẩn hóa) duy nhất trong tenant.
-- 2. khtc_ext_evidence — bảng CHỈ GHI THÊM: siêu dữ liệu tệp minh chứng (văn bản của cơ quan ngoài, không có dữ liệu cá
--    nhân); nội dung ở kho đối tượng (shared/objstore, ghi một lần, khóa theo băm) — KHÔNG lưu byte tệp trong CSDL.
-- 3. khtc_ext_figure — sổ chỉ ghi thêm (shared.apply_ledger): mỗi chuỗi 'khtc_ext_figure:<kỳ>:<chỉ tiêu>' là lịch sử giá
--    trị của MỘT chỉ tiêu × MỘT kỳ KHTC; seq = phiên bản; bản mới nhất là giá trị hiệu lực ("một chỉ tiêu, một kỳ chỉ có
--    một giá trị hiệu lực"). Sửa/ghi đè/rút = bản ghi bù (supersedes_id = bản hiệu lực trước, lý do bắt buộc ≥ 10 ký tự).
--    Nguồn (cơ quan, văn bản hoặc kênh, ngày) bắt buộc với action 'set'. Kỳ đã đóng/hủy hoặc báo cáo đã có Bản cuối →
--    từ chối ghi (BR-DL-19: sửa đến khi kỳ đóng). Khóa tư vấn 'khtc_snapshot:<tenant>:<kỳ>' xếp hàng với đóng kỳ, Bản cuối.
-- 4. khtc_ext_import — sổ chỉ ghi thêm: "Nạp Excel tổng hợp" hai bước. Chuỗi 'khtc_ext_import:<lần nạp>': seq 1 'staged'
--    (kết quả kiểm từng dòng, không ghi số liệu), seq 2 'committed' (đã ghi các dòng hợp lệ). Không lưu byte tệp.
-- Giá trị là numeric (R-CODE-01, không float). Vai r_extension: SELECT, INSERT.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.khtc_ext_indicator (
  tenant_id   uuid        NOT NULL,
  id          uuid        NOT NULL,
  code        text        NOT NULL CHECK (code ~ '^[a-z0-9][a-z0-9._-]{0,39}$'),
  name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 300),
  name_key    text        NOT NULL CHECK (char_length(name_key) BETWEEN 1 AND 300),
  unit        text        NOT NULL DEFAULT '' CHECK (char_length(unit) <= 50),
  group_name  text        NOT NULL DEFAULT '' CHECK (char_length(group_name) <= 200),
  actor_ref   uuid        NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  idem_key    text        NOT NULL,
  PRIMARY KEY (tenant_id, id),
  CONSTRAINT khtc_ext_indicator_code UNIQUE (tenant_id, code),
  CONSTRAINT khtc_ext_indicator_name UNIQUE (tenant_id, name_key),
  CONSTRAINT khtc_ext_indicator_idem UNIQUE (tenant_id, idem_key)
);
SELECT shared.apply_append_only('pf_extension.khtc_ext_indicator');

CREATE TABLE pf_extension.khtc_ext_evidence (
  tenant_id    uuid        NOT NULL,
  id           uuid        NOT NULL,
  file_name    text        NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 200),
  content_type text        NOT NULL CHECK (char_length(content_type) BETWEEN 1 AND 100),
  size_bytes   bigint      NOT NULL CHECK (size_bytes BETWEEN 1 AND 15728640),
  sha256       text        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  object_key   text        NOT NULL CHECK (char_length(object_key) BETWEEN 1 AND 512),
  actor_ref    uuid        NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  idem_key     text        NOT NULL,
  PRIMARY KEY (tenant_id, id),
  CONSTRAINT khtc_ext_evidence_idem UNIQUE (tenant_id, idem_key)
);
SELECT shared.apply_append_only('pf_extension.khtc_ext_evidence');

CREATE TABLE pf_extension.khtc_ext_figure (
  id                uuid          NOT NULL,
  tenant_id         uuid          NOT NULL,
  stream_key        text          NOT NULL,
  seq               bigint        NOT NULL,
  period_id         uuid          NOT NULL,
  indicator_id      uuid          NOT NULL,
  action            text          NOT NULL CHECK (action IN ('set', 'withdraw')),
  value             numeric(22,4) NULL CHECK (value IS NULL OR abs(value) < 1e17),
  unit              text          NOT NULL DEFAULT '' CHECK (char_length(unit) <= 50),
  agency            text          NULL CHECK (agency IS NULL OR char_length(agency) BETWEEN 1 AND 300),
  document          text          NULL CHECK (document IS NULL OR char_length(document) BETWEEN 1 AND 300),
  source_date       date          NULL,
  evidence_ids      uuid[]        NOT NULL DEFAULT '{}' CHECK (cardinality(evidence_ids) <= 10 AND array_position(evidence_ids, NULL) IS NULL),
  import_id         uuid          NULL,
  event_time        timestamptz   NOT NULL,
  recorded_at       timestamptz   NOT NULL DEFAULT now(),
  supersedes_id     uuid          NULL,
  correction_reason text          NULL,
  actor_ref         uuid          NOT NULL,
  device_id         text          NULL,
  idem_key          text          NOT NULL,
  rule_version      text          NOT NULL,
  prev_hash         bytea         NOT NULL,
  row_hash          bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR char_length(correction_reason) BETWEEN 10 AND 1000),
  -- BR-DL-19: số liệu hiệu lực bắt buộc ghi nguồn (cơ quan, văn bản hoặc kênh, ngày); rút số liệu không có giá trị.
  CHECK ((action = 'set') = (value IS NOT NULL)),
  CHECK (action <> 'set' OR (agency IS NOT NULL AND document IS NOT NULL AND source_date IS NOT NULL)),
  CONSTRAINT khtc_ext_figure_stream CHECK (stream_key = 'khtc_ext_figure:' || period_id::text || ':' || indicator_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_ext_figure_period ON pf_extension.khtc_ext_figure (tenant_id, period_id, indicator_id, seq DESC);
CREATE INDEX khtc_ext_figure_id ON pf_extension.khtc_ext_figure (tenant_id, id);

CREATE TABLE pf_extension.khtc_ext_import (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,
  seq               bigint      NOT NULL,
  import_id         uuid        NOT NULL,
  plan_year         smallint    NOT NULL CHECK (plan_year BETWEEN 2000 AND 2100),
  action            text        NOT NULL CHECK (action IN ('staged', 'committed')),
  file_name         text        NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 200),
  file_sha256       text        NOT NULL CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
  payload           jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 4194304),
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
  CHECK (correction_reason IS NULL OR char_length(correction_reason) BETWEEN 10 AND 1000),
  CONSTRAINT khtc_ext_import_stream CHECK (stream_key = 'khtc_ext_import:' || import_id::text)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX khtc_ext_import_id ON pf_extension.khtc_ext_import (tenant_id, import_id, seq DESC);
CREATE INDEX khtc_ext_import_year ON pf_extension.khtc_ext_import (tenant_id, plan_year, recorded_at DESC);

CREATE FUNCTION pf_extension.fn_khtc_ext_figure_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE v_latest uuid;
BEGIN
  -- Cùng khóa với ảnh chụp nguồn, đóng/hủy kỳ và Bản cuối (pf 20261015223036): ghi số liệu xếp hàng với các thao tác đó.
  PERFORM pg_advisory_xact_lock(hashtextextended('khtc_snapshot:' || NEW.tenant_id::text || ':' || NEW.period_id::text, 0));
  IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_period p WHERE p.tenant_id = NEW.tenant_id AND p.id = NEW.period_id) THEN
    RAISE EXCEPTION 'missing KHTC period' USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'khtc_ext_period_fkey';
  END IF;
  IF EXISTS (SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.tenant_id = NEW.tenant_id AND e.period_id = NEW.period_id
              AND e.action IN ('closed', 'cancelled')) THEN
    RAISE EXCEPTION 'closed KHTC period is read-only' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_ext_period_closed';
  END IF;
  IF EXISTS (SELECT 1 FROM pf_extension.khtc_report_final f WHERE f.tenant_id = NEW.tenant_id AND f.period_id = NEW.period_id) THEN
    RAISE EXCEPTION 'final KHTC report freezes its sources' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_report_already_final';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_ext_indicator i WHERE i.tenant_id = NEW.tenant_id AND i.id = NEW.indicator_id) THEN
    RAISE EXCEPTION 'missing external indicator' USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'khtc_ext_indicator_fkey';
  END IF;
  IF EXISTS (SELECT 1 FROM unnest(NEW.evidence_ids) AS x(id)
              WHERE NOT EXISTS (SELECT 1 FROM pf_extension.khtc_ext_evidence v WHERE v.tenant_id = NEW.tenant_id AND v.id = x.id)) THEN
    RAISE EXCEPTION 'missing evidence file' USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'khtc_ext_evidence_fkey';
  END IF;
  -- Bản ghi bù phải trỏ đúng bản hiệu lực hiện tại (không ghi đè âm thầm khi hai người sửa cùng lúc).
  SELECT f.id INTO v_latest FROM pf_extension.khtc_ext_figure f
   WHERE f.tenant_id = NEW.tenant_id AND f.period_id = NEW.period_id AND f.indicator_id = NEW.indicator_id
   ORDER BY f.seq DESC LIMIT 1;
  IF NEW.supersedes_id IS DISTINCT FROM v_latest THEN
    RAISE EXCEPTION 'stale external figure' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_ext_figure_stale';
  END IF;
  IF NEW.action = 'withdraw' AND v_latest IS NULL THEN
    RAISE EXCEPTION 'nothing to withdraw' USING ERRCODE = 'check_violation', CONSTRAINT = 'khtc_ext_figure_stale';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_00_khtc_ext_figure_guard BEFORE INSERT ON pf_extension.khtc_ext_figure
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_ext_figure_guard();

CREATE FUNCTION pf_extension.fn_khtc_ext_import_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('khtc_ext_import:' || NEW.tenant_id::text || ':' || NEW.import_id::text, 0));
  IF NEW.action = 'staged' AND EXISTS (SELECT 1 FROM pf_extension.khtc_ext_import i WHERE i.tenant_id = NEW.tenant_id AND i.import_id = NEW.import_id) THEN
    RAISE EXCEPTION 'import already staged' USING ERRCODE = 'unique_violation', CONSTRAINT = 'khtc_ext_import_committed';
  END IF;
  IF NEW.action = 'committed' THEN
    IF NOT EXISTS (SELECT 1 FROM pf_extension.khtc_ext_import i WHERE i.tenant_id = NEW.tenant_id AND i.import_id = NEW.import_id AND i.action = 'staged') THEN
      RAISE EXCEPTION 'missing staged import' USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'khtc_ext_import_missing';
    END IF;
    IF EXISTS (SELECT 1 FROM pf_extension.khtc_ext_import i WHERE i.tenant_id = NEW.tenant_id AND i.import_id = NEW.import_id AND i.action = 'committed') THEN
      RAISE EXCEPTION 'import already committed' USING ERRCODE = 'unique_violation', CONSTRAINT = 'khtc_ext_import_committed';
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_00_khtc_ext_import_guard BEFORE INSERT ON pf_extension.khtc_ext_import
  FOR EACH ROW EXECUTE FUNCTION pf_extension.fn_khtc_ext_import_guard();

SELECT shared.apply_ledger('pf_extension.khtc_ext_figure', 'pf_extension.khtc_ext_figure');
SELECT shared.apply_ledger('pf_extension.khtc_ext_import', 'pf_extension.khtc_ext_import');

REVOKE ALL ON pf_extension.khtc_ext_indicator, pf_extension.khtc_ext_evidence, pf_extension.khtc_ext_figure,
  pf_extension.khtc_ext_import FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.khtc_ext_indicator, pf_extension.khtc_ext_evidence, pf_extension.khtc_ext_figure,
  pf_extension.khtc_ext_import TO r_extension;
