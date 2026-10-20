// Package hcplan_test kiểm thử tích hợp kế hoạch nhân lực năm (W11.27, BRD HCTH v3.0 FN-11, D3 H7) trên module THẬT:
// pf/hrm (hồ sơ W11.22 + kế hoạch) và pf/forms (cap/forms, profile "hrm_plan" do pf/hrm đăng ký) trên PostgreSQL, mỗi
// module bằng vai DB riêng — nối như gốc lắp ráp cmd/api-platform. Kho định danh là bản giả clienttest.
package hcplan_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type unitsStub []port.Unit

func (u unitsStub) InternalUnits(context.Context, uuid.UUID) ([]port.Unit, error) { return u, nil }

type fixture struct {
	t   *testing.T
	r   *gin.Engine
	who map[string]authz.Principal
}

func (f *fixture) call(as, method, path string, body any, want int) map[string]any {
	f.t.Helper()
	rd := bytes.NewReader(nil)
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
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
	return m
}

// newPlanFixture: pf/hrm (hồ sơ bật, đơn vị nội bộ giả) + pf/forms THẬT với profile hrm_plan và validator do pf/hrm đăng ký.
func newPlanFixture(t *testing.T) (*fixture, *forms.Module) {
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC))
	acc := port.Access{Department: "hanh_chinh_tong_hop", ManageBy: []string{"admin"}, ReadBy: []string{"kn_tinh"}}
	hm, err := hrm.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clk, acc)
	if err != nil {
		t.Fatal(err)
	}
	fm, err := forms.New(env.Pool(t, migrations.PF, dbadmin.AppPlatform), clk, forms.Deps{Profiles: hrm.FormProfiles(acc),
		Validators: map[string]formsport.Validator{port.PlanProfile: hrm.PlanValidator()}})
	if err != nil {
		t.Fatal(err)
	}
	hm.UseRecords(clienttest.New(), unitsStub{{Code: "trong_trot", Name: "Phòng Trồng trọt", Active: true},
		{Code: "chan_nuoi", Name: "Phòng Chăn nuôi", Active: true}, {Code: "hcth", Name: "Phòng HCTH", Active: true},
		{Code: "da_ngung", Active: false}}, true)
	hm.UsePlans(fm.Publisher(), fm.Control(), fm.Entry())
	tid, org := ids.New(), ids.New()
	kn := func(role, dept, unit string, manager bool) *authz.KNContext {
		return &authz.KNContext{Role: role, Department: dept, InternalUnit: unit, Manager: manager, UnitRef: org}
	}
	f := &fixture{t: t, who: map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}, UnitRef: org}},
		"hcth":  {ActorRef: ids.New(), TenantID: tid, KN: kn("kn_tinh", "hanh_chinh_tong_hop", "hcth", false)},
		"hcth2": {ActorRef: ids.New(), TenantID: tid, KN: kn("kn_tinh", "hanh_chinh_tong_hop", "hcth", false)},
		// Lãnh đạo Phòng HCTH: cùng phiên là cấp Trung tâm (tổng hợp, chốt) và lãnh đạo đơn vị HCTH (lập kế hoạch của phòng).
		"ld_hcth":   {ActorRef: ids.New(), TenantID: tid, KN: kn("kn_tinh", "hanh_chinh_tong_hop", "hcth", true)},
		"ld_tt":     {ActorRef: ids.New(), TenantID: tid, KN: kn("kn_tinh", "trong_trot", "trong_trot", true)},
		"ld_cn":     {ActorRef: ids.New(), TenantID: tid, KN: kn("kn_tinh", "chan_nuoi", "chan_nuoi", true)},
		"vc_tt":     {ActorRef: ids.New(), TenantID: tid, KN: kn("kn_tinh", "trong_trot", "trong_trot", false)},
		"xa":        {ActorRef: ids.New(), TenantID: tid, KN: kn("kn_xa", "trong_trot", "trong_trot", false)},
		"tw":        {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: "kn_tw"}},
		"other":     {ActorRef: ids.New(), TenantID: ids.New(), KN: &authz.KNContext{Role: "admin", Scope: authz.Scope{All: true}}},
		"realmonly": {ActorRef: ids.New(), TenantID: tid, Roles: []string{"admin"}},
	}}
	gin.SetMode(gin.TestMode)
	f.r = gin.New()
	f.r.Use(func(c *gin.Context) {
		if p, ok := f.who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		}
	})
	hm.RegisterApp(f.r.Group("/v1"))
	return f, fm
}

func newStaff(typ, name, phone string, events ...map[string]any) map[string]any {
	return map[string]any{"type": typ,
		"profile": map[string]any{"full_name": name, "phone": phone, "address": "Phường Long Xuyên", "birth_date": "1988-05-01",
			"gender": "nu", "email": phone + "@ttkn.vn"},
		"events": events}
}

func ev(kind, from string, payload map[string]any) map[string]any {
	return map[string]any{"kind": kind, "valid_from": from, "payload": payload}
}

func planStaff(name, phone, unit, gender, birth string) map[string]any {
	b := newStaff("vien_chuc", name, phone, ev("unit", "2020-01-01", map[string]any{"unit": unit}),
		ev("position", "2020-01-01", map[string]any{"position": "vien_chuc"}), ev("work_start", "2015-03-01", map[string]any{}))
	p := b["profile"].(map[string]any)
	p["gender"], p["birth_date"] = gender, birth
	return b
}

func rowOf(t *testing.T, sum map[string]any, unit string) map[string]any {
	t.Helper()
	for _, r := range sum["rows"].([]any) {
		if m := r.(map[string]any); m["unit"] == unit {
			return m
		}
	}
	t.Fatalf("không có dòng %s: %v", unit, sum)
	return nil
}

func num(v any) string { return string(v.(json.Number)) }

// W11.27 (FN-11, D3 H7) qua HTTP trên cap/forms thật: mở kỳ năm (B1), số hiện có/dự kiến nghỉ hưu tự tính tại ngày chốt
// (BR-01, AC-01), nháp, gửi (lãnh đạo đơn vị, kể cả lãnh đạo Phòng HCTH), yêu cầu điều chỉnh/xác nhận (bốn mắt), bảng tổng
// hợp cộng đúng (AC-02), chốt, sau chốt chỉ điều chỉnh có lý do (BR-02), xuất Excel, sao chép năm trước, validator profile,
// phân quyền và cách ly tenant.
func TestHRMPlansHTTP_W1127(t *testing.T) {
	f, fm := newPlanFixture(t)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	// Hồ sơ: Trồng trọt 2 viên chức (một người nữ sinh 05/1970 — đủ 57 tuổi 05/2027 → dự kiến nghỉ hưu 2027), Chăn nuôi 1,
	// cán bộ xã do Trồng trọt quản lý (không tính vào số hiện có).
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", planStaff("Lê Thị Hưu", "0912100001", "trong_trot", "nu", "1970-05-01"), http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", planStaff("Trần Văn Trẻ", "0912100002", "trong_trot", "nam", "1990-02-01"), http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", planStaff("Phạm Thị Nuôi", "0912100003", "chan_nuoi", "nu", "1985-01-01"), http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("can_bo_xa", "Nguyễn Văn Xã", "0912100004",
		ev("unit", "2022-01-01", map[string]any{"commune": "30292", "mgmt_unit": "trong_trot"}),
		ev("position", "2020-01-01", map[string]any{"position": "vien_chuc"})), http.StatusCreated)

	// Quyền: viên chức, Cấp Xã, Cấp TW không có kế hoạch; lãnh đạo phòng không mở kỳ.
	for _, as := range []string{"vc_tt", "xa", "tw", "realmonly"} {
		f.call(as, http.MethodGet, "/v1/hrm/plans", nil, http.StatusForbidden)
	}
	open := map[string]any{"year": 2027, "due_on": "2026-11-30", "cutoff_on": "2026-10-12", "units": []string{"trong_trot", "chan_nuoi", "hcth"}}
	f.call("ld_tt", http.MethodPost, "/v1/hrm/plans", open, http.StatusForbidden)
	f.call("hcth", http.MethodPost, "/v1/hrm/plans", map[string]any{"year": 2027, "due_on": "2026-11-30", "units": []string{"da_ngung"}},
		http.StatusUnprocessableEntity)
	f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusNotFound)

	// B1: mở kỳ (kỳ native cap/forms, khóa kỳ năm); mở lại cùng năm → 409.
	p := f.call("hcth", http.MethodPost, "/v1/hrm/plans", open, http.StatusCreated)
	if p["status"] != "open" || p["cutoff_on"] != "2026-10-12" || p["label"] != "Kế hoạch nhân lực năm 2027" {
		t.Fatalf("mở kỳ: %v", p)
	}
	if e := f.call("hcth", http.MethodPost, "/v1/hrm/plans", open, http.StatusConflict); e["code"] != "hrm.plan_exists" {
		t.Fatalf("mở trùng: %v", e)
	}

	// AC-01: số hiện có, dự kiến nghỉ hưu tự tính tại ngày chốt (cán bộ xã không tính).
	sum := f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusOK)
	tt := rowOf(t, sum, "trong_trot")
	if v := tt["values"].(map[string]any); num(v["current"]) != "2" || num(v["retiring"]) != "1" || tt["status"] != "not_started" || tt["source"] != "auto" {
		t.Fatalf("Trồng trọt tự tính: %v", tt)
	}
	if v := rowOf(t, sum, "chan_nuoi")["values"].(map[string]any); num(v["current"]) != "1" || num(v["retiring"]) != "0" {
		t.Fatalf("Chăn nuôi tự tính: %v", v)
	}
	d := f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027/units/trong_trot", nil, http.StatusOK)
	ret := d["figures"].(map[string]any)["retirees"].([]any)
	if len(ret) != 1 || ret[0].(map[string]any)["full_name"] != "Lê Thị Hưu" || ret[0].(map[string]any)["retire_on"] != "2027-05-01" {
		t.Fatalf("danh sách dự kiến nghỉ hưu: %v", d["figures"])
	}

	// Lãnh đạo Trồng trọt chỉ thấy đơn vị mình.
	if l := f.call("ld_tt", http.MethodGet, "/v1/hrm/plans", nil, http.StatusOK); len(l["items"].([]any)) != 1 {
		t.Fatalf("danh sách kỳ của lãnh đạo: %v", l)
	}
	if s := f.call("ld_tt", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusOK); len(s["rows"].([]any)) != 1 || s["can_manage"] != false {
		t.Fatalf("bảng của lãnh đạo: %v", s)
	}
	f.call("ld_tt", http.MethodGet, "/v1/hrm/plans/2027/units/chan_nuoi", nil, http.StatusNotFound)

	// B2: nháp → "Đang lập"; gửi (người khác đơn vị, HCTH không phải lãnh đạo → 403).
	in := map[string]any{"leaving": 1, "additional": 2, "positions": []map[string]any{
		{"title": "Kỹ sư trồng trọt", "level": "Đại học", "count": 2, "when": "Quý I/2027", "reason": "Thay người nghỉ hưu"}}, "note": ""}
	f.call("ld_tt", http.MethodPut, "/v1/hrm/plans/2027/units/trong_trot/draft", map[string]any{"leaving": -1, "additional": 0}, http.StatusUnprocessableEntity)
	f.call("ld_tt", http.MethodPut, "/v1/hrm/plans/2027/units/trong_trot/draft", in, http.StatusOK)
	if r := rowOf(t, f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusOK), "trong_trot"); r["status"] != "draft" {
		t.Fatalf("nháp: %v", r)
	}
	f.call("ld_cn", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/submit", in, http.StatusForbidden)
	f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/submit", in, http.StatusForbidden)
	sub := f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/submit", in, http.StatusCreated)
	if v := sub["values"].(map[string]any); num(v["current"]) != "2" || num(v["retiring"]) != "1" || num(v["demand"]) != "2" || sub["late"] != false {
		t.Fatalf("gửi: %v", sub)
	}
	// Khóa lạc quan: gửi lại với phiên bản cũ → 409.
	f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/submit", in, http.StatusConflict)

	// B3: yêu cầu điều chỉnh (ghi chú bắt buộc), gửi lại, xác nhận.
	f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/review", map[string]any{"decision": "accepted"}, http.StatusForbidden)
	f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/review", map[string]any{"decision": "returned", "note": "Bổ sung lý do vị trí"}, http.StatusOK)
	if r := rowOf(t, f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusOK), "trong_trot"); r["status"] != "returned" || r["review_note"] != "Bổ sung lý do vị trí" {
		t.Fatalf("yêu cầu điều chỉnh: %v", r)
	}
	in2 := map[string]any{"leaving": 1, "additional": 2, "base_version": 1, "positions": []map[string]any{
		{"title": "Kỹ sư trồng trọt", "level": "Đại học", "count": 2, "when": "Quý I/2027", "reason": "Thay người nghỉ hưu và mở rộng mô hình"}}}
	if s := f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/submit", in2, http.StatusCreated); num(s["version"]) != "2" {
		t.Fatalf("gửi lại: %v", s)
	}
	f.call("hcth2", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/review", map[string]any{"decision": "accepted"}, http.StatusOK)
	if dd := f.call("ld_tt", http.MethodGet, "/v1/hrm/plans/2027/units/trong_trot", nil, http.StatusOK); dd["row"].(map[string]any)["status"] != "accepted" ||
		dd["can_edit"] != false || len(dd["submissions"].([]any)) != 2 {
		t.Fatalf("đã xác nhận: %v", dd)
	}

	// Lãnh đạo Phòng HCTH lập kế hoạch của HCTH (tư cách đơn vị); tự rà soát bài mình → bốn mắt chặn.
	f.call("ld_hcth", http.MethodPost, "/v1/hrm/plans/2027/units/hcth/submit", map[string]any{"leaving": 0, "additional": 1}, http.StatusCreated)
	if e := f.call("ld_hcth", http.MethodPost, "/v1/hrm/plans/2027/units/hcth/review", map[string]any{"decision": "accepted"}, http.StatusForbidden); e["code"] != problem.SelfApproval.Code {
		t.Fatalf("tự rà soát: %v", e)
	}

	// AC-02: dòng Toàn Trung tâm cộng đúng các đơn vị.
	sum = f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusOK)
	tot := sum["totals"].(map[string]any)
	var cur, dem int
	for _, r := range sum["rows"].([]any) {
		v := r.(map[string]any)["values"].(map[string]any)
		var a, b int
		_ = json.Unmarshal([]byte(num(v["current"])), &a)
		_ = json.Unmarshal([]byte(num(v["demand"])), &b)
		cur, dem = cur+a, dem+b
	}
	if num(tot["current"]) != "3" || num(tot["current"]) != jsonInt(cur) || num(tot["demand"]) != jsonInt(dem) {
		t.Fatalf("tổng: %v (cộng %d, %d)", tot, cur, dem)
	}

	// Validator profile hrm_plan: bài nộp trực tiếp cap/forms sai hình dạng nhu cầu theo vị trí → chặn.
	ctxCN := authz.WithPrincipal(context.Background(), f.who["ld_cn"])
	_, err := fm.Entry().Submit(ctxCN, port.PlanProfile, uuid.MustParse(p["period_ref"].(string)), "chan_nuoi",
		map[string]string{"current": "1", "retiring": "0", "leaving": "0", "additional": "0", "positions": "không phải JSON"}, 0, "")
	var pe *problem.Problem
	if !errors.As(err, &pe) || pe.Code != formsport.ErrInvalidValues.Code {
		t.Fatalf("validator: %v", err)
	}

	// Điều chỉnh trước chốt → 409; chốt (HCTH); sau chốt không gửi/nháp được, chỉ điều chỉnh có lý do.
	f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/adjustments",
		map[string]any{"leaving": 0, "additional": 3, "reason": "Bổ sung theo chỉ tiêu mới của Sở"}, http.StatusConflict)
	f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/close", nil, http.StatusForbidden)
	cl := f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/close", nil, http.StatusOK)
	if cl["period"].(map[string]any)["status"] != "closed" || rowOf(t, cl, "chan_nuoi")["status"] != "missed" || rowOf(t, cl, "trong_trot")["status"] != "closed" {
		t.Fatalf("chốt: %v", cl)
	}
	if e := f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/close", nil, http.StatusConflict); e["code"] != "hrm.plan_closed" {
		t.Fatalf("chốt lại: %v", e)
	}
	f.call("ld_tt", http.MethodPut, "/v1/hrm/plans/2027/units/trong_trot/draft", in, http.StatusConflict)
	f.call("ld_cn", http.MethodPost, "/v1/hrm/plans/2027/units/chan_nuoi/submit", in, http.StatusConflict)
	f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/adjustments", map[string]any{"leaving": 0, "additional": 3, "reason": "ngắn"},
		http.StatusUnprocessableEntity)
	f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/adjustments",
		map[string]any{"leaving": 0, "additional": 3, "reason": "Bổ sung theo chỉ tiêu mới của Sở"}, http.StatusForbidden)
	adj := f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/adjustments",
		map[string]any{"leaving": 0, "additional": 3, "reason": "Bổ sung theo chỉ tiêu mới của Sở"}, http.StatusCreated)
	if v := adj["values"].(map[string]any); num(v["current"]) != "2" || num(v["retiring"]) != "1" || num(v["demand"]) != "4" {
		t.Fatalf("điều chỉnh: %v", adj)
	}
	tt = rowOf(t, f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusOK), "trong_trot")
	if tt["adjusted"] != true || tt["source"] != "adjusted" || num(tt["values"].(map[string]any)["additional"]) != "3" {
		t.Fatalf("bảng sau điều chỉnh: %v", tt)
	}
	dd := f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027/units/trong_trot", nil, http.StatusOK)
	if len(dd["adjustments"].([]any)) != 1 || dd["can_adjust"] != true || len(dd["submissions"].([]any)) != 2 {
		t.Fatalf("lịch sử điều chỉnh: %v", dd)
	}

	// Xuất Excel bảng tổng hợp (HCTH; lãnh đạo phòng không).
	f.call("ld_tt", http.MethodGet, "/v1/hrm/plans/2027/export", nil, http.StatusForbidden)
	req := httptest.NewRequest(http.MethodGet, "/v1/hrm/plans/2027/export", nil)
	req.Header.Set("X-Test-As", "hcth")
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "ke-hoach-nhan-luc-2027.xlsx") || w.Body.Len() < 1000 {
		t.Fatalf("xuất: %d %v", w.Code, w.Header())
	}

	// Sao chép năm trước: kỳ 2028, lãnh đạo Trồng trọt lấy phần nhập (sau điều chỉnh) của 2027; Chăn nuôi không gửi → 404.
	f.call("hcth", http.MethodPost, "/v1/hrm/plans", map[string]any{"year": 2028, "due_on": "2027-11-30", "units": []string{"trong_trot", "chan_nuoi"}}, http.StatusCreated)
	prev := f.call("ld_tt", http.MethodGet, "/v1/hrm/plans/2028/units/trong_trot/previous", nil, http.StatusOK)
	if num(prev["from_year"]) != "2027" || num(prev["values"].(map[string]any)["additional"]) != "3" {
		t.Fatalf("sao chép năm trước: %v", prev)
	}
	if e := f.call("ld_cn", http.MethodGet, "/v1/hrm/plans/2028/units/chan_nuoi/previous", nil, http.StatusNotFound); e["code"] != "hrm.plan_no_previous" {
		t.Fatalf("không có năm trước: %v", e)
	}

	// Cách ly tenant: tenant khác không thấy kỳ.
	f.call("other", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusNotFound)
	if l := f.call("other", http.MethodGet, "/v1/hrm/plans", nil, http.StatusOK); len(l["items"].([]any)) != 0 {
		t.Fatalf("tenant khác: %v", l)
	}
}

func jsonInt(n int) string { b, _ := json.Marshal(n); return string(b) }
