package app

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestArchive199MixedSelectionContract(t *testing.T) {
	period, exportID, word := ids.New(), ids.New(), ids.New()
	good := fmt.Sprintf(`[{"period_id":%q,"export_id":%q},{"period_id":%q,"export_id":%q,"format":"word","run_id":%q}]`, period, exportID, period, exportID, word)
	var selection []ArchiveSelection
	if err := json.Unmarshal([]byte(good), &selection); err != nil {
		t.Fatal(err)
	}
	if err := ValidateArchiveSelection(selection); err != nil {
		t.Fatalf("Excel and a distinct saved Word must be selectable together: %v", err)
	}
	for _, wire := range []string{
		fmt.Sprintf(`[{"period_id":%q,"export_id":%q,"format":"word"}]`, period, exportID),
		fmt.Sprintf(`[{"period_id":%q,"export_id":%q,"format":"pdf"}]`, period, exportID),
		fmt.Sprintf(`[{"period_id":%q,"export_id":%q,"format":"excel","run_id":%q}]`, period, exportID, word),
	} {
		if err := json.Unmarshal([]byte(wire), &selection); err != nil {
			t.Fatal(err)
		}
		if err := ValidateArchiveSelection(selection); err == nil {
			t.Fatalf("invalid format/run binding accepted: %s", wire)
		}
	}
}
