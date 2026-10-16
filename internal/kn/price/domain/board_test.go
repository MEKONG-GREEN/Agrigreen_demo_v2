package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestBoardView_W1176(t *testing.T) {
	now := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	until := now.Add(time.Minute)
	actor := uuid.New()
	if (Board{}).View(now) != BoardOpen {
		t.Fatal("chưa có sổ → Chưa duyệt")
	}
	b := Board{Seq: 1, Status: BoardApproving, BaseStatus: BoardReopened, LockUntil: &until, Version: 1, Actor: actor}
	if b.View(now) != BoardApproving || b.LockedBy(now) != actor {
		t.Fatalf("khóa còn hạn: %s", b.View(now))
	}
	if later := until; b.View(later) != BoardReopened || b.LockedBy(later) != uuid.Nil {
		t.Fatalf("khóa hết hạn → trạng thái trước khóa: %s", b.View(later))
	}
	if !b.Published() || (Board{Seq: 1, Status: BoardApproving, BaseStatus: BoardOpen, LockUntil: &until}).Published() {
		t.Fatal("Published = đã có phiên bản duyệt")
	}
}

func TestReopenWithin_W1176(t *testing.T) {
	today := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	if !ReopenWithin(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), today, 0) {
		t.Fatal("30 ngày trước: còn mở duyệt được")
	}
	if ReopenWithin(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), today, 30) {
		t.Fatal("31 ngày trước: chặn (AC-05)")
	}
}

func TestAggregateBoard_W1176(t *testing.T) {
	d := func(s string) decimal.Decimal { return decimal.RequireFromString(s) }
	src := []BoardSource{
		{Point: "XBG0001", Item: "MH00002", Min: d("38000"), Max: d("40000"), Mid: d("39000")},
		{Point: "XBG0002", Item: "MH00002", Min: d("37000"), Max: d("41000"), Mid: d("39000")},
		{Point: "XBG0003", Item: "MH00002", Min: d("36500"), Max: d("36500"), Mid: d("36500")},
		{Point: "XBG0001", Item: "MH00001", Min: d("9000"), Max: d("9000"), Mid: d("9000")},
	}
	got := AggregateBoard(src)
	if len(got) != 2 || got[0].Item != "MH00001" || got[0].Points != 1 || got[0].Avg.String() != "9000" {
		t.Fatalf("MH00001: %+v", got)
	}
	// CT-04: min / max toàn tỉnh, trung bình cộng P = (39000 + 39000 + 36500) / 3 = 38166,6667.
	if g := got[1]; g.Min.String() != "36500" || g.Max.String() != "41000" || g.Avg.String() != "38166.6667" || g.Points != 3 {
		t.Fatalf("MH00002: %+v", g)
	}
}

func TestDefaultWeekOf_W1176(t *testing.T) {
	cases := []struct{ day, key, from, to string }{
		{"2026-11-04", "2026-W45", "2026-11-04", "2026-11-10"}, // Thứ Tư — đầu tuần (QĐ-6)
		{"2026-11-10", "2026-W45", "2026-11-04", "2026-11-10"}, // Thứ Ba — cuối tuần
		{"2025-12-31", "2026-W01", "2025-12-31", "2026-01-06"}, // tuần chứa 01/01 thuộc năm của ngày cuối tuần
	}
	for _, c := range cases {
		day, _ := time.Parse(time.DateOnly, c.day)
		w := DefaultWeekOf(day)
		if w.Key != c.key || w.From.Format(time.DateOnly) != c.from || w.To.Format(time.DateOnly) != c.to {
			t.Errorf("%s: %+v", c.day, w)
		}
	}
}
