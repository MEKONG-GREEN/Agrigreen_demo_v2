package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/porttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// fakeCheckpoint là điểm kiểm soát xuất giả (pf/metering chưa nối ở kiểm thử module).
type fakeCheckpoint struct {
	mu    sync.Mutex
	allow bool
	err   error
	seen  []meteringport.Egress
}

func (c *fakeCheckpoint) Pass(_ context.Context, e meteringport.Egress) (meteringport.Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, e)
	if c.err != nil {
		return meteringport.Decision{}, c.err
	}
	return meteringport.Decision{Allowed: c.allow}, nil
}

func (c *fakeCheckpoint) calls() []meteringport.Egress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]meteringport.Egress(nil), c.seen...)
}

func problemCode(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

// assertContract: phần tử chỉ có trường của hợp đồng (Validate mặc định từ chối trường lạ).
func assertContract(t *testing.T, product string, item any) []byte {
	t.Helper()
	p, err := dataproduct.Get(product)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(b); err != nil {
		t.Fatalf("%s không khớp hợp đồng: %v — %s", product, err, b)
	}
	return b
}

// G5 D-02 (hub_capacity v1, g5-conventions §6): nguồn pf/geo chỉ trả hub đã công bố qua van của
// tenant bên cấp, bậc sử dụng thay tấn tồn (không used/fill/lô/giá), khớp hợp đồng; tenant khác và
// lọc tỉnh khác → rỗng; con trỏ mờ phân trang. ADR-0024 (L-05): consumer van gọi Guard kênh valve
// mỗi bên cấp một lần (tenant dữ liệu = tenant phong bì, bên nhận = bên cấp, nguồn = event_id);
// observe không từ chối; enforce + từ chối → lỗi metering.egress_quota_exceeded (giao lại → DLQ),
// không ghi.
func TestHubCapacityProductAndValveEgress_G5(t *testing.T) {
	g := newG2(t)
	mkg := ids.New()
	second := ids.New()
	share := porttest.New()
	share.Grant(g.tenant, mkg, partyport.KindValveHub)
	share.Grant(second, mkg, partyport.KindValveHub)
	g.mod.UseSharing(share, share)
	cp := &fakeCheckpoint{allow: false}
	g.mod.UseEgress(cp, meteringport.ModeObserve, nil)
	h := g.mod.HubValveHandler()
	ctx := context.Background()
	run := func(env outbox.Envelope) (bool, error) {
		return inbox.Process(ctx, g.pool, port.HubValveConsumer, env, h)
	}
	eff := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	facility := func(hub uuid.UUID, code, province, kind string) map[string]any {
		return map[string]any{"hub_ref": hub.String(), "code": code, "name": "Kho MKG " + code, "kind": kind,
			"location":      map[string]any{"lat": json.Number("10.0312345"), "lng": json.Number("105.7654321")},
			"province_code": province, "status": "active", "capacity_tons": "1200.000000", "unit": "t", "effective_from": eff.Format(time.RFC3339)}
	}
	hubA, hubB := ids.New(), ids.New()
	envA := valveEnv(t, mkg, port.EventMKGHubFacilityPublished, facility(hubA, "MKG-KHO-A", "92", "warehouse"))
	if ok, err := run(envA); err != nil || !ok {
		t.Fatalf("observe + vượt hạn mức vẫn ghi: %v %v", ok, err)
	}
	calls := cp.calls()
	if len(calls) != 2 {
		t.Fatalf("mỗi bên cấp một Egress: %+v", calls)
	}
	for _, e := range calls {
		if e.Channel != meteringport.ChannelValve || e.SourceID != envA.EventID || e.DataTenantID != mkg ||
			e.CounterpartyKind != meteringport.CounterpartyTenant || e.Product != port.ContractHubFacility || e.Records != 1 || e.Bytes <= 0 {
			t.Fatalf("Egress van: %+v", e)
		}
	}
	if got := map[uuid.UUID]bool{calls[0].CounterpartyRef: true, calls[1].CounterpartyRef: true}; !got[g.tenant] || !got[second] {
		t.Fatalf("bên nhận = từng bên cấp: %+v", calls)
	}
	// Giao lặp phong bì: inbox chặn, không gọi thêm checkpoint (idempotent theo event_id ở cả hai lớp).
	if ok, err := run(envA); err != nil || ok {
		t.Fatalf("giao lặp: %v %v", ok, err)
	}
	if len(cp.calls()) != 2 {
		t.Fatalf("giao lặp không thêm Egress: %d", len(cp.calls()))
	}
	if ok, err := run(valveEnv(t, mkg, port.EventMKGHubFacilityPublished, facility(hubB, "MKG-HUB-B", "92", "hub"))); err != nil || !ok {
		t.Fatalf("hub B: %v %v", ok, err)
	}
	capEnv := valveEnv(t, mkg, port.EventMKGHubCapacityPublished, map[string]any{"hub_ref": hubA.String(), "as_of": eff.Add(time.Hour).Format(time.RFC3339),
		"capacity_tons": "1200.000000", "used_tons": "1100.000000", "available_tons": "100.000000", "unit": "t"})
	if ok, err := run(capEnv); err != nil || !ok {
		t.Fatalf("hub_capacity: %v %v", ok, err)
	}

	// enforce + từ chối → lỗi đúng mã, không ghi hub mới; checkpoint lỗi → metering.egress_unavailable.
	g.mod.UseEgress(cp, meteringport.ModeEnforce, nil)
	hubC := ids.New()
	if _, err := run(valveEnv(t, mkg, port.EventMKGHubFacilityPublished, facility(hubC, "MKG-KHO-C", "92", "warehouse"))); problemCode(err) != "metering.egress_quota_exceeded" {
		t.Fatalf("enforce + từ chối → metering.egress_quota_exceeded: %v", err)
	}
	cp.err = errors.New("metering down")
	if _, err := run(valveEnv(t, mkg, port.EventMKGHubFacilityPublished, facility(hubC, "MKG-KHO-C", "92", "warehouse"))); problemCode(err) != "metering.egress_unavailable" {
		t.Fatalf("enforce + checkpoint lỗi → metering.egress_unavailable: %v", err)
	}
	cp.err = nil
	g.mod.UseEgress(nil, meteringport.ModeEnforce, nil)

	src := g.mod.HubCapacitySource()
	items, next, err := src.HubCapacities(ctx, g.tenant, "", "", 0)
	if err != nil || next != "" || len(items) != 2 {
		t.Fatalf("hub của bên cấp (hub C bị chặn không có): %+v %q %v", items, next, err)
	}
	byRef := map[string]apiclientport.HubCapacityV1{}
	for _, it := range items {
		raw := assertContract(t, apiclientport.ProductHubCapacity, it)
		for _, banned := range []string{"used", "fill", "available", "lot", "price", "source", "1100"} {
			if strings.Contains(strings.ToLower(string(raw)), banned) {
				t.Fatalf("phần tử không được có %q: %s", banned, raw)
			}
		}
		byRef[it.HubRef] = it
	}
	a, b := byRef[hubA.String()], byRef[hubB.String()]
	if a.Kind != "warehouse" || a.UtilizationBand != app.BandFull || a.CapacityTons == nil || *a.CapacityTons != "1200.00" ||
		a.ProvinceCode != "92" || a.Status != "active" || !strings.HasPrefix(a.Location.Lat.String(), "10.03123") {
		t.Fatalf("hub A (1100/1200 → full): %+v", a)
	}
	if b.Kind != "hub" || b.UtilizationBand != app.BandUnknown {
		t.Fatalf("hub B chưa có số sức chứa → unknown: %+v", b)
	}
	// Phân trang bằng con trỏ mờ.
	p1, n1, err := src.HubCapacities(ctx, g.tenant, "92", "", 1)
	if err != nil || len(p1) != 1 || n1 == "" {
		t.Fatalf("trang 1: %+v %q %v", p1, n1, err)
	}
	p2, n2, err := src.HubCapacities(ctx, g.tenant, "92", n1, 1)
	if err != nil || len(p2) != 1 || n2 != "" || p2[0].HubRef == p1[0].HubRef {
		t.Fatalf("trang 2: %+v %q %v", p2, n2, err)
	}
	// Lọc tỉnh khác, tenant khác (kể cả tenant MKG phong bì) → rỗng; tham số sai → lỗi kiểm dữ liệu.
	for name, tid := range map[string]uuid.UUID{"tenant khác": ids.New(), "tenant phong bì MKG": mkg} {
		if it, _, err := src.HubCapacities(ctx, tid, "", "", 0); err != nil || len(it) != 0 {
			t.Fatalf("%s: %+v %v", name, it, err)
		}
	}
	if it, _, err := src.HubCapacities(ctx, g.tenant, "89", "", 0); err != nil || len(it) != 0 {
		t.Fatalf("tỉnh khác: %+v %v", it, err)
	}
	if _, _, err := src.HubCapacities(ctx, g.tenant, "9", "", 0); err == nil {
		t.Fatalf("mã tỉnh sai: %v", err)
	}
	if _, _, err := src.HubCapacities(ctx, g.tenant, "", "!!", 0); err == nil {
		t.Fatal("con trỏ sai phải lỗi")
	}
	if _, _, err := src.HubCapacities(ctx, uuid.Nil, "", "", 0); err == nil {
		t.Fatal("thiếu tenant dữ liệu phải lỗi")
	}
}

// G5 D-02: bậc sử dụng theo lớp màu widget (GIS US-HUB-01, ngưỡng 60/85 mặc định).
func TestUtilizationBand_G5_D02(t *testing.T) {
	d := decimal.RequireFromString
	for _, c := range []struct{ used, want string }{{"0", app.BandNormal}, {"59", app.BandNormal}, {"60", app.BandWarning}, {"85", app.BandFull}, {"130", app.BandFull}} {
		class := domain.CapacityClass(domain.FillPct(d(c.used), d("100")), domain.DefaultHubWarnPct, domain.DefaultHubFullPct)
		if got := app.UtilizationBand(class); got != c.want {
			t.Errorf("used %s/100 → %s, muốn %s", c.used, got, c.want)
		}
	}
	if app.UtilizationBand(domain.CapacityUnknown) != app.BandUnknown {
		t.Fatal("chưa có số → unknown")
	}
}

// G5 (collection_volume): PlotFacts trả xã/HTX/nông hộ/diện tích của nhiều thửa theo tenant trong
// context; thửa lạ và tenant khác không có trong kết quả.
func TestPlotFacts_G5(t *testing.T) {
	f := newFixture(t)
	p1, _ := f.national().createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	pf := f.mod.PlotFacts()
	got, err := pf.PlotFacts(tenant.WithID(context.Background(), f.tenant), []uuid.UUID{p1, ids.New()})
	if err != nil || len(got) != 1 {
		t.Fatalf("PlotFacts: %+v %v", got, err)
	}
	if p := got[p1]; p.CommuneCode != "31117" || p.CooperativeRef != f.coopA || !p.AreaHa.IsPositive() {
		t.Fatalf("dữ kiện thửa: %+v", p)
	}
	if other, err := pf.PlotFacts(tenant.WithID(context.Background(), ids.New()), []uuid.UUID{p1}); err != nil || len(other) != 0 {
		t.Fatalf("tenant khác: %+v %v", other, err)
	}
	if _, err := pf.PlotFacts(context.Background(), []uuid.UUID{p1}); err == nil {
		t.Fatal("thiếu tenant trong context phải lỗi")
	}
}
