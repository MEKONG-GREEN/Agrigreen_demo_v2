// Package mkgwire là adapter lắp ráp dùng chung của các gốc lắp ráp vùng mkg
// (cmd/api-internal, cmd/worker-internal): nối port của mkg/fieldops với port của
// mkg/measurement (G3-2). Chỉ import port/ và gốc module (R-02); không import
// internal/platform/* (R-03).
//
// Ánh xạ lô (measurement/port ports_g3_2.go): measurement phân giải lô cân → lệnh gom
// (lô gom của fieldops) qua sổ lot_collection trước khi gọi Rolls; mkgwire.Weighed đi
// chiều ngược lại cho fieldops học kg/cuộn.
package mkgwire

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Rolls là measurement/port.RollTotals (+ RollTotalsAsOf, CollectionPlots) trên
// fieldops/port.RollTotals + KgPerRollEstimates. Hai trường được gán sau khi dựng module
// fieldops (measurement dựng trước) — nil → ErrNotFound (đối soát ghi pending_rolls).
type Rolls struct {
	Totals    fieldport.RollTotals
	KgPerRoll fieldport.KgPerRollEstimates
}

var (
	_ mport.RollTotals      = (*Rolls)(nil)
	_ mport.RollTotalsAsOf  = (*Rolls)(nil)
	_ mport.CollectionPlots = (*Rolls)(nil)
)

// farFuture là mốc "không giới hạn" khi RollTotal gọi không kèm thời điểm cân.
var farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// RollTotal cài đặt measurement/port.RollTotals (không mốc thời điểm).
func (r *Rolls) RollTotal(ctx context.Context, tid, order uuid.UUID) (mport.RollTotal, error) {
	return r.RollTotalAsOf(ctx, tid, order, farFuture)
}

// RollTotalAsOf cài đặt measurement/port.RollTotalsAsOf: số cuộn hiệu lực ở công đoạn cuối
// của lệnh gom + kg/cuộn học trước weighedAt (không gồm chính lệnh gom đó).
func (r *Rolls) RollTotalAsOf(ctx context.Context, tid, order uuid.UUID, weighedAt time.Time) (mport.RollTotal, error) {
	lr, err := r.lotRolls(ctx, tid, order)
	if err != nil {
		return mport.RollTotal{}, err
	}
	if lr.Rolls <= 0 {
		return mport.RollTotal{}, mport.ErrNotFound // chỉ rơm rời (tấn) — không đối chiếu cuộn
	}
	out := mport.RollTotal{LotRef: order, Rolls: int64(lr.Rolls), HTXConfirmed: lr.HTXConfirmed, AsOf: weighedAt,
		KgPerRollEstimate: mass.MustKg("20.000"), EstimateSource: "default"}
	if r.KgPerRoll != nil {
		est, err := r.KgPerRoll.KgPerRollAsOf(ctx, tid, lr.HTXRef, weighedAt, order)
		if err != nil {
			return mport.RollTotal{}, err
		}
		out.KgPerRollEstimate, out.EstimateSource = est.Kg, estimateSource(est.Source)
	}
	return out, nil
}

func (r *Rolls) lotRolls(ctx context.Context, tid, order uuid.UUID) (fieldport.LotRolls, error) {
	if r == nil || r.Totals == nil {
		return fieldport.LotRolls{}, mport.ErrNotFound
	}
	lr, err := r.Totals.LotRolls(ctx, tid, order)
	if errors.Is(err, fieldport.ErrLotNotFound) {
		return lr, mport.ErrNotFound
	}
	return lr, err
}

// estimateSource đổi tên nguồn của fieldops sang tên của measurement/port.RollTotal.
func estimateSource(s string) string {
	if s == "htx" {
		return "htx_learned"
	}
	return s
}

// CollectionPlots cài đặt measurement/port.CollectionPlots: tỷ lệ thửa theo số cuộn ở công
// đoạn cuối, 6 chữ số, Σ = 1 chính xác (dư lớn nhất, hòa thì theo plot_ref — tất định).
func (r *Rolls) CollectionPlots(ctx context.Context, tid, order uuid.UUID) ([]mport.PlotShare, error) {
	lr, err := r.lotRolls(ctx, tid, order)
	if err != nil {
		return nil, err
	}
	return Shares(lr.PerPlot), nil
}

// Shares chia tỷ lệ theo số cuộn (6 chữ số, Σ = 1, phương pháp dư lớn nhất).
func Shares(perPlot map[uuid.UUID]int) []mport.PlotShare {
	type row struct {
		plot  uuid.UUID
		units int64
		rem   decimal.Decimal
	}
	total := int64(0)
	rows := make([]row, 0, len(perPlot))
	for p, n := range perPlot {
		if n > 0 {
			rows = append(rows, row{plot: p, units: int64(n)})
			total += int64(n)
		}
	}
	if total == 0 {
		return nil
	}
	const scale = 1_000_000
	sum := int64(0)
	for i := range rows {
		exact := decimal.NewFromInt(rows[i].units * scale).Div(decimal.NewFromInt(total))
		fl := exact.Floor()
		rows[i].rem = exact.Sub(fl)
		rows[i].units = fl.IntPart()
		sum += rows[i].units
	}
	sort.Slice(rows, func(a, b int) bool {
		if c := rows[a].rem.Cmp(rows[b].rem); c != 0 {
			return c > 0
		}
		return rows[a].plot.String() < rows[b].plot.String()
	})
	for i := 0; sum < scale; i = (i + 1) % len(rows) {
		rows[i].units++
		sum++
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].plot.String() < rows[b].plot.String() })
	out := make([]mport.PlotShare, 0, len(rows))
	for _, x := range rows {
		out = append(out, mport.PlotShare{PlotRef: x.plot, Ratio: decimal.New(x.units, -6).StringFixed(6)})
	}
	return out
}

// Weighed là fieldops/port.WeighedMass qua measurement: lệnh gom → lô cân (liên kết tường
// minh hoặc đồng nhất) → khối lượng cân đầu tiên (HUB_IN; CUSTOMER_IN với tuyến DIRECT).
// Dùng cho đường làm mới tay RefreshWeighings (chạy bù consumer mkg.fieldops.weigh_ticket).
type Weighed struct {
	Links mport.CollectionLinks
	Lots  mport.LotMass
}

var _ fieldport.WeighedMass = Weighed{}

// WeighedKg cài đặt fieldops/port.WeighedMass.
func (w Weighed) WeighedKg(ctx context.Context, tid, order uuid.UUID) (mass.Kg, time.Time, bool, error) {
	lot := order
	if w.Links != nil {
		l, err := w.Links.LotOfCollection(ctx, tid, order)
		if err != nil {
			return mass.Kg{}, time.Time{}, false, err
		}
		lot = l.LotRef
	}
	v, err := w.Lots.LotMass(ctx, tid, lot)
	if errors.Is(err, mport.ErrNotFound) {
		return mass.Kg{}, time.Time{}, false, nil
	}
	if err != nil {
		return mass.Kg{}, time.Time{}, false, err
	}
	k := v.HubInKg
	if v.Route == mport.RouteDirect || k == nil {
		k = v.CustomerInKg
	}
	if k == nil {
		return mass.Kg{}, time.Time{}, false, nil
	}
	return *k, v.AsOf, true, nil
}
