package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

// Config là cấu hình đã đọc của api-public.
type Config struct {
	Operator   uuid.UUID
	Peppers    *apiclient.Peppers
	Store      objstore.Store
	Replicas   int
	Production bool
	Clock      clock.Clock
	Log        *slog.Logger
}

// Ports là cổng của module khác mà Public API dùng — mỗi cổng đọc bằng pool vai của module chủ
// (R-05), api-public chỉ mở pool của g5-conventions §3.1.
type Ports struct {
	Ledger meteringport.Ledger
	Quotas meteringport.Quotas
	Usage  meteringport.Usage
	Grants partyport.GrantLookup
	Params mdport.Params

	Collection apiclientport.CollectionVolumeSource
	Hubs       apiclientport.HubCapacitySource
	MRV        apiclientport.MRVPackageSource
}

// ErrNotWired: thiếu cổng bắt buộc (sổ đo lường, hạn mức, cấp quyền) — không khởi động (đóng khi lỗi).
var ErrNotWired = errors.New("api-public: chưa nối metering/port.Ledger, Quotas hoặc party/port.GrantLookup — không khởi động")

// integrationPorts nối cổng của module khác. Đợt G5-1 làm song song:
//   - M (pf/metering): Ledger, Quotas, Usage — pool metering_app;
//   - S/D: GrantLookup (pf/party, pool party_app), CollectionVolumeSource (pf/cropcycle), HubCapacitySource
//     (pf/geo), MRVPackageSource (pf/mrv) — pool vai của từng module.
//
// NGƯỜI TÍCH HỢP nối các dòng đó ở đây khi các nhánh đã merge (giữ api-public không import kho định
// danh: chỉ dựng phần module không cần vault). Thiếu Ledger/Quotas/Grants → Build trả ErrNotWired.
var integrationPorts = func(ctx context.Context, open PoolOpener, clk clock.Clock) (Ports, error) {
	mdPool, err := open(ctx, masterdata.DBRole)
	if err != nil {
		return Ports{}, err
	}
	return Ports{Params: masterdata.New(mdPool, clk).Params()}, nil
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
	ports, err := integrationPorts(ctx, open, cfg.Clock)
	if err != nil {
		return nil, apiclient.PublicDeps{}, err
	}
	mod := apiclient.New(apPool, cfg.Clock, apiclient.Deps{Peppers: cfg.Peppers, OperatorTenant: cfg.Operator, Store: cfg.Store,
		Collection: ports.Collection, Log: cfg.Log})
	d, err := Assemble(cfg, ports, []*pgxpool.Pool{ops, apPool})
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
		Ledger: ports.Ledger, Quotas: ports.Quotas, Usage: ports.Usage, Grants: ports.Grants, Params: ports.Params,
		Collection: ports.Collection, Hubs: ports.Hubs, MRV: ports.MRV, OperatorTenant: cfg.Operator, Production: cfg.Production,
		Clock: cfg.Clock, Log: cfg.Log, Ready: ready}, nil
}
