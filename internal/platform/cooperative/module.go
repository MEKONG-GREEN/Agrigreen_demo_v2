// Package cooperative là module pf/cooperative (dom/cooperative): hồ sơ HTX, nông hộ và xã viên,
// thửa, vụ, nhật ký canh tác, khai báo sản lượng (docs/design/cooperative.md, ADR-0027, ADR-0030).
//
// W3.2: module là MẶT TIỀN không có DB — các cổng của cooperative/port đọc qua store.Legacy mà gốc
// lắp ráp dựng từ port cũ của pf/party, pf/geo, pf/cropcycle (cmd/internal/coopwire). Bên tiêu thụ
// ở tầng domain dùng cooperativeport thay port cũ. W3.3 chuyển bảng sang schema pf_coop (vai r_coop),
// chuyển store/handler/consumer/sync vào module và xóa port cũ.
//
// Module khác chỉ được import cooperative/port (R-02). Tệp này nối các lớp cho các gốc lắp ráp.
package cooperative

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/store"
)

// Name là tên repo của module (module.yaml).
const Name = "pf/cooperative"

// Sources là nguồn cũ (store tạm W3.2); trường nil = chưa nối → port.ErrUnavailable.
type Sources = store.Legacy

// Module là pf/cooperative đã nối.
type Module struct{ svc *app.Service }

// New tạo module trên các nguồn cũ. TODO(W3.3): nhận pool vai r_coop (modreg.DBRole ghi đè
// pf/cooperative → r_coop, dbadmin.groupOnlyModules) và thay store.Legacy bằng store SQL pf_coop.
func New(src Sources) *Module { return &Module{svc: &app.Service{Store: src}} }

// UseCooperatives nối nguồn hồ sơ HTX (gốc lắp ráp dựng sau pf/party, pf/geo).
func (m *Module) UseCooperatives(c port.Cooperatives) { m.svc.Store.Cooperatives = c }

// UseHouseholds nối nguồn nông hộ.
func (m *Module) UseHouseholds(h port.Households) { m.svc.Store.Households = h }

// UsePlots nối nguồn thửa.
func (m *Module) UsePlots(p port.Plots) { m.svc.Store.Plots = p }

// UseSeasons nối nguồn vụ (gốc lắp ráp dựng sau pf/cropcycle — cropcycle lại cần Plots() của module này).
func (m *Module) UseSeasons(s port.Seasons) { m.svc.Store.Seasons = s }

// UseDiary nối nguồn nhật ký canh tác.
func (m *Module) UseDiary(d port.Diary) { m.svc.Store.Diary = d }

// Cooperatives trả cổng hồ sơ HTX.
func (m *Module) Cooperatives() port.Cooperatives { return m.svc }

// Households trả cổng nông hộ.
func (m *Module) Households() port.Households { return m.svc }

// Plots trả cổng thửa.
func (m *Module) Plots() port.Plots { return m.svc }

// Seasons trả cổng vụ.
func (m *Module) Seasons() port.Seasons { return m.svc }

// Diary trả cổng nhật ký canh tác.
func (m *Module) Diary() port.Diary { return app.Diary{Svc: m.svc} }

// Resolve trả cổng phân giải chủ thể (R-DB-07, cap/evidence W6.5).
func (m *Module) Resolve() port.Resolve { return m.svc }
