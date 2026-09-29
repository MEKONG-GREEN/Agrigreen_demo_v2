package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ManualTicketInput là phiếu nhập tay kèm bằng chứng (CUSTOMER_IN khi chưa tích
// hợp hệ cân của khách — C-5; HUB_IN/HUB_OUT chỉ khi cân hỏng — Q-MKG-04).
type ManualTicketInput struct {
	ID               *uuid.UUID
	LotRef           uuid.UUID
	Point            port.WeighPoint
	Route            port.Route
	MassKg           mass.Kg
	MoisturePct      string
	MoistureState    string
	WeighedAt        time.Time
	EvidenceRef      *uuid.UUID
	CustomerRef      *uuid.UUID
	Reason           string
	Supersedes       *uuid.UUID
	CorrectionReason string
	IdemKey          string
}

// TicketResult là phiếu vừa ghi kèm đối soát tự động (nếu lô đã đủ phiếu).
type TicketResult struct {
	Ticket         port.Ticket
	Reconciliation *store.Reconciliation
}

// RecordManual ghi phiếu tay (scale_supervisor) và — nếu lô đủ phiếu — đối soát
// trong CÙNG giao dịch (tranh chấp mở ngay khi vượt dung sai).
func (s *Service) RecordManual(ctx context.Context, in ManualTicketInput) (res TicketResult, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleScaleSupervisor)
	if err != nil {
		return res, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return res, false, err
	}
	id := ids.New()
	if in.ID != nil {
		id = *in.ID
	}
	d := port.TicketDraft{
		ID: id, IdemKey: in.IdemKey, LotRef: in.LotRef, Point: in.Point, Route: in.Route, Source: port.SourceManualEvidence,
		MassKg: in.MassKg, MoisturePct: in.MoisturePct, MoistureState: in.MoistureState, EventTime: in.WeighedAt,
		EvidenceRef: in.EvidenceRef, CustomerRef: in.CustomerRef, ActorRef: p.ActorRef, Supersedes: in.Supersedes,
		CorrectionReason: in.CorrectionReason, Reason: in.Reason,
	}
	if d.Route == "" {
		d.Route = port.RouteHub
	}
	if d.EventTime.IsZero() {
		d.EventTime = s.Clock.Now()
	}
	if d.EventTime.After(s.Clock.Now().Add(5 * time.Minute)) {
		return res, false, problem.Validation.WithField("weighed_at", "không được ở tương lai")
	}
	if err := domain.NormalizeDraft(&d); err != nil {
		return res, false, mapErr(err)
	}
	run := func() error {
		return tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
			t, rep, err := recordTx(ctx, tx, p.TenantID, d, nil)
			if err != nil {
				return err
			}
			res.Ticket, replayed = t, rep
			if rep {
				res.Reconciliation, err = store.LatestReconciliation(ctx, tx, t.LotRef)
				return err
			}
			if t.Point == port.PointCustomerIn {
				r, _, err := s.reconcileTx(ctx, tx, p, t.LotRef, t.IdemKey+"#recon", true)
				res.Reconciliation = r
				return err
			}
			return nil
		})
	}
	err = run()
	if errors.Is(err, ledger.ErrDuplicate) {
		err = run()
	}
	return res, replayed, mapErr(err)
}

// Ticket đọc một phiếu.
func (s *Service) Ticket(ctx context.Context, id uuid.UUID) (port.Ticket, error) {
	var out port.Ticket
	err := s.read(ctx, func(tx pgx.Tx) error {
		t, err := store.TicketByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrTicketNotFound
		}
		out = *t
		return nil
	})
	return out, err
}

// LotTickets trả mọi phiếu của lô (kể cả phiếu đã bị bù) và bảng chiếu hiện hành.
func (s *Service) LotTickets(ctx context.Context, lot uuid.UUID) ([]port.Ticket, *store.LotMass, error) {
	var ts []port.Ticket
	var lm *store.LotMass
	err := s.read(ctx, func(tx pgx.Tx) error {
		var err error
		if ts, err = store.TicketsOfLot(ctx, tx, lot); err != nil {
			return err
		}
		lm, err = store.GetLotMass(ctx, tx, lot, false)
		if err == nil && (lm == nil || len(ts) == 0) {
			return ErrLotNotFound
		}
		return err
	})
	return ts, lm, err
}

// SetTerms đặt cách tính giá của lô (reconciliation_officer) — tạm tới G4 orders.
func (s *Service) SetTerms(ctx context.Context, lot uuid.UUID, basis port.PricingBasis, contract, customer *uuid.UUID, reason, idem string) (t store.Terms, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleReconciliationOfficer)
	if err != nil {
		return t, false, err
	}
	if err := requireIdem(idem); err != nil {
		return t, false, err
	}
	if basis != port.PricingPerTonne && basis != port.PricingPerBale {
		return t, false, problem.Validation.WithField("pricing_basis", "per_tonne hoặc per_bale")
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.GetTerms(ctx, tx, lot)
		if err != nil {
			return err
		}
		var sup *uuid.UUID
		var why *string
		if cur != nil {
			if !domain.ValidReason(reason) {
				return problem.Validation.WithField("reason", "đổi thuộc tính cần lý do 10..500 ký tự")
			}
			sup, why = &cur.ID, &reason
		}
		t = store.Terms{ID: ids.New(), LotRef: lot, Basis: basis, ContractRef: contract, CustomerRef: customer}
		return insert(store.InsertTerms(ctx, tx, p.TenantID, &t, sup, why, p.ActorRef, s.Clock.Now(), idem))
	}, func(tx pgx.Tx) (bool, error) {
		l, err := store.TermsByIdem(ctx, tx, idem)
		if err != nil || l == nil {
			return false, err
		}
		cur, err := store.GetTerms(ctx, tx, *l)
		if cur != nil {
			t = *cur
		}
		return true, err
	})
	return t, replayed, err
}

// RebuildProjections dựng lại mọi bảng chiếu dẫn xuất từ sổ của tenant (R-22):
// lot_mass_current, lot_yield_current, dispute_current.
func (s *Service) RebuildProjections(ctx context.Context) (map[string]int64, error) {
	p, err := authz.Require(ctx, port.RoleReconciliationOfficer)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		n, err := store.RebuildLotMass(ctx, tx, p.TenantID)
		if err != nil {
			return err
		}
		out["lot_mass_current"] = n
		if n, err = store.RebuildYield(ctx, tx, p.TenantID); err != nil {
			return err
		}
		out["lot_yield_current"] = n
		n, err = store.RebuildDisputes(ctx, tx, p.TenantID)
		out["dispute_current"] = n
		return err
	})
	return out, mapErr(err)
}
