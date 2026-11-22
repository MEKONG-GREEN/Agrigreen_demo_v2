package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"strings"
	"testing"
)

func TestW1168RawSourceNumbersAreBoundedBeforeEvaluation(t *testing.T) {
	for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000)} {
		periods := []port.KHTCPeriod{{Departments: []port.DepartmentFigures{{Forms: []port.TargetFigure{{Sources: []port.SourcePeriodRef{{Cells: map[string]string{"value": raw}}}}}}}}}
		if err := validateAdhocSources(periods); err == nil {
			t.Fatalf("hostile source len%d accepted", len(raw))
		}
	}
	good := []port.KHTCPeriod{{Departments: []port.DepartmentFigures{{Forms: []port.TargetFigure{{Sum: map[string]string{"value": "123456789012345678901234567890.123456"}}}}}}}
	if err := validateAdhocSources(good); err != nil {
		t.Fatal(err)
	}
}
