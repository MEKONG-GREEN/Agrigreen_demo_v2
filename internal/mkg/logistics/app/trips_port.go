package app

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// port.Trips — cổng cùng tiến trình cho mkg/costing, mkg/orders (tenant tường minh, không kiểm vai:
// module gọi đã kiểm vai của chính nó; RLS vẫn lọc theo tenant — RB-2).

func legViews(legs []store.Leg) []port.LegView {
	out := make([]port.LegView, 0, len(legs))
	for _, l := range legs {
		out = append(out, port.LegView{LegID: l.LegID, Seq: l.LegSeq, Mode: l.Mode, FromKind: l.FromKind, ToKind: l.ToKind, FromRef: l.FromRef,
			ToRef: l.ToRef, DistanceKm: l.DistanceKm, RouteSource: l.RouteSource, RouteSnapshotRef: l.SnapshotID, NetworkVersion: l.NetworkVersion,
			MassKg: l.MassKg})
	}
	return out
}

func cargoLines(c []store.Cargo) []port.CargoLine {
	out := make([]port.CargoLine, 0, len(c))
	for _, x := range c {
		out = append(out, port.CargoLine{LotRef: x.LotRef, MassKg: x.MassKg, TicketRef: x.TicketRef})
	}
	return out
}

// View quy dòng trip_current + chặng hiệu lực về port.TripView.
func View(t store.Trip, legs []store.Leg) port.TripView {
	v := port.TripView{TripID: t.TripID, Code: t.Code, Status: t.Status, Mode: mainMode(t, legs), VehicleRef: t.VehicleRef,
		CollectionOrderRef: t.CollectionOrderRef, DeliveryRef: t.DeliveryRef, Legs: legViews(legs), Cargo: cargoLines(t.Cargo),
		PlannedKg: t.PlannedKg, ActualKg: t.ActualKg, PlannedCost: t.PlannedCost, ActualCost: t.ActualCost, DepartedAt: t.DepartedAt,
		ArrivedAt: t.ArrivedAt, CompletedAt: t.CompletedAt, RuleVersion: t.RuleVersion, RecordedAt: t.RecordedAt}
	if t.CostSource != nil {
		v.CostSource = *t.CostSource
	}
	return v
}

// Trip cài đặt port.Trips.
func (s *Service) Trip(ctx context.Context, tenantID, tripID uuid.UUID) (port.TripView, error) {
	var out port.TripView
	err := tenant.WithTenantTx(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		t, err := store.GetTrip(ctx, tx, tripID, false)
		if err != nil {
			return err
		}
		if t == nil {
			return port.ErrTripNotFound
		}
		legs, err := store.LatestPlan(ctx, tx, tripID, nil)
		out = View(*t, legs)
		return err
	})
	return out, err
}

// TripsForLot cài đặt port.Trips.
func (s *Service) TripsForLot(ctx context.Context, tenantID, lotRef uuid.UUID) ([]port.TripView, error) {
	var out []port.TripView
	err := tenant.WithTenantTx(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		ts, err := store.TripsForLot(ctx, tx, lotRef)
		if err != nil {
			return err
		}
		for _, t := range ts {
			legs, err := store.LatestPlan(ctx, tx, t.TripID, nil)
			if err != nil {
				return err
			}
			out = append(out, View(t, legs))
		}
		return nil
	})
	return out, err
}

func encodeCursor(at time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

// DecodeCursor đọc con trỏ (thời điểm, id); rỗng → nil.
func DecodeCursor(c string) (*time.Time, uuid.UUID, bool) {
	if c == "" {
		return nil, uuid.Nil, true
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return nil, uuid.Nil, false
	}
	at, id, ok := strings.Cut(string(b), "|")
	if !ok {
		return nil, uuid.Nil, false
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, uuid.Nil, false
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, uuid.Nil, false
	}
	return &t, u, true
}

// EncodeCursor dựng con trỏ (thời điểm, id).
func EncodeCursor(at time.Time, id uuid.UUID) string { return encodeCursor(at, id) }

// Completed cài đặt port.Trips: bản completed mới nhất (đã tính bản bù) có recorded_at < asOf, completed_at ∈
// [from, to) — ảnh chụp đầu vào bất biến của đóng kỳ (R-13); chặng = phiên bản kế hoạch ghi trước asOf.
func (s *Service) Completed(ctx context.Context, tenantID uuid.UUID, from, to, asOf time.Time, cursor string, limit int) (port.TripPage, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	afterAt, afterID, ok := DecodeCursor(cursor)
	if !ok {
		afterAt, afterID = nil, uuid.Nil
	}
	var page port.TripPage
	err := tenant.WithTenantTx(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		evs, err := store.LatestCompletions(ctx, tx, from, to, asOf, afterAt, afterID, limit+1)
		if err != nil {
			return err
		}
		more := len(evs) > limit
		if more {
			evs = evs[:limit]
		}
		for _, e := range evs {
			all, err := store.TripEvents(ctx, tx, e.TripID)
			if err != nil {
				return err
			}
			var t *store.Trip
			for _, x := range all {
				if !x.RecordedAt.Before(asOf) {
					break
				}
				f := store.Fold(t, x)
				t = &f
			}
			if t == nil {
				continue
			}
			ao := asOf
			legs, err := store.LatestPlan(ctx, tx, e.TripID, &ao)
			if err != nil {
				return err
			}
			store.ApplyPlan(t, legs)
			page.Items = append(page.Items, View(*t, legs))
		}
		if more && len(evs) > 0 {
			last := evs[len(evs)-1]
			page.Next = encodeCursor(*last.CompletedAt, last.TripID)
		}
		return nil
	})
	return page, err
}
