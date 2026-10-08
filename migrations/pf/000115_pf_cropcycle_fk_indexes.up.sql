-- pf/cropcycle: chỉ mục cho khóa ngoại chưa có chỉ mục dẫn đầu tương ứng (rà soát FK).
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24).
--
-- variety_sop_binding (pf 000020): FK (tenant_id, template_ref) → sop_template; chỉ có PK (tenant_id, id)
-- và UNIQUE một phần (tenant_id, variety_code) — kiểm FK khi sửa/xóa mẫu và tra "giống nào gắn mẫu này"
-- phải quét bảng. Không CONCURRENTLY: mỗi tệp migration chạy trong một giao dịch (golang-migrate);
-- bảng cấu hình nhỏ, khóa SHARE ngắn — lock_timeout để không treo hàng đợi khóa.
SET LOCAL lock_timeout = '5s';

CREATE INDEX IF NOT EXISTS variety_sop_binding_template
  ON pf_cropcycle.variety_sop_binding (tenant_id, template_ref);
