package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func d(s string) time.Time { t, _ := ParseDate(s); return t }

func ref() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func TestRoman(t *testing.T) {
	for n, want := range map[int]string{1: "I", 4: "IV", 9: "IX", 13: "XIII", 40: "XL", 1999: "MCMXCIX", 0: ""} {
		if got := Roman(n); got != want {
			t.Errorf("Roman(%d) = %q, muốn %q", n, got, want)
		}
	}
}

// BR-DL-13: phiên bản dùng cho ngày at = ngày hiệu lực ≤ at, gần nhất; cùng ngày → phiên bản sau.
func TestBR_DL_13_PickAt(t *testing.T) {
	vs := []VersionHead{{1, d("2027-01-01")}, {2, d("2027-07-01")}, {3, d("2027-04-01")}, {4, d("2027-07-01")}}
	cases := map[string]int{"2026-12-31": 0, "2027-01-01": 1, "2027-05-10": 3, "2027-07-01": 4, "2027-12-31": 4}
	for at, want := range cases {
		if got := PickAt(vs, d(at)); got != want {
			t.Errorf("PickAt(%s) = %d, muốn %d", at, got, want)
		}
	}
}

func tree() []Entry {
	p1, g1, i1, p2 := ref(), ref(), ref(), ref()
	prog1, prog2 := ref(), ref()
	q := decimal.RequireFromString("12.5")
	return []Entry{
		{Ref: p1, Tab: TabPrograms, Kind: EntryProgram, ProgramRef: &prog1, Name: "CT KN trồng trọt", SortOrder: 2},
		{Ref: g1, Tab: TabPrograms, Kind: EntryGroup, ParentRef: &p1, Name: "Mô hình"},
		{Ref: i1, Tab: TabPrograms, Kind: EntryItem, ParentRef: &g1, Name: "Mô hình lúa", Department: "trong_trot", PlannedQty: &q, UnitCode: "ha"},
		{Ref: p2, Tab: TabPrograms, Kind: EntryProgram, ProgramRef: &prog2, Name: "CT KN chăn nuôi", SortOrder: 1},
	}
}

// FN-TI-02.02: cây CT → nhóm → hạng mục; hạng mục có đúng một phòng phụ trách; con nhận CT/DA của tổ tiên.
func TestFN_TI_02_02_NormalizePlan(t *testing.T) {
	es := tree()
	if errs := NormalizePlan(2027, es, Refs{}); len(errs) > 0 {
		t.Fatalf("cây hợp lệ bị từ chối: %v", errs)
	}
	if es[2].ProgramRef == nil || *es[2].ProgramRef != *es[0].ProgramRef || es[2].DataSource != SourceForm || es[2].Tracking != TrackingOn {
		t.Fatalf("hạng mục chưa nhận CT/DA, nguồn mặc định: %+v", es[2])
	}
	roman := RomanNumbers(es, nil)
	if roman[es[3].Ref] != "I" || roman[es[0].Ref] != "II" {
		t.Fatalf("số La Mã theo thứ tự: %v", roman)
	}

	bad := tree()
	bad[2].Department = ""
	bad[1].ParentRef = &bad[2].Ref // nhóm dưới hạng mục đứng sau
	bad[3].Tracking = TrackingStopped
	neg := decimal.RequireFromString("-1")
	bad[0].PlannedQty = &neg
	errs := NormalizePlan(2027, bad, Refs{})
	want := map[string]bool{"entries.2.department_code": false, "entries.1.parent_ref": false, "entries.3.stopped_from": false, "entries.0.planned_qty": false}
	for _, e := range errs {
		if _, ok := want[e.Field]; ok {
			want[e.Field] = true
		}
	}
	for f, seen := range want {
		if !seen {
			t.Errorf("thiếu lỗi %s: %v", f, errs)
		}
	}

	ref := tree()
	errs = NormalizePlan(2027, ref, Refs{Programs: map[uuid.UUID]bool{}, Units: map[string]bool{}})
	if len(errs) != 3 { // 2 CT/DA không có trong danh sách chủ + ĐVT không có
		t.Fatalf("tham chiếu danh mục: %v", errs)
	}
}

// BR-DL-21: CT ngừng từ ngày d không còn số La Mã ở kỳ có ngày ≥ d.
func TestBR_DL_21_StoppedProgram(t *testing.T) {
	es := tree()
	stop := d("2027-07-01")
	es[3].Tracking, es[3].StoppedFrom = TrackingStopped, &stop
	if errs := NormalizePlan(2027, es, Refs{}); len(errs) > 0 {
		t.Fatal(errs)
	}
	before, after := d("2027-06-30"), d("2027-07-01")
	if r := RomanNumbers(es, &before); r[es[3].Ref] != "I" {
		t.Fatalf("trước ngày ngừng: %v", r)
	}
	if r := RomanNumbers(es, &after); r[es[3].Ref] != "" || r[es[0].Ref] != "I" {
		t.Fatalf("sau ngày ngừng: %v", r)
	}
}

// Sao chép từ năm trước: chỉ nội dung còn theo dõi, định danh dòng mới, cha được ánh xạ.
func TestCopyForYear(t *testing.T) {
	es := tree()
	stop := d("2026-10-01")
	es[1].Tracking, es[1].StoppedFrom = TrackingStopped, &stop // nhóm ngừng → bỏ cả hạng mục con
	out := CopyForYear(es, ref)
	if len(out) != 2 || out[0].Ref == es[0].Ref || *out[0].ProgramRef != *es[0].ProgramRef || out[1].Ref == es[3].Ref {
		t.Fatalf("bản sao: %+v", out)
	}
}

func TestEntriesHashStable(t *testing.T) {
	es := tree()
	a, b := EntriesHash(es), EntriesHash(es)
	if string(a) != string(b) || len(a) != 32 {
		t.Fatal("băm không ổn định")
	}
	es[2].Name = "Khác"
	if string(EntriesHash(es)) == string(a) {
		t.Fatal("băm không đổi khi nội dung đổi")
	}
}

// FN-AD-04.01: mặc định cho mẫu chỉ với người ký/nơi nhận/căn cứ; người ký bắt buộc chức vụ; mặt hàng bắt buộc ĐVT.
func TestFN_AD_04_01_NormalizeCatalog(t *testing.T) {
	ok := CatalogItem{Kind: KindSigner, Code: " GD ", Name: "Nguyễn Văn A", DefaultFor: []string{"mau_2", "mau_1", "mau_1"},
		Attrs: map[string]string{"title": "Giám đốc"}}
	if es := NormalizeCatalog(&ok); len(es) > 0 || ok.Code != "gd" || len(ok.DefaultFor) != 2 || ok.DefaultFor[0] != "mau_1" {
		t.Fatalf("%v %+v", es, ok)
	}
	bad := CatalogItem{Kind: KindUnit, Code: "ha", Name: "Héc ta", DefaultFor: []string{"mau_1"}}
	if es := NormalizeCatalog(&bad); len(es) != 1 {
		t.Fatalf("ĐVT có mặc định cho mẫu: %v", es)
	}
	pi := CatalogItem{Kind: KindPriceItem, Code: "lua", Name: "Lúa"}
	if es := NormalizeCatalog(&pi); len(es) != 1 || es[0].Field != "attrs.unit" {
		t.Fatalf("mặt hàng thiếu ĐVT: %v", es)
	}
}
