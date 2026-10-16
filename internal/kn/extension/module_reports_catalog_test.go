package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReportCatalogMatchesLegacyContract(t *testing.T) {
	got, err := json.MarshalIndent(ReportCatalog(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	_, file, _, _ := runtime.Caller(0)
	want, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../tests/golden/kn-report-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("KN source/layout registration changed legacy catalog metadata")
	}
}
