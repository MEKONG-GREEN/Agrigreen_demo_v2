package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
)

func archiveMemoPayload(padding string) []byte {
	return []byte(fmt.Sprintf(`{"data":{"template":"annual","plan":{"Entries":[{"Ref":"00000000-0000-0000-0000-000000000001","Kind":"item","FieldCode":"field.tt"},{"Ref":"00000000-0000-0000-0000-000000000002","Kind":"item","FieldCode":"field.cn"}]},"rows":[{"ref":"00000000-0000-0000-0000-000000000001","kind":"item","department":"TT","level":"tinh"},{"ref":"00000000-0000-0000-0000-000000000002","kind":"item","department":"CN","level":"tinh"}]},"padding":%q}`, padding))
}

func TestArchiveProjectionMemoExactFilterAndOwnership(t *testing.T) {
	var memo archiveProjectionMemo
	payload := archiveMemoPayload("")
	original := bytes.Clone(payload)
	filter := []byte(`{"department":"TT"}`)
	tt := memo.project(payload, filter)
	if !reflect.DeepEqual(tt.Codes, []string{"field.tt"}) {
		t.Fatalf("TT frozen selection: %+v", tt)
	}
	// The same frozen payload with a different filter must never reuse TT fields.
	cn := memo.project(payload, []byte(`{"department":"CN"}`))
	if !reflect.DeepEqual(cn.Codes, []string{"field.cn"}) {
		t.Fatalf("filter key isolation: %+v", cn)
	}
	payload[0] = ' '
	filter[0] = ' '
	tt.Codes[0] = "mutated"
	again := memo.project(original, []byte(`{"department":"TT"}`))
	if !reflect.DeepEqual(again.Codes, []string{"field.tt"}) || memo.count != 2 {
		t.Fatalf("owned keys/result alias: %+v count=%d", again, memo.count)
	}
	again.Codes[0] = "mutated-again"
	if got := memo.project(original, []byte(`{"department":"TT"}`)); got.Codes[0] != "field.tt" {
		t.Fatal("cached result aliases caller")
	}
	// Separate struct fields cannot collide as concatenated or delimiter keys do.
	if (archiveProjectionKey{"ab", "c"}) == (archiveProjectionKey{"a", "bc"}) {
		t.Fatal("ambiguous key boundaries")
	}
}

func TestArchiveProjectionMemoUnknownShapeAndSemanticParity(t *testing.T) {
	var memo archiveProjectionMemo
	for _, payload := range [][]byte{[]byte(`{}`), []byte(`{"data":{"rows":null}}`), []byte(`{"data":{"rows":[]}}`)} {
		want := domain.ProjectArchiveFields(payload, []byte(`{}`))
		memo.project(payload, []byte(`{}`))
		got := memo.project(payload, []byte(`{}`))
		if !reflect.DeepEqual(got, want) || got.Codes == nil {
			t.Fatalf("unknown semantic/empty-array shape: %+v want %+v", got, want)
		}
		wire, err := json.Marshal(got)
		if err != nil || !bytes.Contains(wire, []byte(`"field_codes":[]`)) {
			t.Fatalf("unknown wire array lost: %s", wire)
		}
	}
	if cloneArchiveFields(domain.ArchiveFields{}).Codes != nil {
		t.Fatal("nil semantics changed")
	}
}

func TestArchiveProjectionMemoFIFOBudgetAndOversize(t *testing.T) {
	var memo archiveProjectionMemo
	filter := []byte(`{"department":"TT"}`)
	first := archiveMemoPayload("first")
	memo.project(first, filter)
	for i := 1; i < archiveProjectionEntries; i++ {
		memo.project(archiveMemoPayload(fmt.Sprint(i)), filter)
	}
	memo.project(first, filter) // Hits do not reorder FIFO.
	memo.project(archiveMemoPayload("newest"), filter)
	if _, ok := memo.entries[archiveProjectionKey{string(first), string(filter)}]; ok {
		t.Fatal("FIFO oldest was retained")
	}
	for i := 0; i < 20; i++ {
		payload := archiveMemoPayload(strings.Repeat("x", 100000) + fmt.Sprint(i))
		got := memo.project(payload, filter)
		if !reflect.DeepEqual(got, domain.ProjectArchiveFields(payload, filter)) {
			t.Fatal("budget eviction changed projection")
		}
		if memo.count > archiveProjectionEntries || memo.bytes+archiveProjectionFixedBytes > archiveProjectionBytes {
			t.Fatal("memo exceeds retained limits")
		}
	}
	beforeCount, beforeBytes := memo.count, memo.bytes
	oversized := archiveMemoPayload(strings.Repeat("z", archiveProjectionBytes))
	got := memo.project(oversized, filter)
	if !reflect.DeepEqual(got, domain.ProjectArchiveFields(oversized, filter)) || memo.count != beforeCount || memo.bytes != beforeBytes {
		t.Fatal("oversize must project without retention")
	}
}
