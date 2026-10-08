// Package measurement là module mkg/measurement (tài liệu kiến trúc mục 13;
// docs/design/mkg.md §5.2): sổ thiết bị cân có duyệt, phiên cân (cửa sổ bộ
// đếm), phiếu cân ba điểm HUB_IN / HUB_OUT / CUSTOMER_IN, dung sai và định mức
// hao hụt có phiên bản, đối soát theo lô, sản lượng quy đổi, tranh chấp, nhập số
// cân của khách, van mrv_collection_evidence. Event sourcing mức B: SỔ LÀ NGUỒN.
//
// Vùng nội bộ MKG: chỉ nối ở cmd/api-internal (vai measurement_app) và cmd/ingest
// (vai ingest_mkg, qua NewIngest). Module khác chỉ import measurement/port (R-02).
package measurement

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/ingest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB của module ở cụm mkg (R-05).
const DBRole = port.DBRole

// Deps là phụ thuộc sang module mkg khác (qua port, cùng tiến trình).
type Deps struct {
	// Rolls: số cuộn từ mkg/fieldops; nil → app.NoRolls (đối soát ghi pending_rolls).
	Rolls port.RollTotals
	// Origins: nguồn gốc lô từ mkg/hub (thửa + tỷ lệ) cho van MRV, tenant tường minh (không
	// cần vai hub — G3-2).
	Origins hubport.LotOrigins
	// Plots: tỷ lệ thửa theo lệnh gom (mkg/fieldops) cho van MRV khi lô không có ở hub
	// (tuyến DIRECT). Origins và Plots cùng nil → van MRV từ chối.
	Plots port.CollectionPlots
}

// Module là mkg/measurement đã nối.
type Module struct {
	svc *app.Service
	w   *app.Writer
}

// New tạo module với pool của vai measurement_app (cụm mkg).
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	db = forModule(db)
	w := &app.Writer{DB: db, Clock: clk}
	return &Module{w: w, svc: &app.Service{DB: db, Clock: clk, Writer: w, Rolls: deps.Rolls, Origins: deps.Origins,
		Plots: deps.Plots}}
}

// RegisterApp gắn route vận hành (bề mặt ứng dụng).
// RebuildTenant dựng lại bảng chiếu của tenant từ sổ, tenant tường minh, không kiểm vai — CHỈ cho công cụ diễn
// tập R-22 trên bản sao database (cmd/rebuilddrill). Trả số dòng theo bảng chiếu.
func (m *Module) RebuildTenant(ctx context.Context, tid uuid.UUID) (map[string]int64, error) {
	return m.svc.RebuildTenant(ctx, tid)
}

func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route đăng ký / duyệt (bề mặt quản trị, MFA).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// LotMass cho hub / costing đọc khối lượng chuẩn của lô (port.LotMass).
func (m *Module) LotMass() port.LotMass { return m.w }

// CollectionLinks phân giải lô cân ↔ lệnh gom (G3-2, tenant tường minh).
func (m *Module) CollectionLinks() port.CollectionLinks { return m.w }

// Tickets là TicketWriter của module (vai measurement_app).
func (m *Module) Tickets() port.TicketWriter { return m.w }

// Ingest gom các cổng mà cmd/ingest dùng — dựng bằng NewIngest với pool vai
// ingest_mkg (chỉ có quyền ghi phiếu, đọc sổ thiết bị / phiên, cập nhật sức khỏe).
type Ingest struct {
	Tickets  port.TicketWriter
	Devices  port.DeviceRegistry
	Health   port.DeviceHealth
	Sessions port.WeighingSessions
	LotMass  port.LotMass
}

// NewIngest dựng cổng cho cmd/ingest với pool vai port.IngestRole (cụm mkg).
func NewIngest(db tenant.Beginner, clk clock.Clock) Ingest {
	db = forModule(db)
	w := &app.Writer{DB: db, Clock: clk}
	return Ingest{Tickets: w, Devices: w, Health: w, Sessions: w, LotMass: w}
}

// NewIngestPorts dựng cổng cho vòng tích phân + bộ nhận của cmd/ingest trên pool
// vai ingest_mkg: sổ thiết bị, hàng phiên đã đóng (xuyên tenant), bì theo cửa sổ,
// sổ phiếu (idempotent theo IdemKey ingest:<session>:<rule_version>).
func NewIngestPorts(db tenant.Beginner, clk clock.Clock) IngestPorts {
	db = forModule(db)
	w := &app.Writer{DB: db, Clock: clk}
	return IngestPorts{Devices: w, Sessions: ingestSessions{w}, Tare: w, Tickets: w}
}

// ingestSessions chuyển port.Session của module sang ingest.Session.
type ingestSessions struct{ w *app.Writer }

func (s ingestSessions) PendingSessions(ctx context.Context, limit int) ([]ingest.Session, error) {
	ss, err := s.w.PendingSessions(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ingest.Session, 0, len(ss))
	for _, x := range ss {
		is := ingest.Session{ID: x.ID, TenantID: x.TenantID, LotRef: x.LotRef, Point: x.Point, Route: x.Route, Window: x.Window,
			MoisturePct: x.MoisturePct}
		if x.ClosedBy != nil {
			is.ActorRef = *x.ClosedBy
		}
		if x.ClosedAt != nil {
			is.ClosedAt = *x.ClosedAt
		}
		out = append(out, is)
	}
	return out, nil
}

// Consumers là handler sự kiện NỘI BỘ MKG của measurement cho cmd/worker-internal
// (vhost mkg.internal, ADR-0018). Handler chỉ dùng giao dịch inbox được giao (vai
// measurement_app, tenant của phong bì); số cuộn đọc qua deps.Rolls (fieldops, pool riêng).
type Consumers struct{ svc *app.Service }

// NewConsumers dựng handler consumer của measurement.
func NewConsumers(clk clock.Clock, deps Deps) Consumers {
	return Consumers{svc: &app.Service{Clock: clk, Writer: &app.Writer{Clock: clk}, Rolls: deps.Rolls}}
}

// RollCount: mkg.fieldops.roll_count.recorded → đối soát lại lô cân liên kết (RB-6, G3-2).
func (c Consumers) RollCount() inbox.Handler {
	return func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
		if env.EventType != fieldport.EventRollCountRecorded {
			return nil
		}
		var r fieldport.RollCountRecordedV1
		if err := json.Unmarshal(env.Payload, &r); err != nil {
			return fmt.Errorf("measurement: payload %s không hợp lệ: %w", env.EventType, err)
		}
		return c.svc.RollCountRecorded(ctx, tx, env.TenantID, r.LotRef, r.RollCountID)
	}
}
