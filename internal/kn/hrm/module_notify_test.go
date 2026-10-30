package hrm

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// fakeDirectory ghi lại mọi lần tra (tenant, tham số) và trả người nhận cố định.
type fakeDirectory struct {
	roles    map[string][]uuid.UUID // "role|dept" → người
	managers map[string][]uuid.UUID // đơn vị → lãnh đạo
	err      error
	calls    []string
	tenants  []uuid.UUID
	nodes    []uuid.UUID
}

func (d *fakeDirectory) RoleHolders(_ context.Context, tid uuid.UUID, role, dept string) ([]uuid.UUID, error) {
	d.calls, d.tenants = append(d.calls, "role:"+role+"|"+dept), append(d.tenants, tid)
	if d.err != nil {
		return nil, d.err
	}
	return d.roles[role+"|"+dept], nil
}

func (d *fakeDirectory) UnitManagers(_ context.Context, tid, node uuid.UUID, units []string) (map[string][]uuid.UUID, error) {
	d.calls, d.tenants, d.nodes = append(d.calls, "managers:"+strings.Join(units, ",")), append(d.tenants, tid), append(d.nodes, node)
	if d.err != nil {
		return nil, d.err
	}
	out := map[string][]uuid.UUID{}
	for _, u := range units {
		if m, ok := d.managers[u]; ok {
			out[u] = m
		}
	}
	return out, nil
}

var testAccess = port.Access{Department: "hanh_chinh_tong_hop", ManageBy: []string{"admin"}, ReadBy: []string{"kn_tinh"}}

func build(t *testing.T, dir port.NotifyDirectory, tid uuid.UUID, event string, payload any) ([]notifyport.NotificationDraft, error) {
	t.Helper()
	regs := NotificationBuilders(testAccess, dir)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range regs {
		if slices.Contains(r.Events, event) {
			return r.Build(context.Background(), tid, event, raw, time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC))
		}
	}
	t.Fatalf("không có builder cho %s", event)
	return nil, nil
}

func recipients(ds []notifyport.NotificationDraft) []uuid.UUID {
	var out []uuid.UUID
	for _, d := range ds {
		if d.RecipientRef == nil {
			continue
		}
		out = append(out, *d.RecipientRef)
	}
	return out
}

// Đăng ký đúng hợp đồng #48: loại thông báo là token gạch dưới (khóa DB notification_kind_key_shape), sự kiện outbox có
// namespace dấu chấm hợp lệ, một consumer thứ tự 17, builder phủ đúng các sự kiện đã đăng ký.
func TestNotificationRegistrations_NotifyHCTH(t *testing.T) {
	kindShape := regexp.MustCompile(`^[a-z][a-z_]{0,39}$`)
	kinds := NotificationKinds().Kinds
	if !slices.Equal(kinds, []string{port.KindPromotionDue, port.KindPlanReminder, port.KindAppraisal}) {
		t.Fatalf("kinds: %v", kinds)
	}
	for _, k := range kinds {
		if !kindShape.MatchString(k) || strings.Contains(k, ".") {
			t.Fatalf("kind %q sai khuôn", k)
		}
	}
	defs := NotificationConsumers().Definitions
	if len(defs) != 1 || defs[0].Name != port.NotifyConsumerHRM || defs[0].Order != 17 {
		t.Fatalf("consumer: %+v", defs)
	}
	events := defs[0].Events
	if !slices.Equal(events, []string{port.EventPromotionDueDetected, port.EventPlanReminded, port.EventAppraisalChanged}) {
		t.Fatalf("events: %v", events)
	}
	for _, e := range events {
		if err := outbox.ValidateType(e); err != nil {
			t.Fatal(err)
		}
	}
	var built []string
	for _, r := range NotificationBuilders(testAccess, &fakeDirectory{}) {
		if r.Build == nil || r.Plan != nil {
			t.Fatal("builder phải là Build")
		}
		built = append(built, r.Events...)
	}
	if !slices.Equal(built, events) {
		t.Fatalf("builder events: %v", built)
	}
}

// Nâng bậc: người nhận = Cấp Tỉnh của Phòng HCTH + Admin (lần gán còn hiệu lực), bỏ trùng; nội dung chỉ số đếm; tra người
// nhận đúng tenant của phong bì; lỗi tra → lỗi (consumer NACK, giao lại), không gửi chủ đề toàn tenant.
func TestPromotionDueBuilder_NotifyHCTH(t *testing.T) {
	tid := uuid.New()
	hcth, both, admin := uuid.New(), uuid.New(), uuid.New()
	dir := &fakeDirectory{roles: map[string][]uuid.UUID{
		"kn_tinh|hanh_chinh_tong_hop": {hcth, both, uuid.Nil},
		"admin|":                      {admin, both},
	}}
	p := port.PromotionDueDetectedV1{ScanID: uuid.New(), Day: "2026-10-12", Windows: []port.PromotionDueWindow{{Months: 1, Count: 0},
		{Months: 3, Count: 2}, {Months: 6, Count: 4}}, Overdue: 1, Fail: 1, Review: 0, Missing: 3}
	ds, err := build(t, dir, tid, port.EventPromotionDueDetected, p)
	if err != nil {
		t.Fatal(err)
	}
	got := recipients(ds)
	if len(ds) != 3 || !slices.Equal(got, []uuid.UUID{hcth, both, admin}) {
		t.Fatalf("người nhận: %v", got)
	}
	for _, tt := range dir.tenants {
		if tt != tid {
			t.Fatal("tra người nhận sai tenant")
		}
	}
	d := ds[0]
	if d.Kind != port.KindPromotionDue || d.DeepLink != "/kn/nhan-su/nang-bac" || d.SubjectRef == nil || *d.SubjectRef != p.ScanID ||
		d.Severity != notifyport.SeverityUrgent || d.Audience.National || len(d.Audience.Roles) != 0 {
		t.Fatalf("thông báo: %+v", d)
	}
	for _, want := range []string{"12/10/2026", "3 tháng: 2", "6 tháng: 4", "Quá hạn chưa xử lý: 1", "Chưa đủ điều kiện: 1", "Thiếu mốc xếp bậc: 3"} {
		if !strings.Contains(d.Title+" "+d.Body, want) {
			t.Fatalf("thiếu %q: %s | %s", want, d.Title, d.Body)
		}
	}

	// Không có người nhận → không thông báo (không mở rộng ra vai realm toàn tenant).
	if ds, err := build(t, &fakeDirectory{}, tid, port.EventPromotionDueDetected, p); err != nil || len(ds) != 0 {
		t.Fatalf("không người nhận: %v %v", ds, err)
	}
	// Lỗi tra người nhận → lỗi.
	boom := errors.New("directory down")
	if _, err := build(t, &fakeDirectory{err: boom}, tid, port.EventPromotionDueDetected, p); !errors.Is(err, boom) {
		t.Fatalf("lỗi tra: %v", err)
	}
	// Payload hỏng → lỗi.
	regs := NotificationBuilders(testAccess, dir)
	if _, err := regs[0].Build(context.Background(), tid, port.EventPromotionDueDetected, json.RawMessage(`{"day":1}`), time.Now()); err == nil {
		t.Fatal("payload hỏng phải lỗi")
	}
	// Nil directory → lỗi (đóng an toàn — không đoán người nhận).
	if _, err := build(t, nil, tid, port.EventPromotionDueDetected, p); err == nil {
		t.Fatal("nil directory phải lỗi")
	}
}

// Nhắc kế hoạch: người nhận = lãnh đạo của đúng các đơn vị chưa gửi tại Nút của phiên người nhắc; bỏ người nhắc, bỏ trùng.
func TestPlanReminderBuilder_NotifyHCTH(t *testing.T) {
	tid, node := uuid.New(), uuid.New()
	ldTT, ldCN, actor := uuid.New(), uuid.New(), uuid.New()
	dir := &fakeDirectory{managers: map[string][]uuid.UUID{
		"trong_trot": {ldTT, actor},
		"chan_nuoi":  {ldCN, ldTT},
		"hcth":       {uuid.New()}, // đã gửi — không có trong sự kiện
	}}
	p := port.PlanRemindedV1{ReminderID: uuid.New(), Year: 2027, PeriodRef: uuid.New(), NodeRef: node, Units: []string{"trong_trot", "chan_nuoi", "khong_ld"},
		DueAt: "2026-11-30T16:59:59Z", ActorRef: actor}
	ds, err := build(t, dir, tid, port.EventPlanReminded, p)
	if err != nil {
		t.Fatal(err)
	}
	if got := recipients(ds); !slices.Equal(got, []uuid.UUID{ldTT, ldCN}) {
		t.Fatalf("người nhận: %v", got)
	}
	if len(dir.nodes) != 1 || dir.nodes[0] != node || dir.tenants[0] != tid || dir.calls[0] != "managers:trong_trot,chan_nuoi,khong_ld" {
		t.Fatalf("tra lãnh đạo: %v %v %v", dir.calls, dir.nodes, dir.tenants)
	}
	d := ds[0]
	if d.Kind != port.KindPlanReminder || d.DeepLink != "/kn/nhan-su/ke-hoach-phong" || d.SubjectRef == nil || *d.SubjectRef != p.PeriodRef ||
		!strings.Contains(d.Title, "2027") || !strings.Contains(d.Body, "30/11/2026") {
		t.Fatalf("thông báo: %+v", d)
	}
	// Thiếu Nút hoặc đơn vị → lỗi (không suy toàn tenant).
	bad := p
	bad.NodeRef = uuid.Nil
	if _, err := build(t, dir, tid, port.EventPlanReminded, bad); err == nil {
		t.Fatal("thiếu Nút phải lỗi")
	}
	bad = p
	bad.Units = nil
	if _, err := build(t, dir, tid, port.EventPlanReminded, bad); err == nil {
		t.Fatal("thiếu đơn vị phải lỗi")
	}
	if _, err := build(t, &fakeDirectory{err: errors.New("x")}, tid, port.EventPlanReminded, p); err == nil {
		t.Fatal("lỗi tra phải lỗi")
	}
}
