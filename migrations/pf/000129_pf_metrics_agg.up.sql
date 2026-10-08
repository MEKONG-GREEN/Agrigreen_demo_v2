-- Vai tổng hợp r_agg: danh mục job có phiên bản + nhật ký chạy (W6.4, R-DB-11, R-SEC-03, ADR-0031 §3–§5).
--
-- Module chủ: pf/metrics (`cap/analytics`, aka trong module.yaml) — ADR-0031 đặt job tổng hợp ở cap/analytics; đầu ra
-- sau ngưỡng k thuộc module này. pf/workflow chỉ giữ yêu cầu/quyết định bốn mắt (kind 'agg.job').
--
-- Hai sổ chỉ ghi thêm (mẫu 8.1, shared.apply_ledger — chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant). Dòng
-- nằm ở TENANT QUẢN TRỊ (đơn vị vận hành nền tảng — cấu hình của bộ chạy), không ở tenant góp dữ liệu:
--   - agg_job — mỗi phiên bản định nghĩa job: dòng seq 1 'proposed' (job_id, version, view _pub đọc, câu SQL, mục
--     đích, k tối thiểu, băm định nghĩa, người đề xuất) + đúng MỘT dòng seq 2 'approved' | 'rejected' (người quyết
--     định). Chuỗi 'job:<job_id>:v<version>' — một phiên bản một đề xuất, một quyết định (CHECK seq). Dòng quyết định
--     KHÔNG tự khai định nghĩa: trigger trg_bind_proposal (chạy trước trg_chain theo tên) chép từ dòng đề xuất cùng
--     tenant; CHECK agg_job_four_eyes: người quyết định ≠ người đề xuất (cùng quy tắc ở pf_workflow.decision —
--     quyết định ghi CÙNG giao dịch qua pf_workflow.decide).
--   - agg_run — mỗi lần chạy một dòng: job, phiên bản, băm, dòng danh mục đã duyệt, người/tiến trình gọi, bắt
--     đầu/kết thúc, số tenant/dòng đọc, số nhóm ra/bị ẩn, k dùng, kết quả (succeeded | failed | refused). Không ghi
--     được nhật ký → bộ chạy không trả đầu ra (ADR-0031 §5).
--
-- Bộ chạy (pf/metrics app.Agg, db.WithAggTx) chỉ đổi sang r_agg trong giao dịch CHỈ ĐỌC, SAU khi đọc agg_job dưới
-- r_metrics thấy đúng job + phiên bản + băm là phiên bản đã duyệt MỚI NHẤT (phiên bản cũ, chưa duyệt, bị từ chối
-- hoặc định nghĩa bị sửa → refused, không đổi vai).
--
-- Quyền:
--   - r_metrics: SELECT, INSERT hai sổ; EXECUTE pf_workflow.submit/decide (điểm vào ghi chéo module lâu dài —
--     schema_isolation_exceptions.yaml, owner ADR-0028-write-port, như r_forms).
--   - r_agg (NOLOGIN BYPASSRLS, bootstrap — ag_migrator không có CREATEROLE): đọc view _pub qua membership
--     r_<module>_pub (bootstrap); ở đây chỉ thêm USAGE k_ref + SELECT CỘT tenant_id của k_ref.org (liệt kê tenant góp
--     dữ liệu, như ag_ops_jobs — pf 000114). Không quyền nào trên schema nội bộ, không quyền ghi.
--
-- Không dữ liệu cá nhân (R-04): chỉ mã tham chiếu người (actor_ref), mã job, số đếm.

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_agg' AND NOT rolcanlogin AND rolbypassrls) THEN
    RAISE EXCEPTION 'thiếu vai r_agg (NOLOGIN BYPASSRLS) — chạy cmd/migrate bootstrap bản W6.4 trước migrate up (ADR-0031)';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_metrics' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_metrics (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)';
  END IF;
END
$$;

CREATE TABLE pf_metrics.agg_job (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,                 -- tenant quản trị
  stream_key        text        NOT NULL,                 -- 'job:<job_id>:v<version>'
  seq               bigint      NOT NULL,
  job_id            text        NOT NULL CHECK (job_id ~ '^[a-z][a-z0-9_]{0,39}\.[a-z][a-z0-9_]{0,59}$'),
  version           integer     NOT NULL CHECK (version BETWEEN 1 AND 100000),
  action            text        NOT NULL CHECK (action IN ('proposed', 'approved', 'rejected')),
  def_hash          text        NOT NULL CHECK (def_hash ~ '^[0-9a-f]{64}$'),   -- SHA-256 định nghĩa (domain.AggJob.Hash)
  views             text[]      NOT NULL,                 -- view _pub đọc, đã sắp
  query_sql         text        NOT NULL CHECK (char_length(query_sql) BETWEEN 1 AND 8000),
  purpose           text        NOT NULL CHECK (char_length(purpose) BETWEEN 10 AND 500),
  k_min             integer     NOT NULL CHECK (k_min >= 2),
  proposal_id       uuid        NULL,                     -- dòng quyết định: id dòng đề xuất
  request_ref       uuid        NOT NULL,                 -- pf_workflow.request.id (kind 'agg.job', subject = id đề xuất)
  requester_ref     uuid        NOT NULL,                 -- người đề xuất (dòng quyết định: chép từ đề xuất)
  decider_ref       uuid        NULL,                     -- người quyết định
  note              text        NULL CHECK (note IS NULL OR char_length(note) BETWEEN 1 AND 1000),
  event_time        timestamptz NOT NULL,                 -- R-13
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- 'propose:<job_id>:v<version>' | 'decide:<id đề xuất>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CONSTRAINT agg_job_stream CHECK (stream_key = 'job:' || job_id || ':v' || version::text AND seq IN (1, 2)
                                   AND (seq = 1) = (action = 'proposed')),
  CONSTRAINT agg_job_shape CHECK ((action = 'proposed') = (proposal_id IS NULL AND decider_ref IS NULL)),
  CONSTRAINT agg_job_views CHECK (cardinality(views) BETWEEN 1 AND 20
                                  AND array_to_string(views, ',') ~ '^pf_[a-z0-9_]+_pub\.v[0-9]+_[a-z0-9_]+(,pf_[a-z0-9_]+_pub\.v[0-9]+_[a-z0-9_]+)*$'),
  -- R-56 / R-SEC-03: thêm hoặc sửa job là thay đổi đặc quyền — người quyết định khác người đề xuất.
  CONSTRAINT agg_job_four_eyes CHECK (action = 'proposed' OR decider_ref <> requester_ref),
  CONSTRAINT agg_job_note_required CHECK (action <> 'rejected' OR char_length(btrim(coalesce(note, ''))) >= 1),
  CHECK (actor_ref = coalesce(decider_ref, requester_ref))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX agg_job_job ON pf_metrics.agg_job (tenant_id, job_id, version);
CREATE INDEX agg_job_id  ON pf_metrics.agg_job (tenant_id, id);

-- Gắn quyết định vào đề xuất (kiểm chéo dòng ở DB): chép định nghĩa và người đề xuất từ dòng đề xuất cùng tenant (RLS
-- của người ghi vẫn áp). Không có đề xuất → 23503.
CREATE FUNCTION pf_metrics.fn_bind_agg_proposal() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  p record;
BEGIN
  IF NEW.action = 'proposed' THEN
    RETURN NEW;
  END IF;
  SELECT j.job_id, j.version, j.def_hash, j.views, j.query_sql, j.purpose, j.k_min, j.request_ref, j.requester_ref INTO p
    FROM pf_metrics.agg_job j
   WHERE j.tenant_id = NEW.tenant_id AND j.id = NEW.proposal_id AND j.action = 'proposed';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pf_metrics.agg_job: không có đề xuất % trong tenant', NEW.proposal_id
      USING ERRCODE = 'foreign_key_violation', CONSTRAINT = 'agg_job_proposal_fkey';
  END IF;
  NEW.job_id        := p.job_id;
  NEW.version       := p.version;
  NEW.def_hash      := p.def_hash;
  NEW.views         := p.views;
  NEW.query_sql     := p.query_sql;
  NEW.purpose       := p.purpose;
  NEW.k_min         := p.k_min;
  NEW.request_ref   := p.request_ref;
  NEW.requester_ref := p.requester_ref;
  NEW.stream_key    := 'job:' || p.job_id || ':v' || p.version::text;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_bind_proposal BEFORE INSERT ON pf_metrics.agg_job
  FOR EACH ROW EXECUTE FUNCTION pf_metrics.fn_bind_agg_proposal();
SELECT shared.apply_ledger('pf_metrics.agg_job', 'pf_metrics.agg_job');

CREATE TABLE pf_metrics.agg_run (
  id                uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id         uuid        NOT NULL,                 -- tenant quản trị
  stream_key        text        NOT NULL,                 -- 'run:<job_id>'
  seq               bigint      NOT NULL,
  job_id            text        NOT NULL CHECK (job_id ~ '^[a-z][a-z0-9_]{0,39}\.[a-z][a-z0-9_]{0,59}$'),
  version           integer     NOT NULL CHECK (version >= 1),
  def_hash          text        NOT NULL CHECK (def_hash ~ '^[0-9a-f]{64}$'),
  job_ref           uuid        NULL,                     -- dòng agg_job 'approved' đã kiểm; NULL khi refused
  k_used            integer     NULL CHECK (k_used IS NULL OR k_used >= 2),
  started_at        timestamptz NOT NULL,
  finished_at       timestamptz NOT NULL,
  tenants_read      integer     NOT NULL DEFAULT 0 CHECK (tenants_read >= 0),
  rows_read         bigint      NOT NULL DEFAULT 0 CHECK (rows_read >= 0),
  groups_out        integer     NOT NULL DEFAULT 0 CHECK (groups_out >= 0),
  groups_suppressed integer     NOT NULL DEFAULT 0 CHECK (groups_suppressed >= 0),
  outcome           text        NOT NULL CHECK (outcome IN ('succeeded', 'failed', 'refused')),
  error_code        text        NULL CHECK (error_code IS NULL OR char_length(error_code) BETWEEN 1 AND 100),
  event_time        timestamptz NOT NULL,                 -- R-13
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- người/tiến trình gọi chạy
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- 'run:<id>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (stream_key = 'run:' || job_id),
  CHECK (finished_at >= started_at),
  CONSTRAINT agg_run_succeeded CHECK (outcome <> 'succeeded' OR (job_ref IS NOT NULL AND k_used IS NOT NULL AND error_code IS NULL)),
  CONSTRAINT agg_run_error CHECK (outcome = 'succeeded' OR error_code IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX agg_run_job ON pf_metrics.agg_run (tenant_id, job_id, recorded_at);
SELECT shared.apply_ledger('pf_metrics.agg_run', 'pf_metrics.agg_run');

REVOKE ALL ON pf_metrics.agg_job, pf_metrics.agg_run FROM PUBLIC;
REVOKE ALL ON FUNCTION pf_metrics.fn_bind_agg_proposal() FROM PUBLIC;

-- Sổ của module: chỉ SELECT, INSERT (R-05, R-20).
GRANT SELECT, INSERT ON pf_metrics.agg_job, pf_metrics.agg_run TO r_metrics;

-- Bốn mắt qua cap/workflow (kind 'agg.job') TRONG giao dịch của pf/metrics: chỉ USAGE + EXECUTE hai hàm SECURITY
-- DEFINER (điểm vào ghi chéo module, ADR-0028 Hiện thực), không quyền bảng pf_workflow.
GRANT USAGE ON SCHEMA pf_workflow TO r_metrics;
GRANT EXECUTE ON FUNCTION pf_workflow.submit(uuid, text, uuid, uuid, uuid, uuid, text, timestamptz, text),
  pf_workflow.decide(uuid, uuid, text, uuid, uuid, uuid, uuid, text, text, timestamptz, text) TO r_metrics;

-- r_agg: liệt kê tenant góp dữ liệu (BYPASSRLS, chỉ cột tenant_id). View _pub qua membership r_<module>_pub.
GRANT USAGE ON SCHEMA k_ref TO r_agg;
GRANT SELECT (tenant_id) ON k_ref.org TO r_agg;
