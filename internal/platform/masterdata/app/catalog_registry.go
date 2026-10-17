package app

import (
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"sort"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/seeddata"
)

var catalogKeyPattern = regexp.MustCompile(`^[a-z][a-z_]{1,39}$`)
var catalogAttributePattern = regexp.MustCompile(`^[a-z][a-z_0-9]{0,39}$`)

// CatalogRegistry is immutable and bound to one capability instance.
type CatalogRegistry struct {
	definitions                []port.CatalogDefinition
	seeds                      []port.CatalogSeed
	invalidEffectiveDateDetail string
}

func cloneCatalogDefinition(d port.CatalogDefinition) port.CatalogDefinition {
	d.Attributes = maps.Clone(d.Attributes)
	return d
}
func cloneCatalogSeed(s port.CatalogSeed) port.CatalogSeed {
	s.Attrs = maps.Clone(s.Attrs)
	return s
}

func NewCatalogRegistry(registrations ...port.CatalogRegistration) *CatalogRegistry {
	r := &CatalogRegistry{}
	seen, seeds := map[string]bool{}, map[string]bool{}
	for _, reg := range registrations {
		if reg.InvalidEffectiveDateDetail != "" {
			if r.invalidEffectiveDateDetail != "" && r.invalidEffectiveDateDetail != reg.InvalidEffectiveDateDetail {
				panic("masterdata: conflicting catalog import detail")
			}
			r.invalidEffectiveDateDetail = reg.InvalidEffectiveDateDetail
		}
		owned := map[string]bool{}
		for _, raw := range reg.Definitions {
			d := cloneCatalogDefinition(raw)
			if !catalogKeyPattern.MatchString(d.Catalog) || d.Name == "" || seen[d.Catalog] || domain.NativeCatalog(d.Catalog) {
				panic("masterdata: invalid, duplicate or native catalog " + d.Catalog)
			}
			for name, attr := range d.Attributes {
				if !catalogAttributePattern.MatchString(name) || attr.Normalize == nil {
					panic("masterdata: invalid catalog attribute " + name)
				}
			}
			if d.EffectiveDated {
				from, err := domain.ParseDay(d.SeedValidFrom)
				if err != nil || domain.ValidateValidFrom(from) != nil {
					panic("masterdata: invalid seed date " + d.Catalog)
				}
			} else if d.SeedValidFrom != "" {
				panic("masterdata: seed date on a non-effective catalog " + d.Catalog)
			}
			seen[d.Catalog] = true
			owned[d.Catalog] = true
			r.definitions = append(r.definitions, d)
		}
		for _, raw := range reg.Seeds {
			seed := cloneCatalogSeed(raw)
			key := seed.Catalog + "/" + seed.Code
			if !owned[seed.Catalog] || seeds[key] {
				panic("masterdata: unowned or duplicate catalog seed " + key)
			}
			d, _ := r.definition(seed.Catalog)
			if _, err := (domain.Item{Catalog: seed.Catalog, Code: seed.Code, Name: seed.Name, Attrs: seed.Attrs}).NormalizeWith(d); err != nil {
				panic(fmt.Sprintf("masterdata: invalid catalog seed %s: %v", key, err))
			}
			seeds[key] = true
			r.seeds = append(r.seeds, seed)
		}
	}
	if _, err := (&Service{CatalogRegistry: r}).catalogSeeds(); err != nil {
		panic(err)
	}
	return r
}

func (r *CatalogRegistry) definition(c string) (port.CatalogDefinition, bool) {
	if r != nil {
		for _, d := range r.definitions {
			if d.Catalog == c {
				return cloneCatalogDefinition(d), true
			}
		}
	}
	return port.CatalogDefinition{}, false
}
func (r *CatalogRegistry) Seeds() []port.CatalogSeed {
	if r == nil {
		return nil
	}
	out := make([]port.CatalogSeed, len(r.seeds))
	for i, s := range r.seeds {
		out[i] = cloneCatalogSeed(s)
	}
	return out
}
func (s *Service) catalogKnown(c string) bool {
	if _, ok := s.CatalogRegistry.definition(c); ok {
		return true
	}
	return domain.KnownCatalog(c)
}
func (s *Service) catalogEffective(c string) bool {
	if d, ok := s.CatalogRegistry.definition(c); ok {
		return d.EffectiveDated
	}
	return false
}
func (s *Service) normalizeCatalogItem(it domain.Item) (domain.Item, error) {
	if d, ok := s.CatalogRegistry.definition(it.Catalog); ok {
		return it.NormalizeWith(d)
	}
	return it.Normalize()
}

// NormalizeCatalogItem is a pure schema operation, without access to user authority or storage.
func (s *Service) NormalizeCatalogItem(in port.CatalogItem) (port.CatalogItem, error) {
	it, err := s.normalizeCatalogItem(domain.Item{Catalog: in.Catalog, Code: in.Code, Name: in.Name, Attrs: in.Attrs, Active: in.Active})
	return toPortItem(it), err
}

// CatalogMetadata returns fresh maps/slices without exposing validators.
func (s *Service) CatalogMetadata() []port.CatalogMetadata {
	keys := slices.Clone(domain.Catalogs)
	if s.CatalogRegistry != nil {
		for _, d := range s.CatalogRegistry.definitions {
			if !slices.Contains(keys, d.Catalog) {
				keys = append(keys, d.Catalog)
			}
		}
	}
	out := make([]port.CatalogMetadata, 0, len(keys))
	for _, key := range keys {
		m := port.CatalogMetadata{Catalog: key, Name: domain.CatalogName(key), Attributes: domain.CatalogAttrs(key), EffectiveDated: false, Sync: true}
		if d, ok := s.CatalogRegistry.definition(key); ok {
			m.Name = d.Name
			m.Attributes = make([]string, 0, len(d.Attributes))
			for name := range d.Attributes {
				m.Attributes = append(m.Attributes, name)
			}
			sort.Strings(m.Attributes)
			m.EffectiveDated = d.EffectiveDated
			m.Sync = d.Sync
			m.SeedValidFrom = d.SeedValidFrom
		}
		out = append(out, m)
	}
	return out
}
func (s *Service) catalogSyncKeys() []string {
	var out []string
	for _, m := range s.CatalogMetadata() {
		if m.Sync {
			out = append(out, m.Catalog)
		}
	}
	return out
}
func (s *Service) catalogSeedFrom(c string) string {
	if d, ok := s.CatalogRegistry.definition(c); ok {
		return d.SeedValidFrom
	}
	return "2000-01-01"
}
func (s *Service) catalogSeeds() ([]port.CatalogSeed, error) {
	var cf catalogFile
	if err := decodeStrict(seeddata.Catalogs, &cf); err != nil {
		return nil, fmt.Errorf("masterdata: dữ liệu nhúng catalogs lỗi: %w", err)
	}
	if cf.Format != domain.CatalogFormat {
		return nil, fmt.Errorf("masterdata: dữ liệu nhúng sai định dạng (%q)", cf.Format)
	}
	out := cf.Items
	for _, seed := range s.CatalogRegistry.Seeds() {
		i := slices.IndexFunc(out, func(old port.CatalogSeed) bool { return old.Catalog == seed.Catalog && old.Code == seed.Code })
		if i < 0 {
			out = append(out, seed)
		} else if !reflect.DeepEqual(out[i], seed) {
			return nil, fmt.Errorf("masterdata: conflicting catalog seed %s/%s", seed.Catalog, seed.Code)
		}
	}
	return out, nil
}
func (s *Service) invalidCatalogEffectiveDate() string {
	if s.CatalogRegistry != nil && s.CatalogRegistry.invalidEffectiveDateDetail != "" {
		return s.CatalogRegistry.invalidEffectiveDateDetail
	}
	return "valid_from: YYYY-MM-DD, chỉ danh mục có hiệu lực theo ngày"
}

// CatalogEffectiveDated selects the date-aware HTTP path from the same instance registry.
func (s *Service) CatalogEffectiveDated(c string) bool { return s.catalogEffective(c) }
