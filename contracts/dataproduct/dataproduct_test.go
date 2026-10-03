package dataproduct

import (
	"errors"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
)

// G5-0: ba sản phẩm đầu tiên nạp được, đều là dự thảo — phục vụ ngoài production, KHÔNG ở production.
func TestEmbeddedProductsLoad_G5(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"collection_volume", "hub_capacity", "mrv_package_status"}
	if len(all) != len(want) {
		t.Fatalf("muốn %d sản phẩm, có %d", len(want), len(all))
	}
	for i, p := range all {
		if p.Name != want[i] {
			t.Fatalf("sản phẩm %d: muốn %s, có %s", i, want[i], p.Name)
		}
		if p.Status != StatusDraft || !p.Servable(false) || p.Servable(true) {
			t.Fatalf("%s: dự thảo phải phục vụ ngoài production và không phục vụ ở production", p.Name)
		}
		if p.ContractHeader() != p.Name+".v1" {
			t.Fatalf("%s: AG-Data-Contract sai: %s", p.Name, p.ContractHeader())
		}
		if !p.HasScope("dp:" + p.Name + ":read") {
			t.Fatalf("%s: thiếu scope read", p.Name)
		}
	}
}

// ADR-0022 mục 9: phần tử có khóa ngoài hợp đồng (vd mã nông hộ) bị từ chối — mặc định từ chối.
func TestValidateDefaultDeny_G5(t *testing.T) {
	p, err := Get("collection_volume")
	if err != nil {
		t.Fatal(err)
	}
	ok := `{"province_code":"86","commune_code":"29500","season_code":"2026-HT","period_month":"2026-09",
		"rolls_collected":1200,"area_ha_collected":"35.50","k_applied":10,"as_of":"2026-10-01T00:00:00Z"}`
	if err := p.Validate([]byte(ok)); err != nil {
		t.Fatalf("phần tử hợp lệ bị từ chối: %v", err)
	}
	bad := strings.Replace(ok, `"k_applied":10`, `"k_applied":10,"household_ref":"019a0000-0000-7000-8000-000000000001"`, 1)
	if err := p.Validate([]byte(bad)); !errors.Is(err, publication.ErrDenied) {
		t.Fatalf("khóa ngoài hợp đồng phải bị từ chối: %v", err)
	}
	if _, err := Get("household_list"); !errors.Is(err, publication.ErrDenied) {
		t.Fatalf("sản phẩm không có hợp đồng phải bị từ chối: %v", err)
	}
}

// Siêu dữ liệu: dữ liệu cá nhân, tự phê duyệt, loại cấp quyền / scope sai, nguồn vùng mkg đều bị bắt.
func TestMetaProblems_G5(t *testing.T) {
	base := `{"name":"x_prod","version":1,"owner":"pf/apiclient","sources":["pf/geo"],"data_owner":"bên cấp",
	"grant_kind":"dp_x_prod","scopes":["dp:x_prod:read"],"route":"GET /v1/data/x","status":"draft",
	"proposed_by":"a","approved_by":"b","approved_at":"2026-10-01","purpose":"p","personal_data":false,
	"type":"object","properties":{"a":{"type":"string"}}}`
	p, err := Parse([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	if mp := p.MetaProblems(); len(mp) != 0 {
		t.Fatalf("hợp đồng mẫu hợp lệ có vấn đề: %v", mp)
	}
	for _, c := range []struct{ from, to, want string }{
		{`"personal_data":false`, `"personal_data":true`, "personal_data"},
		{`"approved_by":"b"`, `"approved_by":"A"`, "approved_by phải khác proposed_by"},
		{`"grant_kind":"dp_x_prod"`, `"grant_kind":"plot_directory"`, "grant_kind"},
		{`"scopes":["dp:x_prod:read"]`, `"scopes":["dp:other:read"]`, "scope"},
		{`"sources":["pf/geo"]`, `"sources":["mkg/hub"]`, "R-03"},
		{`"owner":"pf/apiclient"`, `"owner":"mkg/hub"`, "owner"},
		{`"status":"draft"`, `"status":"live"`, "status"},
		{`"properties":{"a":{"type":"string"}}`, `"properties":{"a":{}}`, "thiếu type"},
	} {
		q, err := Parse([]byte(strings.Replace(base, c.from, c.to, 1)))
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(q.MetaProblems(), "\n"); !strings.Contains(got, c.want) {
			t.Errorf("%s → %s: muốn %q, có %q", c.from, c.to, c.want, got)
		}
	}
	if _, err := Parse([]byte(strings.Replace(base, `"purpose":"p"`, `"purpose":"p","unknown":1`, 1))); err == nil {
		t.Fatal("khóa siêu dữ liệu lạ phải bị từ chối khi đọc")
	}
}
