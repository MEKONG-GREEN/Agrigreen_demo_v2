package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/store"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ---------------------------------------------------------------------------
// Cổng đọc cho module khác (W3.3; thay geoport.Plots, PlotFacts, PlotStats, PlotHouseholds, CoopCentroids). Tenant
// lấy từ context; không kiểm phạm vi người gọi — module gọi tự giới hạn.
// ---------------------------------------------------------------------------

var (
	_ port.Plots           = (*Service)(nil)
	_ geoport.PlotGeometry = (*Service)(nil)
)

// Get thực hiện cooperativeport.Plots.
func (s *Service) Get(ctx context.Context, ref uuid.UUID) (port.Plot, error) {
	var p *store.Plot
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		p, e = store.PlotByRef(ctx, tx, ref, false)
		return e
	})
	if err != nil {
		return port.Plot{}, err
	}
	if p == nil {
		return port.Plot{}, port.ErrPlotNotFound
	}
	return port.Plot{
		Ref: p.Ref, Code: p.Code, CommuneCode: p.CommuneCode, CooperativeRef: p.CooperativeRef, HouseholdRef: p.HouseholdRef,
		Status: p.Status, Version: p.CurrentVersion, AreaHa: p.AreaHa,
	}, nil
}

// DistanceM thực hiện cooperativeport.Plots: khoảng cách trắc địa từ điểm tới thửa (HTX US-GPS-01).
func (s *Service) DistanceM(ctx context.Context, ref uuid.UUID, lon, lat decimal.Decimal) (decimal.Decimal, error) {
	var d *decimal.Decimal
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		d, e = store.DistanceM(ctx, tx, ref, lon.String(), lat.String())
		return e
	})
	if err != nil {
		return decimal.Zero, err
	}
	if d == nil {
		return decimal.Zero, port.ErrPlotNotFound
	}
	return *d, nil
}

// Facts thực hiện cooperativeport.Plots: dữ kiện nhiều thửa (kể cả đã ngừng dùng) cho sản phẩm tổng hợp.
func (s *Service) Facts(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]port.Plot, error) {
	out := make(map[uuid.UUID]port.Plot, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		for i := 0; i < len(refs); i += 1000 {
			rows, err := store.PlotFacts(ctx, tx, refs[i:min(i+1000, len(refs))])
			if err != nil {
				return err
			}
			for _, p := range rows {
				out[p.Ref] = port.Plot{Ref: p.Ref, Code: p.Code, CommuneCode: p.CommuneCode, CooperativeRef: p.CooperativeRef,
					HouseholdRef: p.HouseholdRef, Status: p.Status, Version: p.Version, AreaHa: p.AreaHa}
			}
		}
		return nil
	})
	return out, err
}

// InCommunes trả nông hộ có thửa đang dùng (nháp hoặc đã gán HTX) trong các xã; communes nil = mọi xã (nửa
// cooperativeport.Households.InCommunes — HTX US-NEWS-03).
func (s *Service) InCommunes(ctx context.Context, communes []string) ([]uuid.UUID, error) {
	if communes != nil && len(communes) == 0 {
		return []uuid.UUID{}, nil
	}
	var out []uuid.UUID
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.HouseholdsInCommunes(ctx, tx, communes == nil, communes)
		return e
	})
	return out, err
}

// Centroids trả tâm ranh giới HTX đang dùng (nửa cooperativeport.Cooperatives.Centroids — nguồn toạ độ của mô phỏng
// chọn Hub). coops nil = mọi HTX; HTX chưa có ranh giới không có trong kết quả.
func (s *Service) Centroids(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]port.CoopCentroid, error) {
	out := map[uuid.UUID]port.CoopCentroid{}
	if coops != nil && len(coops) == 0 {
		return out, nil
	}
	var rows []store.CoopCentroid
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		rows, e = store.CoopCentroids(ctx, tx, coops == nil, coops)
		return e
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.CooperativeRef] = port.CoopCentroid{CooperativeRef: r.CooperativeRef, Lon: r.Lon, Lat: r.Lat}
	}
	return out, nil
}

// PlotPlace thực hiện geoport.PlotGeometry (log GPS của pf/geo): xã, HTX của thửa và khoảng cách từ điểm tới ranh giới
// hiện hành.
func (s *Service) PlotPlace(ctx context.Context, ref uuid.UUID, lon, lat *decimal.Decimal) (geoport.PlotPlace, bool, error) {
	slon, slat := "", ""
	if lon != nil && lat != nil {
		slon, slat = lon.String(), lat.String()
	}
	var p *store.PlotPlace
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		p, e = store.PlotPlaceAt(ctx, tx, ref, slon, slat)
		return e
	})
	if err != nil || p == nil {
		return geoport.PlotPlace{}, false, err
	}
	return geoport.PlotPlace{CommuneCode: p.CommuneCode, CooperativeRef: p.CooperativeRef, DistanceM: p.DistanceM}, true, nil
}

// PlotsOfHouseholds thực hiện geoport.PlotGeometry: thửa (mọi trạng thái) của các nông hộ — vai farmer đọc log GPS.
func (s *Service) PlotsOfHouseholds(ctx context.Context, households []uuid.UUID) ([]uuid.UUID, error) {
	if len(households) == 0 {
		return []uuid.UUID{}, nil
	}
	var out []uuid.UUID
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.PlotsOfHouseholds(ctx, tx, households)
		return e
	})
	return out, err
}
