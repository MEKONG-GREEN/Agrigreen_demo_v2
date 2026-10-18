package e2e

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
	"net/http"
	"strings"
	"testing"
)

// ORG-NEO-003: HTTP thật qua assembly, party, cooperative và kho định danh;
// đường tổ chức chung đóng, Neo tạo đủ hồ sơ, gửi lại giữ đúng Nút và neo.
func TestCreateAnchoredCooperativeORGUI(t *testing.T) {
	s := harness.New(t)
	tid := harness.NewTenant()
	root := s.User(harness.UserSpec{Tenant: tid, MFA: true, Roles: []string{"platform_admin", "masterdata_admin", "registry_admin", "vault_registrar"}})
	must := func(r harness.Resp, status int) harness.Resp {
		t.Helper()
		if r.Status != status {
			t.Fatalf("muốn %d, nhận %d %s", status, r.Status, trim(r.Raw))
		}
		return r
	}
	must(root.Do(harness.Admin, http.MethodPost, "/v1/masterdata/seed", "", map[string]any{}), 200)
	public := must(root.Do(harness.Admin, http.MethodPost, "/v1/orgs", "", map[string]any{"kind": "cooperative", "name": "HTX vòng", "admin_unit_code": communeNK}), 422)
	if public.Code() != "party.cooperative_requires_anchor" || public.Str("redirect") != "/quan-tri/neo-htx" {
		t.Fatalf("không chỉ sang Neo: %s", trim(public.Raw))
	}
	body := map[string]any{"created_by": "mkg", "mst": "1600-123-456", "agreement_ref": "TT-ORG-NEO-003", "new_cooperative": map[string]any{
		"name": "HTX Neo kiểm thử", "admin_unit_code": communeNK,
		"representative": map[string]any{"full_name": "Người Đại Diện Tổng Hợp", "phone": "0999000703"},
		"profile": map[string]any{"area_ha": "180.50", "established_on": "2019-03-01", "address": "Trụ sở tổng hợp",
			"location": map[string]any{"lat": "10.0341000", "lon": "105.7702000"}},
	}}
	key := harness.NewIdem()
	first := must(root.Do(harness.Admin, http.MethodPost, "/v1/htx-anchors", key, body), 201)
	again := root.Do(harness.Admin, http.MethodPost, "/v1/htx-anchors", key, body)
	if again.Status != 200 && again.Status != 201 {
		t.Fatalf("phát lại: %s", trim(again.Raw))
	}
	if first.Str("org_ref") == "" || first.Str("org_ref") != again.Str("org_ref") || first.Str("anchor_id") != again.Str("anchor_id") {
		t.Fatal("phát lại tạo Nút hoặc neo khác")
	}
	profile := must(root.Do(harness.App, http.MethodGet, "/v1/cooperatives/"+first.Str("org_ref"), "", nil), 200)
	if profile.Str("tax_code") != "1600123456" || profile.Str("confirmation_status") != "unconfirmed" || profile.Str("representative_ref") == "" || !dec(t, profile.Body["area_ha"]).Equal(dec(t, "180.50")) || profile.Str("established_on") != "2019-03-01" {
		t.Fatalf("hồ sơ không đầy đủ: %s", trim(profile.Raw))
	}
	all := must(root.Do(harness.App, http.MethodGet, "/v1/cooperatives", "", nil), 200)
	if len(all.Items()) != 1 {
		t.Fatalf("phải có đúng một HTX: %d", len(all.Items()))
	}
	dup := must(root.Do(harness.Admin, http.MethodPost, "/v1/htx-anchors", "", body), 409)
	if dup.Code() != "cooperative.htx_anchor_mst_taken" {
		t.Fatalf("MST trùng: %s", trim(dup.Raw))
	}
	kn := s.User(harness.UserSpec{Tenant: tid, MFA: true, Roles: []string{"kn_province_admin"}})
	body["created_by"] = "kn"
	must(kn.Do(harness.Admin, http.MethodPost, "/v1/htx-anchors", "", body), 403)
	noMFA := s.User(harness.UserSpec{Tenant: tid, Roles: []string{"platform_admin"}})
	if r := must(noMFA.Do(harness.Admin, http.MethodPost, "/v1/htx-anchors", "", body), 403); r.Code() != "mfa_required" {
		t.Fatalf("MFA: %s", trim(r.Raw))
	}
	other := s.User(harness.UserSpec{Tenant: harness.NewTenant(), MFA: true, Roles: []string{"platform_admin"}})
	must(other.Do(harness.Admin, http.MethodGet, "/v1/htx-anchors/"+first.Str("anchor_id"), "", nil), 404)
	logs := s.Logs.String()
	for _, pii := range []string{"Người Đại Diện Tổng Hợp", "0999000703", root.Token} {
		if strings.Contains(logs, pii) {
			t.Fatal("PII hoặc token bị ghi log")
		}
	}
}
