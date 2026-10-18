package app

import (
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func contractCatalogContribution(t *testing.T) port.CatalogRegistration {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../tests/golden/kn-report-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var registration port.CatalogRegistration
	if err := json.Unmarshal(data, &registration); err != nil {
		t.Fatal(err)
	}
	if len(registration.Sources) != 5 || len(registration.Templates) != 5 {
		t.Fatal("catalog contract must preserve five sources and five layouts")
	}
	return registration
}
func TestReportCatalogRequiresRegistration(t *testing.T) {
	contribution := contractCatalogContribution(t)
	for _, tc := range []struct {
		name     string
		registry *CatalogRegistry
	}{{name: "missing"}, {name: "empty", registry: NewCatalogRegistry()}} {
		t.Run(tc.name, func(t *testing.T) {
			svc := Service{Catalog: tc.registry}
			for key := range contribution.Sources {
				if _, exists := svc.sourceSpecs()[key]; exists {
					t.Errorf("unregistered source inferred: %s", key)
				}
			}
			for _, template := range contribution.Templates {
				if _, exists := svc.builtinTemplate(template.Key); exists {
					t.Errorf("unregistered builtin inferred: %s", template.Key)
				}
			}
			for key := range domain.NativeSources() {
				if _, exists := svc.sourceSpecs()[key]; !exists {
					t.Errorf("native source lost: %s", key)
				}
			}
		})
	}
	svc := Service{Catalog: NewCatalogRegistry(contribution)}
	for key := range contribution.Sources {
		if _, exists := svc.sourceSpecs()[key]; !exists {
			t.Errorf("registered source missing: %s", key)
		}
	}
	for _, template := range contribution.Templates {
		if _, exists := svc.builtinTemplate(template.Key); !exists {
			t.Errorf("registered builtin missing: %s", template.Key)
		}
	}
}
