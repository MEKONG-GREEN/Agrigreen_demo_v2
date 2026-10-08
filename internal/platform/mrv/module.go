// Package mrv là module nền MRV của G1 (mục 16): danh mục trường bằng chứng có
// phiên bản và phê duyệt [CHỜ-1], sổ minh chứng ghim phiên bản danh mục, duyệt
// tối thiểu của TTKN tỉnh, xuất gói bằng chứng bất đồng bộ (mục 12) có băm ảnh
// chụp dữ liệu, và Sink đồng bộ ngoại tuyến cho entity "evidence".
//
// Module khác chỉ được import mrv/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform và cmd/worker.
package mrv

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = "mrv_app"

// Deps là phụ thuộc của module.
type Deps struct {
	// Scope: phạm vi TW → Tỉnh → Xã của người gọi (pf/party, ADR-0009).
	Scope partyport.ScopeResolver
	// Consent: đồng ý theo mục đích mrv.evidence của nông hộ (pf/party, mục 10.5).
	Consent partyport.ConsentChecker
	// Store: kho đối tượng cho gói xuất (packages/<tenant>/<export_id>.json).
	Store objstore.Store
	// Params: tham số đã duyệt của pf/masterdata (G3: hệ số CO2, FR-08, cửa sổ đối chiếu).
	Params params.Params
}

// Module là pf/mrv đã nối.
type Module struct{ svc *app.Service }

// New tạo module với pool của vai mrv_app. Ở worker, db có thể nil — handler
// xuất gói chỉ dùng giao dịch do inbox.Process mở. Thiếu phụ thuộc → 503
// mrv.dependency_unavailable ở thao tác cần nó.
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	return &Module{svc: &app.Service{DB: db, Clock: clk, Scope: deps.Scope, Consent: deps.Consent, Store: deps.Store, Params: deps.Params}}
}

// UseValveGrants nối cổng cấp quyền chia sẻ của pf/party (ADR-0020): van mrv_collection_evidence
// của tenant MKG ghi vào tenant bên cấp.
func (m *Module) UseValveGrants(vg partyport.ValveGrants) { m.svc.ValveGrants = vg }

// UseEgress (G5, ADR-0024) nối điểm kiểm soát xuất (pf/metering): kênh mrv_package (yêu cầu xuất,
// cấp URL tải gói) và kênh valve (consumer mrv_collection_evidence). cp nil → hành vi cũ.
func (m *Module) UseEgress(cp meter.Checkpoint, mode meter.Mode, log *slog.Logger) {
	m.svc.Egress, m.svc.EgressMode, m.svc.Log = cp, mode, log
}

// PackageSource (G5, ADR-0022) là nguồn của sản phẩm dữ liệu mrv_package_status cho cmd/api-public.
func (m *Module) PackageSource() apiclientport.MRVPackageSource { return m.svc }

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API (danh mục trường, phê duyệt).
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// SyncSink cho pf/sync nhận entity "evidence" chiều lên.
func (m *Module) SyncSink() syncport.Sink { return app.Sink{Svc: m.svc} }

// SyncSource cho pf/sync: entity "mrv_catalog" chiều xuống (danh mục đã duyệt).
func (m *Module) SyncSource() syncport.Source { return app.CatalogSource{Svc: m.svc} }

// ExportHandler là inbox.Handler của worker cho pf.mrv.export.requested
// (consumer port.ExportConsumer, pool vai mrv_app).
func (m *Module) ExportHandler() inbox.Handler { return m.svc.HandleExportRequested }

// FarmLogLinkHandler là inbox.Handler của consumer port.FarmLogLinkConsumer (G3):
// tham chiếu vụ + liên kết nhật ký HTX làm bằng chứng (KN US-MRV-02).
func (m *Module) FarmLogLinkHandler() inbox.Handler { return m.svc.HandleFarmLogLink }

// CollectionEvidenceHandler là inbox.Handler của consumer port.CollectionEvidenceConsumer
// (G3): van mrv_collection_evidence, kiểm hợp đồng lúc chạy.
func (m *Module) CollectionEvidenceHandler() inbox.Handler { return m.svc.HandleCollectionEvidence }

// PendingTenants dựng bộ tìm tenant có hồ sơ chờ duyệt cho job leo thang (pool vai
// ag_ops_jobs, chỉ đọc hai cột evidence_current — pf 000045).
func PendingTenants(pool *pgxpool.Pool) app.TenantLister { return app.PendingTenants(pool) }

// EscalateOverdue leo thang hồ sơ quá hạn của một tenant tại now (KN US-MRV-06) — thân job;
// kiểm thử gọi trực tiếp với thời điểm giả.
func (m *Module) EscalateOverdue(ctx context.Context, tid uuid.UUID, now time.Time) (int, error) {
	return m.svc.EscalateOverdue(ctx, tid, now)
}

// RunEscalation chạy leo thang cho mọi tenant của tenants tại giờ đồng hồ module.
func (m *Module) RunEscalation(ctx context.Context, tenants app.TenantLister) (int, error) {
	return m.svc.RunEscalation(ctx, tenants)
}

// Jobs trả job port.JobReviewEscalator (mỗi 15 phút, chạy ở leader): leo thang hồ sơ MRV chờ
// duyệt quá hạn — mức 1 người duyệt, mức 2 quản trị TTKN tỉnh. Idempotent theo (phiên bản, mức).
func (m *Module) Jobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: port.JobReviewEscalator, Spec: schedule.Every{Interval: 15 * time.Minute}, RunOnStart: true, Timeout: 10 * time.Minute,
		Run: func(ctx context.Context) error {
			n, err := m.svc.RunEscalation(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("leo thang hồ sơ MRV quá hạn", "event", "job."+port.JobReviewEscalator, "raised", n, "outcome", outcome)
			}
			return err
		},
	}}
}
