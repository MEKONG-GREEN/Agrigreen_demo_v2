// Package assembly là gốc lắp ráp của api-internal — API nội bộ MKG (vùng mkg,
// mục 5). Chỉ nối module internal/mkg/*, mỗi module một vai DB ở cụm mkg (R-05).
//
// Ranh giới (R-03): package này KHÔNG import internal/platform/*; ngược lại
// cmd/api-platform không import internal/mkg/*. tools/archlint cưỡng chế cả hai
// chiều ở gốc lắp ráp. Dữ liệu sang nền tảng chỉ đi qua van công bố (outbox của
// cụm mkg → outbox-publisher → RabbitMQ).
package assembly

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops"
	foport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
)

// PoolOpener mở pool cho một vai DB của cụm mkg.
type PoolOpener func(ctx context.Context, role string) (*pgxpool.Pool, error)

// Modules là các module mkg đã nối.
type Modules struct {
	Hub         *hub.Module
	Measurement *measurement.Module
	Fieldops    *fieldops.Module
}

// List trả module theo thứ tự gắn route.
func (m Modules) List() []httpx.Module { return []httpx.Module{m.Hub, m.Measurement, m.Fieldops} }

// OpsRole là vai vận hành của api-internal ở cụm mkg (tương đương platform_api ở
// cụm pf): chỉ bảng vận hành shared.idempotency_key qua ag_shared_user, không có
// quyền nghiệp vụ (Q-MKG-07).
const OpsRole = "mkg_api"

// Build mở pool theo vai của từng module và nối port giữa các module mkg.
// Thêm mkg/fieldops, measurement, logistics, orders, costing, acctbridge ở đây.
//
// ops là pool của vai OpsRole cho idempotency (cũng nằm trong pools để /readyz kiểm).
func Build(ctx context.Context, open PoolOpener, clk clock.Clock) (mods Modules, ops *pgxpool.Pool, pools []*pgxpool.Pool, err error) {
	ops, err = open(ctx, OpsRole)
	if err != nil {
		return mods, nil, pools, err
	}
	pools = append(pools, ops)
	hubPool, err := open(ctx, hub.DBRole)
	if err != nil {
		return mods, ops, pools, err
	}
	pools = append(pools, hubPool)
	mods.Hub = hub.New(hubPool, clk)
	measPool, err := open(ctx, measurement.DBRole)
	if err != nil {
		return mods, ops, pools, err
	}
	pools = append(pools, measPool)
	// Rolls: chưa có hiện thực fieldops (đợt fieldops G3) → app.NoRolls (pending_rolls).
	mods.Measurement = measurement.New(measPool, clk, measurement.Deps{Origins: mods.Hub.Lots()})
	foPool, err := open(ctx, fieldops.DBRole)
	if err != nil {
		return mods, ops, pools, err
	}
	pools = append(pools, foPool)
	fo := FieldopsOptionsFromEnv(clk)
	// kg/cuộn học từ khối lượng cân HUB_IN (CUSTOMER_IN với tuyến DIRECT) của mkg/measurement.
	fo.Weighed = fieldops.WeighedFromMeasurement(mods.Measurement.LotMass())
	mods.Fieldops = fieldops.New(foPool, clk, fo)
	return mods, ops, pools, nil
}

// Jobs trả job định kỳ của vùng mkg được chọn (danh sách phân tách bằng dấu phẩy hoặc "all"):
// hub_capacity_daily — công bố sức chứa theo ngày qua van hub_capacity (GIS US-WH-01 AC-2).
// tenantPool là pool vai ag_outbox_publisher của cụm mkg (tìm tenant xuyên RLS, chỉ đọc cột).
func Jobs(mods Modules, tenantPool *pgxpool.Pool, selected string, log *slog.Logger) []schedule.Job {
	var out []schedule.Job
	if selected == "all" || strings.Contains(","+selected+",", ","+hubport.JobDailyCapacity+",") {
		out = append(out, mods.Hub.Jobs(hub.ActiveHubTenants(tenantPool), log)...)
	}
	if selected == "all" || strings.Contains(","+selected+",", ","+foport.JobHTXResponses+",") {
		// Nền tảng → MKG chỉ qua Application API công khai: kéo phản hồi của HTX cho tenant MKG
		// AG_MKG_TENANT_ID (tenant của tài khoản dịch vụ agrigreen-mkg-sync). Thiếu → không có job.
		tid, _ := uuid.Parse(os.Getenv("AG_MKG_TENANT_ID"))
		interval, _ := time.ParseDuration(os.Getenv("AG_FIELDOPS_HTX_POLL_INTERVAL"))
		out = append(out, mods.Fieldops.Jobs(tid, interval, log)...)
	}
	return out
}

// JobNames liệt kê tên job hợp lệ của api-internal.
func JobNames() string { return hubport.JobDailyCapacity + ", " + foport.JobHTXResponses }

// FieldopsOptionsFromEnv đọc cấu hình mkg/fieldops:
//
//	AG_PLATFORM_API_URL, AG_PLATFORM_TOKEN_URL, AG_MKG_SA_CLIENT_ID, AG_MKG_SA_CLIENT_SECRET
//	    tài khoản dịch vụ tenant MKG gọi Application API công khai (OIDC client credentials).
//	    Thiếu → nguồn thửa/HTX đóng (lập lệnh gom trả 503 fieldops.directory_unavailable).
//	AG_FIELDOPS_KG_PER_ROLL   kg/cuộn cấu hình (bậc 3 của chuỗi dự phòng; tạm, chờ khách)
//	AG_FIELDOPS_CURSOR_KEY    khóa HMAC con trỏ pull của app hiện trường (thiếu → ngẫu nhiên theo tiến trình)
//	AG_MKG_TENANT_ID          tenant MKG của tài khoản dịch vụ (job fieldops_htx_responses kéo phản hồi HTX, G3-2)
//	AG_FIELDOPS_HTX_POLL_INTERVAL chu kỳ kéo phản hồi HTX (Go duration, mặc định 5m)
//
// WeighedMass (mkg/measurement) nối trong Build.
func FieldopsOptionsFromEnv(clk clock.Clock) fieldops.Options {
	var o fieldops.Options
	if base := os.Getenv("AG_PLATFORM_API_URL"); base != "" {
		o.Directory = fieldops.NewPlatformDirectory(fieldops.PlatformConfig{
			BaseURL: base, TokenURL: os.Getenv("AG_PLATFORM_TOKEN_URL"),
			ClientID: os.Getenv("AG_MKG_SA_CLIENT_ID"), ClientSecret: os.Getenv("AG_MKG_SA_CLIENT_SECRET"),
		}, clk)
		o.Feed = fieldops.NewPlatformFeed(fieldops.PlatformConfig{
			BaseURL: base, TokenURL: os.Getenv("AG_PLATFORM_TOKEN_URL"),
			ClientID: os.Getenv("AG_MKG_SA_CLIENT_ID"), ClientSecret: os.Getenv("AG_MKG_SA_CLIENT_SECRET"),
		}, clk)
	}
	if s := os.Getenv("AG_FIELDOPS_KG_PER_ROLL"); s != "" {
		if d, err := decimal.NewFromString(s); err == nil && d.Sign() > 0 {
			o.KgPerRoll = &d
		}
	}
	if k := os.Getenv("AG_FIELDOPS_CURSOR_KEY"); k != "" {
		o.CursorKey = []byte(k)
	}
	return o
}

// Deps là phụ thuộc của router.
type Deps struct {
	Log      *slog.Logger
	Verifier *authz.Verifier
	Ops      *pgxpool.Pool
	Modules  []httpx.Module
	Pools    []*pgxpool.Pool
}

const maxBody = 1 << 20

func health(d Deps) func(r *gin.Engine) {
	return func(r *gin.Engine) {
		r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": "mkg"}) })
		r.GET("/readyz", func(c *gin.Context) {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			defer cancel()
			for _, p := range d.Pools {
				if err := p.Ping(ctx); err != nil {
					c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
					return
				}
			}
			c.JSON(http.StatusOK, gin.H{"status": "ready", "zone": "mkg"})
		})
	}
}

// NewRouter dựng bề mặt ứng dụng của API nội bộ MKG.
func NewRouter(d Deps) *gin.Engine {
	r := httpx.NewEngine(d.Log, maxBody)
	health(d)(r)
	v1 := r.Group("/v1", httpx.Authenticate(d.Verifier), idempotency.Middleware(d.Ops))
	for _, m := range d.Modules {
		m.RegisterApp(v1)
	}
	return r
}

// NewAdminRouter dựng bề mặt quản trị của API nội bộ MKG: MFA bắt buộc.
func NewAdminRouter(d Deps) *gin.Engine {
	r := httpx.NewEngine(d.Log, maxBody)
	health(d)(r)
	v1 := r.Group("/v1", httpx.Authenticate(d.Verifier), httpx.RequireMFA(), idempotency.Middleware(d.Ops))
	for _, m := range d.Modules {
		m.RegisterAdmin(v1)
	}
	return r
}
