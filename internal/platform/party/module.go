// Package party là module bên tham gia (mục 10.5): đồng ý theo mục đích, cây tổ
// chức TW → Tỉnh → Xã, HTX/THT và nông hộ (ADR-0009).
//
// Module khác chỉ được import party/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform.
package party

import (
	"github.com/gin-gonic/gin"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "party_app"

// Deps là port của module khác mà party cần.
type Deps struct {
	// AdminUnits: đơn vị hành chính có hiệu lực theo thời gian (pf/masterdata) —
	// kiểm mã đơn vị khi tạo/di chuyển tổ chức và tính phạm vi TTKN tỉnh.
	AdminUnits mdport.AdminUnits
	// Vault: kho định danh — tạo chủ hộ/người đại diện, nhãn hiển thị.
	Vault client.Client
}

// Module là pf/party đã nối.
type Module struct {
	svc *app.Service
	reg *app.Registry
}

// New tạo module với pool của vai party_app. deps tùy chọn để giữ tương thích
// với gốc lắp ráp cũ; thiếu deps thì các thao tác cần masterdata/vault trả
// 503 party.dependency_unavailable.
func New(db tenant.Beginner, clk clock.Clock, deps ...Deps) *Module {
	var d Deps
	if len(deps) > 0 {
		d = deps[0]
	}
	return &Module{
		svc: &app.Service{DB: db, Clock: clk},
		reg: &app.Registry{DB: db, Clock: clk, Units: d.AdminUnits, Vault: d.Vault},
	}
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) {
	handler.Handler{Svc: m.svc}.Register(rg)
	handler.Registry{Reg: m.reg}.RegisterApp(rg)
}

// RegisterAdmin gắn route Admin API.
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Registry{Reg: m.reg}.RegisterAdmin(rg) }

// ConsentChecker cho module khác (qua port).
func (m *Module) ConsentChecker() port.ConsentChecker { return m.svc }

// Orgs cho module khác tra cứu tổ chức (qua port).
func (m *Module) Orgs() port.Orgs { return m.reg }

// ScopeResolver cho module khác tính phạm vi theo cây phân cấp (qua port, ADR-0009).
func (m *Module) ScopeResolver() port.ScopeResolver { return m.reg }
