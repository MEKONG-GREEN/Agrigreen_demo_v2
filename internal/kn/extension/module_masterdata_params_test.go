package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMasterdataParamsMatchLegacyContract(t *testing.T) {
	got, err := json.MarshalIndent(MasterdataParams(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	_, file, _, _ := runtime.Caller(0)
	want, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../tests/golden/kn-masterdata-params.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("KN parameter metadata, default, source or effective date changed")
	}
	a := MasterdataParams()
	a.Specs[0].Name = "corrupt"
	a.Seeds[0].Key = "corrupt"
	next, _ := json.MarshalIndent(MasterdataParams(), "", "  ")
	next = append(next, '\n')
	if string(next) != string(want) {
		t.Fatal("owner registration shares mutable state")
	}
}
