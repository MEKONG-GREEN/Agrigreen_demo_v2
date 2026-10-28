package extension

import (
	"context"
	"time"

	modelapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/app"
	rcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
)

// bm11Programs (W11.45) là nguồn "kết quả chương trình, dự án" của Báo cáo phòng BM-11: CT/DA của tỉnh (W11.47, theo phạm vi
// của người gọi) còn hiệu lực trong [from, to] — bắt đầu trước ngày cuối kỳ và chưa kết thúc trước ngày đầu kỳ — kèm số điểm
// trình diễn, số hộ đang tham gia. Thứ tự: CT tỉnh, KNQG, hợp tác; trong nhóm theo mã.
func (m *Module) bm11Programs(ctx context.Context, province string, from, to time.Time) ([]rcdomain.BM11Program, error) {
	ps, _, _, err := m.model.ListProjects(ctx, modelapp.ProjectQuery{})
	if err != nil {
		return nil, err
	}
	order := map[string]int{"tinh": 0, "knqg": 1, "hop_tac": 2}
	out := []rcdomain.BM11Program{}
	for _, k := range []string{"tinh", "knqg", "hop_tac", ""} {
		for _, p := range ps {
			if _, known := order[p.Kind]; (k == "" && known) || (k != "" && p.Kind != k) {
				continue
			}
			if p.Province != province || p.Start.After(to) || (p.End != nil && p.End.Before(from)) {
				continue
			}
			out = append(out, rcdomain.BM11Program{Ref: p.Ref.String(), Code: p.Code, Name: p.Name, Kind: p.Kind,
				ManagerRef: p.ManagerRef.String(), Sites: p.SiteCount, Households: p.HouseholdCount})
		}
	}
	return out, nil
}
