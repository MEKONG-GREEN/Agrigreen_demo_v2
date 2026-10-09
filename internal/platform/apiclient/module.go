// Package apiclient là module pf/apiclient (G5, ADR-0022): khách hàng Public API, khóa API lưu băm
// (prefix + HMAC-SHA-256 có pepper theo phiên bản), đăng ký sản phẩm dữ liệu + scope, gói hạn mức có
// phiên bản, việc xuất bất đồng bộ và bề mặt Public (package public, chạy ở cmd/api-public).
//
// Module khác chỉ import apiclient/port (R-02). Tệp này nối các lớp cho cmd/api-platform (Admin API),
// cmd/api-public và cmd/worker.
package apiclient

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/public"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai DB riêng của module (R-05).
const DBRole = port.DBRole

// Peppers là pepper HMAC theo phiên bản của khóa API (domain.Peppers).
type Peppers = domain.Peppers

// PublicDeps là phụ thuộc của bề mặt Public (cmd/api-public).
type PublicDeps = public.Deps

// RateLimiter là giới hạn tần suất backend theo khách hàng (trong tiến trình; Valkey là adapter tùy chọn).
type RateLimiter = public.RateLimiter

// NewRateLimiter tạo bộ giới hạn trong tiến trình chia hạn mức cho replicas bản sao.
func NewRateLimiter(replicas int) RateLimiter { return public.NewMemory(replicas) }

// Deps là phụ thuộc của module.
type Deps struct {
	// Peppers: pepper HMAC của khóa API (PeppersFromEnv). Nil → phát hành/xoay khóa trả 503.
	Peppers *Peppers
	// OperatorTenant: tenant đơn vị vận hành (AG_PUBLIC_OPERATOR_TENANT); Nil ở Admin API → tenant
	// của người gọi.
	OperatorTenant uuid.UUID
	// Store: kho đối tượng trong lãnh thổ cho tệp xuất.
	Store objstore.Store
	// Collection: nguồn collection_volume (pf/cropcycle — đợt G5-1 S/D nối).
	Collection port.CollectionVolumeSource
	// Sandbox: môi trường thử của đối tác (W5.8, SandboxFromEnv) — khóa agks_.
	Sandbox bool
	Log     *slog.Logger
}

// Module là pf/apiclient đã nối.
type Module struct {
	svc *app.Service
	wh  webhookWiring // W5.7 (webhook_wire.go)
}

// New tạo module với pool vai apiclient_app. Ở worker, db có thể nil (handler xuất chạy trong giao
// dịch của inbox.Process).
func New(db tenant.Beginner, clk clock.Clock, deps Deps) *Module {
	db = forModule(db)
	return &Module{svc: &app.Service{DB: db, Clock: clk, Peppers: deps.Peppers, OperatorTenant: deps.OperatorTenant, Store: deps.Store,
		Collection: deps.Collection, Sandbox: deps.Sandbox, Log: deps.Log}}
}

// PeppersFromEnv đọc AG_APIKEY_PEPPER_V<n> / AG_APIKEY_PEPPER_CURRENT của tiến trình. Không đặt →
// (nil, nil).
func PeppersFromEnv() (*Peppers, error) { return domain.PeppersFromEnv(os.Getenv, os.Environ()) }

// SandboxFromEnv đọc AG_PARTNER_SANDBOX (môi trường thử của đối tác, W5.8); production → lỗi khởi động.
func SandboxFromEnv(getenv func(string) string) (bool, error) {
	return port.SandboxFromEnv(getenv, residency.Production(getenv))
}

// OperatorTenantFromEnv đọc AG_PUBLIC_OPERATOR_TENANT (rỗng → Nil).
func OperatorTenantFromEnv() (uuid.UUID, error) {
	v := os.Getenv(port.EnvOperatorTenant)
	if v == "" {
		return uuid.Nil, nil
	}
	return uuid.Parse(v)
}

// UseCollectionSource nối nguồn collection_volume (pf/cropcycle) sau khi dựng.
func (m *Module) UseCollectionSource(src port.CollectionVolumeSource) { m.svc.Collection = src }

// Service trả use case (cmd/api-public dựng bề mặt Public từ đây).
func (m *Module) Service() *app.Service { return m.svc }

// Authenticator trả port.Authenticator (prefix + HMAC thời gian hằng) trên tenant đơn vị vận hành.
func (m *Module) Authenticator() port.Authenticator { return app.Authenticator{Svc: m.svc} }

// Plans trả port.Plans cho pf/metering và api-public.
func (m *Module) Plans() port.Plans { return m.svc }

// PublicRouter dựng Gin engine của Public API (cmd/api-public). Svc/Auth/Plans/Limiter rỗng → lấy
// từ module.
func (m *Module) PublicRouter(d PublicDeps) *gin.Engine {
	if d.Svc == nil {
		d.Svc = m.svc
	}
	if d.Auth == nil {
		d.Auth = m.Authenticator()
	}
	if d.Plans == nil {
		d.Plans = m.svc
	}
	return public.NewRouter(d)
}

// RegisterApp: module không có route Application API (bề mặt Public ở cmd/api-public).
func (m *Module) RegisterApp(*gin.RouterGroup) {}

// RegisterAdmin gắn route Admin API (MFA): khách hàng, khóa, đăng ký, gói hạn mức.
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) {
	handler.Admin{Svc: m.svc}.Register(rg)
	handler.Webhooks{Svc: m.Webhooks()}.Register(rg) // W5.7
}

// ExpireDueKeys ghi sự kiện expired cho mọi khóa quá hạn của tenant đơn vị vận hành tại now — thân job
// port.JobKeyExpiry; kiểm thử gọi trực tiếp với thời điểm giả. Trả số khóa vừa chuyển expired.
func (m *Module) ExpireDueKeys(ctx context.Context, now time.Time) (int, error) {
	return m.svc.ExpireAllDue(ctx, m.svc.OperatorTenant, now)
}

// Jobs trả job port.JobKeyExpiry (mỗi giờ, chạy ở leader của cmd/worker). Không có tenant đơn vị vận
// hành (AG_PUBLIC_OPERATOR_TENANT, ĐTK 1) → không job nào.
func (m *Module) Jobs(log *slog.Logger) []schedule.Job {
	if m.svc.OperatorTenant == uuid.Nil {
		return nil
	}
	return []schedule.Job{{
		Name: port.JobKeyExpiry, Spec: schedule.Every{Interval: time.Hour}, RunOnStart: true, Timeout: 5 * time.Minute,
		Run: func(ctx context.Context) error {
			n, err := m.ExpireDueKeys(ctx, m.svc.Clock.Now())
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("khóa API quá hạn → expired", "event", "job."+port.JobKeyExpiry, "expired", n, "outcome", outcome)
			}
			return err
		},
	}}
}

// ExportHandler là inbox.Handler của consumer port.ExportConsumer (pool vai apiclient_app).
func (m *Module) ExportHandler() inbox.Handler { return m.svc.HandleExportRequested }

// ---------------------------------------------------------------------------
// Feed và tập dữ liệu dựng sẵn (W5.3)
// ---------------------------------------------------------------------------

// UseFeedSources nối nguồn ô thô của tập tổng hợp (pf/cropcycle), danh sách tenant đóng góp và tham số k
// (pf/masterdata) cho job dựng feed — gốc lắp ráp cmd/worker gọi sau khi dựng.
func (m *Module) UseFeedSources(src port.AggregateSource, contrib port.Contributors, p params.Params) {
	m.svc.Aggregates, m.svc.Contributors, m.svc.Params = src, contrib, p
}

// UsePartnerFeedSources (W5.5) nối nguồn feed lớp partner theo bên cấp (tên feed → module chủ: pf/party
// cooperative_directory, pf/geo plot_directory) và danh sách bên cấp đang có cấp quyền (party/port/grantlookup).
func (m *Module) UsePartnerFeedSources(src map[string]port.PartnerSource, grantors port.PartnerGrantors) {
	m.svc.PartnerSources, m.svc.Grantors = src, grantors
}

// UseBilateral (W5.6, ADR-0034) nối sổ song phương của pf/cooperative: nguồn feed của cặp (job dựng feed) — gốc lắp ráp
// cmd/worker gọi; cổng tích hợp nối qua PublicDeps.Bilateral và IngestDeps.Bilateral.
func (m *Module) UseBilateral(l port.BilateralLedger) { m.svc.Bilateral = l }

// BuildFeeds dựng một phiên bản mỗi tập có nguồn (thân job port.JobFeedBuild; kiểm thử gọi trực tiếp).
func (m *Module) BuildFeeds(ctx context.Context) ([]app.BuildResult, error) {
	return m.svc.BuildAll(ctx)
}

// PurgeFeeds dọn mục feed quá 30 ngày (thân job port.JobFeedRetention).
func (m *Module) PurgeFeeds(ctx context.Context) (int64, error) { return m.svc.PurgeFeeds(ctx, 5000) }

// ContributorsFromEnv đọc port.EnvContributors (mã tenant phân tách bằng dấu phẩy; rỗng → không tenant nào).
func ContributorsFromEnv() (port.StaticContributors, error) {
	var out port.StaticContributors
	for _, v := range strings.Split(os.Getenv(port.EnvContributors), ",") {
		if v = strings.TrimSpace(v); v == "" {
			continue
		}
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %q không phải UUID", port.EnvContributors, v)
		}
		out = append(out, id)
	}
	return out, nil
}

// FeedBuildIntervalFromEnv đọc port.EnvFeedBuildInterval (mặc định port.DefaultFeedBuildInterval, tối thiểu 1 phút).
func FeedBuildIntervalFromEnv() (time.Duration, error) {
	v := os.Getenv(port.EnvFeedBuildInterval)
	if v == "" {
		return port.DefaultFeedBuildInterval, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < time.Minute {
		return 0, fmt.Errorf("%s: thời lượng ≥ 1m (vd 15m)", port.EnvFeedBuildInterval)
	}
	return d, nil
}

// FeedJobs trả job dựng tập dữ liệu (mỗi interval) và dọn feed quá hạn (mỗi giờ) — chạy ở leader của cmd/worker.
// Không có tenant đơn vị vận hành → không job nào.
func (m *Module) FeedJobs(interval time.Duration, log *slog.Logger) []schedule.Job {
	if m.svc.OperatorTenant == uuid.Nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return []schedule.Job{
		{Name: port.JobFeedBuild, Spec: schedule.Every{Interval: interval}, RunOnStart: true, Timeout: 10 * time.Minute,
			Run: func(ctx context.Context) error {
				_, err := m.BuildFeeds(ctx)
				return err
			}},
		{Name: port.JobFeedRetention, Spec: schedule.Every{Interval: time.Hour}, RunOnStart: true, Timeout: 10 * time.Minute,
			Run: func(ctx context.Context) error {
				n, err := m.PurgeFeeds(ctx)
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("feed: dọn mục quá hạn giữ", "event", "job."+port.JobFeedRetention, "purged", n, "outcome", outcome)
				return err
			}},
	}
}
