package port

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

// MenuMode selects a presentation, not authority. All identity stays in ctx.
type MenuMode string

const (
	MenuDepartment        MenuMode = "department"
	MenuAdmin             MenuMode = "admin"
	MenuDepartmentModules MenuMode = "department_modules"
)

// MenuBuildRequest contains server-validated navigation and current tenant-read
// department metadata. A builder must not infer authority from requested routes.
type MenuBuildRequest struct {
	NavigationRequest
	Mode        MenuMode
	Departments []Department
}

// MenuItem is resolved owner presentation. Module is mandatory on routed leaves;
// distinct presentation nodes may link to the same owned route. Enabled includes
// owner availability/traits. Shared module gating can only hide it.
type MenuItem struct {
	Key, Label, Route, Module, Icon, BadgeSource string
	Enabled                                      bool
	Status, StatusLabel                          string
	Children                                     []MenuItem
}
type MenuGroup struct {
	Key, Label string
	Items      []MenuItem
}
type MenuDefinition struct {
	Version, DefaultRoute string
	Groups                []MenuGroup
}
type MenuBuilder func(context.Context, MenuBuildRequest) (MenuDefinition, error)

// MenuRoute registers ownership of one internal route pattern. Placeholders are
// generic whole segments, e.g. /workspace/{department}/reports. Overlaps fail.
type MenuRoute struct{ Pattern, Module string }

var (
	ErrMenuRegistration = errors.New("party: invalid menu registration")
	ErrMenuDefinition   = errors.New("party: invalid menu definition")
)

// Menus is an immutable instance registration. There is no process-wide catalog.
// The owner callback remains responsible for its own state and authorization.
type Menus struct {
	build  MenuBuilder
	routes []MenuRoute
}

func NewMenus(builder MenuBuilder, routes ...MenuRoute) (*Menus, error) {
	if builder == nil || len(routes) == 0 || len(routes) > 4096 {
		return nil, ErrMenuRegistration
	}
	out := &Menus{build: builder, routes: slices.Clone(routes)}
	for i, route := range out.routes {
		if !validMenuPath(route.Pattern, true) {
			return nil, ErrMenuRegistration
		}
		if _, ok := modreg.Entry(route.Module); !ok {
			return nil, ErrMenuRegistration
		}
		for _, previous := range out.routes[:i] {
			if menuPatternsOverlap(previous.Pattern, route.Pattern) {
				return nil, ErrMenuRegistration
			}
		}
	}
	return out, nil
}

func menuPlaceholder(segment string) bool {
	if len(segment) < 3 || segment[0] != '{' || segment[len(segment)-1] != '}' {
		return false
	}
	for i, c := range segment[1 : len(segment)-1] {
		if (c < 'a' || c > 'z') && (i == 0 || (c != '_' && (c < '0' || c > '9'))) {
			return false
		}
	}
	return true
}
func validMenuPath(path string, pattern bool) bool {
	if len(path) == 0 || len(path) > 512 || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\?#% \t\r\n") {
		return false
	}
	if path == "/" {
		return true
	}
	for _, segment := range strings.Split(path[1:], "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		if strings.ContainsAny(segment, "{}") && (!pattern || !menuPlaceholder(segment)) {
			return false
		}
	}
	return true
}
func menuPatternsOverlap(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if as[i] != bs[i] && !menuPlaceholder(as[i]) && !menuPlaceholder(bs[i]) {
			return false
		}
	}
	return true
}
func (m *Menus) owner(route string) string {
	for _, registration := range m.routes {
		if menuPatternsOverlap(registration.Pattern, route) {
			return registration.Module
		}
	}
	return ""
}

// Build clones the request and validates/clones every result. A callback error is
// final. Empty/unknown/default routes and ambiguous output never reach clients.
func (m *Menus) Build(ctx context.Context, request MenuBuildRequest) (MenuDefinition, error) {
	if m == nil || m.build == nil {
		return MenuDefinition{}, ErrMenuRegistration
	}
	switch request.Mode {
	case MenuDepartment, MenuAdmin, MenuDepartmentModules:
	default:
		return MenuDefinition{}, ErrMenuDefinition
	}
	request.Duties = slices.Clone(request.Duties)
	request.Departments = slices.Clone(request.Departments)
	value, err := m.build(ctx, request)
	if err != nil {
		return MenuDefinition{}, err
	}
	if value.Version == "" || len(value.Version) > 128 || !validMenuPath(value.DefaultRoute, false) || m.owner(value.DefaultRoute) == "" || len(value.Groups) > 64 {
		return MenuDefinition{}, ErrMenuDefinition
	}
	out := MenuDefinition{Version: value.Version, DefaultRoute: value.DefaultRoute, Groups: make([]MenuGroup, 0, len(value.Groups))}
	count := 0
	var cloneItems func([]MenuItem, int) ([]MenuItem, error)
	cloneItems = func(items []MenuItem, depth int) ([]MenuItem, error) {
		if depth > 12 {
			return nil, ErrMenuDefinition
		}
		out := make([]MenuItem, 0, len(items))
		keys := map[string]bool{}
		for _, item := range items {
			count++
			if count > 2048 || item.Key == "" || len(item.Key) > 256 || keys[item.Key] || item.Label == "" || len(item.Label) > 1024 {
				return nil, ErrMenuDefinition
			}
			keys[item.Key] = true
			if len(item.Children) > 0 {
				if item.Route != "" || item.Module != "" {
					return nil, ErrMenuDefinition
				}
			} else {
				if !validMenuPath(item.Route, false) || m.owner(item.Route) != item.Module || item.Module == "" {
					return nil, ErrMenuDefinition
				}
			}
			var err error
			item.Children, err = cloneItems(item.Children, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	}
	groups := map[string]bool{}
	for _, group := range value.Groups {
		if group.Key == "" || len(group.Key) > 256 || groups[group.Key] || group.Label == "" || len(group.Label) > 1024 {
			return MenuDefinition{}, ErrMenuDefinition
		}
		groups[group.Key] = true
		group.Items, err = cloneItems(group.Items, 0)
		if err != nil {
			return MenuDefinition{}, err
		}
		out.Groups = append(out.Groups, group)
	}
	return out, nil
}
