package pubview

import (
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

// W11.10: hợp đồng hợp lệ; "published" đã khai //ag:pubview ở đúng module chủ; "planned" chưa khai (view chưa dựng).
func TestContracts_W1110(t *testing.T) {
	cs, err := All()
	if err != nil {
		t.Fatal(err)
	}
	// W11.10: 3 hợp đồng; PFPUB: + họ closed_figures/v1 (pf_extension_pub, pf_forms_pub).
	if len(cs) != 5 {
		t.Fatalf("số hợp đồng view _pub: %d", len(cs))
	}
	reg, err := modreg.Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]string{}
	for _, m := range reg.Modules() {
		for _, p := range m.Provides.Publications {
			if _, ok := modreg.ParsePubView(p); ok {
				declared[p] = m.Name
			}
		}
	}
	for _, c := range cs {
		if p := c.Problems(); len(p) > 0 {
			t.Errorf("%s: %v", c.View, p)
		}
		owner, ok := declared[c.Directive]
		switch c.Status {
		case StatusPublished:
			if !ok || owner != c.Owner {
				t.Errorf("%s published nhưng không khai ở %s (khai ở %q)", c.View, c.Owner, owner)
			}
			if v, _ := modreg.ParsePubView(c.Directive); v.View() != c.View {
				t.Errorf("%s: modreg đặt tên view %s", c.View, v.View())
			}
		case StatusPlanned:
			if ok {
				t.Errorf("%s planned nhưng đã khai ở %s — đổi status thành published", c.View, owner)
			}
		}
	}
}

func TestProblems(t *testing.T) {
	bad := Contract{View: "pf_x_pub.v1_a", Directive: "pf_x_pub.b/v1", Owner: "x", Status: "draft", PersonalData: true,
		Columns: []Column{{Name: "v", Type: "double precision", Description: "d"}}, Key: []string{"k"}}
	if p := bad.Problems(); len(p) < 6 {
		t.Fatalf("thiếu lỗi: %v", p)
	}
}

// ADR-0053 (PFPUB): họ hợp đồng closed_figures/v1 — hai view của hai module chủ, cột giống hệt; cả hai published.
func TestFamilies_PFPUB(t *testing.T) {
	cs, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if p := FamilyProblems(cs); len(p) > 0 {
		t.Fatal(p)
	}
	var views []string
	for _, c := range cs {
		if c.Family == "closed_figures/v1" {
			if c.Status != StatusPublished || c.PersonalData {
				t.Errorf("%s: %s", c.View, c.Status)
			}
			views = append(views, c.View)
		}
	}
	if len(views) != 2 || views[0] != "pf_extension_pub.v1_closed_figures" || views[1] != "pf_forms_pub.v1_closed_figures" {
		t.Fatalf("họ closed_figures/v1: %v", views)
	}
	a := Contract{View: "pf_a_pub.v1_x", Directive: "pf_a_pub.x/v1", Owner: "pf/a", Family: "x/v1", Columns: []Column{{Name: "tenant_id", Type: "uuid"}}}
	b := Contract{View: "pf_b_pub.v1_x", Directive: "pf_b_pub.x/v1", Owner: "pf/b", Family: "x/v1", Columns: []Column{{Name: "tenant_id", Type: "text"}}}
	c := Contract{View: "pf_c_pub.v2_x", Directive: "pf_c_pub.x/v2", Owner: "pf/a", Family: "x/v1", Columns: a.Columns}
	if p := FamilyProblems([]Contract{a, b, c}); len(p) != 3 {
		t.Fatalf("thiếu lỗi họ (kiểu cột, phiên bản, chủ trùng): %v", p)
	}
}
