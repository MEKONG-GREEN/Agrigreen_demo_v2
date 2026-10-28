package port

import "github.com/google/uuid"

// AssignmentRuleValues contains normalized assignment values for application
// policy validation. Identity, tenant, validity and storage metadata are absent.
// Accounts supplies independent copies of the slices and ignores mutations.
type AssignmentRuleValues struct {
	Role        string
	OrgRef      uuid.UUID
	Departments []string
	MgmtLevel   string
	Communes    []string
}

// AssignmentRules is trusted server configuration for one accounts instance.
// It validates application-owned role/management catalogs and node-kind rules;
// it cannot replace assignment values or grant account administration rights.
// An error from a configured provider is final, without legacy fallback.
type AssignmentRules interface {
	ValidateAssignment(AssignmentRuleValues) error
	ValidateNodeKind(role, kind string) error
}

// AssignmentRuleError lets owner adapters report field validation without
// importing accounts/domain. Other errors are propagated unchanged.
type AssignmentRuleError struct {
	Field string
	Msg   string
}

func (e AssignmentRuleError) Error() string { return e.Field + ": " + e.Msg }
