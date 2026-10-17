package modreg

import (
	"fmt"
	"slices"
)

// publicationReaders resolves declared contracts, never module-specific defaults.
// Multiple contracts from one publisher require just one publication role.
func publicationReaders(mods []*Manifest) (map[string][]string, error) {
	providers := map[string]string{}
	for _, m := range mods {
		for _, publication := range m.Provides.Publications {
			view, ok := ParsePubView(publication)
			if !ok {
				continue
			} // Event valves are not SQL views.
			if !slices.Contains(m.OwnsSchema, view.Schema) {
				return nil, fmt.Errorf("%s: publication %q schema is not owned", m.Path, publication)
			}
			if previous, ok := providers[publication]; ok {
				return nil, fmt.Errorf("publication %q has duplicate providers %s/%s", publication, previous, m.Name)
			}
			providers[publication] = m.Name
		}
	}
	out := map[string][]string{}
	for _, m := range mods {
		seen := map[string]bool{}
		for _, publication := range m.Consumes.Publications {
			view, ok := ParsePubView(publication)
			if !ok {
				return nil, fmt.Errorf("%s: consumes.publications %q must be a versioned _pub view", m.Path, publication)
			}
			if seen[publication] {
				return nil, fmt.Errorf("%s: duplicate consumed publication %q", m.Path, publication)
			}
			seen[publication] = true
			if len(m.OwnsSchema) == 0 {
				return nil, fmt.Errorf("%s: publication reader needs a DB module schema", m.Path)
			}
			if !slices.Contains(m.Zones, schemaZone(view.Schema)) {
				return nil, fmt.Errorf("%s: publication %q is in another cluster", m.Path, publication)
			}
			owner, ok := providers[publication]
			if !ok {
				return nil, fmt.Errorf("%s: consumed publication %q is not provided", m.Path, publication)
			}
			if !slices.Contains(out[m.Name], owner) {
				out[m.Name] = append(out[m.Name], owner)
			}
		}
		slices.Sort(out[m.Name])
	}
	return out, nil
}
