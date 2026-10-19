package extension

import (
	"context"
	reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"testing"
)

func TestReportSessionPolicyRolesAndDepartments(t *testing.T) {
	resolve := ReportSessionResolver(nil)
	for _, role := range []string{authz.KNAdmin, authz.KNTW, authz.KNTinh, authz.KNXa} {
		p := authz.Principal{KN: &authz.KNContext{Role: role, Department: "department"}}
		a, handled, err := resolve(context.Background(), bindCapabilityTestPrincipal(t, p), reportsport.SessionReadForms)
		if !handled || err != nil || !a.Allowed {
			t.Fatalf("form role %s: %+v %v", role, a, err)
		}
		if !a.AllowsDepartment("department") || (a.AllowsDepartment("other") != (role == authz.KNAdmin)) {
			t.Fatalf("department %s: %+v", role, a)
		}
		a, handled, err = resolve(context.Background(), bindCapabilityTestPrincipal(t, p), reportsport.SessionWriteDispatch)
		if !handled || (err == nil) != (role == authz.KNTinh) || a.Allowed != (role == authz.KNTinh) {
			t.Fatalf("write role %s: %+v %v", role, a, err)
		}
		a, _, err = resolve(context.Background(), bindCapabilityTestPrincipal(t, p), reportsport.SessionReadDispatch)
		allowed := role == authz.KNTinh || role == authz.KNTW || role == authz.KNAdmin
		if (err == nil) != allowed || a.UnmaskRecipients != (role == authz.KNTinh) {
			t.Fatalf("read/PII %s: %+v %v", role, a, err)
		}
	}
	p := authz.Principal{KN: &authz.KNContext{Role: authz.KNTinh, Department: "department", ViewOnly: true}}
	if _, _, err := resolve(context.Background(), bindCapabilityTestPrincipal(t, p), reportsport.SessionWriteDispatch); err == nil {
		t.Fatal("view-only session wrote dispatch")
	}
	if _, handled, err := resolve(context.Background(), authz.Principal{}, reportsport.SessionReadForms); handled || err != nil {
		t.Fatalf("non-application session claimed: %v %v", handled, err)
	}
}
