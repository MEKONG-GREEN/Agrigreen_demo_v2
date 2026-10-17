package app

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/seeddata"
)

// ParamRegistry is immutable and local to one capability instance.
type ParamRegistry struct {
	specs []port.ParamSpec
	seeds []port.ParamSeed
}

func cloneSeed(s port.ParamSeed) port.ParamSeed {
	if s.Value != nil {
		v := *s.Value
		s.Value = &v
	}
	if s.Text != nil {
		v := *s.Text
		s.Text = &v
	}
	return s
}

// NewParamRegistry rejects conflicting owners and invalid initial values at startup.
func NewParamRegistry(registrations ...port.ParamRegistration) *ParamRegistry {
	r := &ParamRegistry{}
	keys, seeds := map[string]bool{}, map[string]bool{}
	for _, reg := range registrations {
		for _, sp := range reg.Specs {
			if domain.ValidateParamKey(sp.Key) != nil || strings.Contains(sp.Key, ":") || strings.TrimSpace(sp.Name) == "" || keys[sp.Key] {
				panic("masterdata: invalid or duplicate parameter spec " + sp.Key)
			}
			switch sp.Kind {
			case port.ParamKindDecimal, port.ParamKindText, port.ParamKindColor:
			default:
				panic("masterdata: invalid parameter kind " + sp.Key)
			}
			var lo, hi decimal.Decimal
			var err error
			if sp.Min != "" {
				lo, err = decimal.NewFromString(sp.Min)
				if err != nil {
					panic("masterdata: invalid min " + sp.Key)
				}
			}
			if sp.Max != "" {
				hi, err = decimal.NewFromString(sp.Max)
				if err != nil {
					panic("masterdata: invalid max " + sp.Key)
				}
			}
			if sp.Min != "" && sp.Max != "" && lo.GreaterThan(hi) {
				panic("masterdata: reversed parameter bounds " + sp.Key)
			}
			keys[sp.Key] = true
			r.specs = append(r.specs, sp)
		}
	}
	for _, reg := range registrations {
		owned := map[string]bool{}
		for _, sp := range reg.Specs {
			owned[sp.Key] = true
		}
		for _, raw := range reg.Seeds {
			seed := cloneSeed(raw)
			base, _, _ := strings.Cut(seed.Key, ":")
			if !owned[base] || seeds[seed.Key] {
				panic("masterdata: unowned or duplicate parameter seed " + seed.Key)
			}
			at, err := domain.ParseInstant(seed.EffectiveFrom)
			if err != nil {
				panic("masterdata: invalid seed date " + seed.Key)
			}
			v := domain.ParamVersion{Key: seed.Key, Text: seed.Text, Unit: seed.Unit, EffectiveFrom: at, SourceDoc: seed.SourceDoc}
			if seed.Value != nil {
				d, e := decimal.NewFromString(*seed.Value)
				if e != nil {
					panic("masterdata: invalid seed decimal " + seed.Key)
				}
				v.Value = &d
			}
			if err := (&Service{ParamRegistry: r}).validateParameterProposal(v); err != nil {
				panic(fmt.Sprintf("masterdata: invalid parameter seed %s: %v", seed.Key, err))
			}
			seeds[seed.Key] = true
			r.seeds = append(r.seeds, seed)
		}
	}
	svc := &Service{ParamRegistry: r}
	svc.parameterSpecs()
	if _, err := svc.parameterSeeds(); err != nil {
		panic(err)
	}
	return r
}

func (r *ParamRegistry) Specs() []port.ParamSpec {
	if r == nil {
		return nil
	}
	return slices.Clone(r.specs)
}

func (r *ParamRegistry) Seeds() []port.ParamSeed {
	if r == nil {
		return nil
	}
	out := make([]port.ParamSeed, len(r.seeds))
	for i, s := range r.seeds {
		out[i] = cloneSeed(s)
	}
	return out
}

func (r *ParamRegistry) specOf(key string) (port.ParamSpec, bool) {
	base, dims, _ := strings.Cut(key, ":")
	for _, sp := range r.Specs() {
		if sp.Key == base {
			sp.Key = key
			if sp.Dimensional && dims != "" {
				sp.Name += " — " + strings.ReplaceAll(dims, ":", " × ")
			}
			return sp, true
		}
	}
	return port.ParamSpec{}, false
}

func (s *Service) parameterSpecs() []port.ParamSpec {
	out := slices.Clone(domain.ParamRegistry)
	for _, sp := range s.ParamRegistry.Specs() {
		i := slices.IndexFunc(out, func(old port.ParamSpec) bool { return old.Key == sp.Key })
		if i < 0 {
			out = append(out, sp)
		} else if out[i] != sp {
			panic("masterdata: parameter registration conflicts with built-in " + sp.Key)
		}
	}
	return out
}

func (s *Service) parameterSpec(key string) port.ParamSpec {
	if sp, ok := s.ParamRegistry.specOf(key); ok {
		return sp
	}
	return domain.SpecOf(key)
}

func (s *Service) parameterSeeds() ([]port.ParamSeed, error) {
	var pf paramFile
	if err := decodeStrict(seeddata.Params, &pf); err != nil {
		return nil, fmt.Errorf("masterdata: dữ liệu nhúng params lỗi: %w", err)
	}
	if pf.Format != domain.ParamFormat {
		return nil, fmt.Errorf("masterdata: dữ liệu nhúng sai định dạng (%q)", pf.Format)
	}
	out := pf.Params
	for _, seed := range s.ParamRegistry.Seeds() {
		i := slices.IndexFunc(out, func(old port.ParamSeed) bool { return old.Key == seed.Key })
		if i < 0 {
			out = append(out, seed)
		} else if !reflect.DeepEqual(out[i], seed) {
			return nil, fmt.Errorf("masterdata: parameter registration conflicts with built-in %s", seed.Key)
		}
	}
	return out, nil
}

func (s *Service) parameterDefaults() (map[string]ParamDefault, error) {
	seeds, err := s.parameterSeeds()
	if err != nil {
		return nil, err
	}
	out := make(map[string]ParamDefault, len(seeds))
	for _, p := range seeds {
		d := ParamDefault{Key: p.Key, Text: p.Text, Unit: p.Unit, SourceDoc: p.SourceDoc}
		if p.Value != nil {
			v, e := decimal.NewFromString(*p.Value)
			if e != nil {
				return nil, e
			}
			d.Value = &v
		}
		out[p.Key] = d
	}
	return out, nil
}

func (s *Service) validateParameterProposal(v domain.ParamVersion) error {
	if err := v.ValidateProposal(); err != nil {
		return err
	}
	if sp, ok := s.ParamRegistry.specOf(v.Key); ok && sp.Kind == port.ParamKindColor && (v.Text == nil || !domain.IsHexColor(*v.Text)) {
		return domain.FieldError{Field: "text", Err: domain.ErrHexColor}
	}
	return nil
}
