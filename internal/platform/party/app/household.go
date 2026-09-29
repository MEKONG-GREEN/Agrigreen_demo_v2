package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
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
}

// HouseholdResult là kết quả tạo một nông hộ.
type HouseholdResult struct {
	Household domain.Household
	// Duplicate: chủ hộ đã có trong kho định danh (trùng SĐT/CCCD) — dùng lại mã cũ, báo "trùng".
	Duplicate bool
	// Created: bản ghi nông hộ ở pf_party vừa được tạo trong lần gọi này.
	Created bool
}

// DuplicateLabel là nhãn báo trùng hiển thị cho người dùng.
const DuplicateLabel = "trùng"

// vaultError phân loại lỗi của kho định danh: kho từ chối dữ liệu hoặc quyền (4xx,
// vd SĐT/CCCD sai dạng, thiếu vai vault_registrar) là lỗi của người gọi — trả
// nguyên problem của kho để client sửa, không thử lại; lỗi hệ thống hoặc không
// phản hồi → 503 party.dependency_unavailable.
func vaultError(err error) error {
	var p *problem.Problem
	if errors.As(err, &p) && p.Status >= 400 && p.Status < 500 {
		return p
	}
	return ErrDependency.WithDetail("kho định danh từ chối hoặc không phản hồi").WithCause(err)
}

// CreateHousehold tạo chủ hộ trong kho định danh (mục đích registry.admin) rồi
// ghi nông hộ chỉ chứa mã tham chiếu. Trùng chỉ mục mù → dùng lại mã cũ.
func (r *Registry) CreateHousehold(ctx context.Context, in CreateHouseholdInput) (HouseholdResult, error) {
	p, err := authz.Require(ctx, householdWriterRoles...)
	if err != nil {
		return HouseholdResult{}, err
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return HouseholdResult{}, err
	}
	return r.createHousehold(ctx, p, sc, in)
}

func (r *Registry) createHousehold(ctx context.Context, p authz.Principal, sc authz.Scope, in CreateHouseholdInput) (HouseholdResult, error) {
	now := r.now()
	if strings.TrimSpace(in.Identity.FullName) == "" || strings.TrimSpace(in.Identity.Phone) == "" {
		return HouseholdResult{}, problem.Validation.WithField("identity", domain.ErrIdentity.Error())
	}
	if in.CooperativeRef == uuid.Nil {
		return HouseholdResult{}, problem.Validation.WithField("cooperative_ref", domain.ErrCooperative.Error())
	}
	if err := domain.ValidateHousehold(in.CultivatedAreaHa, in.MemberSince, now); err != nil {
		return HouseholdResult{}, fieldProblem(err)
	}
	if r.Vault == nil {
		return HouseholdResult{}, ErrDependency.WithDetail("chưa nối kho định danh")
	}

	// HTX phải trong phạm vi và đang hoạt động; nông hộ kế thừa phạm vi của HTX (KN US-HTX-02).
	var coop *store.Cooperative
	if err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		coop, e = store.CooperativeByRef(ctx, tx, in.CooperativeRef)
		return e
	}); err != nil {
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
	if !p.Has(port.RoleRegistryAdmin) && !p.Has(port.RoleKNProvinceAdmin) && !p.Has(port.RoleKNOfficer) && coop.Ref != p.OrgRef {
		return HouseholdResult{}, ErrOutOfScope.WithDetail("Bạn không có quyền truy cập nông hộ này.")
	}
	commune := strings.TrimSpace(in.CommuneCode)
	if commune == "" {
		commune = coop.AdminUnitCode
	}
	if !sc.Allows(commune) {
		return HouseholdResult{}, ErrOutOfScope.WithDetail("xã của nông hộ ngoài phạm vi phụ trách")
	}

	created, err := r.Vault.Create(ctx, client.PurposeRegistryAdmin, client.Identity{
		Kind: client.SubjectHousehold, FullName: strings.TrimSpace(in.Identity.FullName), Phone: strings.TrimSpace(in.Identity.Phone),
		NationalID: strings.TrimSpace(in.Identity.NationalID), Address: strings.TrimSpace(in.Identity.Address), AreaCode: commune,
	})
	dup := false
	switch {
	case errors.Is(err, client.ErrDuplicate) && created.Ref != uuid.Nil:
		dup = true
	case err != nil:
		return HouseholdResult{}, vaultError(err)
	}

	h := domain.Household{
		Ref: created.Ref, CommuneCode: commune, CultivatedAreaHa: in.CultivatedAreaHa, Active: true,
		CooperativeRef: coop.Ref, MemberSince: in.MemberSince, CreatedAt: now,
	}
	res := HouseholdResult{Duplicate: dup}
	err = tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
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
	case p.Has(port.RoleCoopManager) && !p.Has(port.RoleKNOfficer) && !p.Has(port.RoleKNProvinceAdmin) && !p.Has(port.RoleRegistryAdmin):
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
		TraceID: telemetry.TraceID(ctx), Producer: "pf/party", Payload: pl,
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
func (r *Registry) ListHouseholds(ctx context.Context, coopRef, after uuid.UUID, limit int) (HouseholdPage, error) {
	p, err := authz.Require(ctx, householdReaderRoles...)
	if err != nil {
		return HouseholdPage{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return HouseholdPage{}, err
	}
	var coop *store.Cooperative
	var items []domain.Household
	err = tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if coop, e = store.CooperativeByRef(ctx, tx, coopRef); e != nil || coop == nil || !visible(sc, coop.AdminUnitCode) {
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
	if p.Has(port.RoleCoopManager) && !p.Has(port.RoleRegistryAdmin) && !p.Has(port.RoleKNProvinceAdmin) &&
		!p.Has(port.RoleKNOfficer) && !p.Has(port.RoleKNNationalAdmin) && !p.Has(authz.RolePlatformAdmin) && coop.Ref != p.OrgRef {
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
		if r.Vault == nil {
			v.LabelUnavailable = true
		} else if d, err := r.Vault.Display(ctx, client.PurposeFieldSupport, h.Ref); err != nil {
			v.LabelUnavailable = true
		} else {
			v.Label, v.MaskedPhone = d.Label, d.MaskedPhone
		}
		pg.Items = append(pg.Items, v)
	}
	return pg, nil
}
