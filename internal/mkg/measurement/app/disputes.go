package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// DisputeActionInput là một thao tác trên hồ sơ tranh chấp.
type DisputeActionInput struct {
	DisputeID    uuid.UUID
	Action       string // add_evidence | start_review | resolve | cancel
	EvidenceRefs []uuid.UUID
	Resolution   string
	AgreedKg     *mass.Kg
	Note         string
	IdemKey      string
}

// DisputeView là hồ sơ kèm lịch sử.
type DisputeView struct {
	Dispute store.Dispute
	Events  []store.DisputeEvent
}

// ActOnDispute áp một thao tác (dispute_officer) theo vòng đời
// open → under_review → resolved | cancelled; bằng chứng thêm khi còn mở.
func (s *Service) ActOnDispute(ctx context.Context, in DisputeActionInput) (v DisputeView, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleDisputeOfficer)
	if err != nil {
		return v, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return v, false, err
	}
	switch in.Action {
	case domain.ActAddEvidence:
		if len(in.EvidenceRefs) == 0 {
			return v, false, problem.Validation.WithField("evidence_refs", "cần ít nhất một tệp bằng chứng")
		}
	case domain.ActStartReview:
	case domain.ActResolve:
		switch in.Resolution {
		case "customer_weight", "mkg_weight":
		case "agreed_weight":
			if in.AgreedKg == nil {
				return v, false, problem.Validation.WithField("agreed_kg", "bắt buộc khi agreed_weight")
			}
		default:
			return v, false, problem.Validation.WithField("resolution", "customer_weight, mkg_weight hoặc agreed_weight")
		}
		if !domain.ValidReason(in.Note) {
			return v, false, problem.Validation.WithField("note", "kết luận cần ghi chú 10..500 ký tự")
		}
	case domain.ActCancel:
		if !domain.ValidReason(in.Note) {
			return v, false, problem.Validation.WithField("note", "hủy cần lý do 10..500 ký tự")
		}
	default:
		return v, false, problem.Validation.WithField("action", "add_evidence, start_review, resolve hoặc cancel")
	}
	load := func(tx pgx.Tx, id uuid.UUID) error {
		d, err := store.GetDispute(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if d == nil {
			return ErrDisputeNotFound
		}
		v.Dispute = *d
		v.Events, err = store.DisputeEvents(ctx, tx, id)
		return err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.GetDispute(ctx, tx, in.DisputeID, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrDisputeNotFound
		}
		next, err := domain.NextDisputeStatus(cur.Status, in.Action)
		if err != nil {
			return mapErr(err)
		}
		e := store.DisputeEvent{ID: ids.New(), DisputeID: cur.ID, LotRef: cur.LotRef, Action: in.Action, StatusAfter: next,
			EvidenceRefs: in.EvidenceRefs, AgreedKg: in.AgreedKg, Note: reasonPtr(in.Note), EventTime: s.Clock.Now(),
			ActorRef: p.ActorRef, IdemKey: in.IdemKey}
		if in.Resolution != "" && in.Action == domain.ActResolve {
			r := in.Resolution
			e.Resolution = &r
		}
		if err := insert(store.InsertDisputeEvent(ctx, tx, p.TenantID, &e)); err != nil {
			return err
		}
		if err := store.ApplyDisputeEvent(ctx, tx, p.TenantID, e); err != nil {
			return err
		}
		return load(tx, cur.ID)
	}, func(tx pgx.Tx) (bool, error) {
		id, err := store.DisputeEventByIdem(ctx, tx, in.IdemKey)
		if err != nil || id == nil {
			return false, err
		}
		return true, load(tx, *id)
	})
	return v, replayed, err
}

// Dispute đọc một hồ sơ.
func (s *Service) Dispute(ctx context.Context, id uuid.UUID) (DisputeView, error) {
	var v DisputeView
	err := s.read(ctx, func(tx pgx.Tx) error {
		d, err := store.GetDispute(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if d == nil {
			return ErrDisputeNotFound
		}
		v.Dispute = *d
		v.Events, err = store.DisputeEvents(ctx, tx, id)
		return err
	})
	return v, err
}

// Disputes liệt kê hồ sơ (con trỏ theo id).
func (s *Service) Disputes(ctx context.Context, status string, after uuid.UUID, limit int) ([]store.Dispute, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []store.Dispute
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListDisputes(ctx, tx, status, after, limit)
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Tham số có phiên bản
// ---------------------------------------------------------------------------

// ProposeTolerance đề xuất phiên bản dung sai (reconciliation_officer). Hiệu lực khi
// người KHÁC có vai tolerance_approver duyệt; áp theo thời điểm cân ≥ effective_from.
func (s *Service) ProposeTolerance(ctx context.Context, t domain.Tolerance, reason, idem string) (store.ParamVersion, bool, error) {
	if err := t.Validate(); err != nil {
		return store.ParamVersion{}, false, mapErr(err)
	}
	return s.proposeParam(ctx, store.ParamTolerance, t.EffectiveFrom, reason, idem, func(tx pgx.Tx, tid, id, actor uuid.UUID, at time.Time) (int64, error) {
		seq, _, err := store.InsertTolerance(ctx, tx, tid, id, t, reason, actor, at, idem)
		return seq, err
	})
}

// ProposeLossNorm đề xuất phiên bản định mức hao hụt sấy (reconciliation_officer).
func (s *Service) ProposeLossNorm(ctx context.Context, n domain.LossNorm, reason, idem string) (store.ParamVersion, bool, error) {
	if err := n.Validate(); err != nil {
		return store.ParamVersion{}, false, mapErr(err)
	}
	for _, x := range []decimal.Decimal{n.DefaultLoss, n.AlertPct} {
		if x.IsNegative() || x.GreaterThan(decimal.NewFromInt(100)) {
			return store.ParamVersion{}, false, problem.Validation.WithField("default_loss_pct", "0..100")
		}
	}
	return s.proposeParam(ctx, store.ParamLossNorm, n.EffectiveFrom, reason, idem, func(tx pgx.Tx, tid, id, actor uuid.UUID, at time.Time) (int64, error) {
		seq, _, err := store.InsertLossNorm(ctx, tx, tid, id, n, reason, actor, at, idem)
		return seq, err
	})
}

func (s *Service) proposeParam(ctx context.Context, kind string, eff time.Time, reason, idem string,
	ins func(tx pgx.Tx, tid, id, actor uuid.UUID, at time.Time) (int64, error)) (pv store.ParamVersion, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleReconciliationOfficer)
	if err != nil {
		return pv, false, err
	}
	if err := requireIdem(idem); err != nil {
		return pv, false, err
	}
	if eff.IsZero() {
		return pv, false, problem.Validation.WithField("effective_from", "bắt buộc, RFC 3339")
	}
	if !domain.ValidReason(reason) {
		return pv, false, problem.Validation.WithField("reason", "lý do / căn cứ 10..500 ký tự")
	}
	load := func(tx pgx.Tx, id uuid.UUID) error {
		v, err := store.ParamByID(ctx, tx, kind, id)
		if v != nil {
			pv = *v
		}
		return err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		id := ids.New()
		seq, err := ins(tx, p.TenantID, id, p.ActorRef, s.Clock.Now())
		if err != nil {
			return insert(err)
		}
		if err := store.InsertParamCurrent(ctx, tx, p.TenantID, kind, id, seq, eff, p.ActorRef); err != nil {
			return err
		}
		return load(tx, id)
	}, func(tx pgx.Tx) (bool, error) {
		id, err := store.ParamIDByIdem(ctx, tx, kind, idem)
		if err != nil || id == nil {
			return false, err
		}
		return true, load(tx, *id)
	})
	return pv, replayed, err
}

// DecideParam duyệt / từ chối một phiên bản (tolerance_approver ≠ người đặt, R-56).
func (s *Service) DecideParam(ctx context.Context, kind string, seq int64, decision, reason, idem string) (pv store.ParamVersion, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleToleranceApprover)
	if err != nil {
		return pv, false, err
	}
	if err := requireIdem(idem); err != nil {
		return pv, false, err
	}
	if decision != "approved" && decision != "rejected" {
		return pv, false, problem.Validation.WithField("decision", "approved hoặc rejected")
	}
	if decision == "rejected" && !domain.ValidReason(reason) {
		return pv, false, problem.Validation.WithField("reason", "từ chối cần lý do 10..500 ký tự")
	}
	load := func(tx pgx.Tx) error {
		v, err := store.ParamBySeq(ctx, tx, kind, seq, false)
		if v != nil {
			pv = *v
		}
		return err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.ParamBySeq(ctx, tx, kind, seq, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrVersionNotFound
		}
		if cur.Status != "pending" {
			return ErrNotPending
		}
		if err := authz.EnsureDistinctApproverFor(cur.ProposedBy, p, port.RoleToleranceApprover); err != nil {
			return err
		}
		if err := insert(store.InsertParamDecision(ctx, tx, p.TenantID, ids.New(), kind, cur.ID, cur.Seq, decision, reasonPtr(reason),
			cur.ProposedBy, p.ActorRef, s.Clock.Now(), idem)); err != nil {
			return err
		}
		return load(tx)
	}, func(tx pgx.Tx) (bool, error) {
		ok, err := store.ParamDecisionExists(ctx, tx, idem)
		if err != nil || !ok {
			return false, err
		}
		return true, load(tx)
	})
	return pv, replayed, err
}

// Params liệt kê phiên bản tham số.
func (s *Service) Params(ctx context.Context, kind string) ([]store.ParamVersion, error) {
	var out []store.ParamVersion
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.Params(ctx, tx, kind)
		return e
	})
	return out, err
}
