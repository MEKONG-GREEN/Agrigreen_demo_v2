package port_test

import (
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

func attributeSpec() port.AttributeCatalogSpec {
	return port.AttributeCatalogSpec{Version: "owner/1", Levels: []port.MgmtLevel{{Code: "staff", Label: "Cán bộ"}, {Code: "head", Label: "Lãnh đạo", Manager: true}},
		Groups: []port.AttributeGroup{{Code: "readers", Label: "Chỉ xem", ReadsAllDepartments: true, ReadOnly: true}}, Duties: []port.CatalogItem{{Code: "task_owner", Label: "Phụ trách"}},
		Roles: []port.AttributeRole{{Role: "professional", GroupAndDuties: true, ManagementPermissions: true}, {Role: "collector", ScopedTargets: true}}}
}

func TestAttributeCatalogImmutableInstancesAndExplicitFlags(t *testing.T) {
	spec := attributeSpec()
	first, err := port.NewAttributeCatalog(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Levels[1].Manager = false
	spec.Levels[1].ReadsAllDepartments = true
	second, err := port.NewAttributeCatalog(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Levels[0].Label = "changed"
	spec.Groups[0].ReadOnly = false
	spec.Duties[0].Label = "changed"
	spec.Roles[0].ManagementPermissions = false
	first.Levels()[0].Label = "changed"
	first.Groups()[0].ReadOnly = false
	first.Duties()[0].Label = "changed"
	first.Roles()[0].ManagementPermissions = false
	if first.LevelLabel("staff") != "Cán bộ" || first.GroupLabel("readers") != "Chỉ xem" || first.DutyLabel("task_owner") != "Phụ trách" {
		t.Fatal("snapshot mutated")
	}
	a, err := first.Clone().Capabilities("professional", "head", "")
	if err != nil || !a.Manager || a.ReadsAllDepartments {
		t.Fatalf("manager inferred reads-all: %+v %v", a, err)
	}
	b, err := second.Capabilities("professional", "head", "")
	if err != nil || b.Manager || !b.ReadsAllDepartments {
		t.Fatalf("instances leaked: %+v %v", b, err)
	}
	ro, err := first.Capabilities("professional", "head", "readers")
	if err != nil || !ro.ForceReadOnly || !ro.ReadsAllDepartments {
		t.Fatalf("read-only flags lost: %+v %v", ro, err)
	}
	collector, err := first.Capabilities("collector", "head", "readers")
	if err != nil || collector.Manager || collector.ReadsAllDepartments || collector.ForceReadOnly || !collector.ScopedTargets {
		t.Fatalf("role flags unioned: %+v %v", collector, err)
	}
	if code, ok := first.ManagementCode("  CÁN-BỘ  "); !ok || code != "staff" {
		t.Fatalf("CSV label: %q %v", code, ok)
	}
	if code, ok := first.ManagementCode("head"); !ok || code != "head" {
		t.Fatal("CSV code")
	}
}

func TestAttributeCatalogRejectsAmbiguousCSVAndInvalidDefinitions(t *testing.T) {
	for name, mutate := range map[string]func(*port.AttributeCatalogSpec){
		"version":            func(s *port.AttributeCatalogSpec) { s.Version = " " },
		"no roles":           func(s *port.AttributeCatalogSpec) { s.Roles = nil },
		"duplicate role":     func(s *port.AttributeCatalogSpec) { s.Roles = append(s.Roles, s.Roles[0]) },
		"duplicate level":    func(s *port.AttributeCatalogSpec) { s.Levels = append(s.Levels, s.Levels[0]) },
		"invalid code":       func(s *port.AttributeCatalogSpec) { s.Groups[0].Code = "Invalid code" },
		"empty label":        func(s *port.AttributeCatalogSpec) { s.Duties[0].Label = " " },
		"normalized labels":  func(s *port.AttributeCatalogSpec) { s.Levels[1].Label = "CÁN---BỘ" },
		"label against code": func(s *port.AttributeCatalogSpec) { s.Levels[1].Label = "STAFF" },
	} {
		t.Run(name, func(t *testing.T) {
			s := attributeSpec()
			mutate(&s)
			if _, err := port.NewAttributeCatalog(s); !errors.Is(err, port.ErrAttributeCatalog) {
				t.Fatalf("accepted ambiguous or invalid catalog: %v", err)
			}
		})
	}
}

func TestAttributeCatalogZeroAndUnknownCodesNeverFallBack(t *testing.T) {
	for _, c := range []*port.AttributeCatalog{nil, {}, (&port.AttributeCatalog{}).Clone()} {
		if c.Valid() || len(c.Levels()) != 0 || len(c.Groups()) != 0 || len(c.Duties()) != 0 || c.LevelLabel(port.MgmtTruongPhong) != "" {
			t.Fatal("invalid catalog fell back")
		}
		if _, err := c.Capabilities(port.KNTinh, port.MgmtTruongPhong, ""); !errors.Is(err, port.ErrAttributeCatalog) {
			t.Fatalf("invalid catalog: %v", err)
		}
	}
	c, err := port.NewAttributeCatalog(attributeSpec())
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range [][3]string{{"unknown", "", ""}, {"professional", "truong_phong", ""}, {"professional", "head", "ban_giam_doc"}} {
		if caps, err := c.Capabilities(in[0], in[1], in[2]); err == nil || caps != (port.AttributeCapabilities{}) {
			t.Fatalf("unknown stored code accepted: %+v %v", caps, err)
		}
	}
	if _, ok := c.ManagementCode("Trưởng phòng"); ok {
		t.Fatal("CSV used legacy label")
	}
}
