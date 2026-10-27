package notify_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

var errFixtureHRMLookup = errors.New("fixture hrm directory unavailable")

// hrmDirectory là danh bạ người nhận HCTH giả: mỗi lần tra mở MỘT giao dịch trên cùng pool cỡ 1 với consumer — nếu builder
// gọi trong giao dịch inbox thì lần tra này không có kết nối (#143: I/O ngoài chạy ngoài giao dịch).
type hrmDirectory struct {
	mu       sync.Mutex
	pool     *pgxpool.Pool
	fail     bool
	roles    map[string][]uuid.UUID
	managers map[string][]uuid.UUID
	tenants  []uuid.UUID
	nodes    []uuid.UUID
}

func (d *hrmDirectory) touch(ctx context.Context, tid uuid.UUID) error {
	d.mu.Lock()
	d.tenants = append(d.tenants, tid)
	d.mu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := d.pool.Begin(cctx)
	if err != nil {
		return err
	}
	_ = tx.Rollback(cctx)
	if d.fail {
		return errFixtureHRMLookup
	}
	return nil
}

func (d *hrmDirectory) RoleHolders(ctx context.Context, tid uuid.UUID, role, dept string) ([]uuid.UUID, error) {
	if err := d.touch(ctx, tid); err != nil {
		return nil, err
	}
	return d.roles[role+"|"+dept], nil
}

func (d *hrmDirectory) UnitManagers(ctx context.Context, tid, node uuid.UUID, units []string) (map[string][]uuid.UUID, error) {
	if err := d.touch(ctx, tid); err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.nodes = append(d.nodes, node)
	d.mu.Unlock()
	out := map[string][]uuid.UUID{}
	for _, u := range units {
		if m, ok := d.managers[u]; ok {
			out[u] = m
		}
	}
	return out, nil
}

func hrmOfKind(xs []map[string]any, kind string) []map[string]any {
	var out []map[string]any
	for _, x := range xs {
		if x["kind"] == kind {
			out = append(out, x)
		}
	}
	return out
}

// NOTIFY-HCTH (#48): consumer pf.notify.hrm chạy trên PostgreSQL thật với đăng ký do pf/hrm sở hữu — loại thông báo
// hrm_* ghi được (khóa DB), tra người nhận NGOÀI giao dịch inbox (pool cỡ 1 không treo), lỗi tra → NACK không ghi, thử lại
// giao đúng một lần, giao lặp bị inbox bỏ qua (idempotent theo event_id), đúng tenant/người nhận/Nút; người nhắc không tự
// nhận; tenant khác không thấy.
func TestHRMNotificationConsumer_NotifyHCTH(t *testing.T) {
	f := newFixture(t)
	env := pgtest.Get(t)
	sized := env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	sizedDB := db.ForModule(sized, notify.DBModule)
	hcth := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNProvinceAdmin)
	admin := f.user(authz.Scope{All: true}, partyport.RoleKNProvinceAdmin)
	ldCN := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNProvinceAdmin)
	ldTT := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNProvinceAdmin)
	bystander := f.user(authz.Scope{Units: []string{prov91}}, partyport.RoleKNProvinceAdmin)
	foreign := f.asActor(ids.New(), ids.New(), hcth.actor, partyport.RoleKNProvinceAdmin)
	dir := &hrmDirectory{pool: sized, fail: true,
		roles:    map[string][]uuid.UUID{"kn_tinh|hanh_chinh_tong_hop": {hcth.actor}, "admin|": {admin.actor, hcth.actor}},
		managers: map[string][]uuid.UUID{"chan_nuoi": {ldCN.actor, hcth.actor}, "trong_trot": {ldTT.actor}}}
	acc := hrmport.Access{Department: "hanh_chinh_tong_hop", ManageBy: []string{"admin"}, ReadBy: []string{"kn_tinh"}}
	mod := notify.New(sized, f.clk, notify.Deps{Scope: f.scope, Kinds: []notifyport.KindRegistration{hrm.NotificationKinds()},
		Consumers: []notifyport.ConsumerRegistration{hrm.NotificationConsumers()}, Builders: hrm.NotificationBuilders(acc, dir)})

	// 1) Nâng bậc: lỗi tra → lỗi, không ghi; thử lại → giao một lần; giao lặp → bỏ qua.
	scan := ids.New()
	promo := f.event(f.tenant, hrmport.EventPromotionDueDetected, hrmport.PromotionDueDetectedV1{ScanID: scan, Day: "2026-10-12",
		Windows: []hrmport.PromotionDueWindow{{Months: 1, Count: 1}, {Months: 3, Count: 2}, {Months: 6, Count: 2}}, Overdue: 1, Fail: 1})
	if _, err := inbox.Process(context.Background(), sizedDB, hrmport.NotifyConsumerHRM, promo, mod.EventHandler()); !errors.Is(err, errFixtureHRMLookup) {
		t.Fatalf("lỗi tra người nhận: %v", err)
	}
	if n := len(hrmOfKind(hcth.inbox(), hrmport.KindPromotionDue)); n != 0 {
		t.Fatal("lỗi tra vẫn ghi thông báo")
	}
	dir.fail = false
	if ok, err := inbox.Process(context.Background(), sizedDB, hrmport.NotifyConsumerHRM, promo, mod.EventHandler()); err != nil || !ok {
		t.Fatalf("thử lại: %v %v", ok, err)
	}
	if ok, err := inbox.Process(context.Background(), sizedDB, hrmport.NotifyConsumerHRM, promo, mod.EventHandler()); err != nil || ok {
		t.Fatalf("giao lặp phải bỏ qua: %v %v", ok, err)
	}
	for _, c := range []caller{hcth, admin} {
		got := hrmOfKind(c.inbox(), hrmport.KindPromotionDue)
		if len(got) != 1 || got[0]["deep_link"] != "/kn/nhan-su/nang-bac" || got[0]["severity"] != "urgent" ||
			!strings.Contains(got[0]["title"].(string), "12/10/2026") || !strings.Contains(got[0]["body"].(string), "3 tháng: 2") {
			t.Fatalf("thông báo nâng bậc: %v", got)
		}
	}
	if len(hrmOfKind(bystander.inbox(), hrmport.KindPromotionDue)) != 0 || len(foreign.inbox()) != 0 {
		t.Fatal("thông báo nâng bậc lọt sang người/tenant khác")
	}

	// 2) Nhắc kế hoạch: lãnh đạo các đơn vị chưa gửi tại Nút; người nhắc (hcth) không tự nhận; đơn vị khác không nhận.
	node, period := ids.New(), ids.New()
	remind := f.event(f.tenant, hrmport.EventPlanReminded, hrmport.PlanRemindedV1{ReminderID: ids.New(), Year: 2027, PeriodRef: period,
		NodeRef: node, Units: []string{"chan_nuoi"}, DueAt: "2026-11-30T16:59:59Z", ActorRef: hcth.actor})
	if ok, err := inbox.Process(context.Background(), sizedDB, hrmport.NotifyConsumerHRM, remind, mod.EventHandler()); err != nil || !ok {
		t.Fatalf("nhắc kế hoạch: %v %v", ok, err)
	}
	got := hrmOfKind(ldCN.inbox(), hrmport.KindPlanReminder)
	if len(got) != 1 || got[0]["deep_link"] != "/kn/nhan-su/ke-hoach-phong" || !strings.Contains(got[0]["body"].(string), "30/11/2026") {
		t.Fatalf("lãnh đạo Chăn nuôi: %v", got)
	}
	if len(hrmOfKind(hcth.inbox(), hrmport.KindPlanReminder)) != 0 || len(hrmOfKind(ldTT.inbox(), hrmport.KindPlanReminder)) != 0 {
		t.Fatal("người nhắc hoặc đơn vị khác nhận nhắc")
	}

	dir.mu.Lock()
	defer dir.mu.Unlock()
	for _, tid := range dir.tenants {
		if tid != f.tenant {
			t.Fatal("tra người nhận sai tenant")
		}
	}
	if len(dir.nodes) != 1 || dir.nodes[0] != node {
		t.Fatalf("Nút tra lãnh đạo: %v", dir.nodes)
	}
}
