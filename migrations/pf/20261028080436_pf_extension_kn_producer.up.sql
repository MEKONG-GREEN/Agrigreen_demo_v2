-- pf/extension (`dom/extension`) — KNSPLIT-2-KN-RECORDS (#67): HỒ SƠ KN RIÊNG cho HTX, Nông hộ, Tổ hợp tác mà cán bộ
-- khuyến nông theo dõi trên các xã phụ trách (TT-08, TT-09). Thay đường ghi legacy /v1/kn/cooperatives (pf/party
-- CreateOrg OrgCooperative, CoopProfiles.UpdateProfile) và /v1/kn/cooperatives/{ref}/households (pf/cooperative): KN
-- KHÔNG tạo/sửa HTX nền tảng, KHÔNG ghi bảng HTX (pf_party, pf_coop, pf_mechanization). Hồ sơ là dữ liệu của KN.
--
-- kn_producer — sổ chỉ ghi thêm (shared.apply_ledger: DB tính seq, prev_hash, row_hash; chặn UPDATE/DELETE; RLS
-- tenant), chuỗi 'kn_producer:<producer_ref>'. Bản hiện hành = dòng có seq lớn nhất của chuỗi; không bảng chiếu riêng.
-- Sự việc: created, updated, deactivated (vô hiệu hóa thay xóa), reactivated.
--   kind: cooperative (HTX) | household (Nông hộ) | coop_group (Tổ hợp tác) — không đổi sau khi tạo.
--   name: tên HTX/Tổ hợp tác; NULL với Nông hộ (tên chủ hộ là dữ liệu cá nhân, ở kho định danh qua person_ref).
--   person_ref: người đại diện (HTX, THT) hoặc chủ hộ — họ tên, SĐT ở vault.identity, mục đích extension.contact.
--   parent_ref: Nông hộ thuộc HTX/THT nào (mã hồ sơ KN cùng sổ, không phải mã HTX nền tảng).
--   htx_ref: liên kết TÙY CHỌN tới HTX đã công bố (pf_party_pub.v1_org_tree), chỉ để đối chiếu; KN không ghi gì về HTX đó.
-- Không dữ liệu cá nhân ngoài person_ref, actor_ref (mã tham chiếu). Vai r_extension: SELECT, INSERT.
SET LOCAL lock_timeout = '5s';

CREATE TABLE pf_extension.kn_producer (
  id               uuid          NOT NULL,                 -- UUIDv7 (R-11)
  tenant_id        uuid          NOT NULL,
  stream_key       text          NOT NULL,                 -- 'kn_producer:<producer_ref>'
  seq              bigint        NOT NULL,
  producer_ref     uuid          NOT NULL,                 -- mã hồ sơ KN
  kind             text          NOT NULL CHECK (kind IN ('cooperative', 'household', 'coop_group')),
  event            text          NOT NULL CHECK (event IN ('created', 'updated', 'deactivated', 'reactivated')),
  commune_code     text          NOT NULL CHECK (char_length(commune_code) BETWEEN 1 AND 20),
  name             text          NULL CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 200),
  person_ref       uuid          NOT NULL,                 -- vault.identity: người đại diện / chủ hộ
  parent_ref       uuid          NULL,                     -- Nông hộ → hồ sơ KN HTX/THT
  htx_ref          uuid          NULL,                     -- HTX đã công bố (chỉ đối chiếu)
  area_ha          numeric(14,4) NULL CHECK (area_ha IS NULL OR (area_ha >= 0 AND area_ha <= 1000000)),
  tax_code         text          NULL CHECK (tax_code IS NULL OR tax_code ~ '^[0-9]{10}(-[0-9]{3})?$'),
  address          text          NULL CHECK (address IS NULL OR char_length(address) BETWEEN 1 AND 300),
  status_reason    text          NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
  event_time       timestamptz   NOT NULL,
  recorded_at      timestamptz   NOT NULL DEFAULT now(),
  actor_ref        uuid          NOT NULL,
  on_behalf_of     uuid          NULL,                     -- chế độ hỗ trợ của Admin
  support_reason   text          NULL,
  idem_key         text          NOT NULL,
  rule_version     text          NOT NULL,
  prev_hash        bytea         NOT NULL,
  row_hash         bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT kn_producer_stream CHECK (stream_key = 'kn_producer:' || producer_ref::text),
  CONSTRAINT kn_producer_name CHECK ((kind = 'household') = (name IS NULL)),
  CONSTRAINT kn_producer_parent CHECK (kind = 'household' OR parent_ref IS NULL),
  CONSTRAINT kn_producer_htx CHECK (kind = 'cooperative' OR htx_ref IS NULL),
  CONSTRAINT kn_producer_org_only CHECK (kind <> 'household' OR (tax_code IS NULL AND address IS NULL))
) PARTITION BY RANGE (recorded_at);
CREATE INDEX kn_producer_ref ON pf_extension.kn_producer (tenant_id, producer_ref, seq DESC);
CREATE INDEX kn_producer_kind ON pf_extension.kn_producer (tenant_id, kind, producer_ref);
CREATE INDEX kn_producer_idem ON pf_extension.kn_producer (tenant_id, idem_key);
SELECT shared.apply_ledger('pf_extension.kn_producer', 'pf_extension.kn_producer');

REVOKE ALL ON pf_extension.kn_producer FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.kn_producer TO r_extension;
