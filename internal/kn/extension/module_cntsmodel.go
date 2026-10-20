package extension

import "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"

// UseCNTSModelVault (W11.47) nối kho định danh cho hộ mô hình CNTS: họ tên, SĐT chủ hộ ghi vào kho (mục đích
// extension.contact), danh sách hiển thị nhãn che bớt (extension.field_support). Sổ KN chỉ giữ person_ref (ADR-0055,
// Q-KNS-04: KN không ghi dữ liệu HTX; dữ liệu cá nhân ở vault). Chưa nối → tạo hộ bằng họ tên trả 503.
func (m *Module) UseCNTSModelVault(v client.Client) { m.model.Vault = v }
