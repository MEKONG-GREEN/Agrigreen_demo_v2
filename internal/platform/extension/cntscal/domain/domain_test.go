package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func ymd(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

func TestGenerateDosesAC03(t *testing.T) {
	// FN-15 AC-03: hộ nhận gà ngày 19/09, Gumboro lần 1 ở 7 ngày tuổi → ngày dự kiến tự tính đúng theo quy trình.
	plan := []PlanDose{{"VX_ND_F", 1, 3}, {"VX_GUMBORO", 1, 7}, {"VX_GUMBORO", 2, 28}}
	ds := GenerateDoses(plan, ymd("2026-09-19"), 1)
	if len(ds) != 3 {
		t.Fatalf("số mũi = %d", len(ds))
	}
	if got := ds[1].Expected.Format(time.DateOnly); ds[1].Key() != "VX_GUMBORO/1" || got != "2026-09-25" {
		t.Fatalf("Gumboro L1 = %s %s, muốn 2026-09-25 (19/09 + 7 − 1)", ds[1].Key(), got)
	}
	// Nhận lúc 10 ngày tuổi: mũi 3 và 7 ngày đã qua — không sinh.
	if ds := GenerateDoses(plan, ymd("2026-09-19"), 10); len(ds) != 1 || ds[0].Key() != "VX_GUMBORO/2" {
		t.Fatalf("bỏ mũi đã qua: %+v", ds)
	}
}

func TestDoseStatusAndUpdates(t *testing.T) {
	recv := ymd("2026-09-19")
	ds := GenerateDoses([]PlanDose{{"A", 1, 3}, {"B", 1, 30}}, recv, 0)
	today := ymd("2026-09-25")
	if StatusOf(BatchCreated, ds, today) != StatusOverdue {
		t.Fatal("mũi A quá hạn → overdue")
	}
	act := ymd("2026-09-22")
	ds2, changed, es := ApplyDoseUpdates(ds, []DoseUpdate{{Vaccine: "A", Shot: 1, Actual: &act}}, false, recv, today)
	if len(es) > 0 || !changed || StatusOf(BatchDoseRecorded, ds2, today) != StatusOpen {
		t.Fatalf("ghi ngày thực tế: %v %v %s", es, changed, StatusOf(BatchDoseRecorded, ds2, today))
	}
	exp := ymd("2026-10-30")
	if _, _, es := ApplyDoseUpdates(ds2, []DoseUpdate{{Vaccine: "B", Shot: 1, Expected: &exp}}, false, recv, today); len(es) == 0 {
		t.Fatal("Cấp Xã không sửa ngày dự kiến")
	}
	future := ymd("2026-09-30")
	if _, _, es := ApplyDoseUpdates(ds2, []DoseUpdate{{Vaccine: "B", Shot: 1, Actual: &future}}, true, recv, today); len(es) == 0 {
		t.Fatal("ngày thực tế ở tương lai phải lỗi")
	}
	if _, _, es := ApplyDoseUpdates(ds2, []DoseUpdate{{Vaccine: "X", Shot: 1, Actual: &act}}, true, recv, today); len(es) == 0 {
		t.Fatal("mũi không có phải lỗi")
	}
	done := ymd("2026-09-24")
	ds3, _, _ := ApplyDoseUpdates(ds2, []DoseUpdate{{Vaccine: "B", Shot: 1, Actual: &done}}, true, recv, today)
	if StatusOf(BatchDoseRecorded, ds3, today) != StatusDone || StatusOf(BatchCancelled, ds3, today) != StatusCancelled {
		t.Fatal("đủ mũi → done; hủy → cancelled")
	}
}

func TestNormalizeCellsAndWarnings(t *testing.T) {
	m := ymd("2026-02-01")
	f1, f2 := uuid.New(), uuid.New()
	o1, o2 := uuid.New(), uuid.New()
	cells, es := NormalizeCells(m, []Cell{
		{Facility: f1, Officer: o1, Day: 1, Shift: "N"},
		{Facility: f2, Officer: o1, Day: 1, Shift: "NĐ"}, // trùng ca với f1 → cảnh báo
		{Facility: f1, Officer: o2, Day: 2, Shift: "Đ"},
	})
	if len(es) > 0 || cells[0].Shift == "" {
		t.Fatalf("chuẩn hóa: %v", es)
	}
	if _, es := NormalizeCells(m, []Cell{{Facility: f1, Officer: o1, Day: 29, Shift: "N"}}); len(es) == 0 {
		t.Fatal("ngày 29/02/2026 không có")
	}
	if _, es := NormalizeCells(m, []Cell{{Facility: f1, Officer: o1, Day: 1, Shift: "X"}}); len(es) == 0 {
		t.Fatal("ca sai phải lỗi")
	}
	grant := func(o, f uuid.UUID, _ string) (bool, bool) { return o == o1, true }
	ws := RosterWarnings(m, cells, []Facility{{Ref: f1}, {Ref: f2}}, grant)
	codes := map[string]int{}
	for _, w := range ws {
		codes[w.Code]++
	}
	if codes[WarnUncovered] != 2 || codes[WarnDouble] != 1 || codes[WarnNotGranted] != 1 {
		t.Fatalf("cảnh báo: %+v", codes)
	}
	next := []Cell{}
	for _, c := range cells {
		if c.Officer == o2 {
			c.Shift = ShiftDay
		}
		next = append(next, c)
	}
	changed := ChangedOfficers(cells, next)
	if len(changed) != 1 || changed[0] != o2 {
		t.Fatalf("đổi ca o2: %v", changed)
	}
	if got := CopyCells([]Cell{{Day: 30}, {Day: 28}}, m); len(got) != 1 {
		t.Fatal("sao chép bỏ ngày không có")
	}
}
