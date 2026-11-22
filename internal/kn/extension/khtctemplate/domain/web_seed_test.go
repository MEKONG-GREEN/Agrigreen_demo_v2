package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The mock's five definitions are generated from the same official seed, not a
// second handwritten interpretation of the report layouts.
func TestW1165WebSeeds(t *testing.T) {
	items := []any{}
	for _, key := range Keys {
		definition := Seed(key)
		_, hash, err := definition.BytesHash()
		if err != nil {
			t.Fatal(err)
		}
		seeded, protected := []string{}, []string{}
		for _, node := range definition.Nodes {
			seeded = append(seeded, node.ID)
			if Protected(key, node.ID) {
				protected = append(protected, node.ID)
			}
		}
		items = append(items, map[string]any{"key": key, "definition": definition, "hash": hash, "version": 0, "version_id": nil, "history": []any{}, "publications": []any{}, "editable": true, "protected_ids": protected, "seeded_ids": seeded})
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join("..", "..", "..", "..", "..", "apps", "web-ops", "src", "api", "mock", "fixtures", "khtc-template-seeds.json")
	if os.Getenv("AG_UPDATE_WEB_FIXTURES") == "1" {
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(raw) {
		t.Fatal("web template seeds differ; regenerate deliberately with TestW1165WebSeeds")
	}
}
