package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// seedCommunes ghi ĐVHC cấp xã vào bảng chiếu k_ref.admin_unit của tenant tid (như pf/kref dựng từ pf/masterdata):
// hiệu lực từ from, đến to (rỗng = còn hiệu lực; không bao gồm — ADR-0032), kế thừa succ (khi có to).
func seedCommunes(t *testing.T, tid uuid.UUID, from, to string, codes ...string) {
	t.Helper()
	seedCommune(t, tid, from, to, nil, codes...)
}

func seedCommune(t *testing.T, tid uuid.UUID, from, to string, succ []string, codes ...string) {
	t.Helper()
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	var until *string
	if to != "" {
		until = &to
	}
	if succ == nil {
		succ = []string{}
	}
	for _, c := range codes {
		if _, err := admin.Exec(context.Background(), `INSERT INTO k_ref.admin_unit (tenant_id, code, valid_from, valid_to, name, level,
		    parent_code, successor_codes) VALUES ($1, $2, $3::date, $4::date, $5, 'commune', '89', $6::text[])`,
			tid, c, from, until, "Xã "+c, succ); err != nil {
			t.Fatal(err)
		}
	}
}

type fakeOfficers map[string][]port.Officer

func (f fakeOfficers) Officers(_ context.Context, _ port.Profile, level string) ([]port.Officer, error) {
	return f[level], nil
}

// Xã báo giá + phân công W11.72 (pf 000144, BRD Báo giá FN-AD-04.02, FN-AD-04.01) qua HTTP: nhóm vùng mặc định + thêm,
// ĐVHC có hiệu lực (ADR-0032), thêm xã (mã tự sinh, ĐVHC hết hiệu lực/trùng bị chặn — AC-02), phân công cán bộ Xã theo
// ngày hiệu lực (AC-02 FN-AD-04.01) và cán bộ Cấp Tỉnh, chuyển mã ĐVHC khi sắp xếp ĐVHC, ngưng xã, cờ QTNV
// (Profile.StewardDuty), port.Points, PointReminders (nhắc cán bộ Xã phụ trách, lần 2 bỏ xã đã gửi).
func TestPricePointsHTTP_W1172(t *testing.T) {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC) // Thứ Hai 10:00 giờ VN
	prof := port.Profile{Name: "kn", Department: "thong_tin_truyen_thong", Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA",
		"kn_tw": "TW"}, ManageBy: []string{"admin"}, StewardDuty: authz.DutyPriceAdmin}
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clock.NewFixed(now),
		[]port.Profile{prof})
	if err != nil {
		t.Fatal(err)
	}
	tid, other := ids.New(), ids.New()
	ctvA, ctvB, tinhRef := ids.New(), ids.New(), ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"tinh":  {ActorRef: tinhRef, TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh"}},
		"qtnv": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tinh",
			Duties: []string{authz.DutyPriceAdmin}}},
		"xaA":   {ActorRef: ctvA, TenantID: tid, KN: &authz.KNContext{Role: "kn_xa", OrgRef: ids.New()}},
		"tw":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tw"}},
		"other": {ActorRef: ids.New(), TenantID: other, KN: &authz.KNContext{Role: "kn_tinh"}},
	}
	svc.Points.Officers = fakeOfficers{
		"XA":   {{Ref: ctvA, Name: "Nguyễn A", Level: "XA"}, {Ref: ctvB, Name: "Trần B", Level: "XA"}},
		"TINH": {{Ref: tinhRef, Name: "Lê Tỉnh", Level: "TINH"}},
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if p, ok := who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	Handler{Svc: svc}.RegisterApp(r.Group("/v1"))
	PointHandler{Svc: svc}.RegisterApp(r.Group("/v1"))
	call := func(as, method, path string, body any, want int) map[string]any {
		t.Helper()
		var buf string
		if body != nil {
			b, _ := json.Marshal(body)
			buf = string(b)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(buf))
		req.Header.Set("X-Test-As", as)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
		}
		return calJSON(t, w.Body.Bytes())
	}
	str := func(v any) string { return fmt.Sprint(v) }

	// Danh mục mặc định có 2 nhóm vùng (An Giang, Kiên Giang); thêm nhóm vùng chỉ Admin.
	if n := call("admin", http.MethodPost, "/v1/price/catalog/defaults", nil, http.StatusCreated)["written"]; str(n) != "15" {
		t.Fatalf("mặc định: %v", n)
	}
	cat := call("tw", http.MethodGet, "/v1/price/catalog", nil, http.StatusOK)
	if rs := cat["regions"].([]any); len(rs) != 2 || rs[0].(map[string]any)["code"] != "an_giang" {
		t.Fatalf("nhóm vùng: %v", cat["regions"])
	}
	call("qtnv", http.MethodPost, "/v1/price/regions", map[string]any{"name": "Vùng thử"}, http.StatusForbidden)
	if c := call("admin", http.MethodPost, "/v1/price/regions", map[string]any{"name": "Vùng thử"}, http.StatusCreated)["code"]; c != "vung_3" {
		t.Fatalf("nhóm vùng mới: %v", c)
	}
	call("admin", http.MethodPost, "/v1/price/regions/vung_3/retire", nil, http.StatusCreated)

	// ĐVHC: A1, A2 còn hiệu lực; OLD hết hiệu lực 01/10 (kế thừa A2); B1 hết hiệu lực từ 20/10 (kế thừa B2 từ 20/10).
	seedCommunes(t, tid, "2025-07-01", "", "A1", "A2", "A3")
	seedCommune(t, tid, "2020-01-01", "2026-10-01", []string{"A2"}, "OLD")
	seedCommune(t, tid, "2020-01-01", "2026-10-20", []string{"B2"}, "B1")
	seedCommunes(t, tid, "2026-10-20", "", "B2")
	if us := call("tinh", http.MethodGet, "/v1/price/admin-units", nil, http.StatusOK)["admin_units"].([]any); len(us) != 4 {
		t.Fatalf("ĐVHC có hiệu lực 12/10 (A1, A2, A3, B1): %v", us)
	}

	// Thêm xã (B2): chỉ Admin; ĐVHC hết hiệu lực / không có → 422; nhóm vùng không có → 422; mã tự sinh XBG####.
	pt := func(au, region, from string) map[string]any {
		return map[string]any{"admin_unit_code": au, "region_code": region, "valid_from": from}
	}
	call("tinh", http.MethodPost, "/v1/price/points", pt("A1", "an_giang", "2026-10-12"), http.StatusForbidden)
	call("admin", http.MethodPost, "/v1/price/points", pt("OLD", "an_giang", "2026-10-12"), http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/price/points", pt("ZZ", "an_giang", "2026-10-12"), http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/price/points", pt("A1", "vung_3", "2026-10-12"), http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/price/points", pt("A1", "an_giang", "2026-10-11"), http.StatusUnprocessableEntity) // quá khứ
	if w := call("admin", http.MethodPost, "/v1/price/points", pt("A1", "an_giang", "2026-10-12"), http.StatusCreated); w["code"] != "XBG0001" {
		t.Fatalf("xã 1: %v", w)
	}
	if p := call("admin", http.MethodPost, "/v1/price/points", pt("A1", "kien_giang", "2026-10-20"), http.StatusConflict); p["code"] != "price.admin_unit_taken" {
		t.Fatalf("AC-02 trùng ĐVHC: %v", p)
	}
	call("admin", http.MethodPost, "/v1/price/points", pt("A2", "kien_giang", "2026-10-14"), http.StatusCreated) // XBG0002 chờ hiệu lực
	call("admin", http.MethodPost, "/v1/price/points", pt("B1", "kien_giang", "2026-10-12"), http.StatusCreated) // XBG0003

	ls := call("tw", http.MethodGet, "/v1/price/points?at=2026-10-13", nil, http.StatusOK)["points"].([]any)
	if len(ls) != 3 {
		t.Fatalf("danh sách: %v", ls)
	}
	p1 := ls[0].(map[string]any)
	if p1["code"] != "XBG0001" || p1["name"] != "Xã A1" || p1["region_name"] != "An Giang" || p1["state"] != "active" ||
		p1["officer_ref"] != nil || p1["admin_unit_valid"] != true {
		t.Fatalf("xã 1: %v", p1)
	}
	if s := ls[1].(map[string]any); s["code"] != "XBG0002" || s["state"] != "pending" {
		t.Fatalf("xã chờ hiệu lực (sắp theo nhóm vùng rồi mã): %v", ls)
	}

	// Phân công (FN-AD-04.01 AC-02): A phụ trách XBG0001 từ 12/10, B thay từ 14/10; tỉnh phụ trách XBG0001.
	off := func(role string, ref *uuid.UUID, from string) map[string]any {
		m := map[string]any{"role": role, "valid_from": from}
		if ref != nil {
			m["officer_ref"] = ref.String()
		}
		return m
	}
	call("tinh", http.MethodPost, "/v1/price/points/XBG0001/officers", off("xa", &ctvA, "2026-10-12"), http.StatusForbidden)
	call("admin", http.MethodPost, "/v1/price/points/XBG0001/officers", off("xa", &tinhRef, "2026-10-12"), http.StatusUnprocessableEntity) // sai cấp
	call("admin", http.MethodPost, "/v1/price/points/XBG0001/officers", off("ca", &ctvA, "2026-10-12"), http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/price/points/XBG0009/officers", off("xa", &ctvA, "2026-10-12"), http.StatusNotFound)
	call("admin", http.MethodPost, "/v1/price/points/XBG0001/officers", off("xa", &ctvA, "2026-10-12"), http.StatusCreated)
	if w := call("admin", http.MethodPost, "/v1/price/points/XBG0001/officers", off("xa", &ctvA, "2026-10-12"), http.StatusCreated); str(w["written"]) != "0" {
		t.Fatalf("phân công trùng: %v", w)
	}
	call("admin", http.MethodPost, "/v1/price/points/XBG0001/officers", off("xa", &ctvB, "2026-10-14"), http.StatusCreated)
	call("admin", http.MethodPost, "/v1/price/points/XBG0001/officers", off("tinh", &tinhRef, "2026-10-12"), http.StatusCreated)
	call("admin", http.MethodPost, "/v1/price/points/XBG0003/officers", off("xa", &ctvA, "2026-10-12"), http.StatusCreated)
	officerAt := func(at string) any {
		d := call("admin", http.MethodGet, "/v1/price/points/XBG0001?at="+at, nil, http.StatusOK)
		return d["point"].(map[string]any)["officer_ref"]
	}
	if officerAt("2026-10-13") != ctvA.String() || officerAt("2026-10-14") != ctvB.String() {
		t.Fatal("AC-02: A đến 13/10, B từ 14/10")
	}
	d := call("tinh", http.MethodGet, "/v1/price/points/XBG0001?at=2026-10-14", nil, http.StatusOK)
	if p := d["point"].(map[string]any); p["officer_name"] != "Trần B" || p["owner_name"] != "Lê Tỉnh" || len(d["officers"].([]any)) != 3 {
		t.Fatalf("chi tiết + lịch sử phân công: %v", d)
	}
	if d := call("tw", http.MethodGet, "/v1/price/points/XBG0001", nil, http.StatusOK); d["point"].(map[string]any)["officer_name"] != nil {
		t.Fatalf("Cấp TW không thấy tên cán bộ: %v", d)
	}
	if mine := call("xaA", http.MethodGet, "/v1/price/points?mine=true", nil, http.StatusOK)["points"].([]any); len(mine) != 2 {
		t.Fatalf("xã của CTV A hôm nay: %v", mine)
	}
	if mine := call("tinh", http.MethodGet, "/v1/price/points?mine=true", nil, http.StatusOK)["points"].([]any); len(mine) != 1 {
		t.Fatalf("xã được phân công cho Tỉnh: %v", mine)
	}
	if os := call("admin", http.MethodGet, "/v1/price/officers?level=XA", nil, http.StatusOK); len(os["officers"].([]any)) != 2 || os["directory"] != true {
		t.Fatalf("danh bạ: %v", os)
	}
	call("xaA", http.MethodGet, "/v1/price/officers?level=XA", nil, http.StatusForbidden)

	// Sắp xếp ĐVHC: B1 hết hiệu lực 20/10 → xã XBG0003 báo "ĐVHC hết hiệu lực" + mã kế thừa; chuyển sang B2 giữ mã xã.
	p3 := call("admin", http.MethodGet, "/v1/price/points/XBG0003?at=2026-10-20", nil, http.StatusOK)["point"].(map[string]any)
	if p3["admin_unit_valid"] != false || str(p3["successor_codes"]) != "[B2]" {
		t.Fatalf("ĐVHC hết hiệu lực: %v", p3)
	}
	call("admin", http.MethodPost, "/v1/price/points/XBG0003/revise", map[string]any{"admin_unit_code": "B2", "valid_from": "2026-10-19"},
		http.StatusUnprocessableEntity) // B2 chưa hiệu lực 19/10
	call("admin", http.MethodPost, "/v1/price/points/XBG0003/revise", map[string]any{"admin_unit_code": "B2", "valid_from": "2026-10-20",
		"note": "Sắp xếp ĐVHC"}, http.StatusCreated)
	p3 = call("admin", http.MethodGet, "/v1/price/points/XBG0003?at=2026-10-20", nil, http.StatusOK)["point"].(map[string]any)
	if p3["admin_unit_code"] != "B2" || p3["name"] != "Xã B2" || p3["admin_unit_valid"] != true || p3["officer_ref"] != ctvA.String() {
		t.Fatalf("sau chuyển ĐVHC: %v", p3)
	}

	// Ngưng xã (B3): lý do bắt buộc; sau ngày ngưng không phân công được, không còn trong port.Points.
	call("admin", http.MethodPost, "/v1/price/points/XBG0003/retire", map[string]any{"valid_from": "2026-10-25"}, http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/price/points/XBG0003/retire", map[string]any{"valid_from": "2026-10-25", "reason": "Không còn thu thập giá"},
		http.StatusCreated)
	call("admin", http.MethodPost, "/v1/price/points/XBG0003/officers", off("xa", &ctvB, "2026-10-26"), http.StatusUnprocessableEntity)
	call("admin", http.MethodPost, "/v1/price/points/XBG0003/officers", off("xa", nil, "2026-10-22"), http.StatusCreated) // bỏ phân công
	if p := call("tw", http.MethodGet, "/v1/price/points/XBG0003?at=2026-10-26", nil, http.StatusOK)["point"].(map[string]any); p["state"] != "retired" ||
		p["valid_to"] != "2026-10-24" {
		t.Fatalf("xã ngưng: %v", p)
	}
	call("admin", http.MethodPost, "/v1/price/regions/kien_giang/retire", nil, http.StatusConflict) // còn xã

	// Cờ QTNV (Profile.StewardDuty — W11.07 qtnv_bao_gia): Cấp Tỉnh có cờ cấu hình được lịch/nhắc; Cấp Tỉnh thường không.
	if st := call("qtnv", http.MethodGet, "/v1/price/status", nil, http.StatusOK); st["can_steward"] != true || st["can_manage"] != false {
		t.Fatalf("status QTNV: %v", st)
	}
	if st := call("tinh", http.MethodGet, "/v1/price/status", nil, http.StatusOK); st["can_steward"] != false {
		t.Fatalf("status Tỉnh: %v", st)
	}
	call("tinh", http.MethodPost, "/v1/price/calendar/reminder-test", nil, http.StatusForbidden)
	call("qtnv", http.MethodPost, "/v1/price/calendar/reminder-test", nil, http.StatusAccepted)
	if cal := call("qtnv", http.MethodGet, "/v1/price/calendar", nil, http.StatusOK); cal["can_manage"] != true {
		t.Fatalf("lịch QTNV: %v", cal["can_manage"])
	}

	// port.Points theo tenant của ctx.
	ctx := tenant.WithID(context.Background(), tid)
	ps, err := svc.PointsAt(ctx, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	if err != nil || len(ps) != 3 || ps[0].Code != "XBG0001" || ps[0].Officer != ctvB || !ps[0].HandledBy(tinhRef) || ps[0].HandledBy(ctvA) ||
		!ps[2].HandledBy(ctvA) {
		t.Fatalf("PointsAt: %+v %v", ps, err)
	}
	if _, ok, err := svc.PointAt(ctx, "XBG0003", time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)); ok || err != nil {
		t.Fatalf("PointAt xã ngưng: %v %v", ok, err)
	}

	// Nhắc nhập giá (W11.74 ReminderTargets): cán bộ Xã phụ trách từng xã đang hoạt động; lần 2 bỏ xã đã gửi (W11.75 hook).
	rem := app.PointReminders{S: svc}
	ts, err := rem.Targets(context.Background(), tid, "kn", time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), 1)
	if err != nil || len(ts) != 2 || ts[0].CommuneCode != "XBG0001" || ts[0].RecipientRefs[0] != ctvB || ts[1].CommuneCode != "XBG0003" ||
		ts[0].CommuneName != "Xã A1" {
		t.Fatalf("nhắc lần 1 (XBG0002 chưa có cán bộ Xã): %+v %v", ts, err)
	}
	svc.Points.Submitted = func(_ context.Context, _ pgx.Tx, _ time.Time, point string) (bool, error) {
		return point == "XBG0001", nil
	}
	ts, err = rem.Targets(context.Background(), tid, "kn", time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), 2)
	if err != nil || len(ts) != 1 || ts[0].CommuneCode != "XBG0003" {
		t.Fatalf("nhắc lần 2 bỏ xã đã gửi: %+v %v", ts, err)
	}
	if ts, err = rem.Targets(context.Background(), other, "kn", time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), 1); err != nil || len(ts) != 0 {
		t.Fatalf("tenant khác: %v %v", ts, err)
	}

	// Cách ly tenant.
	if ls := call("other", http.MethodGet, "/v1/price/points", nil, http.StatusOK)["points"].([]any); len(ls) != 0 {
		t.Fatalf("tenant khác thấy xã: %v", ls)
	}
}
