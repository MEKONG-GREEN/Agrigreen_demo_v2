package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/google/uuid"
)

func TestFixedWordContainerAllFiveAndImmutableInput(t *testing.T) {
	renderer := FixedWordTemplate()
	if renderer.Key != "khtc.fixed" || renderer.Version != 1 {
		t.Fatal("generic identity")
	}
	if err := doctpl.Validate(renderer); err != nil {
		t.Fatal(err)
	}
	for _, key := range templatedomain.Keys {
		t.Run(key, func(t *testing.T) {
			draft, source := fixedFixture(key)
			draft.Data.Period.Name = "Kỳ kiểm chứng"
			draft.Admin["place"] = domain.AdminField{System: "An Giang"}
			draft.Admin["report_date"] = domain.AdminField{System: "2026-10-07"}
			draft.Admin["recipient"] = domain.AdminField{System: "Sở Nông nghiệp và Môi trường"}
			draft.Admin["signer"] = domain.AdminField{System: "Người ký đã đóng băng"}
			planned, percent := "10", "70"
			draft.Data.Rows = []domain.ProgramRow{{Ref: uuid.New(), Kind: "item", Name: "Mô hình đã chụp", InPeriod: "7", YearToDate: "7", Planned: &planned, Percent: &percent, Unit: "ha", Department: "trong_trot", Content: "=1+1 giữ nguyên văn"}}
			definition := templatedomain.Seed(key)
			definition.Nodes = append(definition.Nodes, templatedomain.Node{ID: "memo", Parent: definition.Nodes[0].ID, Order: 100, Title: "Mục Admin bổ sung", Repeat: "none", Body: "Lời văn: {van}", TextSlot: true, Bindings: []templatedomain.Binding{{Variable: "van", Source: "text", Field: "narrative"}}})
			raw, hash, err := definition.BytesHash()
			if err != nil {
				t.Fatal(err)
			}
			source.Definition, source.Hash = raw, hash
			draft.Texts["memo"] = "Giá trị {không_thực_thi}"
			provider := &fixedProvider{selected: source}
			pinned, err := selectFixedTemplate(context.Background(), provider, draft)
			if err != nil {
				t.Fatal(err)
			}
			keyOut, version, data, err := fixedTemplateWord(pinned, draft, "", domain.Filter{}, 4)
			if err != nil {
				t.Fatal(err)
			}
			if keyOut != renderer.Key || version != renderer.Version || pinned.WordHash != renderer.Hash() {
				t.Fatal("renderer not pinned")
			}
			body, err := doctpl.RenderDOCX(renderer, data)
			if err != nil {
				t.Fatal(err)
			}
			extracted, err := docx.Extract(body)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"TRUNG TÂM KHUYẾN NÔNG", "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", "Mục Admin bổ sung", "Lời văn: Giá trị {không_thực_thi}", "Người ký đã đóng băng", source.Hash} {
				if !strings.Contains(extracted.Text(), want) {
					t.Fatalf("missing %q", want)
				}
			}
			for _, node := range definition.Nodes {
				if templatedomain.Protected(key, node.ID) && !strings.Contains(extracted.Text(), node.Title) {
					t.Fatalf("missing protected heading %s", node.ID)
				}
			}
			if key == "mau_3" || key == "mau_4" || key == "mau_5" {
				found := false
				for _, table := range extracted.Tables {
					if len(table) > 0 && len(table[0]) == 5 && table[0][0] == "TT" && table[0][4] == "Ghi chú" {
						found = true
					}
				}
				if !found {
					t.Fatal("native five-column appendix lost")
				}
			}
			// A later publication never participates when rendering this revision again.
			provider.selected.Version = 9
			provider.selected.Hash = "later"
			provider.selected.Definition = nil
			_, _, again, err := fixedTemplateWord(pinned, draft, "", domain.Filter{}, 4)
			if err != nil {
				t.Fatal(err)
			}
			after, err := doctpl.RenderDOCX(renderer, again)
			if err != nil || !bytes.Equal(body, after) {
				t.Fatal("publication rebound pinned Word", err)
			}
			bad := *pinned
			bad.WordHash = "tampered"
			if _, _, _, err := fixedTemplateWord(&bad, draft, "", domain.Filter{}, 4); err == nil {
				t.Fatal("renderer error fell back")
			}
		})
	}
}

func TestFixedWordLegacyPreservesSource(t *testing.T) {
	draft, _ := fixedFixture("mau_1")
	draft.Admin["place"] = domain.AdminField{System: "An Giang"}
	draft.Admin["report_date"] = domain.AdminField{System: "2026-10-07"}
	raw, _ := json.Marshal(domain.Sections(draft.Data.Template))
	legacy, err := templatedomain.LegacySnapshot(draft.Data.Template, domain.RuleVersion, raw, WordTemplate(draft.Data.Template))
	if err != nil {
		t.Fatal(err)
	}
	_, _, before, err := fixedTemplateWord(nil, draft, "", domain.Filter{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	key, version, after, err := fixedTemplateWord(&legacy, draft, "", domain.Filter{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if key != legacy.WordKey || version != legacy.WordVersion {
		t.Fatal("legacy renderer identity changed")
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if !bytes.Equal(a, b) {
		t.Fatal("legacy source data changed")
	}
	old, err := doctpl.RenderDOCX(WordTemplate(draft.Data.Template), before)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := doctpl.RenderDOCX(WordTemplate(draft.Data.Template), after)
	if err != nil || !bytes.Equal(old, pinned) {
		t.Fatal("legacy bytes changed", err)
	}
}

func TestFixedWordLongSubstitutionsRemainComplete(t *testing.T) {
	draft, source := fixedFixture("mau_5")
	draft.Admin["place"] = domain.AdminField{System: "An Giang"}
	draft.Admin["report_date"] = domain.AdminField{System: "2026-10-07"}
	definition := templatedomain.Seed("mau_5")
	for i, node := range definition.Nodes {
		if node.ID == "II" {
			definition.Nodes[i].Body = "{loi_van}{loi_van}"
		}
	}
	raw, hash, err := definition.BytesHash()
	if err != nil {
		t.Fatal(err)
	}
	source.Definition, source.Hash = raw, hash
	draft.Texts["II"] = strings.Repeat("x", 12000)
	pinned, err := selectFixedTemplate(context.Background(), &fixedProvider{selected: source}, draft)
	if err != nil {
		t.Fatal(err)
	}
	_, _, data, err := fixedTemplateWord(pinned, draft, "", domain.Filter{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	body, err := doctpl.RenderDOCX(FixedWordTemplate(), data)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := docx.Extract(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(extracted.Text(), strings.Repeat("x", 24000)) {
		t.Fatal("bounded substitution was truncated or split")
	}
}
