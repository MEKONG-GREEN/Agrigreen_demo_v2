package kn77_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// TestFN_XA_03_02_LayoutAcceptsKNContext: màn nhập báo cáo của Cấp Xã (FN-XA-03.02) đọc layout, mẫu Excel và tính thử
// bằng tài khoản KN v7.7 CHỈ có vai cổng kn_user (vai nghiệp vụ ở ngữ cảnh phiên, P1) — trước đây bị 403 vì chỉ xét vai
// realm (formReaders). Không có ngữ cảnh → vẫn 403; ngữ cảnh khác phòng ban → không thấy mẫu (404, không lộ).
func TestFN_XA_03_02_LayoutAcceptsKNContext(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	forms := f.publishForms("T01")
	c1 := f.communes89[0]
	xa := f.kn(f.tenant, authz.KNXa, dept, f.p89, []string{c1})
	base := "/v1/reports/forms/" + forms["T01"] + "/versions/1"

	r := xa.must(xa.get(base+"/layout?period_label=Tuần%201&period_label=Tuần%202&season=Thu%20Đông%202026"), http.StatusOK, "layout bằng ngữ cảnh KN")
	def := obj(r.body, "definition")
	if str(def, "code") != "T01" {
		t.Fatalf("layout: %v", r.body)
	}
	var titled bool
	for _, c := range arr(def, "columns") {
		if str(c, "letter") == "F" && str(c, "title") == "Tuần 1" {
			titled = true
		}
	}
	if !titled {
		t.Fatalf("nhãn kỳ theo đợt: %v", def["columns"])
	}
	versionID := str(obj(r.body, "version"), "id")

	// Mẫu Excel và tính thử cùng quy tắc (Application API).
	x := xa.get(base + "/template.xlsx")
	if x.status != http.StatusOK || len(x.raw) < 100 {
		t.Fatalf("mẫu Excel: %d", x.status)
	}
	ev := xa.must(xa.post("/v1/reports/form-versions/"+versionID+"/evaluate", map[string]any{"mode": "draft", "rows": []map[string]any{
		{"commune": c1, "input": map[string]string{"F": "2", "G": "3"}},
	}}), http.StatusOK, "tính thử bằng ngữ cảnh KN")
	if str(obj(arr(ev.body, "rows")[0], "cells"), "M") != "5" {
		t.Fatalf("tính thử: %v", ev.body)
	}
	// Mẫu đã gán theo phòng ban của ngữ cảnh.
	av := xa.must(xa.get("/v1/reports/forms/available?department="+dept+"&level=commune&report_type=weekly&at=2026-09-14"), http.StatusOK, "mẫu đã gán")
	if len(arr(av.body, "items")) != 1 {
		t.Fatalf("mẫu đã gán: %v", av.body)
	}

	// Không có ngữ cảnh phiên: kn_user một mình không đọc được (403).
	tok := f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: f.tenant, Roles: []string{accport.RoleKNUser}, TTL: time.Hour})
	bare := caller{f: f, token: tok, ctxID: uuid.Nil}
	bare.fail(bare.get(base+"/layout"), http.StatusForbidden, "forbidden", "kn_user không ngữ cảnh")

	// Ngữ cảnh phòng ban khác: không thấy mẫu của Phòng Trồng trọt.
	other := f.kn(f.tenant, authz.KNXa, "bao_ve_thuc_vat", f.p89, []string{c1})
	other.fail(other.get(base+"/layout"), http.StatusNotFound, "reports.form_not_found", "phòng ban khác")
	if n := len(arr(other.must(other.get("/v1/reports/forms/available?department="+dept+"&level=commune&report_type=weekly&at=2026-09-14"), http.StatusOK, "mẫu đã gán khác phòng ban").body, "items")); n != 0 {
		t.Fatalf("phòng ban khác thấy %d mẫu", n)
	}
}
