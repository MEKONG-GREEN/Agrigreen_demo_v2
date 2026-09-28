package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

type fakeStaff map[uuid.UUID]string

func (f fakeStaff) DisplayNames(_ context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	for _, r := range refs {
		if n, ok := f[r]; ok {
			out[r] = n
		}
	}
	return out, nil
}

// G-06 danh sách danh mục; G-07 sổ đăng ký tham số và tên người đặt/duyệt.
func TestCatalogsAndParamRegistryG06G07(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, mdport.RoleMasterdataAdmin)
	code, body := admin.adm(http.MethodGet, "/v1/masterdata/catalogs", nil)
	if code != 200 {
		t.Fatalf("catalogs: %d %v", code, body)
	}
	var rice map[string]any
	for _, it := range body["items"].([]any) {
		if m := it.(map[string]any); m["catalog"] == "rice_variety" {
			rice = m
		}
	}
	if rice == nil || rice["name"] != "Giống lúa" || rice["item_count"].(float64) < 1 || len(strs(rice["attrs_schema"])) == 0 {
		t.Fatalf("rice_variety: %v", rice)
	}
	if c, _ := f.caller(tid, mdport.RoleParamSetter).adm(http.MethodGet, "/v1/masterdata/catalogs", nil); c != 403 {
		t.Fatalf("không có vai masterdata_admin: %d", c)
	}

	setter := f.caller(tid, mdport.RoleParamSetter)
	approver := f.caller(tid, mdport.RoleParamApprover)
	f.mod.UseStaffDirectory(fakeStaff{setter.actor: "Trần Thị C"})
	code, body = setter.adm(http.MethodGet, "/v1/masterdata/params?prefix=cgh.", nil)
	if code != 200 {
		t.Fatalf("params: %d %v", code, body)
	}
	var suff, norm map[string]any
	for _, it := range body["items"].([]any) {
		m := it.(map[string]any)
		switch m["key"] {
		case "cgh.coverage.sufficient_min_pct":
			suff = m
		case "cgh.norm_ha_per_machine:MLD:lam_dat":
			norm = m
		}
		if k := m["key"].(string); k[:4] != "cgh." {
			t.Fatalf("lọc tiền tố: %s", k)
		}
	}
	if suff == nil || suff["name"] != "Ngưỡng Đủ máy (mức đáp ứng)" || suff["unit"] != "%" || suff["kind"] != "decimal" ||
		suff["current"].(map[string]any)["value"] != "85" {
		t.Fatalf("ngưỡng Đủ: %v", suff)
	}
	if norm == nil || norm["name"] != "Định mức máy — MLD × khâu làm đất" {
		t.Fatalf("định mức: %v", norm)
	}
	// Khóa đã biết chưa có phiên bản vẫn có trong sổ.
	_, body = setter.adm(http.MethodGet, "/v1/masterdata/params?prefix=sync.", nil)
	if its := body["items"].([]any); len(its) != 1 || its[0].(map[string]any)["current"] != nil {
		t.Fatalf("sync.conflict_sla_hours chưa có phiên bản: %v", body)
	}

	// Đề xuất → gửi duyệt → submitted_count, setter_name.
	_, v := setter.adm(http.MethodPost, "/v1/masterdata/params/versions", map[string]any{
		"key": "cgh.coverage.sufficient_min_pct", "value": "90", "unit": "%", "effective_from": time.Now().AddDate(0, 1, 0).Format("2006-01-02"),
		"source_doc": "QĐ thử nghiệm",
	})
	id := v["id"].(string)
	setter.adm(http.MethodPost, "/v1/masterdata/params/versions/"+id+"/submit", nil)
	_, body = setter.adm(http.MethodGet, "/v1/masterdata/params?prefix=cgh.coverage.sufficient", nil)
	if its := body["items"].([]any); its[0].(map[string]any)["submitted_count"] != 1.0 {
		t.Fatalf("submitted_count: %v", body)
	}
	_, vv := approver.adm(http.MethodGet, "/v1/masterdata/params/versions/"+id, nil)
	if vv["setter_name"] != "Trần Thị C" || vv["approver_name"] != nil {
		t.Fatalf("tên người đặt: %v", vv)
	}
}
