package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/grantlookup"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
)

// apiclientFeedJobs dựng job feed của cổng tích hợp (W5.3, ADR-0033 §2, ADR-0035): dựng tập dữ liệu
// (apiclientport.JobFeedBuild) và dọn feed quá 30 ngày (apiclientport.JobFeedRetention). Một pool app_worker nối thẳng
// cụm pf; mỗi module chạy dưới vai nhóm của nó (pf/apiclient r_apiclient; nguồn ô thô pf/cropcycle + dữ kiện thửa
// và vụ pf/cooperative + đơn vị hành chính và k ở pf/masterdata). Ảnh chụp ghi kho đối tượng (AG_S3_* hoặc
// AG_OBJSTORE_DIR; không cấu hình → phiên bản không có ảnh chụp, cảnh báo). Tenant đóng góp: AG_INTEGRATION_CONTRIBUTORS.
func apiclientFeedJobs(ctx context.Context, direct string, closeFn *func(), log *slog.Logger) ([]schedule.Job, error) {
	op, err := apiclient.OperatorTenantFromEnv()
	if err != nil {
		return nil, err
	}
	contrib, err := apiclient.ContributorsFromEnv()
	if err != nil {
		return nil, err
	}
	interval, err := apiclient.FeedBuildIntervalFromEnv()
	if err != nil {
		return nil, err
	}
	var store objstore.Store
	if os.Getenv("AG_S3_ENDPOINT") != "" || os.Getenv("AG_OBJSTORE_DIR") != "" {
		if store, err = objstore.FromEnv(ctx); err != nil {
			return nil, err
		}
	} else {
		log.Warn("feed: chưa cấu hình kho đối tượng — phiên bản tập dữ liệu không có ảnh chụp", "event", "startup", "outcome", "degraded")
	}
	pool, err := openPF(ctx, direct, closeFn)
	if err != nil {
		return nil, err
	}
	clk := clock.System{}
	md := masterdata.New(pool, clk)
	cc := cropcycle.New(pool, clk, cropcycle.Deps{})
	coop := cooperative.New(pool, clk, cooperative.Deps{}) // W3.3: dữ kiện thửa và vụ ở pf_coop (r_cooperative)
	cc.UseCollectionVolume(coop.Plots(), coop.Seasons(), md.AdminUnits())
	mod := apiclient.New(pool, clk, apiclient.Deps{OperatorTenant: op, Store: store, Log: log})
	mod.UseFeedSources(cc.AggregateSource(), contrib, md.Params())
	// W5.5: feed lớp partner theo bên cấp — danh bạ HTX (pf/party, bộ điều hợp không vault, vị trí hồ sơ từ
	// pf/cooperative) và thửa (pf/cooperative, W3.3); bên cấp từ cấp quyền đang hiệu lực của bên nhận có đăng ký feed.
	gl := grantlookup.New(pool, clk)
	gl.Locations = coop.CoopLocations()
	mod.UsePartnerFeedSources(map[string]apiclientport.PartnerSource{
		apiclientport.FeedCooperativeDirectory: gl,
		apiclientport.FeedPlotDirectory:        coop.PartnerFeedSource(),
	}, gl)
	return mod.FeedJobs(interval, log), nil
}
