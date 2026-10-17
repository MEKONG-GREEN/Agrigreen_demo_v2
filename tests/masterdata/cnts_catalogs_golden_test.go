package masterdata_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

const lookupContractCases = 17

type lookupSnapshot struct {
	Value            any
	Error            string
	NotEffective     bool
	FacilityNotFound bool
	ItemNotFound     bool
}

type lookupContract struct {
	t       *testing.T
	replace *strings.Replacer
	cases   map[string]lookupSnapshot
	seen    map[string]bool
}

func readLookupContract(t *testing.T, path string) map[string]lookupSnapshot {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases map[string]lookupSnapshot
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != lookupContractCases {
		t.Fatalf("lookup contract must contain %d cases, got %d", lookupContractCases, len(cases))
	}
	return cases
}

func newLookupContract(t *testing.T, known, missing string) *lookupContract {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	c := &lookupContract{
		t:       t,
		replace: strings.NewReplacer(known, "00000000-0000-7000-8000-000000000001", missing, "00000000-0000-7000-8000-000000000002"),
		cases:   readLookupContract(t, filepath.Join(filepath.Dir(file), "../golden/kn-masterdata-lookups.json")),
		seen:    map[string]bool{},
	}
	t.Cleanup(func() {
		if t.Failed() {
			return
		}
		if len(c.cases) != lookupContractCases || len(c.seen) != lookupContractCases {
			t.Errorf("lookup cases lost: expected=%d observed=%d", len(c.cases), len(c.seen))
			return
		}

	})
	return c
}

func (c *lookupContract) snapshot(value any, err error) lookupSnapshot {
	c.t.Helper()
	raw, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		c.t.Fatal(marshalErr)
	}
	var normalized any
	if decodeErr := json.Unmarshal([]byte(c.replace.Replace(string(raw))), &normalized); decodeErr != nil {
		c.t.Fatal(decodeErr)
	}
	out := lookupSnapshot{Value: normalized, NotEffective: errors.Is(err, port.ErrNotEffective),
		FacilityNotFound: errors.Is(err, knport.ErrFacilityNotFound), ItemNotFound: errors.Is(err, port.ErrCatalogItemNotFound)}
	if err != nil {
		out.Error = c.replace.Replace(err.Error())
	}
	return out
}

func (c *lookupContract) check(label string, value any, err error) {
	c.t.Helper()
	want, ok := c.cases[label]
	if !ok {
		c.t.Fatalf("missing frozen lookup case %s", label)
	}
	if c.seen[label] {
		c.t.Fatalf("lookup case repeated: %s", label)
	}
	c.seen[label] = true
	got := c.snapshot(value, err)
	if !reflect.DeepEqual(want, got) {
		c.t.Fatalf("%s frozen lookup changed: want=%+v got=%+v", label, want, got)
	}
}
