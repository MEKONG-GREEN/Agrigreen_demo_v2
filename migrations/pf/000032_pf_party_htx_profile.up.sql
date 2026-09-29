-- pf/party: hồ sơ HTX có địa chỉ trụ sở (cột head_office) (HTX US-HTX-02: cột bắt buộc "Địa chỉ"
-- của tệp nhập HTX hàng loạt; US-HTX-03: bản sao giữ địa bàn của hồ sơ nguồn).
--
-- Địa chỉ trụ sở của TỔ CHỨC không phải dữ liệu cá nhân; người đại diện và SĐT
-- vẫn chỉ nằm ở kho định danh (representative_ref, R-04).
ALTER TABLE pf_party.cooperative ADD COLUMN head_office text NULL CHECK (char_length(head_office) BETWEEN 1 AND 500);

-- Tra trùng tên HTX không phân biệt hoa thường ("Tên HTX này đã tồn tại", US-HTX-03 AC-3).
-- Không đặt UNIQUE: dữ liệu G1 có thể đã có tên trùng; ứng dụng kiểm dưới khóa tư vấn.
CREATE INDEX org_cooperative_name ON pf_party.org (tenant_id, lower(name)) WHERE kind = 'cooperative';
