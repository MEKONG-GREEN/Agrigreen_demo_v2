package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestParseSourceKey_W1164(t *testing.T) {
	for _, c := range []struct{ raw, mode, dept, form, cell string }{
		{"chan_nuoi_thuy_san:bm-04:san_luong_luy_ke", ModeCumulative, "chan_nuoi_thuy_san", "bm-04", "san_luong_luy_ke"},
		{"season:trong_trot:pl01:lua_td:f", ModeSeason, "trong_trot", "pl01", "lua_td/f"},
		{"Thong_Tin_Truyen_Thong:PRICE-BOARD:MH00001", ModePrice, "thong_tin_truyen_thong", "price-board", "mh00001"},
		{"thong_tin_truyen_thong:price-tickets:tickets_collected", ModeSum, "thong_tin_truyen_thong", "price-tickets", "tickets_collected"},
		{"sum:chan_nuoi_thuy_san:bm-01:so_con", ModeSum, "chan_nuoi_thuy_san", "bm-01", "so_con"},
	} {
		k, err := ParseSourceKey(c.raw)
		if err != nil || k.Mode != c.mode || k.Department != c.dept || k.Form != c.form || k.Cell != c.cell {
			t.Errorf("%s: %+v %v", c.raw, k, err)
		}
	}
	for _, bad := range []string{"", "cnts.demo_points", "a:b", "x:bm-04:c", "price:trong_trot:pl01:f", "sum:thong_tin_truyen_thong:price-board:mh1",
		"thong_tin_truyen_thong:price-board:mh1:price_min", "chan_nuoi_thuy_san:bm-04:a:b:c", "chan_nuoi_thuy_san:bm-04:a b"} {
		if _, err := ParseSourceKey(bad); err == nil {
			t.Errorf("%q phải lỗi", bad)
		}
	}
}

func str(s string) *string { return &s }

// "Nguồn đã thay đổi" chỉ theo figures_hash và tập kỳ nguồn; status_hash (nhận HTX — D2) không cảnh báo.
func TestChanges_FiguresHashOnly_W1164(t *testing.T) {
	d := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	v := 3
	row := func(key, fh, sh string) Row {
		return Row{TargetKey: "2026-W45", Department: "chan_nuoi_thuy_san", SourceModule: "pf/forms", FormCode: "BM-04", CalendarID: "chan_nuoi_thuy_san",
			CalendarVersion: &v, NativeKey: key, To: &d, FiguresHash: str(fh), StatusHash: str(sh)}
	}
	snap := []Row{row("2026-W45", "f1", "s1"), {TargetKey: "2026-W45", Department: "trong_trot", Missing: true}}
	if ch := Changes(snap, []Row{row("2026-W45", "f1", "s2")}); len(ch) != 0 {
		t.Fatalf("đổi status_hash không phải nguồn đổi: %+v", ch)
	}
	ch := Changes(snap, []Row{row("2026-W45", "f2", "s1"), row("2026-W46", "f9", "s9")})
	if len(ch) != 2 || ch[0].Kind != ChangeFigures || ch[0].Was == nil || ch[1].Kind != ChangeAdded {
		t.Fatalf("số đổi + kỳ mới: %+v", ch)
	}
	if ch := Changes(snap, nil); len(ch) != 1 || ch[0].Kind != ChangeRemoved {
		t.Fatalf("kỳ nguồn mất: %+v", ch)
	}
}

func TestDiff_W1164(t *testing.T) {
	d := func(s string) *decimal.Decimal { x := decimal.RequireFromString(s); return &x }
	prev := map[string]Value{"a": {Cumulative: d("3230"), Increment: d("180")}, "p": {Min: d("8000"), Max: d("14000")}}
	cur := map[string]Value{"a": {Cumulative: d("3230.0"), Increment: d("140")}, "p": {Min: d("8000"), Max: d("13500")}, "n": {Sum: d("8")}}
	got := Diff(prev, cur)
	if len(got) != 3 || got[0].SourceKey != "a" || got[0].Field != FieldIncrement || got[1].SourceKey != "n" || got[1].Old != nil ||
		got[2].Field != FieldPriceMax || !got[2].New.Equal(decimal.NewFromInt(13500)) {
		t.Fatalf("%+v", got)
	}
	if Diff(nil, cur) != nil {
		t.Fatal("ảnh chụp đầu không có lịch sử")
	}
}
