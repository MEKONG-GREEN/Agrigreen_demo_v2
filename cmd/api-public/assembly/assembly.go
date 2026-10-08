// Package assembly là gốc lắp ráp của cmd/api-public (Public API cho bên thứ ba, ADR-0022): nối pool
// vai của từng module chủ dữ liệu (g5-conventions §3.1) thành apiclient.PublicDeps. Tách khỏi package
// main để kịch bản đầu–cuối (tests/e2e) chạy ĐÚNG gốc lắp ráp và router của tiến trình thật. archlint
// (zoneRoots, noVaultRoots) áp cho cả cây cmd/api-public, kể cả gói này.
package assembly

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/grantlookup"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
)

// PoolOpener mở pool cho một vai DB của cụm pf.
type PoolOpener func(ctx context.Context, role string) (*pgxpool.Pool, error)

// partyDBRole là vai DB của pf/party (party.DBRole). Không import gốc module pf/party: gốc đó kéo
// client kho định danh và handler Application/Admin (RB-3, ADR-0022 mục 1) — api-public chỉ dùng bộ
// điều hợp chỉ đọc party/port/grantlookup.
const partyDBRole = "party_app"

// Config là cấu hình đã đọc của api-public.
type Config struct {
	Operator   uuid.UUID
	Peppers    *apiclient.Peppers
	Store      objstore.Store
	Replicas   int
	Production bool
	// EgressMode: AG_EGRESS_MODE cho pf/metering. Public API luôn đóng khi lỗi qua Ledger.Record,
	// không phụ thuộc chế độ (ADR-0022 mục 1, ADR-0024).
	EgressMode meter.Mode
	Clock      clock.Clock
	Log        *slog.Logger
}

// Ports là cổng của module khác mà Public API dùng — mỗi cổng đọc bằng pool vai của module chủ
// (R-05), api-public chỉ mở pool của g5-conventions §3.1.
type Ports struct {
	Ledger meter.Ledger
	Quotas meter.Quotas
	Usage  meter.Usage
	Grants partyport.GrantLookup
	// Reads: sổ đọc partner_read_log ở tenant bên cấp cho lần đọc của khách hàng API (K-05).
	Reads  partyport.APIReadLog
	Params params.Params

	Collection apiclientport.CollectionVolumeSource
	Hubs       apiclientport.HubCapacitySource
	MRV        apiclientport.MRVPackageSource

	// Ready: pool của cổng bắt buộc (metering, party) — /readyz ping thêm.
	Ready []*pgxpool.Pool
}

// ErrNotWired: thiếu cổng bắt buộc (sổ đo lường, hạn mức, cấp quyền) — không khởi động (đóng khi lỗi).
var ErrNotWired = errors.New("api-public: chưa nối metering/port.Ledger, Quotas hoặc party/port.GrantLookup — không khởi động")

// integrationPorts nối cổng THẬT của module khác (tích hợp G5, 02/10/2026), mỗi cổng một pool vai
// của module chủ (g5-conventions §3.1):
//   - pf/metering (metering_app): Ledger, Quotas, Usage; gói hạn mức qua plans (pf/apiclient, pool
//     apiclient_app); tham số kênh qua pf/masterdata;
//   - pf/party (party_app): GrantLookup + sổ đọc partner_read_log của bên cấp (APIReadLog, K-05) — bộ
//     điều hợp party/port/grantlookup (không vault);
//   - pf/cropcycle (cropcycle_app): CollectionVolumeSource, dữ kiện thửa qua pf/geo (geo_app) + đơn
//     vị hành chính pf/masterdata (masterdata_app);
//   - pf/geo (geo_app): HubCapacitySource;
//   - pf/mrv (mrv_app): MRVPackageSource (chỉ đọc export_job — không kho đối tượng).
//
// Biến (không phải hàm) để kiểm thử thay bằng cổng giả.
var integrationPorts = func(ctx context.Context, open PoolOpener, cfg Config, plans apiclientport.Plans) (Ports, error) {
	clk := cfg.Clock
	pools := map[string]*pgxpool.Pool{}
	for _, role := range []string{masterdata.DBRole, metering.DBRole, partyDBRole, geo.DBRole, cropcycle.DBRole, mrv.DBRole} {
		p, err := open(ctx, role)
		if err != nil {
			return Ports{}, err
		}
		pools[role] = p
	}
	mdPool, meterPool, partyPool := pools[masterdata.DBRole], pools[metering.DBRole], pools[partyDBRole]
	geoPool, ccPool, mrvPool := pools[geo.DBRole], pools[cropcycle.DBRole], pools[mrv.DBRole]
	md := masterdata.New(mdPool, clk)
	meter := metering.New(meterPool, clk, metering.Deps{Plans: plans, Params: md.Params(), Mode: cfg.EgressMode, Log: cfg.Log})
	g := geo.New(geoPool, clk, geo.Deps{Params: md.Params(), AdminUnits: md.AdminUnits()})
	cc := cropcycle.New(ccPool, clk, cropcycle.Deps{Params: md.Params()})
	cc.UseCollectionVolume(g.PlotFacts(), md.AdminUnits())
	m := mrv.New(mrvPool, clk, mrv.Deps{Params: md.Params()})
	grants := grantlookup.New(partyPool, clk)
	return Ports{Ledger: meter.Ledger(), Quotas: meter.Quotas(), Usage: meter.Usage(), Grants: grants, Reads: grants, Params: md.Params(),
		Collection: cc.CollectionVolumeSource(), Hubs: g.HubCapacitySource(), MRV: m.PackageSource(),
		Ready: []*pgxpool.Pool{meterPool, partyPool}}, nil
}

// Build dựng phụ thuộc của Public API: pool public_api (vận hành shared, /readyz), pool apiclient_app
// (khách hàng, khóa, gói, việc xuất) và cổng của module khác qua integrationPorts.
func Build(ctx context.Context, open PoolOpener, cfg Config) (*apiclient.Module, apiclient.PublicDeps, error) {
	ops, err := open(ctx, apiclientport.PublicDBRole)
	if err != nil {
		return nil, apiclient.PublicDeps{}, err
	}
	apPool, err := open(ctx, apiclient.DBRole)
	if err != nil {
		return nil, apiclient.PublicDeps{}, err
	}
	mod := apiclient.New(apPool, cfg.Clock, apiclient.Deps{Peppers: cfg.Peppers, OperatorTenant: cfg.Operator, Store: cfg.Store, Log: cfg.Log})
	ports, err := integrationPorts(ctx, open, cfg, mod.Plans())
	if err != nil {
		return nil, apiclient.PublicDeps{}, err
	}
	mod.UseCollectionSource(ports.Collection)
	d, err := Assemble(cfg, ports, append([]*pgxpool.Pool{ops, apPool}, ports.Ready...))
	return mod, d, err
}

// Assemble nối cổng thành apiclient.PublicDeps (kiểm thử dùng trực tiếp với cổng giả).
func Assemble(cfg Config, ports Ports, pools []*pgxpool.Pool) (apiclient.PublicDeps, error) {
	if ports.Ledger == nil || ports.Quotas == nil || ports.Grants == nil {
		return apiclient.PublicDeps{}, ErrNotWired
	}
	ready := make([]func(context.Context) error, 0, len(pools))
	for _, p := range pools {
		ready = append(ready, p.Ping)
	}
	return apiclient.PublicDeps{Limiter: apiclient.NewRateLimiter(cfg.Replicas),
		Ledger: ports.Ledger, Quotas: ports.Quotas, Usage: ports.Usage, Grants: ports.Grants, Reads: ports.Reads, Params: ports.Params,
		Collection: ports.Collection, Hubs: ports.Hubs, MRV: ports.MRV, OperatorTenant: cfg.Operator, Production: cfg.Production,
		Clock: cfg.Clock, Log: cfg.Log, Ready: ready}, nil
}
