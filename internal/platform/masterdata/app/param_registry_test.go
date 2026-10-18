package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

func sampleParamRegistration() port.ParamRegistration {
	value := "17.25"
	return port.ParamRegistration{
		Specs: []port.ParamSpec{{Key: "fixture.delay", Name: "Độ trễ", Kind: port.ParamKindDecimal, Unit: "giờ", Min: "0", Max: "72", Dimensional: true}},
		Seeds: []port.ParamSeed{{Key: "fixture.delay:north", Value: &value, Unit: "giờ", EffectiveFrom: "2026-01-01", SourceDoc: "Quyết định thử nghiệm"}},
	}
}

func TestParamRegistryCopiesAndIsolates(t *testing.T) {
	in := sampleParamRegistration()
	r := NewParamRegistry(in)
	in.Specs[0].Name = "corrupt"
	*in.Seeds[0].Value = "99"
	r.Specs()[0].Name = "corrupt"
	*r.Seeds()[0].Value = "88"
	svc := &Service{ParamRegistry: r}
	sp := svc.parameterSpec("fixture.delay:north:south")
	if sp.Name != "Độ trễ — north × south" || sp.Key != "fixture.delay:north:south" {
		t.Fatalf("metadata: %+v", sp)
	}
	ds, err := svc.parameterDefaults()
	if err != nil || ds["fixture.delay:north"].Value.String() != "17.25" {
		t.Fatalf("default alias: %+v %v", ds, err)
	}
	d := ds["fixture.delay:north"]
	d.Value = nil
	ds["fixture.delay:north"] = d
	ds2, err := svc.parameterDefaults()
	if err != nil || ds2["fixture.delay:north"].Value == nil {
		t.Fatal("caller changed stored default")
	}
	other := &Service{ParamRegistry: NewParamRegistry()}
	for _, s := range other.parameterSpecs() {
		if strings.HasPrefix(s.Key, "fixture.") {
			t.Fatal("registration leaked between instances")
		}
	}
	plain, err := other.parameterDefaults()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain["fixture.delay:north"]; ok {
		t.Fatal("seed leaked between instances")
	}
}

func TestParamRegistryRejectsInvalidRegistration(t *testing.T) {
	for _, tc := range []string{"duplicate-spec", "bad-key", "blank-name", "bad-kind", "bad-min", "bad-max", "reversed", "unowned", "duplicate-seed", "bad-date", "bad-decimal", "both-values", "native-spec", "native-seed", "bad-color", "cross-owner"} {
		t.Run(tc, func(t *testing.T) {
			in := sampleParamRegistration()
			switch tc {
			case "duplicate-spec":
				in.Specs = append(in.Specs, in.Specs[0])
			case "bad-key":
				in.Specs[0].Key = "bad:key"
			case "blank-name":
				in.Specs[0].Name = " "
			case "bad-kind":
				in.Specs[0].Kind = "uuid"
			case "bad-min":
				in.Specs[0].Min = "abc"
			case "bad-max":
				in.Specs[0].Max = "abc"
			case "reversed":
				in.Specs[0].Min = "80"
			case "unowned":
				in.Seeds[0].Key = "other.key"
			case "duplicate-seed":
				in.Seeds = append(in.Seeds, in.Seeds[0])
			case "bad-date":
				in.Seeds[0].EffectiveFrom = "yesterday"
			case "bad-decimal":
				*in.Seeds[0].Value = "NaN"
			case "both-values":
				text := "text"
				in.Seeds[0].Text = &text
			case "native-spec":
				in.Specs[0] = domain.ParamRegistry[0]
				in.Specs[0].Name = "override"
				in.Seeds = nil
			case "native-seed":
				in.Specs[0] = domain.SpecOf("geo.hub_capacity_warn_pct")
				in.Seeds[0].Key = in.Specs[0].Key
			case "bad-color":
				in.Specs[0].Kind = port.ParamKindColor
			}
			defer func() {
				if recover() == nil {
					t.Fatal("invalid registration accepted")
				}
			}()
			if tc == "cross-owner" {
				other := port.ParamRegistration{Seeds: in.Seeds}
				in.Seeds = nil
				NewParamRegistry(in, other)
			} else {
				NewParamRegistry(in)
			}
		})
	}
	in := sampleParamRegistration()
	defer func() {
		if recover() == nil {
			t.Fatal("multiple owners accepted")
		}
	}()
	NewParamRegistry(in, in)
}

func TestRegisteredColorProposal(t *testing.T) {
	svc := &Service{ParamRegistry: NewParamRegistry(port.ParamRegistration{Specs: []port.ParamSpec{{Key: "fixture.color", Name: "Màu", Kind: port.ParamKindColor}}})}
	value := "#12AB34"
	v := domain.ParamVersion{Key: "fixture.color", Text: &value, EffectiveFrom: domain.Day(2026, 1, 1), SourceDoc: "Quyết định thử nghiệm"}
	if err := svc.validateParameterProposal(v); err != nil {
		t.Fatal(err)
	}
	value = "blue"
	if err := svc.validateParameterProposal(v); err == nil {
		t.Fatal("unregistered color validation path")
	}
}

func TestParametersRequireRegistration(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../tests/golden/kn-masterdata-params.json"))
	if err != nil {
		t.Fatal(err)
	}
	var owned port.ParamRegistration
	if err := json.Unmarshal(raw, &owned); err != nil {
		t.Fatal(err)
	}
	if len(owned.Specs) != 16 || len(owned.Seeds) != 13 {
		t.Fatal("frozen ownership inventory changed")
	}
	for _, tc := range []struct {
		name     string
		registry *ParamRegistry
	}{{"nil", nil}, {"empty", NewParamRegistry()}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{ParamRegistry: tc.registry}
			specs := svc.parameterSpecs()
			for _, expected := range owned.Specs {
				for _, sp := range specs {
					if sp.Key == expected.Key {
						t.Fatalf("unregistered parameter inferred: %s", sp.Key)
					}
				}
				if svc.parameterSpec(expected.Key).Name != expected.Key {
					t.Fatalf("unregistered label inferred: %s", expected.Key)
				}
			}
			defaults, err := svc.parameterDefaults()
			if err != nil {
				t.Fatal(err)
			}
			for _, seed := range owned.Seeds {
				if _, found := defaults[seed.Key]; found {
					t.Fatalf("unregistered default inferred: %s", seed.Key)
				}
			}
		})
	}
}
