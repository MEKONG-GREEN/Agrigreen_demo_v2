package main

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref"
	krefport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// krefConsumer là worker pf.kref (W2.5, ADR-0029): dựng k_ref từ sự kiện pf.party.org.*,
// pf.masterdata.admin_unit.changed, pf.party.share_grant.*. Pool là vai đăng nhập của đơn vị app_worker
// (ADR-0028 — pf/kref không có vai cũ), mọi giao dịch inbox chạy SET LOCAL ROLE r_kref: vai DUY NHẤT ghi được k_ref.
func krefConsumer() consumer {
	return consumer{
		name: kref.Consumer, cluster: migrations.PF, module: kref.DBModule,
		events: kref.Events, handler: kref.New(nil, clock.System{}).Handler(),
	}
}

// krefSources nối nguồn của lần dựng lại k_ref: pf/party (r_party), pf/masterdata (r_masterdata) trên pool app_worker và
// tìm tenant bằng ag_ops_jobs (chỉ cột tenant_id). open mở pool theo vai.
func krefSources(open func(role string) (*pgxpool.Pool, error)) (krefport.Sources, error) {
	clk := clock.System{}
	partyPool, err := open(dbadmin.AppWorker)
	if err != nil {
		return krefport.Sources{}, err
	}
	mdPool, err := open(dbadmin.AppWorker)
	if err != nil {
		return krefport.Sources{}, err
	}
	pub, err := open(dbadmin.OpsJobs)
	if err != nil {
		return krefport.Sources{}, err
	}
	pty := party.New(partyPool, clk)
	md := masterdata.New(mdPool, clk, extension.MasterdataParams())
	return krefport.Sources{
		Orgs: pty.KRefOrgs(), ShareGrants: pty.KRefShareGrants(), AdminUnits: md.KRefAdminUnits(),
		Tenants: kref.UnionTenants(party.KRefTenants(pub), masterdata.KRefTenants(pub), kref.ProjectedTenants(pub)),
	}, nil
}

// runKRefRebuild (`worker -kref-rebuild`): dựng lại k_ref của mọi tenant từ nguồn rồi thoát — sau triển khai
// W2.5 lần đầu (Nút có trước W2.5 chưa có sự kiện created), sau phục hồi (thứ tự §34: nguồn trước, k_ref sau),
// hoặc khi consumer pf.kref có sự kiện vào DLQ vì ErrNotYetProjected.
func runKRefRebuild(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := residency.CheckEnv(); err != nil {
		return err
	}
	direct := os.Getenv("AG_DB_DIRECT_URL")
	if direct == "" {
		direct = os.Getenv("AG_DB_URL")
	}
	if direct == "" {
		return errors.New("thiếu AG_DB_URL")
	}
	var pools []*pgxpool.Pool
	defer func() {
		for _, p := range pools {
			p.Close()
		}
	}()
	opened := map[string]*pgxpool.Pool{}
	open := func(role string) (*pgxpool.Pool, error) {
		if p, ok := opened[role]; ok {
			return p, nil
		}
		dsn, err := dbadmin.RoleURL(direct, migrations.PF, "", role)
		if err != nil {
			return nil, err
		}
		p, err := db.Open(ctx, dsn, db.Options{AppName: "worker-kref-rebuild", MaxConns: 2})
		if err != nil {
			return nil, err
		}
		pools = append(pools, p)
		opened[role] = p
		return p, nil
	}
	src, err := krefSources(open)
	if err != nil {
		return err
	}
	workerPool, err := open(dbadmin.AppWorker)
	if err != nil {
		return err
	}
	res, err := kref.New(workerPool, clock.System{}).Rebuild(ctx, src)
	if err != nil {
		return err
	}
	log.Info("đã dựng lại k_ref", "event", "kref.rebuild", "tenants", res.Tenants, "org", res.Rows[krefport.RelOrg],
		"node_closure", res.Rows[krefport.RelNodeClosure], "admin_unit", res.Rows[krefport.RelAdminUnit],
		"share_grant_active", res.Rows[krefport.RelShareGrantActive], "outcome", "ok")
	return nil
}
