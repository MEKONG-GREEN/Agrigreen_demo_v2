package domain

import (
	"strings"
	"time"
)

// NormalizeAssignmentStructure normalizes only generic code syntax, duplicate
// values and validity. Application role catalogs and role-specific combinations
// are checked separately by the configured owner rules. NormalizeAssignment
// remains the unchanged legacy entry point, including its error order.
func NormalizeAssignmentStructure(in AssignmentInput, now time.Time) (AssignmentInput, error) {
	in.Role = strings.TrimSpace(in.Role)
	in.MgmtLevel = strings.TrimSpace(in.MgmtLevel)
	if !deptCodeRe.MatchString(in.Role) {
		return in, FieldError{Field: "role", Msg: "mã vai trò không hợp lệ"}
	}
	if in.MgmtLevel != "" && !deptCodeRe.MatchString(in.MgmtLevel) {
		return in, FieldError{Field: "mgmt_level", Msg: "mã cấp quản lý không hợp lệ"}
	}
	var err error
	if in.Departments, err = dedupe(in.Departments, deptCodeRe, "department_codes", MsgKNDeptCode); err != nil {
		return in, err
	}
	if in.Communes, err = dedupe(in.Communes, communeCodeRe, "commune_codes", MsgKNCommuneCode); err != nil {
		return in, err
	}
	if in.ValidFrom.IsZero() {
		in.ValidFrom = now
	}
	in.ValidFrom = in.ValidFrom.UTC().Truncate(time.Microsecond)
	if in.ValidTo != nil {
		t := in.ValidTo.UTC().Truncate(time.Microsecond)
		if !t.After(in.ValidFrom) {
			return in, FieldError{Field: "valid_to", Msg: MsgKNValidity}
		}
		in.ValidTo = &t
	}
	return in, nil
}
