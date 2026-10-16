package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"slices"
	"strings"
)

// CatalogRegistry is immutable after startup and private to one application.
type CatalogRegistry struct {
	sources   map[string]domain.SourceSpec
	templates map[string]domain.Template
}

func copyCatalogTemplate(t domain.Template) domain.Template {
	t.Def.Columns = slices.Clone(t.Def.Columns)
	t.Def.Formats = slices.Clone(t.Def.Formats)
	return t
}
func NewCatalogRegistry(registrations ...port.CatalogRegistration) *CatalogRegistry {
	r := &CatalogRegistry{sources: domain.NativeSources(), templates: map[string]domain.Template{}}
	for _, t := range domain.NativeBuiltins() {
		r.templates[t.Key] = copyCatalogTemplate(t)
	}
	for _, registration := range registrations {
		for key, src := range registration.Sources {
			if _, exists := r.sources[key]; exists {
				panic("reports: duplicate source registration: " + key)
			}
			if key == "" || src.Key != key || strings.TrimSpace(src.Module) == "" || !slices.Contains(src.Levels, src.DefaultLevel) || len(src.Columns) == 0 {
				panic("reports: invalid source registration: " + key)
			}
			spec := domain.SourceSpec{Key: key, Module: src.Module, Levels: slices.Clone(src.Levels), DefaultLevel: src.DefaultLevel, NeedsPeriod: src.NeedsPeriod, UsesSeason: src.UsesSeason, NeedsCompare: src.NeedsCompare}
			seen := map[string]bool{}
			for _, c := range src.Columns {
				if c.Key == "" || seen[c.Key] || !slices.Contains([]string{"text", "int", "decimal", "percent"}, c.Kind) {
					panic("reports: invalid source column registration: " + key)
				}
				seen[c.Key] = true
				spec.Columns = append(spec.Columns, domain.ColumnSpec{Key: c.Key, Title: c.Title, Kind: domain.Kind(c.Kind), Scale: c.Scale, MinLevel: c.MinLevel, MaxLevel: c.MaxLevel})
			}
			r.sources[key] = spec
		}
	}
	for _, registration := range registrations {
		for _, t := range registration.Templates {
			if _, exists := r.templates[t.Key]; exists || !domain.ValidKey(t.Key) {
				panic("reports: duplicate or invalid template registration: " + t.Key)
			}
			def := domain.Definition{Title: t.Def.Title, Source: t.Def.Source, Grouping: t.Def.Grouping, Formats: slices.Clone(t.Def.Formats), Provisional: t.Def.Provisional, FormNote: t.Def.FormNote}
			for _, c := range t.Def.Columns {
				def.Columns = append(def.Columns, domain.Column{Key: c.Key, Title: c.Title})
			}
			if err := def.ValidateWithSources(r.sources); err != nil {
				panic("reports: invalid template registration: " + t.Key + ": " + err.Error())
			}
			r.templates[t.Key] = domain.Template{Key: t.Key, Version: 0, Status: domain.TemplateActive, Def: def}
		}
	}
	return r
}
func (r *CatalogRegistry) Sources() map[string]domain.SourceSpec {
	return domain.CopySources(r.sources)
}
func (r *CatalogRegistry) Templates() []domain.Template {
	out := make([]domain.Template, 0, len(r.templates))
	for _, t := range r.templates {
		out = append(out, copyCatalogTemplate(t))
	}
	return out
}
func (s *Service) sourceSpecs() map[string]domain.SourceSpec {
	if s.Catalog == nil {
		return domain.Sources
	}
	return s.Catalog.Sources()
}
func (s *Service) sourceSpec(key string) domain.SourceSpec { return s.sourceSpecs()[key] }
func (s *Service) builtinTemplates() []domain.Template {
	if s.Catalog == nil {
		return domain.Builtins()
	}
	return s.Catalog.Templates()
}
func (s *Service) builtinTemplate(key string) (domain.Template, bool) {
	for _, t := range s.builtinTemplates() {
		if t.Key == key {
			return t, true
		}
	}
	return domain.Template{}, false
}
