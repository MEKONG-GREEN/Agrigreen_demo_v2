package app

import (
	"context"
	"errors"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// registeredMenu keeps identity/tenant selection outside the owner's presentation
// callback. A configured failure must never fall back to the compatibility menu.
func (k *KN) registeredMenu(ctx context.Context, tid uuid.UUID, request port.MenuBuildRequest) (MenuView, error) {
	def, err := k.Menus.Build(ctx, request)
	if err != nil {
		if errors.Is(err, port.ErrMenuDefinition) || errors.Is(err, port.ErrMenuRegistration) {
			return MenuView{}, ErrDependency.WithDetail("cấu hình menu chưa sẵn sàng").WithCause(err)
		}
		return MenuView{}, err
	}
	disabled := map[string]bool{}
	if k.Modules != nil {
		disabled, err = k.Modules.DisabledModules(ctx, tid)
		if err != nil {
			return MenuView{}, err
		}
	}
	var convert func([]port.MenuItem) []MenuItemView
	convert = func(items []port.MenuItem) []MenuItemView {
		out := []MenuItemView{}
		for _, item := range items {
			if len(item.Children) == 0 && disabled[item.Module] {
				continue
			}
			kids := convert(item.Children)
			if len(item.Children) > 0 && len(kids) == 0 {
				continue
			}
			out = append(out, MenuItemView{Key: item.Key, Label: item.Label, Route: item.Route, Icon: item.Icon, BadgeSource: item.BadgeSource,
				Enabled: item.Enabled, Status: item.Status, StatusLabel: item.StatusLabel, Children: kids})
		}
		return out
	}
	view := MenuView{Version: def.Version, Audience: request.Audience, UnitRef: request.UnitRef, DefaultRoute: def.DefaultRoute, Groups: []MenuGroupView{}}
	if request.Department.Code != "" {
		d := request.Department
		view.Department = &d
	}
	for _, group := range def.Groups {
		items := convert(group.Items)
		if len(items) > 0 {
			view.Groups = append(view.Groups, MenuGroupView{Key: group.Key, Label: group.Label, Items: items})
		}
	}
	policy := k.NavigationPolicy
	if policy == nil {
		policy = func(port.NavigationRequest) ([]string, error) { return []string{def.DefaultRoute}, nil }
	}
	// Configured catalogs own profile aliases too; the legacy canonicalizer must
	// not silently reinterpret a registered application's metadata.
	return applyNavigation(view, policy, false)
}

func menuRequest(ctx context.Context, audience string, unit uuid.UUID, department port.Department) port.MenuBuildRequest {
	traits, filter := menuTraits(ctx)
	return port.MenuBuildRequest{NavigationRequest: port.NavigationRequest{Audience: audience, UnitRef: unit, Department: department,
		FilterTraits: filter, Manager: traits.Manager, Duties: traits.Duties}, Mode: port.MenuDepartment}
}

// checkMenuUnit binds explicit presentation queries to the actual selected
// session's node. It never upgrades a KN payload to platform administration.
func (k *KN) checkMenuUnit(ctx context.Context, p authz.Principal, unit uuid.UUID) error {
	if k.Menus == nil || k.Session == nil || p.Has(authz.RolePlatformAdmin) {
		return nil
	}
	if _, _, selected, ok := k.Session.Current(ctx); ok && unit != uuid.Nil && selected != unit {
		return problem.Forbidden
	}
	return nil
}

// registeredContextNavigation reloads metadata in the authenticated tenant before
// using the same builder/module filters as HTTP. The owner validated the selected
// assignment; no Principal or tenant is fabricated for presentation.
func (k *KN) registeredContextNavigation(ctx context.Context, p authz.Principal, in port.NavigationRequest) (string, error) {
	if p.TenantID == uuid.Nil || p.ActorRef == uuid.Nil {
		return "", problem.Unauthenticated
	}
	if err := k.checkMenuUnit(ctx, p, in.UnitRef); err != nil {
		return "", err
	}
	request := port.MenuBuildRequest{NavigationRequest: in, Mode: port.MenuDepartment}
	err := tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		if in.Audience == port.AudienceAdmin {
			request.Mode = port.MenuAdmin
			var err error
			if in.UnitRef == uuid.Nil {
				request.Departments, err = store.ListAllDepartments(ctx, tx)
			} else {
				request.Departments, err = store.ListDepartments(ctx, tx, in.UnitRef, false)
			}
			return err
		}
		if in.UnitRef == uuid.Nil || in.Department.Code == "" || in.Department.OrgRef != in.UnitRef {
			return problem.Forbidden
		}
		d, err := departmentByCodeTx(ctx, tx, in.UnitRef, in.Department.Code)
		if err != nil {
			return err
		}
		if d == nil {
			return ErrDepartmentNotFound
		}
		if !d.Selectable() {
			return ErrDepartmentNotOpen
		}
		request.Department = *d
		return nil
	})
	if err != nil {
		return "", err
	}
	view, err := k.registeredMenu(ctx, p.TenantID, request)
	return view.DefaultRoute, err
}
