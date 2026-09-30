package mkgwire

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Tỷ lệ thửa theo số cuộn: 6 chữ số, Σ = 1 chính xác, tất định (dư lớn nhất, hòa theo plot_ref).
func TestSharesSumToOne(t *testing.T) {
	a, b, c := uuid.MustParse("00000000-0000-0000-0000-00000000000a"), uuid.MustParse("00000000-0000-0000-0000-00000000000b"),
		uuid.MustParse("00000000-0000-0000-0000-00000000000c")
	for _, per := range []map[uuid.UUID]int{{a: 1, b: 1, c: 1}, {a: 4400}, {a: 2, b: 1}, {a: 1000, b: 0, c: 3}} {
		xs := Shares(per)
		sum := decimal.Zero
		for _, x := range xs {
			sum = sum.Add(decimal.RequireFromString(x.Ratio))
			if len(x.Ratio) != len("0.000000") {
				t.Fatalf("6 chữ số: %s", x.Ratio)
			}
		}
		if !sum.Equal(decimal.NewFromInt(1)) {
			t.Fatalf("Σ = %s cho %v", sum, per)
		}
	}
	if xs := Shares(map[uuid.UUID]int{a: 1, b: 1, c: 1}); xs[0].Ratio != "0.333334" || xs[1].Ratio != "0.333333" {
		t.Fatalf("dư lớn nhất theo plot_ref: %v", xs)
	}
	if Shares(map[uuid.UUID]int{a: 0}) != nil {
		t.Fatal("không có cuộn → không tỷ lệ")
	}
}

type fakeTotals struct{ lr *fieldport.LotRolls }

func (f fakeTotals) LotRolls(context.Context, uuid.UUID, uuid.UUID) (fieldport.LotRolls, error) {
	if f.lr == nil {
		return fieldport.LotRolls{}, fieldport.ErrLotNotFound
	}
	return *f.lr, nil
}

type fakeKpr struct {
	before time.Time
	excl   uuid.UUID
}

func (f *fakeKpr) KgPerRollAsOf(_ context.Context, _, _ uuid.UUID, before time.Time, excl uuid.UUID) (fieldport.KgPerRollEstimate, error) {
	f.before, f.excl = before, excl
	return fieldport.KgPerRollEstimate{Kg: mass.MustKg("21.600"), Source: "htx", Samples: 3}, nil
}

// Adapter measurement ← fieldops: lô gom không có → ErrNotFound (pending_rolls); kg/cuộn học
// trước mốc cân và bỏ chính lệnh gom; tên nguồn htx → htx_learned; chưa nối → ErrNotFound.
func TestRollsAdapter(t *testing.T) {
	ctx, tid, order := context.Background(), uuid.New(), uuid.New()
	if _, err := (&Rolls{}).RollTotal(ctx, tid, order); !errors.Is(err, mport.ErrNotFound) {
		t.Fatalf("chưa nối: %v", err)
	}
	if _, err := (&Rolls{Totals: fakeTotals{}}).RollTotal(ctx, tid, order); !errors.Is(err, mport.ErrNotFound) {
		t.Fatalf("không có lô gom: %v", err)
	}
	k := &fakeKpr{}
	r := &Rolls{Totals: fakeTotals{&fieldport.LotRolls{LotRef: order, Rolls: 4400, HTXConfirmed: true,
		PerPlot: map[uuid.UUID]int{uuid.New(): 4400}}}, KgPerRoll: k}
	at := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	rt, err := r.RollTotalAsOf(ctx, tid, order, at)
	if err != nil || rt.Rolls != 4400 || rt.KgPerRollEstimate.String() != "21.600" || rt.EstimateSource != "htx_learned" || !rt.HTXConfirmed ||
		!k.before.Equal(at) || k.excl != order {
		t.Fatalf("RollTotalAsOf: %+v %v (mốc %v, bỏ %v)", rt, err, k.before, k.excl)
	}
	ps, err := r.CollectionPlots(ctx, tid, order)
	if err != nil || len(ps) != 1 || ps[0].Ratio != "1.000000" {
		t.Fatalf("CollectionPlots: %v %v", ps, err)
	}
}
