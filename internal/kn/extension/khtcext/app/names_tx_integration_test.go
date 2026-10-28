package app_test

// Hồi quy #32 (Codex P1, W11.67): danh sách, lịch sử và lưu số liệu nguồn ngoài KHÔNG gọi port ActorNames (module tài
// khoản — Keycloak) khi đang giữ giao dịch/kết nối KN (AGENTS §9.1, P0-POOL-KN #125/#139). Pool một kết nối: port gọi
// trong giao dịch sẽ thấy kết nối đang bị mượn, và nếu tự mở giao dịch trên cùng pool sẽ chờ tới hết hạn.

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/domain"
	pstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// namesProbe là port ActorNames giả: ghi số kết nối pool đang bị mượn mỗi lần được gọi, rồi tự mở một giao dịch ngắn
// trên CÙNG pool (như port thật đọc danh bạ) — gọi trong giao dịch KN thì pool một kết nối hết chỗ.
type namesProbe struct {
	pool *pgxpool.Pool
	db   tenant.Beginner
	tid  uuid.UUID
	fail bool

	mu    sync.Mutex
	held  []int32
	errs  []error
	names map[uuid.UUID]string
}

func (p *namesProbe) DisplayNames(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	held := p.pool.Stat().AcquiredConns()
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := tenant.WithTenantTx(cctx, p.db, p.tid, func(tx pgx.Tx) error { _, e := tx.Exec(cctx, "SELECT 1"); return e })
	p.mu.Lock()
	p.held = append(p.held, held)
	p.errs = append(p.errs, err)
	p.mu.Unlock()
	if p.fail {
		return nil, errors.New("keycloak không phản hồi")
	}
	out := map[uuid.UUID]string{}
	for _, r := range refs {
		if n, ok := p.names[r]; ok {
			out[r] = n
		}
	}
	return out, nil
}

func (p *namesProbe) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.held, p.errs = nil, nil
}

func (p *namesProbe) check(t *testing.T, what string) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.held) == 0 {
		t.Fatalf("%s: port ActorNames không được gọi", what)
	}
	for i, n := range p.held {
		if n != 0 {
			t.Errorf("%s: ActorNames gọi khi còn giữ %d kết nối KN (giao dịch chưa đóng)", what, n)
		}
		if p.errs[i] != nil {
			t.Errorf("%s: port tra tên không lấy được kết nối pool: %v", what, p.errs[i])
		}
	}
}

func TestKHTCExternalNamesOutsideTx_Issue32(t *testing.T) {
	env := pgtest.Get(t)
	sized := env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	mdb := db.ForModule(sized, extension.DBModule)
	tid, actor := ids.New(), ids.New()
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	probe := &namesProbe{pool: sized, db: mdb, tid: tid, names: map[uuid.UUID]string{actor: "Nguyễn Văn KHTC"}}
	svc := &app.Service{DB: mdb, Clock: clock.NewFixed(now), Names: func() partyport.ActorNames { return probe }}

	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	k := authz.KNContext{ID: ids.New(), OrgRef: ids.New(), Role: authz.KNTinh, Department: partyport.DeptKeHoachTaiChinh, Scope: authz.Scope{Units: []string{"89"}}}
	pr := authz.Principal{ActorRef: actor, TenantID: tid, MFA: true, Roles: []string{"kn_user"}, KN: &k}
	selected, err := registry.ResolveSelected(context.Background(), pr)
	if err != nil {
		t.Fatal(err)
	}
	pr.SelectedSession = &selected
	ctx, cancel := context.WithTimeout(authz.WithPrincipal(context.Background(), pr), 60*time.Second)
	defer cancel()

	// Kỳ tháng 9/2026 còn mở.
	key := "2026-09"
	period := pstore.Period{ID: ids.New(), Type: "month", Year: 2026, Key: &key, Name: "Tháng 9/2026",
		From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), Cutoff: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
		DueAt: now, ExportDueAt: now, MaxSubmissions: 3, Departments: []string{partyport.DeptKeHoachTaiChinh}, FormsPeriodRef: ids.New(),
		TemplateCode: "KHTC-M1", Rule: "test", ActorRef: actor, EventTime: now}
	if err := tenant.WithTenantTx(ctx, mdb, tid, func(tx pgx.Tx) error { return pstore.InsertPeriod(ctx, tx, tid, &period, "p-"+period.ID.String()) }); err != nil {
		t.Fatalf("seed kỳ: %v", err)
	}
	ind, err := svc.AddIndicator(ctx, domain.IndicatorInput{Name: "Diện tích nuôi tôm nước lợ", Unit: "ha", Group: "Sản xuất"}, "ind-1")
	if err != nil {
		t.Fatalf("thêm chỉ tiêu: %v", err)
	}
	in := app.SaveInput{FigureInput: domain.FigureInput{PeriodID: period.ID, IndicatorID: ind.ID, Value: "26 400", Agency: "Chi cục Thủy sản",
		Document: "CV 12/CCTS", SourceDate: "2026-10-01"}}

	t.Run("save mới", func(t *testing.T) {
		probe.reset()
		v, err := svc.Save(ctx, in, "save-1")
		if err != nil {
			t.Fatalf("lưu: %v", err)
		}
		if v.ActorName != "Nguyễn Văn KHTC" || v.Value != "26400" {
			t.Fatalf("dòng trả về: actor_name=%q value=%q", v.ActorName, v.Value)
		}
		probe.check(t, "Save")
	})
	t.Run("save phát lại", func(t *testing.T) {
		probe.reset()
		v, err := svc.Save(ctx, in, "save-1")
		if err != nil || v.ActorName != "Nguyễn Văn KHTC" || v.Version != 1 {
			t.Fatalf("phát lại: v=%+v err=%v", v, err)
		}
		probe.check(t, "Save replay")
	})
	t.Run("list", func(t *testing.T) {
		probe.reset()
		vs, err := svc.List(ctx, app.Query{Year: 2026})
		if err != nil || len(vs) != 1 || vs[0].ActorName != "Nguyễn Văn KHTC" {
			t.Fatalf("danh sách: %+v err=%v", vs, err)
		}
		probe.check(t, "List")
	})
	t.Run("history", func(t *testing.T) {
		probe.reset()
		hs, err := svc.History(ctx, 2026, nil, 50)
		if err != nil || len(hs) != 1 || hs[0].ActorName != "Nguyễn Văn KHTC" {
			t.Fatalf("lịch sử: %+v err=%v", hs, err)
		}
		probe.check(t, "History")
	})
	// Port tra tên lỗi: số liệu vẫn lưu (đã commit trước khi tra tên), tên để trống — web hiện nhãn dự phòng.
	t.Run("port tên lỗi", func(t *testing.T) {
		probe.reset()
		probe.fail = true
		defer func() { probe.fail = false }()
		edit := in
		edit.Value, edit.Overwrite, edit.Reason = "26 500", true, "Chi cục gửi số liệu điều chỉnh"
		v, err := svc.Save(ctx, edit, "save-2")
		if err != nil || v.Value != "26500" || v.ActorName != "" || v.Version != 2 {
			t.Fatalf("lưu khi port tên lỗi: v=%+v err=%v", v, err)
		}
		probe.check(t, "Save khi port lỗi")
		probe.reset()
		vs, err := svc.List(ctx, app.Query{Year: 2026})
		if err != nil || len(vs) != 1 || vs[0].Value != "26500" || vs[0].ActorName != "" {
			t.Fatalf("danh sách khi port lỗi: %+v err=%v", vs, err)
		}
		hs, err := svc.History(ctx, 2026, nil, 50)
		if err != nil || len(hs) != 2 || hs[0].ActorName != "" {
			t.Fatalf("lịch sử khi port lỗi: %+v err=%v", hs, err)
		}
		probe.check(t, "List/History khi port lỗi")
	})
}
