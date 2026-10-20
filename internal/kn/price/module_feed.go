package price

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
)

// PublicBoardFeed (W11.81) là nguồn tập dữ liệu price_board_daily của cổng tích hợp: chỉ bảng giá ngày ĐÃ DUYỆT của bên
// dùng profile (rỗng → bên dùng đầu tiên). Gốc lắp ráp cmd/worker nối vào job dựng feed của pf/apiclient.
func (m *Module) PublicBoardFeed(profile string) port.PublicBoardFeed {
	return app.PublicFeed{T: m.tickets, Profile: profile}
}
