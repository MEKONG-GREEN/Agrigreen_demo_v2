package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"testing"
)

func TestArchiveSelectionLimits(t *testing.T) {
	rows := make([]ArchiveSelection, ArchiveMaxFiles)
	for i := range rows {
		rows[i] = ArchiveSelection{PeriodID: ids.New(), ExportID: ids.New()}
	}
	if err := ValidateArchiveSelection(rows); err != nil {
		t.Fatal(err)
	}
	invalid := [][]ArchiveSelection{nil, append(append([]ArchiveSelection{}, rows...), ArchiveSelection{PeriodID: ids.New(), ExportID: ids.New()}), {{}}, {rows[0], rows[0]}}
	for i, in := range invalid {
		if err := ValidateArchiveSelection(in); err == nil {
			t.Fatalf("case%d accepted", i)
		}
	}
}
