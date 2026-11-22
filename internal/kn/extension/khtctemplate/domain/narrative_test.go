package domain

import (
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

func TestFreeTextSlotIsVisibleWithoutAPlaceholder(t *testing.T) {
	definition := Seed("mau_5")
	definition.Nodes = append(definition.Nodes, Node{ID: "memo", Parent: "II", Order: 100, Title: "Lời văn bổ sung", Repeat: "none", Body: "Câu cố định", TextSlot: true, Bindings: []Binding{}, LockedAdmin: []string{}})
	context := port.KHTCTemplateContext{PeriodType: "program", Values: map[string]string{}, Lists: map[string][]port.KHTCTemplateRow{}, Texts: map[string]string{"memo": "Nội dung KHTC đã biên tập", "II": "Kỳ tới"}}
	document, err := Render(definition, context, "actual")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range document.Nodes {
		if node.ID == "memo" {
			if len(node.Rows) != 2 || node.Rows[1].Tokens[0].Text != context.Texts["memo"] {
				t.Fatalf("free text silently missing: %+v", node.Rows)
			}
			return
		}
	}
	t.Fatal("memo node missing")
}
