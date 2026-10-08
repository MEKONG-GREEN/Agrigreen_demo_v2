-- pf_apiclient W5.3 (R-INT-05, R-INT-10, ADR-0035 §2–§3): TẬP DỮ LIỆU DỰNG SẴN theo phiên bản của cổng tích hợp.
--
--   dataset_version  một phiên bản đã phát (BẤT BIẾN — trigger chặn UPDATE/DELETE/TRUNCATE): dataset_version tăng đơn
--                    điệu, k_used + hysteresis (tập tổng hợp), contributors (SỐ đơn vị chủ dữ liệu, không tên),
--                    admin_unit_as_of (ADR-0032), tổng kiểm record_count + sha256 (R-INT-10) = băm của ảnh chụp
--                    NDJSON, snapshot_key = khóa đối tượng của ảnh chụp trong kho đối tượng trong lãnh thổ (URL ký
--                    chỉ cấp lúc yêu cầu qua cổng — GET /v1/snapshots/{dataset}/{version}, không bao giờ lưu),
--                    first_seq/last_seq = khoảng seq trong feed_entry của phiên bản.
--   dataset_record   trạng thái ĐÃ PHÁT hiện hành của tập (một dòng mỗi entity_key; record NULL = đã retract — không
--                    xóa dòng): job dựng so phiên bản mới với bảng này để chỉ nối upsert cho bản ghi đổi và retract cho
--                    bản ghi biến mất.
--   dataset_cell_state  trạng thái ẨN của từng ô tổng hợp (ADR-0035 §3, trễ ẩn): ô đã ẩn giữ ẩn tới khi số đơn vị
--                    đếm ≥ k + hysteresis; lý do below_k | complementary | withheld_below_k.
--
-- dataset_record / dataset_cell_state là bảng chiếu vận hành (dựng lại được từ phiên bản kế tiếp), không phải sổ.
-- Không dữ liệu cá nhân (R-04): tập tổng hợp chỉ có ô ≥ k; số đơn vị đếm của từng ô KHÔNG lưu.

CREATE TABLE pf_apiclient.dataset_version (
  tenant_id         uuid        NOT NULL,                 -- đơn vị vận hành
  dataset           text        NOT NULL CHECK (dataset ~ '^[a-z][a-z0-9_]{2,47}$'),
  version           bigint      NOT NULL CHECK (version >= 1),
  contract_version  integer     NOT NULL CHECK (contract_version >= 1),
  built_at          timestamptz NOT NULL,
  as_of             timestamptz NOT NULL,                 -- mốc dữ liệu nguồn
  admin_unit_as_of  date        NULL,
  k_used            integer     NULL CHECK (k_used IS NULL OR k_used >= 2),
  hysteresis        integer     NULL CHECK (hysteresis IS NULL OR hysteresis >= 1),
  contributors      integer     NULL CHECK (contributors IS NULL OR contributors >= 0),
  record_count      bigint      NOT NULL CHECK (record_count >= 0),
  sha256            text        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  snapshot_key      text        NULL CHECK (snapshot_key IS NULL OR snapshot_key ~ '^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$'),
  snapshot_size     bigint      NULL CHECK (snapshot_size IS NULL OR snapshot_size >= 0),
  first_seq         bigint      NULL,
  last_seq          bigint      NULL,
  upserts           integer     NOT NULL CHECK (upserts >= 0),
  retracts          integer     NOT NULL CHECK (retracts >= 0),
  suppressed        integer     NULL CHECK (suppressed IS NULL OR suppressed >= 0),
  rule_version      text        NOT NULL,
  PRIMARY KEY (tenant_id, dataset, version),
  CHECK ((k_used IS NULL) = (hysteresis IS NULL)),
  CHECK ((snapshot_key IS NULL) = (snapshot_size IS NULL)),
  CHECK ((first_seq IS NULL) = (last_seq IS NULL) AND (first_seq IS NULL OR first_seq <= last_seq))
);
SELECT shared.apply_tenant_rls('pf_apiclient.dataset_version');
CREATE TRIGGER trg_immutable BEFORE UPDATE OR DELETE ON pf_apiclient.dataset_version
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_immutable_trunc BEFORE TRUNCATE ON pf_apiclient.dataset_version
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

CREATE TABLE pf_apiclient.dataset_record (
  tenant_id         uuid        NOT NULL,
  dataset           text        NOT NULL CHECK (dataset ~ '^[a-z][a-z0-9_]{2,47}$'),
  partition_key     text        NOT NULL DEFAULT '',
  entity_key        text        NOT NULL CHECK (char_length(entity_key) BETWEEN 1 AND 200),
  record            jsonb       NULL CHECK (record IS NULL OR jsonb_typeof(record) = 'object'),   -- NULL = đã retract
  record_sha256     text        NULL CHECK (record_sha256 ~ '^[0-9a-f]{64}$'),
  version           bigint      NULL,                     -- phiên bản phát gần nhất (NULL: feed không theo phiên bản)
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, dataset, partition_key, entity_key),
  CHECK ((record IS NULL) = (record_sha256 IS NULL))
);
SELECT shared.apply_tenant_rls('pf_apiclient.dataset_record');

CREATE TABLE pf_apiclient.dataset_cell_state (
  tenant_id         uuid        NOT NULL,
  dataset           text        NOT NULL CHECK (dataset ~ '^[a-z][a-z0-9_]{2,47}$'),
  entity_key        text        NOT NULL CHECK (char_length(entity_key) BETWEEN 1 AND 200),
  suppressed        boolean     NOT NULL,
  reason            text        NULL CHECK (reason IS NULL OR reason IN ('below_k', 'complementary', 'withheld_below_k')),
  since_version     bigint      NOT NULL CHECK (since_version >= 1),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, dataset, entity_key),
  CHECK (suppressed = (reason IS NOT NULL))
);
SELECT shared.apply_tenant_rls('pf_apiclient.dataset_cell_state');

REVOKE ALL ON pf_apiclient.dataset_version, pf_apiclient.dataset_record, pf_apiclient.dataset_cell_state FROM PUBLIC;
GRANT SELECT, INSERT ON pf_apiclient.dataset_version TO r_apiclient;
GRANT SELECT, INSERT, UPDATE ON pf_apiclient.dataset_record, pf_apiclient.dataset_cell_state TO r_apiclient;
