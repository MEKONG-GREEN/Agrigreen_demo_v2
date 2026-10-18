package masterdatacatalogs_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

type catalogCase struct {
	Input  port.CatalogItem
	Output port.CatalogItem
	Error  string
}
type catalogContract struct {
	Metadata []port.CatalogMetadata
	Seeds    []port.CatalogSeed
	Cases    []catalogCase
}

func TestOwnedCatalogsMatchLegacyContract(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../golden/kn-masterdata-catalogs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract catalogContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	reg := extension.MasterdataCatalogs()
	mod := masterdata.New(nil, clock.System{}, masterdata.Deps{Catalogs: []port.CatalogRegistration{reg}, Params: []port.ParamRegistration{extension.MasterdataParams()}})
	if !reflect.DeepEqual(reg.Seeds, contract.Seeds) {
		t.Fatal("owner seed data/order changed")
	}
	var metadata []port.CatalogMetadata
	for _, m := range mod.CatalogMetadata() {
		for _, want := range contract.Metadata {
			if m.Catalog == want.Catalog {
				metadata = append(metadata, m)
			}
		}
	}
	if !reflect.DeepEqual(metadata, contract.Metadata) {
		t.Fatal("catalog name/schema/date/sync order changed")
	}
	for i, tc := range contract.Cases {
		got, err := mod.NormalizeCatalogItem(tc.Input)
		if tc.Error != "" {
			if err == nil || err.Error() != tc.Error {
				t.Fatalf("case %d error got %v want %s", i, err, tc.Error)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.Output) {
			t.Fatalf("case %d normalization got %+v %v want %+v", i, got, err, tc.Output)
		}
	}
	// Mutating caller-owned definitions and returned metadata cannot affect the module.
	reg.Definitions[0].Name = "corrupt"
	for attr := range reg.Definitions[0].Attributes {
		reg.Definitions[0].Attributes[attr] = port.AttributeSpec{}
	}
	m := mod.CatalogMetadata()
	m[0].Name = "corrupt"
	m[0].Attributes = nil
	got, err := mod.NormalizeCatalogItem(contract.Cases[0].Input)
	if err != nil || !reflect.DeepEqual(got, contract.Cases[0].Output) {
		t.Fatal("definition mutation reached capability")
	}
	next := extension.MasterdataCatalogs()
	if !reflect.DeepEqual(next.Seeds, contract.Seeds) {
		t.Fatal("owner factory shares state")
	}
	t.Logf("%d schemas, %d seed rows, %d normalization/error cases", len(metadata), len(contract.Seeds), len(contract.Cases))
}
