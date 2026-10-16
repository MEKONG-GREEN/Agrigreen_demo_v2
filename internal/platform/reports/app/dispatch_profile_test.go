package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/dispatch"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

type dispatchProfileCase struct {
	Name, PeriodLabel, RangeLabel, FormCode string
	Forms, Missing, Links                   []string
	Provisional                             bool
	Subject, Body, FileKey, SheetKey        string
}
type dispatchProfileContract struct {
	Profile port.DispatchProfile
	Cases   []dispatchProfileCase
}

func dispatchProfileContractPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "../../../../tests/golden/kn-report-dispatch-profile.json")
}
func loadDispatchProfileContract(t *testing.T) dispatchProfileContract {
	t.Helper()
	raw, err := os.ReadFile(dispatchProfileContractPath())
	if err != nil {
		t.Fatal(err)
	}
	var c dispatchProfileContract
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestLegacyDispatchProfileContract(t *testing.T) {
	c := dispatchProfileContract{Profile: legacyDispatchProfile(), Cases: []dispatchProfileCase{
		{Name: "closed", PeriodLabel: "Tuần 36", Forms: []string{"T01", "T04"}, FormCode: "T01"},
		{Name: "provisional", PeriodLabel: "Tuần 36", RangeLabel: "31/08–06/09/2026", Forms: []string{"T04"}, Provisional: true, Missing: []string{"Xã A", "Xã B"}, FormCode: "T04"},
		{Name: "links", PeriodLabel: "Tuần 36", Forms: []string{"T01"}, Links: []string{"T01.xlsx: https://example.invalid/one", "T04.pdf: https://example.invalid/two"}, FormCode: "PL_01"},
		{Name: "empty", FormCode: "t07"},
	}}
	for i := range c.Cases {
		p := &c.Cases[i]
		p.Subject = dispatch.Subject("Phòng Trồng trọt", p.PeriodLabel, strings.Join(p.Forms, ", "), p.Provisional)
		p.Body = dispatch.Body(p.PeriodLabel, p.RangeLabel, p.Forms, p.Provisional, p.Missing, p.Links)
		p.FileKey = "kn." + strings.ToLower(p.FormCode)
		p.SheetKey = "kn.gsheet"
	}
	got, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("AG_CAPTURE_LEGACY_DISPATCH_PROFILE") == "1" {
		if err := os.WriteFile(dispatchProfileContractPath(), got, 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(dispatchProfileContractPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("legacy dispatch contract changed")
	}
}
func TestRegisteredDispatchMessagesMatchLegacyContract(t *testing.T) {
	c := loadDispatchProfileContract(t)
	svc := Service{Policies: NewPolicyRegistry(port.Policy{DispatchProfile: &c.Profile})}
	profile, err := svc.dispatchProfile()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range c.Cases {
		t.Run(p.Name, func(t *testing.T) {
			subject := dispatch.Subject(profile.SubjectDepartment, p.PeriodLabel, strings.Join(p.Forms, ", "), p.Provisional)
			body := dispatch.BodyFor(profile, p.PeriodLabel, p.RangeLabel, p.Forms, p.Provisional, p.Missing, p.Links)
			if subject != p.Subject || body != p.Body {
				t.Fatal("registered dispatch message changed legacy bytes")
			}
			if dispatchFileKey(profile, p.FormCode) != p.FileKey || profile.SheetTemplateKey != p.SheetKey {
				t.Fatal("registered egress namespace changed")
			}
		})
	}
}
func TestRegisteredDispatchProfileCopiesAndIsolation(t *testing.T) {
	c := loadDispatchProfileContract(t)
	input := c.Profile
	registry := NewPolicyRegistry(port.Policy{DispatchProfile: &input})
	input.SubjectDepartment = "changed"
	got, ok := registry.DispatchProfile()
	if !ok || got != c.Profile {
		t.Fatal("profile kept input alias")
	}
	got.SenderName = "changed output"
	again, _ := registry.DispatchProfile()
	if again != c.Profile {
		t.Fatal("profile leaked output alias")
	}
	other := c.Profile
	other.TemplatePrefix = "another."
	other.SheetTemplateKey = "another.sheet"
	otherRegistry := NewPolicyRegistry(port.Policy{DispatchProfile: &other})
	separate, _ := otherRegistry.DispatchProfile()
	if separate.TemplatePrefix != "another." || again.TemplatePrefix != "kn." {
		t.Fatal("profile crossed service boundaries")
	}
	svc := Service{Policies: NewPolicyRegistry()}
	if _, err := svc.dispatchProfile(); err == nil {
		t.Fatal("explicit empty registry inferred KN dispatch profile")
	}
}
func TestDispatchProfileRegistrationRejectsInvalid(t *testing.T) {
	changes := map[string]func(*port.DispatchProfile){
		"subject":    func(p *port.DispatchProfile) { p.SubjectDepartment = "" },
		"salutation": func(p *port.DispatchProfile) { p.Salutation = "" },
		"sender":     func(p *port.DispatchProfile) { p.SenderName = "" },
		"prefix":     func(p *port.DispatchProfile) { p.TemplatePrefix = "invalid" },
		"sheet":      func(p *port.DispatchProfile) { p.SheetTemplateKey = "Invalid Key" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			p := loadDispatchProfileContract(t).Profile
			change(&p)
			defer func() {
				if recover() == nil {
					t.Error("invalid dispatch registration accepted")
				}
			}()
			NewPolicyRegistry(port.Policy{DispatchProfile: &p})
		})
	}
	t.Run("duplicate", func(t *testing.T) {
		p := loadDispatchProfileContract(t).Profile
		defer func() {
			if recover() == nil {
				t.Error("duplicate dispatch registration accepted")
			}
		}()
		NewPolicyRegistry(port.Policy{DispatchProfile: &p}, port.Policy{DispatchProfile: &p})
	})
}
