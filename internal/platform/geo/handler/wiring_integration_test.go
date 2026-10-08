package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/porttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

type fakeFarmerLinks struct {
	mu sync.Mutex
	m  map[uuid.UUID][]uuid.UUID
}

func (f *fakeFarmerLinks) HouseholdsOf(_ context.Context, actor uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.m[actor], nil
}

func valveEnv(t *testing.T, tid uuid.UUID, typ string, payload map[string]any) outbox.Envelope {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return outbox.Envelope{EventID: ids.New(), TenantID: tid, EventType: typ, SchemaVersion: 1, OccurredAt: time.Now().UTC(), Producer: "mkg/hub", Payload: b}
}

// GIS FN-04/US-HUB-01 + van công bố MKG (R-03): consumer pf.geo.hub_valve kiểm payload
// theo hợp đồng hub_facility / hub_capacity lúc chạy (trường lạ → từ chối, không ghi),
// ghi mô hình đọc hub qua HubSink, idempotent theo event_id và phiên bản effective_from.
// ADR-0020: phong bì của tenant MKG riêng được ghi vào tenant bên cấp (cấp quyền valve_hub);
// tenant phong bì chưa có cấp quyền → party.valve_share_grant_missing (DLQ), không ghi.
func TestHubValveConsumerValidatesContractAndIsIdempotent(t *testing.T) {
	g := newG2(t)
	mkg := ids.New()
	share := porttest.New()
	share.Grant(g.tenant, mkg, partyport.KindValveHub)
	g.mod.UseSharing(share)
	h := g.mod.HubValveHandler()
	ctx := context.Background()
	run := func(env outbox.Envelope) (bool, error) {
		return inbox.Process(ctx, g.pool, port.HubValveConsumer, env, h)
	}
	dLat, dLon := degPerMetre(baseLat)
	hub := ids.New()
	eff := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	facility := func(name string, at time.Time, extra map[string]any) map[string]any {
		m := map[string]any{"hub_ref": hub.String(), "code": "MKG-KHO-01", "name": name, "kind": "warehouse",
			"location":      map[string]any{"lat": json.Number(decimal.NewFromFloat(baseLat + 300*dLat).String()), "lng": json.Number(decimal.NewFromFloat(baseLon + 300*dLon).String())},
			"province_code": "92", "status": "active", "capacity_tons": "1200.000000", "unit": "t", "effective_from": at.Format(time.RFC3339)}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	// Trường ngoài hợp đồng (vd tồn theo lô) → van từ chối, không ghi.
	if _, err := run(valveEnv(t, mkg, port.EventMKGHubFacilityPublished, facility("Kho MKG", eff, map[string]any{"lots": []any{}}))); err == nil {
		t.Fatal("trường ngoài hợp đồng phải bị từ chối")
	}
	widget := func() map[string]any {
		t.Helper()
		r := g.national().appDo(http.MethodGet, g2BBox("/v1/geo/capacity-widgets"), nil)
		expect(t, r, http.StatusOK, "widget")
		for _, it := range list(r.body, "items") {
			if str(it, "ref") == hub.String() {
				return it.(map[string]any)
			}
		}
		return nil
	}
	if widget() != nil {
		t.Fatal("payload bị từ chối không được ghi")
	}
	env := valveEnv(t, mkg, port.EventMKGHubFacilityPublished, facility("Kho MKG Cờ Đỏ", eff, nil))
	if ok, err := run(env); err != nil || !ok {
		t.Fatalf("hub_facility hợp lệ: %v %v", ok, err)
	}
	if ok, err := run(env); err != nil || ok {
		t.Fatalf("giao lặp: %v %v", ok, err)
	}
	// Bản effective_from cũ hơn → giữ bản mới.
	if _, err := run(valveEnv(t, mkg, port.EventMKGHubFacilityPublished, facility("Tên cũ", eff.Add(-time.Hour), nil))); err != nil {
		t.Fatal(err)
	}
	capP := map[string]any{"hub_ref": hub.String(), "as_of": eff.Add(time.Hour).Format(time.RFC3339), "capacity_tons": "1200.000000",
		"used_tons": "300.000000", "available_tons": "900.000000", "unit": "t"}
	capEnv := valveEnv(t, mkg, port.EventMKGHubCapacityPublished, capP)
	if ok, err := run(capEnv); err != nil || !ok {
		t.Fatalf("hub_capacity: %v %v", ok, err)
	}
	// Cùng (hub_ref, as_of) qua sự kiện khác (MKG phát lại) → HubSink bỏ qua.
	if _, err := run(valveEnv(t, mkg, port.EventMKGHubCapacityPublished, capP)); err != nil {
		t.Fatal(err)
	}
	w := widget()
	if w == nil || str(w, "origin") != "mkg" || str(w, "name") != "Kho MKG Cờ Đỏ" || str(w, "fill_pct") != "25.0" || str(w, "source") != "valve" {
		t.Fatalf("widget hub MKG: %v", w)
	}
	r := g.national().appDo(http.MethodGet, "/v1/geo/hubs/"+hub.String()+"/capacity", nil)
	if n := len(list(r.body, "capacity_history")); n != 1 {
		t.Fatalf("một ảnh chụp cho một as_of: %s", r.raw)
	}
	// Sai kiểu (capacity_tons là số) → từ chối.
	bad := map[string]any{"hub_ref": hub.String(), "as_of": eff.Format(time.RFC3339), "capacity_tons": 12, "used_tons": "1.000000",
		"available_tons": "11.000000", "unit": "t"}
	if _, err := run(valveEnv(t, mkg, port.EventMKGHubCapacityPublished, bad)); err == nil {
		t.Fatal("sai kiểu phải bị từ chối")
	}
	// Chưa có cấp quyền valve_hub cho tenant phong bì → lỗi rõ mã, không ghi.
	if _, err := run(valveEnv(t, ids.New(), port.EventMKGHubFacilityPublished, facility("Kho lạ", eff.Add(time.Hour), nil))); !errors.Is(err, partyport.ErrValveGrantMissing) {
		t.Fatalf("không có cấp quyền → party.valve_share_grant_missing: %v", err)
	}
	// Tenant MKG (phong bì) không có dòng hub ở pf — hub nằm ở tenant bên cấp.
	if r := g.as(mkg, uuid.Nil, rolePlotViewer).appDo(http.MethodGet, g2BBox("/v1/geo/capacity-widgets"), nil); len(list(r.body, "items")) != 0 {
		t.Fatalf("tenant MKG không được có hub ở pf: %s", r.raw)
	}
	// Tenant khác không thấy hub.
	other := g.as(ids.New(), uuid.Nil, rolePlotViewer)
	r = other.appDo(http.MethodGet, g2BBox("/v1/geo/capacity-widgets"), nil)
	for _, it := range list(r.body, "items") {
		if str(it, "ref") == hub.String() {
			t.Fatal("cách ly tenant")
		}
	}
}

// HTX US-GPS-03: nông dân (vai farmer) chỉ đọc log GPS của chính mình và log gắn với
// thửa của nông hộ liên kết với tài khoản; không đọc được log người khác ngoài thửa nhà.
func TestFarmerReadsOwnGpsLog_HTX_US_GPS_03(t *testing.T) {
	g := newG2(t)
	links := &fakeFarmerLinks{m: map[uuid.UUID][]uuid.UUID{}}
	hh, otherHH := ids.New(), ids.New()
	// Thửa của hộ ở pf/cooperative (geoport.PlotGeometry giả, W3.3).
	mk := func(household uuid.UUID) uuid.UUID {
		ref := ids.New()
		g.plots.put(ref, household, port.PlotPlace{CommuneCode: "31117", CooperativeRef: g.coopA})
		return ref
	}
	own, foreign := mk(hh), mk(otherHH)
	farmer := g.as(g.tenant, uuid.Nil, port.RoleFarmer)
	// Chưa nối liên kết tài khoản → 503.
	r := farmer.appDo(http.MethodGet, "/v1/geo/gps-fixes", nil)
	if r.status != http.StatusServiceUnavailable {
		t.Fatalf("chưa nối FarmerHouseholds: %d %s", r.status, r.raw)
	}
	g.mod.UseFarmers(links)
	links.m[farmer.actor] = []uuid.UUID{hh}
	log := g.mod.GpsLog()
	ctx := hubCtx(g.fixture)
	rec := func(actor, plot uuid.UUID) uuid.UUID {
		t.Helper()
		rc, err := log.RecordFix(ctx, port.GpsFix{ID: ids.New(), ActorRef: actor, Purpose: port.GpsPurposeFarmLog, PlotRef: plot, Source: port.GpsSourceManualPick})
		if err != nil {
			t.Fatal(err)
		}
		return rc.ID
	}
	mine := rec(farmer.actor, foreign)  // do chính mình ghi (kể cả thửa khác)
	onMy := rec(ids.New(), own)         // quản lý HTX ghi hộ trên thửa nhà mình
	stranger := rec(ids.New(), foreign) // người khác, thửa khác → không thấy
	r = farmer.appDo(http.MethodGet, "/v1/geo/gps-fixes", nil)
	expect(t, r, http.StatusOK, "nông dân đọc log")
	var got []string
	for _, it := range list(r.body, "items") {
		got = append(got, str(it, "id"))
	}
	if len(got) != 2 || !slices.Contains(got, mine.String()) || !slices.Contains(got, onMy.String()) || slices.Contains(got, stranger.String()) {
		t.Fatalf("log của nông dân: %v", got)
	}
	// Lọc actor_ref của người khác chỉ thu hẹp trong phạm vi của mình.
	r = farmer.appDo(http.MethodGet, "/v1/geo/gps-fixes?plot_ref="+foreign.String(), nil)
	if items := list(r.body, "items"); len(items) != 1 || str(items[0], "id") != mine.String() {
		t.Fatalf("lọc thửa khác: %s", r.raw)
	}
	// Tenant khác: cùng actor, không thấy gì.
	if n := len(list(g.as(ids.New(), uuid.Nil, port.RoleFarmer).appDo(http.MethodGet, "/v1/geo/gps-fixes", nil).body, "items")); n != 0 {
		t.Fatalf("tenant khác thấy %d", n)
	}
}
