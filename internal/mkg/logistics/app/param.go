package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Tham số có phiên bản của logistics (g4-conventions §1.2): dispatcher đề xuất ≠ chief_accountant duyệt
// (R-56 ở app/ và CHECK DB). Mặc định #0 là số của prototype, TẠM, CHỜ KHÁCH.

// ParamInput là một đề xuất phiên bản tham số.
type ParamInput struct {
	Key           string
	Value         decimal.Decimal
	EffectiveFrom string // YYYY-MM-DD, rỗng = hôm nay (giờ Việt Nam)
	Provisional   bool
	Reason        string
	IdemKey       string
}

// ParamResult là kết quả một thao tác tham số.
type ParamResult struct {
	Param store.Param
	Event store.ParamEvent
}

// ProposeParam đề xuất một phiên bản tham số (pending tới khi kế toán trưởng KHÁC duyệt).
func (s *Service) ProposeParam(ctx context.Context, in ParamInput) (ParamResult, bool, error) {
	p, err := authz.Require(ctx, RoleDispatcher)
	if err != nil {
		return ParamResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return ParamResult{}, false, err
	}
	spec, ok := domain.Spec(in.Key)
	if !ok {
		return ParamResult{}, false, ErrParamNotFound
	}
	if err := domain.CheckParamValue(spec, in.Value); err != nil {
		return ParamResult{}, false, mapErr(err)
	}
	reason, err := domain.CheckReason("reason", in.Reason, true)
	if err != nil {
		return ParamResult{}, false, mapErr(err)
	}
	now := s.Clock.Now()
	eff := day(now)
	if in.EffectiveFrom != "" {
		if eff, err = time.Parse(time.DateOnly, in.EffectiveFrom); err != nil {
			return ParamResult{}, false, problem.Validation.WithField("effective_from", "YYYY-MM-DD")
		}
	}
	var res ParamResult
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockParamKey(ctx, tx, p.TenantID, in.Key); err != nil {
			return err
		}
		n, err := store.NextParamSeq(ctx, tx, in.Key)
		if err != nil {
			return err
		}
		e := store.ParamEvent{ID: ids.New(), TenantID: p.TenantID, Key: in.Key, VersionID: ids.New(), VersionSeq: n, Kind: "proposed",
			Value: in.Value, EffectiveFrom: eff, Provisional: in.Provisional, ProposerRef: p.ActorRef, Reason: &reason, EventTime: now,
			ActorRef: p.ActorRef, IdemKey: in.IdemKey}
		return applyParam(ctx, tx, &e, &res)
	}, replayParam(ctx, in.IdemKey, &res))
	return res, replayed, err
}

// DecideParam duyệt / từ chối một phiên bản đang chờ (chief_accountant, KHÁC người đề xuất).
func (s *Service) DecideParam(ctx context.Context, key string, version uuid.UUID, approve bool, reason, idem string) (ParamResult, bool, error) {
	p, err := authz.Require(ctx, RoleChiefAccountant)
	if err != nil {
		return ParamResult{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return ParamResult{}, false, err
	}
	r, err := domain.CheckReason("reason", reason, !approve)
	if err != nil {
		return ParamResult{}, false, mapErr(err)
	}
	var res ParamResult
	now := s.Clock.Now()
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.GetParam(ctx, tx, key, version, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrParamNotFound
		}
		if cur.Status != "pending" {
			return ErrParamDecided
		}
		if err := authz.EnsureDistinctApproverFor(cur.ProposedBy, p, RoleChiefAccountant); err != nil {
			return err
		}
		kind := "approved"
		if !approve {
			kind = "rejected"
		}
		e := store.ParamEvent{ID: ids.New(), TenantID: p.TenantID, Key: key, VersionID: version, VersionSeq: cur.VersionSeq, Kind: kind,
			Value: cur.Value, EffectiveFrom: cur.EffectiveFrom, Provisional: cur.Provisional, ProposerRef: cur.ProposedBy,
			Reason: strPtr(r), EventTime: now, ActorRef: p.ActorRef, IdemKey: idem}
		return applyParam(ctx, tx, &e, &res)
	}, replayParam(ctx, idem, &res))
	return res, replayed, err
}

func applyParam(ctx context.Context, tx pgx.Tx, e *store.ParamEvent, res *ParamResult) error {
	if err := insert(store.InsertParamEvent(ctx, tx, e)); err != nil {
		return err
	}
	if err := store.ApplyParamEvent(ctx, tx, *e); err != nil {
		return err
	}
	cur, err := store.GetParam(ctx, tx, e.Key, e.VersionID, false)
	if err != nil {
		return err
	}
	res.Param, res.Event = *cur, *e
	return nil
}

func replayParam(ctx context.Context, idem string, res *ParamResult) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		e, err := store.ParamEventByIdem(ctx, tx, idem)
		if err != nil || e == nil {
			return false, err
		}
		cur, err := store.GetParam(ctx, tx, e.Key, e.VersionID, false)
		if err != nil || cur == nil {
			return false, err
		}
		res.Param, res.Event = *cur, *e
		return true, nil
	}
}

// ParamsView là các phiên bản của một tham số và giá trị hiệu lực hôm nay.
type ParamsView struct {
	Key       string
	Versions  []store.Param
	Effective ParamValue
	Default   decimal.Decimal
}

// Params đọc các phiên bản của tham số key (điều phối, kế toán trưởng).
func (s *Service) Params(ctx context.Context, key string) (ParamsView, error) {
	spec, ok := domain.Spec(key)
	if !ok {
		return ParamsView{}, ErrParamNotFound
	}
	v := ParamsView{Key: key, Default: spec.Default}
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		if v.Versions, e = store.Params(ctx, tx, key); e != nil {
			return e
		}
		v.Effective, e = param(ctx, tx, key, s.Clock.Now())
		return e
	}, RoleDispatcher, RoleChiefAccountant)
	return v, err
}
