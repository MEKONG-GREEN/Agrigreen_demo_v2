package extension

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/closedfig"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
)

// ClosedFigures là bộ đọc "số liệu đã chốt" thống nhất của KHTC (ADR-0053, gói PFPUB) trên họ hợp đồng
// closed_figures/v1 (pf_extension_pub + pf_forms_pub), bằng vai r_extension của module.
func (m *Module) ClosedFigures() port.ClosedFigures { return closedfig.Reader{DB: m.rc.DB, HTX: m.htx} }

// KHTCSources là port duy nhất phía KHTC (ADR-0053 §7): số đã chốt của Trồng trọt, CNTS, Báo giá theo từng kỳ KHTC.
func (m *Module) KHTCSources() port.KHTCSources { return closedfig.Reader{DB: m.rc.DB, HTX: m.htx} }

// UseHTXConfirmations nối cổng trạng thái xác nhận HTX của gói D2 (gốc lắp ráp: cooperative.Module.HTXConfirmations()).
// Chưa nối → dòng HTX trong số đã chốt là "Chưa xác nhận".
func (m *Module) UseHTXConfirmations(h port.HTXConfirmations) { m.htx = h }
