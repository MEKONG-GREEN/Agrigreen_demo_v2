package modreg

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func publicationFixture() (*Manifest, *Manifest) {
	source := &Manifest{Name: "pf/source", Path: "internal/platform/source/module.yaml", Kind: KindDomain, Enable: EnablePerTenant, Zones: []string{"pf"}, Version: "1.0.0", OwnsSchema: []string{"pf_source", "pf_source_pub"},
		Provides: Provides{Publications: []string{"pf_source_pub.alpha/v1", "pf_source_pub.beta/v2"}}}
	reader := &Manifest{Name: "pf/reader", Path: "internal/platform/reader/module.yaml", Kind: KindDomain, Enable: EnablePerTenant, Zones: []string{"pf"}, Version: "1.0.0", OwnsSchema: []string{"pf_reader"},
		Consumes: Consumes{Publications: []string{"pf_source_pub.beta/v2", "pf_source_pub.alpha/v1"}}}
	return source, reader
}
func TestPublicationReaderDeclarations(t *testing.T) {
	source, reader := publicationFixture()
	original := slices.Clone(reader.Consumes.Publications)
	if _, err := New([]*Manifest{source, reader}); err != nil {
		t.Fatal(err)
	}
	owners, err := publicationReaders([]*Manifest{source, reader})
	if err != nil || !reflect.DeepEqual(owners, map[string][]string{"pf/reader": {"pf/source"}}) {
		t.Fatalf("resolved owners: %v %v", owners, err)
	}
	if !slices.Equal(reader.Consumes.Publications, original) || len(reader.Depends) != 0 {
		t.Fatal("data read mutated input/call dependencies")
	}
	bytes, err := FormatCatalog([]*Manifest{source, reader})
	if err != nil || !strings.Contains(string(bytes), `PubReadOwners: []string{"pf/source"}`) {
		t.Fatalf("compiled reader missing: %s %v", bytes, err)
	}
	round := mustParse(t, string(Format(reader)), reader.Path)
	if !slices.Equal(round.Consumes.Publications, []string{"pf_source_pub.alpha/v1", "pf_source_pub.beta/v2"}) {
		t.Fatal("reader declaration lost in YAML round-trip")
	}
	reader.Consumes.Publications = nil
	owners, err = publicationReaders([]*Manifest{source, reader})
	if err != nil || len(owners) != 0 {
		t.Fatalf("undeclared read granted owners: %v %v", owners, err)
	}
	bytes, err = FormatCatalog([]*Manifest{source, reader})
	if err != nil || strings.Contains(string(bytes), "PubReadOwners:") {
		t.Fatal("default/empty declaration compiled reader rights")
	}
	source.Consumes.Publications = []string{"pf_source_pub.alpha/v1"}
	if _, err := New([]*Manifest{source, reader}); err != nil {
		t.Fatalf("self publication read must be declarable: %v", err)
	}
}
func TestPublicationReaderDeclarationsRejectInvalid(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Manifest, *Manifest)
	}{
		{"missing", func(_, r *Manifest) { r.Consumes.Publications = []string{"pf_missing_pub.alpha/v1"} }},
		{"wrong_version", func(_, r *Manifest) { r.Consumes.Publications = []string{"pf_source_pub.beta/v1"} }},
		{"valve", func(_, r *Manifest) { r.Consumes.Publications = []string{"event_valve"} }},
		{"duplicate_read", func(_, r *Manifest) {
			r.Consumes.Publications = append(r.Consumes.Publications, r.Consumes.Publications[0])
		}},
		{"duplicate_provider", func(s, _ *Manifest) {
			s.Provides.Publications = append(s.Provides.Publications, s.Provides.Publications[0])
		}},
		{"wrong_owner_schema", func(s, _ *Manifest) { s.OwnsSchema = []string{"pf_source"} }},
		{"no_db_role", func(_, r *Manifest) { r.OwnsSchema = nil }},
		{"other_cluster", func(_, r *Manifest) { r.Zones = []string{"mkg"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, reader := publicationFixture()
			tc.change(source, reader)
			if _, err := New([]*Manifest{source, reader}); err == nil {
				t.Fatal("invalid read contract accepted")
			}
			if _, err := FormatCatalog([]*Manifest{source, reader}); err == nil {
				t.Fatal("invalid contract compiled runtime owners")
			}
		})
	}
}
func TestPublicationReaderCatalogClone(t *testing.T) {
	entry := CatalogEntry{PubReadOwners: []string{"pf/source"}}
	clone := entry.clone()
	clone.PubReadOwners[0] = "corrupt"
	if entry.PubReadOwners[0] != "pf/source" {
		t.Fatal("reader owner slice aliases catalog entry")
	}
}
