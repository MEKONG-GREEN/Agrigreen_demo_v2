-- mkg_fieldops W5.6 (ADR-0034 luồng song phương, ADR-0033 §3; Đề xuất — bật production chờ C-12): phía MKG của luồng
-- song phương MKG ↔ HTX qua cổng tích hợp, như mọi đối tác (khóa API, nạp theo lô W5.4, feed của cặp, tổng kiểm ngày
-- W5.7). Chạy SONG SONG với van valve_collection (mkg 000006 — giữ nguyên tới W5.10).
--
--   bilateral_send         hàng gửi: mỗi bản ghi van đã xuất (valve_export collection_schedule / htx_collection_statement)
--                          được chiếu sang hợp đồng song phương (collection_schedule giữ nguyên lược đồ;
--                          htx_collection_statement → roll_count_statement + schedule_ref = lệnh gom) và gửi qua
--                          POST /v1/batches/{hợp đồng}. Trạng thái pending → submitted (batch_ref = Idempotency-Key ghi
--                          TRƯỚC khi gọi — gửi lại đúng lô sau lỗi tạm) → accepted | rejected (mã problem của cổng; từ chối
--                          integration.pair_missing được xếp lại sau 1 giờ — cặp có thể được lập muộn). Bảng trạng thái
--                          (không sổ): nguồn sự thật là sổ valve_export và sổ song phương của nền tảng.
--   bilateral_feed_state   một dòng mỗi cặp (htx_ref) của tenant MKG: cursor feed của cặp GET /v1/feeds/bilateral?pair=,
--                          as_of, terminated_at (cổng trả 403 integration.pair_missing — cặp chấm dứt; bản sao giữ để đối
--                          soát lịch sử), lỗi gần nhất.
--   bilateral_feed_record  bản sao các mục feed của cặp (cả hai phía: bản ghi MKG đã gửi và xác nhận/phản đối của HTX),
--                          một dòng mỗi (cặp, seq) — áp lại là no-op. recorded_at = recorded_at của sổ song phương: đối
--                          soát tổng kiểm ngày bilateral_day.v1 với GET /v1/feeds/{hợp đồng}/checksums.
--
-- Số 000038 (bản kế hoạch đầu ghi mkg 000025 — đã lỗi thời; 000037 là W8.2). Không dữ liệu cá nhân (R-04): mã, ngày, số cuộn.

CREATE TABLE mkg_fieldops.bilateral_send (
  tenant_id         uuid        NOT NULL,
  source_id         uuid        NOT NULL,                -- valve_export.id
  contract          text        NOT NULL CHECK (contract IN ('collection_schedule', 'roll_count_statement')),
  contract_version  integer     NOT NULL CHECK (contract_version >= 1),
  entity_key        text        NOT NULL CHECK (char_length(entity_key) BETWEEN 1 AND 200),
  business_key      text        NOT NULL CHECK (char_length(business_key) BETWEEN 1 AND 400),
  htx_ref           uuid        NOT NULL,
  record            jsonb       NOT NULL CHECK (jsonb_typeof(record) = 'object'),
  payload_sha256    text        NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  status            text        NOT NULL CHECK (status IN ('pending', 'submitted', 'accepted', 'rejected')),
  batch_ref         uuid        NULL,
  batch_id          uuid        NULL,
  item_index        integer     NULL CHECK (item_index IS NULL OR item_index >= 0),
  problem_code      text        NULL CHECK (problem_code IS NULL OR char_length(problem_code) <= 100),
  attempts          integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  staged_at         timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, source_id),
  CHECK (status = 'pending' OR (batch_ref IS NOT NULL AND item_index IS NOT NULL))
);
CREATE UNIQUE INDEX bilateral_send_key ON mkg_fieldops.bilateral_send (tenant_id, contract, contract_version, business_key);
CREATE INDEX bilateral_send_status ON mkg_fieldops.bilateral_send (tenant_id, status, contract);
SELECT shared.apply_tenant_rls('mkg_fieldops.bilateral_send');

CREATE TABLE mkg_fieldops.bilateral_feed_state (
  tenant_id       uuid        NOT NULL,
  htx_ref         uuid        NOT NULL,
  feed_cursor     text        NULL CHECK (feed_cursor IS NULL OR char_length(feed_cursor) BETWEEN 1 AND 200),
  caught_up       boolean     NOT NULL DEFAULT false,
  as_of           timestamptz NULL,
  terminated_at   timestamptz NULL,
  last_pulled_at  timestamptz NULL,
  last_error      text        NULL CHECK (last_error IS NULL OR char_length(last_error) <= 300),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, htx_ref),
  CHECK (NOT caught_up OR as_of IS NOT NULL)
);
SELECT shared.apply_tenant_rls('mkg_fieldops.bilateral_feed_state');

CREATE TABLE mkg_fieldops.bilateral_feed_record (
  tenant_id         uuid        NOT NULL,
  htx_ref           uuid        NOT NULL,
  seq               bigint      NOT NULL CHECK (seq >= 1),
  event_id          uuid        NOT NULL,
  contract          text        NOT NULL CHECK (contract ~ '^[a-z][a-z0-9_]{2,47}$'),
  contract_version  integer     NOT NULL CHECK (contract_version >= 1),
  entity_key        text        NOT NULL CHECK (char_length(entity_key) BETWEEN 1 AND 200),
  business_key      text        NOT NULL CHECK (char_length(business_key) BETWEEN 1 AND 400),
  record            jsonb       NOT NULL CHECK (jsonb_typeof(record) = 'object'),
  payload_sha256    text        NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  recorded_at       timestamptz NOT NULL,
  applied_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, htx_ref, seq)
);
CREATE UNIQUE INDEX bilateral_feed_record_event ON mkg_fieldops.bilateral_feed_record (tenant_id, event_id);
CREATE INDEX bilateral_feed_record_day ON mkg_fieldops.bilateral_feed_record (tenant_id, contract, recorded_at);
SELECT shared.apply_tenant_rls('mkg_fieldops.bilateral_feed_record');

-- Bảng trạng thái / bản sao: vai nhóm của module (W2.6). bilateral_send: UPDATE cột trạng thái (R-60).
REVOKE ALL ON mkg_fieldops.bilateral_send, mkg_fieldops.bilateral_feed_state, mkg_fieldops.bilateral_feed_record FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON mkg_fieldops.bilateral_send TO r_fieldops;
GRANT SELECT, INSERT, UPDATE ON mkg_fieldops.bilateral_feed_state TO r_fieldops;
GRANT SELECT, INSERT ON mkg_fieldops.bilateral_feed_record TO r_fieldops;
