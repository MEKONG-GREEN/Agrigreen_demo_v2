package extension

import (
	"context"
	"time"

	"github.com/google/uuid"

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

// bm11Herds (CNTS-V4-BM11-TYPES #253, FN-16 BR-07) là nguồn bảng "hộ, trang trại theo loại chăn nuôi" của Báo cáo phòng BM-11:
// đếm hộ Đang dùng (bản hiện hành) của danh sách hộ chăn nuôi FN-10 theo xã × loại, cộng số con theo vật nuôi × loại.
func (m *Module) bm11Herds(ctx context.Context, tid uuid.UUID, communes []string) (rcdomain.BM11HerdInput, error) {
	t, err := m.livestock.ActiveTally(ctx, tid, communes)
	if err != nil {
		return rcdomain.BM11HerdInput{}, err
	}
	out := rcdomain.BM11HerdInput{Counts: make([]rcdomain.BM11HerdCount, 0, len(t.Types)),
		Heads: make([]rcdomain.BM11HeadCount, 0, len(t.Heads)), Species: make(map[string]rcdomain.BM11Species, len(t.Species))}
	for _, c := range t.Types {
		out.Counts = append(out.Counts, rcdomain.BM11HerdCount{Commune: c.Commune, Type: c.Type, Households: c.Households})
	}
	for _, h := range t.Heads {
		out.Heads = append(out.Heads, rcdomain.BM11HeadCount{Species: h.Species, Type: h.Type, Heads: h.Heads})
	}
	for code, sp := range t.Species {
		out.Species[code] = rcdomain.BM11Species{Name: sp.Name, Order: sp.Order}
	}
	return out, nil
}
