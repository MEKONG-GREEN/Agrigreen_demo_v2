package handler_test

// P0-POOL-KN-2 (#139): job cảnh báo CNTS (nhắc nộp biểu kỳ, trễ liên tiếp, quá hạn xử lý) trên CHUNG một pool vai đăng nhập
// app_platform nhỏ như gốc lắp ráp/worker thật. Đơn vị hành chính (KN platformread), danh bạ cán bộ (pf/accounts) và bài nộp
// (cap/forms) là bản giả MỞ GIAO DỊCH NGẮN trên cùng pool như cài đặt thật. Trước sửa: giao dịch nhắc nộp giữ một kết nối rồi
// tra tỉnh của xã + cán bộ nhận nhắc qua port → nhiều tenant chạy cùng lúc làm cạn pool.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type poolToucher struct{ pool *pgxpool.Pool }

func (p poolToucher) touch(ctx context.Context) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	_, err = tx.Exec(ctx, "SELECT 1")
	return err
}

type touchUnits struct{ poolToucher }

func (u touchUnits) Get(ctx context.Context, code string, at time.Time) (partyport.AdminUnit, error) {
	if err := u.touch(ctx); err != nil {
		return partyport.AdminUnit{}, err
	}
	return units{}.Get(ctx, code, at)
}
func (u touchUnits) Descendants(ctx context.Context, _ string, _ time.Time) ([]string, error) {
	return nil, u.touch(ctx)
}
func (u touchUnits) Current(ctx context.Context, _ string, _ time.Time) ([]string, error) {
	return nil, u.touch(ctx)
}

type touchDir struct {
	dir
	poolToucher
}

func (d touchDir) Officers(ctx context.Context, tid uuid.UUID, dept, province string) ([]accport.KNOfficer, error) {
	if err := d.touch(ctx); err != nil {
		return nil, err
	}
	return d.dir.Officers(ctx, tid, dept, province)
}

func (d touchDir) DutyHolders(ctx context.Context, tid uuid.UUID, role, dept, duty string) ([]uuid.UUID, error) {
	if err := d.touch(ctx); err != nil {
		return nil, err
	}
	return d.dir.DutyHolders(ctx, tid, role, dept, duty)
}

type touchFeed struct {
	*feed
	poolToucher
}

func (f touchFeed) DuePeriods(ctx context.Context, tid uuid.UUID, profile string, from, to time.Time) ([]formsport.PeriodProgress, error) {
	if err := f.touch(ctx); err != nil {
		return nil, err
	}
	return f.feed.DuePeriods(ctx, tid, profile, from, to)
}

// TestP0PoolKN2_CNTSAlertJobNoNestedTx: job cảnh báo CNTS của nhiều tenant chạy cùng lúc (worker nhiều leader/tenant, cùng
// pool với consumer) trên pool 2 kết nối: nhắc nộp trước hạn + trễ liên tiếp phải xong trong hạn, đúng người nhận.
func TestP0PoolKN2_CNTSAlertJobNoNestedTx(t *testing.T) {
	const poolSize = 2
	const tenants = 5
	env := pgtest.Get(t)
	dsn, err := dbadmin.RoleURL(env.PoolURL, migrations.PF, env.Suffix, dbadmin.AppPlatform)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := db.Open(context.Background(), dsn, db.Options{ViaPgBouncer: true, MaxConns: poolSize, AppName: "pgtest-p0poolkn2"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	tp := poolToucher{pool: shared}
	d := dir{xa1: ids.New(), tinh: ids.New(), lead: ids.New()}
	fd := &feed{}
	// Kỳ W42 hạn 06:00 14/10 giờ VN (t0 + 20 giờ — nhắc trước hạn 24 giờ), W41, W40 đã quá hạn (nhắc lặp 08:00): xã 30001 chưa
	// gửi kỳ nào → 3 lượt nhắc, một cảnh báo trễ liên tiếp (ngưỡng mặc định 2 kỳ).
	due := t0.Add(20 * time.Hour)
	for i, key := range []string{"2027-W42", "2027-W41", "2027-W40"} {
		fd.due = append(fd.due, formsport.PeriodProgress{ID: ids.New(), Profile: "kn_cnts", Label: key, TemplateCode: "CNTS-BM04",
			DueAt: due.Add(-time.Duration(i) * 7 * 24 * time.Hour), Status: "open", PeriodKey: key,
			Units: []formsport.UnitProgress{{Unit: "30001", Parent: "30001", Status: "not_submitted"}}})
	}
	mod := extension.New(shared, clock.NewFixed(t0), extension.Deps{Units: touchUnits{tp}})
	td := touchDir{dir: d, poolToucher: tp}
	mod.UseCNTSAlerts(extension.CNTSAlertDeps{Feed: touchFeed{feed: fd, poolToucher: tp}, Directory: td, Holders: td})

	tids := make([]uuid.UUID, tenants)
	for i := range tids {
		tids[i] = ids.New()
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var fails []string
	start := make(chan struct{})
	for _, tid := range tids {
		wg.Add(1)
		go func(tid uuid.UUID) {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r, err := mod.CNTSAlertJobRun(ctx, tid, t0)
			if err != nil || r.Reminders != 3 || r.Streaks != 1 {
				mu.Lock()
				fails = append(fails, fmt.Sprintf("tenant %s: %+v err=%v", tid, r, err))
				mu.Unlock()
			}
		}(tid)
	}
	began := time.Now()
	close(start)
	wg.Wait()
	for _, f := range fails {
		t.Error(f)
	}
	if len(fails) > 0 {
		t.Fatalf("P0-POOL-KN-2: %d/%d job cảnh báo CNTS song song hỏng trên pool %d kết nối sau %s", len(fails), tenants, poolSize,
			time.Since(began).Round(time.Millisecond))
	}
	t.Logf("P0-POOL-KN-2: %d job cảnh báo CNTS song song trên pool %d kết nối xong sau %s", tenants, poolSize, time.Since(began).Round(time.Millisecond))
	// Người nhận nhắc nộp: cán bộ xã 30001 (tra trước giao dịch, như cũ).
	admin := env.AdminPool(t, migrations.PF)
	var recips string
	if err := admin.QueryRow(context.Background(), `SELECT payload->>'recipient_refs' FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2 LIMIT 1`,
		tids[0], port.EventCNTSFormReminded).Scan(&recips); err != nil {
		t.Fatal(err)
	}
	if recips != `["`+d.xa1.String()+`"]` {
		t.Fatalf("người nhận nhắc nộp: %s", recips)
	}
}
