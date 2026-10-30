// Test fixture copied from existing KN77 helpers only, without duplicating legacy Test cases.
// Colocated probes may inspect their own module internals; composition-root tests remain port-only.
package adhocintegration_test

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"

	"net/http"
)

type adhocTestRequest struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	ReturnApplied *bool  `json:"return_applied"`
}

func (f *reportFx) openAdhoc(name string) khtcPeriod {
	f.t.Helper()
	var p khtcPeriod
	c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{"report_type": "adhoc", "year": 2026, "name": name, "from_date": "2026-10-01", "to_date": "2026-10-13", "due_at": "2026-10-20T10:00:00+07:00", "export_due_at": "2026-10-21T16:00:00+07:00", "departments": []string{dTT, dTTTT}})
	f.ok(c, b, &p)
	return p
}
func adhocRequestPath(id string) string {
	return "/v1/extension/khtc/periods/" + id + "/adhoc-requests"
}
func adhocRequestBody() map[string]any {
	return map[string]any{"department": dTT, "instruction": "Bổ sung số lớp tập huấn theo chỉ đạo 15", "due_at": "2026-10-19T10:00:00+07:00", "indicators": []map[string]string{{"code": "SO_LOP", "name": "Số lớp yêu cầu mới", "unit": "lớp"}}}
}
func (f *reportFx) request(pid, key string) adhocTestRequest {
	f.t.Helper()
	var r adhocTestRequest
	c, b := f.do(http.MethodPost, adhocRequestPath(pid), authz.KNTinh, khtcDept, f.khtcUser, key, adhocRequestBody())
	f.ok(c, b, &r)
	if r.State != "issued" {
		f.t.Fatal("request not issued", r)
	}
	return r
}
func answerSheet(token, value string) map[string]any {
	return map[string]any{"cells": map[string]any{}, "adjustments": []any{}, "texts": map[string]string{}, "adhoc_request_id": token, "adhoc_values": map[string]string{"SO_LOP": value}, "adhoc_text": "Phòng đã đối chiếu danh sách lớp."}
}
