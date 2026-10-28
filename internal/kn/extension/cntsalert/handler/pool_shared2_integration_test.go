package handler_test

// P0-POOL-SHARED-2 (#143, bàn giao từ #139): consumer cảnh báo CNTS (pf.extension.cnts_alert) KHÔNG gọi port module khác
// (bài nộp cap/forms, đơn vị hành chính pf/party, danh bạ cán bộ pf/accounts) khi đang giữ giao dịch inbox — mỗi port thật
// tự mở giao dịch trên CÙNG pool (ADR-0028), gọi trong giao dịch inbox là giữ một kết nối chờ kết nối thứ hai.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// probe ghi số kết nối đang mượn của pool mỗi lần một port module khác được gọi.
type probe struct {
	pool  *pgxpool.Pool
	mu    sync.Mutex
	calls []string
	held  []int32
}

func (p *probe) hit(what string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, what)
	p.held = append(p.held, p.pool.Stat().AcquiredConns())
}

func (p *probe) check(t *testing.T) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.calls) == 0 {
		t.Fatal("không port nào được gọi")
	}
	for i, n := range p.held {
		if n != 0 {
			t.Errorf("P0-POOL-SHARED-2: %s gọi trong lúc giữ %d kết nối pool (giao dịch inbox mở)", p.calls[i], n)
		}
	}
}

type probeFeed struct {
	*feed
	p *probe
}

func (f probeFeed) Recorded(ctx context.Context, tid, id uuid.UUID) (*formsport.RecordedSubmission, error) {
	f.p.hit("SubmissionFeed.Recorded")
	return f.feed.Recorded(ctx, tid, id)
}

func (f probeFeed) Latest(ctx context.Context, tid uuid.UUID, q formsport.SubmissionQuery) ([]formsport.RecordedSubmission, error) {
	f.p.hit("SubmissionFeed.Latest")
	return f.feed.Latest(ctx, tid, q)
}

type probeUnits struct {
	units
	p *probe
}

func (u probeUnits) Get(ctx context.Context, code string, at time.Time) (partyport.AdminUnit, error) {
	u.p.hit("AdminUnits.Get")
	return u.units.Get(ctx, code, at)
}

type probeDir struct {
	dir
	p *probe
}

func (d probeDir) Officers(ctx context.Context, tid uuid.UUID, dept, province string) ([]accport.KNOfficer, error) {
	d.p.hit("Directory.Officers")
	return d.dir.Officers(ctx, tid, dept, province)
}

func (d probeDir) DutyHolders(ctx context.Context, tid uuid.UUID, role, dept, duty string) ([]uuid.UUID, error) {
	d.p.hit("Holders.DutyHolders")
	return d.dir.DutyHolders(ctx, tid, role, dept, duty)
}

func TestCNTSAlertPortsOutsideInboxTx_P0PoolShared2(t *testing.T) {
	env := pgtest.Get(t)
	sized := env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	pr := &probe{pool: sized}
	f := &fix{t: t, tenant: ids.New(), feed: &feed{}, dir: dir{xa1: ids.New(), tinh: ids.New(), lead: ids.New()}}
	f.pool = env.AdminPool(t, migrations.PF)
	f.mod = extension.New(sized, clock.NewFixed(t0), extension.Deps{Units: probeUnits{p: pr}})
	pd := probeDir{dir: f.dir, p: pr}
	f.mod.UseCNTSAlerts(extension.CNTSAlertDeps{Feed: probeFeed{feed: f.feed, p: pr}, Directory: pd, Holders: pd})
	f.process = func(e outbox.Envelope) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := inbox.Process(ctx, db.ForModule(sized, extension.DBModule), port.CNTSAlertConsumer, e, f.mod.CNTSAlertHandler()); err != nil {
			t.Fatalf("consumer: %v", err)
		}
	}
	// BM-04 W40 rồi W41 (đàn heo 100 → 150, vượt ngưỡng 15%) → cảnh báo biến động đàn tới cán bộ xã + người phụ trách biểu.
	f.submit("CNTS-BM04", "30001", "30001", "commune", ids.New(), "2027-W40", "2027-09-28", "2027-10-04", 1, herd("100", "0"), false, f.dir.xa1)
	f.submit("CNTS-BM04", "30001", "30001", "commune", ids.New(), "2027-W41", "2027-10-05", "2027-10-11", 1, herd("150", "0"), false, f.dir.xa1)
	pr.check(t)
	alerts := f.outbox(port.EventCNTSAlertChanged)
	if len(alerts) == 0 {
		t.Fatal("không sinh cảnh báo biến động đàn")
	}
	if recips, _ := alerts[len(alerts)-1]["recipient_refs"].([]any); len(recips) == 0 {
		t.Fatalf("cảnh báo không có người nhận (danh bạ đọc ngoài giao dịch): %v", alerts[len(alerts)-1])
	}
}
