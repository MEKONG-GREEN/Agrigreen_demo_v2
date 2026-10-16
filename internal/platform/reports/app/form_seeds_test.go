package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

func TestLegacyFormSeedContract(t *testing.T) {
	got, err := json.MarshalIndent(legacyFormSeedSet(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "../../../../tests/golden/kn-report-form-seeds.json")
	if os.Getenv("AG_CAPTURE_LEGACY_FORM_SEEDS") == "1" {
		if err := os.WriteFile(path, got, 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("legacy form seed contract changed")
	}
}

func formSeedFixture(t *testing.T) port.FormSeedRegistration {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../tests/golden/kn-report-form-seeds.json"))
	if err != nil {
		t.Fatal(err)
	}
	var set port.FormSeedRegistration
	if err = json.Unmarshal(data, &set); err != nil {
		t.Fatal(err)
	}
	return set
}
func TestRegisteredFormSeedsIsolationAndCopies(t *testing.T) {
	input := formSeedFixture(t)
	registry := NewFormSeedRegistry(input)
	before, _ := registry.defaultSet()
	input.Templates[0].Def.Columns[0].Title = "changed input"
	input.Templates[0].Def.Header.Cells[0].Text = "changed header"
	input.Templates[0].Def.Source.SeedNote = "changed source"
	input.Templates[0].Name = "changed name"
	first, ok := registry.defaultSet()
	if !ok {
		t.Fatal("no default set")
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(first)
	if !bytes.Equal(a, b) {
		t.Fatal("registration retained input aliases")
	}
	first.Templates[0].Def.Columns[0].Title = "changed output"
	first.Templates[0].Def.Pending[0].Detail = "changed pending"
	first.Templates[0].Def.Source.SeedNote = "changed source"
	again, _ := registry.defaultSet()
	b, _ = json.Marshal(again)
	if !bytes.Equal(a, b) {
		t.Fatal("registry leaked output aliases")
	}
	other := formSeedFixture(t)
	other.Key = "another.forms"
	other.DefaultDepartment = "another_department"
	other.Version = "another-version"
	other.Templates = other.Templates[:1]
	second := NewFormSeedRegistry(other)
	otherSet, _ := second.defaultSet()
	if len(otherSet.Templates) != 1 || otherSet.DefaultDepartment != "another_department" || len(again.Templates) != 14 {
		t.Fatal("seed registry crossed service boundaries")
	}
	svc := Service{FormSeeds: NewFormSeedRegistry()}
	if _, err := svc.formSeedSet(); err == nil {
		t.Fatal("explicit empty registry inferred application seeds")
	}
}
func TestFormSeedRegistryRejectsInvalid(t *testing.T) {
	cases := map[string]func(*port.FormSeedRegistration){
		"key":             func(s *port.FormSeedRegistration) { s.Key = "" },
		"department":      func(s *port.FormSeedRegistration) { s.DefaultDepartment = "Invalid Department" },
		"version":         func(s *port.FormSeedRegistration) { s.Version = "" },
		"empty":           func(s *port.FormSeedRegistration) { s.Templates = nil },
		"duplicate-code":  func(s *port.FormSeedRegistration) { s.Templates = append(s.Templates, s.Templates[0]) },
		"definition-code": func(s *port.FormSeedRegistration) { s.Templates[0].Def.Code = "DIFFERENT" },
		"level":           func(s *port.FormSeedRegistration) { s.Templates[0].EntryLevel = "invalid" },
		"report-type":     func(s *port.FormSeedRegistration) { s.Templates[0].ReportType = "invalid" },
		"formula":         func(s *port.FormSeedRegistration) { s.Templates[0].Def.Columns[12].Formula = "SUM(" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			set := formSeedFixture(t)
			change(&set)
			defer func() {
				if recover() == nil {
					t.Error("invalid registration accepted")
				}
			}()
			NewFormSeedRegistry(set)
		})
	}
	for _, tc := range []string{"duplicate-key", "multiple-default"} {
		t.Run(tc, func(t *testing.T) {
			a := formSeedFixture(t)
			b := formSeedFixture(t)
			if tc == "multiple-default" {
				b.Key = "another.forms"
			}
			defer func() {
				if recover() == nil {
					t.Error("ambiguous registration accepted")
				}
			}()
			NewFormSeedRegistry(a, b)
		})
	}
}
