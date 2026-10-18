package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

func TestReportDispatchProfileMatchesLegacyContract(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../tests/golden/kn-report-dispatch-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c struct{ Profile reportsport.DispatchProfile }
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	got := ReportPolicy().DispatchProfile
	if got == nil || *got != c.Profile {
		t.Fatal("KN dispatch profile changed approved labels or egress keys")
	}
}
