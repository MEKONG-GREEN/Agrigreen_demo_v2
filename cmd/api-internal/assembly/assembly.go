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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/mkgwire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops"
	foport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders"
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
	Logistics   *logistics.Module
	Orders      *orders.Module // G4: hợp đồng mua rơm, đơn mua/phiếu nhận, đơn bán/giao nhận (orders_app)
	Costing     *costing.Module
	Acctbridge  *acctbridge.Module
}

// List trả module theo thứ tự gắn route.
func (m Modules) List() []httpx.Module {
	return []httpx.Module{m.Hub, m.Measurement, m.Fieldops, m.Logistics, m.Orders, m.Costing, m.Acctbridge}
}

// OpsRole là vai vận hành của api-internal ở cụm mkg (tương đương platform_api ở
// cụm pf): chỉ bảng vận hành shared.idempotency_key qua ag_shared_user, không có
// quyền nghiệp vụ (Q-MKG-07).
const OpsRole = "mkg_api"

// Build mở pool theo vai của từng module và nối port giữa các module mkg.
//
// G4 (CHỖ NỐI — docs/dev/g4-conventions.md §6): logistics (logistics_app; WaterwayRoutes qua bề mặt
// đối tác, cùng tài khoản dịch vụ AG_MKG_SA_* của fieldops), orders (orders_app), costing
// (costing_app; đọc hub.LotOrigins, measurement.LotMass, logistics.Trips, orders.Deliveries/Contracts),
// acctbridge (acctbridge_app; costing.ClosedPeriods, LedgerAdapter theo AG_ACCTBRIDGE_ADAPTER). Thêm
// trường vào Modules và List theo thứ tự phụ thuộc: logistics, orders → costing → acctbridge.
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
	// G3-2: số cuộn + tỷ lệ thửa từ mkg/fieldops qua adapter mkgwire.Rolls (gán sau khi dựng
	// fieldops); nguồn gốc lô hub đọc với tenant tường minh (không cần vai hub).
	rolls := &mkgwire.Rolls{}
	mods.Measurement = measurement.New(measPool, clk, measurement.Deps{Rolls: rolls, Origins: mods.Hub.LotOrigins(), Plots: rolls})
	foPool, err := open(ctx, fieldops.DBRole)
	if err != nil {
		return mods, ops, pools, err
	}
	pools = append(pools, foPool)
	fo := FieldopsOptionsFromEnv(clk)
	// kg/cuộn học từ khối lượng cân HUB_IN (CUSTOMER_IN với tuyến DIRECT) của mkg/measurement,
	// lệnh gom → lô cân qua liên kết lot_collection (đường làm mới tay; consumer
	// mkg.fieldops.weigh_ticket ở worker-internal là đường chính).
	fo.Weighed = mkgwire.Weighed{Links: mods.Measurement.CollectionLinks(), Lots: mods.Measurement.LotMass()}
	mods.Fieldops = fieldops.New(foPool, clk, fo)
	rolls.Totals, rolls.KgPerRoll = mods.Fieldops.RollTotals(), mods.Fieldops.KgPerRoll()
	// G4 mkg/logistics: định tuyến đường thủy qua bề mặt đối tác (cùng tài khoản dịch vụ AG_MKG_SA_*).
	logPool, err := open(ctx, logistics.DBRole)
	if err != nil {
		return mods, ops, pools, err
	}
	pools = append(pools, logPool)
	mods.Logistics = logistics.New(logPool, clk, LogisticsOptionsFromEnv(clk))
	// G4 mkg/orders: hợp đồng mua rơm, đơn mua/phiếu nhận, đơn bán/giao nhận.
	ordPool, err := open(ctx, orders.DBRole)
	if err != nil {
		return mods, ops, pools, err
	}
	pools = append(pools, ordPool)
	mods.Orders = orders.NewWith(ordPool, clk, orders.Options{Stock: mods.Hub.Stock()}) // O-05: cảnh báo thiếu tồn
	// G4 costing (costing_app): nguồn gốc lô hub (quy kết hao hụt), hợp đồng mua rơm + giao nhận của
	// mkg/orders, chuyến của mkg/logistics (ghe chưa cân, đọc lại khi đóng kỳ).
	costPool, err := open(ctx, costing.DBRole)
	if err != nil {
		return mods, ops, pools, err
	}
	pools = append(pools, costPool)
	mods.Costing = costing.New(costPool, clk, costing.Deps{
		Origins:    mods.Hub.LotOrigins(),
		Contracts:  mods.Orders.Contracts(),
		Trips:      mods.Logistics.Trips(),
		Deliveries: mods.Orders.Deliveries(),
	})
	// G4 mkg/acctbridge (vai acctbridge_app): HTTP + job thử lại đẩy. API không dựng bút toán (worker dựng)
	// nên không cần costing.ClosedPeriods ở đây; bộ điều hợp dùng cho đẩy lại chạy ở worker-internal.
	abPool, err := open(ctx, acctbridge.DBRole)
	if err != nil {
		return mods, ops, pools, err
	}
	pools = append(pools, abPool)
	mods.Acctbridge = acctbridge.New(abPool, clk, acctbridge.Deps{RetryMax: acctbridge.RetryMaxFromEnv()})
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
	if selected == "all" || strings.Contains(","+selected+",", ","+foport.JobHarvestDates+",") {
		// FM-01/02: bảng đệm ngày gặt của lệnh gom đang mở, đọc bề mặt partner của nền tảng.
		tid, _ := uuid.Parse(os.Getenv("AG_MKG_TENANT_ID"))
		interval, _ := time.ParseDuration(os.Getenv("AG_FIELDOPS_HARVEST_POLL_INTERVAL"))
		out = append(out, mods.Fieldops.HarvestJobs(tid, interval, log)...)
	}
	if selected == "all" || strings.Contains(","+selected+",", ","+logistics.JobAlerts+",") {
		// L-08: cảnh báo ghe chưa cân > N giờ, đăng kiểm hết / sắp hết hạn (AG_LOGISTICS_ALERT_INTERVAL, mặc định 15m).
		interval, _ := time.ParseDuration(os.Getenv("AG_LOGISTICS_ALERT_INTERVAL"))
		out = append(out, mods.Logistics.Jobs(logistics.ActiveTenants(tenantPool), interval, log)...)
	}
	if selected == "all" || strings.Contains(","+selected+",", ","+acctbridge.JobPushRetry+",") {
		// R-62: đẩy lại bút toán tổng hợp có hạn thử lại (giãn cách mũ) — tenant tìm qua vai ag_outbox_publisher.
		out = append(out, mods.Acctbridge.Jobs(acctbridge.PendingRetryTenants(tenantPool), log)...)
	}
	return out
}

// JobNames liệt kê tên job hợp lệ của api-internal.
func JobNames() string {
	return hubport.JobDailyCapacity + ", " + foport.JobHTXResponses + ", " + foport.JobHarvestDates + ", " + logistics.JobAlerts + ", " +
		acctbridge.JobPushRetry
}

// FieldopsOptionsFromEnv đọc cấu hình mkg/fieldops:
//
//	AG_PLATFORM_API_URL, AG_PLATFORM_TOKEN_URL, AG_MKG_SA_CLIENT_ID, AG_MKG_SA_CLIENT_SECRET
//	    tài khoản dịch vụ tenant MKG gọi bề mặt đối tác /v1/partner/* của Application API công khai
//	    (OIDC client credentials). Thiếu → nguồn thửa/HTX đóng (lập lệnh gom trả 503
//	    fieldops.directory_unavailable).
//	AG_PARTNER_GRANTOR_TENANT tenant chương trình (bên cấp) của cấp quyền chia sẻ (ADR-0020) — tham số
//	    grantor của /v1/partner/*. Rỗng → nền tảng tự chọn khi chỉ có đúng một bên cấp.
//	AG_FIELDOPS_KG_PER_ROLL   kg/cuộn cấu hình (bậc 3 của chuỗi dự phòng; tạm, chờ khách)
//	AG_FIELDOPS_CURSOR_KEY    khóa HMAC con trỏ pull của app hiện trường (thiếu → ngẫu nhiên theo tiến trình)
//	AG_MKG_TENANT_ID          tenant MKG của tài khoản dịch vụ (job fieldops_htx_responses kéo phản hồi HTX, G3-2)
//	AG_FIELDOPS_HTX_POLL_INTERVAL chu kỳ kéo phản hồi HTX (Go duration, mặc định 5m)
//	AG_FIELDOPS_HARVEST_POLL_INTERVAL chu kỳ làm mới ngày gặt (job fieldops_harvest_dates, mặc định 1h)
//	    Ngày gặt đọc qua GET /v1/partner/season-harvest-dates (cùng tài khoản dịch vụ); thiếu cấu
//	    hình → kiểm ngày gặt gắn cờ unavailable (vẫn cho ghi, FM-01).
//
// WeighedMass (mkg/measurement) nối trong Build.
func FieldopsOptionsFromEnv(clk clock.Clock) fieldops.Options {
	var o fieldops.Options
	if base := os.Getenv("AG_PLATFORM_API_URL"); base != "" {
		cfg := fieldops.PlatformConfig{
			BaseURL: base, TokenURL: os.Getenv("AG_PLATFORM_TOKEN_URL"),
			ClientID: os.Getenv("AG_MKG_SA_CLIENT_ID"), ClientSecret: os.Getenv("AG_MKG_SA_CLIENT_SECRET"),
			GrantorTenant: strings.TrimSpace(os.Getenv("AG_PARTNER_GRANTOR_TENANT")),
		}
		o.Directory = fieldops.NewPlatformDirectory(cfg, clk)
		o.Feed = fieldops.NewPlatformFeed(cfg, clk)
		o.Harvest = fieldops.NewPlatformHarvestCalendar(cfg, clk)
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

// LogisticsOptionsFromEnv đọc cấu hình mkg/logistics: cùng AG_PLATFORM_API_URL, AG_PLATFORM_TOKEN_URL,
// AG_MKG_SA_CLIENT_ID/SECRET, AG_PARTNER_GRANTOR_TENANT với fieldops. Thiếu → mọi chặng thủy dự phòng
// Haversine × #10 (không chặn lập chuyến).
func LogisticsOptionsFromEnv(clk clock.Clock) logistics.Options {
	var o logistics.Options
	if base := os.Getenv("AG_PLATFORM_API_URL"); base != "" {
		o.Waterway = logistics.NewPlatformWaterway(logistics.PlatformConfig{
			BaseURL: base, TokenURL: os.Getenv("AG_PLATFORM_TOKEN_URL"),
			ClientID: os.Getenv("AG_MKG_SA_CLIENT_ID"), ClientSecret: os.Getenv("AG_MKG_SA_CLIENT_SECRET"),
			GrantorTenant: strings.TrimSpace(os.Getenv("AG_PARTNER_GRANTOR_TENANT")),
		}, clk)
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
	// CORSOrigins là origin trình duyệt được gọi chéo (AG_CORS_ORIGINS, khớp chính xác) —
	// web nội bộ MKG (apps/web-internal).
	CORSOrigins []string
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
	r.Use(httpx.CORS(d.CORSOrigins)) // trước mọi route: preflight đi qua cả nhánh 404/405
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
	r.Use(httpx.CORS(d.CORSOrigins))
	health(d)(r)
	v1 := r.Group("/v1", httpx.Authenticate(d.Verifier), httpx.RequireMFA(), idempotency.Middleware(d.Ops))
	for _, m := range d.Modules {
		m.RegisterAdmin(v1)
	}
	return r
}
