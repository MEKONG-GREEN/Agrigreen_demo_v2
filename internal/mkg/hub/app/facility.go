package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ProposeInput là một đề xuất dữ liệu chủ cơ sở (tạo mới hoặc thay đổi).
type ProposeInput struct {
	// FacilityID nil → cơ sở mới (server sinh UUIDv7).
	FacilityID *uuid.UUID
	Spec       domain.FacilitySpec
	// Reason bắt buộc từ phiên bản 2 (lý do thay đổi, ≥ 10 ký tự).
	Reason    string
	EventTime time.Time
	DeviceID  *string
	IdemKey   string
}

// Propose ghi một phiên bản đề xuất — chưa có hiệu lực tới khi người khác duyệt.
// Mỗi cơ sở chỉ có tối đa một phiên bản chờ duyệt.
func (s *Service) Propose(ctx context.Context, in ProposeInput) (v domain.FacilityVersion, replayed bool, err error) {
	p, err := authz.Require(ctx, RoleHubOperator)
	if err != nil {
		return v, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return v, false, err
	}
	if in.EventTime.IsZero() {
		in.EventTime = s.Clock.Now()
	}
	if err := domain.CheckEventTime("event_time", in.EventTime, s.Clock.Now()); err != nil {
		return v, false, mapErr(err)
	}
	if err := in.Spec.Validate(); err != nil {
		return v, false, mapErr(err)
	}
	fid := ids.New()
	if in.FacilityID != nil {
		fid = *in.FacilityID
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockFacilityStream(ctx, tx, fid); err != nil {
			return err
		}
		latest, err := store.LatestVersion(ctx, tx, fid)
		if err != nil {
			return err
		}
		if in.FacilityID != nil && latest == nil {
			return ErrFacilityNotFound
		}
		if latest != nil && latest.Pending() {
			return ErrPendingExists
		}
		v = domain.FacilityVersion{
			ID: ids.New(), TenantID: p.TenantID, FacilityID: fid, Spec: in.Spec, ProposedBy: p.ActorRef,
			EventTime: in.EventTime.UTC(), IdemKey: in.IdemKey,
		}
		if latest != nil {
			if err := domain.CheckChangeReason(in.Reason); err != nil {
				return mapErr(err)
			}
			if latest.Spec.Kind != in.Spec.Kind {
				return problem.Validation.WithField("kind", "không đổi loại cơ sở; tạo cơ sở mới")
			}
			prev := latest.ID
			if cur, err := store.Current(ctx, tx, fid, false); err != nil {
				return err
			} else if cur != nil {
				prev = cur.VersionID
			}
			reason := strings.TrimSpace(in.Reason)
			v.Supersedes, v.ChangeReason = &prev, &reason
		}
		if taken, err := store.CodeTaken(ctx, tx, in.Spec.Code, fid); err != nil {
			return err
		} else if taken {
			return ErrCodeTaken
		}
		if in.Spec.ParentFacilityID != nil {
			parent, err := store.Current(ctx, tx, *in.Spec.ParentFacilityID, false)
			if err != nil {
				return err
			}
			if parent == nil || parent.Spec.Kind != domain.KindHub || parent.ID == fid {
				return ErrParentInvalid
			}
		}
		if ok, err := store.BoundaryValid(ctx, tx, in.Spec.Boundary); err != nil {
			return err
		} else if !ok {
			return ErrBoundaryInvalid
		}
		return insert(store.InsertVersion(ctx, tx, &v, in.DeviceID))
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.VersionByIdem(ctx, tx, in.IdemKey)
		if prev != nil {
			v = *prev
		}
		return prev != nil, err
	})
	return v, replayed, err
}

// DecideInput là quyết định với một phiên bản chờ duyệt.
type DecideInput struct {
	FacilityID uuid.UUID
	VersionSeq int64
	Decision   domain.Decision
	Reason     string
	IdemKey    string
}

// DecideResult là kết quả phê duyệt.
type DecideResult struct {
	Version  domain.FacilityVersion
	Facility *domain.Facility    // hiện hành sau quyết định (nil nếu chưa từng được duyệt)
	Exports  []store.ValveExport // bản ghi van đã xuất khi phê duyệt
}

// Decide phê duyệt hoặc từ chối một phiên bản. Người duyệt phải có vai
// hub_approver và KHÁC người đề xuất (R-56 — kiểm ở đây, DB kiểm lần hai).
// Phê duyệt → cập nhật dữ liệu chủ hiện hành và xuất qua van hub_facility +
// hub_capacity trong CÙNG giao dịch.
func (s *Service) Decide(ctx context.Context, in DecideInput) (res DecideResult, replayed bool, err error) {
	p, err := authz.Require(ctx, RoleHubApprover)
	if err != nil {
		return res, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return res, false, err
	}
	if in.Decision != domain.DecisionApproved && in.Decision != domain.DecisionRejected {
		return res, false, problem.Validation.WithField("decision", "approved hoặc rejected")
	}
	if err := domain.CheckDecisionReason(in.Decision, in.Reason); err != nil {
		return res, false, mapErr(err)
	}
	now := s.Clock.Now()
	load := func(tx pgx.Tx, seq int64) error {
		v, err := store.VersionBySeq(ctx, tx, in.FacilityID, seq)
		if err != nil {
			return err
		}
		if v == nil {
			return ErrVersionNotFound
		}
		res.Version = *v
		if res.Facility, err = store.Current(ctx, tx, in.FacilityID, false); err != nil {
			return err
		}
		return nil
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockFacilityStream(ctx, tx, in.FacilityID); err != nil {
			return err
		}
		v, err := store.VersionBySeq(ctx, tx, in.FacilityID, in.VersionSeq)
		if err != nil {
			return err
		}
		if v == nil {
			return ErrVersionNotFound
		}
		if !v.Pending() {
			return ErrNotPending
		}
		if v.ProposedBy == p.ActorRef {
			return problem.SelfApproval.WithCause(authz.ErrSelfApproval)
		}
		d := store.Decision{
			ID: ids.New(), TenantID: p.TenantID, FacilityID: in.FacilityID, VersionID: v.ID, VersionSeq: v.Seq,
			Decision: in.Decision, ProposerRef: v.ProposedBy, ActorRef: p.ActorRef, EventTime: now, IdemKey: in.IdemKey,
		}
		if r := strings.TrimSpace(in.Reason); r != "" {
			d.Reason = &r
		}
		if err := insert(store.InsertDecision(ctx, tx, &d)); err != nil {
			return err
		}
		if in.Decision == domain.DecisionApproved {
			if err := store.UpsertCurrent(ctx, tx, *v, p.ActorRef, now); err != nil {
				return err
			}
		}
		if err := load(tx, v.Seq); err != nil {
			return err
		}
		if in.Decision != domain.DecisionApproved {
			return nil
		}
		f := *res.Facility
		fx, err := s.export(ctx, tx, p, facilityPayload(f), f.ID, now, in.IdemKey+":valve:facility")
		if err != nil {
			return err
		}
		cx, err := s.publishCapacity(ctx, tx, p, f, now, in.IdemKey+":valve:capacity")
		if err != nil {
			return err
		}
		res.Exports = []store.ValveExport{fx, cx}
		return nil
	}, func(tx pgx.Tx) (bool, error) {
		d, err := store.DecisionByIdem(ctx, tx, in.IdemKey)
		if err != nil || d == nil {
			return false, err
		}
		if err := load(tx, d.VersionSeq); err != nil {
			return false, err
		}
		for _, k := range []string{":valve:facility", ":valve:capacity"} {
			if x, err := store.ValveByIdem(ctx, tx, in.IdemKey+k); err != nil {
				return false, err
			} else if x != nil {
				res.Exports = append(res.Exports, *x)
			}
		}
		return true, nil
	})
	return res, replayed, err
}

// Facility đọc dữ liệu chủ hiện hành.
func (s *Service) Facility(ctx context.Context, id uuid.UUID) (domain.Facility, error) {
	var f *domain.Facility
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		f, e = store.Current(ctx, tx, id, false)
		return e
	})
	if err != nil {
		return domain.Facility{}, err
	}
	if f == nil {
		return domain.Facility{}, ErrFacilityNotFound
	}
	return *f, nil
}

// FacilityPage là một trang cơ sở.
type FacilityPage struct {
	Items []domain.Facility
	Next  *uuid.UUID
}

// Facilities liệt kê cơ sở hiện hành, phân trang bằng cursor (R-53).
func (s *Service) Facilities(ctx context.Context, kind string, after uuid.UUID, limit int) (FacilityPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var items []domain.Facility
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListCurrent(ctx, tx, kind, after, limit+1)
		return e
	})
	if err != nil {
		return FacilityPage{}, err
	}
	var pg FacilityPage
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].ID
		pg.Next = &last
	}
	pg.Items = items
	return pg, nil
}

// Versions liệt kê mọi phiên bản (cả chờ duyệt, từ chối) của một cơ sở.
func (s *Service) Versions(ctx context.Context, id uuid.UUID) ([]domain.FacilityVersion, error) {
	var out []domain.FacilityVersion
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListVersions(ctx, tx, id)
		return e
	})
	if err == nil && len(out) == 0 {
		return nil, ErrFacilityNotFound
	}
	return out, err
}
