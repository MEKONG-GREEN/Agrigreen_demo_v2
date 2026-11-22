-- pf/extension (`dom/extension`, phòng ban chan_nuoi_thuy_san) — CNTS-V4-HOUSEHOLD-LIST (#251), ADR-0056, BRD CNTS v4.0
-- FN-10 "Danh sách hộ chăn nuôi" (H1–H4, BR-03…BR-08, AC-05…AC-09, bảng trường HCN-01…HCN-09).
--
-- Danh sách hộ chăn nuôi là master data của KN (không phải hộ mô hình, không phải hộ HTX — ADR-0055 Q-KNS-04: KN không
-- ghi dữ liệu HTX). Họ tên, SĐT chủ hộ ở kho định danh (vault) — sổ chỉ giữ person_ref; lịch sử họ tên/SĐT là mã bản sửa
-- của kho (pii_revision_ref). Vị trí hộ là dữ liệu cá nhân: ở bảng thường cnts_livestock_household_checkin (xóa được khi
-- có yêu cầu xóa — QĐ-8), không vào sổ không xóa được.
--
-- 1. cnts_livestock_household — sổ chỉ ghi thêm (shared.apply_ledger), chuỗi 'lhh:<household_ref>': mỗi dòng là MỘT
--    phiên bản đủ trường. Sự việc: created (thêm từ danh sách, có check-in), created_pending (BM-10 DB-04, BR-08 — #252),
--    updated, checked_in (Chờ bổ sung vị trí → Đang dùng), relocated (check-in lại có lý do — H4), deactivated (BR-04).
-- 2. cnts_livestock_household_key — bảng thường: mã hộ HCN-01 duy nhất theo tenant (sổ phân vùng không đặt được UNIQUE).
-- 3. cnts_livestock_household_counter — bộ đếm mã hộ theo tenant (HCN + 7 chữ số), không dùng lại.
-- 4. cnts_livestock_household_checkin — bảng thường: mỗi lần check-in tại nhà hộ một dòng (HCN-07).
-- Vai r_extension: sổ SELECT, INSERT; key SELECT, INSERT; counter SELECT, INSERT, UPDATE; checkin SELECT, INSERT, DELETE.

CREATE TABLE pf_extension.cnts_livestock_household (
  id                  uuid          NOT NULL,             -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'lhh:<household_ref>'
  seq                 bigint        NOT NULL,
  household_ref       uuid          NOT NULL,             -- khóa định danh bền của hộ (ADR-0056 mục 2)
  code                text          NOT NULL CHECK (code ~ '^HCN[0-9]{7}$'),                       -- HCN-01
  event               text          NOT NULL CHECK (event IN ('created', 'created_pending', 'updated', 'checked_in',
                                      'relocated', 'deactivated')),
  status              text          NOT NULL CHECK (status IN ('active', 'pending_location', 'deactivated')), -- HCN-08
  commune_code        text          NOT NULL CHECK (commune_code ~ '^[A-Za-z0-9._-]{1,20}$'),      -- HCN-04 xã
  hamlet              text          NULL CHECK (hamlet IS NULL OR char_length(hamlet) BETWEEN 1 AND 200), -- HCN-04 ấp
  person_ref          uuid          NOT NULL,             -- HCN-02/03 ở kho định danh
  livestock_type      text          NOT NULL CHECK (livestock_type IN ('nong_ho', 'trang_trai_nho', 'trang_trai_vua',
                                      'trang_trai_cong_nghiep')),                                    -- HCN-06
  species_codes       text[]        NOT NULL CHECK (cardinality(species_codes) BETWEEN 1 AND 30)     -- HCN-05
                        CHECK (array_position(species_codes, NULL) IS NULL),
  head_counts         bigint[]      NOT NULL CHECK (array_position(head_counts, NULL) IS NULL),
  checkin_ref         uuid          NULL,                 -- HCN-07: lần check-in hiện hành
  changed_fields      text[]        NOT NULL DEFAULT '{}' CHECK (array_position(changed_fields, NULL) IS NULL), -- HCN-09
  pii_revision_ref    uuid          NULL,                 -- HCN-09: bản sửa họ tên/SĐT ở kho định danh
  reason              text          NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 1000),
  origin_outbreak_ref uuid          NULL,                 -- BR-08: ổ dịch đã tạo hộ (chỉ created_pending)
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  on_behalf_of        uuid          NULL,                 -- chế độ hỗ trợ của Admin
  support_reason      text          NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_livestock_household_stream CHECK (stream_key = 'lhh:' || household_ref::text),
  CONSTRAINT cnts_livestock_household_counts CHECK (cardinality(head_counts) = cardinality(species_codes)
    AND 0 <= ALL (head_counts)),
  CONSTRAINT cnts_livestock_household_located CHECK (status <> 'active' OR checkin_ref IS NOT NULL),
  CONSTRAINT cnts_livestock_household_pending CHECK (status <> 'pending_location' OR checkin_ref IS NULL),
  CONSTRAINT cnts_livestock_household_status CHECK (
    (event IN ('created', 'checked_in', 'relocated') AND status = 'active')
    OR (event = 'created_pending' AND status = 'pending_location')
    OR (event = 'updated' AND status IN ('active', 'pending_location'))
    OR (event = 'deactivated' AND status = 'deactivated')),
  CONSTRAINT cnts_livestock_household_reason CHECK (event NOT IN ('relocated', 'deactivated') OR reason IS NOT NULL),
  CONSTRAINT cnts_livestock_household_origin CHECK (origin_outbreak_ref IS NULL OR event = 'created_pending')
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_livestock_household_ref ON pf_extension.cnts_livestock_household (tenant_id, household_ref, seq);
CREATE INDEX cnts_livestock_household_commune ON pf_extension.cnts_livestock_household (tenant_id, commune_code);
CREATE INDEX cnts_livestock_household_idem ON pf_extension.cnts_livestock_household (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_extension.cnts_livestock_household', 'pf_extension.cnts_livestock_household');

CREATE TABLE pf_extension.cnts_livestock_household_key (
  tenant_id           uuid          NOT NULL,
  household_ref       uuid          NOT NULL,
  code                text          NOT NULL CHECK (code ~ '^HCN[0-9]{7}$'),
  created_at          timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, household_ref),
  UNIQUE (tenant_id, code)
);
SELECT shared.apply_tenant_rls('pf_extension.cnts_livestock_household_key');

CREATE TABLE pf_extension.cnts_livestock_household_counter (
  tenant_id           uuid          NOT NULL PRIMARY KEY,
  last                bigint        NOT NULL CHECK (last BETWEEN 1 AND 9999999)
);
SELECT shared.apply_tenant_rls('pf_extension.cnts_livestock_household_counter');

CREATE TABLE pf_extension.cnts_livestock_household_checkin (
  id                  uuid          NOT NULL PRIMARY KEY, -- checkin_ref (UUIDv7)
  tenant_id           uuid          NOT NULL,
  household_ref       uuid          NOT NULL,
  location            geometry(Point, 4326) NOT NULL,
  gps_accuracy_m      numeric(10,2) NOT NULL CHECK (gps_accuracy_m >= 0),
  mocked              boolean       NOT NULL,             -- vị trí giả lập (đánh dấu, cảnh báo)
  captured_at         timestamptz   NOT NULL,             -- thời điểm bắt GPS trên máy
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  reason              text          NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 1000) -- H4 đổi vị trí
);
CREATE INDEX cnts_livestock_household_checkin_ref ON pf_extension.cnts_livestock_household_checkin (tenant_id, household_ref);
SELECT shared.apply_tenant_rls('pf_extension.cnts_livestock_household_checkin');

REVOKE ALL ON pf_extension.cnts_livestock_household, pf_extension.cnts_livestock_household_key,
  pf_extension.cnts_livestock_household_counter, pf_extension.cnts_livestock_household_checkin FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_livestock_household, pf_extension.cnts_livestock_household_key TO r_extension;
GRANT SELECT, INSERT, UPDATE ON pf_extension.cnts_livestock_household_counter TO r_extension;
GRANT SELECT, INSERT, DELETE ON pf_extension.cnts_livestock_household_checkin TO r_extension;
