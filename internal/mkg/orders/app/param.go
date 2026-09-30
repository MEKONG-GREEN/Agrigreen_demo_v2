package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// Tham số có phiên bản của orders (g4-conventions §1.2): sales_officer đề xuất ≠ sales_approver duyệt
// (R-56, CHECK DB lớp hai). Mặc định #0 TẠM, CHỜ KHÁCH; áp theo ngày của sự việc.

// ParamInput là một đề xuất phiên bản tham số.
type ParamInput struct {
	Key           string
	Value         decimal.Decimal
	EffectiveFrom string // YYYY-MM-DD, rỗng = hôm nay
	Provisional   bool
	Reason        string
	IdemKey       string
}

// ProposeParam đề xuất một phiên bản tham số (pending tới khi người KHÁC duyệt).
func (s *Service) ProposeParam(ctx context.Context, in ParamInput) (store.Param, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer, RoleApprover)
	if err != nil {
		return store.Param{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return store.Param{}, false, err
	}
	def, ok := domain.ParamByKey(in.Key)
	if !ok {
		return store.Param{}, false, ErrParamNotFound
	}
	if err := domain.CheckParamValue(def, in.Value); err != nil {
		return store.Param{}, false, mapErr(err)
	}
	reason, err := domain.CheckReason("reason", in.Reason, true)
	if err != nil {
		return store.Param{}, false, mapErr(err)
	}
	now := s.Clock.Now()
	eff := domain.Day(now)
	if in.EffectiveFrom != "" {
		if eff, err = domain.ParseDate("effective_from", in.EffectiveFrom); err != nil {
			return store.Param{}, false, mapErr(err)
		}
	}
	var res store.Param
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, p.TenantID, "param:"+in.Key); err != nil {
			return err
		}
		n, err := store.NextParamSeq(ctx, tx, in.Key)
		if err != nil {
			return err
		}
		e := store.ParamEvent{Meta: store.Meta{ID: ids.New(), EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey, RuleVersion: domain.RuleParam},
			Key: in.Key, VersionID: ids.New(), VersionSeq: n, Kind: "proposed", Value: in.Value, EffectiveFrom: eff, Provisional: in.Provisional,
			ProposerRef: p.ActorRef, Reason: &reason}
		return s.applyParam(ctx, tx, p.TenantID, &e, &res)
	}, s.replayParam(ctx, in.IdemKey, &res))
	return res, replayed, err
}

// DecideParam duyệt / từ chối một phiên bản đang chờ. Người quyết định KHÁC người đề xuất.
func (s *Service) DecideParam(ctx context.Context, key string, version uuid.UUID, approve bool, reason, idem string) (store.Param, bool, error) {
	p, err := authz.Require(ctx, RoleApprover)
	if err != nil {
		return store.Param{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return store.Param{}, false, err
	}
	if _, ok := domain.ParamByKey(key); !ok {
		return store.Param{}, false, ErrParamNotFound
	}
	r, err := domain.CheckReason("reason", reason, !approve)
	if err != nil {
		return store.Param{}, false, mapErr(err)
	}
	var res store.Param
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
		if err := authz.EnsureDistinctApproverFor(cur.ProposedBy, p, RoleApprover); err != nil {
			return err
		}
		kind := "approved"
		if !approve {
			kind = "rejected"
		}
		e := store.ParamEvent{Meta: store.Meta{ID: ids.New(), EventTime: now, ActorRef: p.ActorRef, IdemKey: idem, RuleVersion: domain.RuleParam},
			Key: key, VersionID: version, VersionSeq: cur.VersionSeq, Kind: kind, Value: cur.Value, EffectiveFrom: cur.EffectiveFrom,
			Provisional: cur.Provisional, ProposerRef: cur.ProposedBy, Reason: strPtr(r)}
		return s.applyParam(ctx, tx, p.TenantID, &e, &res)
	}, s.replayParam(ctx, idem, &res))
	return res, replayed, err
}

func (s *Service) applyParam(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *store.ParamEvent, res *store.Param) error {
	if err := store.InsertParamEvent(ctx, tx, tid, e); err != nil {
		return err
	}
	cur, err := store.GetParam(ctx, tx, e.Key, e.VersionID, false)
	if err != nil {
		return err
	}
	*res = *cur
	return nil
}

func (s *Service) replayParam(ctx context.Context, idem string, res *store.Param) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		cur, err := store.ParamEventByIdem(ctx, tx, idem)
		if err != nil || cur == nil {
			return false, err
		}
		*res = *cur
		return true, nil
	}
}

// ParamsView là các phiên bản của một tham số và giá trị hiệu lực hôm nay.
type ParamsView struct {
	Def          domain.ParamDef
	Versions     []store.Param
	Effective    decimal.Decimal
	ParamVersion string
	Provisional  bool
}

// Params đọc các phiên bản của tham số key.
func (s *Service) Params(ctx context.Context, key string) (ParamsView, error) {
	def, ok := domain.ParamByKey(key)
	if !ok {
		return ParamsView{}, ErrParamNotFound
	}
	v := ParamsView{Def: def}
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		if v.Versions, e = store.Params(ctx, tx, key); e != nil {
			return e
		}
		pv, e := effectiveParam(ctx, tx, key, s.Clock.Now())
		v.Effective, v.ParamVersion, v.Provisional = pv.Value, pv.Label, pv.Provisional
		return e
	})
	return v, err
}

// paramValue là giá trị tham số đã áp.
type paramValue struct {
	Value       decimal.Decimal
	Label       string // "<key>:v<n>" | "<key>:default"
	Seq         int    // 0 = mặc định
	Provisional bool
}

// effectiveParam đọc giá trị đã duyệt hiệu lực vào ngày nghiệp vụ của at, không có thì mặc định tạm.
func effectiveParam(ctx context.Context, tx pgx.Tx, key string, at time.Time) (paramValue, error) {
	def, _ := domain.ParamByKey(key)
	p, err := store.EffectiveParam(ctx, tx, key, domain.Day(at))
	if err != nil {
		return paramValue{}, err
	}
	if p == nil {
		return paramValue{Value: def.Default, Label: domain.ParamVersionLabel(key, 0), Provisional: true}, nil
	}
	return paramValue{Value: p.Value, Label: domain.ParamVersionLabel(key, p.VersionSeq), Seq: p.VersionSeq, Provisional: p.Provisional}, nil
}
