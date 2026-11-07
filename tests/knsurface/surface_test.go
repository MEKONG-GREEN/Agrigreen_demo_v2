// Package knsurface_test inventories presentation and registered routes without
// serving requests. Surface differences are informational until #272.
package knsurface_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.yaml.in/yaml/v3"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

type catalog struct {
	Version   string   `yaml:"version"`
	Functions []string `yaml:"functions"`
}

type entry struct {
	ID      string   `yaml:"id"`
	Kind    string   `yaml:"kind"`
	Keys    []string `yaml:"keys"`
	Status  string   `yaml:"status"`
	FN      []string `yaml:"fn"`
	Needs   []string `yaml:"needs"`
	Package string   `yaml:"package"`
	Note    string   `yaml:"note"`
}

type manifest struct {
	Version int                `yaml:"version"`
	Mode    string             `yaml:"mode"`
	Catalog map[string]catalog `yaml:"catalog"`
	Entries []entry            `yaml:"entries"`
}

var functionCode = regexp.MustCompile(`^FN-(?:[0-9]{2}|(?:CH|TW|TI|XA|PN|AD)-[0-9]{2}\.[0-9]{2})$`)

func parseManifest(raw string) (manifest, error) {
	var m manifest
	d := yaml.NewDecoder(strings.NewReader(raw))
	d.KnownFields(true)
	if err := d.Decode(&m); err != nil {
		return m, err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return m, fmt.Errorf("expected one YAML document")
		}
		return m, fmt.Errorf("trailing YAML document: %w", err)
	}
	if m.Version != 1 || m.Mode != "report" || len(m.Catalog) == 0 || len(m.Entries) == 0 {
		return m, fmt.Errorf("expected version 1, report mode, catalog and entries")
	}
	refs := map[string]bool{}
	for name, c := range m.Catalog {
		if !slices.Contains([]string{"tt", "cnts", "hcth", "khtc", "price"}, name) || c.Version == "" || len(c.Functions) == 0 {
			return m, fmt.Errorf("invalid catalog %q", name)
		}
		for _, code := range c.Functions {
			ref := name + "/" + code
			if !functionCode.MatchString(code) || refs[ref] {
				return m, fmt.Errorf("invalid or duplicate function %q", ref)
			}
			refs[ref] = true
		}
	}
	ids, keys := map[string]bool{}, map[string]bool{}
	for _, e := range m.Entries {
		if e.ID == "" || ids[e.ID] || !slices.Contains([]string{"menu", "web", "api", "mobile"}, e.Kind) || len(e.Keys) == 0 {
			return m, fmt.Errorf("invalid or duplicate entry %q", e.ID)
		}
		ids[e.ID] = true
		for _, ref := range append(slices.Clone(e.FN), e.Needs...) {
			if !refs[ref] {
				return m, fmt.Errorf("%s: unknown function %q", e.ID, ref)
			}
		}
		switch e.Status {
		case "FN":
			if len(e.FN) == 0 || len(e.Needs) != 0 || e.Package != "" {
				return m, fmt.Errorf("%s: FN requires fn only", e.ID)
			}
		case "HẠ_TẦNG":
			if len(e.Needs) == 0 || len(e.FN) != 0 || e.Package != "" {
				return m, fmt.Errorf("%s: infrastructure requires needs only", e.ID)
			}
		case "GỠ":
			if e.Package == "" || len(e.FN) != 0 || len(e.Needs) != 0 {
				return m, fmt.Errorf("%s: removal requires package without fn", e.ID)
			}
		case "HOÃN":
			if e.Package == "" || len(e.FN) == 0 || len(e.Needs) != 0 {
				return m, fmt.Errorf("%s: deferred requires package and fn", e.ID)
			}
		default:
			return m, fmt.Errorf("%s: unknown status %q", e.ID, e.Status)
		}
		for _, key := range e.Keys {
			identity := e.Kind + ":" + key
			if key == "" || keys[identity] || (strings.Contains(key, "*") && (e.Kind != "api" || !strings.HasSuffix(key, "/**") || strings.Contains(strings.TrimSuffix(key, "/**"), "*"))) {
				return m, fmt.Errorf("%s: invalid or duplicate key %q", e.ID, key)
			}
			keys[identity] = true
		}
	}
	return m, nil
}

// API selectors own a resource root plus descendants, on segment boundaries.
// Other kinds enumerate exact keys, including Next dynamic route filenames.
func matches(kind, selector, key string) bool {
	if kind == "api" && strings.HasSuffix(selector, "/**") {
		base := strings.TrimSuffix(selector, "/**")
		return key == base || strings.HasPrefix(key, base+"/")
	}
	return key == selector
}

type difference struct {
	Missing, Unused, Ambiguous []string
}

func compare(m manifest, kind string, actual []string) difference {
	var result difference
	used := map[string]bool{}
	for _, key := range actual {
		var found []string
		for _, e := range m.Entries {
			if e.Kind != kind {
				continue
			}
			for _, selector := range e.Keys {
				if matches(kind, selector, key) {
					used[selector] = true
					found = append(found, e.ID)
				}
			}
		}
		if len(found) == 0 {
			result.Missing = append(result.Missing, key)
		} else if len(found) > 1 {
			result.Ambiguous = append(result.Ambiguous, key+" -> "+strings.Join(found, ", "))
		}
	}
	for _, e := range m.Entries {
		if e.Kind == kind {
			for _, key := range e.Keys {
				if !used[key] {
					result.Unused = append(result.Unused, key)
				}
			}
		}
	}
	slices.Sort(result.Missing)
	slices.Sort(result.Unused)
	slices.Sort(result.Ambiguous)
	return result
}

type availableForms struct{}

func (availableForms) HasForms(context.Context, string, string, string) (bool, error) {
	return true, nil
}

func menuKeys(t *testing.T) []string {
	t.Helper()
	c, err := extension.MenuCatalog(hrm.MenuSource(), price.MenuSource(), contact.MenuSource())
	if err != nil {
		t.Fatal(err)
	}
	c.UseFormAvailability(availableForms{}) // Include conditional and disabled leaves.
	registry, err := c.Registry()
	if err != nil {
		t.Fatal(err)
	}
	unit := uuid.MustParse("018f8f00-0000-7000-8000-000000000001")
	profiles := []string{"trong_trot", "chan_nuoi_thuy_san", "hanh_chinh_tong_hop", "ke_hoach_tai_chinh", "thong_tin_truyen_thong"}
	var all []string
	var departments []partyport.Department
	add := func(scope string, definition partyport.MenuDefinition) {
		var walk func([]partyport.MenuItem)
		walk = func(items []partyport.MenuItem) {
			for _, item := range items {
				owner := scope
				if scope == "admin" && strings.HasPrefix(item.Route, "/kn/phong-ban/") {
					owner += "/" + strings.Split(strings.TrimPrefix(item.Route, "/kn/phong-ban/"), "/")[0]
				}
				all = append(all, owner+":"+item.Key)
				walk(item.Children)
			}
		}
		for _, group := range definition.Groups {
			all = append(all, scope+":"+group.Key)
			walk(group.Items)
		}
	}
	for _, profile := range profiles {
		dept := partyport.Department{OrgRef: unit, Code: profile, Name: profile, MenuProfile: profile, Status: partyport.DeptActive}
		departments = append(departments, dept)
		for _, audience := range []string{partyport.AudienceTW, partyport.AudienceTinh, partyport.AudienceXa} {
			definition, err := registry.Build(context.Background(), partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{
				Audience: audience, UnitRef: unit, Department: dept}, Mode: partyport.MenuDepartment})
			if err != nil {
				t.Fatal(err)
			}
			add(profile, definition)
		}
	}
	definition, err := registry.Build(context.Background(), partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{
		Audience: partyport.AudienceAdmin, UnitRef: unit}, Mode: partyport.MenuAdmin, Departments: departments})
	if err != nil {
		t.Fatal(err)
	}
	add("admin", definition)
	return unique(all)
}

func apiKeys(t *testing.T) []string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	type routes interface {
		RegisterApp(*gin.RouterGroup)
		RegisterAdmin(*gin.RouterGroup)
	}
	// Registration does not execute handlers or access a database/external service.
	mods := []routes{extension.New(nil, clock.NewFixed(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)), extension.Deps{}), &hrm.Module{}, &price.Module{}, &contact.Module{}}
	var all []string
	operations := 0
	for _, mod := range mods {
		for _, admin := range []bool{false, true} {
			r := gin.New()
			if admin {
				mod.RegisterAdmin(r.Group("/v1"))
			} else {
				mod.RegisterApp(r.Group("/v1"))
			}
			for _, route := range r.Routes() {
				all = append(all, route.Path)
				operations++
			}
		}
	}
	t.Logf("registered KN API operations=%d", operations)
	return unique(all)
}

func unique(values []string) []string {
	slices.Sort(values)
	return slices.Compact(values)
}

var screenClass = regexp.MustCompile(`\bclass ([A-Z][A-Za-z0-9]*Screen) extends\b`)

func fileKeys(t *testing.T, root string) ([]string, []string) {
	t.Helper()
	var web, mobile []string
	app := filepath.Join(root, "apps/web-ops/src/app")
	err := filepath.WalkDir(app, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(app, path)
		if err != nil {
			return err
		}
		if d.Name() == "page.tsx" || (d.Name() == "route.ts" && strings.HasPrefix(filepath.ToSlash(rel), "kn/")) {
			route := filepath.ToSlash(filepath.Dir(rel))
			if route == "." {
				route = ""
			}
			web = append(web, "/"+route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"apps/field-app/lib", "packages/agrigreen_mobile/lib"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Ext(path) != ".dart" {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, match := range screenClass.FindAllStringSubmatch(string(raw), -1) {
				mobile = append(mobile, match[1])
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return unique(web), unique(mobile)
}

func TestReportRegisteredSurfaces(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "docs/kn/brd-surface.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := parseManifest(string(raw))
	if err != nil {
		t.Fatal(err) // Malformed policy is an error; surface differences below are not.
	}
	web, mobile := fileKeys(t, root)
	for _, inventory := range []struct {
		kind string
		keys []string
	}{{"menu", menuKeys(t)}, {"api", apiKeys(t)}, {"web", web}, {"mobile", mobile}} {
		diff := compare(m, inventory.kind, inventory.keys)
		t.Logf("KN surface REPORT ONLY %s: actual=%d missing=%d unused=%d ambiguous=%d", inventory.kind, len(inventory.keys), len(diff.Missing), len(diff.Unused), len(diff.Ambiguous))
		for _, group := range []struct {
			label string
			keys  []string
		}{{"missing", diff.Missing}, {"unused", diff.Unused}, {"ambiguous", diff.Ambiguous}} {
			for _, key := range group.keys {
				t.Logf("%s %s %s", inventory.kind, group.label, key)
			}
		}
	}
}

const validPolicy = `version: 1
mode: report
catalog:
  tt:
    version: "7.7"
    functions: [FN-XA-03.05]
entries:
  - id: plots
    kind: api
    keys: [/v1/kn/plots/**]
    status: FN
    fn: [tt/FN-XA-03.05]
`

func TestManifestValidation(t *testing.T) {
	if _, err := parseManifest(validPolicy); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"unknown field":                validPolicy + "surprise: true\n",
		"wrong mode":                   strings.Replace(validPolicy, "mode: report", "mode: enforce", 1),
		"old function":                 strings.Replace(validPolicy, "tt/FN-XA-03.05", "hcth/FN-21", 1),
		"missing function":             strings.Replace(validPolicy, "fn: [tt/FN-XA-03.05]", "fn: []", 1),
		"infrastructure without needs": strings.Replace(validPolicy, "status: FN", "status: HẠ_TẦNG", 1),
		"removal without package":      strings.Replace(validPolicy, "status: FN\n    fn: [tt/FN-XA-03.05]", "status: GỠ", 1),
		"deferred without package":     strings.Replace(validPolicy, "status: FN", "status: HOÃN", 1),
		"duplicate key":                strings.Replace(validPolicy, "[/v1/kn/plots/**]", "[/v1/kn/plots/**, /v1/kn/plots/**]", 1),
		"duplicate YAML field":         validPolicy + "version: 1\n",
		"multiple documents":           validPolicy + "---\nversion: 1\n",
		"syntax":                       "entries: [",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseManifest(raw); err == nil {
				t.Fatal("accepted invalid policy")
			}
		})
	}
}

func TestReportDetectsUnmappedStaleAndOverlappingSurfaces(t *testing.T) {
	m, err := parseManifest(validPolicy)
	if err != nil {
		t.Fatal(err)
	}
	actual := []string{"/v1/kn/plots/abc", "/v1/kn/plots-extra"}
	diff := compare(m, "api", actual)
	if !slices.Equal(diff.Missing, []string{"/v1/kn/plots-extra"}) || len(diff.Unused) != 0 || len(diff.Ambiguous) != 0 {
		t.Fatalf("segment boundary/missing report: %+v", diff)
	}
	diff = compare(m, "api", []string{"/v1/new-resource"})
	if !slices.Equal(diff.Unused, []string{"/v1/kn/plots/**"}) {
		t.Fatalf("stale report: %+v", diff)
	}
	m.Entries = append(m.Entries, entry{ID: "specific", Kind: "api", Keys: []string{"/v1/kn/plots/abc"}})
	diff = compare(m, "api", actual)
	if len(diff.Ambiguous) != 1 || !strings.Contains(diff.Ambiguous[0], "plots, specific") {
		t.Fatalf("overlap report: %+v", diff)
	}
	// A caller logs these differences; it never converts them to a failing gate.
}
