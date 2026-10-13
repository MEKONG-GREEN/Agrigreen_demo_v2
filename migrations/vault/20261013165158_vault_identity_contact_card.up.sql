-- vault/identity — W11.23 (Contact FN-13, BRD HCTH v3.0; pf/contact `cap/directory`): THẺ DANH BẠ.
--
-- 1. vault.section thêm ngăn 'contact.card' (email, năm sinh của liên hệ ngoài tỉnh — mục đích contact.directory, khóa dữ
--    liệu riêng của ngăn như W11.22). Họ tên, SĐT của liên hệ ngoài tỉnh ở vault.identity như mọi chủ thể người.
-- 2. vault.access_log.op thêm 'card' (đọc thẻ danh bạ theo lô — mỗi chủ thể một dòng sổ). Sổ có chuỗi băm: chỉ thay ràng
--    buộc CHECK (tập cũ là tập con; CHECK mới access_log_op_v3), KHÔNG thêm cột (bài học W4.1).
-- Mẫu khóa nhẹ (pf 000143): thêm CHECK mới NOT VALID → VALIDATE → bỏ CHECK cũ. Dòng cũ đều thỏa CHECK mới.

SET LOCAL lock_timeout = '5s';

ALTER TABLE vault.section ADD CONSTRAINT section_section_v2
  CHECK (section IN ('hrm.profile', 'hrm.sensitive', 'contact.card')) NOT VALID;
ALTER TABLE vault.section VALIDATE CONSTRAINT section_section_v2;
ALTER TABLE vault.section DROP CONSTRAINT section_section_check;

ALTER TABLE vault.access_log ADD CONSTRAINT access_log_op_v3
  CHECK (op IN ('create', 'display', 'resolve', 'lookup', 'unwrap', 'erase', 'amend', 'section_write', 'section_read',
                'card')) NOT VALID;
ALTER TABLE vault.access_log VALIDATE CONSTRAINT access_log_op_v3;
ALTER TABLE vault.access_log DROP CONSTRAINT access_log_op_v2;
