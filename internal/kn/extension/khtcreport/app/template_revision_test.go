package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
)

func TestFixedRevisionKeepsDefinitionAndCapturesTerminalSeparately(t *testing.T) {
	content, source := fixedFixture("mau_1")
	svc := Service{Templates: &fixedProvider{selected: source}}
	if err := svc.pinNewTemplate(context.Background(), &content, ""); err != nil {
		t.Fatal(err)
	}
	saved, _ := json.Marshal(content)
	copied := domain.Refresh(content, content.Data, nil)
	if err := captureTemplateTerminal(&copied, "closed"); err != nil {
		t.Fatal(err)
	}
	if content.TemplateTerminal == nil || *content.TemplateTerminal != "" || *copied.TemplateTerminal != "closed" {
		t.Fatal("terminal shared across revisions")
	}
	if copied.TemplateSnapshot.Hash != source.Hash {
		t.Fatal("refresh rebound template")
	}
	copied.TemplateSnapshot.Definition[0] = '!'
	after, _ := json.Marshal(content)
	if string(saved) != string(after) {
		t.Fatal("refresh mutated historical source")
	}
	historical := domain.Draft{Data: content.Data}
	raw, _ := json.Marshal(historical)
	if strings.Contains(string(raw), "template_snapshot") || strings.Contains(string(raw), "template_terminal") {
		t.Fatal("nil history JSON changed")
	}
	if err := captureTemplateTerminal(&historical, "closed"); err != nil || historical.TemplateTerminal != nil {
		t.Fatal("nil history migrated implicitly", err)
	}
}
func TestFixedDetailUsesEmbeddedOutlineAndAdminLocks(t *testing.T) {
	content, source := fixedFixture("mau_1")
	definition := templatedomain.Seed("mau_1")
	definition.Nodes = append(definition.Nodes, templatedomain.Node{ID: "extra", Parent: "B", Order: 100, Title: "Ghi chú riêng", Repeat: "none", TextSlot: true, Body: "{van}", Bindings: []templatedomain.Binding{{Variable: "van", Source: "text", Field: "narrative"}}, LockedAdmin: []string{"signer"}})
	source.Definition, source.Hash, _ = definition.BytesHash()
	svc := Service{Templates: &fixedProvider{selected: source}}
	if err := svc.pinNewTemplate(context.Background(), &content, ""); err != nil {
		t.Fatal(err)
	}
	content.Texts["extra"] = "Nội dung riêng"
	out := Detail{Content: content}
	if err := decorateFixedDetail(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.LockedAdmin) != 1 || out.LockedAdmin[0] != "signer" || out.Document == nil {
		t.Fatal("compiled permissions missing")
	}
	found := false
	for _, s := range out.Sections {
		if s.Key == "extra" && s.Editable {
			found = true
		}
	}
	if !found {
		t.Fatal("custom slot missing")
	}
	out.Content.TemplateSnapshot.Hash = "bad"
	if err := decorateFixedDetail(&out); err == nil {
		t.Fatal("broken source fell back to legacy")
	}
}
