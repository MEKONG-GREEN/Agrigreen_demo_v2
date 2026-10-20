package app

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ApplyNavigation chooses an enabled leaf after all menu filters. Application
// preferences never grant access to a route absent from the resolved menu.
func ApplyNavigation(view MenuView, policy port.NavigationPolicy) (MenuView, error) {
	if policy == nil {
		return view, nil
	}
	request := port.NavigationRequest{Audience: view.Audience, UnitRef: view.UnitRef}
	if view.Department != nil {
		request.Department = *view.Department
		request.Department.MenuProfile = domain.EffectiveMenuProfile(request.Department.MenuProfile, request.Department.Code)
	}
	preferred, err := policy(request)
	if err != nil {
		return MenuView{}, err
	}
	routes := []string{}
	var visit func([]MenuItemView)
	visit = func(items []MenuItemView) {
		for _, item := range items {
			if !item.Enabled {
				continue
			}
			if len(item.Children) > 0 {
				visit(item.Children)
				continue
			}
			if strings.HasPrefix(item.Route, "/") && !strings.HasPrefix(item.Route, "//") {
				routes = append(routes, item.Route)
			}
		}
	}
	for _, group := range view.Groups {
		visit(group.Items)
	}
	for _, want := range preferred {
		for _, available := range routes {
			if want == available {
				view.DefaultRoute = want
				return view, nil
			}
		}
	}
	if len(routes) == 0 {
		return MenuView{}, problem.Forbidden.WithDetail("không có trang khả dụng trong menu")
	}
	view.DefaultRoute = routes[0]
	return view, nil
}

// ContextNavigation is a server-to-server callback. The caller has already
// validated the assignment, department and attributes; identity stays in ctx.
func (k *KN) ContextNavigation(ctx context.Context, in port.NavigationRequest) (string, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return "", problem.Unauthenticated
	}
	var view MenuView
	if in.Audience == port.AudienceAdmin {
		var departments []port.Department
		err := tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
			var err error
			if in.UnitRef == uuid.Nil {
				departments, err = store.ListAllDepartments(ctx, tx)
			} else {
				departments, err = store.ListDepartments(ctx, tx, in.UnitRef, false)
			}
			return err
		})
		if err != nil {
			return "", err
		}
		view = AdminMenu(in.UnitRef, departments)
		view.Audience = in.Audience
	} else {
		if !in.Department.Selectable() {
			return "", ErrDepartmentNotOpen
		}
		groups, err := departmentGroupsWithTraits(ctx, k.Forms, in.Department, in.Audience, domain.MenuTraits{Manager: in.Manager, Duties: in.Duties}, in.FilterTraits)
		if err != nil {
			return "", err
		}
		d := in.Department
		view = MenuView{Version: domain.MenuVersion, Audience: in.Audience, UnitRef: in.UnitRef, Department: &d, Groups: groups, DefaultRoute: domain.DefaultRoute(in.Audience)}
	}
	view, err := k.withModules(ctx, p.TenantID, view)
	return view.DefaultRoute, err
}
