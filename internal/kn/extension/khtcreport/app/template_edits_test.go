package app

import (
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/google/uuid"
)

func TestFixedEditsOnlyNarrativeAndUnlockedAdmin(t *testing.T) {
	draft, pinned := fixedFixture("mau_1")
	definition := templatedomain.Seed("mau_1")
	definition.Nodes = append(definition.Nodes, templatedomain.Node{ID: "memo", Parent: "B", Order: 100, Title: "Tự biên tập", Repeat: "none", Body: "Nội dung riêng", TextSlot: true, Bindings: []templatedomain.Binding{}, LockedAdmin: []string{"signer"}})
	raw, hash, err := definition.BytesHash()
	if err != nil {
		t.Fatal(err)
	}
	pinned.Definition, pinned.Hash = raw, hash
	who := uuid.New()
	now := time.Now()
	place := "Địa danh mới"
	edited, err := editFixedTemplate(&pinned, draft, domain.Edits{Admin: map[string]*string{"place": &place}, Texts: map[string]string{"memo": "  Nội dung riêng đã lưu  "}}, who, now)
	if err != nil || edited.Texts["memo"] != "Nội dung riêng đã lưu" || edited.Admin["place"].Value() != place || edited.Admin["place"].Actor == nil || *edited.Admin["place"].Actor != who {
		t.Fatal("valid edit", err, edited)
	}
	if draft.Texts["memo"] != "" || draft.Admin["place"].Value() == place {
		t.Fatal("mutated prior payload")
	}
	for name, edits := range map[string]domain.Edits{
		"locked":      {Admin: map[string]*string{"signer": &place}},
		"clearlocked": {Admin: map[string]*string{"signer": nil}},
		"source":      {Texts: map[string]string{"plan.value": "999"}},
		"overlimit":   {Texts: map[string]string{"memo": strings.Repeat("x", 12001)}},
		"nul":         {Texts: map[string]string{"memo": "x\x00"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := editFixedTemplate(&pinned, draft, edits, who, now); err == nil {
				t.Fatal("forged edit accepted")
			}
		})
	}
	document, err := renderFixedTemplate(&pinned, edited, "", domain.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range document.Nodes {
		if node.ID == "memo" {
			for _, row := range node.Rows {
				for _, token := range row.Tokens {
					if token.Text == edited.Texts["memo"] {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("saved custom narrative missing from compiled document")
	}
}
