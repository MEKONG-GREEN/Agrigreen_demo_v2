package app

import (
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// X-12 (W11.02): thửa có trước v7.7 (department_code NULL) đọc qua KN là Phòng Trồng trọt — không hiện ở phòng ban khác.
func TestX12_LegacyPlotReadsAsTrongTrot(t *testing.T) {
	a := access{sc: authz.Scope{Units: []string{"31117"}}}
	legacy := store.Plot{CommuneCode: "31117"}
	tagged := store.Plot{CommuneCode: "31117"}
	tagged.Department = "bvtv"
	cases := []struct {
		dept, role   string
		legacy, bvtv bool
	}{
		{"trong_trot", authz.KNXa, true, false},
		{"bvtv", authz.KNXa, false, true},
		{"", authz.KNAdmin, true, true},
	}
	for _, c := range cases {
		k := knCaller{a: a, k: authz.KNContext{Role: c.role, Department: c.dept}}
		if got := k.sees(legacy); got != c.legacy {
			t.Errorf("%s/%s thửa cũ: %v, muốn %v", c.role, c.dept, got, c.legacy)
		}
		if got := k.sees(tagged); got != c.bvtv {
			t.Errorf("%s/%s thửa bvtv: %v, muốn %v", c.role, c.dept, got, c.bvtv)
		}
	}
	if DefaultKNDepartment != store.LegacyKNDepartment {
		t.Fatal("phòng ban mặc định khi ghi phải trùng phòng ban đọc thay cho dữ liệu cũ")
	}
}
