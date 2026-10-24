package hcplan_test

import (
	"net/http"
	"testing"
)

// Regression Codex review #20 (P2, BR-01): số hiện có/dự kiến nghỉ hưu luôn do máy chủ tính từ hồ sơ tại ngày chốt — kể cả
// khi lãnh đạo đơn vị gọi THẲNG POST /v1/forms/periods/{id}/submissions?profile=hrm_plan với số tự khai. Trước sửa: số giả
// (999/123) được 201 và thành số chính thức sau chốt. Sau sửa: validator profile hrm_plan từ chối mọi bài nộp không mang số
// tự tính của pf/hrm (422 forms.values_invalid); bài nộp qua /v1/hrm/plans dùng số từ hồ sơ.
func TestReviewDirectPlanSubmissionCannotForgeLiveStaffFigures(t *testing.T) {
	f, _ := newPlanFixture(t)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", planStaff("Lê Thị Hưu", "0912200001", "trong_trot", "nu", "1970-05-01"), http.StatusCreated)
	p := f.call("hcth", http.MethodPost, "/v1/hrm/plans", map[string]any{"year": 2027, "due_on": "2026-11-30", "cutoff_on": "2026-10-12",
		"units": []string{"trong_trot", "chan_nuoi"}}, http.StatusCreated)
	pid := p["period_ref"].(string)
	const q = "?profile=hrm_plan"
	if v := rowOf(t, f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusOK), "trong_trot")["values"].(map[string]any); num(v["current"]) != "1" || num(v["retiring"]) != "1" {
		t.Fatalf("tự tính: %v", v)
	}

	// Gọi thẳng cap/forms với số giả → 422; cả số trùng hồ sơ cũng không được tự khai qua đường này.
	for _, vals := range []map[string]string{
		{"current": "999", "retiring": "123", "leaving": "0", "additional": "1", "positions": "[]"},
		{"current": "1", "retiring": "1", "leaving": "0", "additional": "1", "positions": "[]"},
	} {
		e := f.call("ld_tt", http.MethodPost, "/v1/forms/periods/"+pid+"/submissions"+q,
			map[string]any{"unit": "trong_trot", "values": vals, "base_version": 0}, http.StatusUnprocessableEntity)
		if e["code"] != "forms.values_invalid" {
			t.Fatalf("nộp thẳng số tự khai: %v", e)
		}
	}

	// Đường nghiệp vụ: số từ hồ sơ; chốt; báo cáo chính thức giữ số hồ sơ.
	f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/submit", map[string]any{"leaving": 0, "additional": 1, "base_version": 0},
		http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/close", nil, http.StatusOK)
	sum := f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusOK)
	r := rowOf(t, sum, "trong_trot")
	if v := r["values"].(map[string]any); num(v["current"]) != "1" || num(v["retiring"]) != "1" || num(v["additional"]) != "1" || r["source"] != "submitted" {
		t.Fatalf("số chính thức sau chốt: %v", r)
	}
}

// Regression Codex review #20 (P2, BR-02): sau chốt chỉ điều chỉnh có lý do qua sổ plan_adjustment. Trước sửa: HCTH gọi
// POST /v1/forms/periods/{id}/reopen?profile=hrm_plan được 200, lãnh đạo nộp lại qua forms (additional 9) thành số chính
// thức (periodStatus=reopened, source=submitted, không bút toán). Sau sửa: profile hrm_plan NoReopen — reopen 403 với cả
// HCTH và Admin; nộp lại 409 (kỳ đã chốt); ảnh chụp chốt và bảng tổng hợp giữ nguyên; điều chỉnh qua sổ vẫn chạy.
func TestReviewClosedPlanCannotChangeThroughGenericFormsReopen(t *testing.T) {
	f, _ := newPlanFixture(t)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	p := f.call("hcth", http.MethodPost, "/v1/hrm/plans", map[string]any{"year": 2027, "due_on": "2026-11-30", "cutoff_on": "2026-10-12",
		"units": []string{"trong_trot", "chan_nuoi"}}, http.StatusCreated)
	pid := p["period_ref"].(string)
	const q = "?profile=hrm_plan"
	f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/submit", map[string]any{"leaving": 0, "additional": 1, "base_version": 0},
		http.StatusCreated)
	f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/close", nil, http.StatusOK)
	snaps := f.call("hcth", http.MethodGet, "/v1/forms/periods/"+pid+"/snapshots"+q, nil, http.StatusOK)

	// Mở lại qua REST cap/forms dùng chung → 403 (HCTH, Admin, lãnh đạo HCTH).
	for _, as := range []string{"hcth", "admin", "ld_hcth"} {
		f.call(as, http.MethodPost, "/v1/forms/periods/"+pid+"/reopen"+q,
			map[string]any{"reason": "Mở lại để đơn vị nộp bổ sung kế hoạch"}, http.StatusForbidden)
	}
	// Nộp lại sau chốt (forms và HRM) → 409.
	if e := f.call("ld_tt", http.MethodPost, "/v1/forms/periods/"+pid+"/submissions"+q, map[string]any{"unit": "trong_trot",
		"values":       map[string]string{"current": "0", "retiring": "0", "leaving": "0", "additional": "9", "positions": "[]"},
		"base_version": 1}, http.StatusConflict); e["code"] != "forms.period_closed" {
		t.Fatalf("nộp lại qua forms sau chốt: %v", e)
	}
	f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/submit", map[string]any{"leaving": 0, "additional": 9, "base_version": 1},
		http.StatusConflict)

	sum := f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusOK)
	r := rowOf(t, sum, "trong_trot")
	if sum["period"].(map[string]any)["status"] != "closed" || num(r["values"].(map[string]any)["additional"]) != "1" || r["adjusted"] != false || r["source"] != "submitted" {
		t.Fatalf("bảng sau thử mở lại: %v %v", sum["period"], r)
	}
	pv := f.call("hcth", http.MethodGet, "/v1/forms/periods/"+pid+q, nil, http.StatusOK)
	if pv["status"] != "closed" {
		t.Fatalf("kỳ cap/forms: %v", pv["status"])
	}
	if after := f.call("hcth", http.MethodGet, "/v1/forms/periods/"+pid+"/snapshots"+q, nil, http.StatusOK); len(after["items"].([]any)) != 1 ||
		len(snaps["items"].([]any)) != 1 {
		t.Fatalf("ảnh chụp chốt: trước %v sau %v", snaps, after)
	}

	// Đường thay đổi duy nhất sau chốt: điều chỉnh có lý do (sổ plan_adjustment).
	f.call("hcth", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/adjustments",
		map[string]any{"leaving": 0, "additional": 9, "reason": "Bổ sung theo chỉ tiêu mới của Sở"}, http.StatusCreated)
	r = rowOf(t, f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusOK), "trong_trot")
	if r["adjusted"] != true || r["source"] != "adjusted" || num(r["values"].(map[string]any)["additional"]) != "9" {
		t.Fatalf("điều chỉnh sau chốt: %v", r)
	}
}
