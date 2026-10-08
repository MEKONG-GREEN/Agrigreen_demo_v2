-- pf_apiclient W5.4: nạp theo lô của cổng tích hợp (R-INT-03, R-INT-04, ADR-0033 §3, kiến trúc v4.1 §41.3).
--
-- POST /v1/batches/{contract} → kiểm phong bì đồng bộ → 202 + mã lô → hàng đợi riêng đối tác → inbox → kiểm hợp
-- đồng (mặc định từ chối) → port của module chủ → kết quả từng bản ghi. Mọi bảng là SỔ chỉ ghi thêm
-- (shared.apply_ledger: chuỗi băm, chặn UPDATE/DELETE/TRUNCATE, RLS theo tenant). tenant_id = tenant ĐỐI TÁC
-- (client_tenant_id của khách hàng API, lấy từ bản ghi khóa — không bao giờ từ request): đối tác chỉ thấy lô của
-- chính mình, kể cả khi hai khách hàng khác tenant cùng dùng một cổng.
--
--   ingest_batch          SỔ lô đã nhận: hợp đồng + phiên bản, batch_ref (= Idempotency-Key), băm thân, các phần
--                         tử (jsonb — bộ xử lý đọc lại từ đây, không từ thông điệp hàng đợi). idem_key
--                         'batch:<client_id>:<batch_ref>' (shared.ledger_idem) → gọi lặp trả cùng mã lô.
--   ingest_record_result  SỔ kết quả từng bản ghi: accepted | rejected + mã problem (không giá trị bản ghi).
--                         idem_key 'result:<batch_id>:<index>'.
--   ingest_batch_outcome  SỔ kết cục lô: completed (đếm accepted/rejected) | dead_lettered (vượt số lần giao,
--                         thông điệp ở DLQ của đối tác). Một dòng mỗi lô: idem_key 'outcome:<batch_id>'.
--   ingest_staging        SỔ đệm của hợp đồng song phương (collection_schedule, roll_count_statement,
--                         htx_roll_confirmation — proposed, C-12) khi port của module chủ CHƯA có: bản ghi đã
--                         qua hợp đồng ghi ở đây, idempotent theo khóa nghiệp vụ (idem_key
--                         'stage:<contract>:v<N>:<business_key>'). W5.6 nối port thật (pf_coop/pf_cropcycle) và
--                         chuyển dữ liệu; bảng này không được module khác đọc.
--
-- Không dữ liệu cá nhân (R-04): hợp đồng nạp hiện có đều personal_data = false; đối tác là khách hàng API (tổ chức).
-- Không UPDATE/DELETE (R-25). Không quyền cho module khác (cổng cách ly schema W9.1).

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Lô đã nhận
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.ingest_batch (
  id                uuid        NOT NULL,                 -- mã lô (UUIDv7) trả trong 202
  tenant_id         uuid        NOT NULL,                 -- tenant đối tác (client_tenant_id)
  stream_key        text        NOT NULL,                 -- 'client:<client_id>'
  seq               bigint      NOT NULL,
  client_id         uuid        NOT NULL,                 -- khách hàng API (hàng đợi riêng theo khách hàng)
  operator_tenant_id uuid       NOT NULL,                 -- đơn vị vận hành của khách hàng
  contract          text        NOT NULL CHECK (contract ~ '^[a-z][a-z0-9_]{2,47}$'),
  contract_version  integer     NOT NULL CHECK (contract_version >= 1),
  batch_ref         uuid        NOT NULL,                 -- = Idempotency-Key (phong bì ingest_batch.v1)
  request_hash      text        NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
  item_count        integer     NOT NULL CHECK (item_count BETWEEN 1 AND 10000),
  items             jsonb       NOT NULL CHECK (jsonb_typeof(items) = 'array'),
  sent_at           timestamptz NOT NULL,
  as_of             timestamptz NULL,
  trace_id          text        NULL CHECK (trace_id IS NULL OR length(trace_id) <= 64),
  event_time        timestamptz NOT NULL,                 -- lúc cổng nhận
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- = client_id
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- 'batch:<client_id>:<batch_ref>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (jsonb_array_length(items) = item_count)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX ingest_batch_id ON pf_apiclient.ingest_batch (tenant_id, id);
CREATE INDEX ingest_batch_ref ON pf_apiclient.ingest_batch (tenant_id, client_id, batch_ref);
SELECT shared.apply_ledger('pf_apiclient.ingest_batch', 'pf_apiclient.ingest_batch');

-- ---------------------------------------------------------------------------
-- 2. Kết quả từng bản ghi
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.ingest_record_result (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,                 -- tenant đối tác
  stream_key        text        NOT NULL,                 -- 'batch:<batch_id>'
  seq               bigint      NOT NULL,
  batch_id          uuid        NOT NULL,
  item_index        integer     NOT NULL CHECK (item_index >= 0),
  entity_key        text        NOT NULL CHECK (length(entity_key) <= 300),
  outcome           text        NOT NULL CHECK (outcome IN ('accepted', 'rejected')),
  problem_code      text        NULL CHECK (problem_code IS NULL OR problem_code ~ '^[a-z][a-z0-9_]*\.[a-z][a-z0-9_.]*$'),
  detail            text        NULL CHECK (detail IS NULL OR length(detail) <= 300),
  target            text        NOT NULL CHECK (length(target) BETWEEN 1 AND 100), -- port đích đã gọi (hoặc 'gate')
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- = client_id
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- 'result:<batch_id>:<index>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK ((outcome = 'rejected') = (problem_code IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX ingest_record_result_batch ON pf_apiclient.ingest_record_result (tenant_id, batch_id, item_index);
SELECT shared.apply_ledger('pf_apiclient.ingest_record_result', 'pf_apiclient.ingest_record_result');

-- ---------------------------------------------------------------------------
-- 3. Kết cục lô
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.ingest_batch_outcome (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,                 -- tenant đối tác
  stream_key        text        NOT NULL,                 -- 'batch:<batch_id>'
  seq               bigint      NOT NULL,
  batch_id          uuid        NOT NULL,
  outcome           text        NOT NULL CHECK (outcome IN ('completed', 'dead_lettered')),
  accepted          integer     NOT NULL CHECK (accepted >= 0),
  rejected          integer     NOT NULL CHECK (rejected >= 0),
  problem_code      text        NULL CHECK (problem_code IS NULL OR length(problem_code) <= 100),
  delivery_count    integer     NOT NULL CHECK (delivery_count >= 0),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- = client_id
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- 'outcome:<batch_id>' — một kết cục mỗi lô
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (outcome = 'completed' OR (accepted = 0 AND rejected = 0))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX ingest_batch_outcome_batch ON pf_apiclient.ingest_batch_outcome (tenant_id, batch_id);
SELECT shared.apply_ledger('pf_apiclient.ingest_batch_outcome', 'pf_apiclient.ingest_batch_outcome');

-- ---------------------------------------------------------------------------
-- 4. Sổ đệm song phương (port chủ chưa có — W5.6 nối port thật)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_apiclient.ingest_staging (
  id                uuid        NOT NULL,
  tenant_id         uuid        NOT NULL,                 -- tenant đối tác gửi (bên a của cặp, ADR-0034)
  stream_key        text        NOT NULL,                 -- 'stage:<contract>'
  seq               bigint      NOT NULL,
  contract          text        NOT NULL CHECK (contract ~ '^[a-z][a-z0-9_]{2,47}$'),
  contract_version  integer     NOT NULL CHECK (contract_version >= 1),
  client_id         uuid        NOT NULL,
  batch_id          uuid        NOT NULL,
  item_index        integer     NOT NULL CHECK (item_index >= 0),
  entity_key        text        NOT NULL CHECK (length(entity_key) <= 300),
  business_key      text        NOT NULL CHECK (length(business_key) BETWEEN 1 AND 400),
  payload           jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
  payload_sha256    text        NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  event_time        timestamptz NOT NULL,
  recorded_at       timestamptz NOT NULL DEFAULT now(),
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,                 -- = client_id
  device_id         text        NULL,
  idem_key          text        NOT NULL,                 -- 'stage:<contract>:v<N>:<business_key>'
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX ingest_staging_key ON pf_apiclient.ingest_staging (tenant_id, contract, contract_version, business_key);
SELECT shared.apply_ledger('pf_apiclient.ingest_staging', 'pf_apiclient.ingest_staging');

-- ---------------------------------------------------------------------------
-- 5. Quyền: chỉ vai nhóm của module (ADR-0028, W2.6). Sổ: chỉ SELECT, INSERT (R-05, R-20).
-- ---------------------------------------------------------------------------
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_apiclient' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_apiclient (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)';
  END IF;
END
$$;
GRANT SELECT, INSERT ON pf_apiclient.ingest_batch, pf_apiclient.ingest_record_result, pf_apiclient.ingest_batch_outcome,
  pf_apiclient.ingest_staging TO r_apiclient;
