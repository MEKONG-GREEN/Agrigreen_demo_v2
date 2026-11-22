package domain

import (
	"github.com/google/uuid"

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

// Codex #168 [P2]: cùng một dòng NC-11 qua các phiên bản của lần ghi (điều chỉnh, gửi lại) không thành mũi mới.
func TestMatchDiaryDosesOwnedStable(t *testing.T) {
	recv, today := d("2027-10-01"), d("2027-10-20")
	l1 := d("2027-10-08")
	cur := []Dose{
		{Vaccine: "VX_GUMBORO", Shot: 1, Expected: d("2027-10-07"), Actual: &l1}, // phiên bản trước của lần ghi đã điền
		{Vaccine: "VX_GUMBORO", Shot: 2, Expected: d("2027-10-21")},
	}
	owned := map[string]bool{"VX_GUMBORO/1": true}
	same := []DiaryDose{{Vaccine: "VX_GUMBORO", Date: d("2027-10-08")}}
	if ups := MatchDiaryDosesOwned(cur, same, nil, recv, today, owned); len(ups) != 0 {
		t.Fatalf("gửi lại cùng dòng (lần 0) điền mũi mới: %+v", ups)
	}
	if ups := MatchDiaryDosesOwned(cur, same, nil, recv, today, nil); len(ups) != 0 {
		t.Fatalf("mũi đã có đúng ngày (ghi tay) — dòng lần 0 cùng ngày không điền L2: %+v", ups)
	}
	fixed := []DiaryDose{{Vaccine: "VX_GUMBORO", Date: d("2027-10-09")}} // điều chỉnh ngày của chính dòng đó
	if ups := MatchDiaryDosesOwned(cur, fixed, nil, recv, today, owned); len(ups) != 0 {
		t.Fatalf("dòng đổi ngày không được điền mũi mới (không ghi đè L1): %+v", ups)
	}
	// Dòng mới thêm ở phiên bản sau (thứ tự bất kỳ) vẫn điền L2; dòng cũ giữ L1.
	more := []DiaryDose{{Vaccine: "VX_GUMBORO", Date: d("2027-10-19")}, {Vaccine: "VX_GUMBORO", Date: d("2027-10-08")}}
	ups := MatchDiaryDosesOwned(cur, more, nil, recv, today, owned)
	if len(ups) != 1 || ups[0].Shot != 2 || !ups[0].Actual.Equal(d("2027-10-19")) {
		t.Fatalf("dòng mới → L2: %+v", ups)
	}
	if f := FilledBy([]Dose{cur[1]}, []Dose{{Vaccine: "VX_GUMBORO", Shot: 2, Actual: &l1}}); !f["VX_GUMBORO/2"] || len(f) != 1 {
		t.Fatalf("FilledBy: %v", f)
	}
}

// Codex #168 [P1]: NC-11 chỉ ghi lịch của hộ của lần ghi, hoặc lịch địa bàn cùng xã/loại mô hình trong phạm vi người ghi.
func TestDiaryRecordCovers(t *testing.T) {
	h, other := uuid.New(), uuid.New()
	r := DiaryRecord{Household: h, Commune: "30001", ModelType: "GA_TP", Scope: []string{"30001"}}
	for _, tc := range []struct {
		commune   string
		household *uuid.UUID
		model     string
		want      bool
	}{
		{"30001", &h, "GA_TP", true},
		{"30001", &other, "GA_TP", false}, // hộ khác
		{"30001", nil, "GA_TP", true},     // lịch địa bàn cùng xã, loại mô hình
		{"30002", nil, "GA_TP", false},    // lịch địa bàn xã khác (ngoài phạm vi)
		{"30001", nil, "CA", false},       // khác loại mô hình
		{"30002", &h, "GA_TP", false},     // xã ngoài phạm vi
	} {
		if got := r.Covers(tc.commune, tc.household, tc.model); got != tc.want {
			t.Fatalf("Covers(%s, %v, %s) = %v", tc.commune, tc.household, tc.model, got)
		}
	}
	admin := DiaryRecord{Household: h, Commune: "30002", ModelType: "GA_TP", ScopeAll: true}
	if !admin.Covers("30002", nil, "GA_TP") || admin.Covers("30001", nil, "GA_TP") {
		t.Fatal("Admin: vẫn chỉ lịch địa bàn cùng xã của hộ")
	}
}
