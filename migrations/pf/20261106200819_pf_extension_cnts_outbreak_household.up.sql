SET LOCAL lock_timeout = '5s';

-- pf/extension (`dom/extension`, phòng ban chan_nuoi_thuy_san) — CNTS-V4-OUTBREAK-MULTI (#252), ADR-0056 mục 7–8, BRD CNTS
-- v4.0 FN-11 (BR-03, AC-04, AC-05), UR-XA-10 (bảng trường DB-04, DB-14…DB-16).
--
-- 1. cnts_outbreak_household — bảng con chỉ ghi thêm: mỗi phiên bản ổ dịch v2 (rule_version kn.cnts.outbreak/v2) ghi ĐỦ
--    tập hộ bị ảnh hưởng của phiên bản đó (DB-04), cùng giao dịch với dòng cnts_outbreak. livestock_type (DB-14) và
--    household_seq chụp theo phiên bản hộ lúc ghi: hộ đổi loại / bị vô hiệu hóa sau đó không đổi báo cáo cũ (FN-10 BR-06).
--    Hộ là household_ref của danh sách hộ chăn nuôi (cnts_livestock_household, #251). Vai r_extension: SELECT, INSERT.
-- 2. cnts_outbreak thêm ba cột cho phép NULL (ADR-0012 "cột thêm vào sổ", đăng ký register_ledger_columns):
--    affected_households (DB-15 = số dòng bảng con của phiên bản, dịch vụ đếm), aqua_farmed_ha / aqua_wild_ha (DB-16 diện
--    tích thủy sản bị ảnh hưởng: nuôi trồng, khai thác; ≥ 0, 2 chữ số lẻ). Dòng v1 (dữ liệu cũ một hộ) giữ NULL — không
--    chuyển đổi, không UPDATE.
-- 3. Ràng buộc DB-04 theo phiên bản quy tắc: v1 cần household_ref hoặc locality (như cũ); v2 cần affected_households,
--    household_ref NULL. Ràng buộc cũ cnts_outbreak_household được thay bằng cnts_outbreak_db04.

CREATE TABLE pf_extension.cnts_outbreak_household (
  tenant_id           uuid          NOT NULL,
  outbreak_ref        uuid          NOT NULL,
  outbreak_seq        bigint        NOT NULL CHECK (outbreak_seq >= 1),
  household_ref       uuid          NOT NULL,             -- cnts_livestock_household.household_ref
  household_seq       bigint        NOT NULL CHECK (household_seq >= 1), -- phiên bản hộ đã chụp
  livestock_type      text          NOT NULL CHECK (livestock_type IN ('nong_ho', 'trang_trai_nho', 'trang_trai_vua',
                                      'trang_trai_cong_nghiep')),                                    -- DB-14
  position            smallint      NOT NULL CHECK (position BETWEEN 1 AND 200),
  recorded_at         timestamptz   NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, outbreak_ref, outbreak_seq, household_ref),
  UNIQUE (tenant_id, outbreak_ref, outbreak_seq, position)
);
CREATE INDEX cnts_outbreak_household_ref ON pf_extension.cnts_outbreak_household (tenant_id, household_ref);
SELECT shared.apply_tenant_rls('pf_extension.cnts_outbreak_household');
REVOKE ALL ON pf_extension.cnts_outbreak_household FROM PUBLIC;
GRANT SELECT, INSERT ON pf_extension.cnts_outbreak_household TO r_extension;

ALTER TABLE pf_extension.cnts_outbreak
  ADD COLUMN affected_households integer NULL,
  ADD COLUMN aqua_farmed_ha numeric(12,2) NULL,
  ADD COLUMN aqua_wild_ha numeric(12,2) NULL;
-- ADR-0012: cột thêm vào sổ đăng ký để verify_chain xác minh được các dòng ghi trước migration này.
SELECT shared.register_ledger_columns('pf_extension.cnts_outbreak',
  ARRAY['affected_households', 'aqua_farmed_ha', 'aqua_wild_ha']);

ALTER TABLE pf_extension.cnts_outbreak
  ADD CONSTRAINT cnts_outbreak_db15 CHECK (affected_households IS NULL OR affected_households BETWEEN 1 AND 200) NOT VALID;
ALTER TABLE pf_extension.cnts_outbreak VALIDATE CONSTRAINT cnts_outbreak_db15;

ALTER TABLE pf_extension.cnts_outbreak
  ADD CONSTRAINT cnts_outbreak_db16 CHECK ((aqua_farmed_ha IS NULL OR aqua_farmed_ha >= 0)
    AND (aqua_wild_ha IS NULL OR aqua_wild_ha >= 0)) NOT VALID;
ALTER TABLE pf_extension.cnts_outbreak VALIDATE CONSTRAINT cnts_outbreak_db16;

ALTER TABLE pf_extension.cnts_outbreak
  ADD CONSTRAINT cnts_outbreak_db04 CHECK (
    (rule_version <> 'kn.cnts.outbreak/v2' AND (household_ref IS NOT NULL OR locality IS NOT NULL))
    OR (rule_version = 'kn.cnts.outbreak/v2' AND household_ref IS NULL AND affected_households IS NOT NULL)) NOT VALID;
ALTER TABLE pf_extension.cnts_outbreak VALIDATE CONSTRAINT cnts_outbreak_db04;

ALTER TABLE pf_extension.cnts_outbreak DROP CONSTRAINT cnts_outbreak_household;
