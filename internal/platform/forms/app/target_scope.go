package app

import (
	"slices"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
)

// filterTargets projects only response metadata; it never changes stored events,
// snapshot bytes or the unfiltered views used by write authorization.
func (a actor) filterTargets(v *PeriodView) {
	if a.targetFilter == nil {
		return
	}
	allowed := make(map[string]bool, len(v.Period.Units))
	units := make([]string, 0, len(v.Period.Units))
	var parents []string
	aligned := len(v.Spec.UnitParents) == len(v.Period.Units)
	for i, u := range v.Period.Units {
		if !a.allowsUnit(v.Spec, v.Period.Units, u) {
			continue
		}
		allowed[u] = true
		units = append(units, u)
		if aligned {
			parents = append(parents, v.Spec.UnitParents[i])
		}
	}
	v.Units = slices.DeleteFunc(v.Units, func(u UnitView) bool { return !allowed[u.Unit] })
	v.Period.Units = units
	if aligned {
		v.Spec.UnitParents = parents
	}
	events := make([]domain.Event, 0, len(v.Events))
	for _, event := range v.Events {
		if len(event.Units) != 0 {
			event.Units = slices.DeleteFunc(slices.Clone(event.Units), func(u string) bool { return !allowed[u] })
			if len(event.Units) == 0 {
				// Empty reopened units mean all. An event wholly outside the
				// visible targets must not become a visible all-target reopen.
				continue
			}
		}
		events = append(events, event)
	}
	v.Events = events
	v.State = domain.StateOf(events)
}
