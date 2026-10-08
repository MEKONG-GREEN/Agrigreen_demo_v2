// Package store là các nguồn của mặt tiền pf/cooperative (app.Service).
//
// W3.3: dữ liệu ở schema pf_coop; SQL nằm trong store của từng phần (season/store, plot/store, member/store) và chạy
// dưới vai r_cooperative. Sources gom các nguồn đã hiện thực cổng của cooperative/port để mặt tiền thêm phần chung
// (khóa rỗng, nguồn chưa nối → port.ErrUnavailable, Resolve). Thay store.Legacy + bộ chuyển cmd/internal/coopwire
// của W3.2 (nguồn cũ từ port của party/geo/cropcycle).
package store

import (
	"context"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
)

// SubjectResolver phân giải chủ thể không có cổng đọc riêng (vụ, khai báo sản lượng) cho Resolve.
type SubjectResolver interface {
	ResolveSeason(ctx context.Context, ref uuid.UUID) (port.Subject, error)
	ResolveHarvest(ctx context.Context, id uuid.UUID) (port.Subject, error)
}

// Sources là các nguồn của mặt tiền; trường nil = chưa nối (thao tác cần nó trả port.ErrUnavailable).
type Sources struct {
	Cooperatives port.Cooperatives // member (hồ sơ HTX, tâm vùng HTX từ plot)
	Households   port.Households   // member (+ plot: nông hộ có thửa trong xã)
	Plots        port.Plots        // plot
	Seasons      port.Seasons      // season
	Diary        port.Diary        // season (farm_log_current)
	Season       SubjectResolver   // season (Resolve season, harvest)
}
