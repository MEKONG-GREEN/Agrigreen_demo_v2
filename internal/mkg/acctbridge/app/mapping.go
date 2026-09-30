package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Bảng ánh xạ tài khoản có phiên bản (acctbridge.account_mapping, B-05): accounting_bridge_operator đề
// xuất ≠ chief_accountant duyệt (R-56, CHECK DB lớp hai). Chế độ kế toán, hệ thống tài khoản chờ khách:
// đề xuất không kèm rules dùng domain.DefaultRules (VAS kiểu TT200, provisional = true).

const dateLayout = "2006-01-02"

// MappingInput là một đề xuất phiên bản ánh xạ.
type MappingInput struct {
	Rules         []domain.Rule // rỗng = bảng mặc định tạm
	EffectiveFrom string        // YYYY-MM-DD, rỗng = hôm nay (giờ VN)
	Provisional   *bool         // nil = true khi dùng mặc định, false khi có rules
	Reason        string
	IdemKey       string
}

// MappingResult là kết quả một thao tác ánh xạ.
type MappingResult struct {
	Mapping store.Mapping
	Event   store.MappingEvent
}

func checkReason(field, s string, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" && !required {
		return "", nil
	}
	if n := len([]rune(s)); n < 10 || n > 500 {
		return "", problem.Validation.WithField(field, "lý do 10..500 ký tự")
	}
	return s, nil
}

func businessDay(t time.Time) time.Time {
	d := clock.BusinessDate(t)
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// ProposeMapping đề xuất một phiên bản ánh xạ (pending tới khi người KHÁC duyệt). Tối đa một phiên bản chờ.
func (s *Service) ProposeMapping(ctx context.Context, in MappingInput) (MappingResult, bool, error) {
	p, err := authz.Require(ctx, RoleOperator)
	if err != nil {
		return MappingResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return MappingResult{}, false, err
	}
	rules, provisional := in.Rules, false
	if len(rules) == 0 {
		rules, provisional = domain.DefaultRules(), true
	}
	if in.Provisional != nil {
		provisional = *in.Provisional
	}
	if err := domain.ValidateRules(rules); err != nil {
		return MappingResult{}, false, problem.Validation.WithField("rules", err.Error())
	}
	rules = domain.CanonicalRules(rules)
	reason, err := checkReason("reason", in.Reason, true)
	if err != nil {
		return MappingResult{}, false, err
	}
	now := s.Clock.Now()
	eff := businessDay(now)
	if in.EffectiveFrom != "" {
		if eff, err = time.Parse(dateLayout, in.EffectiveFrom); err != nil {
			return MappingResult{}, false, problem.Validation.WithField("effective_from", "YYYY-MM-DD")
		}
	}
	var res MappingResult
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockMapping(ctx, tx, p.TenantID); err != nil {
			return err
		}
		if pend, err := store.PendingMapping(ctx, tx); err != nil {
			return err
		} else if pend != nil {
			return ErrMappingPending.WithDetail("phiên bản " + strconv.Itoa(pend.VersionSeq) + " đang chờ duyệt")
		}
		n, err := store.NextMappingSeq(ctx, tx)
		if err != nil {
			return err
		}
		e := store.MappingEvent{ID: ids.New(), TenantID: p.TenantID, VersionID: ids.New(), VersionSeq: n, Kind: "proposed", Rules: rules,
			RulesSHA256: domain.RulesSHA256(rules), EffectiveFrom: eff, Provisional: provisional, ProposerRef: p.ActorRef, Reason: &reason,
			EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey}
		return s.applyMapping(ctx, tx, &e, &res)
	}, s.replayMapping(ctx, in.IdemKey, &res))
	return res, replayed, err
}

// DecideMapping duyệt / từ chối phiên bản đang chờ. Người quyết định (chief_accountant) KHÁC người đề xuất.
func (s *Service) DecideMapping(ctx context.Context, version uuid.UUID, approve bool, reason, idem string) (MappingResult, bool, error) {
	p, err := authz.Require(ctx, RoleChief)
	if err != nil {
		return MappingResult{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return MappingResult{}, false, err
	}
	r, err := checkReason("reason", reason, !approve)
	if err != nil {
		return MappingResult{}, false, err
	}
	var res MappingResult
	now := s.Clock.Now()
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockMapping(ctx, tx, p.TenantID); err != nil {
			return err
		}
		cur, err := store.GetMapping(ctx, tx, version, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrMappingNotFound
		}
		if cur.Status != "pending" {
			return ErrMappingDecided
		}
		if err := authz.EnsureDistinctApproverFor(cur.ProposedBy, p, RoleChief); err != nil {
			return err
		}
		kind := "approved"
		if !approve {
			kind = "rejected"
		}
		e := store.MappingEvent{ID: ids.New(), TenantID: p.TenantID, VersionID: version, VersionSeq: cur.VersionSeq, Kind: kind, Rules: cur.Rules,
			RulesSHA256: cur.RulesSHA256, EffectiveFrom: cur.EffectiveFrom, Provisional: cur.Provisional, ProposerRef: cur.ProposedBy,
			Reason: strPtr(r), EventTime: now, ActorRef: p.ActorRef, IdemKey: idem}
		return s.applyMapping(ctx, tx, &e, &res)
	}, s.replayMapping(ctx, idem, &res))
	return res, replayed, err
}

func (s *Service) applyMapping(ctx context.Context, tx pgx.Tx, e *store.MappingEvent, res *MappingResult) error {
	if err := insert(store.InsertMappingEvent(ctx, tx, e)); err != nil {
		return err
	}
	if err := store.ApplyMappingEvent(ctx, tx, *e); err != nil {
		return err
	}
	cur, err := store.GetMapping(ctx, tx, e.VersionID, false)
	if err != nil {
		return err
	}
	res.Mapping, res.Event = *cur, *e
	return nil
}

func (s *Service) replayMapping(ctx context.Context, idem string, res *MappingResult) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		e, err := store.MappingEventByIdem(ctx, tx, idem)
		if err != nil || e == nil {
			return false, err
		}
		cur, err := store.GetMapping(ctx, tx, e.VersionID, false)
		if err != nil || cur == nil {
			return false, err
		}
		res.Mapping, res.Event = *cur, *e
		return true, nil
	}
}

// MappingsView là các phiên bản ánh xạ, phiên bản hiệu lực hôm nay và bảng mặc định tạm.
type MappingsView struct {
	Versions  []store.Mapping
	Effective *store.Mapping
	Default   []domain.Rule
}

// Mappings đọc các phiên bản ánh xạ.
func (s *Service) Mappings(ctx context.Context) (MappingsView, error) {
	v := MappingsView{Default: domain.DefaultRules()}
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		if v.Versions, e = store.Mappings(ctx, tx); e != nil {
			return e
		}
		v.Effective, e = store.EffectiveMapping(ctx, tx, businessDay(s.Clock.Now()))
		return e
	})
	return v, err
}
