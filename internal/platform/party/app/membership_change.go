package app

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi đổi thành viên HTX (docs/errors.md).
var (
	ErrHouseholdNotFound  = problem.New(http.StatusNotFound, "party.household_not_found", "Không tìm thấy nông hộ")
	ErrMembershipSame     = problem.New(http.StatusConflict, "party.membership_unchanged", "Nông hộ đã ở trạng thái thành viên này")
	ErrMembershipReason   = problem.New(http.StatusUnprocessableEntity, "party.membership_reason_required", "Nêu lý do đổi thành viên HTX (10–500 ký tự)")
	errMembershipDateText = "không được trước ngày vào HTX hiện tại hoặc ở tương lai"
)

// ChangeMembershipInput: nông hộ vào (CooperativeRef khác Nil, đang không thuộc HTX),
// chuyển (sang HTX khác) hoặc rời HTX (CooperativeRef = Nil).
type ChangeMembershipInput struct {
	HouseholdRef   uuid.UUID
	CooperativeRef uuid.UUID
	EffectiveDate  *time.Time // ngày (giờ Việt Nam); nil = hôm nay
	Reason         string     // không chứa dữ liệu cá nhân; không đi vào sự kiện
}

// MembershipResult là kết quả đổi thành viên.
type MembershipResult struct {
	Household domain.Household
	Change    string
	Prev      uuid.UUID
}

// ChangeMembership đổi HTX của nông hộ (một hộ gắn đúng một HTX tại một thời điểm,
// KN US-HTX-02): đóng thành viên đang mở (không xóa) và mở thành viên mới, rồi phát
// pf.party.household.membership_changed CÙNG giao dịch (KN US-HTX-05). Quản lý HTX
// chỉ cho hộ vào HTX của mình (khi hộ chưa thuộc HTX nào) hoặc rời HTX của mình.
func (r *Registry) ChangeMembership(ctx context.Context, in ChangeMembershipInput) (MembershipResult, error) {
	p, err := authz.Require(ctx, householdWriterRoles...)
	if err != nil {
		return MembershipResult{}, err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if n := utf8.RuneCountInString(in.Reason); n < 10 || n > 500 {
		return MembershipResult{}, ErrMembershipReason
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return MembershipResult{}, err
	}
	now := r.now()
	today, _ := time.Parse(time.DateOnly, now.In(clock.Zone()).Format(time.DateOnly))
	eff := today
	if in.EffectiveDate != nil {
		eff, _ = time.Parse(time.DateOnly, in.EffectiveDate.Format(time.DateOnly))
	}
	if eff.After(today) {
		return MembershipResult{}, problem.Validation.WithField("effective_date", errMembershipDateText)
	}
	coopOnly := p.Has(port.RoleCoopManager) && !p.Has(port.RoleRegistryAdmin) && !p.Has(port.RoleKNProvinceAdmin) && !p.Has(port.RoleKNOfficer)
	var res MembershipResult
	err = tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		h, err := store.HouseholdByRef(ctx, tx, in.HouseholdRef)
		if err != nil {
			return err
		}
		if h == nil || !visible(sc, h.CommuneCode) {
			return ErrHouseholdNotFound
		}
		if !h.Active {
			return ErrOrgState.WithDetail("Nông hộ đã ngừng hoạt động")
		}
		prev := h.CooperativeRef
		if prev == in.CooperativeRef {
			return ErrMembershipSame
		}
		if coopOnly {
			joinOwn := prev == uuid.Nil && in.CooperativeRef == p.OrgRef
			leaveOwn := prev == p.OrgRef && in.CooperativeRef == uuid.Nil
			if p.OrgRef == uuid.Nil || (!joinOwn && !leaveOwn) {
				return ErrOutOfScope.WithDetail("Bạn không có quyền truy cập nông hộ này.")
			}
		}
		if in.CooperativeRef != uuid.Nil {
			coop, err := store.CooperativeByRef(ctx, tx, in.CooperativeRef)
			if err != nil {
				return err
			}
			if coop == nil || !visible(sc, coop.AdminUnitCode) {
				return problem.Validation.WithField("cooperative_ref", domain.ErrCooperative.Error())
			}
			if !coop.Active {
				return ErrOrgState.WithDetail("HTX đã bị vô hiệu hóa")
			}
		}
		if prev != uuid.Nil {
			since, err := store.OpenMembershipSince(ctx, tx, h.Ref)
			if err != nil {
				return err
			}
			if since != nil && eff.Before(*since) {
				return problem.Validation.WithField("effective_date", errMembershipDateText)
			}
			if err := store.CloseMembership(ctx, tx, h.Ref, eff); err != nil {
				return err
			}
		}
		if in.CooperativeRef != uuid.Nil {
			if err := store.OpenMembership(ctx, tx, p.TenantID, h.Ref, in.CooperativeRef, eff); err != nil {
				return err
			}
		}
		res.Change = port.MembershipTransferred
		switch {
		case prev == uuid.Nil:
			res.Change = port.MembershipJoined
		case in.CooperativeRef == uuid.Nil:
			res.Change = port.MembershipLeft
		}
		res.Prev = prev
		pl := port.HouseholdMembershipChangedV1{HouseholdRef: h.Ref, CommuneCode: h.CommuneCode, Change: res.Change,
			EffectiveDate: eff.Format(time.DateOnly), ActorRef: p.ActorRef, EventTime: now.Format(time.RFC3339)}
		if prev != uuid.Nil {
			pr := prev
			pl.PrevCooperativeRef = &pr
		}
		if in.CooperativeRef != uuid.Nil {
			c := in.CooperativeRef
			pl.CooperativeRef = &c
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: p.TenantID, Type: port.EventHouseholdMembershipChanged, SchemaVersion: 1, OccurredAt: now,
			TraceID: telemetry.TraceID(ctx), Producer: "pf/party", Payload: pl,
		}); err != nil {
			return err
		}
		cur, err := store.HouseholdByRef(ctx, tx, h.Ref)
		if err != nil || cur == nil {
			return err
		}
		res.Household = *cur
		return nil
	})
	return res, err
}
