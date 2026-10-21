package app

import (
	"slices"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

func (s *Service) normalizeAssignment(in domain.AssignmentInput, now time.Time) (domain.AssignmentInput, error) {
	if s.AssignmentRules == nil {
		return domain.NormalizeAssignment(in, now)
	}
	out, err := domain.NormalizeAssignmentStructure(in, now)
	if err != nil {
		return out, err
	}
	values := port.AssignmentRuleValues{Role: out.Role, OrgRef: out.OrgRef,
		Departments: slices.Clone(out.Departments), MgmtLevel: out.MgmtLevel, Communes: slices.Clone(out.Communes)}
	if err := s.AssignmentRules.ValidateAssignment(values); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) validateAssignmentNodeKind(role, kind string) error {
	if s.AssignmentRules == nil {
		return domain.KNRoleFitsOrg(role, kind)
	}
	return s.AssignmentRules.ValidateNodeKind(role, kind)
}
