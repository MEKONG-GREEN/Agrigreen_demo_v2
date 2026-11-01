package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
)

type fixedProvider struct {
	selected port.KHTCTemplateSnapshot
	err      error
	calls    int
}

func (p *fixedProvider) Select(context.Context, string, time.Time) (port.KHTCTemplateSnapshot, error) {
	p.calls++
	return p.selected, p.err
}
func (p *fixedProvider) Resolve(context.Context, string, time.Time) (port.KHTCTemplateSnapshot, error) {
	panic("must not resolve a different publication")
}
func (p *fixedProvider) ByID(context.Context, uuid.UUID) (port.KHTCTemplateSnapshot, error) {
	panic("must not rebind history")
}
func fixedFixture(key string) (domain.Draft, port.KHTCTemplateSnapshot) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	definition := templatedomain.Seed(key)
	raw, hash, _ := definition.BytesHash()
	draft := domain.Refresh(domain.Draft{}, domain.Data{Template: key, Period: domain.Period{ID: uuid.New(), From: start, To: start, Type: map[string]string{"mau_1": "week", "mau_2": "month", "mau_3": "quarter", "mau_4": "year", "mau_5": "program"}[key]}, InputHash: strings.Repeat("a", 64)}, map[string]string{"signer": "Người ký"})
	pinned := port.KHTCTemplateSnapshot{Mode: "published", ID: uuid.New(), Key: key, Version: 1, Rule: templatedomain.RuleVersion, Hash: hash, Definition: raw, ApplicableFrom: start}
	return draft, pinned
}
func TestFixedAdapterSelectsOnceAndKeepsHistoricalSources(t *testing.T) {
	draft, pinned := fixedFixture("mau_1")
	provider := &fixedProvider{selected: pinned}
	got, err := selectFixedTemplate(context.Background(), provider, draft)
	if err != nil || provider.calls != 1 {
		t.Fatal("select", err, provider.calls)
	}
	got.Definition[0] = '!'
	if provider.selected.Definition[0] == '!' {
		t.Fatal("mutated provider snapshot")
	}
	provider.err = errors.New("database denied")
	if got, err = selectFixedTemplate(context.Background(), provider, draft); err == nil || got != nil {
		t.Fatal("lookup failure fell back")
	}
	sections, err := fixedTemplateSections(nil, draft)
	if err != nil || !reflect.DeepEqual(sections, domain.Sections(draft.Data.Template)) {
		t.Fatal("nil historical frame changed", err)
	}
	if document, err := renderFixedTemplate(nil, draft, "", domain.Filter{}); err != nil || document != nil {
		t.Fatal("nil history entered dynamic renderer", err)
	}
	raw, _ := json.Marshal(domain.Sections(draft.Data.Template))
	legacy, err := templatedomain.LegacySnapshot(draft.Data.Template, domain.RuleVersion, raw, WordTemplate(draft.Data.Template))
	if err != nil {
		t.Fatal(err)
	}
	sections, err = fixedTemplateSections(&legacy, draft)
	if err != nil || !reflect.DeepEqual(sections, domain.Sections(draft.Data.Template)) {
		t.Fatal("legacy frame", err)
	}
	if document, err := renderFixedTemplate(&legacy, draft, "", domain.Filter{}); err != nil || document != nil {
		t.Fatal("legacy entered dynamic renderer", err)
	}
}
func TestFixedAdapterAllFiveUsePinnedOutlineAndFrozenText(t *testing.T) {
	for _, key := range templatedomain.Keys {
		t.Run(key, func(t *testing.T) {
			draft, pinned := fixedFixture(key)
			definition := templatedomain.Seed(key)
			parent := definition.Nodes[0].ID
			definition.Nodes = append(definition.Nodes, templatedomain.Node{ID: "added", Parent: parent, Order: 100, Title: "Mục thêm", Repeat: "none", Body: "Người ký: {nguoi}", Bindings: []templatedomain.Binding{{Variable: "nguoi", Source: "admin", Field: "signer"}}, LockedAdmin: []string{"signer"}})
			raw, hash, err := definition.BytesHash()
			if err != nil {
				t.Fatal(err)
			}
			pinned.Definition, pinned.Hash = raw, hash
			sections, err := fixedTemplateSections(&pinned, draft)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, s := range sections {
				if s.Key == "added" {
					found = true
					if s.Editable {
						t.Fatal("source-only node became editable")
					}
				}
			}
			if !found {
				t.Fatal("custom outline missing")
			}
			document, err := renderFixedTemplate(&pinned, draft, "", domain.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			if document.InputHash != draft.Data.InputHash {
				t.Fatal("live source replaced frozen input")
			}
			found = false
			for _, n := range document.Nodes {
				if n.ID == "added" {
					found = true
					if len(n.Rows) != 1 || len(n.Rows[0].Tokens) != 2 || n.Rows[0].Tokens[1].Text != "Người ký" || !n.Rows[0].Tokens[1].Locked {
						t.Fatalf("token: %+v", n.Rows)
					}
				}
			}
			if !found {
				t.Fatal("custom content missing")
			}
			bad := pinned
			bad.Hash = "changed"
			if _, err := renderFixedTemplate(&bad, draft, "", domain.Filter{}); err == nil {
				t.Fatal("hash failure fell back")
			}
		})
	}
}
