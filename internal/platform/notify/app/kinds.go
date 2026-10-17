package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"regexp"
)

var kindKeyPattern = regexp.MustCompile("^[a-z][a-z_]{0,39}$")

type kindRegistry map[string]bool

func (s *Service) RegisterKinds(registrations []port.KindRegistration, native []string) {
	if s.kinds != nil {
		panic("notify: notification kinds already initialized")
	}
	kinds := kindRegistry{}
	for _, kind := range native {
		if !kindKeyPattern.MatchString(kind) || kinds[kind] {
			panic("notify: invalid or duplicate native kind " + kind)
		}
		kinds[kind] = true
	}
	for _, registration := range registrations {
		if len(registration.Kinds) == 0 {
			panic("notify: empty kind registration")
		}
		for _, kind := range registration.Kinds {
			if !kindKeyPattern.MatchString(kind) || kinds[kind] {
				panic("notify: invalid, duplicate or native kind " + kind)
			}
			kinds[kind] = true
		}
	}
	s.kinds = kinds
}
