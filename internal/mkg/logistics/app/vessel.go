package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// VesselInput là một phiên bản danh mục phương tiện (proto GH-01..03).
type VesselInput struct {
	Code               string
	Kind               string
	Name               string
	OwnerRef           *uuid.UUID
	RegisteredTons     *decimal.Decimal
	StrawPayloadTons   *decimal.Decimal
	RegistrationExpiry *time.Time
	RateType           string
	RateVND            *decimal.Decimal
	Active             bool
	Reason             string // bắt buộc khi sửa
	ExpectedVersion    int    // sửa: phiên bản hiện hành
	IdemKey            string
}

// VesselView là phương tiện kèm trạng thái đăng kiểm (GH-02 — chỉ cảnh báo).
type VesselView struct {
	Vessel      store.Vessel
	Expiry      string
	DaysLeft    *int
	Warning     string
	WarnDays    int
	WarnVersion string
}

func (in VesselInput) toDomain() domain.Vessel {
	return domain.Vessel{Code: domain.NormalizeCode(in.Code), Kind: in.Kind, Name: in.Name, RegisteredTons: in.RegisteredTons,
		StrawPayloadTons: in.StrawPayloadTons, RegistrationExpiry: in.RegistrationExpiry, RateType: in.RateType, RateVND: in.RateVND,
		Active: in.Active}
}

func (s *Service) vesselWrite(ctx context.Context, vessel *uuid.UUID, in VesselInput) (VesselView, bool, error) {
	p, err := authz.Require(ctx, RoleDispatcher)
	if err != nil {
		return VesselView{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return VesselView{}, false, err
	}
	d := in.toDomain()
	if err := domain.ValidateVessel(d); err != nil {
		return VesselView{}, false, mapErr(err)
	}
	reason, err := domain.CheckReason("reason", in.Reason, vessel != nil)
	if err != nil {
		return VesselView{}, false, mapErr(err)
	}
	var view VesselView
	now := s.Clock.Now()
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		e := store.VesselEvent{ID: ids.New(), TenantID: p.TenantID, Kind: "created", Version: 1, Code: d.Code, VehicleKind: d.Kind,
			Name: strPtr(in.Name), OwnerRef: in.OwnerRef, RegisteredTons: in.RegisteredTons, StrawPayloadTons: in.StrawPayloadTons,
			RegistrationExpiry: in.RegistrationExpiry, RateType: strPtr(in.RateType), RateVND: in.RateVND, Status: "active",
			Reason: strPtr(reason), EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey, RuleVersion: domain.RuleVessel}
		if vessel == nil {
			e.VesselID = ids.New()
		} else {
			cur, err := store.GetVessel(ctx, tx, *vessel, true)
			if err != nil {
				return err
			}
			if cur == nil {
				return ErrVesselNotFound
			}
			if in.ExpectedVersion != cur.Version {
				return ErrVesselStale
			}
			e.VesselID, e.Kind, e.Version = cur.VesselID, "revised", cur.Version+1
			if !in.Active {
				e.Status = "inactive"
			}
		}
		if other, err := store.VesselByCode(ctx, tx, d.Code); err != nil {
			return err
		} else if other != nil && other.VesselID != e.VesselID {
			return ErrVesselCodeTaken
		}
		if err := insert(store.InsertVesselEvent(ctx, tx, &e)); err != nil {
			return err
		}
		if err := store.ApplyVesselEvent(ctx, tx, e); err != nil {
			return mapErr(err)
		}
		v, err := vesselView(ctx, tx, e.VesselID, now)
		view = v
		return err
	}, func(tx pgx.Tx) (bool, error) {
		e, err := store.VesselEventByIdem(ctx, tx, in.IdemKey)
		if err != nil || e == nil {
			return false, err
		}
		v, err := vesselView(ctx, tx, e.VesselID, now)
		view = v
		return err == nil, err
	})
	return view, replayed, err
}

// CreateVessel thêm phương tiện vào danh mục (số hiệu chuẩn hóa duy nhất — GH-01).
func (s *Service) CreateVessel(ctx context.Context, in VesselInput) (VesselView, bool, error) {
	in.Active = true
	return s.vesselWrite(ctx, nil, in)
}

// ReviseVessel ghi phiên bản mới của phương tiện (có lý do, theo phiên bản hiện hành).
func (s *Service) ReviseVessel(ctx context.Context, vessel uuid.UUID, in VesselInput) (VesselView, bool, error) {
	return s.vesselWrite(ctx, &vessel, in)
}

func vesselView(ctx context.Context, tx pgx.Tx, id uuid.UUID, now time.Time) (VesselView, error) {
	v, err := store.GetVessel(ctx, tx, id, false)
	if err != nil {
		return VesselView{}, err
	}
	if v == nil {
		return VesselView{}, ErrVesselNotFound
	}
	return decorate(ctx, tx, *v, now)
}

func decorate(ctx context.Context, tx pgx.Tx, v store.Vessel, now time.Time) (VesselView, error) {
	w, err := param(ctx, tx, port.ParamExpiryWarnDays, now)
	if err != nil {
		return VesselView{}, err
	}
	days := int(w.Value.IntPart())
	state, left := domain.ExpiryStatus(v.RegistrationExpiry, day(now), days)
	return VesselView{Vessel: v, Expiry: state, DaysLeft: left, Warning: domain.ExpiryFlag(state), WarnDays: days, WarnVersion: w.Version}, nil
}

// GetVessel đọc một phương tiện.
func (s *Service) GetVessel(ctx context.Context, id uuid.UUID) (VesselView, error) {
	var out VesselView
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = vesselView(ctx, tx, id, s.Clock.Now())
		return e
	})
	return out, err
}

// ListVessels liệt kê danh mục phương tiện kèm cảnh báo đăng kiểm.
func (s *Service) ListVessels(ctx context.Context, status string) ([]VesselView, error) {
	var out []VesselView
	err := s.read(ctx, func(tx pgx.Tx) error {
		vs, err := store.ListVessels(ctx, tx, status)
		if err != nil {
			return err
		}
		now := s.Clock.Now()
		for _, v := range vs {
			d, err := decorate(ctx, tx, v, now)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return nil
	})
	return out, err
}

// Alerts là cảnh báo vận hành: chuyến xuống ghe quá N giờ chưa cân, phương tiện hết / sắp hết hạn đăng kiểm.
type Alerts struct {
	UnweighedHours   int
	UnweighedVersion string
	Unweighed        []store.Trip
	Vessels          []VesselView
}

// Alerts đọc cảnh báo (proto nhắc ghe chưa cân > 48 giờ, GH-02 quét đăng kiểm) — chỉ cảnh báo.
func (s *Service) Alerts(ctx context.Context) (Alerts, error) {
	var out Alerts
	err := s.read(ctx, func(tx pgx.Tx) error {
		now := s.Clock.Now()
		h, err := param(ctx, tx, port.ParamUnweighedAlertHours, now)
		if err != nil {
			return err
		}
		out.UnweighedHours, out.UnweighedVersion = int(h.Value.IntPart()), h.Version
		if out.Unweighed, err = store.UnweighedTrips(ctx, tx, now.Add(-time.Duration(out.UnweighedHours)*time.Hour)); err != nil {
			return err
		}
		vs, err := store.ListVessels(ctx, tx, "active")
		if err != nil {
			return err
		}
		for _, v := range vs {
			d, err := decorate(ctx, tx, v, now)
			if err != nil {
				return err
			}
			if d.Warning != "" {
				out.Vessels = append(out.Vessels, d)
			}
		}
		return nil
	}, RoleDispatcher)
	return out, err
}
