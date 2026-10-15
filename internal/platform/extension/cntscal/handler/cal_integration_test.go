package handler_test

// W11.46 (D4 CN6, BRD CNTS v3.7 FN-12, FN-15): lịch trực KSGM BM-03, lịch tiêm phòng BM-07 và check-in GPS của lịch
// công tác trên PostgreSQL thật qua module pf/extension (vai extension_app). Ngữ cảnh phiên KN giả lập bằng middleware
// X-AG-Context; danh mục CNTS, đơn vị hành chính, danh bạ Cấp Xã là bản giả.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	scheduleStore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const cnts = "chan_nuoi_thuy_san"

// 10:00 thứ Tư 13/10/2027 giờ Việt Nam.
var t0 = time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)

var (
	fac1 = uuid.MustParse("0192a000-0000-7000-8000-000000000001") // xã 30001
	fac2 = uuid.MustParse("0192a000-0000-7000-8000-000000000002") // xã 30002
)

type units struct{}

func (units) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	switch code {
	case "30001", "30002":
		return partyport.AdminUnit{Code: code, Name: "Xã " + code, Level: partyport.LevelCommune, ParentCode: "89"}, nil
	case "31001":
		return partyport.AdminUnit{Code: code, Name: "Xã tỉnh khác", Level: partyport.LevelCommune, ParentCode: "92"}, nil
	case "89", "92":
		return partyport.AdminUnit{Code: code, Name: "Tỉnh", Level: partyport.LevelProvince}, nil
	}
	return partyport.AdminUnit{}, partyport.ErrUnitNotFound
}
func (units) Descendants(context.Context, string, time.Time) ([]string, error) { return nil, nil }
func (units) Current(context.Context, string, time.Time) ([]string, error)     { return nil, nil }

type catalogs struct {
	mdport.CNTSCatalogs
	multiProvince bool
}

var items = map[string]mdport.CatalogItem{
	"cnts_model_type:GA_TP": {Catalog: "cnts_model_type", Code: "GA_TP", Name: "Gà thương phẩm", Active: true},
	"cnts_vaccination_schedule:QT_GA": {Catalog: "cnts_vaccination_schedule", Code: "QT_GA", Name: "Quy trình gà", Active: true,
		Attrs: map[string]string{"model_type": "GA_TP"}},
	"cnts_vaccine:VX_GUMBORO": {Catalog: "cnts_vaccine", Code: "VX_GUMBORO", Name: "Gumboro", Active: true},
}

func (catalogs) ItemAt(_ context.Context, catalog, code string, _ time.Time) (mdport.CatalogItem, error) {
	if it, ok := items[catalog+":"+code]; ok {
		return it, nil
	}
	return mdport.CatalogItem{}, fmt.Errorf("giả: %w", mdport.ErrCatalogItemNotFound)
}

func (catalogs) ListAt(_ context.Context, catalog string, _ time.Time) ([]mdport.CatalogItem, error) {
	out := []mdport.CatalogItem{}
	for _, it := range items {
		if it.Catalog == catalog {
			out = append(out, it)
		}
	}
	return out, nil
}

func (c catalogs) FacilitiesAt(context.Context, time.Time, []string) ([]mdport.SlaughterFacility, error) {
	out := []mdport.SlaughterFacility{
		{Ref: fac1, Code: "CS1", Name: "Lò mổ 1", CommuneCode: "30001", Kind: mdport.FacilityConcentrated, Active: true},
		{Ref: fac2, Code: "CS2", Name: "Lò mổ 2", CommuneCode: "30002", Kind: mdport.FacilitySmall, Active: true},
	}
	if c.multiProvince {
		out = append(out, mdport.SlaughterFacility{Ref: facProvince92, Code: "CS92", Name: "Cơ sở tỉnh khác", CommuneCode: "31001", Kind: mdport.FacilitySmall, Active: true})
	}
	return out, nil
}

func (catalogs) ScheduleAt(_ context.Context, code string, _ time.Time) (mdport.VaccinationSchedule, error) {
	if code != "QT_GA" {
		return mdport.VaccinationSchedule{}, mdport.ErrCatalogItemNotFound
	}
	return mdport.VaccinationSchedule{Code: code, ModelType: "GA_TP", ValidFrom: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		Doses: []mdport.Dose{{Vaccine: "VX_ND_F", Shot: 1, AgeDays: 3}, {Vaccine: "VX_GUMBORO", Shot: 1, AgeDays: 7}, {Vaccine: "VX_GUMBORO", Shot: 2, AgeDays: 28}}}, nil
}

// xaScopes: danh bạ Cấp Xã CNTS giả (được đặt sau khi tạo người dùng).
type xaScopes struct {
	mu   sync.Mutex
	list []accport.XaScope
}

func (x *xaScopes) XaScopes(context.Context, uuid.UUID, string) ([]accport.XaScope, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.list, nil
}

// knResolver: ngữ cảnh KN của lịch công tác lấy từ Principal.KN (middleware giả).
type knResolver struct{}

func (knResolver) KNContext(ctx context.Context) (accport.KNContext, bool, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || p.KN == nil {
		return accport.KNContext{}, false, nil
	}
	return accport.KNContext{Role: p.KN.Role, Department: p.KN.Department, ProvinceCode: "89", Communes: p.KN.Communes,
		MgmtLevel: p.KN.MgmtLevel, ReadOnly: p.KN.ViewOnly}, true, nil
}

type fix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	pool   *pgxpool.Pool
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
	xa     *xaScopes
}

func newFix(t *testing.T, multi ...bool) *fix {
	env := pgtest.Get(t)
	f := &fix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}, xa: &xaScopes{}}
	f.pool = env.AdminPool(t, migrations.PF)
	mod := extension.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clock.NewFixed(t0), extension.Deps{Units: units{}})
	mod.UseCNTSCatalogs(catalogs{multiProvince: len(multi) > 0 && multi[0]})
	if len(multi) < 2 || multi[1] {
		mod.UseXaScopes(f.xa)
	}
	mod.UseKNContext(knResolver{}, nil)
	ops := env.Pool(t, migrations.PF, "platform_api")
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fix) knMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := authz.FromContext(c.Request.Context())
		id, err := uuid.Parse(c.GetHeader(authz.ContextHeader))
		f.mu.Lock()
		kc := f.ctxs[id]
		f.mu.Unlock()
		if !ok || err != nil || kc == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		cp := *kc
		p.KN = &cp
		scope, _ := json.Marshal(cp)
		ctx := idempotency.WithAuthorizationScope(c.Request.Context(), scope)
		c.Request = c.Request.WithContext(authz.WithPrincipal(ctx, p))
		c.Next()
	}
}

type user struct {
	f     *fix
	token string
	ctxID uuid.UUID
	ref   uuid.UUID
}

func (f *fix) user(kc authz.KNContext) user {
	sub := ids.New()
	tok := f.iss.Sign(f.t, authztest.Token{Subject: sub, TenantID: f.tenant, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	f.mu.Lock()
	f.ctxs[kc.ID] = &kc
	f.mu.Unlock()
	return user{f: f, token: tok, ctxID: kc.ID, ref: sub}
}

func tinh(group string) authz.KNContext {
	return authz.KNContext{Role: authz.KNTinh, Department: cnts, ProvinceGroup: group,
		Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}}
}

func xa(communes ...string) authz.KNContext {
	return authz.KNContext{Role: authz.KNXa, Department: cnts, Communes: communes, Scope: authz.Scope{Units: communes}}
}

type resp struct {
	status int
	raw    []byte
	body   map[string]any
}

func (u user) do(method, path, idem string, body any) resp {
	u.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, u.f.srv.URL+path, rd)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(authz.ContextHeader, u.ctxID.String())
	if idem != "" {
		r.Header.Set("Idempotency-Key", idem)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		u.f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&m)
	return resp{status: res.StatusCode, raw: raw, body: m}
}

func key() string                                   { return "k-" + ids.New().String() }
func (u user) post(p string, b any) resp            { return u.do(http.MethodPost, p, key(), b) }
func (u user) postK(p, k string, b any) resp        { return u.do(http.MethodPost, p, k, b) }
func (u user) get(p string) resp                    { return u.do(http.MethodGet, p, "", nil) }
func str(m map[string]any, k string) string         { s, _ := m[k].(string); return s }
func num(m map[string]any, k string) string         { n, _ := m[k].(json.Number); return n.String() }
func list(m map[string]any, k string) []any         { l, _ := m[k].([]any); return l }
func obj(m map[string]any, k string) map[string]any { o, _ := m[k].(map[string]any); return o }

func want(t *testing.T, r resp, status int, code, what string) map[string]any {
	t.Helper()
	if r.status != status || (code != "" && str(r.body, "code") != code) {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.status, r.raw)
	}
	return r.body
}

func codes(l []any) map[string]int {
	out := map[string]int{}
	for _, w := range l {
		out[str(w.(map[string]any), "code")]++
	}
	return out
}

func TestDutyRoster(t *testing.T) {
	f := newFix(t)
	planner := f.user(tinh("tong_hop"))
	viewer := f.user(tinh("vien_chuc"))
	officer := f.user(xa("30001"))
	other := f.user(xa("30002"))
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001"}}, {ActorRef: other.ref, Communes: []string{"30002"}}}

	g := want(t, planner.get("/v1/extension/cnts/duty-rosters/2027-10"), 200, "", "lịch chưa lập")
	if num(g, "version") != "0" || len(list(g, "facilities")) != 2 || len(list(g, "officers")) != 2 || g["can_edit"] != true {
		t.Fatalf("lịch rỗng: %s", g)
	}
	cells := []map[string]any{
		{"facility_ref": fac1, "officer_ref": officer.ref, "day": 13, "shift": "N"},
		{"facility_ref": fac2, "officer_ref": officer.ref, "day": 13, "shift": "NĐ"}, // trực 2 cơ sở cùng ca + chưa cấp quyền cơ sở 2
		{"facility_ref": fac2, "officer_ref": other.ref, "day": 14, "shift": "Đ"},
	}
	want(t, viewer.post("/v1/extension/cnts/duty-rosters/2027-10", map[string]any{"base_version": 0, "cells": cells}), 403, "", "viên chức không lập lịch")
	want(t, planner.post("/v1/extension/cnts/duty-rosters/2027-10", map[string]any{"cells": cells}), 422, "validation_failed", "thiếu base_version")
	k := key()
	s := want(t, planner.postK("/v1/extension/cnts/duty-rosters/2027-10", k, map[string]any{"base_version": 0, "cells": cells, "note": "Lịch tháng 10"}), 201, "", "lưu")
	ws := codes(list(s, "warnings"))
	if num(s, "version") != "1" || ws["facility_uncovered"] != 2 || ws["double_shift"] != 1 || ws["officer_not_granted"] != 1 {
		t.Fatalf("lưu/cảnh báo: %s", s)
	}
	if len(list(s, "notified_officer_refs")) != 2 {
		t.Fatalf("thông báo 2 viên chức: %s", s)
	}
	want(t, planner.postK("/v1/extension/cnts/duty-rosters/2027-10", k, map[string]any{"base_version": 0, "cells": cells, "note": "Lịch tháng 10"}), 201, "", "gọi lặp (middleware trả lại phản hồi đầu)")
	want(t, planner.post("/v1/extension/cnts/duty-rosters/2027-10", map[string]any{"base_version": 0, "cells": cells}), 409, "extension.cnts_roster_version_conflict", "khóa lạc quan")
	bad := []map[string]any{{"facility_ref": ids.New(), "officer_ref": officer.ref, "day": 1, "shift": "N"}}
	want(t, planner.post("/v1/extension/cnts/duty-rosters/2027-10", map[string]any{"base_version": 1, "cells": bad}), 422, "extension.cnts_roster_invalid", "cơ sở không hoạt động")
	// Đổi ca của other → chỉ other được thông báo.
	cells[2]["shift"] = "N"
	s = want(t, planner.post("/v1/extension/cnts/duty-rosters/2027-10", map[string]any{"base_version": 1, "cells": cells}), 201, "", "đổi ca")
	if n := list(s, "notified_officer_refs"); len(n) != 1 || n[0] != other.ref.String() {
		t.Fatalf("đổi ca: %s", s)
	}
	var events int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = 'pf.extension.cnts_duty.changed'`, f.tenant).Scan(&events); err != nil || events != 2 {
		t.Fatalf("sự kiện đổi ca = %d %v", events, err)
	}
	want(t, officer.get("/v1/extension/cnts/duty-rosters/2027-10"), 403, "", "Cấp Xã không xem lưới toàn tỉnh")
	m := want(t, officer.get("/v1/extension/cnts/my-duties?month=2027-10"), 200, "", "lịch trực của tôi")
	if l := list(m, "items"); len(l) != 2 || str(l[0].(map[string]any), "date") != "2027-10-13" {
		t.Fatalf("lịch của tôi: %s", m)
	}
}

func TestVaccinationBatch(t *testing.T) {
	f := newFix(t)
	chu := f.user(tinh("chu_nhiem"))
	tongHop := f.user(tinh("tong_hop"))
	officer := f.user(xa("30001"))
	stranger := f.user(xa("30002"))

	body := map[string]any{"commune_code": "30001", "model_type_code": "GA_TP", "locality": "Ấp 3", "received_date": "2027-10-01",
		"age_at_receipt": 1, "head_count": 500}
	want(t, tongHop.post("/v1/extension/cnts/vaccination-batches", body), 403, "", "tổng hợp không lập lịch tiêm")
	want(t, chu.post("/v1/extension/cnts/vaccination-batches", map[string]any{"commune_code": "30001", "model_type_code": "GA_TP",
		"received_date": "2027-10-01", "age_at_receipt": 1}), 422, "extension.cnts_vaccination_invalid", "thiếu hộ/địa bàn")
	b := want(t, chu.post("/v1/extension/cnts/vaccination-batches", body), 201, "", "lập lịch tiêm")
	ds := list(b, "doses")
	if len(ds) != 3 || str(b, "schedule_code") != "QT_GA" {
		t.Fatalf("mũi: %s", b)
	}
	// FN-15 AC-03: ngày dự kiến = 01/10 + 7 − 1.
	gum := ds[1].(map[string]any)
	if str(gum, "vaccine_code") != "VX_GUMBORO" || str(gum, "expected_date") != "2027-10-07" || str(gum, "vaccine_name") != "Gumboro" {
		t.Fatalf("Gumboro L1: %v", gum)
	}
	// Hôm nay 13/10: mũi 03/10 và 07/10 quá hạn.
	if str(b, "status") != "overdue" || num(b, "overdue_count") != "2" {
		t.Fatalf("quá hạn: %s", b)
	}
	ref := str(b, "ref")
	want(t, stranger.get("/v1/extension/cnts/vaccination-batches/"+ref), 404, "extension.cnts_vaccination_not_found", "ngoài phạm vi")
	if l := list(want(t, stranger.get("/v1/extension/cnts/vaccination-batches"), 200, "", "danh sách xã khác"), "items"); len(l) != 0 {
		t.Fatalf("xã khác thấy lứa: %v", l)
	}
	want(t, officer.post("/v1/extension/cnts/vaccination-batches/"+ref+"/doses", map[string]any{"base_version": 1,
		"doses": []map[string]any{{"vaccine_code": "VX_GUMBORO", "shot": 2, "expected_date": "2027-10-30"}}}), 422, "extension.cnts_vaccination_invalid", "Cấp Xã không sửa ngày dự kiến")
	r := want(t, officer.post("/v1/extension/cnts/vaccination-batches/"+ref+"/doses", map[string]any{"base_version": 1,
		"doses": []map[string]any{{"vaccine_code": "VX_ND_F", "shot": 1, "actual_date": "2027-10-03"}, {"vaccine_code": "VX_GUMBORO", "shot": 1, "actual_date": "2027-10-08"}}}),
		200, "", "Cấp Xã ghi ngày tiêm thực tế")
	if num(r, "version") != "2" || str(r, "event") != "dose_recorded" || str(r, "status") != "open" || len(list(r, "versions")) != 2 {
		t.Fatalf("ghi ngày thực tế: %s", r)
	}
	want(t, officer.post("/v1/extension/cnts/vaccination-batches/"+ref+"/doses", map[string]any{"base_version": 1,
		"doses": []map[string]any{{"vaccine_code": "VX_GUMBORO", "shot": 2, "actual_date": "2027-10-12"}}}), 409, "extension.cnts_vaccination_version_conflict", "khóa lạc quan")
	r = want(t, chu.post("/v1/extension/cnts/vaccination-batches/"+ref+"/doses", map[string]any{"base_version": 2,
		"doses": []map[string]any{{"vaccine_code": "VX_GUMBORO", "shot": 2, "expected_date": "2027-10-30"}}}), 200, "", "chủ nhiệm sửa ngày dự kiến từng hộ")
	if str(r, "event") != "updated" {
		t.Fatalf("sửa ngày dự kiến: %s", r)
	}
	want(t, officer.post("/v1/extension/cnts/vaccination-batches/"+ref+"/cancel", map[string]any{"base_version": 3, "reason": "lập nhầm"}), 403, "", "Cấp Xã không hủy")
	c := want(t, chu.post("/v1/extension/cnts/vaccination-batches/"+ref+"/cancel", map[string]any{"base_version": 3, "reason": "Lứa không nuôi"}), 200, "", "hủy")
	if str(c, "status") != "cancelled" || c["can_record"] != false {
		t.Fatalf("hủy: %s", c)
	}
	if l := list(want(t, chu.get("/v1/extension/cnts/vaccination-batches?status=cancelled"), 200, "", "lọc đã hủy"), "items"); len(l) != 1 {
		t.Fatalf("lọc: %v", l)
	}
}

func TestWorkScheduleCheckin(t *testing.T) {
	f := newFix(t)
	officer := f.user(xa("30001"))
	place := map[string]any{"lat": 10.0, "lon": 105.0}
	itemNear, itemFar, itemOffice := ids.New(), ids.New(), ids.New()
	items := []map[string]any{
		{"id": itemNear, "day": "2027-10-13", "session": "morning", "content": "Kiểm tra lò mổ", "commune_code": "30001", "place": place},
		{"id": itemFar, "day": "2027-10-13", "session": "afternoon", "content": "Thăm hộ mô hình", "commune_code": "30001", "place": place},
		{"id": itemOffice, "day": "2027-10-12", "session": "morning", "content": "Họp tại xã", "location": "Ấp 2"},
	}
	sid := ids.New()
	d := want(t, officer.post("/v1/extension/work-schedules", map[string]any{"id": sid, "week_start": "2027-10-11", "action": "draft", "items": items}), 201, "", "lưu nháp")
	if p := obj(list(d, "items")[1].(map[string]any), "place"); p == nil {
		t.Fatalf("địa điểm: %s", d)
	}
	path := fmt.Sprintf("/v1/extension/work-schedules/%s/items/%s/checkin", sid, itemNear)
	near := map[string]any{"lat": 10.001, "lon": 105.0, "accuracy_m": 12, "captured_at": "2027-10-13T09:55:00+07:00"}
	want(t, officer.post(path, near), 409, "extension.checkin_not_registered", "lịch nháp chưa check-in")
	want(t, officer.post("/v1/extension/work-schedules", map[string]any{"id": sid, "week_start": "2027-10-11", "action": "submit", "base_version": 1, "items": items}), 201, "", "gửi duyệt")
	k := key()
	c := want(t, officer.postK(path, k, near), 201, "", "check-in gần")
	ck := obj(c, "checkin")
	if ck["far"] != false || obj(ck, "location") == nil || ck["point_stored"] != true {
		t.Fatalf("check-in gần: %s", c)
	}
	replayed := want(t, officer.postK(path, k, near), 201, "", "gọi lặp (metadata giữ nguyên, vị trí không nằm trong cache)")
	// CNTS-CHECKIN-PRIVACY-001: không tạo bản sao tọa độ trong cache 48 giờ, ngoài bảng có hạn lưu.
	if obj(obj(replayed, "checkin"), "location") != nil {
		t.Fatal("cache chống trùng đang giữ tọa độ cá nhân ngoài bảng có hạn lưu")
	}

	want(t, officer.post(path, near), 409, "extension.checkin_exists", "check-in không sửa được")
	far := map[string]any{"lat": 10.01, "lon": 105.0, "accuracy_m": 80, "captured_at": "2027-10-13T09:58:00+07:00", "mock_location": true}
	fp := fmt.Sprintf("/v1/extension/work-schedules/%s/items/%s/checkin", sid, itemFar)
	c = want(t, officer.post(fp, far), 201, "", "check-in xa")
	if w := codes(list(c, "warnings")); w["checkin_far"] != 1 || w["checkin_weak_gps"] != 1 || w["checkin_mock_location"] != 1 {
		t.Fatalf("cảnh báo xa: %s", c)
	}
	wrong := map[string]any{"lat": 10.0, "lon": 105.0, "captured_at": "2027-10-13T09:00:00+07:00"}
	want(t, officer.post(fmt.Sprintf("/v1/extension/work-schedules/%s/items/%s/checkin", sid, itemOffice), wrong), 422, "validation_failed", "khác ngày công tác")
	m := want(t, officer.get("/v1/extension/work-schedules/mine?week_start=2027-10-11"), 200, "", "lịch của tôi")
	sc := obj(m, "schedule")
	if m["checkin_enabled"] != true || num(sc, "checkin_count") != "2" || num(sc, "missed_checkin_count") != "1" {
		t.Fatalf("lịch của tôi: %s", m)
	}
	// Cả phản hồi duyệt lịch sau check-in cũng chỉ cache metadata.
	lead := tinh("lanh_dao_phong")
	lead.Manager = true
	lead.MgmtLevel = "truong_phong"
	reviewer := f.user(lead)
	approvePath := "/v1/extension/work-schedules/" + sid.String() + "/approve"
	approveKey := key()
	approved := want(t, reviewer.postK(approvePath, approveKey, map[string]any{"version": 2}), 200, "", "duyệt sau check-in")
	if len(list(approved, "checkins")) != 2 || obj(list(approved, "checkins")[0].(map[string]any), "location") == nil {
		t.Fatal("phản hồi đầu của người có quyền phải còn vị trí")
	}
	approved = want(t, reviewer.postK(approvePath, approveKey, map[string]any{"version": 2}), 200, "", "gửi lại duyệt")
	for _, v := range list(approved, "checkins") {
		if obj(v.(map[string]any), "location") != nil {
			t.Fatal("cache duyệt giữ tọa độ cá nhân")
		}
	}
	// Tọa độ ở bảng có hạn lưu (90 ngày), sổ không có tọa độ.
	var n int
	var purge time.Time
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*), max(purge_after) FROM pf_extension.work_schedule_checkin_point WHERE tenant_id = $1`, f.tenant).Scan(&n, &purge); err != nil || n != 2 || purge.Format(time.DateOnly) != "2028-01-11" {
		t.Fatalf("bảng tọa độ: %d %s %v", n, purge, err)
	}
	ctx := context.Background()
	var cachedPoints int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM shared.idempotency_key WHERE tenant_id=$1 AND response_body IS NOT NULL
      AND jsonb_path_exists(convert_from(response_body,'UTF8')::jsonb,'$.**.location.lat')`, f.tenant).Scan(&cachedPoints); err != nil || cachedPoints != 0 {
		t.Fatalf("cache giữ tọa độ: %d %v", cachedPoints, err)
	}
	var before, after string
	const hashes = `SELECT string_agg(encode(row_hash,'hex'),',' ORDER BY id) FROM pf_extension.work_schedule_checkin WHERE tenant_id=$1`
	if err := f.pool.QueryRow(ctx, hashes, f.tenant).Scan(&before); err != nil {
		t.Fatal(err)
	}
	otherTenant := ids.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO pf_extension.work_schedule_checkin_point (tenant_id,checkin_id,point,captured_at,purge_after)
      VALUES ($1,$2,ST_SetSRID(ST_MakePoint(105,10),4326),$3,$4)`, otherTenant, ids.New(), t0, purge); err != nil {
		t.Fatal(err)
	}
	appDB := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	runPurge := func(at time.Time) int {
		t.Helper()
		n := 0
		err := tenant.WithTenantTx(ctx, appDB, f.tenant, func(tx pgx.Tx) error {
			var err error
			n, err = scheduleStore.PurgeCheckinPoints(ctx, tx, at, 1000)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := runPurge(purge); n != 0 {
		t.Fatalf("xóa trước hết hạn: %d", n)
	}
	if n := runPurge(purge.AddDate(0, 0, 1)); n != 2 {
		t.Fatalf("phải xóa đúng2tọa độ tenant hiện tại: %d", n)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM pf_extension.work_schedule_checkin_point WHERE tenant_id=$1`, otherTenant).Scan(&n); err != nil || n != 1 {
		t.Fatalf("purge chạm tenant khác: %d %v", n, err)
	}
	if err := f.pool.QueryRow(ctx, hashes, f.tenant).Scan(&after); err != nil || before == "" || before != after {
		t.Fatalf("purge thay đổi bằng chứng check-in: %v", err)
	}
	detail := want(t, officer.get("/v1/extension/work-schedules/"+sid.String()), 200, "", "đọc sau purge")
	if len(list(detail, "checkins")) != 2 {
		t.Fatal("purge làm mất metadata check-in")
	}
	for _, v := range list(detail, "checkins") {
		if obj(v.(map[string]any), "location") != nil {
			t.Fatal("tọa độ còn sau purge")
		}
	}
}
