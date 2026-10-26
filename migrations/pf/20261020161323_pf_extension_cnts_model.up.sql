-- pf/extension (`dom/extension`, phòng ban chan_nuoi_thuy_san) — W11.47, D4 CN7, BRD CNTS v3.7 FN-10 (phần hộ), FN-14,
-- FN-19 (phần CT/DA), UR-XA-04, UR-TINH-09, UR-TINH-19:
-- CHƯƠNG TRÌNH/DỰ ÁN, ĐIỂM TRÌNH DIỄN, HỘ MÔ HÌNH, CẤP QUYỀN CÁN BỘ XÃ/NÔNG HỘ.
--
-- ADR-0055 + quyết định PO 14/10/2026 (Q-KNS-04): KN KHÔNG ghi dữ liệu HTX. Mọi bảng dưới đây là bản ghi RIÊNG của KN
-- trong pf_extension; HTX chỉ được tham chiếu bằng UUID (cooperative_ref, đọc qua pf_cooperative_pub v1). Dữ liệu cá nhân
-- của hộ (họ tên, số điện thoại) ở kho định danh (vault) — sổ chỉ giữ person_ref. Tọa độ điểm/hộ không nằm trong sổ
-- không xóa được (QĐ-8): ở bảng thường cnts_model_point (sửa/xóa được, xóa khi rút hộ hoặc khi có yêu cầu xóa).
--
-- 1. cnts_project — sổ chỉ ghi thêm (shared.apply_ledger), chuỗi 'cproj:<project_ref>': mỗi dòng là MỘT phiên bản đủ trường
--    của CT/DA (tên, căn cứ, loại, quy mô, thời gian, chủ nhiệm, kinh phí, tập huấn, giống/vật tư, hồ sơ). program_ref
--    tham chiếu danh sách CT/DA chủ của KHTC (D20, Q-061 giả định: KHTC là chủ danh sách, CNTS gắn điểm/hộ). Sự việc:
--    created, updated, closed (đã nghiệm thu/kết thúc), reopened.
-- 2. cnts_demo_site — sổ, chuỗi 'csite:<site_ref>': điểm trình diễn/điểm nuôi của dự án (xã, loại mô hình, quy mô, cán bộ
--    theo dõi, mẫu nhật ký BM-08/09/19, HTX tham gia nếu có). Sự việc: created, updated, closed, reopened.
-- 3. cnts_model_household — sổ, chuỗi 'chh:<household_ref>': hộ mô hình thuộc một điểm (person_ref ở vault, quy mô, người
--    ghi nhật ký: cán bộ hoặc hộ tự nhập). household_ref là mã KN (không phải mã hộ của HTX). Sự việc: created, updated,
--    withdrawn (rút khỏi mô hình), reinstated.
-- 4. cnts_grant — sổ, chuỗi 'cgrant:<grant_ref>': CẤP QUYỀN trong module CNTS (FN-14) cho tài khoản Cấp Xã/Nông hộ: vai
--    trong module, địa bàn (xã, cơ sở giết mổ, điểm, hộ), biểu mẫu được giao, hiệu lực; thu hồi có lý do và người nhận
--    việc. Mọi thay đổi quyền là một dòng sổ (BR-03 "mọi thay đổi quyền có log").
-- 5. cnts_model_point — BẢNG THƯỜNG: tọa độ của điểm trình diễn / hộ mô hình (một dòng mỗi đối tượng), sửa/xóa được.
-- Vai r_extension: sổ SELECT, INSERT; bảng tọa độ SELECT, INSERT, UPDATE, DELETE.

CREATE TABLE pf_extension.cnts_project (
  id                  uuid          NOT NULL,             -- UUIDv7 (R-11) — mã của PHIÊN BẢN
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'cproj:<project_ref>'
  seq                 bigint        NOT NULL,             -- = số phiên bản
  project_ref         uuid          NOT NULL,             -- định danh bền của CT/DA
  event               text          NOT NULL CHECK (event IN ('created', 'updated', 'closed', 'reopened')),
  province_code       text          NOT NULL CHECK (province_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  code                text          NOT NULL CHECK (code ~ '^[A-Za-z0-9._/-]{1,40}$'),
  name                text          NOT NULL CHECK (char_length(name) BETWEEN 1 AND 300),
  kind                text          NOT NULL CHECK (kind IN ('tinh', 'knqg', 'hop_tac')),
  basis               text          NULL CHECK (basis IS NULL OR char_length(basis) <= 1000),          -- căn cứ
  scale_note          text          NULL CHECK (scale_note IS NULL OR char_length(scale_note) <= 1000), -- quy mô
  start_date          date          NOT NULL,
  end_date            date          NULL,
  budget              numeric(18,0) NULL CHECK (budget IS NULL OR budget >= 0),                      -- đồng
  manager_ref         uuid          NOT NULL,             -- chủ nhiệm (tài khoản)
  program_ref         uuid          NULL,                 -- CT/DA chủ của KHTC (pf_extension KHTC, D20)
  training_note       text          NULL CHECK (training_note IS NULL OR char_length(training_note) <= 2000),
  supplies_note       text          NULL CHECK (supplies_note IS NULL OR char_length(supplies_note) <= 2000),
  attachment_refs     uuid[]        NOT NULL DEFAULT '{}' CHECK (cardinality(attachment_refs) <= 20)  -- hồ sơ dự án
                        CHECK (array_position(attachment_refs, NULL) IS NULL),
  status_reason       text          NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
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
  CONSTRAINT cnts_project_stream CHECK (stream_key = 'cproj:' || project_ref::text),
  CONSTRAINT cnts_project_dates CHECK (end_date IS NULL OR end_date >= start_date),
  CONSTRAINT cnts_project_reason CHECK (event NOT IN ('closed', 'reopened') OR status_reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_project_ref ON pf_extension.cnts_project (tenant_id, project_ref, seq);
CREATE INDEX cnts_project_code ON pf_extension.cnts_project (tenant_id, code);
SELECT shared.apply_ledger('pf_extension.cnts_project', 'pf_extension.cnts_project');

CREATE TABLE pf_extension.cnts_demo_site (
  id                  uuid          NOT NULL,
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'csite:<site_ref>'
  seq                 bigint        NOT NULL,
  site_ref            uuid          NOT NULL,
  event               text          NOT NULL CHECK (event IN ('created', 'updated', 'closed', 'reopened')),
  project_ref         uuid          NOT NULL,
  commune_code        text          NOT NULL CHECK (commune_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  name                text          NOT NULL CHECK (char_length(name) BETWEEN 1 AND 300),
  model_type_code     text          NOT NULL CHECK (char_length(model_type_code) BETWEEN 1 AND 64), -- cnts_model_type
  scale               numeric(14,2) NULL CHECK (scale IS NULL OR scale > 0),
  scale_unit          text          NULL CHECK (scale_unit IS NULL OR scale_unit IN ('con', 'm2', 'm3', 'ha', 'long', 'be')),
  officer_ref         uuid          NULL,                 -- cán bộ theo dõi điểm
  diary_form          text          NULL CHECK (diary_form IS NULL OR diary_form IN ('BM-08', 'BM-09', 'BM-19')),
  cooperative_ref     uuid          NULL,                 -- HTX tham gia — chỉ mã, đọc qua pf_cooperative_pub v1
  note                text          NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  status_reason       text          NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  on_behalf_of        uuid          NULL,
  support_reason      text          NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_demo_site_stream CHECK (stream_key = 'csite:' || site_ref::text),
  CONSTRAINT cnts_demo_site_scale CHECK ((scale IS NULL) = (scale_unit IS NULL)),
  CONSTRAINT cnts_demo_site_reason CHECK (event NOT IN ('closed', 'reopened') OR status_reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_demo_site_ref ON pf_extension.cnts_demo_site (tenant_id, site_ref, seq);
CREATE INDEX cnts_demo_site_project ON pf_extension.cnts_demo_site (tenant_id, project_ref);
CREATE INDEX cnts_demo_site_commune ON pf_extension.cnts_demo_site (tenant_id, commune_code);
SELECT shared.apply_ledger('pf_extension.cnts_demo_site', 'pf_extension.cnts_demo_site');

CREATE TABLE pf_extension.cnts_model_household (
  id                  uuid          NOT NULL,
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'chh:<household_ref>'
  seq                 bigint        NOT NULL,
  household_ref       uuid          NOT NULL,             -- mã hộ mô hình của KN
  event               text          NOT NULL CHECK (event IN ('created', 'updated', 'withdrawn', 'reinstated')),
  site_ref            uuid          NOT NULL,
  project_ref         uuid          NOT NULL,             -- của điểm (chép để lọc)
  commune_code        text          NOT NULL CHECK (commune_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  person_ref          uuid          NOT NULL,             -- chủ hộ ở kho định danh (họ tên, SĐT không ở đây)
  scale               numeric(14,2) NULL CHECK (scale IS NULL OR scale > 0),
  scale_unit          text          NULL CHECK (scale_unit IS NULL OR scale_unit IN ('con', 'm2', 'm3', 'ha', 'long', 'be')),
  keeper_kind         text          NOT NULL CHECK (keeper_kind IN ('officer', 'self')),          -- người ghi nhật ký
  keeper_ref          uuid          NULL,                 -- cán bộ ghi nhật ký (keeper_kind = officer)
  self_entry          boolean       NOT NULL,             -- "Hộ tự nhập" (GĐ2, đề nghị cấp quyền nông hộ)
  joined_on           date          NULL,
  note                text          NULL CHECK (note IS NULL OR char_length(note) <= 1000),
  status_reason       text          NULL CHECK (status_reason IS NULL OR char_length(status_reason) BETWEEN 1 AND 1000),
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  on_behalf_of        uuid          NULL,
  support_reason      text          NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_model_household_stream CHECK (stream_key = 'chh:' || household_ref::text),
  CONSTRAINT cnts_model_household_scale CHECK ((scale IS NULL) = (scale_unit IS NULL)),
  CONSTRAINT cnts_model_household_keeper CHECK (keeper_kind = 'self' OR keeper_ref IS NOT NULL),
  CONSTRAINT cnts_model_household_reason CHECK (event NOT IN ('withdrawn', 'reinstated') OR status_reason IS NOT NULL)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_model_household_ref ON pf_extension.cnts_model_household (tenant_id, household_ref, seq);
CREATE INDEX cnts_model_household_site ON pf_extension.cnts_model_household (tenant_id, site_ref);
CREATE INDEX cnts_model_household_commune ON pf_extension.cnts_model_household (tenant_id, commune_code);
SELECT shared.apply_ledger('pf_extension.cnts_model_household', 'pf_extension.cnts_model_household');

CREATE TABLE pf_extension.cnts_grant (
  id                  uuid          NOT NULL,
  tenant_id           uuid          NOT NULL,
  stream_key          text          NOT NULL,             -- 'cgrant:<grant_ref>'
  seq                 bigint        NOT NULL,
  grant_ref           uuid          NOT NULL,
  event               text          NOT NULL CHECK (event IN ('granted', 'updated', 'revoked')),
  grantee_ref         uuid          NOT NULL,             -- tài khoản được cấp (Admin tạo, FN-04)
  role                text          NOT NULL CHECK (role IN ('tang_cuong', 'ksgm', 'theo_doi_mo_hinh', 'nong_ho')),
  province_code       text          NOT NULL CHECK (province_code ~ '^[A-Za-z0-9._-]{1,20}$'),
  communes            text[]        NOT NULL CHECK (cardinality(communes) BETWEEN 1 AND 200)
                        CHECK (array_position(communes, NULL) IS NULL),
  facility_refs       uuid[]        NOT NULL DEFAULT '{}' CHECK (cardinality(facility_refs) <= 200)
                        CHECK (array_position(facility_refs, NULL) IS NULL),
  site_refs           uuid[]        NOT NULL DEFAULT '{}' CHECK (cardinality(site_refs) <= 200)
                        CHECK (array_position(site_refs, NULL) IS NULL),
  household_refs      uuid[]        NOT NULL DEFAULT '{}' CHECK (cardinality(household_refs) <= 200)
                        CHECK (array_position(household_refs, NULL) IS NULL),
  forms               text[]        NOT NULL DEFAULT '{}' CHECK (cardinality(forms) <= 30)
                        CHECK (array_position(forms, NULL) IS NULL),
  valid_from          date          NOT NULL,
  valid_to            date          NULL,
  successor_ref       uuid          NULL,                 -- người nhận việc chưa nộp khi thu hồi (FN-14 luồng phụ)
  successor_grant_ref uuid          NULL,                 -- quyền mới cấp cho người nhận
  reason              text          NULL CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 1000),
  event_time          timestamptz   NOT NULL,
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  on_behalf_of        uuid          NULL,
  support_reason      text          NULL,
  idem_key            text          NOT NULL,
  rule_version        text          NOT NULL,
  prev_hash           bytea         NOT NULL,
  row_hash            bytea         NOT NULL,
  PRIMARY KEY (id, recorded_at),
  CONSTRAINT cnts_grant_stream CHECK (stream_key = 'cgrant:' || grant_ref::text),
  CONSTRAINT cnts_grant_dates CHECK (valid_to IS NULL OR valid_to >= valid_from),
  CONSTRAINT cnts_grant_revoked CHECK (event <> 'revoked' OR reason IS NOT NULL),
  CONSTRAINT cnts_grant_successor CHECK (successor_ref IS NULL OR successor_ref <> grantee_ref)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX cnts_grant_ref ON pf_extension.cnts_grant (tenant_id, grant_ref, seq);
CREATE INDEX cnts_grant_grantee ON pf_extension.cnts_grant (tenant_id, grantee_ref);
SELECT shared.apply_ledger('pf_extension.cnts_grant', 'pf_extension.cnts_grant');

CREATE TABLE pf_extension.cnts_model_point (
  tenant_id           uuid          NOT NULL,
  kind                text          NOT NULL CHECK (kind IN ('site', 'household')),
  ref                 uuid          NOT NULL,             -- site_ref | household_ref
  point               geometry(Point, 4326) NOT NULL,
  source              text          NOT NULL CHECK (source IN ('gps', 'map')),
  updated_at          timestamptz   NOT NULL DEFAULT now(),
  actor_ref           uuid          NOT NULL,
  PRIMARY KEY (tenant_id, kind, ref)
);
SELECT shared.apply_tenant_rls('pf_extension.cnts_model_point');

REVOKE ALL ON pf_extension.cnts_project, pf_extension.cnts_demo_site, pf_extension.cnts_model_household,
  pf_extension.cnts_grant, pf_extension.cnts_model_point FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_project, pf_extension.cnts_demo_site, pf_extension.cnts_model_household,
  pf_extension.cnts_grant TO r_extension;
GRANT SELECT, INSERT, UPDATE, DELETE ON pf_extension.cnts_model_point TO r_extension;
