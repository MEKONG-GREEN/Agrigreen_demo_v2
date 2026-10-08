package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/member/store"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// FarmerIdentity là định danh chủ hộ — CHỈ đi qua bộ nhớ để gửi vào kho định danh (R-04).
type FarmerIdentity struct {
	FullName   string `pii:"true"`
	Phone      string `pii:"true"`
	NationalID string `pii:"true"`
	Address    string `pii:"true"`
}

// CreateHouseholdInput là dữ liệu vào của CreateHousehold.
type CreateHouseholdInput struct {
	Identity         FarmerIdentity
	CooperativeRef   uuid.UUID
	CommuneCode      string // rỗng = xã của HTX
	CultivatedAreaHa *decimal.Decimal
	MemberSince      *time.Time
	// Source: nguồn đăng ký ghi vào pf.party.household.registered; rỗng → suy từ vai
	// (quản lý HTX → htx_app, còn lại → kn_app).
	Source string
	// RejectDuplicate: chủ hộ đã có trong kho định danh (trùng SĐT/CCCD) → lỗi
	// party.household_phone_taken thay vì dùng lại mã cũ (sao chép hàng loạt, HTX US-HH-02).
	RejectDuplicate bool
	// KNScoped: người gọi đã được kiểm bằng ngữ cảnh phiên KN v7.7 (authz.RequireKN) — bỏ kiểm
	// "quản lý HTX chỉ HTX mình" theo vai realm (FN-XA-03.03).
	KNScoped bool
}

// HouseholdResult là kết quả tạo một nông hộ.
type HouseholdResult struct {
	Household domain.Household
	// Duplicate: chủ hộ đã có trong kho định danh (trùng SĐT/CCCD) — dùng lại mã cũ, báo "trùng".
	Duplicate bool
	// Created: bản ghi nông hộ ở pf_coop vừa được tạo trong lần gọi này.
	Created bool
}

// DuplicateLabel là nhãn báo trùng hiển thị cho người dùng.
const DuplicateLabel = "trùng"

// CreateHousehold tạo chủ hộ trong kho định danh (mục đích registry.admin) rồi
// ghi nông hộ chỉ chứa mã tham chiếu. Trùng chỉ mục mù → dùng lại mã cũ.
func (s *Service) CreateHousehold(ctx context.Context, in CreateHouseholdInput) (HouseholdResult, error) {
	p, err := authz.Require(ctx, householdWriterRoles...)
	if err != nil {
		return HouseholdResult{}, err
	}
	sc, err := s.scopeFor(ctx)
	if err != nil {
		return HouseholdResult{}, err
	}
	return s.createHousehold(ctx, p, sc, in)
}

func (s *Service) createHousehold(ctx context.Context, p authz.Principal, sc authz.Scope, in CreateHouseholdInput) (HouseholdResult, error) {
	now := s.now()
	if strings.TrimSpace(in.Identity.FullName) == "" || strings.TrimSpace(in.Identity.Phone) == "" {
		return HouseholdResult{}, problem.Validation.WithField("identity", domain.ErrIdentity.Error())
	}
	if in.CooperativeRef == uuid.Nil {
		return HouseholdResult{}, problem.Validation.WithField("cooperative_ref", domain.ErrCooperative.Error())
	}
	if err := domain.ValidateHousehold(in.CultivatedAreaHa, in.MemberSince, now); err != nil {
		return HouseholdResult{}, fieldProblem(err)
	}
	if s.Vault == nil {
		return HouseholdResult{}, ErrDependency.WithDetail("chưa nối kho định danh")
	}

	// HTX phải trong phạm vi và đang hoạt động; nông hộ kế thừa phạm vi của HTX (KN US-HTX-02).
	coop, err := s.coopOrg(ctx, p.TenantID, in.CooperativeRef)
	if err != nil {
		return HouseholdResult{}, err
	}
	if coop == nil || !visible(sc, coop.AdminUnitCode) {
		if coop != nil {
			return HouseholdResult{}, ErrOutOfScope.WithDetail("Bạn không có quyền truy cập nông hộ này.")
		}
		return HouseholdResult{}, problem.Validation.WithField("cooperative_ref", domain.ErrCooperative.Error())
	}
	if !coop.Active {
		return HouseholdResult{}, ErrOrgState.WithDetail("HTX đã bị vô hiệu hóa")
	}
	// Quản lý HTX chỉ thao tác nông hộ của chính HTX mình (HTX US-HH-01 AC-3).
	// KN v7.7 (FN-XA-03.03): vai nằm trong ngữ cảnh phiên, phạm vi sc đã là xã phụ trách.
	if !in.KNScoped && !p.Has(partyport.RoleRegistryAdmin) && !p.Has(partyport.RoleKNProvinceAdmin) && !p.Has(partyport.RoleKNOfficer) && coop.Ref != p.OrgRef {
		return HouseholdResult{}, ErrOutOfScope.WithDetail("Bạn không có quyền truy cập nông hộ này.")
	}
	commune := strings.TrimSpace(in.CommuneCode)
	if commune == "" {
		commune = coop.AdminUnitCode
	}
	if !sc.Allows(commune) {
		return HouseholdResult{}, ErrOutOfScope.WithDetail("xã của nông hộ ngoài phạm vi phụ trách")
	}

	createdRef, err := s.Vault.RegisterHousehold(ctx, port.HouseholdIdentity{
		FullName: strings.TrimSpace(in.Identity.FullName), Phone: strings.TrimSpace(in.Identity.Phone),
		NationalID: strings.TrimSpace(in.Identity.NationalID), Address: strings.TrimSpace(in.Identity.Address), AreaCode: commune,
	})
	dup := false
	switch {
	case errors.Is(err, port.ErrIdentityDuplicate) && in.RejectDuplicate:
		return HouseholdResult{}, ErrPhoneTaken
	case errors.Is(err, port.ErrIdentityDuplicate) && createdRef != uuid.Nil:
		dup = true
	case err != nil:
		return HouseholdResult{}, vaultError(err)
	}

	h := domain.Household{
		Ref: createdRef, CommuneCode: commune, CultivatedAreaHa: in.CultivatedAreaHa, Active: true,
		CooperativeRef: coop.Ref, MemberSince: in.MemberSince, CreatedAt: now,
	}
	res := HouseholdResult{Duplicate: dup}
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		inserted, err := store.InsertHousehold(ctx, tx, p.TenantID, h, p.ActorRef)
		if err != nil {
			return err
		}
		res.Created = inserted
		if !inserted {
			res.Duplicate = true
		} else if err := enqueueRegistered(ctx, tx, p, h, householdSource(p, in.Source), now); err != nil {
			return err
		}
		cur, err := store.HouseholdByRef(ctx, tx, h.Ref)
		if err != nil {
			return err
		}
		if cur != nil {
			res.Household = *cur
		}
		return nil
	})
	return res, err
}

func householdSource(p authz.Principal, src string) string {
	switch {
	case src != "":
		return src
	case p.Has(partyport.RoleCoopManager) && !p.Has(partyport.RoleKNOfficer) && !p.Has(partyport.RoleKNProvinceAdmin) && !p.Has(partyport.RoleRegistryAdmin):
		return port.HouseholdSourceHTXApp
	}
	return port.HouseholdSourceKNApp
}

// enqueueRegistered phát pf.party.household.registered cùng giao dịch ghi nông hộ (KN US-HTX-05).
func enqueueRegistered(ctx context.Context, tx pgx.Tx, p authz.Principal, h domain.Household, source string, now time.Time) error {
	pl := port.HouseholdRegisteredV1{HouseholdRef: h.Ref, CommuneCode: h.CommuneCode, Source: source, ActorRef: p.ActorRef,
		EventTime: now.UTC().Format(time.RFC3339)}
	if h.CooperativeRef != uuid.Nil {
		c := h.CooperativeRef
		pl.CooperativeRef = &c
		since := h.CreatedAt.Format(time.DateOnly) // như store.InsertHousehold
		if h.MemberSince != nil {
			since = h.MemberSince.Format(time.DateOnly)
		}
		pl.MemberSince = &since
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: p.TenantID, Type: port.EventHouseholdRegistered, SchemaVersion: 1, OccurredAt: now,
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: pl,
	})
}

// HouseholdView là nông hộ kèm nhãn hiển thị tối thiểu từ kho định danh.
type HouseholdView struct {
	domain.Household
	Label       string `pii:"true"` // đã che bớt, vd "Nguyễn V. A."
	MaskedPhone string `pii:"true"`
	// LabelUnavailable: kho định danh không trả nhãn (đã xóa định danh hoặc lỗi).
	LabelUnavailable bool
}

// HouseholdPage là một trang nông hộ.
type HouseholdPage struct {
	Items []HouseholdView
	Next  *uuid.UUID
}

// ListHouseholds liệt kê nông hộ của một HTX trong phạm vi, kèm nhãn hiển thị
// qua kho định danh (mục đích extension.field_support) — không bao giờ trả định danh đầy đủ.
//
// phone khác rỗng: tìm đúng SĐT qua chỉ mục mù của kho định danh (HTX US-HH-01 AC-2) —
// trả tối đa một hộ của HTX này. Họ tên chỉ nằm ở kho (R-04) nên không tìm theo tên.
func (s *Service) ListHouseholds(ctx context.Context, coopRef, after uuid.UUID, limit int, phone string) (HouseholdPage, error) {
	p, err := authz.Require(ctx, householdReaderRoles...)
	if err != nil {
		return HouseholdPage{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	sc, err := s.scopeFor(ctx)
	if err != nil {
		return HouseholdPage{}, err
	}
	phone = strings.TrimSpace(phone)
	byPhone := uuid.Nil
	if phone != "" {
		// Kho định danh tự kiểm vai vault_registrar trên token được chuyển tiếp.
		if byPhone, err = s.householdByPhone(ctx, phone); err != nil {
			return HouseholdPage{}, err
		}
	}
	coop, err := s.coopOrg(ctx, p.TenantID, coopRef)
	if err != nil {
		return HouseholdPage{}, err
	}
	var items []domain.Household
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if coop == nil || !visible(sc, coop.AdminUnitCode) {
			return nil
		}
		if phone != "" {
			if byPhone == uuid.Nil {
				return nil
			}
			h, e := store.HouseholdByRef(ctx, tx, byPhone)
			if e == nil && h != nil && h.CooperativeRef == coopRef {
				items = []domain.Household{*h}
			}
			return e
		}
		items, e = store.ListHouseholdsByCooperative(ctx, tx, coopRef, after, limit+1)
		return e
	})
	if err != nil {
		return HouseholdPage{}, err
	}
	if coop == nil || !visible(sc, coop.AdminUnitCode) {
		return HouseholdPage{}, ErrOrgNotFound
	}
	if p.Has(partyport.RoleCoopManager) && !p.Has(partyport.RoleRegistryAdmin) && !p.Has(partyport.RoleKNProvinceAdmin) &&
		!p.Has(partyport.RoleKNOfficer) && !p.Has(partyport.RoleKNNationalAdmin) && !p.Has(authz.RolePlatformAdmin) && coop.Ref != p.OrgRef {
		return HouseholdPage{}, ErrOrgNotFound
	}
	var pg HouseholdPage
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].Ref
		pg.Next = &last
	}
	pg.Items = make([]HouseholdView, 0, len(items))
	for _, h := range items {
		v := HouseholdView{Household: h}
		if s.Vault == nil {
			v.LabelUnavailable = true
		} else if d, err := s.Vault.Label(ctx, port.IdentityFieldSupport, h.Ref); err != nil {
			v.LabelUnavailable = true
		} else {
			v.Label, v.MaskedPhone = d.Label, d.MaskedPhone
		}
		pg.Items = append(pg.Items, v)
	}
	return pg, nil
}
