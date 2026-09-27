// Package party là module bên tham gia và đồng ý theo mục đích (mục 10.5).
//
// Module khác chỉ được import party/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform.
package party

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "party_app"

// Module là pf/party đã nối.
type Module struct {
	svc *app.Service
}

// New tạo module với pool của vai party_app.
func New(db tenant.Beginner, clk clock.Clock) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk}}
}

// Register gắn route Application API.
func (m *Module) Register(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.Register(rg) }

// ConsentChecker cho module khác (qua port).
func (m *Module) ConsentChecker() port.ConsentChecker { return m.svc }
