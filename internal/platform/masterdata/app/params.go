package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// ProposeInput là dữ liệu vào của Propose.
type ProposeInput struct {
	Key              string
	Value            *decimal.Decimal
	Text             *string
	Unit             string
	EffectiveFrom    time.Time
	SourceDoc        string // số/ngày văn bản ban hành
	CorrectionReason *string
	IdemKey          string // = Idempotency-Key của request
}

// Propose tạo phiên bản nháp của một tham số (vai param_setter). Số phiên bản
// tăng dần theo khóa. Gọi lặp cùng IdemKey trả lại phiên bản cũ, replayed = true.
func (s *Service) Propose(ctx context.Context, in ProposeInput) (domain.ParamVersion, bool, error) {
	p, err := authz.Require(ctx, port.RoleParamSetter)
	if err != nil {
		return domain.ParamVersion{}, false, err
	}
	if in.IdemKey == "" {
		return domain.ParamVersion{}, false, problem.IdempotencyRequired
	}
	if in.CorrectionReason != nil {
		r := strings.TrimSpace(*in.CorrectionReason)
		in.CorrectionReason = &r
	}
	v := domain.ParamVersion{
		ID: ids.New(), TenantID: p.TenantID, Key: in.Key, Value: in.Value, Text: in.Text, Unit: strings.TrimSpace(in.Unit),
		EffectiveFrom: in.EffectiveFrom.UTC(), SourceDoc: strings.TrimSpace(in.SourceDoc), CorrectionReason: in.CorrectionReason,
		Status: domain.StatusDraft, SetterRef: p.ActorRef, IdemKey: in.IdemKey,
	}
	if err := v.ValidateProposal(); err != nil {
		return v, false, asProblem(err)
	}
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.VersionByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			v, replayed = *prev, true
			return nil
		}
		if err := store.LockParamKey(ctx, tx, p.TenantID, v.Key); err != nil {
			return err
		}
		if same, err := store.ApprovedSameEffective(ctx, tx, v.Key, v.EffectiveFrom); err != nil {
			return err
		} else if same != nil && v.CorrectionReason == nil {
			return ErrCorrectionRequired
		}
		n, err := store.NextVersion(ctx, tx, v.Key)
		if err != nil {
			return err
		}
		v.Version = n
		return store.InsertVersion(ctx, tx, &v)
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		// Hai request cùng khóa chạy song song: bên thua đọc lại phiên bản của bên thắng.
		var prev *domain.ParamVersion
		if rerr := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.VersionByIdemKey(ctx, tx, in.IdemKey)
			return e
		}); rerr == nil && prev != nil {
			return *prev, true, nil
		}
	}
	return v, replayed, err
}

func (s *Service) lockedVersion(ctx context.Context, tx pgx.Tx, id uuid.UUID) (domain.ParamVersion, error) {
	v, err := store.VersionByID(ctx, tx, id, true)
	if err != nil {
		return domain.ParamVersion{}, err
	}
	if v == nil {
		return domain.ParamVersion{}, ErrVersionNotFound
	}
	return *v, nil
}

// Submit gửi duyệt một phiên bản nháp — chỉ người đặt.
func (s *Service) Submit(ctx context.Context, id uuid.UUID) (domain.ParamVersion, error) {
	p, err := authz.Require(ctx, port.RoleParamSetter)
	if err != nil {
		return domain.ParamVersion{}, err
	}
	now := s.Clock.Now()
	var v domain.ParamVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if v, e = s.lockedVersion(ctx, tx, id); e != nil {
			return e
		}
		switch e := v.CanSubmit(p.ActorRef); {
		case errors.Is(e, domain.ErrNotSetter):
			return ErrNotSetter
		case e != nil:
			return ErrTransition.WithDetail(e.Error())
		}
		if e := store.MarkSubmitted(ctx, tx, v.ID, now); e != nil {
			return e
		}
		v.Status, v.SubmittedAt = domain.StatusSubmitted, &now
		return nil
	})
	return v, err
}

// Approve phê duyệt một phiên bản đã gửi duyệt (vai param_approver, R-56: không
// được là người đặt). Ghi sổ param_approved + outbox trong MỘT giao dịch. Nếu đã có
// bản duyệt cùng effective_from, bản mới là bản ghi bù (supersedes_id).
func (s *Service) Approve(ctx context.Context, id uuid.UUID, note *string) (domain.ParamVersion, domain.Approved, error) {
	p, err := authz.Require(ctx, port.RoleParamApprover)
	if err != nil {
		return domain.ParamVersion{}, domain.Approved{}, err
	}
	now := s.Clock.Now()
	var v domain.ParamVersion
	var a domain.Approved
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if v, e = s.lockedVersion(ctx, tx, id); e != nil {
			return e
		}
		if e := authz.EnsureDistinctApprover(v.SetterRef, p); e != nil {
			return e
		}
		if e := v.CanDecide(); e != nil {
			return ErrTransition.WithDetail(e.Error())
		}
		if e := store.LockParamKey(ctx, tx, p.TenantID, v.Key); e != nil {
			return e
		}
		prev, e := store.ApprovedSameEffective(ctx, tx, v.Key, v.EffectiveFrom)
		if e != nil {
			return e
		}
		if prev != nil && v.CorrectionReason == nil {
			return ErrCorrectionRequired
		}
		if e := store.MarkDecided(ctx, tx, v.ID, domain.StatusApproved, p.ActorRef, now, note); e != nil {
			return e
		}
		approver := p.ActorRef
		v.Status, v.ApproverRef, v.DecidedAt, v.DecisionNote = domain.StatusApproved, &approver, &now, note
		a, e = s.recordApproved(ctx, tx, p.TenantID, v, prev, now)
		return e
	})
	return v, a, err
}

// recordApproved ghi sổ phiên bản đã duyệt và sự kiện pf.masterdata.param.approved.
func (s *Service) recordApproved(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v domain.ParamVersion, prev *domain.Approved, now time.Time) (domain.Approved, error) {
	a := domain.Approved{
		ID: ids.New(), Key: v.Key, Version: v.Version, ParamVersionID: v.ID, Value: v.Value, Text: v.Text, Unit: v.Unit,
		EffectiveFrom: v.EffectiveFrom, SourceDoc: v.SourceDoc, SetterRef: v.SetterRef, ApproverRef: *v.ApproverRef,
		EventTime: now, IdemKey: "approve:" + v.ID.String(),
	}
	if prev != nil {
		a.SupersedesID, a.CorrectionReason = &prev.ID, v.CorrectionReason
	}
	if err := store.InsertApproved(ctx, tx, tid, &a); err != nil {
		return a, ledger.Classify(err)
	}
	pl := port.ParamApprovedV1{
		ParamKey: a.Key, Version: a.Version, ParamVersionID: a.ParamVersionID.String(), Text: a.Text, Unit: a.Unit,
		EffectiveFrom: a.EffectiveFrom.UTC().Format(time.RFC3339), SourceDoc: a.SourceDoc,
		SetterRef: a.SetterRef.String(), ApproverRef: a.ApproverRef.String(), Seq: a.Seq,
	}
	if a.Value != nil {
		sv := a.Value.String()
		pl.Value = &sv
	}
	if a.SupersedesID != nil {
		sid := a.SupersedesID.String()
		pl.SupersedesID = &sid
	}
	err := outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: tid, Type: port.EventParamApproved, SchemaVersion: 1,
		OccurredAt: now, TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: pl,
	})
	return a, err
}

// Reject từ chối một phiên bản đã gửi duyệt (vai param_approver, không được là người đặt).
func (s *Service) Reject(ctx context.Context, id uuid.UUID, note string) (domain.ParamVersion, error) {
	p, err := authz.Require(ctx, port.RoleParamApprover)
	if err != nil {
		return domain.ParamVersion{}, err
	}
	if err := domain.ValidateDecisionNote(note); err != nil {
		return domain.ParamVersion{}, asProblem(err)
	}
	note = strings.TrimSpace(note)
	now := s.Clock.Now()
	var v domain.ParamVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if v, e = s.lockedVersion(ctx, tx, id); e != nil {
			return e
		}
		if e := authz.EnsureDistinctApprover(v.SetterRef, p); e != nil {
			return e
		}
		if e := v.CanDecide(); e != nil {
			return ErrTransition.WithDetail(e.Error())
		}
		if e := store.MarkDecided(ctx, tx, v.ID, domain.StatusRejected, p.ActorRef, now, &note); e != nil {
			return e
		}
		approver := p.ActorRef
		v.Status, v.ApproverRef, v.DecidedAt, v.DecisionNote = domain.StatusRejected, &approver, &now, &note
		return nil
	})
	return v, err
}

var paramViewerRoles = []string{port.RoleParamSetter, port.RoleParamApprover, port.RoleMasterdataAdmin}

// Version đọc một phiên bản trong quy trình duyệt.
func (s *Service) Version(ctx context.Context, id uuid.UUID) (domain.ParamVersion, error) {
	p, err := authz.Require(ctx, paramViewerRoles...)
	if err != nil {
		return domain.ParamVersion{}, err
	}
	var v *domain.ParamVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		v, e = store.VersionByID(ctx, tx, id, false)
		return e
	})
	if err != nil {
		return domain.ParamVersion{}, err
	}
	if v == nil {
		return domain.ParamVersion{}, ErrVersionNotFound
	}
	return *v, nil
}

// Versions liệt kê lịch sử phiên bản (mọi trạng thái), lọc theo khóa và trạng thái.
func (s *Service) Versions(ctx context.Context, key, status string, after uuid.UUID, limit int) (Page[domain.ParamVersion], error) {
	p, err := authz.Require(ctx, paramViewerRoles...)
	if err != nil {
		return Page[domain.ParamVersion]{}, err
	}
	if key != "" {
		if err := domain.ValidateParamKey(key); err != nil {
			return Page[domain.ParamVersion]{}, asProblem(err)
		}
	}
	switch domain.ParamStatus(status) {
	case "", domain.StatusDraft, domain.StatusSubmitted, domain.StatusApproved, domain.StatusRejected:
	default:
		return Page[domain.ParamVersion]{}, problem.Validation.WithField("status", "phải là draft, submitted, approved hoặc rejected")
	}
	limit = clampLimit(limit)
	var vs []domain.ParamVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		vs, e = store.ListVersions(ctx, tx, key, status, after, limit+1)
		return e
	})
	if err != nil {
		return Page[domain.ParamVersion]{}, err
	}
	var pg Page[domain.ParamVersion]
	if len(vs) > limit {
		vs = vs[:limit]
		last := vs[limit-1].ID.String()
		pg.Next = &last
	}
	pg.Items = vs
	return pg, nil
}

// ParamAt trả phiên bản đã duyệt có hiệu lực tại at (Application API, chỉ đọc).
func (s *Service) ParamAt(ctx context.Context, key string, at time.Time) (domain.Approved, error) {
	p, err := reader(ctx)
	if err != nil {
		return domain.Approved{}, err
	}
	if err := domain.ValidateParamKey(key); err != nil {
		return domain.Approved{}, asProblem(err)
	}
	return s.paramAt(ctx, p.TenantID, key, at)
}

func (s *Service) paramAt(ctx context.Context, tid uuid.UUID, key string, at time.Time) (domain.Approved, error) {
	var a *domain.Approved
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		a, e = store.ApprovedAt(ctx, tx, key, at)
		return e
	})
	if err != nil {
		return domain.Approved{}, err
	}
	if a == nil {
		return domain.Approved{}, ErrParamNotFound
	}
	return *a, nil
}
