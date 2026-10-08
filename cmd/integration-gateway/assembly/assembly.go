// Package assembly là gốc lắp ráp của cmd/integration-gateway (cổng tích hợp, ADR-0033; trước W5.2 là
// cmd/api-public — Public API cho bên thứ ba, ADR-0022): nối pool vai của từng module chủ dữ liệu
// (g5-conventions §3.1) thành apiclient.PublicDeps. Tách khỏi package main để kịch bản đầu–cuối (tests/e2e)
// chạy ĐÚNG gốc lắp ráp và router của tiến trình thật. archlint (zoneRoots, noVaultRoots) áp cho cả cây
// cmd/integration-gateway, kể cả gói này; cmd/api-public (bí danh một kỳ) chỉ gọi gói gateway.
package assembly

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/coopwire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/grantlookup"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
)

// PoolOpener mở pool cho một vai đăng nhập của cụm pf (W2.6: app_public; mở lại cùng vai nên trả cùng pool).
type PoolOpener func(ctx context.Context, role string) (*pgxpool.Pool, error)

// Config là cấu hình đã đọc của cổng tích hợp (integration-gateway, bí danh api-public).
type Config struct {
	Operator   uuid.UUID
	Peppers    *apiclient.Peppers
	Store      objstore.Store
	Replicas   int
	Production bool
	// AllowUnapproved (W5.3): AG_GATEWAY_FEED_ALLOW_PROPOSED=1 — phục vụ feed có hợp đồng chưa approved, chỉ ngoài
	// production (gateway.Run từ chối khởi động nếu đặt ở production).
	AllowUnapproved bool
	// EgressMode: AG_EGRESS_MODE cho pf/metering. Public API luôn đóng khi lỗi qua Ledger.Record,
	// không phụ thuộc chế độ (ADR-0022 mục 1, ADR-0024).
	EgressMode meter.Mode
	Clock      clock.Clock
	Log        *slog.Logger

	// Nạp theo lô (W5.4, ADR-0033 §3): IngestQueue nil → không bật /v1/batches (503 apiclient.dependency_unavailable).
	IngestQueue         apiclient.IngestQueue
	IngestAllowProposed bool // cờ dev/test — bỏ qua ở Production
	IngestMaxQueued     int
	IngestRetryAfter    time.Duration
	// IngestOwners: hợp đồng → port module chủ; nil → ba hợp đồng song phương vào sổ đệm (W5.6 nối port thật).
	IngestOwners map[string]apiclient.IngestOwner
	// Kênh tệp (W5.8): giới hạn byte / bản ghi mỗi tệp (≤ 0 → mặc định).
	IngestMaxFileBytes   int64
	IngestMaxFileRecords int
	// Sandbox: môi trường thử của đối tác (W5.8, AG_PARTNER_SANDBOX) — chỉ khóa agks_; Production + Sandbox bị
	// gateway từ chối khởi động.
	Sandbox bool
}

// Ports là cổng của module khác mà Public API dùng — mỗi cổng đọc bằng pool vai của module chủ
// (R-05), cổng chỉ mở pool của g5-conventions §3.1.
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

	// Ready: pool của cổng bắt buộc (app_public) — /readyz ping thêm.
	Ready []*pgxpool.Pool
}

// ErrNotWired: thiếu cổng bắt buộc (sổ đo lường, hạn mức, cấp quyền) — không khởi động (đóng khi lỗi).
var ErrNotWired = errors.New("integration-gateway: chưa nối metering/port.Ledger, Quotas hoặc party/port.GrantLookup — không khởi động")

// integrationPorts nối cổng THẬT của module khác (tích hợp G5, 02/10/2026) trên MỘT pool vai đăng nhập
// app_public (W2.6, ADR-0028); mỗi cổng chạy dưới vai nhóm của module chủ (r_<module>, g5-conventions §3.1):
//   - pf/metering: Ledger, Quotas, Usage; gói hạn mức qua plans (pf/apiclient); tham số kênh qua pf/masterdata;
//   - pf/party: GrantLookup + sổ đọc partner_read_log của bên cấp (APIReadLog, K-05) — bộ điều hợp
//     party/port/grantlookup (không vault, r_party);
//   - pf/cropcycle: CollectionVolumeSource, dữ kiện thửa qua pf/cooperative (mặt tiền W3.2 trên pf/geo) + đơn vị
//     hành chính pf/masterdata;
//   - pf/geo: HubCapacitySource;
//   - pf/mrv: MRVPackageSource (chỉ đọc export_job — không kho đối tượng).
//
// Biến (không phải hàm) để kiểm thử thay bằng cổng giả.
var integrationPorts = func(ctx context.Context, open PoolOpener, cfg Config, plans apiclientport.Plans) (Ports, error) {
	clk := cfg.Clock
	// W2.6 (ADR-0028): một pool vai app_public; mỗi module tự bọc db.ForModule (r_<module>), grantlookup → r_party.
	unit, err := open(ctx, dbadmin.AppPublic)
	if err != nil {
		return Ports{}, err
	}
	mdPool, meterPool, partyPool, geoPool, ccPool, mrvPool := unit, unit, unit, unit, unit, unit
	md := masterdata.New(mdPool, clk)
	meter := metering.New(meterPool, clk, metering.Deps{Plans: plans, Params: md.Params(), Mode: cfg.EgressMode, Log: cfg.Log})
	g := geo.New(geoPool, clk, geo.Deps{Params: md.Params(), AdminUnits: md.AdminUnits()})
	cc := cropcycle.New(ccPool, clk, cropcycle.Deps{Params: md.Params()})
	// pf/cooperative (W3.2): dữ kiện thửa qua mặt tiền (nguồn cũ pf/geo, cmd/internal/coopwire).
	coop := cooperative.New(coopwire.Sources(coopwire.Legacy{PlotFacts: g.PlotFacts()}))
	cc.UseCollectionVolume(coop.Plots(), md.AdminUnits())
	m := mrv.New(mrvPool, clk, mrv.Deps{Params: md.Params()})
	grants := grantlookup.New(partyPool, clk)
	return Ports{Ledger: meter.Ledger(), Quotas: meter.Quotas(), Usage: meter.Usage(), Grants: grants, Reads: grants, Params: md.Params(),
		Collection: cc.CollectionVolumeSource(), Hubs: g.HubCapacitySource(), MRV: m.PackageSource(),
		Ready: []*pgxpool.Pool{unit}}, nil
}

// Build dựng phụ thuộc của Public API: pool app_public (W2.6; pf/apiclient — khách hàng, khóa, gói, việc xuất —
// chạy dưới r_apiclient) và cổng của module khác qua integrationPorts.
func Build(ctx context.Context, open PoolOpener, cfg Config) (*apiclient.Module, apiclient.PublicDeps, error) {
	apPool, err := open(ctx, dbadmin.AppPublic)
	if err != nil {
		return nil, apiclient.PublicDeps{}, err
	}
	mod := apiclient.New(apPool, cfg.Clock, apiclient.Deps{Peppers: cfg.Peppers, OperatorTenant: cfg.Operator, Store: cfg.Store, Sandbox: cfg.Sandbox,
		Log: cfg.Log})
	ports, err := integrationPorts(ctx, open, cfg, mod.Plans())
	if err != nil {
		return nil, apiclient.PublicDeps{}, err
	}
	mod.UseCollectionSource(ports.Collection)
	d, err := Assemble(cfg, ports, append([]*pgxpool.Pool{apPool}, ports.Ready...))
	if err == nil && cfg.IngestQueue != nil {
		d.Ingest = apiclient.NewIngest(apPool, apiclient.IngestDeps{Queue: cfg.IngestQueue, Production: cfg.Production,
			AllowProposed: cfg.IngestAllowProposed, Owners: cfg.IngestOwners, MaxQueued: cfg.IngestMaxQueued, RetryAfter: cfg.IngestRetryAfter,
			MaxFileBytes: cfg.IngestMaxFileBytes, MaxFileRecords: cfg.IngestMaxFileRecords, OperatorTenant: cfg.Operator, Clock: cfg.Clock, Log: cfg.Log})
	}
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
		AllowUnapproved: cfg.AllowUnapproved && !cfg.Production, Sandbox: cfg.Sandbox, Clock: cfg.Clock, Log: cfg.Log, Ready: ready}, nil
}
