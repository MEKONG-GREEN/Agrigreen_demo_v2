package extension

import "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"

// UseCNTSModelVault (W11.47) nối kho định danh cho hộ mô hình CNTS: họ tên, SĐT chủ hộ ghi vào kho (mục đích
// extension.contact), danh sách hiển thị nhãn che bớt (extension.field_support). Sổ KN chỉ giữ person_ref (ADR-0055,
// Q-KNS-04: KN không ghi dữ liệu HTX; dữ liệu cá nhân ở vault). Chưa nối → tạo hộ bằng họ tên trả 503.
// BF-CN-VACC (#168): cùng kho cho nhãn chủ hộ (che bớt) ở danh sách lịch tiêm, bảng và tệp BM-07.
// CNTS-V4-HOUSEHOLD-LIST (#251): cùng kho cho chủ hộ chăn nuôi (họ tên, SĐT, bản sửa — ADR-0056).
// CNTS-V4-OUTBREAK-MULTI (#252): cùng kho cho hộ nhập mới tại BM-10 DB-04 (FN-10 BR-08) và nhãn chủ hộ ở ổ dịch.
func (m *Module) UseCNTSModelVault(v client.Client) {
	m.model.Vault, m.cal.Vault, m.livestock.Vault, m.ob.Vault = v, v, v, v
}
