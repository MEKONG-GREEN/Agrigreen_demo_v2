package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

func TestSeedExportUsesOwnerLayout(t *testing.T) {
	out := filepath.Join(t.TempDir(), "T04.xlsx")
	if err := run("", "", "", "", 0, 0, "", false, "T04", out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	def, _, err := reports.ImportFormXLSX(bytes.NewReader(data), port.FormImportOptions{Code: "T04"})
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Columns) != 226 || def.Columns[225].Letter != "HR" || def.Columns[4].Formula == "" {
		t.Fatal("seed CLI changed T04 layout/formulas")
	}
	if err := run("", "", "", "", 0, 0, "", false, "UNKNOWN", out); err == nil {
		t.Fatal("unknown seed accepted")
	}
}
