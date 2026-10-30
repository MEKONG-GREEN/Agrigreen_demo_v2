package domain

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"testing"
)

func TestAutoBindingUsesReferencedSource(t *testing.T) {
	d := Seed("mau_5")
	d.Nodes = append(d.Nodes, Node{ID: "custom-ref", Parent: "II", Order: 10, Title: "Nguồn tham chiếu", Repeat: "none", Body: "Số nguồn: {so}", Bindings: []Binding{{Variable: "so", Source: "auto", Field: "value", Ref: "module-row"}}})
	c := port.KHTCTemplateContext{Lists: map[string][]port.KHTCTemplateRow{"metric": {{Ref: "module-row", Department: "trong_trot", Values: map[string]string{"data_source": "module", "module.value": "7", "form.value": ""}}}}}
	out, err := Render(d, c, "actual")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range out.Nodes {
		if n.ID != "custom-ref" {
			continue
		}
		for _, token := range n.Rows[0].Tokens {
			if token.Variable == "so" {
				if token.Text != "7" || token.Source != "module" || !token.Locked {
					t.Fatalf("referenced module value must retain its source and lock: %#v", token)
				}
				return
			}
		}
	}
	t.Fatal("referenced token was not rendered")
}
