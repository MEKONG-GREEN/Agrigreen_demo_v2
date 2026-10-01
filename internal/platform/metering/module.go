// Package metering là module pf/metering (G5, ADR-0023 + ADR-0024): sổ đo lường sử dụng Public API là
// chứng từ (mẫu §8, mỗi lời gọi đã xác thực một dòng), bộ đếm hạn mức thẩm quyền cùng giao dịch, ĐIỂM
// KIỂM SOÁT XUẤT DỮ LIỆU duy nhất của vùng nền tảng (mục 18) và bảng kê theo kỳ tháng (mẫu §12, R-56).
//
// Module khác chỉ import metering/port (R-02). Tệp này nối các lớp cho cmd/api-platform, cmd/api-public
// và cmd/worker.
package metering

import (
	"context"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = port.DBRole

// Deps là phụ thuộc của module.
type Deps struct {
	// Plans: gói hạn mức của pf/apiclient (pool apiclient_app). nil → mặc định "tạm, chờ khách".
	Plans apiclientport.Plans
	// Params: tham số đã duyệt của pf/masterdata (hạn mức/ngưỡng kênh, giá). nil → mặc định.
	Params mdport.Params
	// Mode: AG_EGRESS_MODE (rỗng = observe). Public API luôn enforce qua Ledger.Record.
	Mode port.Mode
	// Metrics: nil → bộ đo trên telemetry.Meter().
	Metrics *app.Metrics
	Log     *slog.Logger
}

// Module là pf/metering đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai metering_app.
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	m := deps.Metrics
	if m == nil {
		m = app.NewMetrics(telemetry.Meter())
	}
	return &Module{svc: &app.Service{DB: db, Clock: clk, Plans: deps.Plans, Params: deps.Params, Mode: deps.Mode, Metrics: m, Log: deps.Log}}
}

// UsePlans nối gói hạn mức của pf/apiclient sau khi dựng module đó.
func (m *Module) UsePlans(p apiclientport.Plans) { m.svc.Plans = p }

// RegisterApp: không có route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (MFA): /v1/metering/*.
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// Ledger là sổ sử dụng cho cmd/api-public (ghi TRƯỚC khi gửi byte; lỗi → 503, không dữ liệu).
func (m *Module) Ledger() port.Ledger { return m.svc }

// Quotas là kiểm hạn mức sớm cho cmd/api-public.
func (m *Module) Quotas() port.Quotas { return m.svc }

// Usage là tổng sử dụng của chính khách hàng (GET /v1/usage của Public API).
func (m *Module) Usage() port.Usage { return m.svc }

// Checkpoint là ĐIỂM KIỂM SOÁT XUẤT DUY NHẤT cho reports, mrv, van, party (gọi qua port.Guard).
func (m *Module) Checkpoint() port.Checkpoint { return m.svc }

// Mode là chế độ điểm kiểm soát đã cấu hình.
func (m *Module) Mode() port.Mode {
	if m.svc.Mode == "" {
		return port.ModeObserve
	}
	return m.svc.Mode
}

// StatementHandler là inbox.Handler của consumer port.StatementConsumer (pool metering_app).
func (m *Module) StatementHandler() inbox.Handler { return m.svc.HandleStatementRequested }

// RebuildTenant dựng lại bảng chiếu của tenant chỉ từ sổ (R-22).
func (m *Module) RebuildTenant(ctx context.Context, tid uuid.UUID) (store.Rebuilt, error) {
	return m.svc.RebuildTenant(ctx, tid)
}

// Metrics trả bộ đo (kiểm thử).
func (m *Module) Metrics() *app.Metrics { return m.svc.Metrics }
