package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Tham số có phiên bản của fieldops: người đề xuất ≠ người duyệt (R-56, CHECK DB lớp hai),
// cả hai vai field_supervisor (vai MKG riêng, g3-conventions §3). G3 chỉ có
// baling_deadline_days (FM-02) — mặc định 3 ngày của prototype, TẠM, chờ khách.

// ParamInput là một đề xuất phiên bản tham số.
type ParamInput struct {
	Key           string
	Value         decimal.Decimal
	EffectiveFrom string // YYYY-MM-DD, rỗng = hôm nay
	Provisional   bool
	Reason        string
	IdemKey       string
}

// ParamResult là kết quả một thao tác tham số.
type ParamResult struct {
	Param store.Param
	Event store.ParamEvent
}

func validParamKey(k string) bool { return k == ParamBalingDeadlineDays }

// ProposeParam đề xuất một phiên bản tham số (pending tới khi người KHÁC duyệt).
func (s *Service) ProposeParam(ctx context.Context, in ParamInput) (ParamResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return ParamResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return ParamResult{}, false, err
	}
	if !validParamKey(in.Key) {
		return ParamResult{}, false, ErrParamNotFound
	}
	if !in.Value.Equal(in.Value.Truncate(0)) || in.Value.Sign() < 0 || in.Value.GreaterThan(decimal.NewFromInt(60)) {
		return ParamResult{}, false, problem.Validation.WithField("value", "số ngày nguyên 0..60")
	}
	reason, err := domain.CheckReason("reason", in.Reason, true)
	if err != nil {
		return ParamResult{}, false, mapErr(err)
	}
	now := s.Clock.Now()
	eff := domain.Day(now)
	if in.EffectiveFrom != "" {
		if eff, err = time.Parse(dateLayout, in.EffectiveFrom); err != nil {
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
		return s.applyParam(ctx, tx, &e, &res)
	}, s.replayParam(ctx, in.IdemKey, &res))
	return res, replayed, err
}

// DecideParam duyệt / từ chối một phiên bản đang chờ. Người quyết định KHÁC người đề xuất.
func (s *Service) DecideParam(ctx context.Context, key string, version uuid.UUID, approve bool, reason, idem string) (ParamResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
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
		if err := authz.EnsureDistinctApproverFor(cur.ProposedBy, p, RoleSupervisor); err != nil {
			return err
		}
		kind := "approved"
		if !approve {
			kind = "rejected"
		}
		e := store.ParamEvent{ID: ids.New(), TenantID: p.TenantID, Key: key, VersionID: version, VersionSeq: cur.VersionSeq, Kind: kind,
			Value: cur.Value, EffectiveFrom: cur.EffectiveFrom, Provisional: cur.Provisional, ProposerRef: cur.ProposedBy,
			Reason: strPtr(r), EventTime: now, ActorRef: p.ActorRef, IdemKey: idem}
		return s.applyParam(ctx, tx, &e, &res)
	}, s.replayParam(ctx, idem, &res))
	return res, replayed, err
}

func (s *Service) applyParam(ctx context.Context, tx pgx.Tx, e *store.ParamEvent, res *ParamResult) error {
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

func (s *Service) replayParam(ctx context.Context, idem string, res *ParamResult) func(pgx.Tx) (bool, error) {
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
	Key          string
	Versions     []store.Param
	Effective    int
	ParamVersion string // "baling_deadline_days:v<n>" | "provisional-default"
	Provisional  bool
	Default      int
}

// Params đọc các phiên bản của tham số key.
func (s *Service) Params(ctx context.Context, key string) (ParamsView, error) {
	if !validParamKey(key) {
		return ParamsView{}, ErrParamNotFound
	}
	v := ParamsView{Key: key, Default: domain.DefaultBalingDeadlineDays}
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		if v.Versions, e = store.Params(ctx, tx, key); e != nil {
			return e
		}
		v.Effective, v.ParamVersion, v.Provisional, e = balingDays(ctx, tx, domain.Day(s.Clock.Now()))
		return e
	})
	return v, err
}
