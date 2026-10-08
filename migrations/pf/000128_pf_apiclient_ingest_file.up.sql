-- pf_apiclient W5.8: kênh tệp cùng lược đồ của cổng tích hợp (R-INT-11, ADR-0033 §3, kiến trúc v4.1 §40).
--
-- Đối tác không có đội IT tải MỘT tệp (NDJSON hoặc CSV, cùng lược đồ bản ghi của hợp đồng nạp) qua
-- POST /v1/files/{contract}; cổng đổi tệp thành các lô ingest_batch.v1 và đưa vào CHÍNH đường ống nạp theo lô
-- của W5.4 (pf 000125: kiểm hợp đồng mặc định từ chối, ValidateRecord, kết quả từng bản ghi) — không có đường
-- kiểm thứ hai. Bảng này chỉ là SỔ tệp đã nhận để idempotent theo tổng kiểm tệp và để tra lô của tệp:
--
--   ingest_file   SỔ tệp đã nhận: hợp đồng + phiên bản, định dạng, SHA-256 + kích thước tệp, số bản ghi, cỡ lô
--                 (max_batch lúc nhận) và số lô. Mã lô của bên gửi (batch_ref) của lô thứ k là UUIDv5 tất định
--                 từ (mã tệp, k) — tra ingest_batch theo (client_id, batch_ref), không lưu mảng mã lô.
--                 idem_key 'file:<client_id>:<hợp đồng>.v<N>:<sha256>' (shared.ledger_idem) → tải lại cùng tệp
--                 trả cùng mã tệp và tiếp tục gửi lô còn thiếu (vd sau 429).
--
-- KHÔNG lưu nội dung tệp: các bản ghi đã nằm trong ingest_batch.items. tenant_id = tenant ĐỐI TÁC
-- (client_tenant_id của khách hàng API — từ bản ghi khóa, không từ request) như mọi sổ nạp.
-- Không dữ liệu cá nhân (R-04). Không UPDATE/DELETE (R-25). Không quyền cho module khác (cổng cách ly W9.1).

SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_apiclient.ingest_file (
  id                 uuid        NOT NULL,                -- mã tệp (UUIDv7) trả trong 202
  tenant_id          uuid        NOT NULL,                -- tenant đối tác (client_tenant_id)
  stream_key         text        NOT NULL,                -- 'client:<client_id>'
  seq                bigint      NOT NULL,
  client_id          uuid        NOT NULL,
  operator_tenant_id uuid        NOT NULL,
  contract           text        NOT NULL CHECK (contract ~ '^[a-z][a-z0-9_]{2,47}$'),
  contract_version   integer     NOT NULL CHECK (contract_version >= 1),
  format             text        NOT NULL CHECK (format IN ('ndjson', 'csv')),
  file_sha256        text        NOT NULL CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
  file_bytes         bigint      NOT NULL CHECK (file_bytes BETWEEN 1 AND 104857600),
  record_count       integer     NOT NULL CHECK (record_count BETWEEN 1 AND 100000),
  chunk_size         integer     NOT NULL CHECK (chunk_size BETWEEN 1 AND 10000),
  batch_count        integer     NOT NULL CHECK (batch_count BETWEEN 1 AND 1000),
  trace_id           text        NULL CHECK (trace_id IS NULL OR length(trace_id) <= 64),
  event_time         timestamptz NOT NULL,                -- lúc cổng nhận (= sent_at của các lô của tệp)
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  supersedes_id      uuid        NULL,
  correction_reason  text        NULL,
  actor_ref          uuid        NOT NULL,                -- = client_id
  device_id          text        NULL,
  idem_key           text        NOT NULL,                -- 'file:<client_id>:<hợp đồng>.v<N>:<sha256>'
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (batch_count = (record_count + chunk_size - 1) / chunk_size)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX ingest_file_id ON pf_apiclient.ingest_file (tenant_id, id);
SELECT shared.apply_ledger('pf_apiclient.ingest_file', 'pf_apiclient.ingest_file');

-- Quyền: chỉ vai nhóm của module (ADR-0028, W2.6). Sổ: chỉ SELECT, INSERT (R-05, R-20).
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'r_apiclient' AND NOT rolcanlogin) THEN
    RAISE EXCEPTION 'thiếu vai nhóm r_apiclient (NOLOGIN) — chạy cmd/migrate bootstrap trước migrate up (ADR-0028)';
  END IF;
END
$$;
GRANT SELECT, INSERT ON pf_apiclient.ingest_file TO r_apiclient;
