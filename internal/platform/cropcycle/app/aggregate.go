package app

// Nguồn ô THÔ của tập tổng hợp cho feed cổng tích hợp (W5.3, apiclientport.AggregateSource — ADR-0033 §2,
// ADR-0035). pf/apiclient gọi cho từng tenant đóng góp rồi áp quy tắc k MỘT lần (xuyên tenant); ở đây KHÔNG ẩn,
// chỉ gộp theo xã × vụ [× tháng] trên tenant chủ dữ liệu (pool vai của module — R-05):
//   - harvest_progress_by_commune: vụ chưa hủy (season_current) — diện tích gieo, đã gặt (status completed), ngày
//     gặt dự kiến của phần chưa gặt; đơn vị đếm = nông hộ;
//   - collection_volume_by_commune: bảng kê thu gom hiện hành (cùng phép gộp với sản phẩm collection_volume) — số
//     cuộn, diện tích thửa; đơn vị đếm = nông hộ.
// Xã → tỉnh và phiên bản đơn vị hành chính (admin_unit_valid_from) tra pf/masterdata tại asOf (ADR-0032).
// Units là mã nông hộ — chỉ để pf/apiclient đếm phân biệt trong bộ nhớ, không ra feed.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var _ apiclientport.AggregateSource = (*Service)(nil)

// AggregateCells cài đặt apiclientport.AggregateSource.
func (s *Service) AggregateCells(ctx context.Context, dataset string, dataTenant uuid.UUID, asOf time.Time) ([]apiclientport.AggregateCell, error) {
	if dataTenant == uuid.Nil {
		return nil, tenant.ErrMissing
	}
	if s.Units == nil || (dataset == apiclientport.FeedCollectionVolume && s.PlotFacts == nil) {
		return nil, ErrDependency.WithDetail("chưa nối pf/masterdata AdminUnits hoặc pf/cooperative Plots")
	}
	if asOf.IsZero() {
		asOf = s.now()
	}
	units := &unitVersions{ctx: tenant.WithID(ctx, dataTenant), src: s.Units, at: asOf, cache: map[string]partyport.AdminUnit{}}
	switch dataset {
	case apiclientport.FeedHarvestProgress:
		return s.harvestCells(ctx, dataTenant, asOf, units)
	case apiclientport.FeedCollectionVolume:
		return s.volumeCells(ctx, dataTenant, asOf, units)
	}
	return nil, apiclientport.ErrUnknownFeed.WithDetail(dataset)
}

// unitVersions tra phiên bản đơn vị hành chính tại at (có bộ đệm).
type unitVersions struct {
	ctx   context.Context
	src   partyport.AdminUnits
	at    time.Time
	cache map[string]partyport.AdminUnit
}

// get trả đơn vị; ok = false khi không có tại at.
func (u *unitVersions) get(code string) (partyport.AdminUnit, bool, error) {
	if v, ok := u.cache[code]; ok {
		return v, v.Code != "", nil
	}
	v, err := u.src.Get(u.ctx, code, u.at)
	if errors.Is(err, partyport.ErrUnitNotFound) {
		u.cache[code] = partyport.AdminUnit{}
		return partyport.AdminUnit{}, false, nil
	}
	if err != nil {
		return partyport.AdminUnit{}, false, ErrDependency.WithDetail("không đọc được đơn vị hành chính (pf/masterdata)").WithCause(err)
	}
	u.cache[code] = v
	return v, true, nil
}

type place struct {
	province, provinceFrom, communeFrom string
}

// place trả tỉnh + phiên bản tỉnh + phiên bản xã tại at; ok = false → ô bị bỏ (xã không còn hiệu lực tại at hoặc
// không tra được tỉnh — như sản phẩm collection_volume).
func (u *unitVersions) place(commune, province string) (place, bool, error) {
	cu, ok, err := u.get(commune)
	if err != nil || !ok {
		return place{}, false, err
	}
	if province == "" {
		province = cu.ParentCode
	}
	pu, ok, err := u.get(province)
	if err != nil || !ok || pu.Level != partyport.LevelProvince || !provinceRe.MatchString(pu.Code) {
		return place{}, false, err
	}
	return place{province: pu.Code, provinceFrom: pu.ValidFrom.Format(time.DateOnly), communeFrom: cu.ValidFrom.Format(time.DateOnly)}, true, nil
}

func (s *Service) harvestCells(ctx context.Context, dataTenant uuid.UUID, asOf time.Time, units *unitVersions) ([]apiclientport.AggregateCell, error) {
	var rows []store.HarvestRow
	if err := s.inTx(ctx, dataTenant, func(tx pgx.Tx) error {
		var e error
		rows, e = store.HarvestRows(ctx, tx, asOf)
		return e
	}); err != nil {
		return nil, err
	}
	type acc struct {
		cell apiclientport.AggregateCell
		hh   map[uuid.UUID]struct{}
	}
	cells := map[string]*acc{}
	var order []string
	for _, r := range rows {
		season := fmt.Sprintf("%d-%s", r.SeasonYear, r.SeasonCode)
		if !communeRe.MatchString(r.CommuneCode) || !seasonRe.MatchString(season) {
			continue
		}
		key := r.CommuneCode + ":" + season
		a := cells[key]
		if a == nil {
			p, ok, err := units.place(r.CommuneCode, "")
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			a = &acc{cell: apiclientport.AggregateCell{ProvinceCode: p.province, ProvinceValidFrom: p.provinceFrom, CommuneCode: r.CommuneCode,
				AdminUnitValidFrom: p.communeFrom, SeasonCode: season, SeasonYear: r.SeasonYear,
				Measures: map[string]decimal.Decimal{apiclientport.MeasureAreaSown: decimal.Zero, apiclientport.MeasureAreaHarvested: decimal.Zero}},
				hh: map[uuid.UUID]struct{}{}}
			cells[key] = a
			order = append(order, key)
		}
		a.cell.Measures[apiclientport.MeasureAreaSown] = a.cell.Measures[apiclientport.MeasureAreaSown].Add(r.AreaHa)
		if r.Completed {
			a.cell.Measures[apiclientport.MeasureAreaHarvested] = a.cell.Measures[apiclientport.MeasureAreaHarvested].Add(r.AreaHa)
		} else {
			d := r.ExpectedHarvestDate.Format(time.DateOnly)
			if a.cell.DateFrom == "" || d < a.cell.DateFrom {
				a.cell.DateFrom = d
			}
			if d > a.cell.DateTo {
				a.cell.DateTo = d
			}
		}
		if r.HouseholdRef != nil && *r.HouseholdRef != uuid.Nil {
			a.hh[*r.HouseholdRef] = struct{}{}
		}
	}
	out := make([]apiclientport.AggregateCell, 0, len(order))
	for _, k := range order {
		a := cells[k]
		for h := range a.hh {
			a.cell.Units = append(a.cell.Units, h.String())
		}
		out = append(out, a.cell)
	}
	return out, nil
}

func (s *Service) volumeCells(ctx context.Context, dataTenant uuid.UUID, asOf time.Time, units *unitVersions) ([]apiclientport.AggregateCell, error) {
	keys, aggs, err := s.volumeAggs(ctx, dataTenant, asOf)
	if err != nil {
		return nil, err
	}
	out := make([]apiclientport.AggregateCell, 0, len(keys))
	for _, k := range keys {
		a := aggs[k]
		p, ok, err := units.place(a.cell.CommuneCode, a.cell.ProvinceCode)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue // xã không còn hiệu lực tại asOf — ô bị bỏ (cuộn theo kế thừa: W6.4, ADR-0032 §5)
		}
		area := decimal.Zero
		for _, v := range a.plots {
			area = area.Add(v)
		}
		c := apiclientport.AggregateCell{ProvinceCode: p.province, ProvinceValidFrom: p.provinceFrom, CommuneCode: a.cell.CommuneCode,
			AdminUnitValidFrom: p.communeFrom, SeasonCode: a.cell.SeasonCode, PeriodMonth: a.cell.PeriodMonth,
			Measures: map[string]decimal.Decimal{apiclientport.MeasureRolls: decimal.NewFromInt(a.cell.Rolls), apiclientport.MeasureAreaCollected: area}}
		for h := range a.hh {
			c.Units = append(c.Units, h.String())
		}
		out = append(out, c)
	}
	return out, nil
}
