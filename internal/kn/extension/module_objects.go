package extension

import "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"

// UseObjectStore thay kho ảnh check-in/check-out (kiểm thử tích hợp dùng kho cục bộ).
func (m *Module) UseObjectStore(s objstore.Store) {
	m.svc.Objects = s
}
