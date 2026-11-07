package handler

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"net/http"
	"testing"
)

func TestHRMAppraisalPeriodV4GroupsWarningsAndActions281(t *testing.T) {
	a := newAppraisalFixture(t)
	f := a.f
	// Viên chức không có tài khoản vẫn thuộc đợt; đề xuất do lãnh đạo nhập.
	f.call("hcth", http.MethodPost, "/v1/hrm/staff", newStaff("vien_chuc", "Người Không Tài Khoản", "0912345699", ev("unit", "2020-01-01", map[string]any{"unit": "trong_trot"}), ev("position", "2020-01-01", map[string]any{"position": "vien_chuc"}), ev("work_start", "2020-01-01", map[string]any{})), 201)
	period := a.open(t, "281-period-v4-open")
	base := "/v1/hrm/appraisals/periods/" + period
	list := appraisalCall(t, f, "hcth", http.MethodGet, "/v1/hrm/appraisals/periods", "", nil, 200)["items"].([]any)
	if len(list) != 1 || len(list[0].(map[string]any)["warnings"].([]any)) != 0 {
		t.Fatal("thiếu tài khoản vẫn bắt scan hoặc OTP")
	}
	for _, action := range []string{"lock_self", "unlock_self", "lock_manager", "unlock_manager"} {
		appraisalCall(t, f, "hcth", http.MethodPost, base, "281-period-retired-"+action, map[string]any{"action": action, "version": 1}, 422)
	}
	appraisalCall(t, f, "hcth", http.MethodPost, base, "281-period-extend-old-field", map[string]any{"action": "extend", "version": 1, "self_due_at": "2026-11-01T00:00:00Z"}, 422)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "281-period-extend", app.AppraisalPeriodChange{Action: "extend", Version: 1, ManagerDueAt: "2026-11-01T00:00:00Z"}, 201)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "281-period-remind", app.AppraisalPeriodChange{Action: "remind", Version: 2, Step: "manager"}, 201)
	appraisalCall(t, f, "ld_tt", http.MethodPost, base, "281-period-manager-extend", app.AppraisalPeriodChange{Action: "extend", Version: 3, ManagerDueAt: "2026-12-01T00:00:00Z"}, 403)
	appraisalCall(t, f, "hcth", http.MethodPost, base, "281-period-close-missing", app.AppraisalPeriodChange{Action: "close", Version: 3, AcknowledgeUnsigned: true}, 422)
}
