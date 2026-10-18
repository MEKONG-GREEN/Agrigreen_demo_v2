package extension

import (
	"context"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/closedfig"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// ClosedFigures là bộ đọc "số liệu đã chốt" thống nhất của KHTC (ADR-0053, gói PFPUB) trên họ hợp đồng
// closed_figures/v1 (pf_extension_pub + pf_forms_pub), bằng vai r_extension của module.
func (m *Module) ClosedFigures() port.ClosedFigures { return closedfig.Reader{DB: m.rc.DB, HTX: m.htx} }

// KHTCSources là port duy nhất phía KHTC (ADR-0053 §7): số đã chốt của Trồng trọt, CNTS, Báo giá theo từng kỳ KHTC.
func (m *Module) KHTCSources() port.KHTCSources { return closedfig.Reader{DB: m.rc.DB, HTX: m.htx} }

// UseHTXConfirmations nối cổng trạng thái xác nhận HTX của gói D2 (gốc lắp ráp nối cooperative.Module.HTXConfirmations()).
// Chưa nối → dòng HTX trong số đã chốt là "Chưa xác nhận".
func (m *Module) UseHTXConfirmations(h port.HTXConfirmations) { m.htx = h }

// KHTCSourceCrosswalk là bộ quy đổi kỳ nguồn cho Kỳ KHTC (W11.62, ADR-0053 §7): mở kỳ lấy kỳ nguồn quy vào kỳ, "Đủ dữ
// liệu" lấy cờ chốt từng kỳ nguồn. Lịch KHTC theo D8 (CalendarAt của phòng ke_hoach_tai_chinh tại ngày đầu kỳ); cổng
// xác nhận HTX đọc lúc gọi (nối trước hay sau đều được).
func (m *Module) KHTCSourceCrosswalk() port.KHTCSourceCrosswalk { return khtcCrosswalk{m} }

type khtcCrosswalk struct{ m *Module }

func (c khtcCrosswalk) impl() closedfig.KHTCCrosswalk {
	return closedfig.KHTCCrosswalk{R: closedfig.Reader{DB: c.m.rc.DB, HTX: c.m.htx}, RuleTag: func(ctx context.Context, day time.Time) string {
		return c.m.rc.CalendarAt(ctx, partyport.DeptKeHoachTaiChinh, day).Rule.Tag()
	}}
}

func (c khtcCrosswalk) Sources(ctx context.Context, kind, key string, from, to time.Time, departments []string) ([]port.KHTCSourceRef, error) {
	return c.impl().Sources(ctx, kind, key, from, to, departments)
}

func (c khtcCrosswalk) States(ctx context.Context, refs []port.KHTCSourceRef) ([]port.KHTCSourceState, error) {
	return c.impl().States(ctx, refs)
}
