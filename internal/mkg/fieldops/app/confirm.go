package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Kênh ghi xác nhận của HTX.
const (
	ChannelOnBehalf = "mkg_on_behalf" // G3: nhân viên MKG ghi thay (có bằng chứng nếu có)
	ChannelPlatform = "platform"      // G3-2: HTX xác nhận trên App HTX, quay về qua Application API
)

// ConfirmInput là xác nhận (hoặc tranh chấp) của HTX với bảng kê một thửa.
type ConfirmInput struct {
	OrderID  uuid.UUID
	PlotRef  uuid.UUID
	Outcome  string // confirmed | disputed
	HTXRolls *int   // bắt buộc khi disputed; confirmed → = số MKG
	Reason   string
	// StatementRevision: phiên bản bảng kê HTX đã xem; nil = hiện hành.
	StatementRevision *int
	EvidenceRef       *uuid.UUID
	EventTime         time.Time
	IdemKey           string
}

// ConfirmResult là kết quả ghi xác nhận.
type ConfirmResult struct {
	Confirmation store.Confirmation
	Recon        store.Recon
}

// RecordConfirmation ghi xác nhận của HTX do nhân viên MKG ghi thay (G3).
// RB-6 chiều mua: MKG đếm, HTX xác nhận hoặc tranh chấp số cuộn.
func (s *Service) RecordConfirmation(ctx context.Context, in ConfirmInput) (ConfirmResult, bool, error) {
	p, err := authz.Require(ctx, RoleCollector, RoleSupervisor)
	if err != nil {
		return ConfirmResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return ConfirmResult{}, false, err
	}
	if in.Outcome != "confirmed" && in.Outcome != "disputed" {
		return ConfirmResult{}, false, problem.Validation.WithField("outcome", "confirmed | disputed")
	}
	reason, err := domain.CheckReason("reason", in.Reason, in.Outcome == "disputed")
	if err != nil {
		return ConfirmResult{}, false, mapErr(err)
	}
	if in.Outcome == "disputed" && (in.HTXRolls == nil || *in.HTXRolls < 0) {
		return ConfirmResult{}, false, problem.Validation.WithField("htx_rolls", "bắt buộc khi disputed, ≥ 0")
	}
	if err := domain.CheckEventTime("event_time", in.EventTime, s.Clock.Now()); err != nil {
		return ConfirmResult{}, false, mapErr(err)
	}
	var res ConfirmResult
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		o, err := store.GetOrder(ctx, tx, in.OrderID, true)
		if err != nil {
			return err
		}
		if o == nil {
			return ErrOrderNotFound
		}
		if !o.HasPlot(in.PlotRef) {
			return ErrPlotNotInOrder
		}
		r, err := store.GetRecon(ctx, tx, in.OrderID, in.PlotRef)
		if err != nil {
			return err
		}
		if r == nil {
			return ErrNothingToConfirm
		}
		if in.StatementRevision != nil && *in.StatementRevision != r.StatementRevision {
			return ErrStatementStale
		}
		htx := r.MKGRolls
		if in.Outcome == "disputed" {
			htx = *in.HTXRolls
			if htx == r.MKGRolls {
				return problem.Validation.WithField("htx_rolls", "bằng số MKG — dùng outcome confirmed")
			}
		} else if in.HTXRolls != nil && *in.HTXRolls != r.MKGRolls {
			return problem.Validation.WithField("htx_rolls", "khác số MKG — dùng outcome disputed kèm lý do")
		}
		c := store.Confirmation{
			ID: ids.New(), TenantID: p.TenantID, OrderID: in.OrderID, PlotRef: in.PlotRef, HTXRef: o.HTXRef,
			StatementRef: r.StatementRef, StatementRevision: r.StatementRevision, RollCountID: r.MKGRollCountID,
			MKGRolls: r.MKGRolls, Outcome: in.Outcome, HTXRolls: htx, Reason: strPtr(reason), Channel: ChannelOnBehalf,
			EvidenceRef: in.EvidenceRef, EventTime: in.EventTime.UTC(), ActorRef: p.ActorRef, IdemKey: in.IdemKey,
		}
		if err := insert(store.InsertConfirmation(ctx, tx, &c)); err != nil {
			return err
		}
		out, rev := c.Outcome, c.StatementRevision
		r.HTXRolls, r.Outcome, r.ConfirmationID, r.ConfirmedRevision = &htx, &out, &c.ID, &rev
		r.Status = domain.ReconStatus(r.MKGRolls, r.HTXRolls)
		if err := store.PutRecon(ctx, tx, p.TenantID, *r); err != nil {
			return err
		}
		res.Confirmation, res.Recon = c, *r
		return nil
	}, func(tx pgx.Tx) (bool, error) {
		c, err := store.ConfirmationByIdem(ctx, tx, in.IdemKey)
		if err != nil || c == nil {
			return false, err
		}
		r, err := store.GetRecon(ctx, tx, c.OrderID, c.PlotRef)
		if err != nil || r == nil {
			return false, err
		}
		res.Confirmation, res.Recon = *c, *r
		return true, nil
	})
	return res, replayed, err
}

// Confirmations liệt kê xác nhận của HTX trên một lệnh gom.
func (s *Service) Confirmations(ctx context.Context, orderID uuid.UUID) ([]store.Confirmation, error) {
	var out []store.Confirmation
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.Confirmations(ctx, tx, orderID)
		return e
	})
	return out, err
}

// ReconPage là một trang đối soát chiều mua.
type ReconPage struct {
	Items []store.Recon
	Next  *[2]uuid.UUID
}

// Reconciliation liệt kê đối soát chiều mua MKG ↔ HTX (RB-6).
func (s *Service) Reconciliation(ctx context.Context, orderID *uuid.UUID, status string, after [2]uuid.UUID, limit int) (ReconPage, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	switch status {
	case "", domain.ReconPending, domain.ReconMatch, domain.ReconMismatch:
	default:
		return ReconPage{}, problem.Validation.WithField("status", "pending | match | mismatch")
	}
	var items []store.Recon
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListRecon(ctx, tx, orderID, status, after[0], after[1], limit+1)
		return e
	})
	var pg ReconPage
	if err != nil {
		return pg, err
	}
	if len(items) > limit {
		items = items[:limit]
		last := [2]uuid.UUID{items[limit-1].OrderID, items[limit-1].PlotRef}
		pg.Next = &last
	}
	pg.Items = items
	return pg, nil
}

// ---------------------------------------------------------------------------
// Ca tổ gom
// ---------------------------------------------------------------------------

// ShiftInput là mở/đóng một ca tổ gom trên lệnh gom.
type ShiftInput struct {
	OrderID       uuid.UUID
	CrewRef       uuid.UUID
	Kind          string // opened | closed
	Headcount     *int
	EquipmentRefs []string
	EventTime     time.Time
	DeviceID      *string
	IdemKey       string
}

// RecordShift ghi sổ ca tổ gom (field_collector, field_supervisor).
func (s *Service) RecordShift(ctx context.Context, in ShiftInput) (store.CrewShift, bool, error) {
	p, err := authz.Require(ctx, RoleCollector, RoleSupervisor)
	if err != nil {
		return store.CrewShift{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return store.CrewShift{}, false, err
	}
	v := problem.Validation
	bad := false
	if in.CrewRef == uuid.Nil {
		v, bad = v.WithField("crew_ref", "bắt buộc, UUID"), true
	}
	if in.Kind != "opened" && in.Kind != "closed" {
		v, bad = v.WithField("kind", "opened | closed"), true
	}
	if in.Kind == "opened" && (in.Headcount == nil || *in.Headcount < 1 || *in.Headcount > 200) {
		v, bad = v.WithField("headcount", "bắt buộc khi mở ca, 1..200"), true
	}
	if len(in.EquipmentRefs) > 50 {
		v, bad = v.WithField("equipment_refs", "tối đa 50"), true
	}
	if bad {
		return store.CrewShift{}, false, v
	}
	if err := domain.CheckEventTime("event_time", in.EventTime, s.Clock.Now()); err != nil {
		return store.CrewShift{}, false, mapErr(err)
	}
	var out store.CrewShift
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		o, err := store.GetOrder(ctx, tx, in.OrderID, false)
		if err != nil {
			return err
		}
		if o == nil {
			return ErrOrderNotFound
		}
		if !domain.Countable(o.Status) {
			return ErrOrderNotOpen
		}
		out = store.CrewShift{
			ID: ids.New(), TenantID: p.TenantID, CrewRef: in.CrewRef, OrderID: in.OrderID, Kind: in.Kind, Headcount: in.Headcount,
			EquipmentRefs: in.EquipmentRefs, EventTime: in.EventTime.UTC(), ActorRef: p.ActorRef, DeviceID: in.DeviceID, IdemKey: in.IdemKey,
		}
		return insert(store.InsertCrewShift(ctx, tx, &out))
	}, func(tx pgx.Tx) (bool, error) {
		c, err := store.CrewShiftByIdem(ctx, tx, in.IdemKey)
		if c != nil {
			out = *c
		}
		return c != nil, err
	})
	return out, replayed, err
}

// Shifts liệt kê ca tổ gom của một lệnh gom.
func (s *Service) Shifts(ctx context.Context, orderID uuid.UUID) ([]store.CrewShift, error) {
	var out []store.CrewShift
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.CrewShifts(ctx, tx, orderID)
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------
// kg/cuộn học theo HTX
// ---------------------------------------------------------------------------

// KgPerRoll trả kg/cuộn của một HTX theo chuỗi dự phòng HTX → hệ thống → cấu hình → 20.
// Chỉ dùng nội bộ MKG (ước tấn, costing G4) — KHÔNG BAO GIỜ qua van.
func (s *Service) KgPerRoll(ctx context.Context, htx uuid.UUID) (domain.KgPerRoll, error) {
	var r domain.KgPerRoll
	err := s.read(ctx, func(tx pgx.Tx) error {
		own, err := store.Samples(ctx, tx, &htx, domain.LearnWindow)
		if err != nil {
			return err
		}
		all, err := store.Samples(ctx, tx, nil, domain.LearnWindow)
		if err != nil {
			return err
		}
		r = domain.ResolveKgPerRoll(own, all, s.ConfiguredKgPerRoll)
		return nil
	})
	return r, err
}

// RefreshWeighings kéo khối lượng cân của các lô gom đã xếp ghe/xe chưa có số
// cân (qua port WeighedMass của mkg/measurement) vào bảng đệm học kg/cuộn.
// Trả số lô vừa có khối lượng. Vai field_supervisor.
func (s *Service) RefreshWeighings(ctx context.Context) (int, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return 0, err
	}
	if s.Weighed == nil {
		return 0, nil
	}
	var lots []store.Unweighed
	if err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		lots, e = store.UnweighedLots(ctx, tx, 500)
		return e
	}); err != nil {
		return 0, err
	}
	n := 0
	for _, u := range lots {
		kg, at, ok, err := s.Weighed.WeighedKg(ctx, p.TenantID, u.OrderID)
		if err != nil {
			return n, err
		}
		if !ok || kg.Decimal().Sign() <= 0 {
			continue
		}
		if err := s.read(ctx, func(tx pgx.Tx) error { return store.PutWeighed(ctx, tx, p.TenantID, u, kg, at) }); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
