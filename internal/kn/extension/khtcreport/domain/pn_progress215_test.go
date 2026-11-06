package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func pn215Payload(t *testing.T, change func(map[string]any)) []byte {
	t.Helper()
	p, own, foreign := uuid.New(), uuid.New(), uuid.New()
	data := map[string]any{"template": "mau_5", "period": Period{ID: uuid.New(), Type: "program", Year: 2026}, "captured_at": time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), "input_hash": strings.Repeat("a", 64), "rows": []map[string]any{
		{"ref": p, "kind": "program", "name": "Chương trình chung", "planned": "999999", "year_to_date": "888888", "budget": "foreign aggregate"},
		{"ref": own, "parent_ref": p, "kind": "item", "name": "Lúa", "department": "trong_trot", "unit": "ha", "planned": "100", "year_to_date": "25.125", "percent": "25.13", "level": "progressing"},
		{"ref": foreign, "parent_ref": p, "kind": "item", "name": "FOREIGN ROW", "department": "thong_tin_truyen_thong", "planned": "10", "year_to_date": "secret", "percent": "secret"},
	}, "plan": map[string]any{"Entries": []map[string]any{{"PlannedQty": "1e100000"}}}, "form_texts": map[string]any{"foreign": "private narrative"}}
	if change != nil {
		change(data)
	}
	raw, err := json.Marshal(map[string]any{"data": data, "texts": map[string]string{"I": "private narrative"}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPN215ProjectionOwnItemsAndAncestorLabels(t *testing.T) {
	raw := pn215Payload(t, nil)
	p, err := ProjectPNProgress215(raw, "trong_trot", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 2 || len(p.Programs) != 1 || p.Rows[1].YearToDate != "25.125" || p.Rows[1].Percent == nil || *p.Rows[1].Percent != "25.13" {
		t.Fatalf("own exact M5 rows missing: %+v", p)
	}
	if p.Rows[0].Planned != nil || p.Rows[0].YearToDate != "" || p.Rows[0].Percent != nil || p.Rows[0].Department != "" {
		t.Fatalf("ancestor retained aggregate/department: %+v", p.Rows[0])
	}
	visible, _ := json.Marshal(struct {
		Rows     []PNProgressRow215
		Programs []PNProgram215
	}{p.Rows, p.Programs})
	for _, forbidden := range []string{"FOREIGN ROW", "secret", "999999", "888888", "foreign aggregate", "private narrative", "PlannedQty"} {
		if strings.Contains(string(visible), forbidden) {
			t.Fatalf("foreign data in projection: %s", visible)
		}
	}
	filtered, err := ProjectPNProgress215(raw, "trong_trot", Filter{Program: p.Programs[0].Ref.String(), Level: "progressing"})
	if err != nil || len(filtered.Rows) != 2 {
		t.Fatalf("own programme filter: %+v %v", filtered, err)
	}
	_, err = ProjectPNProgress215(raw, "trong_trot", Filter{Program: uuid.NewString()})
	if !errors.Is(err, ErrPNProgramScope215) {
		t.Fatalf("unknown/foreign programme must not grant access: %v", err)
	}
}

func TestPN215NumericColumnsBoundBeforeAnyParser(t *testing.T) {
	for _, column := range []string{"planned", "year_to_date", "percent"} {
		for _, bad := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "=HYPERLINK(1)"} {
			t.Run(column+"/"+bad[:min(len(bad), 16)], func(t *testing.T) {
				raw := pn215Payload(t, func(d map[string]any) { d["rows"].([]map[string]any)[1][column] = bad })
				if _, err := ProjectPNProgress215(raw, "trong_trot", Filter{}); err == nil {
					t.Fatal("hostile frozen number accepted")
				}
			})
		}
	}
	if _, err := ProjectPNProgress215(pn215Payload(t, nil), "trong_trot", Filter{}); err != nil {
		t.Fatal("unrelated legacy decimal object was deserialized", err)
	}
}

func TestPN215MissingAndMalformedSourceStayExplicit(t *testing.T) {
	raw := pn215Payload(t, func(d map[string]any) {
		row := d["rows"].([]map[string]any)[1]
		row["missing"] = true
		row["percent"] = nil
	})
	p, err := ProjectPNProgress215(raw, "trong_trot", Filter{})
	if err != nil || !p.Rows[1].Missing || p.Rows[1].Percent != nil {
		t.Fatalf("missing source invented percent: %+v %v", p, err)
	}
	for _, change := range []func(map[string]any){func(d map[string]any) { d["template"] = "mau_1" }, func(d map[string]any) { d["captured_at"] = time.Time{} }, func(d map[string]any) { rows := d["rows"].([]map[string]any); rows[1]["ref"] = rows[0]["ref"] }} {
		if _, err := ProjectPNProgress215(pn215Payload(t, change), "trong_trot", Filter{}); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
}
