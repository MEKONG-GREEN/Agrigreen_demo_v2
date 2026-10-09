-- pf_coop W5.6 (ADR-0034 luồng song phương, kiến trúc v4.1 §38.2; Đề xuất — bật production chờ C-12): SỔ SONG PHƯƠNG
-- MKG ↔ HTX ở module chủ pf/cooperative (schema pf_coop, W3.3) — hợp đồng contracts/integration/ingest
-- collection_schedule, roll_count_statement (bên a = MKG ghi qua nạp theo lô W5.4) và htx_roll_confirmation (bên b =
-- HTX ghi trên App HTX). Chọn pf/cooperative (không pf_cropcycle): HTX, thửa và vụ của bản ghi đều ở pf_coop sau W3.3;
-- bảng van cũ pf_cropcycle.collection_* giữ nguyên tới W5.10.
--
--   bilateral_pair_event  SỔ vòng đời cặp (counterparty_tenant_id = tenant MKG, htx_node_id = Nút HTX = htx_ref của
--                         hợp đồng): established (chỉ HTX lập — bên b), terminated (bên nào cũng chấm dứt được, lý do
--                         bắt buộc). Cặp hiệu lực = sự kiện cuối là established. stream 'pair:<pair_id>'.
--   bilateral_record      SỔ bản ghi song phương: đúng byte đã kiểm hợp đồng (payload chuẩn hóa + payload_sha256), khóa
--                         nghiệp vụ idempotent (idem_key 'rec:<pair>:<hợp đồng>:v<N>:<business_key>'). stream
--                         'pair:<pair_id>' → seq LIÊN TỤC từ 1 mỗi cặp theo thứ tự commit (khóa chain_head): feed của cặp
--                         chép theo seq, không sót.
--
-- CHÍNH SÁCH hai bên (ADR-0034 §3, biểu thức p_pair): HTX = (tenant chủ, Nút = app.node_id — so khớp BẰNG, không qua
-- node_closure: cán bộ KN cấp xã/tỉnh cùng tenant KHÔNG thấy); MKG = counterparty_tenant_id = app.tenant_id (chỉ qua
-- cổng tích hợp, tenant lấy từ khóa API). WITH CHECK ràng phía tác giả. Một chính sách mỗi bảng (ADR-0005); TÊN giữ
-- p_tenant vì shared.v_missing_rls hiện chỉ chấp nhận tên đó — đổi tên p_pair khi cổng chấp nhận (sau W3.4, shared
-- 000012). Phân vùng con giữ p_tenant mặc định (truy cập thẳng phân vùng chỉ thấy tenant chủ; ứng dụng chỉ đi qua
-- bảng cha).
--
-- KHÔNG BAO GIỜ tổng hợp (ADR-0034 §5): chỉ vai r_cooperative có quyền (không r_agg, không view _pub, không sản phẩm
-- dữ liệu, không module khác) — kiểm ở migrations/bilateral_isolation_integration_test.go.
--
-- Feed của cặp (GET /v1/feeds/bilateral?pair=…) dùng NGUYÊN kho feed W5.3 của pf/apiclient (partition_key = mã cặp,
-- entity_key lưu "<hợp đồng>|<khóa>") — không đổi lược đồ pf_apiclient ở đây (migration này chỉ chạm pf_coop).
--
-- Số 000135 (bản kế hoạch đầu ghi pf 000103 — đã lỗi thời). Không UPDATE/DELETE (R-25). Không dữ liệu cá nhân (R-04):
-- ba hợp đồng personal_data = false (mã, ngày, số cuộn).

SET LOCAL lock_timeout = '5s';

-- ---------------------------------------------------------------------------
-- 1. Vòng đời cặp
-- ---------------------------------------------------------------------------
CREATE TABLE pf_coop.bilateral_pair_event (
  id                     uuid        NOT NULL,
  tenant_id              uuid        NOT NULL,            -- tenant CHỦ của bên HTX
  stream_key             text        NOT NULL,            -- 'pair:<pair_id>'
  seq                    bigint      NOT NULL,
  pair_id                uuid        NOT NULL,            -- UUIDv5(counterparty_tenant_id, htx_node_id) — apiclient/port.PairID
  htx_node_id            uuid        NOT NULL,            -- Nút HTX (= htx_ref của hợp đồng)
  counterparty_tenant_id uuid        NOT NULL,            -- tenant MKG
  author_side            text        NOT NULL CHECK (author_side IN ('a', 'b')),
  action                 text        NOT NULL CHECK (action IN ('established', 'terminated')),
  reason                 text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 10 AND 500),
  event_time             timestamptz NOT NULL,
  recorded_at            timestamptz NOT NULL DEFAULT now(),
  supersedes_id          uuid        NULL,
  correction_reason      text        NULL,
  actor_ref              uuid        NOT NULL,
  device_id              text        NULL,
  idem_key               text        NOT NULL,            -- 'pair:<pair_id>:<n>' (n = số sự kiện trước + 1)
  rule_version           text        NOT NULL,
  prev_hash              bytea       NOT NULL,
  row_hash               bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK (counterparty_tenant_id <> tenant_id),
  CHECK (action = 'terminated' OR author_side = 'b'),     -- chỉ HTX lập cặp
  CHECK ((action = 'terminated') = (reason IS NOT NULL)),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX bilateral_pair_event_pair ON pf_coop.bilateral_pair_event (pair_id, seq);
CREATE INDEX bilateral_pair_event_cp   ON pf_coop.bilateral_pair_event (counterparty_tenant_id, htx_node_id);
CREATE INDEX bilateral_pair_event_htx  ON pf_coop.bilateral_pair_event (tenant_id, htx_node_id);
SELECT shared.apply_ledger('pf_coop.bilateral_pair_event', 'pf_coop.bilateral_pair_event');

-- ---------------------------------------------------------------------------
-- 2. Bản ghi song phương
-- ---------------------------------------------------------------------------
CREATE TABLE pf_coop.bilateral_record (
  id                     uuid        NOT NULL,
  tenant_id              uuid        NOT NULL,            -- tenant CHỦ của bên HTX
  stream_key             text        NOT NULL,            -- 'pair:<pair_id>'
  seq                    bigint      NOT NULL,            -- thứ tự trong cặp (fn_chain, liên tục)
  pair_id                uuid        NOT NULL,
  htx_node_id            uuid        NOT NULL,
  counterparty_tenant_id uuid        NOT NULL,
  contract               text        NOT NULL CHECK (contract IN ('collection_schedule', 'roll_count_statement', 'htx_roll_confirmation')),
  contract_version       integer     NOT NULL CHECK (contract_version >= 1),
  record_ref             text        NOT NULL CHECK (char_length(record_ref) BETWEEN 1 AND 200),   -- entity_key
  business_key           text        NOT NULL CHECK (char_length(business_key) BETWEEN 1 AND 400),
  payload                jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
  payload_sha256         text        NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
  author_side            text        NOT NULL CHECK (author_side IN ('a', 'b')),
  channel                text        NOT NULL CHECK (channel IN ('api', 'file', 'app')),
  client_id              uuid        NULL,                -- khách hàng API (bên a)
  batch_id               uuid        NULL,                -- lô nạp W5.4 (bên a)
  item_index             integer     NULL CHECK (item_index IS NULL OR item_index >= 0),
  event_time             timestamptz NOT NULL,
  recorded_at            timestamptz NOT NULL DEFAULT now(),
  supersedes_id          uuid        NULL,
  correction_reason      text        NULL,
  actor_ref              uuid        NOT NULL,            -- client_id (bên a) | người dùng HTX (bên b)
  device_id              text        NULL,
  idem_key               text        NOT NULL,
  rule_version           text        NOT NULL,
  prev_hash              bytea       NOT NULL,
  row_hash               bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK (counterparty_tenant_id <> tenant_id),
  CHECK ((author_side = 'b') = (contract = 'htx_roll_confirmation')),
  CHECK ((author_side = 'a') = (client_id IS NOT NULL AND batch_id IS NOT NULL AND item_index IS NOT NULL)),
  CHECK ((author_side = 'a') = (channel IN ('api', 'file'))),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX bilateral_record_pair ON pf_coop.bilateral_record (pair_id, seq);
CREATE INDEX bilateral_record_key  ON pf_coop.bilateral_record (pair_id, contract, contract_version, business_key);
CREATE INDEX bilateral_record_cp   ON pf_coop.bilateral_record (counterparty_tenant_id, contract, recorded_at);
CREATE INDEX bilateral_record_htx  ON pf_coop.bilateral_record (tenant_id, htx_node_id, recorded_at);
SELECT shared.apply_ledger('pf_coop.bilateral_record', 'pf_coop.bilateral_record');

-- ---------------------------------------------------------------------------
-- 3. Chính sách hai bên (ADR-0034 §3) — ALTER (không DROP: migratecheck RB-2) biểu thức của p_tenant mặc định
--    trên bảng cha; vẫn là policy PERMISSIVE duy nhất (shared.v_missing_rls), nhánh HTX vẫn cách ly tenant.
-- ---------------------------------------------------------------------------
ALTER POLICY p_tenant ON pf_coop.bilateral_pair_event
  USING (
    (tenant_id = shared.current_tenant() AND htx_node_id = k_ref.ctx_uuid('app.node_id'))
    OR counterparty_tenant_id = shared.current_tenant())
  WITH CHECK (
    (author_side = 'b' AND tenant_id = shared.current_tenant() AND htx_node_id = k_ref.ctx_uuid('app.node_id'))
    OR (author_side = 'a' AND counterparty_tenant_id = shared.current_tenant()));

ALTER POLICY p_tenant ON pf_coop.bilateral_record
  USING (
    (tenant_id = shared.current_tenant() AND htx_node_id = k_ref.ctx_uuid('app.node_id'))
    OR counterparty_tenant_id = shared.current_tenant())
  WITH CHECK (
    (author_side = 'b' AND tenant_id = shared.current_tenant() AND htx_node_id = k_ref.ctx_uuid('app.node_id'))
    OR (author_side = 'a' AND counterparty_tenant_id = shared.current_tenant()));

-- ---------------------------------------------------------------------------
-- 4. Quyền: chỉ vai nhóm của module chủ; sổ chỉ SELECT, INSERT (R-05, R-20)
-- ---------------------------------------------------------------------------
REVOKE ALL ON pf_coop.bilateral_pair_event, pf_coop.bilateral_record FROM PUBLIC;
GRANT SELECT, INSERT ON pf_coop.bilateral_pair_event, pf_coop.bilateral_record TO r_cooperative;
