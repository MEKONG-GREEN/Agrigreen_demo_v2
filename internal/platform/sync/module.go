// Package sync là module đồng bộ ngoại tuyến phía máy chủ (mục 15, ADR-0006,
// docs/dev/sync-contract.md): đăng ký/thu hồi thiết bị, pull theo phạm vi, push
// chỉ ghi thêm, tệp minh chứng đã mã hóa, sổ đăng ký xung đột có SLA.
//
// pf/sync không đọc bảng của module khác: dữ liệu đi xuống qua port.Source, đi lên
// qua port.Sink, do gốc lắp ráp truyền vào (R-02, R-05, R-06).
package sync

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// DBRole là vai DB riêng của module (R-05) cho cả pf_sync và pf_files.
const DBRole = "sync_app"

// MetricConflictOverdue là chỉ số nghiệp vụ bắt buộc có cảnh báo (mục 19): số
// xung đột đồng bộ đang mở đã quá SLA, theo tenant.
const MetricConflictOverdue = "ag.sync.conflict.overdue"

// Deps là phụ thuộc của module. Sources/Sinks là port của các module có dữ liệu
// hiện trường (geo, content, extension, mrv…), gốc lắp ráp gom lại.
type Deps struct {
	Scope     partyport.ScopeResolver  // phạm vi TW → Tỉnh → Xã (ADR-0009); nil → phạm vi rỗng
	Consent   partyport.ConsentChecker // tùy chọn: kiểm đồng ý trước khi cấp tệp minh chứng
	Params    mdport.Params            // tùy chọn: sync.conflict_sla_hours (mặc định 72 giờ)
	Vault     client.Client            // khóa công khai (pubkey), mở DEK xem tệp
	Store     objstore.Store           // kho đối tượng chỉ chứa bản mã (R-40); nil → /v1/files trả 503
	CursorKey []byte                   // AG_SYNC_CURSOR_KEY, ít nhất 32 byte
	Log       *slog.Logger
	Sources   []port.Source
	Sinks     []port.Sink
}

// Module là pf/sync đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai sync_app. Thiếu CursorKey (AG_SYNC_CURSOR_KEY)
// → sinh khóa ngẫu nhiên cho tiến trình: con trỏ cũ hết hiệu lực sau khi khởi
// động lại hoặc khi đi qua phiên bản khác — CHỈ chấp nhận ở Local/Dev.
// Hai Source/Sink cùng entity là lỗi lắp ráp → panic lúc khởi động.
func New(db tenant.Beginner, clk clock.Clock, d Deps) *Module {
	if len(d.CursorKey) == 0 {
		d.CursorKey = make([]byte, 32)
		if _, err := rand.Read(d.CursorKey); err != nil {
			panic("sync: không sinh được khóa con trỏ: " + err.Error())
		}
	}
	svc, err := app.NewService(db, clk, app.Deps{Scope: d.Scope, Consent: d.Consent, Params: d.Params, Vault: d.Vault,
		Store: d.Store, CursorKey: d.CursorKey, Log: d.Log}, d.Sources, d.Sinks)
	if err != nil {
		panic(err.Error())
	}
	return &Module{svc: svc}
}

// ErrCursorKey: khóa HMAC con trỏ pull không dùng được (ADR-0006).
var ErrCursorKey = errors.New("sync: AG_SYNC_CURSOR_KEY")

// CheckCursorKey kiểm khóa HMAC con trỏ trước khi dựng module (ADR-0006, sync-contract.md):
// khóa có thì phải ≥ 32 byte; ở production bắt buộc có — khóa ngẫu nhiên theo tiến trình
// làm con trỏ hỏng sau mỗi lần khởi động lại và giữa các bản chạy song song.
func CheckCursorKey(key []byte, production bool) error {
	switch {
	case len(key) == 0 && production:
		return fmt.Errorf("%w: bắt buộc ở production (khóa ngẫu nhiên chỉ cho Local/Dev)", ErrCursorKey)
	case len(key) > 0 && len(key) < 32:
		return fmt.Errorf("%w: phải có ít nhất 32 byte", ErrCursorKey)
	}
	return nil
}

// NewDevices dựng cổng đọc sổ thiết bị (port.Devices) từ pool vai sync_app, độc
// lập với module (không cần Source/Sink) — gốc lắp ráp dùng khi module khác vừa cần
// kiểm thiết bị vừa là Source của pf/sync.
func NewDevices(db tenant.Beginner) port.Devices { return app.DeviceLookup{DB: db} }

// Devices là cổng đọc sổ thiết bị của module đã nối.
func (m *Module) Devices() port.Devices { return app.DeviceLookup{DB: m.svc.DB} }

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API.
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// UseFarmers nối liên kết tài khoản nông dân ↔ nông hộ (pf/accounts) sau khi dựng pf/accounts:
// thiết bị của nông dân (vai farmer, không org_ref) pull các entity của port.FarmerSource theo
// nông hộ của mình và khóa công khai của chính các hộ đó (HTX US-SEASON-01, US-LOG-01, US-SUPPORT-01).
func (m *Module) UseFarmers(f accport.FarmerHouseholds) { m.svc.Farmers = f }

// UseIntegrations nối danh sách consumer đã khai và (tùy chọn) bộ đọc độ sâu DLQ cho màn giám
// sát đồng bộ tích hợp (GIS US-SYNC-01).
func (m *Module) UseIntegrations(consumers []string, dlq func(context.Context) (map[string]int64, error)) {
	m.svc.Integration = app.IntegrationDeps{Consumers: consumers, DLQDepth: dlq}
}

// FailureRecorder dựng inbox.FailureRecorder ghi lần xử lý thất bại của consumer vào
// pf_sync.integration_failure (vai sync_app) và phát pf.sync.integration.dead_lettered ở lần
// giao cuối (GIS US-SYNC-01/02). Không cần dựng module đầy đủ — worker dùng cho mọi consumer.
func FailureRecorder(db tenant.Beginner, clk clock.Clock) inbox.FailureRecorder {
	svc := &app.Service{DB: db, Clock: clk}
	return func(ctx context.Context, f inbox.Failure) error {
		return svc.RecordFailure(ctx, port.ConsumerFailure{
			TenantID: f.Envelope.TenantID, EventID: f.Envelope.EventID, Consumer: f.Consumer, EventType: f.Envelope.EventType,
			ErrorCode: app.ErrorCode(f.Err), DeliveryCount: int(f.DeliveryCount), DeadLettered: f.DeadLettered, EventTime: f.Envelope.OccurredAt,
		})
	}
}

// PullEntities liệt kê entity chiều xuống đã nối (kể cả pubkey).
func (m *Module) PullEntities() []string { return m.svc.PullEntities() }

// ConflictSLA cho worker đo xung đột quá hạn của một tenant (mục 19).
func (m *Module) ConflictSLA(ctx context.Context, tenantID uuid.UUID) (port.ConflictSLA, error) {
	return m.svc.ConflictSLA(ctx, tenantID)
}

// RegisterSLAMetrics đăng ký gauge ag.sync.conflict.overdue (theo tenant_id) trên
// meter OpenTelemetry chung; tenants trả danh sách tenant cần đo.
func (m *Module) RegisterSLAMetrics(tenants func(context.Context) ([]uuid.UUID, error)) (metric.Registration, error) {
	return registerSLA(m.svc, tenants)
}

// RegisterConflictSLAMetrics đăng ký gauge ag.sync.conflict.overdue mà không cần dựng
// module đầy đủ (worker, ADR-0006 + mục 19): db là pool vai sync_app, tenants thường là
// ConflictTenants(pool ag_outbox_publisher).
func RegisterConflictSLAMetrics(db tenant.Beginner, clk clock.Clock, tenants func(context.Context) ([]uuid.UUID, error)) (metric.Registration, error) {
	return registerSLA(&app.Service{DB: db, Clock: clk}, tenants)
}

// ConflictTenants liệt kê tenant có xung đột đồng bộ đang mở, đọc bằng pool vai
// ag_outbox_publisher (hai cột tenant_id, status — migration pf 000043).
func ConflictTenants(pool *pgxpool.Pool) func(context.Context) ([]uuid.UUID, error) {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, "SELECT DISTINCT tenant_id FROM pf_sync.conflict WHERE status = 'open'")
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}

func registerSLA(svc *app.Service, tenants func(context.Context) ([]uuid.UUID, error)) (metric.Registration, error) {
	mt := telemetry.Meter()
	g, err := mt.Int64ObservableGauge(MetricConflictOverdue,
		metric.WithDescription("Số xung đột đồng bộ đang mở đã quá SLA (C-8)"))
	if err != nil {
		return nil, err
	}
	return mt.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		ts, err := tenants(ctx)
		if err != nil {
			return err
		}
		for _, t := range ts {
			s, err := svc.ConflictSLA(ctx, t)
			if err != nil {
				return err
			}
			o.ObserveInt64(g, s.Overdue, metric.WithAttributes(attribute.String("tenant_id", t.String())))
		}
		return nil
	}, g)
}
