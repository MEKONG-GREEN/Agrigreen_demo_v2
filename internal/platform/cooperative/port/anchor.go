package port

import (
	"context"

	"github.com/google/uuid"
)

// Trạng thái xác nhận HTX (D2 — CEO 13/10/2026, ADR-0052). HTX tạm do Khuyến nông/MKG lập vẫn được ghi, dữ liệu vẫn
// vào mọi báo cáo và tính vào tổng mặc định, mang nhãn "Chưa xác nhận" tới khi HTX thật nhận quyền theo mã số thuế.
const (
	HTXConfirmed   = "confirmed"
	HTXUnconfirmed = "unconfirmed"
	// Nhãn hiển thị.
	HTXConfirmedLabel   = "Đã xác nhận"
	HTXUnconfirmedLabel = "Chưa xác nhận"
)

// HTXConfirmations cho đường đọc cấp báo cáo của module khác (cap/forms tổng hợp, báo cáo, danh sách KN): trạng thái
// xác nhận của từng Nút HTX trong tenant của context. Nút không có neo đã nhận quyền → HTXUnconfirmed. Không kiểm vai.
type HTXConfirmations interface {
	Confirmations(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]string, error)
}
