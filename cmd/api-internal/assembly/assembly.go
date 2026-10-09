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
	"errors"
	"fmt"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine"
	engport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops"
	foport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
)

// PoolOpener mở pool cho một vai đăng nhập của cụm mkg (W2.6: app_internal).
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
	// W8.2: kho quan sát của engine (r_engine) — không route HTTP (không có trong List); job engine_observation_feed.
	Engine *engine.Module
}

// List trả module theo thứ tự gắn route.
func (m Modules) List() []httpx.Module {
	return []httpx.Module{m.Hub, m.Measurement, m.Fieldops, m.Logistics, m.Orders, m.Costing, m.Acctbridge}
}

// OpsRole là vai đăng nhập của api-internal ở cụm mkg (W2.6, ADR-0028): app_internal NOINHERIT — idempotency chạy
// SET LOCAL ROLE r_ops (chỉ shared.idempotency_key qua ag_shared_user, Q-MKG-07), module chạy r_<module>.
// Thay vai mkg_api + bảy vai <module>_app.
const OpsRole = dbadmin.AppInternal

// Build mở MỘT pool vai đăng nhập app_internal (W2.6) và nối port giữa các module mkg (mỗi module chạy r_<module>).
//
// G4 (CHỖ NỐI — docs/dev/g4-conventions.md §6): logistics (WaterwayRoutes qua bề mặt
// đối tác, cùng tài khoản dịch vụ AG_MKG_SA_* của fieldops), orders, costing
// (đọc hub.LotOrigins, measurement.LotMass, logistics.Trips, orders.Deliveries/Contracts),
// acctbridge (costing.ClosedPeriods, LedgerAdapter theo AG_ACCTBRIDGE_ADAPTER). Thêm
// trường vào Modules và List theo thứ tự phụ thuộc: logistics, orders → costing → acctbridge.
//
// ops là pool của vai OpsRole (một pool cho cả đơn vị): idempotency (r_ops) và mọi module (r_<module>).
func Build(ctx context.Context, open PoolOpener, clk clock.Clock) (mods Modules, ops *pgxpool.Pool, pools []*pgxpool.Pool, err error) {
	ops, err = open(ctx, OpsRole)
	if err != nil {
		return mods, nil, pools, err
	}
	pools = append(pools, ops)
	unit := ops
	mods.Hub = hub.New(unit, clk)
	// G3-2: số cuộn + tỷ lệ thửa từ mkg/fieldops qua adapter mkgwire.Rolls (gán sau khi dựng
	// fieldops); nguồn gốc lô hub đọc với tenant tường minh (không cần vai hub).
	rolls := &mkgwire.Rolls{}
	mods.Measurement = measurement.New(unit, clk, measurement.Deps{Rolls: rolls, Origins: mods.Hub.LotOrigins(), Plots: rolls})
	fo := FieldopsOptionsFromEnv(clk)
	if fo.DirectorySource, err = DirectorySourceFromEnv(fo.DirectoryFeed != nil); err != nil {
		return mods, ops, pools, err
	}
	// kg/cuộn học từ khối lượng cân HUB_IN (CUSTOMER_IN với tuyến DIRECT) của mkg/measurement,
	// lệnh gom → lô cân qua liên kết lot_collection (đường làm mới tay; consumer
	// mkg.fieldops.weigh_ticket ở worker-internal là đường chính).
	fo.Weighed = mkgwire.Weighed{Links: mods.Measurement.CollectionLinks(), Lots: mods.Measurement.LotMass()}
	mods.Fieldops = fieldops.New(unit, clk, fo)
	rolls.Totals, rolls.KgPerRoll = mods.Fieldops.RollTotals(), mods.Fieldops.KgPerRoll()
	// G4 mkg/logistics: định tuyến đường thủy qua bề mặt đối tác (cùng tài khoản dịch vụ AG_MKG_SA_*).
	// G4: tệp chứng từ vùng mkg (biên bản chênh lệch, ePOD / e-bill) ở kho đối tượng trong lãnh thổ (RB-7).
	docs := DocumentStoreFromEnv(ctx)
	lo := LogisticsOptionsFromEnv(clk)
	lo.Files = docs
	mods.Logistics = logistics.New(unit, clk, lo)
	// G4 mkg/orders: hợp đồng mua rơm, đơn mua/phiếu nhận, đơn bán/giao nhận.
	mods.Orders = orders.NewWith(unit, clk, orders.Options{Stock: mods.Hub.Stock(), Files: docs}) // O-05: cảnh báo thiếu tồn
	// G4 costing (costing_app): nguồn gốc lô hub (quy kết hao hụt), hợp đồng mua rơm + giao nhận của
	// mkg/orders, chuyến của mkg/logistics (ghe chưa cân, đọc lại khi đóng kỳ).
	mods.Costing = costing.New(unit, clk, costing.Deps{
		Origins:    mods.Hub.LotOrigins(),
		Contracts:  mods.Orders.Contracts(),
		Trips:      mods.Logistics.Trips(),
		Deliveries: mods.Orders.Deliveries(),
	})
	// G4 mkg/acctbridge (r_acctbridge): HTTP + job thử lại đẩy. API không dựng bút toán (worker dựng)
	// nên không cần costing.ClosedPeriods ở đây; bộ điều hợp dùng cho đẩy lại chạy ở worker-internal.
	mods.Acctbridge = acctbridge.New(unit, clk, acctbridge.Deps{RetryMax: acctbridge.RetryMaxFromEnv()})
	// W8.2 mkg/engine (r_engine): kho quan sát nạp từ feed của cổng tích hợp qua CÙNG bộ điều hợp feed của W5.5.
	mods.Engine = engine.New(unit, clk, EngineOptionsFromEnv(fo.DirectoryFeed))
	return mods, ops, pools, nil
}

// Jobs trả job định kỳ của vùng mkg được chọn (danh sách phân tách bằng dấu phẩy hoặc "all"):
// hub_capacity_daily — công bố sức chứa theo ngày qua van hub_capacity (GIS US-WH-01 AC-2).
// tenantPool là pool vai ag_ops_jobs của cụm mkg (tìm tenant xuyên RLS, chỉ đọc cột).
// Biến môi trường của job ĐÃ ĐẶT nhưng sai cú pháp (UUID, Go duration) → lỗi khởi động thay vì lặng lẽ tắt
// job hoặc rơi về mặc định; chưa đặt → giữ mặc định (job fieldops không chạy khi thiếu AG_MKG_TENANT_ID).
func Jobs(mods Modules, tenantPool *pgxpool.Pool, selected string, log *slog.Logger) ([]schedule.Job, error) {
	var out []schedule.Job
	picked := func(name string) bool {
		return selected == "all" || strings.Contains(","+selected+",", ","+name+",")
	}
	if picked(hubport.JobDailyCapacity) {
		out = append(out, mods.Hub.Jobs(hub.ActiveHubTenants(tenantPool), log)...)
	}
	if picked(foport.JobHTXResponses) {
		// Nền tảng → MKG chỉ qua Application API công khai: kéo phản hồi của HTX cho tenant MKG
		// AG_MKG_TENANT_ID (tenant của tài khoản dịch vụ agrigreen-mkg-sync). Thiếu → không có job.
		tid, err := EnvUUID("AG_MKG_TENANT_ID")
		if err != nil {
			return nil, err
		}
		interval, err := EnvDuration("AG_FIELDOPS_HTX_POLL_INTERVAL")
		if err != nil {
			return nil, err
		}
		out = append(out, mods.Fieldops.Jobs(tid, interval, log)...)
	}
	if picked(foport.JobHarvestDates) {
		// FM-01/02: bảng đệm ngày gặt của lệnh gom đang mở, đọc bề mặt partner của nền tảng.
		tid, err := EnvUUID("AG_MKG_TENANT_ID")
		if err != nil {
			return nil, err
		}
		interval, err := EnvDuration("AG_FIELDOPS_HARVEST_POLL_INTERVAL")
		if err != nil {
			return nil, err
		}
		out = append(out, mods.Fieldops.HarvestJobs(tid, interval, log)...)
	}
	if picked(foport.JobDirectory) {
		// R-INT-02: bản sao danh mục HTX/thửa trong cụm mkg — lập lệnh gom không gọi nền tảng cho mục đã có.
		tid, err := EnvUUID("AG_MKG_TENANT_ID")
		if err != nil {
			return nil, err
		}
		interval, err := EnvDuration("AG_FIELDOPS_DIRECTORY_POLL_INTERVAL")
		if err != nil {
			return nil, err
		}
		out = append(out, mods.Fieldops.DirectoryJobs(tid, interval, log)...)
	}
	if picked(foport.JobDirectoryFeed) {
		// W5.5 (ADR-0033 §5): bản sao danh bạ HTX/thửa từ feed của cổng tích hợp, chạy song song với fieldops_directory
		// (đối soát hai nguồn ghi log). Thiếu AG_MKG_TENANT_ID hoặc cấu hình feed → không có job.
		tid, err := EnvUUID("AG_MKG_TENANT_ID")
		if err != nil {
			return nil, err
		}
		interval, err := EnvDuration("AG_FIELDOPS_DIRECTORY_FEED_INTERVAL")
		if err != nil {
			return nil, err
		}
		out = append(out, mods.Fieldops.DirectoryFeedJobs(tid, interval, log)...)
	}
	if picked(engport.JobObservationFeed) && mods.Engine != nil {
		// W8.2 (ADR-0036 §3, v4.1 §41.4): feed của cổng tích hợp → kho quan sát của engine. Thiếu AG_MKG_TENANT_ID
		// hoặc cấu hình feed → không có job.
		tid, err := EnvUUID("AG_MKG_TENANT_ID")
		if err != nil {
			return nil, err
		}
		interval, err := EnvDuration("AG_ENGINE_FEED_INTERVAL")
		if err != nil {
			return nil, err
		}
		out = append(out, mods.Engine.Jobs(tid, interval, log)...)
	}
	if picked(foport.JobBilateral) {
		// W5.6 (ADR-0034): luồng song phương qua cổng tích hợp — gửi lịch gom + bảng kê số cuộn (nạp theo lô), kéo feed của
		// cặp, đối soát tổng kiểm ngày. Thiếu AG_MKG_TENANT_ID hoặc AG_FIELDOPS_BILATERAL_API_KEY → không có job.
		tid, err := EnvUUID("AG_MKG_TENANT_ID")
		if err != nil {
			return nil, err
		}
		interval, err := EnvDuration("AG_FIELDOPS_BILATERAL_INTERVAL")
		if err != nil {
			return nil, err
		}
		out = append(out, mods.Fieldops.BilateralJobs(tid, interval, log)...)
	}
	if picked(logistics.JobAlerts) {
		// L-08: cảnh báo ghe chưa cân > N giờ, đăng kiểm hết / sắp hết hạn (AG_LOGISTICS_ALERT_INTERVAL, mặc định 15m).
		interval, err := EnvDuration("AG_LOGISTICS_ALERT_INTERVAL")
		if err != nil {
			return nil, err
		}
		out = append(out, mods.Logistics.Jobs(logistics.ActiveTenants(tenantPool), interval, log)...)
	}
	if picked(acctbridge.JobPushRetry) {
		// R-62: đẩy lại bút toán tổng hợp có hạn thử lại (giãn cách mũ) — tenant tìm qua vai ag_ops_jobs.
		out = append(out, mods.Acctbridge.Jobs(acctbridge.PendingRetryTenants(tenantPool), log)...)
	}
	return out, nil
}

// EnvUUID đọc biến môi trường name dạng UUID: chưa đặt (hoặc chỉ khoảng trắng) → uuid.Nil, nil;
// đặt nhưng sai cú pháp hoặc là UUID rỗng → lỗi nêu tên biến (gõ nhầm không được lặng lẽ tắt job).
func EnvUUID(name string) (uuid.UUID, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s không phải UUID hợp lệ: %w", name, err)
	}
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%s không được là UUID rỗng", name)
	}
	return id, nil
}

// EnvDuration đọc biến môi trường name dạng Go duration: chưa đặt → 0, nil (module dùng mặc định);
// đặt nhưng sai cú pháp hoặc ≤ 0 → lỗi nêu tên biến (gõ nhầm không được lặng lẽ rơi về mặc định).
func EnvDuration(name string) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s không phải Go duration hợp lệ (vd 5m, 1h): %w", name, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s phải > 0, nhận %q", name, v)
	}
	return d, nil
}

// JobNames liệt kê tên job hợp lệ của api-internal.
func JobNames() string {
	return hubport.JobDailyCapacity + ", " + foport.JobHTXResponses + ", " + foport.JobHarvestDates + ", " + foport.JobDirectory + ", " + foport.JobDirectoryFeed + ", " +
		engport.JobObservationFeed + ", " + foport.JobBilateral + ", " + logistics.JobAlerts + ", " + acctbridge.JobPushRetry
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
//	AG_FIELDOPS_DIRECTORY_POLL_INTERVAL chu kỳ làm mới bản sao danh mục HTX/thửa (job fieldops_directory, mặc định 15m)
//	AG_FIELDOPS_DIRECTORY_MAX_AGE tuổi tối đa của bản sao trước khi lệnh gom gắn cờ fieldops.directory_stale (mặc định 6h)
//	AG_FIELDOPS_DIRECTORY_LOOKUP_BUDGET tổng thời gian tra trực tiếp HTX/thửa CHƯA có trong bản sao khi lập/sửa lệnh gom
//	    (mặc định 2s; "off" = chỉ bản sao, không gọi nền tảng trong thao tác người dùng — R-INT-02)
//	AG_INTEGRATION_GATEWAY_URL, AG_FIELDOPS_FEED_API_KEY (W5.5, ADR-0033 §5) gốc cổng tích hợp + khóa API của khách hàng
//	    API tenant MKG (scope feed:cooperative_directory:read, feed:plot_directory:read); bên cấp = AG_PARTNER_GRANTOR_TENANT.
//	    Có cả hai → job fieldops_directory_feed (AG_FIELDOPS_DIRECTORY_FEED_INTERVAL, mặc định 15m) kéo feed danh bạ và
//	    đối soát với bản sao cũ.
//	AG_FIELDOPS_DIRECTORY_SOURCE partner (mặc định) | feed — nguồn đối chiếu của lập/sửa lệnh gom (DirectorySourceFromEnv).
//	AG_FIELDOPS_BILATERAL_API_KEY (W5.6, ADR-0034) khóa API của khách hàng API tenant MKG cho luồng song phương (scope
//	    ingest:collection_schedule:write, ingest:roll_count_statement:write, feed:htx_roll_confirmation:read) trên
//	    AG_INTEGRATION_GATEWAY_URL → job fieldops_bilateral (AG_FIELDOPS_BILATERAL_INTERVAL, mặc định 5m). Hợp đồng còn
//	    proposed: cổng production từ chối (C-12) — chỉ đặt ở dev/staging.
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
	if gw, key := strings.TrimSpace(os.Getenv("AG_INTEGRATION_GATEWAY_URL")), os.Getenv("AG_FIELDOPS_FEED_API_KEY"); gw != "" && key != "" {
		o.DirectoryFeed = fieldops.NewGatewayDirectoryFeed(fieldops.GatewayConfig{BaseURL: gw, APIKey: key,
			GrantorTenant: strings.TrimSpace(os.Getenv("AG_PARTNER_GRANTOR_TENANT"))})
	}
	if gw, key := strings.TrimSpace(os.Getenv("AG_INTEGRATION_GATEWAY_URL")), os.Getenv("AG_FIELDOPS_BILATERAL_API_KEY"); gw != "" && key != "" {
		o.Bilateral = fieldops.NewGatewayBilateral(fieldops.GatewayConfig{BaseURL: gw, APIKey: key}, clk)
	}
	if s := os.Getenv("AG_FIELDOPS_KG_PER_ROLL"); s != "" {
		if d, err := decimal.NewFromString(s); err == nil && d.Sign() > 0 {
			o.KgPerRoll = &d
		}
	}
	if k := os.Getenv("AG_FIELDOPS_CURSOR_KEY"); k != "" {
		o.CursorKey = []byte(k)
	}
	if d, err := time.ParseDuration(os.Getenv("AG_FIELDOPS_DIRECTORY_MAX_AGE")); err == nil && d > 0 {
		o.DirectoryMaxAge = d
	}
	switch b := strings.TrimSpace(os.Getenv("AG_FIELDOPS_DIRECTORY_LOOKUP_BUDGET")); b {
	case "":
	case "off":
		o.DirectoryLookupBudget = -1
	default:
		if d, err := time.ParseDuration(b); err == nil && d > 0 {
			o.DirectoryLookupBudget = d
		}
	}
	return o
}

// DirectorySourceFromEnv đọc AG_FIELDOPS_DIRECTORY_SOURCE (W5.5): rỗng / "partner" → bản sao cũ qua /v1/partner/*;
// "feed" → bản sao dựng từ feed của cổng tích hợp (cần cấu hình feed — haveFeed). Giá trị khác, hoặc "feed" khi chưa cấu
// hình feed → lỗi khởi động (gõ nhầm không được lặng lẽ đổi nguồn).
func DirectorySourceFromEnv(haveFeed bool) (string, error) {
	switch v := strings.TrimSpace(os.Getenv("AG_FIELDOPS_DIRECTORY_SOURCE")); v {
	case "", foport.DirectorySourcePartner:
		return foport.DirectorySourcePartner, nil
	case foport.DirectorySourceFeed:
		if !haveFeed {
			return "", errors.New("AG_FIELDOPS_DIRECTORY_SOURCE=feed cần AG_INTEGRATION_GATEWAY_URL và AG_FIELDOPS_FEED_API_KEY")
		}
		return v, nil
	default:
		return "", fmt.Errorf("AG_FIELDOPS_DIRECTORY_SOURCE phải là partner | feed, nhận %q", v)
	}
}

// EngineOptionsFromEnv đọc cấu hình kho quan sát mkg/engine (W8.2):
//
//	feed  bộ điều hợp feed của W5.5 (AG_INTEGRATION_GATEWAY_URL + AG_FIELDOPS_FEED_API_KEY, bên cấp
//	      AG_PARTNER_GRANTOR_TENANT); khóa API cần thêm scope feed:<tên>:read của các feed quan sát. nil → không job.
//	AG_ENGINE_FEEDS                danh sách feed kéo, phân tách bằng dấu phẩy (rỗng = mọi feed có bộ ánh xạ).
//	AG_ENGINE_FEED_ALLOW_PROPOSED  "1" — cờ dev/test: kéo cả feed có hợp đồng proposed (chưa ký A7); bỏ qua ở
//	                               production (AG_ENV), như AG_GATEWAY_FEED_ALLOW_PROPOSED của cổng.
//	AG_ENGINE_FEED_INTERVAL        chu kỳ job engine_observation_feed (Go duration, mặc định 15m).
func EngineOptionsFromEnv(feed foport.DirectoryFeed) engine.Options {
	o := engine.Options{Feed: feed, Production: residency.Production(os.Getenv),
		AllowProposed: strings.TrimSpace(os.Getenv("AG_ENGINE_FEED_ALLOW_PROPOSED")) == "1"}
	if v := strings.TrimSpace(os.Getenv("AG_ENGINE_FEEDS")); v != "" {
		for _, f := range strings.Split(v, ",") {
			if f = strings.TrimSpace(f); f != "" {
				o.Feeds = append(o.Feeds, f)
			}
		}
	}
	return o
}

// DocumentStoreFromEnv chọn kho đối tượng trong lãnh thổ cho tệp chứng từ vùng mkg (AG_S3_* | AG_OBJSTORE_DIR,
// shared/objstore.FromEnv). Chưa cấu hình → nil: POST /v1/orders/files, /v1/logistics/files trả 503
// (*.file_store_unavailable); phiếu nhận / giao nhận vẫn nhận file_ref.
func DocumentStoreFromEnv(ctx context.Context) objstore.Store {
	st, err := objstore.FromEnv(ctx)
	if err != nil {
		return nil
	}
	return st
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
	v1 := r.Group("/v1", httpx.Authenticate(d.Verifier), idempotency.Middleware(db.ForModule(d.Ops, db.Ops)))
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
	v1 := r.Group("/v1", httpx.Authenticate(d.Verifier), httpx.RequireMFA(), idempotency.Middleware(db.ForModule(d.Ops, db.Ops)))
	for _, m := range d.Modules {
		m.RegisterAdmin(v1)
	}
	return r
}
