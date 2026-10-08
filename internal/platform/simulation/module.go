// Package simulation là module pf/simulation (mục 12, 16): Hub ứng viên, kịch bản
// chọn Hub (sổ + bảng chiếu), việc mô phỏng bất đồng bộ, ảnh chụp đầu vào bất
// biến và kết quả bất biến. Engine tính là optimizer/ (Python 3.12) — Go không
// tính mô phỏng.
//
// Luồng: POST /v1/simulations (202) → worker Go dựng ảnh chụp qua SupplySource,
// Router, ParamSet → pf.simulation.job.requested → optimizer GET ảnh chụp và POST
// kết quả qua Internal API (mTLS) → pf.simulation.job.completed.
//
// Module khác chỉ được import simulation/port (R-02).
package simulation

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "simulation_app"

// MaxInternalBodyBytes là giới hạn thân request của Internal API (phong bì kết
// quả, handler.MaxResultBytes) — gốc lắp ráp dùng để dựng engine của listener nội bộ.
const MaxInternalBodyBytes = handler.MaxResultBytes

// Deps là cổng của module khác. Supply/Router chỉ worker cần.
type Deps struct {
	Params port.ParamSet     // MasterdataParams(md.Params()); nil → giá trị danh mục, không "Chính thức" được
	Supply port.SupplySource // pf/cropcycle + pf/mechanization (chưa có ở đợt 1) — nil → việc hỏng simulation.dependency_unavailable
	Router port.Router       // pf/waterway (+ đường bộ) — nil → Haversine × hệ số #10
	// InternalAuth: kiểm client của Internal API; nil → mTLS với CN/SAN thuộc AG_INTERNAL_CLIENT_NAMES.
	InternalAuth handler.Authorizer
	Log          *slog.Logger
}

// Module là pf/simulation đã nối.
type Module struct {
	svc  *app.Service
	auth handler.Authorizer
}

// New tạo module với pool của vai simulation_app.
func New(db tenant.Beginner, clk clock.Clock, d Deps) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Params: d.Params, Supply: d.Supply, Router: d.Router, Log: d.Log}, auth: d.InternalAuth}
}

// MasterdataParams bọc pf/masterdata thành port.ParamSet.
func MasterdataParams(p params.Params) port.ParamSet { return app.MasterdataParams{P: p} }

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin: module không có route quản trị (tham số #1–#57 quản lý ở pf/masterdata).
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}

// RegisterInternal gắn Internal API cho optimizer vào nhóm /internal/v1 của một
// listener chỉ trong cụm có mTLS (mục 11). Người tích hợp nối vào api-platform.
func (m *Module) RegisterInternal(rg *gin.RouterGroup) {
	handler.Internal{Svc: m.svc, Authorize: m.auth}.Register(rg)
}

// SnapshotHandler là inbox.Handler cho pf.simulation.run.submitted (consumer port.SnapshotConsumer).
func (m *Module) SnapshotHandler() inbox.Handler { return m.svc.HandleRunSubmitted }
