-- pf_party: bổ sung mô hình đọc cho web vận hành (web-backend-gaps G-03, G-04, G-08, G-02).
--
-- Chỉ thêm chỉ mục, không đổi bảng: đếm hộ thành viên đang mở theo HTX (G-03),
-- đếm HTX theo đơn vị hành chính (G-02), lọc cây tổ chức theo loại/đơn vị (G-08).
-- Trạng thái đồng ý (G-04) đọc từ consent_current qua khóa chính (tenant, subject, purpose).

CREATE INDEX household_membership_org_open ON pf_party.household_membership (tenant_id, org_ref)
  WHERE until_date IS NULL;
CREATE INDEX org_kind_unit ON pf_party.org (tenant_id, kind, admin_unit_code);
