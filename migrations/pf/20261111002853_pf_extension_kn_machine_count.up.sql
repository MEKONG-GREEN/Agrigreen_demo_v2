-- pf/extension (`dom/extension`) — KN-STANDALONE-5-TT-DATA-MAY (#306, kế hoạch KN-STANDALONE quyết định D1): SỐ MÁY THEO
-- LOẠI CỦA HTX (Trồng trọt FN-XA-03.04 "Quản lý máy móc của HTX") về kho KN tự quản. Trước đây KN ghi vào module CGH
-- (pf_mechanization.coop_machine_count_event, coop_machine_count) khóa theo HTX nền tảng; từ đây KN ghi sổ riêng, khóa theo
-- hồ sơ KN loại HTX (pf_extension.kn_producer, kind cooperative). Bảng CGH GIỮ NGUYÊN, chỉ đọc một lần để chép — không
-- DROP, không UPDATE/DELETE.
--
-- kn_machine_count_event — sổ chỉ ghi thêm (shared.apply_ledger: DB tính seq, prev_hash, row_hash; chặn UPDATE/DELETE; RLS
--   tenant), chuỗi 'kn_machines:<cooperative_ref>'. Mỗi loại máy đổi trong một lần lưu = một dòng mang số cũ → mới ("lưu
--   lịch sử thay đổi số lượng", BRD FN-XA-03.04): set | removed; migrated = dòng chép một lần từ module CGH.
--   department_code của người ghi; chế độ hỗ trợ Admin ghi on_behalf_of + lý do.
-- kn_machine_count — bảng chiếu hiện hành (HTX × loại máy; R-21, cập nhật cùng giao dịch với sổ). Dòng đã bỏ giữ lại với
--   active = false, quantity = 0 (không xóa).
--   cooperative_ref: hồ sơ KN loại HTX (kn_producer.producer_ref). Dòng chép từ HTX nền tảng CHƯA có hồ sơ KN liên kết giữ
--   khóa là mã HTX cũ (cooperative_ref = legacy_htx_ref) để tổng theo xã không đổi; cán bộ xã vẫn sửa/bỏ được dòng đó.
--   legacy_htx_ref: HTX nền tảng của dòng lúc chép (chỉ đối chiếu, không ghi lại).
-- Không dữ liệu cá nhân ngoài actor_ref, updated_by (mã tài khoản). Vai r_extension: sổ SELECT, INSERT; bảng chiếu SELECT,
-- INSERT, UPDATE.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.kn_machine_count_event (
  id                 uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,                 -- 'kn_machines:<cooperative_ref>'
  seq                bigint      NOT NULL,
  cooperative_ref    uuid        NOT NULL,
  commune_code       text        NOT NULL CHECK (char_length(commune_code) BETWEEN 1 AND 20),
  machine_type       text        NOT NULL CHECK (char_length(machine_type) BETWEEN 1 AND 60),
  action             text        NOT NULL CHECK (action IN ('migrated', 'set', 'removed')),
  quantity           integer     NOT NULL CHECK (quantity BETWEEN 0 AND 1000000),
  prev_quantity      integer     NULL CHECK (prev_quantity IS NULL OR prev_quantity >= 0),
  note               text        NULL CHECK (note IS NULL OR char_length(note) <= 500),
  prev_note          text        NULL,
  department_code    text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  reason             text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
  request_key        text        NOT NULL,                 -- Idempotency-Key của lần lưu (gom các dòng một lần PUT)
  on_behalf_of       uuid        NULL,                     -- chế độ hỗ trợ của Admin
  support_session_id uuid        NULL,
  support_reason     text        NULL,
  legacy_htx_ref     uuid        NULL,                     -- chỉ dòng migrated: HTX nền tảng nguồn
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  actor_ref          uuid        NOT NULL,
  idem_key           text        NOT NULL,                 -- request_key || '|' || machine_type
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT kn_machine_count_event_stream CHECK (stream_key = 'kn_machines:' || cooperative_ref::text),
  CONSTRAINT kn_machine_count_event_removed CHECK (action <> 'removed' OR quantity = 0),
  CONSTRAINT kn_machine_count_event_support CHECK ((support_session_id IS NULL) = (support_reason IS NULL)),
  CONSTRAINT kn_machine_count_event_legacy CHECK ((action = 'migrated') = (legacy_htx_ref IS NOT NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX kn_machine_count_event_stream_seq ON pf_extension.kn_machine_count_event (tenant_id, stream_key, seq);
CREATE INDEX kn_machine_count_event_idem ON pf_extension.kn_machine_count_event (tenant_id, idem_key);
CREATE INDEX kn_machine_count_event_request ON pf_extension.kn_machine_count_event (tenant_id, request_key);
SELECT shared.apply_ledger('pf_extension.kn_machine_count_event', 'pf_extension.kn_machine_count_event');

CREATE TABLE pf_extension.kn_machine_count (
  tenant_id       uuid        NOT NULL,
  cooperative_ref uuid        NOT NULL,
  machine_type    text        NOT NULL,
  quantity        integer     NOT NULL CHECK (quantity BETWEEN 0 AND 1000000),
  note            text        NULL,
  commune_code    text        NOT NULL,
  department_code text        NOT NULL,
  active          boolean     NOT NULL DEFAULT true,
  legacy_htx_ref  uuid        NULL,
  updated_at      timestamptz NOT NULL,
  updated_by      uuid        NOT NULL,
  last_event_id   uuid        NOT NULL,
  last_seq        bigint      NOT NULL,
  PRIMARY KEY (tenant_id, cooperative_ref, machine_type),
  CONSTRAINT kn_machine_count_active CHECK (active OR quantity = 0)
);
CREATE INDEX kn_machine_count_commune ON pf_extension.kn_machine_count (tenant_id, commune_code, machine_type) WHERE active;
SELECT shared.apply_tenant_rls('pf_extension.kn_machine_count');
CREATE TRIGGER trg_no_delete BEFORE DELETE ON pf_extension.kn_machine_count
  FOR EACH ROW EXECUTE FUNCTION shared.fn_immutable();
CREATE TRIGGER trg_no_truncate BEFORE TRUNCATE ON pf_extension.kn_machine_count
  FOR EACH STATEMENT EXECUTE FUNCTION shared.fn_immutable();

REVOKE ALL ON pf_extension.kn_machine_count_event, pf_extension.kn_machine_count FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.kn_machine_count_event TO r_extension;
GRANT SELECT, INSERT, UPDATE ON pf_extension.kn_machine_count TO r_extension;
-- Chép dữ liệu cũ: migration kế tiếp 20261111002854_pf_extension_kn_machine_count_copy.
