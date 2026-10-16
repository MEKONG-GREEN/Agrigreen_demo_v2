package extension

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReportFormSeedsMatchLegacyContract(t *testing.T) {
	got, err := json.MarshalIndent(ReportFormSeeds(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	_, file, _, _ := runtime.Caller(0)
	want, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../tests/golden/kn-report-form-seeds.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("KN seed payload changed legacy metadata, formulas or defaults")
	}
}
