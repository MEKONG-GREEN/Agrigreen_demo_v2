package app

import (
	"slices"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/form"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// FormSeedRegistry freezes application-owned seed definitions at startup.
type FormSeedRegistry struct {
	sets       map[string]port.FormSeedRegistration
	defaultKey string
}

func copyFormSeedSet(in port.FormSeedRegistration) port.FormSeedRegistration {
	out := in
	out.Templates = slices.Clone(in.Templates)
	for i := range out.Templates {
		out.Templates[i].Def = form.Clone(in.Templates[i].Def)
	}
	return out
}

func NewFormSeedRegistry(registrations ...port.FormSeedRegistration) *FormSeedRegistry {
	r := &FormSeedRegistry{sets: map[string]port.FormSeedRegistration{}}
	for _, registration := range registrations {
		if strings.TrimSpace(registration.Key) == "" || !deptRe(registration.DefaultDepartment) || strings.TrimSpace(registration.Version) == "" || len(registration.Templates) == 0 {
			panic("reports: invalid form seed registration")
		}
		if _, exists := r.sets[registration.Key]; exists {
			panic("reports: duplicate form seed registration: " + registration.Key)
		}
		if registration.Default {
			if r.defaultKey != "" {
				panic("reports: ambiguous default form seed registration")
			}
			r.defaultKey = registration.Key
		}
		seen := map[string]bool{}
		for _, template := range registration.Templates {
			if template.Code == "" || seen[template.Code] || strings.TrimSpace(template.Name) == "" || template.Code != template.Def.Code ||
				!slices.Contains([]string{port.LevelCommune, port.LevelProvince}, template.EntryLevel) || !slices.Contains(port.ReportTypes, template.ReportType) {
				panic("reports: invalid form seed template: " + template.Code)
			}
			if err := form.Validate(template.Def); err != nil {
				panic("reports: invalid form seed definition: " + template.Code + ": " + err.Error())
			}
			seen[template.Code] = true
		}
		r.sets[registration.Key] = copyFormSeedSet(registration)
	}
	return r
}

func (r *FormSeedRegistry) defaultSet() (port.FormSeedRegistration, bool) {
	if r == nil || r.defaultKey == "" {
		return port.FormSeedRegistration{}, false
	}
	set, ok := r.sets[r.defaultKey]
	return copyFormSeedSet(set), ok
}

func (s *Service) formSeedSet() (port.FormSeedRegistration, error) {
	if set, ok := s.FormSeeds.defaultSet(); ok {
		return set, nil
	}
	return port.FormSeedRegistration{}, ErrDependency.WithDetail("chưa đăng ký bộ mẫu khởi tạo")
}
