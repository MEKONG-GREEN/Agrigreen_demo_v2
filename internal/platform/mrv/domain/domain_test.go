package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func dec(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

func intp(n int) *int { return &n }

func catalog() CatalogVersion {
	return CatalogVersion{EvidenceType: "awd_log", Version: 3, SourceNote: "thử", Fields: []FieldDef{
		{Key: KeyPhoto, LabelVi: "Ảnh", Type: TypePhoto, Required: true, Min: dec("1"), Max: dec("2")},
		{Key: KeyGPS, LabelVi: "Toạ độ", Type: TypeGeoPoint, Required: true},
		{Key: "level", LabelVi: "Mực nước", Type: TypeDecimal, Unit: "cm", Required: true, Min: dec("-30"), Max: dec("30"), MaxScale: intp(1)},
		{Key: "days", LabelVi: "Số ngày", Type: TypeInteger, Min: dec("0"), Max: dec("60")},
		{Key: "flag", LabelVi: "Cờ", Type: TypeBoolean},
		{Key: "stage", LabelVi: "Giai đoạn", Type: TypeEnum, EnumValues: []string{"ma", "chin"}},
		{Key: "on", LabelVi: "Ngày", Type: TypeDate},
		{Key: "note", LabelVi: "Ghi chú", Type: TypeText, Max: dec("5")},
		{Key: "closeup", LabelVi: "Ảnh cận", Type: TypePhoto, Max: dec("1")},
	}}
}

func subject(vals map[string]string) Subject {
	f := ids.New()
	s := Subject{Values: map[string]json.RawMessage{}, GPS: &GeoPoint{Lat: decimal.NewFromInt(10), Lon: decimal.NewFromInt(105)},
		FileRefs: []FileRef{{FileRef: f, SHA256: strings.Repeat("0", 64)}}}
	for k, v := range vals {
		s.Values[k] = json.RawMessage(strings.ReplaceAll(v, "$F", f.String()))
	}
	return s
}

func fields(t *testing.T, c CatalogVersion, s Subject) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, e := range c.ValidateRecord(s) {
		out[e.Field] = true
	}
	return out
}

func TestValidateRecordTypes(t *testing.T) {
	c := catalog()
	ok := subject(map[string]string{"level": `"-12.5"`, "days": `5`, "flag": `true`, "stage": `"ma"`, "on": `"2026-09-01"`,
		"note": `"abc"`, "closeup": `["$F"]`})
	if errs := c.ValidateRecord(ok); len(errs) != 0 {
		t.Fatalf("hợp lệ: %v", errs)
	}
	cases := map[string]string{
		"level":   `12.5`,  // số JSON (float) → cấm, R-10
		"days":    `"2.5"`, // không nguyên
		"flag":    `"yes"`,
		"stage":   `"lam_dong"`,
		"on":      `"01/09/2026"`,
		"note":    `"quá dài"`,
		"closeup": `["` + uuid.NewString() + `"]`, // file_ref không thuộc bản ghi
	}
	for k, v := range cases {
		s := subject(map[string]string{"level": `"1"`, k: v})
		if !fields(t, c, s)["values."+k] {
			t.Errorf("%s=%s phải lỗi", k, v)
		}
	}
	for _, v := range []string{`"30.1"`, `"-31"`, `"1.25"`, `"1e1"`, `"0x10"`} {
		if !fields(t, c, subject(map[string]string{"level": v}))["values.level"] {
			t.Errorf("level=%s phải lỗi", v)
		}
	}
}

func TestValidateRecordRequiredAndReserved(t *testing.T) {
	c := catalog()
	s := subject(map[string]string{"mystery": `"x"`, "gps": `{}`})
	s.GPS, s.FileRefs = nil, nil
	got := fields(t, c, s)
	for _, f := range []string{"values.level", "gps", "file_refs", "values.mystery", "values.gps"} {
		if !got[f] {
			t.Errorf("thiếu lỗi %s: %v", f, got)
		}
	}
	// Ảnh vượt tối đa.
	s = subject(map[string]string{"level": `"1"`})
	s.FileRefs = append(s.FileRefs, FileRef{FileRef: ids.New(), SHA256: strings.Repeat("1", 64)}, FileRef{FileRef: ids.New(), SHA256: strings.Repeat("2", 64)})
	if !fields(t, c, s)["file_refs"] {
		t.Error("3 ảnh > tối đa 2 phải lỗi")
	}
	// null coi như vắng mặt.
	if !fields(t, c, subject(map[string]string{"level": `null`}))["values.level"] {
		t.Error("null của trường bắt buộc phải lỗi")
	}
}

func TestValidateDefinition(t *testing.T) {
	if errs := catalog().ValidateDefinition(); len(errs) != 0 {
		t.Fatalf("danh mục mẫu hợp lệ: %v", errs)
	}
	bad := CatalogVersion{EvidenceType: "A", SourceNote: "", Fields: []FieldDef{
		{Key: "gps", LabelVi: "x", Type: TypeText},
		{Key: "where", LabelVi: "x", Type: TypeGeoPoint},
		{Key: "n", LabelVi: "x", Type: TypeInteger},
		{Key: "u", LabelVi: "x", Type: TypeBoolean, Unit: "cm"},
		{Key: "t", LabelVi: "x", Type: TypeText, Min: dec("1.5")},
		{Key: "t", LabelVi: "", Type: "float"},
	}}
	got := map[string]bool{}
	for _, e := range bad.ValidateDefinition() {
		got[e.Field] = true
	}
	for _, f := range []string{"evidence_type", "source_note", "fields[0].data_type", "fields[1].data_type", "fields[2].key",
		"fields[3].unit", "fields[4].min", "fields[5].key", "fields[5].label_vi", "fields[5].data_type"} {
		if !got[f] {
			t.Errorf("thiếu lỗi %s: %v", f, got)
		}
	}
}

func TestEnvelope(t *testing.T) {
	now := time.Date(2026, 12, 3, 0, 0, 0, 0, time.UTC)
	reason := "ngắn"
	e := Evidence{ID: uuid.New(), EvidenceType: "awd_log", AdminUnitCode: "3000a", EventTime: now.Add(time.Hour),
		CorrectionReason: &reason, GPS: &GeoPoint{Lat: decimal.NewFromInt(91), Lon: decimal.Zero},
		FileRefs: []FileRef{{FileRef: ids.New(), SHA256: "abc"}}}
	got := map[string]bool{}
	for _, f := range e.ValidateEnvelope(now) {
		got[f.Field] = true
		if strings.Contains(f.Reason, "91") {
			t.Error("không được lộ toạ độ trong lỗi (R-14)")
		}
	}
	for _, f := range []string{"id", "admin_unit_code", "event_time", "gps", "file_refs", "correction_reason"} {
		if !got[f] {
			t.Errorf("thiếu lỗi %s", f)
		}
	}
}

// Ảnh chụp chuẩn hóa không phụ thuộc thứ tự dòng hay thứ tự khóa trong values.
func TestSnapshotCanonicalIsStable(t *testing.T) {
	a := SnapshotEvidence{ID: "2", RootID: "2", Values: json.RawMessage(`{"b":"1","a":2}`), FileRefs: []FileRef{}}
	b := SnapshotEvidence{ID: "1", RootID: "1", Values: json.RawMessage(`{"z":true}`), FileRefs: []FileRef{}}
	s1 := Snapshot{Evidence: []SnapshotEvidence{a, b}}
	a.Values = json.RawMessage(`{ "a": 2, "b": "1" }`)
	s2 := Snapshot{Evidence: []SnapshotEvidence{b, a}}
	c1, h1, err := s1.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	c2, h2, _ := s2.Canonical()
	if h1 != h2 || string(c1) != string(c2) {
		t.Fatalf("băm phải ổn định:\n%s\n%s", c1, c2)
	}
	if !strings.Contains(string(c1), `"values":{"a":2,"b":"1"}`) {
		t.Fatalf("khóa phải sắp xếp, số giữ nguyên: %s", c1)
	}
	job := ExportJob{ID: ids.New(), TenantID: ids.New(), RequestedBy: ids.New(), RequestedAt: time.Now()}
	p1, sum1, _ := BuildPackage(job, s1)
	p2, sum2, _ := BuildPackage(job, s2)
	if string(p1) != string(p2) || sum1 != h1 || sum2 != h1 {
		t.Fatal("gói dựng lại phải trùng từng byte")
	}
}
