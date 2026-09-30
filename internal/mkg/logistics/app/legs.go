package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Endpoint là một đầu chặng.
type Endpoint struct {
	Kind  string // plot | collection_point | facility | customer_site | point
	Ref   *uuid.UUID
	Point port.GeoPoint
}

// LegInput là một chặng điều phối lập.
type LegInput struct {
	Mode         port.Mode
	From, To     Endpoint
	ManualKm     *decimal.Decimal // km nhập tay (bỏ qua định tuyến) — bắt buộc ManualReason
	ManualReason string
}

func validKind(k string) bool {
	switch k {
	case "plot", "collection_point", "facility", "customer_site", "point":
		return true
	}
	return false
}

// routed là kết quả định tuyến một chặng (tính ngoài giao dịch).
type routed struct {
	km       decimal.Decimal
	source   port.RouteSource
	res      *port.RouteResult
	provider string
	request  []byte
	flags    []string
	manual   *string
}

type routeParams struct {
	factor, feasibleKm decimal.Decimal
}

func (s *Service) waterway() port.WaterwayRoutes {
	if s.Waterway == nil {
		return adapter.Unconfigured{}
	}
	return s.Waterway
}

func (s *Service) road(factor decimal.Decimal) port.RoadRoutes {
	if s.Road != nil {
		return s.Road
	}
	return adapter.HaversineRoad{Factor: factor, Now: s.Clock.Now}
}

func requestJSON(provider string, mode port.Mode, req port.RouteRequest, factor *decimal.Decimal) []byte {
	pts := make([][2]string, 0, len(req.Points))
	for _, p := range req.Points {
		pts = append(pts, [2]string{p.Lat.String(), p.Lng.String()})
	}
	m := map[string]any{"provider": provider, "mode": mode, "points": pts, "min_load_tons": req.MinLoadTons.String(),
		"network_version": req.NetworkVersion, "rule_version": domain.RuleRoute}
	if factor != nil {
		m["factor"] = factor.String()
	}
	b, _ := json.Marshal(m)
	return b
}

// routeLeg định tuyến một chặng. Không bao giờ chặn (L-04): đường thủy lỗi / thiếu cấp quyền / không có
// đường → dự phòng Haversine × #10 (nhãn haversine) kèm cờ; km tay có lý do bỏ qua định tuyến.
func (s *Service) routeLeg(ctx context.Context, tid uuid.UUID, in LegInput, tonnes decimal.Decimal, rp routeParams) (routed, error) {
	if in.ManualKm != nil {
		r, err := domain.CheckReason("manual_reason", in.ManualReason, true)
		if err != nil {
			return routed{}, ErrManualReason
		}
		return routed{km: in.ManualKm.Round(3), source: port.RouteManual, manual: &r}, nil
	}
	req := port.RouteRequest{Points: []port.GeoPoint{in.From.Point, in.To.Point}}
	haversine := func(flags ...string) (routed, error) {
		res, err := s.road(rp.factor).Route(ctx, tid, req)
		if err != nil {
			return routed{}, err
		}
		f := rp.factor
		return routed{km: res.DistanceKm.Round(3), source: res.Source, res: &res, provider: store.ProviderRoad,
			request: requestJSON(store.ProviderRoad, port.ModeRoad, req, &f), flags: flags}, nil
	}
	if in.Mode == port.ModeWaterway {
		wreq := req
		wreq.MinLoadTons = tonnes.Round(3)
		res, err := s.waterway().Route(ctx, tid, wreq)
		switch {
		case errors.Is(err, port.ErrRouteUnavailable), errors.Is(err, port.ErrRouteGrantMissing):
			return haversine(domain.FlagRoutingUnavailable)
		case err != nil:
			return haversine(domain.FlagRoutingUnavailable)
		case !res.Found:
			return haversine(domain.FlagNoWaterwayRoute)
		}
		return routed{km: res.DistanceKm.Round(3), source: res.Source, res: &res, provider: store.ProviderWaterway,
			request: requestJSON(store.ProviderWaterway, port.ModeWaterway, wreq, nil)}, nil
	}
	r, err := haversine()
	if err != nil {
		return r, err
	}
	// proto decideMode (GIS FN-08 BR-04): đường bộ xa hơn #27 và có kênh tiếp cận → gợi ý đường thủy.
	if r.km.GreaterThan(rp.feasibleKm) {
		if w, err := s.waterway().Route(ctx, tid, req); err == nil && w.Found {
			r.flags = append(r.flags, domain.FlagWaterwaySuggested)
		}
	}
	return r, nil
}

// PlanLegs lập (lại) các chặng của chuyến: định tuyến từng chặng NGOÀI giao dịch, rồi trong một giao
// dịch ghi ảnh chụp nguyên văn + SHA-256, sổ chặng (phiên bản kế hoạch mới), cước kế hoạch theo tham số
// tại thời điểm lập (rule_version logistics.trip_cost.v1) và bảng chiếu chuyến.
func (s *Service) PlanLegs(ctx context.Context, trip uuid.UUID, legs []LegInput, idem string) (TripResult, bool, error) {
	p, err := authz.Require(ctx, RoleDispatcher)
	if err != nil {
		return TripResult{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return TripResult{}, false, err
	}
	if len(legs) == 0 || len(legs) > 24 {
		return TripResult{}, false, problem.Validation.WithField("legs", "1..24 chặng")
	}
	for i, l := range legs {
		f := func(n string) string { return "legs[" + itoa(i) + "]." + n }
		switch {
		case l.Mode != port.ModeRoad && l.Mode != port.ModeWaterway:
			return TripResult{}, false, problem.Validation.WithField(f("mode"), "road | waterway")
		case !validKind(l.From.Kind) || !validKind(l.To.Kind):
			return TripResult{}, false, problem.Validation.WithField(f("from.kind"), "plot | collection_point | facility | customer_site | point")
		case !domain.ValidPoint(l.From.Point) || !domain.ValidPoint(l.To.Point):
			return TripResult{}, false, problem.Validation.WithField(f("from"), "tọa độ WGS84")
		case l.ManualKm != nil && l.ManualKm.Sign() < 0:
			return TripResult{}, false, problem.Validation.WithField(f("manual_km"), "không âm")
		}
	}
	prefix := "legs:" + idem
	var res TripResult
	replay := func(tx pgx.Tx) (bool, error) {
		ls, err := store.LegsByIdemPrefix(ctx, tx, prefix)
		if err != nil || len(ls) == 0 {
			return false, err
		}
		t, err := store.GetTrip(ctx, tx, ls[0].TripID, false)
		if err != nil || t == nil {
			return false, err
		}
		cur, err := store.LatestPlan(ctx, tx, t.TripID, nil)
		res = TripResult{Trip: *t, Legs: cur}
		return err == nil, err
	}
	// 1. Đọc chuyến + tham số (giao dịch ngắn), gọi lặp → trả lại.
	var cur store.Trip
	var rp routeParams
	replayed := false
	now := s.Clock.Now()
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		ok, err := replay(tx)
		if err != nil || ok {
			replayed = ok
			return err
		}
		t, err := store.GetTrip(ctx, tx, trip, false)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrTripNotFound
		}
		cur = *t
		f, err := param(ctx, tx, port.ParamRoadHaversineFactor, now)
		if err != nil {
			return err
		}
		k, err := param(ctx, tx, port.ParamRoadFeasibleKm, now)
		rp = routeParams{factor: f.Value, feasibleKm: k.Value}
		return err
	})
	if err != nil || replayed {
		if replayed && res.Trip.TripID != trip {
			return TripResult{}, false, problem.IdempotencyConflict
		}
		return res, replayed, mapErr(err)
	}
	if cur.Status == port.TripCompleted || cur.Status == port.TripCancelled {
		return TripResult{}, false, ErrTripClosed
	}
	planned := mass.MustKg("0")
	if cur.PlannedKg != nil {
		planned = *cur.PlannedKg
	}
	// 2. Định tuyến ngoài giao dịch (gọi mạng).
	rs := make([]routed, len(legs))
	for i, l := range legs {
		if rs[i], err = s.routeLeg(ctx, p.TenantID, l, domain.Tonnes(planned), rp); err != nil {
			return TripResult{}, false, mapErr(err)
		}
	}
	// 3. Ghi (ảnh chụp + chặng + bảng chiếu) trong một giao dịch.
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := loadTrip(ctx, tx, p.TenantID, trip)
		if err != nil {
			return err
		}
		if t.Status == port.TripCompleted || t.Status == port.TripCancelled {
			return ErrTripClosed
		}
		rev := t.PlanRevision + 1
		var out []store.Leg
		for i, l := range legs {
			r := rs[i]
			leg := store.Leg{ID: ids.New(), TenantID: p.TenantID, TripID: trip, LegID: ids.New(), PlanRevision: rev, LegSeq: i + 1,
				Mode: l.Mode, FromKind: l.From.Kind, FromRef: l.From.Ref, From: roundPt(l.From.Point), ToKind: l.To.Kind, ToRef: l.To.Ref,
				To: roundPt(l.To.Point), DistanceKm: r.km, RouteSource: r.source, ManualReason: r.manual, MassKg: t.PlannedKg,
				Flags: mergeFlags(nil, nil, r.flags), EventTime: now, ActorRef: p.ActorRef, IdemKey: prefix + ":" + itoa(i+1),
				RuleVersion: domain.RuleTripCost}
			if r.res != nil {
				sum, err := hex.DecodeString(r.res.RawSHA256)
				if err != nil {
					return err
				}
				fetched := r.res.FetchedAt
				if fetched.IsZero() {
					fetched = now
				}
				snap := store.Snapshot{ID: ids.New(), TenantID: p.TenantID, TripID: trip, LegID: leg.LegID, Provider: r.provider,
					Request: r.request, Raw: r.res.Raw, RawSHA256: sum, GrantorTenant: r.res.GrantorTenant, NetworkVersion: r.res.NetworkVersion,
					FetchedAt: fetched, EventTime: now, ActorRef: p.ActorRef, IdemKey: leg.IdemKey + ":route", RuleVersion: domain.RuleRoute}
				if err := insert(store.InsertSnapshot(ctx, tx, &snap)); err != nil {
					return err
				}
				leg.SnapshotID, leg.NetworkVersion = &snap.ID, r.res.NetworkVersion
			}
			rate, err := param(ctx, tx, rateKey(l.Mode), now)
			if err != nil {
				return err
			}
			leg.RateVNDPerTKm, leg.ParamVersion = rate.Value, rate.Version
			leg.PlannedCost = domain.LegCost(planned, r.km, rate.Value)
			if err := insert(store.InsertLeg(ctx, tx, &leg)); err != nil {
				return err
			}
			out = append(out, leg)
		}
		next := *t
		store.ApplyPlan(&next, out)
		if err := store.UpsertTrip(ctx, tx, next); err != nil {
			return err
		}
		res = TripResult{Trip: next, Legs: out}
		return nil
	}, replay)
	return res, replayed, err
}

func roundPt(p port.GeoPoint) port.GeoPoint {
	return port.GeoPoint{Lat: p.Lat.Round(7), Lng: p.Lng.Round(7)}
}
