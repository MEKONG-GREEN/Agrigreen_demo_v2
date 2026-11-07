SET LOCAL lock_timeout = '5s';

-- CNTS-CAMPAIGN-SCOPE (#236, BRD CNTS v4.0 FN-21 B1 "phạm vi (toàn bộ xã, Khu vực, danh sách xã, hộ)"; ADR-0056 mục 2, 9):
-- phạm vi đợt điều tra theo HỘ chăn nuôi. Phiên bản nội dung đợt ghi danh sách household_ref (khóa bền của sổ
-- pf_extension.cnts_livestock_household — #251; không khóa ngoại sang sổ phân vùng, ứng dụng kiểm hộ Đang dùng); cột units
-- của phiên bản giữ danh sách xã chứa các hộ (phạm vi Nút). Phát hành: kỳ native đơn vị lá `household` (cha = xã của hộ).
-- Sổ chỉ ghi thêm: chỉ thêm cột và nới CHECK chế độ phạm vi, không sửa dòng cũ.

ALTER TABLE pf_extension.cnts_campaign
  ADD COLUMN households uuid[] NULL;
-- ADR-0012: cột thêm vào sổ đăng ký để verify_chain xác minh được các dòng ghi trước migration này.
SELECT shared.register_ledger_columns('pf_extension.cnts_campaign', ARRAY['households']);

ALTER TABLE pf_extension.cnts_campaign
  DROP CONSTRAINT cnts_campaign_scope_mode_check;
ALTER TABLE pf_extension.cnts_campaign
  ADD CONSTRAINT cnts_campaign_scope_mode CHECK (scope_mode IN ('province', 'regions', 'communes', 'households')) NOT VALID;
ALTER TABLE pf_extension.cnts_campaign VALIDATE CONSTRAINT cnts_campaign_scope_mode;

ALTER TABLE pf_extension.cnts_campaign
  ADD CONSTRAINT cnts_campaign_households CHECK (
    (scope_mode = 'households') = (households IS NOT NULL)
    AND (households IS NULL OR (cardinality(households) BETWEEN 1 AND 2000 AND array_position(households, NULL) IS NULL))
  ) NOT VALID;
ALTER TABLE pf_extension.cnts_campaign VALIDATE CONSTRAINT cnts_campaign_households;
