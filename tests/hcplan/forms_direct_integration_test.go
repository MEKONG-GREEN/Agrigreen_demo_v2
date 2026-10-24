package hcplan_test

import (
	"net/http"
	"testing"
)

// W11.27 — chính sách phiên của profile hrm_plan áp cả khi gọi THẲNG REST cap/forms (/v1/forms) dùng chung: phiên CHỈ XEM
// (lãnh đạo xem phòng ngoài lần gán, nhóm Ban Giám đốc, Admin chỉ xem) bị từ chối ở mọi thao tác — định nghĩa biểu, phát hành
// kỳ, nộp, rà soát, chốt — và cả đọc kỳ (FN-11 không cấp quyền xem cho ngữ cảnh chỉ xem); phiên thường vẫn làm được (nộp bài
// kế hoạch chỉ qua /v1/hrm/plans — BR-01).
func TestHRMPlanFormsDirectViewOnly_W1127(t *testing.T) {
	f, _ := newPlanFixture(t)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	p := f.call("hcth", http.MethodPost, "/v1/hrm/plans", map[string]any{"year": 2027, "due_on": "2026-11-30", "cutoff_on": "2026-10-12",
		"units": []string{"trong_trot", "chan_nuoi", "hcth"}}, http.StatusCreated)
	pid := p["period_ref"].(string)
	const q = "?profile=hrm_plan"
	values := map[string]string{"current": "0", "retiring": "0", "leaving": "0", "additional": "1", "positions": "[]"}

	// Phiên thường gọi thẳng /v1/forms: định nghĩa biểu, phát hành kỳ (HCTH), nộp (lãnh đạo đơn vị).
	tpl := f.call("hcth", http.MethodPost, "/v1/forms/templates", map[string]any{"profile": "hrm_plan", "code": "HRM_DIRECT",
		"title": "Biểu thử gọi thẳng", "fields": []map[string]any{{"key": "a", "label": "A", "type": "integer"}}}, http.StatusCreated)
	pub := f.call("hcth", http.MethodPost, "/v1/forms/periods", map[string]any{"profile": "hrm_plan", "template_id": tpl["id"],
		"label": "Kỳ gọi thẳng", "due_at": "2026-12-31T16:59:59Z", "units": []string{"trong_trot"}, "unit_kind": "department"}, http.StatusCreated)
	pid2 := pub["period"].(map[string]any)["id"].(string)
	// Nộp thẳng /v1/forms không mang số tự tính từ hồ sơ → 422 (BR-01); lãnh đạo đơn vị nộp qua /v1/hrm/plans.
	f.call("ld_tt", http.MethodPost, "/v1/forms/periods/"+pid+"/submissions"+q,
		map[string]any{"unit": "trong_trot", "values": values, "base_version": 0}, http.StatusUnprocessableEntity)
	sub := f.call("ld_tt", http.MethodPost, "/v1/hrm/plans/2027/units/trong_trot/submit", map[string]any{"leaving": 0, "additional": 1,
		"base_version": 0}, http.StatusCreated)
	sid := sub["id"].(string)

	// Phiên chỉ xem — lãnh đạo phòng, lãnh đạo HCTH (Ban Giám đốc), Admin — mọi lời gọi thẳng đều bị từ chối.
	for _, as := range []string{"vo_mgr", "vo_hcth", "vo_admin"} {
		f.call(as, http.MethodGet, "/v1/forms/templates"+q, nil, http.StatusForbidden)
		f.call(as, http.MethodPost, "/v1/forms/templates", map[string]any{"profile": "hrm_plan", "code": "HRM_VO",
			"title": "x", "fields": []map[string]any{{"key": "a", "label": "A", "type": "integer"}}}, http.StatusForbidden)
		f.call(as, http.MethodPost, "/v1/forms/periods", map[string]any{"profile": "hrm_plan", "template_id": tpl["id"],
			"label": "Kỳ chỉ xem", "due_at": "2026-12-31T16:59:59Z", "units": []string{"trong_trot"}, "unit_kind": "department"}, http.StatusForbidden)
		f.call(as, http.MethodGet, "/v1/forms/periods/"+pid+q, nil, http.StatusForbidden)
		f.call(as, http.MethodPost, "/v1/forms/periods/"+pid+"/submissions"+q,
			map[string]any{"unit": "trong_trot", "values": values, "base_version": 1}, http.StatusForbidden)
		f.call(as, http.MethodPost, "/v1/forms/submissions/"+sid+"/review"+q, map[string]any{"decision": "accepted"}, http.StatusForbidden)
		f.call(as, http.MethodPost, "/v1/forms/periods/"+pid2+"/close"+q, nil, http.StatusForbidden)
		f.call(as, http.MethodPost, "/v1/forms/periods/"+pid2+"/cancel"+q, map[string]any{"reason": "Hủy thử từ phiên chỉ xem"}, http.StatusForbidden)
		// Cùng chính sách ở API nghiệp vụ.
		f.call(as, http.MethodGet, "/v1/hrm/plans", nil, http.StatusForbidden)
		f.call(as, http.MethodPost, "/v1/hrm/plans/2027/close", nil, http.StatusForbidden)
		f.call(as, http.MethodPut, "/v1/hrm/plans/2027/units/trong_trot/draft", map[string]any{"leaving": 0, "additional": 0}, http.StatusForbidden)
	}

	// Phiên thường vẫn làm được: rà soát, chốt qua /v1/forms; kỳ kế hoạch còn nguyên bài nộp của lãnh đạo.
	f.call("hcth", http.MethodPost, "/v1/forms/submissions/"+sid+"/review"+q, map[string]any{"decision": "accepted"}, http.StatusOK)
	f.call("hcth", http.MethodPost, "/v1/forms/periods/"+pid2+"/close"+q, nil, http.StatusOK)
	if r := rowOf(t, f.call("hcth", http.MethodGet, "/v1/hrm/plans/2027", nil, http.StatusOK), "trong_trot"); r["status"] != "accepted" {
		t.Fatalf("bài nộp qua /v1/forms: %v", r)
	}
	// Admin thường (không chỉ xem) vẫn là cấp Trung tâm trên /v1/forms.
	f.call("admin", http.MethodGet, "/v1/forms/periods/"+pid+q, nil, http.StatusOK)
}
