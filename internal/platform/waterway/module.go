// Package waterway là module pf/waterway (mục 16; GIS FN-09, US-WATER-01/02/03):
// sổ đoạn sông/kênh và công trình vượt sông, dựng đồ thị pgRouting CÓ PHIÊN BẢN
// bất đồng bộ (worker), phát hành có tách vai (người phát hành ≠ người dựng),
// tra lớp đường thủy phù hợp tải trọng và định tuyến ghim phiên bản cho
// pf/simulation (port.Router).
//
// Module khác chỉ được import waterway/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform và cmd/worker.
package waterway

import (
	"github.com/gin-gonic/gin"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "waterway_app"

// Deps là phụ thuộc của module.
type Deps struct {
	// Params: ngưỡng dựng, biên an toàn, bán kính tiếp cận, hệ số Haversine (#10) (pf/masterdata).
	// Thiếu tham số → giá trị mặc định trong mã (domain.DefaultParams).
	Params mdport.Params
	// Catalogs: danh mục lớp phương tiện vessel_class (tùy chọn; thiếu → ba lớp mặc định).
	Catalogs mdport.Catalogs
	// Read: pool bản sao đọc (vai waterway_app) cho truy vấn đồ thị (mục 16); nil → pool chính.
	Read tenant.Beginner
}

// Module là pf/waterway đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai waterway_app. Ở worker, db có thể nil — handler
// dựng mạng chỉ dùng giao dịch do inbox.Process mở.
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	return &Module{svc: &app.Service{DB: db, Read: deps.Read, Clock: clk, Params: deps.Params, Catalogs: deps.Catalogs}}
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (nhóm đã bắt buộc MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// Router cho pf/simulation: định tuyến ghim phiên bản mạng (qua port).
func (m *Module) Router() port.Router { return m.svc }

// Layer cho pf/geo: lớp tuyến đường thủy theo khung nhìn của bản đồ chuẩn hóa (GIS US-INT-02).
func (m *Module) Layer() port.Layer { return m.svc }

// BuildHandler là inbox.Handler của worker cho pf.waterway.network_build.requested
// (consumer port.BuildConsumer, pool vai waterway_app).
func (m *Module) BuildHandler() inbox.Handler { return m.svc.HandleBuildRequested }
