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
	if len(cs) != 3 {
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
