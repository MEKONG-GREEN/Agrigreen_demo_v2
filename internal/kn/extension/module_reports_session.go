package extension

import (
	"context"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"net/http"
	"slices"
)

// ReportSessionResolver owns KN policy for cap/reports. It only projects
// permissions and filters; the capability binds Principal actor and tenant.
func ReportSessionResolver(orgs partyport.Orgs) reportsport.SessionResolver {
	return func(ctx context.Context, p authz.Principal, operation string) (reportsport.SessionActor, bool, error) {
		// Keep native realm requests unhandled; the legacy marker supplies no permissions.
		if p.SelectedSession == nil && p.KN == nil {
			return reportsport.SessionActor{}, false, nil
		}
		_, k, err := extport.RequireSession(authz.WithPrincipal(ctx, p))
		if err != nil {
			return reportsport.SessionActor{}, true, err
		}
		a := reportsport.SessionActor{Department: k.Department, AllDepartments: k.Role == extport.SessionRoleAdmin,
			AllowsDepartment: k.AllowsDepartment, UnmaskRecipients: k.Role == extport.SessionRoleProvince}
		if k.Support != nil {
			id := k.Support.OnBehalfOf
			a.OnBehalfOf = &id
		}
		switch operation {
		case reportsport.SessionReadForms:
			a.Allowed = slices.Contains([]string{extport.SessionRoleAdmin, extport.SessionRoleNational, extport.SessionRoleProvince, extport.SessionRoleCommune}, k.Role)
			return a, true, nil
		case reportsport.SessionReadDispatch:
			a.Allowed = slices.Contains([]string{extport.SessionRoleAdmin, extport.SessionRoleNational, extport.SessionRoleProvince}, k.Role)
		case reportsport.SessionWriteDispatch:
			a.Allowed = k.Role == extport.SessionRoleProvince && !k.ReadOnly()
		default:
			return a, true, problem.Forbidden
		}
		if !a.Allowed {
			return a, true, problem.Forbidden
		}
		if k.Role == extport.SessionRoleNational || k.Role == extport.SessionRoleAdmin || k.OrgRef == uuid.Nil {
			a.AllProvinces = true
			return a, true, nil
		}
		if orgs == nil {
			return a, true, problem.New(http.StatusServiceUnavailable, "reports.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng").WithDetail("chưa nối cây tổ chức (pf/party)")
		}
		org, err := orgs.Get(ctx, k.OrgRef)
		if err != nil {
			return a, true, err
		}
		if org.Kind != partyport.OrgTTKNNational {
			a.Province = org.AdminUnitCode
		} else {
			a.AllProvinces = true
		}
		return a, true, nil
	}
}
