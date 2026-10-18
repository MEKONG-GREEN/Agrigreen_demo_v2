package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type unitsStub map[uuid.UUID][]port.Unit

func (u unitsStub) InternalUnits(_ context.Context, ref uuid.UUID) ([]port.Unit, error) {
	return u[ref], nil
}

type recFixture struct {
	t     *testing.T
	r     *gin.Engine
	svc   *app.Service
	vault *clienttest.Fake
	env   *pgtest.Env
	tid   uuid.UUID
	who   map[string]authz.Principal
}

func (f *recFixture) call(as, method, path string, body any, want int) map[string]any {
	f.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("X-Test-As", as)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	dec.UseNumber()
	_ = dec.Decode(&m)
	if w.Code != want {
		f.t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
	}
	if m == nil {
		m = map[string]any{"_raw": w.Body.String(), "_type": w.Header().Get("Content-Type"), "_cache": w.Header().Get("Cache-Control")}
	}
	return m
}

func newRecFixture(t *testing.T, enabled bool) *recFixture {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	svc, err := app.NewService(db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), app.DBModule), clock.NewFixed(now),
		port.Access{Department: "hanh_chinh_tong_hop", ManageBy: []string{"admin"}, ReadBy: []string{"kn_tinh"}})
	if err != nil {
		t.Fatal(err)
	}
	f := &recFixture{t: t, svc: svc, vault: clienttest.New(), env: env, tid: ids.New()}
	org := ids.New()
	svc.UseRecords(f.vault, unitsStub{org: {{Code: "trong_trot", Active: true}, {Code: "chan_nuoi", Active: true},
		{Code: "hcth", Active: true}, {Code: "tram_cho_moi", Active: true}, {Code: "da_ngung", Active: false}}}, enabled)
	kn := func(role, dept, unit string, manager bool) *authz.KNContext {
		return &authz.KNContext{Role: role, Department: dept, InternalUnit: unit, Manager: manager, UnitRef: org}
	}
	f.who = map[string]authz.Principal{
		"admin":     {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"hcth":      {ActorRef: ids.New(), TenantID: f.tid, KN: kn("kn_tinh", "hanh_chinh_tong_hop", "hcth", false)},
		"hcth2":     {ActorRef: ids.New(), TenantID: f.tid, KN: kn("kn_tinh", "hanh_chinh_tong_hop", "hcth", false)},
		"ld_tt":     {ActorRef: ids.New(), TenantID: f.tid, KN: kn("kn_tinh", "trong_trot", "trong_trot", true)},
		"ld_cn":     {ActorRef: ids.New(), TenantID: f.tid, KN: kn("kn_tinh", "chan_nuoi", "chan_nuoi", true)},
		"vc_tt":     {ActorRef: ids.New(), TenantID: f.tid, KN: kn("kn_tinh", "trong_trot", "trong_trot", false)},
		"xa":        {ActorRef: ids.New(), TenantID: f.tid, KN: kn("kn_xa", "trong_trot", "trong_trot", false)},
		"tw":        {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tw"}},
		"viewonly":  {ActorRef: ids.New(), TenantID: f.tid, KN: &authz.KNContext{Role: "kn_tinh", Department: "hanh_chinh_tong_hop", ViewOnly: true, Manager: true, InternalUnit: "bgd"}},
		"other":     {ActorRef: ids.New(), TenantID: ids.New(), KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"realmonly": {ActorRef: ids.New(), TenantID: f.tid, Roles: []string{"admin"}},
	}
	gin.SetMode(gin.TestMode)
	f.r = gin.New()
	f.r.Use(func(c *gin.Context) {
		if p, ok := f.who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	Handler{Svc: svc}.RegisterApp(f.r.Group("/v1"))
	return f
}

func newStaff(typ, name, phone string, events ...map[string]any) map[string]any {
	return map[string]any{"type": typ,
		"profile": map[string]any{"full_name": name, "phone": phone, "address": "Phường Long Xuyên", "birth_date": "1988-05-01",
			"gender": "nu", "email": strings.ToLower(strings.ReplaceAll(name, " ", "")) + "@ttkn.vn"},
		"events": events}
}

func ev(kind, from string, payload map[string]any) map[string]any {
	return map[string]any{"kind": kind, "valid_from": from, "payload": payload}
}

// W11.22 (FN-07, FN-08, FN-20) qua HTTP: cờ bật (Q-005), tạo hồ sơ với PII chỉ ở kho (mục đích hrm.profile; thẻ TK mục
// đích hrm.sensitive), mã tự sinh, quá trình có hiệu lực, bậc = phép chiếu + hệ số tra bảng (AC-04), as-of (AC-03),
// FN-08 trạng thái, nhật ký xem/xuất, ma trận quyền theo nhóm/đơn vị/bản thân/người dùng, RLS tenant, sổ chỉ ghi thêm.
func TestHRMRecordsHTTP_W1122(t *testing.T) {
	// Cờ tắt: mọi route hồ sơ 403 hrm.records_disabled; /access báo enabled = false.
	off := newRecFixture(t, false)
	if p := off.call("hcth", http.MethodGet, "/v1/hrm/staff", nil, http.StatusForbidden); p["code"] != "hrm.records_disabled" {
		t.Fatalf("tắt: %v", p)
	}
	if a := off.call("hcth", http.MethodGet, "/v1/hrm/access", nil, http.StatusOK); a["enabled"] != false {
		t.Fatalf("access khi tắt: %v", a)
	}

	f := newRecFixture(t, true)
	f.call("", http.MethodGet, "/v1/hrm/staff", nil, http.StatusUnauthorized)
	f.call("realmonly", http.MethodGet, "/v1/hrm/staff", nil, http.StatusForbidden)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)

	a := f.call("hcth", http.MethodGet, "/v1/hrm/access", nil, http.StatusOK)
	if a["group"] != "hcth" || a["perms"].(map[string]any)["sensitive.view"] != "all" {
		t.Fatalf("quyền HCTH: %v", a)
	}
	if a := f.call("ld_tt", http.MethodGet, "/v1/hrm/access", nil, http.StatusOK); a["group"] != "manager" ||
		a["perms"].(map[string]any)["profile.view"] != "unit" || a["perms"].(map[string]any)["sensitive.view"] != "none" {
		t.Fatalf("quyền lãnh đạo phòng: %v", a)
	}
	for _, as := range []string{"vc_tt", "xa", "tw"} {
		f.call(as, http.MethodGet, "/v1/hrm/staff", nil, http.StatusForbidden) // BR-04: Cấp TW, Cấp Xã, viên chức không xem hồ sơ
	}

	// Tạo hồ sơ (HCTH): đơn vị + chức vụ bắt buộc; chức vụ lãnh đạo thiếu ngày bổ nhiệm → 422 (HS-20).
	unitTT := ev("unit", "2020-01-01", map[string]any{"unit": "trong_trot"})
	vc := ev("position", "2020-01-01", map[string]any{"position": "vien_chuc"})
	grade := ev("grade", "2020-01-01", map[string]any{"grade": "01.003", "step": "5", "step_since": "2020-01-01", "reserved_coeff": "0.12"})
	start := ev("work_start", "2015-03-01", map[string]any{})
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", "Trần Thị Mai", "0912000111", unitTT), http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", "Trần Thị Mai", "0912000111", unitTT,
		ev("position", "2020-01-01", map[string]any{"position": "truong_phong"})), http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", "Trần Thị Mai (19 VC)", "0912000111", unitTT, vc), http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", "Trần Thị Mai", "0912000111",
		ev("unit", "2020-01-01", map[string]any{"unit": "da_ngung"}), vc), http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", "Trần Thị Mai", "0912000111", unitTT, vc,
		ev("grade", "2020-01-01", map[string]any{"grade": "01.003", "step": "10"})), http.StatusUnprocessableEntity) // A1: 9 bậc
	f.call("ld_tt", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", "Trần Thị Mai", "0912000111", unitTT, vc), http.StatusForbidden)

	body := newStaff("vien_chuc", "Trần Thị Mai", "0912000111", unitTT, vc, grade, start)
	body["sensitive"] = map[string]any{"ethnicity": "kinh", "religion": "khong", "hometown": "Xã Mỹ Hòa Hưng", "revolution_date": "2010-05"}
	c1 := f.call("hcth", http.MethodPost, "/v1/hrm/staff", body, http.StatusCreated)
	if c1["code"] != "VC-00001" {
		t.Fatalf("mã hồ sơ: %v", c1)
	}
	id1 := c1["id"].(string)
	c2 := f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", "Lê Văn Bình", "0912000222",
		ev("unit", "2021-01-01", map[string]any{"unit": "chan_nuoi"}), vc, start), http.StatusCreated)
	if c2["code"] != "VC-00002" {
		t.Fatalf("mã hồ sơ 2: %v", c2)
	}
	id2 := c2["id"].(string)
	xa := f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("can_bo_xa", "Nguyễn Văn Xã", "0912000333",
		ev("unit", "2022-01-01", map[string]any{"commune": "30292", "mgmt_unit": "trong_trot"}), vc), http.StatusCreated)
	if xa["code"] != "XA-00001" {
		t.Fatalf("mã cán bộ xã: %v", xa)
	}
	idXa := xa["id"].(string)
	// Trùng SĐT (quy tắc 2.0 mục 4) → chặn.
	if p := f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("hop_dong", "Phạm Văn Trùng", "0912 000 111", unitTT, vc), http.StatusConflict); p["code"] != "hrm.duplicate_person" {
		t.Fatalf("trùng SĐT: %v", p)
	}
	// Cán bộ xã không có thẻ Thống kê BC Sở.
	bx := newStaff("can_bo_xa", "Võ Thị Xã", "0912000444", ev("unit", "2022-01-01", map[string]any{"commune": "30292", "mgmt_unit": "trong_trot"}), vc)
	bx["sensitive"] = map[string]any{"ethnicity": "kinh"}
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", bx, http.StatusUnprocessableEntity)

	// Kho định danh: mục đích đúng, thẻ TK ghi với hrm.sensitive.
	ops := map[string]bool{}
	for _, x := range f.vault.Accesses() {
		ops[x.Op+"/"+string(x.Purpose)] = true
	}
	for _, w := range []string{"create/hrm.profile", "section_write/hrm.profile", "section_write/hrm.sensitive"} {
		if !ops[w] {
			t.Fatalf("thiếu lời gọi kho %s: %v", w, ops)
		}
	}

	// Danh sách (HCTH toàn Trung tâm), nhãn che bớt; lãnh đạo Trồng trọt: người phòng mình + cán bộ xã do phòng quản lý (HS-37).
	l := f.call("hcth", http.MethodGet, "/v1/hrm/staff", nil, http.StatusOK)
	if l["total"] != json.Number("3") || l["scope"] != "all" {
		t.Fatalf("danh sách HCTH: %v", l)
	}
	first := l["items"].([]any)[0].(map[string]any)
	cur := first["current"].(map[string]any)
	if first["label"] != "Trần T. M." || cur["coefficient"] != "3.66" || cur["steps"] != json.Number("9") || cur["status"] != "active" {
		t.Fatalf("dòng 1: %v", first)
	}
	lt := f.call("ld_tt", http.MethodGet, "/v1/hrm/staff", nil, http.StatusOK)
	if lt["total"] != json.Number("2") {
		t.Fatalf("lãnh đạo Trồng trọt: %v", lt)
	}
	if lc := f.call("ld_cn", http.MethodGet, "/v1/hrm/staff", nil, http.StatusOK); lc["total"] != json.Number("1") {
		t.Fatalf("lãnh đạo Chăn nuôi: %v", lc)
	}
	f.call("ld_cn", http.MethodGet, "/v1/hrm/staff/"+id1, nil, http.StatusNotFound) // AC-02
	if q := f.call("hcth", http.MethodGet, "/v1/hrm/staff?q=0912000222", nil, http.StatusOK); q["total"] != json.Number("1") {
		t.Fatalf("tìm theo SĐT: %v", q)
	}
	if q := f.call("hcth", http.MethodGet, "/v1/hrm/staff?q=XA-", nil, http.StatusOK); q["total"] != json.Number("1") {
		t.Fatalf("tìm theo mã: %v", q)
	}

	// Chi tiết: PII từ kho, thẻ TK KHÔNG có trong chi tiết; ghi nhật ký xem.
	d := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id1, nil, http.StatusOK)
	prof := d["profile"].(map[string]any)
	if prof["full_name"] != "Trần Thị Mai" || prof["phone"] != "0912000111" || prof["gender"] != "nu" || d["age"] != json.Number("38") {
		t.Fatalf("chi tiết: %v", d)
	}
	if strings.Contains(mustJSON(d), "Mỹ Hòa Hưng") || strings.Contains(mustJSON(d), "kinh") {
		t.Fatal("chi tiết lộ trường thẻ Thống kê BC Sở")
	}
	if d["can"].(map[string]any)["sensitive.view"] != true {
		t.Fatalf("can: %v", d["can"])
	}
	sens := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id1+"/sensitive", nil, http.StatusOK)
	if sf := sens["fields"].(map[string]any); sf["hometown"] != "Xã Mỹ Hòa Hưng" || sf["revolution_precision"] != "month" {
		t.Fatalf("thẻ TK: %v", sens)
	}
	// Lãnh đạo phòng xem hồ sơ phòng mình nhưng không thấy thẻ TK (AC-05), không sửa.
	dl := f.call("ld_tt", http.MethodGet, "/v1/hrm/staff/"+id1, nil, http.StatusOK)
	if dl["can"].(map[string]any)["sensitive.view"] != false || dl["can"].(map[string]any)["profile.edit"] != false {
		t.Fatalf("can lãnh đạo: %v", dl["can"])
	}
	f.call("ld_tt", http.MethodGet, "/v1/hrm/staff/"+id1+"/sensitive", nil, http.StatusForbidden)
	f.call("ld_tt", http.MethodGet, "/v1/hrm/staff/"+idXa, nil, http.StatusOK) // cán bộ xã do phòng quản lý
	f.call("ld_tt", http.MethodPost, "/v1/hrm/staff/"+id1+"/events", ev("award", "2024", map[string]any{"form": "bang_khen_ubnd_tinh"}), http.StatusForbidden)
	f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+idXa+"/sensitive", nil, http.StatusUnprocessableEntity)
	// Ngữ cảnh chỉ xem không ghi.
	f.call("viewonly", http.MethodPost, "/v1/hrm/staff/"+id1+"/events", ev("award", "2024", map[string]any{"form": "bang_khen_ubnd_tinh"}), http.StatusForbidden)

	// Kho: xem chi tiết dùng hrm.profile; chỉ thẻ TK dùng hrm.sensitive.
	reads := map[string]int{}
	for _, x := range f.vault.Accesses() {
		if x.Op == "section_read" || x.Op == "resolve" {
			reads[x.Op+"/"+string(x.Purpose)]++
		}
	}
	if reads["section_read/hrm.sensitive"] != 1 || reads["resolve/hrm.profile"] < 2 || reads["resolve/hrm.sensitive"] != 0 {
		t.Fatalf("lời gọi đọc kho: %v", reads)
	}

	// Quá trình: as-of (AC-03), bậc = phép chiếu (AC-04: Chuyên viên bậc 6 → 3,99), FN-08.
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id1+"/events", ev("step", "2024-01-01", map[string]any{"step": "6"}), http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id1+"/events", ev("step", "2025-01-01", map[string]any{"step": "10"}), http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id1+"/events", ev("unit", "2026-11-01", map[string]any{"unit": "chan_nuoi"}), http.StatusCreated)
	now := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id1, nil, http.StatusOK)["current"].(map[string]any)
	if now["unit"] != "trong_trot" || now["step"] != json.Number("6") || now["coefficient"] != "3.99" || now["step_since"] != "2024-01-01" || now["reserved_coeff"] != "" {
		t.Fatalf("hôm nay: %v", now)
	}
	past := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id1+"?at=2023-06-01", nil, http.StatusOK)["current"].(map[string]any)
	if past["step"] != json.Number("5") || past["coefficient"] != "3.66" || past["reserved_coeff"] != "0.12" {
		t.Fatalf("2023: %v", past)
	}
	later := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id1+"?at=2026-11-15", nil, http.StatusOK)["current"].(map[string]any)
	if later["unit"] != "chan_nuoi" {
		t.Fatalf("sau đổi đơn vị: %v", later)
	}
	if l := f.call("hcth", http.MethodGet, "/v1/hrm/staff?unit=chan_nuoi&at=2026-11-15", nil, http.StatusOK); l["total"] != json.Number("2") {
		t.Fatalf("lọc đơn vị as-of: %v", l)
	}
	// FN-08: ngày nghỉ trước ngày vào làm → chặn; thiếu lý do → chặn; nghỉ 15/08/2026 → as-of.
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id2+"/events", ev("work_end", "2014-01-01", map[string]any{"reason": "nghi_huu"}), http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id2+"/events", ev("work_end", "2026-08-15", map[string]any{}), http.StatusUnprocessableEntity)
	we := f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id2+"/events", ev("work_end", "2026-08-15", map[string]any{"reason": "nghi_huu"}), http.StatusCreated)
	if s := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id2+"?at=2026-08-14", nil, http.StatusOK)["current"].(map[string]any); s["status"] != "active" {
		t.Fatalf("trước nghỉ: %v", s)
	}
	if s := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id2+"?at=2026-08-15", nil, http.StatusOK)["current"].(map[string]any); s["status"] != "left" || s["leave_reason"] != "nghi_huu" {
		t.Fatalf("sau nghỉ: %v", s)
	}
	if l := f.call("hcth", http.MethodGet, "/v1/hrm/staff?status=left", nil, http.StatusOK); l["total"] != json.Number("1") {
		t.Fatalf("lọc đã nghỉ: %v", l)
	}
	// Sửa/hủy dòng quá trình cần lý do; dòng bị thay còn trong lịch sử, không còn hiệu lực.
	weID := we["id"].(string)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id2+"/events/"+weID+"/void", map[string]any{"reason": "ngắn"}, http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id2+"/events/"+weID+"/replace",
		map[string]any{"kind": "work_end", "valid_from": "2026-08-31", "payload": map[string]any{"reason": "nghi_huu"}, "reason": "Sửa theo quyết định nghỉ hưu số 12"},
		http.StatusCreated)
	if s := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id2+"?at=2026-08-20", nil, http.StatusOK)["current"].(map[string]any); s["status"] != "active" {
		t.Fatalf("sau sửa ngày nghỉ: %v", s)
	}
	d2 := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id2, nil, http.StatusOK)
	var live, dead int
	for _, e := range d2["events"].([]any) {
		if e.(map[string]any)["kind"] == "work_end" {
			if e.(map[string]any)["effective"] == true {
				live++
			} else {
				dead++
			}
		}
	}
	if live != 1 || dead != 1 {
		t.Fatalf("dòng nghỉ việc: còn %d, bị thay %d", live, dead)
	}
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id2+"/events/"+weID+"/void", map[string]any{"reason": "Dòng đã bị thay trước đó"}, http.StatusNotFound)

	// Sửa hồ sơ: đổi SĐT, email → sổ ghi TÊN trường; SĐT của người khác → 409.
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id1+"/profile", map[string]any{"phone": "0912000222"}, http.StatusConflict)
	up := f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id1+"/profile", map[string]any{"phone": "0912000999", "email": "mai.tran@ttkn.vn"}, http.StatusOK)
	if up["version"] != json.Number("2") {
		t.Fatalf("sửa hồ sơ: %v", up)
	}
	hist := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id1, nil, http.StatusOK)["history"].([]any)
	if h := hist[len(hist)-1].(map[string]any); h["change"] != "profile" || mustJSON(h["changed_fields"]) != `["phone","email"]` {
		t.Fatalf("lịch sử sửa: %v", hist)
	}
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id1+"/sensitive", map[string]any{"fields": map[string]any{"ethnicity": "khmer", "religion": "khong",
		"hometown": "Xã Mỹ Hòa Hưng", "revolution_date": "2010-05"}}, http.StatusOK)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id1+"/sensitive", map[string]any{"fields": map[string]any{"salary": "1"}}, http.StatusUnprocessableEntity)

	// Xuất: CSV không có trường TK, ghi một dòng nhật ký xuất.
	x := f.call("hcth", http.MethodGet, "/v1/hrm/staff/export?type=vien_chuc", nil, http.StatusOK)
	if raw := x["_raw"].(string); !strings.Contains(raw, "VC-00001") || strings.Contains(raw, "Mỹ Hòa Hưng") || x["_cache"] != "no-store" {
		t.Fatalf("xuất: %v", x)
	}
	f.call("ld_cn", http.MethodGet, "/v1/hrm/staff/export", nil, http.StatusOK) // lãnh đạo: xuất trong đơn vị
	f.call("vc_tt", http.MethodGet, "/v1/hrm/staff/export", nil, http.StatusForbidden)

	// Nhật ký xem/xuất của hồ sơ 1.
	logs := f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id1+"/access-log", nil, http.StatusOK)["items"].([]any)
	acts := map[string]int{}
	for _, it := range logs {
		acts[it.(map[string]any)["action"].(string)]++
	}
	if acts["view"] < 5 || acts["view_sensitive"] != 1 || acts["export"] != 1 {
		t.Fatalf("nhật ký: %v", acts)
	}
	f.call("ld_tt", http.MethodGet, "/v1/hrm/staff/"+id1+"/access-log", nil, http.StatusForbidden)

	// Ma trận quyền (FN-20): chỉ Admin; Admin cố định; nhóm và người dùng (người dùng ưu tiên).
	f.call("hcth", http.MethodGet, "/v1/hrm/permissions", nil, http.StatusForbidden)
	pm := f.call("admin", http.MethodGet, "/v1/hrm/permissions", nil, http.StatusOK)
	if len(pm["items"].([]any)) != 6 {
		t.Fatalf("ma trận: %v", pm)
	}
	if p := f.call("admin", http.MethodPost, "/v1/hrm/permissions", map[string]any{"grantee_kind": "role", "grantee": "admin",
		"perms": map[string]any{"profile.view": "none"}}, http.StatusUnprocessableEntity); p["code"] != "hrm.permission_locked" {
		t.Fatalf("khóa Admin: %v", p)
	}
	f.call("admin", http.MethodPost, "/v1/hrm/permissions", map[string]any{"grantee_kind": "role", "grantee": "manager",
		"perms": map[string]any{"sensitive.view": "all"}}, http.StatusUnprocessableEntity) // BR-03
	f.call("hcth", http.MethodPost, "/v1/hrm/permissions", map[string]any{"grantee_kind": "role", "grantee": "tinh",
		"perms": map[string]any{"profile.view": "self"}}, http.StatusForbidden)
	// Viên chức xem hồ sơ của chính mình khi nhóm "tinh" được "bản thân" và hồ sơ gắn tài khoản.
	vcRef := f.who["vc_tt"].ActorRef.String()
	f.call("hcth", http.MethodPost, "/v1/hrm/staff/"+id2+"/profile", map[string]any{"account_ref": vcRef}, http.StatusOK)
	f.call("admin", http.MethodPost, "/v1/hrm/permissions", map[string]any{"grantee_kind": "role", "grantee": "tinh",
		"perms": map[string]any{"profile.view": "self"}, "note": "Viên chức xem hồ sơ của mình"}, http.StatusCreated)
	if l := f.call("vc_tt", http.MethodGet, "/v1/hrm/staff", nil, http.StatusOK); l["total"] != json.Number("1") || l["items"].([]any)[0].(map[string]any)["id"] != id2 {
		t.Fatalf("bản thân: %v", l)
	}
	f.call("vc_tt", http.MethodGet, "/v1/hrm/staff/"+id1, nil, http.StatusNotFound)
	// Nhóm HCTH bị tắt thẻ TK; một người HCTH được cấp lại theo người dùng.
	f.call("admin", http.MethodPost, "/v1/hrm/permissions", map[string]any{"grantee_kind": "role", "grantee": "hcth",
		"perms": map[string]any{"sensitive.view": "none"}}, http.StatusCreated)
	f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+id1+"/sensitive", nil, http.StatusForbidden)
	f.call("admin", http.MethodPost, "/v1/hrm/permissions", map[string]any{"grantee_kind": "user", "grantee": f.who["hcth2"].ActorRef.String(),
		"perms": map[string]any{"sensitive.view": "all"}}, http.StatusCreated)
	f.call("hcth2", http.MethodGet, "/v1/hrm/staff/"+id1+"/sensitive", nil, http.StatusOK)
	if h := f.call("admin", http.MethodGet, "/v1/hrm/permissions/history", nil, http.StatusOK); len(h["items"].([]any)) != 3 {
		t.Fatalf("nhật ký đổi quyền: %v", h)
	}

	// RLS: tenant khác không thấy hồ sơ.
	if l := f.call("other", http.MethodGet, "/v1/hrm/staff", nil, http.StatusOK); l["total"] != json.Number("0") {
		t.Fatalf("tenant khác: %v", l)
	}
	f.call("other", http.MethodGet, "/v1/hrm/staff/"+id1, nil, http.StatusNotFound)
	f.call("hcth", http.MethodGet, "/v1/hrm/staff/"+ids.New().String(), nil, http.StatusNotFound)

	// Không PII trong pf_hrm (RB-3): giá trị và tên cột.
	admin := f.env.AdminPool(f.t, migrations.PF)
	var dump string
	if err := admin.QueryRow(context.Background(), `SELECT concat_ws(' ',
	    (SELECT string_agg((to_jsonb(s) - 'row_hash' - 'prev_hash')::text, ' ') FROM pf_hrm.staff s WHERE tenant_id = $1),
	    (SELECT string_agg((to_jsonb(e) - 'row_hash' - 'prev_hash')::text, ' ') FROM pf_hrm.staff_event e WHERE tenant_id = $1),
	    (SELECT string_agg((to_jsonb(a) - 'row_hash' - 'prev_hash')::text, ' ') FROM pf_hrm.record_access a WHERE tenant_id = $1),
	    (SELECT string_agg((to_jsonb(p) - 'row_hash' - 'prev_hash')::text, ' ') FROM pf_hrm.perm_rule p WHERE tenant_id = $1))`, f.tid).Scan(&dump); err != nil {
		t.Fatal(err)
	}
	for _, pii := range []string{"Trần", "Mai", "0912000", "912000", "ttkn.vn", "1988-05", "Long Xuyên", "Mỹ Hòa", "khmer"} {
		if strings.Contains(dump, pii) {
			t.Fatalf("pf_hrm chứa dữ liệu cá nhân %q", pii)
		}
	}
	rows, err := admin.Query(context.Background(), `SELECT table_name, column_name FROM information_schema.columns
	  WHERE table_schema = 'pf_hrm' AND table_name IN ('staff', 'staff_event', 'record_access', 'perm_rule')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var tbl, col string
		if err := rows.Scan(&tbl, &col); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"name", "phone", "email", "birth", "address", "national", "ethnic", "religion", "gender", "hometown"} {
			if strings.Contains(col, bad) {
				t.Errorf("pf_hrm.%s.%s trông như dữ liệu cá nhân", tbl, col)
			}
		}
	}
	rows.Close()

	// Sổ chỉ ghi thêm: vai của module không sửa/xóa được nhật ký xem/xuất, hồ sơ.
	for _, q := range []string{"UPDATE pf_hrm.record_access SET item_count = 0", "DELETE FROM pf_hrm.record_access", "UPDATE pf_hrm.staff SET code = 'VC-99999'"} {
		err := db.WithModuleTenantTx(context.Background(), f.svc.DB, app.DBModule, f.tid, func(tx pgx.Tx) error {
			_, e := tx.Exec(context.Background(), q)
			return e
		})
		if err == nil {
			t.Fatalf("%s phải thất bại", q)
		}
	}
	// RLS ở DB: giao dịch tenant khác không thấy dòng.
	var n int
	if err := db.WithModuleTenantTx(context.Background(), f.svc.DB, app.DBModule, ids.New(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_hrm.staff").Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("RLS staff: %d %v", n, err)
	}
	_ = client.PurposeHRMProfile
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
