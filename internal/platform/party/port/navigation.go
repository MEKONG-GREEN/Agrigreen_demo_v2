package port

import (
	"context"
	"errors"
	"maps"
	"strings"

	"github.com/google/uuid"
)

// NavigationRequest is trusted server state, after the caller has validated its
// selected assignment and department. It carries no identity or extra authority.
type NavigationRequest struct {
	Audience     string
	UnitRef      uuid.UUID
	Department   Department
	FilterTraits bool
	Manager      bool
	Duties       []string
}

// NavigationPolicy returns preferred pages from application-owned registration.
// An error is final. Selection still requires an enabled leaf of the actual menu.
type NavigationPolicy func(NavigationRequest) ([]string, error)

// Navigation resolves an actual landing page after current menu/module filters.
type Navigation func(context.Context, NavigationRequest) (string, error)

// RegisteredNavigation builds a private immutable copy of application data.
// Unknown profiles retain the supplied audience default; no policy is inferred.
func RegisteredNavigation(defaults map[string]string, profiles map[string]map[string]string) (NavigationPolicy, error) {
	valid := func(route string) bool {
		return strings.HasPrefix(route, "/") && !strings.HasPrefix(route, "//") && !strings.ContainsAny(route, "\\?#\r\n")
	}
	base := maps.Clone(defaults)
	perProfile := make(map[string]map[string]string, len(profiles))
	for audience, route := range base {
		if audience == "" || !valid(route) {
			return nil, errors.New("navigation: invalid audience default")
		}
	}
	for profile, routes := range profiles {
		if profile == "" {
			return nil, errors.New("navigation: empty profile")
		}
		perProfile[profile] = maps.Clone(routes)
		for audience, route := range routes {
			if _, ok := base[audience]; !ok || !valid(route) {
				return nil, errors.New("navigation: invalid profile route")
			}
		}
	}
	return func(in NavigationRequest) ([]string, error) {
		fallback, ok := base[in.Audience]
		if !ok {
			return nil, errors.New("navigation: unknown audience")
		}
		if preferred := perProfile[in.Department.MenuProfile][in.Audience]; preferred != "" && preferred != fallback {
			return []string{preferred, fallback}, nil
		}
		return []string{fallback}, nil
	}, nil
}
