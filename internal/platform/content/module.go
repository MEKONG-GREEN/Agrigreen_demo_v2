// Package content là module thư viện nội dung (ADR-0011): quy trình kỹ thuật
// chuẩn quốc gia, hướng dẫn đặc thù địa phương, tài liệu, tin tức và cảnh báo
// khẩn cho App Khuyến nông và App HTX.
//
// Module khác chỉ được import content/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform.
package content

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/handler"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "content_app"

// Deps là port của module khác và hạ tầng mà content cần.
type Deps struct {
	// Scope: phạm vi TW → Tỉnh → Xã của người gọi (pf/party, ADR-0009). Bắt buộc.
	Scope partyport.ScopeResolver
	// Catalogs: kiểm nhãn phân loại (giống lúa, khâu, vụ, dịch hại — pf/masterdata).
	Catalogs mdport.Catalogs
	// AdminUnits (tùy chọn): kiểm mã đơn vị của phạm vi áp dụng và tìm tỉnh cha
	// của xã, để cán bộ xã/HTX thấy tài liệu cấp tỉnh.
	AdminUnits mdport.AdminUnits
	// Store: kho đối tượng cho tệp đính kèm. nil → tải tệp trả 503.
	Store objstore.Store
}

// Module là pf/content đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai content_app.
func New(db tenant.Beginner, clk clock.Clock, d Deps) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Scope: d.Scope, Catalogs: d.Catalogs, Units: d.AdminUnits, Store: d.Store}}
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API.
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// SyncSource cho pf/sync: entity "document" chiều xuống (docs/dev/sync-contract.md).
func (m *Module) SyncSource() syncport.Source { return app.Sync{Svc: m.svc} }
