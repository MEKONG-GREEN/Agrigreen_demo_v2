package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// ProposeParamInput là đề xuất một phiên bản tham số.
type ProposeParamInput struct {
	Key           string
	Value         string
	Reason        string
	EffectiveFrom time.Time
	IdemKey       string
}

// ProposeParam ghi phiên bản chờ duyệt (cost_accountant). Một phiên bản chờ mỗi khóa.
func (s *Service) ProposeParam(ctx context.Context, in ProposeParamInput) (v store.ParamVersion, replayed bool, err error) {
	if _, ok := domain.ParamDefFor(in.Key); !ok {
		return v, false, ErrParamNotFound
	}
	p, err := authz.Require(ctx, port.RoleCostAccountant)
	if err != nil {
		return v, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return v, false, err
	}
	val, err := domain.NormalizeParam(in.Key, in.Value)
	if err != nil {
		return v, false, mapErr(err)
	}
	reason := strings.TrimSpace(in.Reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return v, false, problem.Validation.WithField("reason", "lý do bắt buộc, 10..500 ký tự")
	}
	now := s.now()
	if in.EffectiveFrom.IsZero() {
		in.EffectiveFrom = now
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockKey(ctx, tx, store.ParamStream(in.Key)); err != nil {
			return err
		}
		if pend, err := store.PendingParam(ctx, tx, in.Key); err != nil {
			return err
		} else if pend {
			return ErrParamPending
		}
		v = store.ParamVersion{ID: ids.New(), Key: in.Key, Value: val, Reason: reason, EffectiveFrom: in.EffectiveFrom.UTC(), EventTime: now,
			ProposedBy: p.ActorRef, IdemKey: in.IdemKey}
		return store.InsertParamVersion(ctx, tx, p.TenantID, &v)
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.ParamVersionByIdem(ctx, tx, in.IdemKey)
		if prev != nil {
			v = *prev
		}
		return prev != nil, err
	})
	return v, replayed, err
}

// DecideParam duyệt / từ chối (chief_accountant ≠ người đặt — R-56 app + CHECK DB).
func (s *Service) DecideParam(ctx context.Context, key string, seq int64, decision, reason, idem string) (v store.ParamVersion, replayed bool, err error) {
	if _, ok := domain.ParamDefFor(key); !ok {
		return v, false, ErrParamNotFound
	}
	p, err := authz.Require(ctx, port.RoleChiefAccountant)
	if err != nil {
		return v, false, err
	}
	if err := requireIdem(idem); err != nil {
		return v, false, err
	}
	if decision != "approved" && decision != "rejected" {
		return v, false, problem.Validation.WithField("decision", "approved hoặc rejected")
	}
	reason = strings.TrimSpace(reason)
	if decision == "rejected" && len([]rune(reason)) < 10 {
		return v, false, problem.Validation.WithField("reason", "từ chối cần lý do ≥ 10 ký tự")
	}
	load := func(tx pgx.Tx, seq int64) error {
		cur, err := store.ParamVersionBySeq(ctx, tx, key, seq)
		if cur != nil {
			v = *cur
		}
		return err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockKey(ctx, tx, store.ParamStream(key)); err != nil {
			return err
		}
		cur, err := store.ParamVersionBySeq(ctx, tx, key, seq)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrParamVersionNotFound
		}
		if cur.Decision != nil {
			return ErrParamNotPending
		}
		if cur.ProposedBy == p.ActorRef {
			return problem.SelfApproval.WithCause(authz.ErrSelfApproval)
		}
		d := store.ParamDecision{ID: ids.New(), Key: key, VersionID: cur.ID, VersionSeq: cur.Seq, Decision: decision, Reason: strPtr(reason),
			ProposerRef: cur.ProposedBy, ActorRef: p.ActorRef, EventTime: s.now(), IdemKey: idem}
		if err := store.InsertParamDecision(ctx, tx, p.TenantID, d); err != nil {
			return err
		}
		return load(tx, cur.Seq)
	}, func(tx pgx.Tx) (bool, error) {
		_, sq, ok, err := store.ParamDecisionByIdem(ctx, tx, idem)
		if err != nil || !ok {
			return false, err
		}
		return true, load(tx, sq)
	})
	return v, replayed, err
}

// ParamView là một khóa tham số: giá trị đang áp dụng lúc này + lịch sử phiên bản.
type ParamView struct {
	Def      domain.ParamDef
	Active   domain.ParamValue
	Versions []store.ParamVersion
}

// ParamsView đọc mọi khóa tham số.
func (s *Service) ParamsView(ctx context.Context) (out []ParamView, err error) {
	err = s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		now := s.now()
		for _, d := range domain.Params {
			a, err := paramAt(ctx, tx, d.Key, now, nil)
			if err != nil {
				return err
			}
			vs, err := store.ParamVersions(ctx, tx, d.Key)
			if err != nil {
				return err
			}
			out = append(out, ParamView{Def: d, Active: a, Versions: vs})
		}
		return nil
	})
	return out, err
}

// CarbonInput là doanh thu carbon được ghi (hợp đồng tín chỉ đã thu — nguồn ngoài hệ thống).
type CarbonInput struct {
	Amount    money.Amount
	EventTime time.Time
	Reference string
	IdemKey   string
}

// RecordCarbonRevenue ghi doanh thu carbon và CHIA theo tham số tại thời điểm sự việc (45 % giữ lại /
// 70-15-15, tạm chờ khách). Chỉ chia số tiền đã có; KHÔNG tính tCO2e (pf/mrv, C-1).
func (s *Service) RecordCarbonRevenue(ctx context.Context, in CarbonInput) (c store.Carbon, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleCostAccountant)
	if err != nil {
		return c, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return c, false, err
	}
	if in.Amount.IsZero() {
		return c, false, problem.Validation.WithField("amount", "≠ 0 (âm = đảo khoản đã ghi)")
	}
	if in.EventTime.IsZero() {
		in.EventTime = s.now()
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		per, err := s.ensurePeriod(ctx, tx, p.TenantID, domain.PeriodCode(in.EventTime), domain.SystemActorRef)
		if err != nil {
			return err
		}
		if per.Status != port.PeriodOpen {
			return ErrPeriodNotOpen
		}
		keys := []string{port.ParamCarbonRetainedPct, port.ParamCarbonSplitFarmers, port.ParamCarbonSplitAuthority, port.ParamCarbonSplitVerifier}
		vals := make([]domain.ParamValue, 0, len(keys))
		pins := domain.Pins{}
		for _, k := range keys {
			v, err := paramAt(ctx, tx, k, in.EventTime, nil)
			if err != nil {
				return err
			}
			vals, pins = append(vals, v), pins.Add(v)
		}
		sp, err := domain.SplitCarbon(in.Amount, vals[0].Decimal(), vals[1].Decimal(), vals[2].Decimal(), vals[3].Decimal())
		if err != nil {
			return err
		}
		c = store.Carbon{ID: ids.New(), PeriodRef: per.ID, PeriodCode: per.Code, Revenue: in.Amount, Split: sp, Pins: pins,
			Reference: strPtr(strings.TrimSpace(in.Reference)), EventTime: in.EventTime.UTC(), ActorRef: p.ActorRef, IdemKey: in.IdemKey}
		return store.InsertCarbon(ctx, tx, p.TenantID, &c)
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.CarbonByIdem(ctx, tx, in.IdemKey)
		if prev != nil {
			c = *prev
		}
		return prev != nil, err
	})
	return c, replayed, err
}
