-- KN v7.7 P7 (Cán bộ xã trên field-app) — Thu thập dữ liệu: máy móc HTX (FN-XA-03.04) và sửa hồ sơ HTX/nông hộ
-- (FN-XA-03.03). docs/brd/kn-v7.7-plan.md §2 (pf 000078), hợp đồng P7 §1–§2.
--
--   1. pf_mechanization.coop_machine_count_event  SỔ khai báo SỐ LƯỢNG theo loại máy của HTX (mô hình v7.7 — khác
--      bảng machine từng chiếc, giữ nguyên). Mỗi loại đổi trong một lần lưu = một dòng ('set' | 'removed') mang số
--      cũ → mới: "lưu lịch sử thay đổi số lượng" (BRD FN-XA-03.04). department_code của người ghi; chế độ hỗ trợ
--      Admin ghi on_behalf_of + lý do (FN-AD-04.07).
--   2. pf_mechanization.coop_machine_count        bảng chiếu hiện hành (HTX × loại máy). Dòng đã bỏ giữ lại với
--      active = false, quantity = 0 (không xóa — RB-1).
--   3. pf_party.kn_profile_event                   SỔ sự kiện bù khi sửa hồ sơ HTX (tên, diện tích, mã số thuế, trụ
--      sở, người đại diện) và diện tích canh tác của nông hộ — ảnh chụp trước/sau KHÔNG có dữ liệu cá nhân (người đại
--      diện chỉ là representative_ref của kho định danh, R-04). Bảng chiếu pf_party.org / cooperative / household
--      được UPDATE trong CÙNG giao dịch (R-21; party_app đã có quyền UPDATE từ pf 000003).
--
-- Không khóa ngoại xuyên schema (R-06): cooperative_ref là UUID thuần của pf_party.

-- ---------------------------------------------------------------------------
-- 1. Sổ khai báo số lượng máy theo loại của HTX
-- ---------------------------------------------------------------------------
CREATE TABLE pf_mechanization.coop_machine_count_event (
  id                 uuid        NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,                 -- 'coop_machines:<cooperative_ref>'
  seq                bigint      NOT NULL,
  cooperative_ref    uuid        NOT NULL,
  commune_code       text        NOT NULL,
  machine_type       text        NOT NULL CHECK (char_length(machine_type) BETWEEN 1 AND 60),
  action             text        NOT NULL CHECK (action IN ('set', 'removed')),
  quantity           integer     NOT NULL CHECK (quantity BETWEEN 0 AND 1000000),
  prev_quantity      integer     NULL CHECK (prev_quantity IS NULL OR prev_quantity >= 0),
  note               text        NULL CHECK (note IS NULL OR char_length(note) <= 500),
  prev_note          text        NULL,
  department_code    text        NOT NULL CHECK (department_code ~ '^[a-z][a-z0-9_]{1,39}$'),
  reason             text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
  request_key        text        NOT NULL,                 -- Idempotency-Key của lần lưu (gom các dòng một lần PUT)
  on_behalf_of       uuid        NULL,                     -- chế độ hỗ trợ Admin (FN-AD-04.07)
  support_session_id uuid        NULL,
  support_reason     text        NULL,
  event_time         timestamptz NOT NULL,                 -- R-13
  recorded_at        timestamptz NOT NULL DEFAULT now(),   -- R-13, khóa phân vùng
  supersedes_id      uuid        NULL,
  correction_reason  text        NULL,
  actor_ref          uuid        NOT NULL,
  device_id          text        NULL,
  idem_key           text        NOT NULL,                 -- request_key || '|' || machine_type
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK (action <> 'removed' OR quantity = 0),
  CHECK ((support_session_id IS NULL) = (support_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX coop_machine_count_event_stream  ON pf_mechanization.coop_machine_count_event (tenant_id, stream_key, seq);
CREATE INDEX coop_machine_count_event_idem    ON pf_mechanization.coop_machine_count_event (tenant_id, idem_key);
CREATE INDEX coop_machine_count_event_request ON pf_mechanization.coop_machine_count_event (tenant_id, request_key);
SELECT shared.apply_ledger('pf_mechanization.coop_machine_count_event', 'pf_mechanization.coop_machine_count_event');

-- ---------------------------------------------------------------------------
-- 2. Bảng chiếu hiện hành
-- ---------------------------------------------------------------------------
CREATE TABLE pf_mechanization.coop_machine_count (
  tenant_id       uuid        NOT NULL,
  cooperative_ref uuid        NOT NULL,
  machine_type    text        NOT NULL,
  quantity        integer     NOT NULL CHECK (quantity BETWEEN 0 AND 1000000),
  note            text        NULL,
  commune_code    text        NOT NULL,
  department_code text        NOT NULL,
  active          boolean     NOT NULL DEFAULT true,
  updated_at      timestamptz NOT NULL,
  updated_by      uuid        NOT NULL,
  last_event_id   uuid        NOT NULL,
  last_seq        bigint      NOT NULL,
  PRIMARY KEY (tenant_id, cooperative_ref, machine_type),
  CHECK (active OR quantity = 0)
);
CREATE INDEX coop_machine_count_commune ON pf_mechanization.coop_machine_count (tenant_id, commune_code, machine_type) WHERE active;
SELECT shared.apply_tenant_rls('pf_mechanization.coop_machine_count');

GRANT SELECT, INSERT ON pf_mechanization.coop_machine_count_event TO mechanization_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_mechanization.coop_machine_count_event FROM mechanization_app;
GRANT SELECT, INSERT, UPDATE ON pf_mechanization.coop_machine_count TO mechanization_app;

-- ---------------------------------------------------------------------------
-- 3. pf_party: sổ sự kiện bù khi sửa hồ sơ HTX / nông hộ (FN-XA-03.03)
-- ---------------------------------------------------------------------------
CREATE TABLE pf_party.kn_profile_event (
  id                 uuid        NOT NULL,
  tenant_id          uuid        NOT NULL,
  stream_key         text        NOT NULL,                 -- 'kn_profile:<subject_kind>:<subject_ref>'
  seq                bigint      NOT NULL,
  subject_kind       text        NOT NULL CHECK (subject_kind IN ('cooperative', 'household')),
  subject_ref        uuid        NOT NULL,
  commune_code       text        NOT NULL,
  action             text        NOT NULL CHECK (action IN ('updated')),
  before             jsonb       NOT NULL,                 -- ảnh chụp trước (không PII)
  after              jsonb       NOT NULL,                 -- ảnh chụp sau (không PII)
  reason             text        NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
  on_behalf_of       uuid        NULL,                     -- chế độ hỗ trợ Admin (FN-AD-04.07)
  support_session_id uuid        NULL,
  support_reason     text        NULL,
  event_time         timestamptz NOT NULL,
  recorded_at        timestamptz NOT NULL DEFAULT now(),
  supersedes_id      uuid        NULL,
  correction_reason  text        NULL,
  actor_ref          uuid        NOT NULL,
  device_id          text        NULL,
  idem_key           text        NOT NULL,
  rule_version       text        NOT NULL,
  prev_hash          bytea       NOT NULL,
  row_hash           bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL)),
  CHECK (correction_reason IS NULL OR length(correction_reason) >= 10),
  CHECK ((support_session_id IS NULL) = (support_reason IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX kn_profile_event_stream  ON pf_party.kn_profile_event (tenant_id, stream_key, seq);
CREATE INDEX kn_profile_event_idem    ON pf_party.kn_profile_event (tenant_id, idem_key);
CREATE INDEX kn_profile_event_subject ON pf_party.kn_profile_event (tenant_id, subject_kind, subject_ref, seq);
SELECT shared.apply_ledger('pf_party.kn_profile_event', 'pf_party.kn_profile_event');

GRANT SELECT, INSERT ON pf_party.kn_profile_event TO party_app;
REVOKE UPDATE, DELETE, TRUNCATE ON pf_party.kn_profile_event FROM party_app;
