package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

func TestFiveOfficialSeeds(t *testing.T) {
	for _, key := range Keys {
		t.Run(key, func(t *testing.T) {
			d := Seed(key)
			if err := Validate(d, nil); err != nil {
				t.Fatal(err)
			}
			raw, hash, err := d.BytesHash()
			if err != nil || len(hash) != 64 || len(raw) == 0 {
				t.Fatalf("hash: %s %v", hash, err)
			}
			if _, err = Render(d, port.KHTCTemplateContext{}, "actual"); err != nil {
				t.Fatal(err)
			}
		})
	}
	if Fixed("adhoc") || Fixed("mau_6") {
		t.Fatal("a sixth report must not be authorable")
	}
}
func TestOfficialHeadingsParentAndVariablesCannotBeForged(t *testing.T) {
	tests := map[string]func(*Definition){
		"protected title":       func(d *Definition) { d.Nodes[0].Title = "Đổi tiêu đề chính" },
		"protected order":       func(d *Definition) { d.Nodes[0].Order = 999 },
		"delete official child": func(d *Definition) { d.Nodes = d.Nodes[1:] },
		"cycle":                 func(d *Definition) { d.Nodes[2].Parent = d.Nodes[2].ID },
		"unbound":               func(d *Definition) { d.Nodes[2].Body = "{bi_mat}" },
		"invalid source":        func(d *Definition) { d.Nodes[2].Bindings[0].Source = "unknown_source" },
		"source field":          func(d *Definition) { d.Nodes[2].Bindings[0].Field = "household_phone" },
		"missing body":          func(d *Definition) { d.Nodes[2].Body = "" },
		"huge order":            func(d *Definition) { d.Nodes[2].Order = 10001 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			d := Seed("mau_1")
			mutate(&d)
			if Validate(d, nil) == nil {
				t.Fatal("invalid definition accepted")
			}
		})
	}
	d := Seed("mau_1")
	custom := Node{ID: "custom_child", Parent: "A.I.1", Order: 10, Title: "Mục con", Repeat: "none", Body: "Nội dung"}
	d.Nodes = append(d.Nodes, custom)
	if err := Validate(d, nil); err != nil {
		t.Fatal(err)
	}
	previous := Canonical(d)
	d.Nodes[len(d.Nodes)-1].Parent = "A.I.2"
	if Validate(d, &previous) == nil {
		t.Fatal("moving an existing item between parents was accepted")
	}
}

func planRow(i int, status string) port.KHTCTemplateRow {
	return port.KHTCTemplateRow{Ref: fmt.Sprint(i), ProgramRef: "program-1", Department: "trong_trot", Status: status, Values: map[string]string{
		"tab": "programs", "data_source": "form", "plan.program_name": "I. Chương trình lúa", "plan.item_name": fmt.Sprintf("Hạng mục %d", i), "plan.unit": "ha", "form.value": "12.5", "form.content": "Tập huấn {không_thực_thi}", "calculated.year_to_date": "50"}}
}
func TestAnnualPlanLoopsAndSourceLocks(t *testing.T) {
	for _, count := range []int{12, 13} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			c := port.KHTCTemplateContext{PeriodType: "week", Lists: map[string][]port.KHTCTemplateRow{"plan_item": {}}, Texts: map[string]string{"B": "Lời văn riêng"}}
			for i := 1; i <= count; i++ {
				c.Lists["plan_item"] = append(c.Lists["plan_item"], planRow(i, "normal"))
			}
			out, err := Render(Seed("mau_1"), c, "actual")
			if err != nil {
				t.Fatal(err)
			}
			seen, preambles := 0, 0
			for _, n := range out.Nodes {
				if n.ID != "A.II.2" {
					continue
				}
				for _, row := range n.Rows {
					seen++
					if row.Preamble != "" {
						preambles++
					}
					for _, token := range row.Tokens {
						if token.Variable != "" && !token.Locked {
							t.Fatalf("source token is editable: %#v", token)
						}
					}
				}
			}
			if seen != count || preambles != 1 {
				t.Fatalf("rows=%d preambles=%d", seen, preambles)
			}
		})
	}
}
func TestSourceStatusPriorityAndPreviewScenarios(t *testing.T) {
	for _, test := range []struct{ status, want string }{{"no_send", "chưa gửi số liệu."}, {"unclosed", "chưa chốt số liệu."}, {"no_activity", "Trong tuần không tổ chức."}} {
		t.Run(test.status, func(t *testing.T) {
			c := port.KHTCTemplateContext{PeriodType: "week", Lists: map[string][]port.KHTCTemplateRow{"plan_item": {planRow(1, test.status)}}}
			out, err := Render(Seed("mau_1"), c, "actual")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, n := range out.Nodes {
				if n.ID == "A.II.2" {
					found = true
					if len(n.Rows) != 1 || len(n.Rows[0].Tokens) != 1 || !n.Rows[0].Tokens[0].Locked || !strings.Contains(n.Rows[0].Tokens[0].Text, test.want) {
						t.Fatalf("unexpected status output: %#v", n)
					}
				}
			}
			if !found {
				t.Fatal("section missing")
			}
		})
	}
}
func TestAdministrativeLocksAndEscapedTemplateText(t *testing.T) {
	d := Seed("mau_1")
	d.Nodes = append(d.Nodes, Node{ID: "custom", Parent: "A.I.1", Order: 10, Title: "Hành chính", Repeat: "none", Body: "{{Số}} {so}", Bindings: []Binding{{Variable: "so", Source: "admin", Field: "document_no"}}, LockedAdmin: []string{"document_no"}})
	before, _, _ := d.BytesHash()
	out, err := Render(d, port.KHTCTemplateContext{Values: map[string]string{"admin.document_no": "12/BC"}}, "actual")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range out.Nodes {
		if n.ID == "custom" {
			last := n.Rows[0].Tokens[len(n.Rows[0].Tokens)-1]
			if !last.Locked || last.Text != "12/BC" {
				t.Fatalf("lock: %#v", last)
			}
		}
	}
	after, _, _ := d.BytesHash()
	if string(before) != string(after) {
		t.Fatal("renderer mutated the definition")
	}
}
func TestHostileNumericAndRenderLimits(t *testing.T) {
	for _, value := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000)} {
		var n Node
		if json.Unmarshal([]byte(`{"order":`+value+`}`), &n) == nil {
			t.Fatal("hostile integer accepted")
		}
	}
	c := port.KHTCTemplateContext{Lists: map[string][]port.KHTCTemplateRow{"plan_item": make([]port.KHTCTemplateRow, MaxRows+1)}}
	if _, err := Render(Seed("mau_1"), c, "actual"); err == nil {
		t.Fatal("unbounded loop accepted")
	}
	c = port.KHTCTemplateContext{Values: map[string]string{"admin.document_no": strings.Repeat("9", 100000)}}
	if _, err := Render(Seed("mau_1"), c, "actual"); err == nil {
		t.Fatal("unbounded source value accepted")
	}
}
