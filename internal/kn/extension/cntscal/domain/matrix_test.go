package domain

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

// AC-03 của #168: hộ nhận gà 19/09, Gumboro L1 lúc 7 ngày tuổi, tuổi lúc nhận 1 ngày → dự kiến 25/09 (khớp tệp mẫu BM-07).
func TestGenerateDosesBM07Sample(t *testing.T) {
	ds := GenerateDoses([]PlanDose{{Vaccine: "VX_GUMBORO", Shot: 1, AgeDays: 7}, {Vaccine: "VX_ND_F", Shot: 1, AgeDays: 3}}, d("2026-09-19"), 1)
	if len(ds) != 2 || ds[1].Vaccine != "VX_GUMBORO" || !ds[1].Expected.Equal(d("2026-09-25")) || !ds[0].Expected.Equal(d("2026-09-21")) {
		t.Fatalf("ngày dự kiến: %+v", ds)
	}
}

func TestMatrixColumnsAndAlign(t *testing.T) {
	a := []Dose{{Vaccine: "VX_IB", Shot: 1, AgeDays: 5}, {Vaccine: "VX_ND", Shot: 1, AgeDays: 3}, {Vaccine: "VX_IB", Shot: 2, AgeDays: 35}}
	b := []Dose{{Vaccine: "VX_IB", Shot: 2, AgeDays: 35}, {Vaccine: "VX_CUM", Shot: 1, AgeDays: 21}}
	cols := MatrixColumns([][]Dose{a, b})
	var keys []string
	for _, c := range cols {
		keys = append(keys, c.Key())
	}
	want := []string{"VX_ND/1", "VX_IB/1", "VX_IB/2", "VX_CUM/1"}
	if len(keys) != len(want) {
		t.Fatalf("cột: %v", keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("thứ tự cột: %v", keys)
		}
	}
	row := Align(cols, b)
	if row[0] != nil || row[1] != nil || row[2] == nil || row[2].Shot != 2 || row[3] == nil {
		t.Fatalf("căn hàng: %+v", row)
	}
}

func TestMatchDiaryDoses(t *testing.T) {
	recv, today := d("2027-10-01"), d("2027-10-13")
	done := d("2027-10-03")
	cur := []Dose{
		{Vaccine: "VX_ND_F", Shot: 1, Expected: d("2027-10-03"), Actual: &done},
		{Vaccine: "VX_GUMBORO", Shot: 1, Expected: d("2027-10-07")},
		{Vaccine: "VX_GUMBORO", Shot: 2, Expected: d("2027-10-28")},
		{Vaccine: "VX_IB", Shot: 1, Expected: d("2027-10-06")},
	}
	names := map[string]string{"VX_GUMBORO": "Gumboro", "VX_ND_F": "Newcastle F", "VX_IB": "Viêm phế quản truyền nhiễm (IB)"}
	ups := MatchDiaryDoses(cur, []DiaryDose{
		{Vaccine: "  gumboro ", Date: d("2027-10-08")},                  // tên danh mục, không ghi lần → mũi chưa tiêm sớm nhất
		{Vaccine: "Gumboro", Date: d("2027-10-09")},                     // lần 0 lần nữa → L2 (L1 đã dùng)
		{Vaccine: "Newcastle F", Shot: 1, Date: d("2027-10-04")},        // đã có ngày thực tế → không ghi đè
		{Vaccine: "vx_ib", Shot: 1, Date: d("2027-10-20")},              // ngày ở tương lai → bỏ
		{Vaccine: "Vaccine lạ", Date: d("2027-10-05")},                  // không khớp → bỏ
		{Vaccine: "VX_IB", Shot: 1, Date: d("2027-09-30")},              // trước ngày nhận giống → bỏ
		{Vaccine: "Viêm phế quản truyền nhiễm (IB)", Date: time.Time{}}, // thiếu ngày → bỏ
	}, names, recv, today)
	if len(ups) != 2 || ups[0].Vaccine != "VX_GUMBORO" || ups[0].Shot != 1 || !ups[0].Actual.Equal(d("2027-10-08")) ||
		ups[1].Shot != 2 || !ups[1].Actual.Equal(d("2027-10-09")) {
		t.Fatalf("khớp NC-11: %+v", ups)
	}
	out, changed, es := ApplyDoseUpdates(cur, ups, false, recv, today)
	if len(es) != 0 || !changed || out[2].Key() != "VX_GUMBORO/1" || out[2].Actual == nil || out[1].Actual != nil {
		t.Fatalf("áp NC-11: %v %v %+v", changed, es, out)
	}
	big := make([]DiaryDose, MaxDiaryDoses+10)
	if len(MatchDiaryDoses(cur, big, names, recv, today)) != 0 {
		t.Fatal("dòng rỗng không khớp")
	}
}
