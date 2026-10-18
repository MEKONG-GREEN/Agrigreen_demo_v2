package domain

import (
	"testing"

	"github.com/google/uuid"
)

func pv(code, au, status, from string, seq int64) Ver[Point] {
	return Ver[Point]{Key: code, Seq: seq, ValidFrom: d(from), Status: status,
		V: Point{Code: code, AdminUnitCode: au, Name: "Xã " + au, RegionCode: "an_giang", Status: status, ValidFrom: d(from), Note: "x"}}
}

func TestPointStates(t *testing.T) {
	vs := []Ver[Point]{pv("XBG0001", "A1", StatusActive, "2026-10-12", 1), pv("XBG0001", "A1", StatusRetired, "2026-10-25", 2),
		pv("XBG0002", "A2", StatusActive, "2026-10-14", 1)}
	st := PointStates(vs, d("2026-10-13"))
	if st["XBG0001"].State != PointStateActive || !st["XBG0001"].ValidTo.Equal(d("2026-10-24")) || st["XBG0002"].State != PointStatePending {
		t.Fatalf("13/10: %+v", st)
	}
	st = PointStates(vs, d("2026-10-25"))
	if st["XBG0001"].State != PointStateRetired || !st["XBG0001"].Since.Equal(d("2026-10-12")) {
		t.Fatalf("25/10: %+v", st)
	}
}

func TestAdminUnitClash(t *testing.T) {
	vs := []Ver[Point]{pv("XBG0001", "A1", StatusActive, "2026-10-12", 1), pv("XBG0001", "A1", StatusRetired, "2026-10-25", 2),
		pv("XBG0002", "A2", StatusActive, "2026-11-01", 1)}
	cases := []struct {
		au, self, from, want string
	}{
		{"A1", "", "2026-10-20", "XBG0001"},
		{"A1", "", "2026-10-25", ""}, // xã cũ đã ngưng
		{"A1", "XBG0001", "2026-10-20", ""},
		{"A2", "", "2026-10-20", "XBG0002"}, // xã sắp hoạt động cũng chặn
		{"A3", "", "2026-10-20", ""},
	}
	for _, c := range cases {
		if got := AdminUnitClash(vs, c.au, c.self, d(c.from)); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

func TestOfficerAt(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	vs := []Ver[OfficerAssign]{
		{Key: OfficerKey(OfficerCommune, "XBG0001"), Seq: 1, ValidFrom: d("2026-10-01"), Status: StatusActive,
			V: OfficerAssign{PointCode: "XBG0001", Role: OfficerCommune, OfficerRef: a}},
		{Key: OfficerKey(OfficerCommune, "XBG0001"), Seq: 2, ValidFrom: d("2026-10-05"), Status: StatusActive,
			V: OfficerAssign{PointCode: "XBG0001", Role: OfficerCommune, OfficerRef: b}},
		{Key: OfficerKey(OfficerCommune, "XBG0001"), Seq: 3, ValidFrom: d("2026-10-20"), Status: StatusActive,
			V: OfficerAssign{PointCode: "XBG0001", Role: OfficerCommune}},
	}
	// AC-02 FN-AD-04.01: A đến 04/10, B từ 05/10; bỏ phân công từ 20/10.
	for at, want := range map[string]uuid.UUID{"2026-09-30": uuid.Nil, "2026-10-04": a, "2026-10-05": b, "2026-10-20": uuid.Nil} {
		if got, _ := OfficerAt(vs, OfficerCommune, "XBG0001", d(at)); got != want {
			t.Errorf("%s: %v, muốn %v", at, got, want)
		}
	}
	states := map[string]PointState{"XBG0001": {State: PointStateActive}}
	if ps := OfficerPoints(vs, states, OfficerCommune, b, d("2026-10-06")); len(ps) != 1 || ps[0] != "XBG0001" {
		t.Fatalf("OfficerPoints: %v", ps)
	}
	if ps := OfficerPoints(vs, states, OfficerProvince, b, d("2026-10-06")); len(ps) != 0 {
		t.Fatalf("OfficerPoints vai tỉnh: %v", ps)
	}
}

func TestValidatePoint(t *testing.T) {
	ok := Point{Code: PointCode(1), AdminUnitCode: "30280", Name: "Xã Mỹ Chánh", RegionCode: "an_giang", Status: StatusActive, ValidFrom: d("2026-10-12")}
	if err := ValidatePoint(ok); err != nil || ok.Code != "XBG0001" {
		t.Fatal(err)
	}
	bad := ok
	bad.Status, bad.Note = StatusRetired, ""
	if ValidatePoint(bad) == nil {
		t.Fatal("ngưng thiếu lý do")
	}
	bad = ok
	bad.Code = "X1"
	if ValidatePoint(bad) == nil {
		t.Fatal("mã sai dạng")
	}
}
