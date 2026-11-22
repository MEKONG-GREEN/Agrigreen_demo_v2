package domain

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func archive205Payload(t *testing.T) (map[string]any, []uuid.UUID) {
	t.Helper()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	nodes := []map[string]any{
		{"Ref": ids[0], "Kind": "program", "FieldCode": "trong_trot"},
		{"Ref": ids[1], "Kind": "group", "ParentRef": ids[0]},
		{"Ref": ids[2], "Kind": "item", "ParentRef": ids[1]},
		{"Ref": ids[3], "Kind": "item", "ParentRef": ids[1], "FieldCode": "chan_nuoi"},
		{"Ref": ids[4], "Kind": "item", "ParentRef": ids[0], "FieldCode": "thuy_san"},
	}
	rows := []map[string]any{
		{"ref": ids[0], "kind": "program"}, {"ref": ids[1], "kind": "group", "parent_ref": ids[0]},
		{"ref": ids[2], "kind": "item", "parent_ref": ids[1], "department": "one"},
		{"ref": ids[3], "kind": "item", "parent_ref": ids[1], "department": "one"},
		{"ref": ids[4], "kind": "item", "parent_ref": ids[0], "department": "two"},
	}
	return map[string]any{"data": map[string]any{"template": "mau_5", "plan": map[string]any{"Entries": nodes}, "rows": rows}}, ids
}
func archive205Project(t *testing.T, payload any, filter Filter) ArchiveFields {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(raw)
	f, err := json.Marshal(filter)
	if err != nil {
		t.Fatal(err)
	}
	out := ProjectArchiveFields(raw, f)
	if !bytes.Equal(raw, before) {
		t.Fatal("projection mutated immutable bytes")
	}
	return out
}
func TestArchive205FrozenSelectedItemInheritance(t *testing.T) {
	payload, ids := archive205Payload(t)
	got := archive205Project(t, payload, Filter{Department: "one"})
	if got.Status != "complete" || !slices.Equal(got.Codes, []string{"chan_nuoi", "trong_trot"}) || !got.RowCountKnown || got.AllRows != 5 {
		t.Fatalf("selected metadata: %+v", got)
	}
	got = archive205Project(t, payload, Filter{Program: ids[3].String()})
	if got.Status != "complete" || !slices.Equal(got.Codes, []string{"chan_nuoi"}) {
		t.Fatalf("context headings created a fake field: %+v", got)
	}
	payload["data"].(map[string]any)["template"] = "adhoc"
	payload["adhoc"] = AdhocSelection{Rows: []string{ids[3].String()}}
	got = archive205Project(t, payload, Filter{})
	if got.Status != "complete" || !slices.Equal(got.Codes, []string{"chan_nuoi"}) {
		t.Fatalf("frozen adhoc selection ignored: %+v", got)
	}
}
func TestArchive205LegacyUnknownPartialAndUnusedNumericPoison(t *testing.T) {
	payload, _ := archive205Payload(t)
	data := payload["data"].(map[string]any)
	nodes := data["plan"].(map[string]any)["Entries"].([]map[string]any)
	nodes[0]["PlannedQty"] = json.RawMessage(`1e100000`)
	nodes[0]["Budget"] = strings.Repeat("9", 100000)
	data["metrics"] = []map[string]any{{"value": json.RawMessage(`1e-100000`)}}
	if got := archive205Project(t, payload, Filter{Department: "one"}); got.Status != "complete" {
		t.Fatal("unused numeric fields were decoded", got)
	}
	data["plan"].(map[string]any)["Entries"] = nodes[3:]
	if got := archive205Project(t, payload, Filter{Department: "one"}); got.Status != "partial" || !slices.Equal(got.Codes, []string{"chan_nuoi"}) {
		t.Fatal("missing legacy field was invented", got)
	}
	delete(data, "plan")
	if got := archive205Project(t, payload, Filter{}); got.Status != "unknown" || len(got.Codes) != 0 || !got.RowCountKnown || got.AllRows != 5 {
		t.Fatal("unknown metadata lost file/count distinction", got)
	}
	for _, raw := range []string{`{}`, `{"data":{"rows":null}}`, `{"data":{"rows":"broken"}}`} {
		if got := ProjectArchiveFields([]byte(raw), []byte(`{}`)); got.Status != "unknown" || got.RowCountKnown {
			t.Fatal("unknown count was substituted", got)
		}
	}
	if got := ProjectArchiveFields([]byte(`{"data":{"rows":[]}}`), []byte(`{}`)); !got.RowCountKnown || got.AllRows != 0 || got.Status != "unknown" {
		t.Fatal("explicit empty array count", got)
	}
}
func TestArchive205FieldSelectorNeverGrantsUnknownMetadata(t *testing.T) {
	unknown := ArchiveFields{Codes: []string{}, Status: "unknown"}
	if !(ArchiveFieldSelector{}).Matches(unknown) || (ArchiveFieldSelector{Code: "trong_trot"}).Matches(unknown) {
		t.Fatal("default lost legacy file or fabricated code")
	}
	for _, f := range []ArchiveFieldSelector{{Code: strings.Repeat("a", 41)}, {Code: "bad/code"}, {Status: "all-known-by-kind"}} {
		if f.Valid() {
			t.Fatal("invalid field selector accepted")
		}
	}
}
