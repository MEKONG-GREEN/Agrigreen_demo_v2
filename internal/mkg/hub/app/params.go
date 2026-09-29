package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Lỗi tham số.
var (
	ErrParamSetUnknown     = problem.New(404, "hub.param_set_not_found", "Không có bộ tham số này (risk, weigh)")
	ErrParamVersionMissing = problem.New(404, "hub.param_version_not_found", "Không tìm thấy phiên bản tham số")
	ErrParamPending        = problem.New(409, "hub.param_pending_exists", "Bộ tham số đang có phiên bản chờ duyệt")
)

// activeRisk trả bộ tham số hub.risk.v2 hiện hành và chuỗi ghim; chưa duyệt → mặc định tạm (#0).
func activeRisk(ctx context.Context, tx pgx.Tx) (domain.RiskParams, string, error) {
	seq, raw, ok, err := store.ParamCurrent(ctx, tx, domain.ParamSetRisk)
	if err != nil || !ok {
		return domain.DefaultRiskV2, domain.PinnedVersion(domain.RuleRiskV2, 0), err
	}
	p := domain.DefaultRiskV2
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, "", err
	}
	return p, domain.PinnedVersion(domain.RuleRiskV2, seq), nil
}

// activeWeigh trả bộ tham số hub.weigh.v1 hiện hành và chuỗi ghim; chưa duyệt → mặc định tạm (#0).
func activeWeigh(ctx context.Context, tx pgx.Tx) (domain.WeighParams, string, error) {
	seq, raw, ok, err := store.ParamCurrent(ctx, tx, domain.ParamSetWeigh)
	if err != nil || !ok {
		return domain.DefaultWeighV1, domain.PinnedVersion(domain.RuleWeighV1, 0), err
	}
	p := domain.DefaultWeighV1
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, "", err
	}
	return p, domain.PinnedVersion(domain.RuleWeighV1, seq), nil
}

// ProposeParamsInput là đề xuất một phiên bản bộ tham số.
type ProposeParamsInput struct {
	Set           string
	Params        json.RawMessage
	Reason        string
	EffectiveFrom time.Time
	DeviceID      *string
	IdemKey       string
}

// ProposeParams ghi một phiên bản chờ duyệt (hub_operator). Một phiên bản chờ mỗi bộ.
func (s *Service) ProposeParams(ctx context.Context, in ProposeParamsInput) (v store.ParamVersion, replayed bool, err error) {
	rule := domain.RuleForSet(in.Set)
	if rule == "" {
		return v, false, ErrParamSetUnknown
	}
	p, err := authz.Require(ctx, RoleHubOperator)
	if err != nil {
		return v, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return v, false, err
	}
	val, err := domain.DecodeParams(in.Set, in.Params)
	if err != nil {
		return v, false, mapErr(err)
	}
	reason := strings.TrimSpace(in.Reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return v, false, problem.Validation.WithField("reason", "lý do bắt buộc, 10..500 ký tự")
	}
	now := s.Clock.Now()
	if in.EffectiveFrom.IsZero() {
		in.EffectiveFrom = now
	}
	norm, err := json.Marshal(val)
	if err != nil {
		return v, false, err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockStream(ctx, tx, store.ParamStream(in.Set)); err != nil {
			return err
		}
		if pend, err := store.PendingParam(ctx, tx, in.Set); err != nil {
			return err
		} else if pend {
			return ErrParamPending
		}
		v = store.ParamVersion{
			ID: ids.New(), TenantID: p.TenantID, Set: in.Set, Rule: rule, Params: norm, Reason: reason,
			EffectiveFrom: in.EffectiveFrom, EventTime: now, ProposedBy: p.ActorRef, IdemKey: in.IdemKey,
		}
		return insert(store.InsertParamVersion(ctx, tx, &v, in.DeviceID))
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.ParamVersionByIdem(ctx, tx, in.IdemKey)
		if prev != nil {
			v = *prev
		}
		return prev != nil, err
	})
	return v, replayed, err
}

// DecideParamsInput là quyết định với một phiên bản tham số.
type DecideParamsInput struct {
	Set        string
	VersionSeq int64
	Decision   domain.Decision
	Reason     string
	IdemKey    string
}

// DecideParams duyệt / từ chối (hub_approver ≠ người đặt, R-56 ở app + CHECK DB).
func (s *Service) DecideParams(ctx context.Context, in DecideParamsInput) (v store.ParamVersion, replayed bool, err error) {
	if domain.RuleForSet(in.Set) == "" {
		return v, false, ErrParamSetUnknown
	}
	p, err := authz.Require(ctx, RoleHubApprover)
	if err != nil {
		return v, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return v, false, err
	}
	if in.Decision != domain.DecisionApproved && in.Decision != domain.DecisionRejected {
		return v, false, problem.Validation.WithField("decision", "approved hoặc rejected")
	}
	if err := domain.CheckDecisionReason(in.Decision, in.Reason); err != nil {
		return v, false, mapErr(err)
	}
	now := s.Clock.Now()
	load := func(tx pgx.Tx, seq int64) error {
		cur, err := store.ParamVersionBySeq(ctx, tx, in.Set, seq)
		if err == nil && cur != nil {
			v = *cur
		}
		return err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockStream(ctx, tx, store.ParamStream(in.Set)); err != nil {
			return err
		}
		cur, err := store.ParamVersionBySeq(ctx, tx, in.Set, in.VersionSeq)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrParamVersionMissing
		}
		if cur.Decision != nil {
			return ErrNotPending
		}
		if cur.ProposedBy == p.ActorRef {
			return problem.SelfApproval.WithCause(authz.ErrSelfApproval)
		}
		d := store.ParamDecision{
			ID: ids.New(), TenantID: p.TenantID, Set: in.Set, VersionID: cur.ID, VersionSeq: cur.Seq, Decision: string(in.Decision),
			ProposerRef: cur.ProposedBy, ActorRef: p.ActorRef, EventTime: now, IdemKey: in.IdemKey,
		}
		if r := strings.TrimSpace(in.Reason); r != "" {
			d.Reason = &r
		}
		if err := insert(store.InsertParamDecision(ctx, tx, &d)); err != nil {
			return err
		}
		if in.Decision == domain.DecisionApproved {
			if err := store.UpsertParamCurrent(ctx, tx, *cur, p.ActorRef, now); err != nil {
				return err
			}
		}
		return load(tx, cur.Seq)
	}, func(tx pgx.Tx) (bool, error) {
		d, err := store.ParamDecisionByIdem(ctx, tx, in.IdemKey)
		if err != nil || d == nil {
			return false, err
		}
		return true, load(tx, d.VersionSeq)
	})
	return v, replayed, err
}

// ParamsView là các phiên bản của một bộ kèm bộ đang áp dụng.
type ParamsView struct {
	Set      string
	Active   string // chuỗi ghim, vd hub.risk.v2#2 (#0 = mặc định tạm, chờ khách)
	Current  any
	Versions []store.ParamVersion
}

// Params đọc phiên bản của bộ tham số.
func (s *Service) Params(ctx context.Context, set string) (ParamsView, error) {
	if domain.RuleForSet(set) == "" {
		return ParamsView{}, ErrParamSetUnknown
	}
	out := ParamsView{Set: set}
	err := s.read(ctx, func(tx pgx.Tx) error {
		var err error
		if set == domain.ParamSetRisk {
			out.Current, out.Active, err = activeRisk(ctx, tx)
		} else {
			out.Current, out.Active, err = activeWeigh(ctx, tx)
		}
		if err != nil {
			return err
		}
		out.Versions, err = store.ParamVersions(ctx, tx, set)
		return err
	})
	return out, err
}
