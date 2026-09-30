-- mkg_acctbridge (G4, docs/dev/g4-conventions.md §2; docs/design/mkg.md §5.7; tài liệu kiến trúc mục 16):
-- bút toán TỔNG HỢP theo kỳ từ kết quả đóng kỳ của mkg/costing, đẩy MỘT CHIỀU, tần suất tháng, tới
-- phần mềm kế toán pháp định (nhà cung cấp chưa chốt — bộ xuất tệp CSV/XML + bộ giả). Không giao dịch chi tiết.
--
--   mapping_event    SỔ bảng ánh xạ tài khoản có phiên bản (stream 'mapping'): proposed / approved / rejected.
--                    Người duyệt (chief_accountant) ≠ người đề xuất (accounting_bridge_operator) — R-56, CHECK DB.
--                    Mặc định VAS/TT200 là TẠM, CHỜ KHÁCH (provisional = true).
--   mapping_current  bảng chiếu phiên bản ánh xạ.
--   job              bảng VẬN HÀNH của job dựng / đẩy (mẫu mục 12). Đầu vào job chốt lúc yêu cầu (kỳ, lần đóng kỳ,
--                    source_hash mong đợi, phiên bản ánh xạ) — worker không đọc lại cấu hình "hiện tại".
--   journal_event    SỔ bút toán tổng hợp (stream 'period:<period_id>'): built / approved / pushed / push_failed /
--                    superseded. Σ Nợ = Σ Có > 0 trên mọi dòng (CHECK); duyệt ≠ dựng (R-56, CHECK DB).
--   journal_current  bảng chiếu bút toán; dòng đã 'pushed' KHÔNG BAO GIỜ được sửa (trigger) — sai sót sau khi
--                    đẩy = bút toán điều chỉnh của costing ở kỳ sau.
--   push_attempt     bảng VẬN HÀNH các lần đẩy (thành công / tạm không nhận / từ chối), chỉ INSERT.

-- ---------------------------------------------------------------------------
-- 1. Bảng ánh xạ tài khoản có phiên bản
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_acctbridge.mapping_event (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                  -- 'mapping'
  seq               bigint      NOT NULL,
  version_id        uuid        NOT NULL,
  version_seq       integer     NOT NULL CHECK (version_seq >= 1),
  kind              text        NOT NULL CHECK (kind IN ('proposed', 'approved', 'rejected')),
  rules             jsonb       NOT NULL CHECK (jsonb_typeof(rules) = 'array' AND jsonb_array_length(rules) BETWEEN 1 AND 500),
  rules_sha256      text        NOT NULL CHECK (rules_sha256 ~ '^[0-9a-f]{64}$'),
  effective_from    date        NOT NULL,
  provisional       boolean     NOT NULL,
  proposer_ref      uuid        NOT NULL,
  reason            text        NULL CHECK (reason IS NULL OR length(reason) BETWEEN 10 AND 500),
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
  CHECK (kind <> 'proposed' OR (actor_ref = proposer_ref AND reason IS NOT NULL)),
  CHECK (kind = 'proposed' OR actor_ref <> proposer_ref),              -- R-56 ở tầng DB
  CHECK (kind <> 'rejected' OR reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX mapping_event_idem    ON mkg_acctbridge.mapping_event (tenant_id, idem_key);
CREATE INDEX mapping_event_stream  ON mkg_acctbridge.mapping_event (tenant_id, stream_key, seq);
CREATE INDEX mapping_event_version ON mkg_acctbridge.mapping_event (tenant_id, version_id);
SELECT shared.apply_ledger('mkg_acctbridge.mapping_event', 'mkg_acctbridge.mapping_event');

CREATE TABLE mkg_acctbridge.mapping_current (
  tenant_id       uuid        NOT NULL,
  version_id      uuid        NOT NULL,
  version_seq     integer     NOT NULL CHECK (version_seq >= 1),
  rules           jsonb       NOT NULL,
  rules_sha256    text        NOT NULL,
  effective_from  date        NOT NULL,
  provisional     boolean     NOT NULL,
  status          text        NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
  proposed_by     uuid        NOT NULL,
  proposal_reason text        NOT NULL,
  proposed_at     timestamptz NOT NULL,
  decided_by      uuid        NULL,
  decided_at      timestamptz NULL,
  decision_reason text        NULL,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, version_id),
  UNIQUE (tenant_id, version_seq),
  CHECK (decided_by IS NULL OR decided_by <> proposed_by),            -- R-56 lớp hai
  CHECK ((status = 'pending') = (decided_by IS NULL))
);
CREATE INDEX mapping_current_effective ON mkg_acctbridge.mapping_current (tenant_id, status, effective_from, version_seq);
SELECT shared.apply_tenant_rls('mkg_acctbridge.mapping_current');

-- ---------------------------------------------------------------------------
-- 2. Job dựng / đẩy (bảng vận hành, mẫu mục 12)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_acctbridge.job (
  tenant_id            uuid        NOT NULL,
  job_id               uuid        NOT NULL,
  kind                 text        NOT NULL CHECK (kind IN ('build', 'push')),
  status               text        NOT NULL CHECK (status IN ('queued', 'done', 'failed')),
  period_id            uuid        NOT NULL,
  period_code          text        NOT NULL CHECK (period_code ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
  period_from          timestamptz NOT NULL,
  period_to            timestamptz NOT NULL,
  close_run_id         uuid        NOT NULL,
  expected_source_hash text        NOT NULL CHECK (expected_source_hash ~ '^[0-9a-f]{64}$'),
  mapping_version      integer     NULL CHECK (mapping_version IS NULL OR mapping_version >= 1),
  mapping_version_id   uuid        NULL,
  journal_id           uuid        NULL,                    -- build: bút toán dựng ra; push: bút toán cần đẩy
  attempt              integer     NULL CHECK (attempt IS NULL OR attempt >= 1),
  failure_code         text        NULL CHECK (failure_code IS NULL OR failure_code ~ '^acctbridge\.[a-z_]+$'),
  requested_by         uuid        NOT NULL,
  requested_at         timestamptz NOT NULL,
  completed_at         timestamptz NULL,
  idem_key             text        NULL,
  PRIMARY KEY (tenant_id, job_id),
  CHECK (period_to > period_from),
  CHECK ((mapping_version IS NULL) = (mapping_version_id IS NULL)),
  CHECK (kind <> 'push' OR (journal_id IS NOT NULL AND attempt IS NOT NULL)),
  CHECK (kind <> 'build' OR attempt IS NULL),
  CHECK ((status = 'failed') = (failure_code IS NOT NULL)),
  CHECK ((status = 'queued') = (completed_at IS NULL))
);
-- Idempotent theo (kỳ, lần đóng kỳ, phiên bản ánh xạ) — B-02; job lỗi không chặn yêu cầu lại.
CREATE UNIQUE INDEX job_build_unique ON mkg_acctbridge.job (tenant_id, period_id, close_run_id, coalesce(mapping_version, 0))
  WHERE kind = 'build' AND status <> 'failed';
CREATE UNIQUE INDEX job_push_unique ON mkg_acctbridge.job (tenant_id, journal_id, attempt) WHERE kind = 'push';
CREATE UNIQUE INDEX job_idem ON mkg_acctbridge.job (tenant_id, idem_key) WHERE idem_key IS NOT NULL;
CREATE INDEX job_period ON mkg_acctbridge.job (tenant_id, period_id, requested_at);
SELECT shared.apply_tenant_rls('mkg_acctbridge.job');

-- ---------------------------------------------------------------------------
-- 3. Sổ bút toán tổng hợp
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_acctbridge.journal_event (
  id                    uuid          NOT NULL,
  tenant_id             uuid          NOT NULL,
  stream_key            text          NOT NULL,             -- 'period:<period_id>'
  seq                   bigint        NOT NULL,
  journal_id            uuid          NOT NULL,
  period_id             uuid          NOT NULL,
  kind                  text          NOT NULL CHECK (kind IN ('built', 'approved', 'pushed', 'push_failed', 'superseded')),
  version               integer       NOT NULL CHECK (version >= 1),
  period_code           text          NOT NULL CHECK (period_code ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
  close_run_id          uuid          NOT NULL,
  source_hash           text          NOT NULL CHECK (source_hash ~ '^[0-9a-f]{64}$'),
  lines_sha256          text          NOT NULL CHECK (lines_sha256 ~ '^[0-9a-f]{64}$'),
  mapping_version       integer       NOT NULL CHECK (mapping_version >= 1),
  currency              text          NOT NULL CHECK (currency = 'VND'),
  lines                 jsonb         NULL,                 -- chỉ ở 'built'
  line_count            integer       NOT NULL CHECK (line_count >= 2),
  total_debit           numeric(18,2) NOT NULL,
  total_credit          numeric(18,2) NOT NULL,
  built_by              uuid          NOT NULL,
  build_job_id          uuid          NULL,
  supersedes_journal_id uuid          NULL,
  superseded_by         uuid          NULL,
  adapter               text          NULL,
  external_ref          text          NULL,
  artifact_sha256       text          NULL CHECK (artifact_sha256 IS NULL OR artifact_sha256 ~ '^[0-9a-f]{64}$'),
  attempt               integer       NULL CHECK (attempt IS NULL OR attempt >= 1),
  failure_code          text          NULL CHECK (failure_code IS NULL OR failure_code ~ '^acctbridge\.[a-z_]+$'),
  event_time            timestamptz   NOT NULL,
  recorded_at           timestamptz   NOT NULL DEFAULT now(),
  supersedes_id         uuid          NULL,
  correction_reason     text          NULL,
  actor_ref             uuid          NOT NULL,
  device_id             text          NULL,
  idem_key              text          NOT NULL,
  rule_version          text          NOT NULL,
  prev_hash             bytea         NOT NULL,
  row_hash              bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (total_debit = total_credit AND total_debit > 0),              -- B-03: Σ Nợ = Σ Có > 0
  CHECK ((kind = 'built') = (lines IS NOT NULL)),
  CHECK (kind <> 'built' OR (jsonb_typeof(lines) = 'array' AND jsonb_array_length(lines) = line_count
                             AND build_job_id IS NOT NULL AND actor_ref = built_by)),
  CHECK (kind <> 'built' OR (supersedes_journal_id IS NULL) = (supersedes_id IS NULL)),
  CHECK (kind <> 'approved' OR actor_ref <> built_by),                -- R-56 ở tầng DB (B-04)
  CHECK (kind <> 'pushed' OR (adapter IS NOT NULL AND external_ref IS NOT NULL AND attempt IS NOT NULL)),
  CHECK (kind <> 'push_failed' OR (failure_code IS NOT NULL AND attempt IS NOT NULL)),
  CHECK (kind <> 'superseded' OR (superseded_by IS NOT NULL AND superseded_by <> journal_id))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX journal_event_idem    ON mkg_acctbridge.journal_event (tenant_id, idem_key);
CREATE INDEX journal_event_stream  ON mkg_acctbridge.journal_event (tenant_id, stream_key, seq);
CREATE INDEX journal_event_journal ON mkg_acctbridge.journal_event (tenant_id, journal_id, seq);
SELECT shared.apply_ledger('mkg_acctbridge.journal_event', 'mkg_acctbridge.journal_event');

CREATE TABLE mkg_acctbridge.journal_current (
  tenant_id             uuid          NOT NULL,
  journal_id            uuid          NOT NULL,
  period_id             uuid          NOT NULL,
  period_code           text          NOT NULL,
  version               integer       NOT NULL CHECK (version >= 1),
  status                text          NOT NULL CHECK (status IN ('built', 'approved', 'pushed', 'push_failed', 'superseded')),
  close_run_id          uuid          NOT NULL,
  source_hash           text          NOT NULL,
  lines_sha256          text          NOT NULL,
  mapping_version       integer       NOT NULL,
  currency              text          NOT NULL,
  lines                 jsonb         NOT NULL,
  line_count            integer       NOT NULL,
  total_debit           numeric(18,2) NOT NULL,
  total_credit          numeric(18,2) NOT NULL,
  built_by              uuid          NOT NULL,
  built_at              timestamptz   NOT NULL,
  build_job_id          uuid          NOT NULL,
  supersedes_journal_id uuid          NULL,
  superseded_by         uuid          NULL,
  approved_by           uuid          NULL,
  approved_at           timestamptz   NULL,
  adapter               text          NULL,
  external_ref          text          NULL,
  artifact_sha256       text          NULL,
  pushed_at             timestamptz   NULL,
  push_attempts         integer       NOT NULL DEFAULT 0 CHECK (push_attempts >= 0),
  last_failure_code     text          NULL,
  next_retry_at         timestamptz   NULL,
  updated_at            timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, journal_id),
  UNIQUE (tenant_id, period_id, version),
  CHECK (total_debit = total_credit AND total_debit > 0),
  CHECK (approved_by IS NULL OR approved_by <> built_by),             -- R-56 lớp hai
  CHECK (status NOT IN ('approved', 'pushed', 'push_failed') OR approved_by IS NOT NULL),
  CHECK ((status = 'pushed') = (external_ref IS NOT NULL)),
  CHECK (next_retry_at IS NULL OR status = 'approved')
);
-- Một bút toán hiệu lực mỗi kỳ; bản cũ chỉ 'superseded' (không xóa).
CREATE UNIQUE INDEX journal_current_active ON mkg_acctbridge.journal_current (tenant_id, period_id) WHERE status <> 'superseded';
CREATE INDEX journal_current_list  ON mkg_acctbridge.journal_current (tenant_id, journal_id DESC);
CREATE INDEX journal_current_retry ON mkg_acctbridge.journal_current (next_retry_at) WHERE next_retry_at IS NOT NULL;
SELECT shared.apply_tenant_rls('mkg_acctbridge.journal_current');

-- Bút toán đã đẩy không bao giờ sửa (mục 16; RB-1): chặn mọi UPDATE dòng 'pushed' ở tầng DB, kể cả vai
-- ứng dụng. DELETE vẫn cho để dựng lại bảng chiếu từ sổ (R-22) — sổ journal_event là nguồn và bất biến.
CREATE FUNCTION mkg_acctbridge.fn_journal_pushed_frozen() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF OLD.status = 'pushed' THEN
    RAISE EXCEPTION 'acctbridge: bút toán % đã đẩy sang phần mềm kế toán — không sửa; điều chỉnh ở kỳ sau', OLD.journal_id
      USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_journal_pushed_frozen BEFORE UPDATE ON mkg_acctbridge.journal_current
  FOR EACH ROW EXECUTE FUNCTION mkg_acctbridge.fn_journal_pushed_frozen();

-- ---------------------------------------------------------------------------
-- 4. Lần đẩy (bảng vận hành, chỉ INSERT)
-- ---------------------------------------------------------------------------
CREATE TABLE mkg_acctbridge.push_attempt (
  tenant_id       uuid        NOT NULL,
  id              uuid        NOT NULL,
  journal_id      uuid        NOT NULL,
  job_id          uuid        NOT NULL,
  attempt         integer     NOT NULL CHECK (attempt >= 1),
  adapter         text        NOT NULL,
  idempotency_key text        NOT NULL,
  outcome         text        NOT NULL CHECK (outcome IN ('succeeded', 'unavailable', 'rejected')),
  error_code      text        NULL CHECK (error_code IS NULL OR error_code ~ '^acctbridge\.[a-z_]+$'),
  external_ref    text        NULL,
  artifact_sha256 text        NULL,
  next_retry_at   timestamptz NULL,
  started_at      timestamptz NOT NULL,
  finished_at     timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, journal_id, attempt),
  CHECK ((outcome = 'succeeded') = (external_ref IS NOT NULL)),
  CHECK ((outcome = 'succeeded') = (error_code IS NULL)),
  CHECK (next_retry_at IS NULL OR outcome = 'unavailable'),
  CHECK (finished_at >= started_at)
);
CREATE INDEX push_attempt_journal ON mkg_acctbridge.push_attempt (tenant_id, journal_id, attempt);
SELECT shared.apply_tenant_rls('mkg_acctbridge.push_attempt');

-- ---------------------------------------------------------------------------
-- Quyền
-- ---------------------------------------------------------------------------
GRANT SELECT, INSERT ON mkg_acctbridge.mapping_event, mkg_acctbridge.journal_event, mkg_acctbridge.push_attempt TO acctbridge_app;
REVOKE UPDATE, DELETE, TRUNCATE ON mkg_acctbridge.mapping_event, mkg_acctbridge.journal_event, mkg_acctbridge.push_attempt
  FROM acctbridge_app;
-- Bảng chiếu + job: được UPDATE (R-21, R-60); DELETE bảng chiếu chỉ để dựng lại (R-22).
GRANT SELECT, INSERT, UPDATE, DELETE ON mkg_acctbridge.mapping_current, mkg_acctbridge.journal_current TO acctbridge_app;
GRANT SELECT, INSERT, UPDATE ON mkg_acctbridge.job TO acctbridge_app;

-- Job thử lại đẩy bút toán (api-internal, leader) tìm tenant có lần thử lại đến hạn — vai ag_outbox_publisher
-- chỉ đọc ba cột (như mkg_hub.facility_current, mkg 000005). Không đọc số tiền, dòng bút toán.
GRANT USAGE ON SCHEMA mkg_acctbridge TO ag_outbox_publisher;
GRANT SELECT (tenant_id, status, next_retry_at) ON mkg_acctbridge.journal_current TO ag_outbox_publisher;
