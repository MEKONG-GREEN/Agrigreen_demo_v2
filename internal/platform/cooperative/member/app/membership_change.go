package app

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/store"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
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
func (s *Service) ChangeMembership(ctx context.Context, in ChangeMembershipInput) (MembershipResult, error) {
	p, err := authz.Require(ctx, householdWriterRoles...)
	if err != nil {
		return MembershipResult{}, err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if n := utf8.RuneCountInString(in.Reason); n < 10 || n > 500 {
		return MembershipResult{}, ErrMembershipReason
	}
	sc, err := s.scopeFor(ctx)
	if err != nil {
		return MembershipResult{}, err
	}
	now := s.now()
	today, _ := time.Parse(time.DateOnly, now.In(clock.Zone()).Format(time.DateOnly))
	eff := today
	if in.EffectiveDate != nil {
		eff, _ = time.Parse(time.DateOnly, in.EffectiveDate.Format(time.DateOnly))
	}
	if eff.After(today) {
		return MembershipResult{}, problem.Validation.WithField("effective_date", errMembershipDateText)
	}
	coopOnly := p.Has(partyport.RoleCoopManager) && !p.Has(partyport.RoleRegistryAdmin) && !p.Has(partyport.RoleKNProvinceAdmin) && !p.Has(partyport.RoleKNOfficer)
	// HTX đích đọc qua pf/party TRƯỚC giao dịch (Nút ở pf_party, xã viên ở pf_coop — không giao dịch nào chạy
	// qua hai vai). FK household_membership → pf_party.org vẫn chặn Nút không tồn tại.
	var target *partyport.Org
	if in.CooperativeRef != uuid.Nil {
		if target, err = s.coopOrg(ctx, p.TenantID, in.CooperativeRef); err != nil {
			return MembershipResult{}, err
		}
	}
	var res MembershipResult
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		h, err := lookupHousehold(ctx, tx, p, sc, in.HouseholdRef)
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
			if target == nil || !visible(sc, target.AdminUnitCode) {
				return problem.Validation.WithField("cooperative_ref", domain.ErrCooperative.Error())
			}
			if !target.Active {
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
			TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: pl,
		}); err != nil {
			return err
		}
		cur, err := lookupHousehold(ctx, tx, p, sc, h.Ref)
		if err != nil || cur == nil {
			return err
		}
		res.Household = *cur
		return nil
	})
	return res, err
}
