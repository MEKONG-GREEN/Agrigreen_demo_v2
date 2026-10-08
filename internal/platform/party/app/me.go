package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Cấp phạm vi trả ở GET /v1/me (ADR-0009). Client chỉ dùng để hiển thị; quyền
// thật vẫn do từng module kiểm bằng ScopeResolver.
const (
	LevelNational    = "national"
	LevelProvince    = "province"
	LevelCommune     = "commune"
	LevelCooperative = "cooperative"
	LevelNone        = "none"
)

// MeOrg là đơn vị công tác của người gọi (tên tổ chức không phải dữ liệu cá nhân).
type MeOrg struct {
	Ref           uuid.UUID
	Kind          port.OrgKind
	Name          string
	AdminUnitCode string
	Active        bool
}

// Me là người gọi hiện tại và phạm vi đã giải. KHÔNG chứa dữ liệu cá nhân
// (họ tên, SĐT… của cán bộ nằm ở Keycloak/kho định danh, không ở đây — R-04).
type Me struct {
	ActorRef  uuid.UUID
	TenantID  uuid.UUID
	Roles     []string
	MFA       bool
	Org       *MeOrg // nil khi token không có org_ref hoặc tổ chức không tồn tại
	Scope     authz.Scope
	Level     string
	UnitNames map[string]string // mã đơn vị → tên, theo masterdata tại thời điểm hiện tại
}

// Me trả thông tin người gọi cho giao diện (GET /v1/me). Mọi người gọi đã xác
// thực đều gọi được; không cần vai nghiệp vụ.
func (r *Registry) Me(ctx context.Context) (Me, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return Me{}, problem.Unauthenticated
	}
	roles := p.Roles
	if roles == nil {
		roles = []string{}
	}
	out := Me{ActorRef: p.ActorRef, TenantID: p.TenantID, Roles: roles, MFA: p.MFA, UnitNames: map[string]string{}}
	if p.OrgRef != uuid.Nil {
		var s *store.OrgState
		err := tenant.WithTenantTx(ctx, r.DB, p.TenantID, func(tx pgx.Tx) error {
			var e error
			s, e = store.OrgByRef(ctx, tx, p.OrgRef, false)
			return e
		})
		if err != nil {
			return Me{}, err
		}
		if s != nil {
			out.Org = &MeOrg{Ref: s.Ref, Kind: s.Kind, Name: s.Name, AdminUnitCode: s.AdminUnitCode, Active: s.Active}
		}
	}
	sc, err := r.scopeFor(ctx, p)
	if err != nil {
		return Me{}, err
	}
	if sc.Units == nil {
		sc.Units = []string{}
	}
	out.Scope = sc
	out.Level = levelOf(sc, out.Org)
	if err := r.fillUnitNames(ctx, &out); err != nil {
		return Me{}, err
	}
	return out, nil
}

// levelOf suy cấp phạm vi từ phạm vi đã giải và loại tổ chức.
func levelOf(sc authz.Scope, org *MeOrg) string {
	switch {
	case sc.All:
		return LevelNational
	case sc.Empty() || org == nil:
		return LevelNone
	}
	switch org.Kind {
	case port.OrgTTKNProvince:
		return LevelProvince
	case port.OrgExtensionTeam:
		return LevelCommune
	case port.OrgCooperative:
		return LevelCooperative
	}
	return LevelNone
}

// fillUnitNames tra tên các đơn vị trong phạm vi và đơn vị của tổ chức. Đơn vị
// không còn trong danh mục thì bỏ qua (giao diện hiển thị mã).
func (r *Registry) fillUnitNames(ctx context.Context, m *Me) error {
	if r.Units == nil {
		return nil
	}
	codes := append([]string(nil), m.Scope.Units...)
	if m.Org != nil && m.Org.AdminUnitCode != "" {
		codes = append(codes, m.Org.AdminUnitCode)
	}
	now := r.Clock.Now()
	for _, code := range codes {
		if _, done := m.UnitNames[code]; done {
			continue
		}
		u, err := r.Units.Get(ctx, code, now)
		if errors.Is(err, port.ErrUnitNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		m.UnitNames[code] = u.Name
	}
	return nil
}
