-- KN-STANDALONE-7-KHTC: BRD KHTC v2.3 không có nguồn số liệu ngoài.
-- Giữ nguyên bảng, RLS, sổ băm, metadata minh chứng và mọi payload/file lịch sử.
-- Không sửa dữ liệu hay thu quyền đọc. Không còn writer/route nhập mới.
SET LOCAL lock_timeout = '5s';
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON
  pf_extension.khtc_ext_indicator,
  pf_extension.khtc_ext_evidence,
  pf_extension.khtc_ext_figure,
  pf_extension.khtc_ext_import
FROM r_extension;
