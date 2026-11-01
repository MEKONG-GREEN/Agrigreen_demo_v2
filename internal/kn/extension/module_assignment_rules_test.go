package extension_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

func ruleErr(t *testing.T, err error) accport.AssignmentRuleError {
	t.Helper()
	var re accport.AssignmentRuleError
	if err != nil && !errors.As(err, &re) {
		t.Fatalf("not an AssignmentRuleError: %T %v", err, err)
	}
	return re
}

// Bảng lỗi của adapter KN: cùng trường, câu chữ và thứ tự kiểm với luật cũ (accounts/domain.NormalizeAssignment).
// Parity HTTP end-to-end với bridge cũ ở tests/knaccounts.
func TestKNAssignmentRulesValidateAssignment(t *testing.T) {
	rules, org := extension.AssignmentRules(), uuid.New()
	cases := []struct {
		name  string
		in    accport.AssignmentRuleValues
		field string
		msg   string
	}{
		{"unknown role", accport.AssignmentRuleValues{Role: "kn_boss", OrgRef: org, Departments: []string{"tt"}}, "role", "vai trò phải là kn_tw | kn_tinh | kn_xa | admin"},
		{"realm role is not an assignment role", accport.AssignmentRuleValues{Role: "kn_officer", OrgRef: org, Departments: []string{"tt"}}, "role", "vai trò phải là kn_tw | kn_tinh | kn_xa | admin"},
		{"role before level", accport.AssignmentRuleValues{Role: "x_y", MgmtLevel: "khong_co"}, "role", "vai trò phải là kn_tw | kn_tinh | kn_xa | admin"},
		{"unknown level", accport.AssignmentRuleValues{Role: authz.KNTinh, OrgRef: org, Departments: []string{"tt"}, MgmtLevel: "khong_co"}, "mgmt_level", "cấp quản lý không có trong danh mục (tạm, chờ khách Q-05)"},
		{"level before combination", accport.AssignmentRuleValues{Role: authz.KNTinh, MgmtLevel: "khong_co"}, "mgmt_level", "cấp quản lý không có trong danh mục (tạm, chờ khách Q-05)"},
		{"admin with department", accport.AssignmentRuleValues{Role: authz.KNAdmin, Departments: []string{"tt"}}, "department_codes", "Admin không chọn phòng ban, không gán xã"},
		{"admin with commune", accport.AssignmentRuleValues{Role: authz.KNAdmin, Communes: []string{"30292"}}, "department_codes", "Admin không chọn phòng ban, không gán xã"},
		{"org required", accport.AssignmentRuleValues{Role: authz.KNTW, Departments: []string{"tt"}}, "org_ref", "bắt buộc Nút tổ chức của vai trò"},
		{"org before department", accport.AssignmentRuleValues{Role: authz.KNXa}, "org_ref", "bắt buộc Nút tổ chức của vai trò"},
		{"department required", accport.AssignmentRuleValues{Role: authz.KNTinh, OrgRef: org}, "department_codes", "bắt buộc ít nhất một phòng ban"},
		{"communes only for xa", accport.AssignmentRuleValues{Role: authz.KNTinh, OrgRef: org, Departments: []string{"tt"}, Communes: []string{"30292"}}, "commune_codes", "chỉ Cấp Xã có xã phụ trách"},
		{"tw communes", accport.AssignmentRuleValues{Role: authz.KNTW, OrgRef: org, Departments: []string{"tt"}, Communes: []string{"30292"}}, "commune_codes", "chỉ Cấp Xã có xã phụ trách"},
		{"admin ok", accport.AssignmentRuleValues{Role: authz.KNAdmin}, "", ""},
		{"admin with node ok", accport.AssignmentRuleValues{Role: authz.KNAdmin, OrgRef: org, MgmtLevel: "giam_doc"}, "", ""},
		{"xa ok", accport.AssignmentRuleValues{Role: authz.KNXa, OrgRef: org, Departments: []string{"tt"}, Communes: []string{"30292"}, MgmtLevel: "chuyen_vien"}, "", ""},
		{"xa without commune ok", accport.AssignmentRuleValues{Role: authz.KNXa, OrgRef: org, Departments: []string{"tt"}}, "", ""},
		{"tinh leader ok", accport.AssignmentRuleValues{Role: authz.KNTinh, OrgRef: org, Departments: []string{"tt", "cn"}, MgmtLevel: "lanh_dao_don_vi"}, "", ""},
	}
	for _, c := range cases {
		err := rules.ValidateAssignment(c.in)
		re := ruleErr(t, err)
		if (err == nil) != (c.field == "") || re.Field != c.field || re.Msg != c.msg {
			t.Errorf("%s: got %v", c.name, err)
		}
	}
	// Adapter không sửa đầu vào (accounts chỉ truyền bản sao, nhưng adapter cũng không được ghi).
	in := accport.AssignmentRuleValues{Role: authz.KNXa, OrgRef: org, Departments: []string{"tt"}, Communes: []string{"30292"}}
	before := accport.AssignmentRuleValues{Role: in.Role, OrgRef: in.OrgRef, Departments: slices.Clone(in.Departments), Communes: slices.Clone(in.Communes)}
	_ = rules.ValidateAssignment(in)
	if !reflect.DeepEqual(in, before) {
		t.Fatal("adapter mutated its input")
	}
}

func TestKNAssignmentRulesNodeKind(t *testing.T) {
	rules := extension.AssignmentRules()
	cases := []struct{ role, kind, msg string }{
		{authz.KNTW, "ttkn_national", ""},
		{authz.KNTW, "ttkn_province", "Nút không đúng cấp của vai trò (ttkn_national)"},
		{authz.KNTinh, "ttkn_province", ""},
		{authz.KNTinh, "extension_team", "Nút không đúng cấp của vai trò (ttkn_province)"},
		{authz.KNXa, "ttkn_province", ""},
		{authz.KNXa, "extension_team", "Nút không đúng cấp của vai trò (ttkn_province)"},
		{authz.KNXa, "cooperative", "Nút không đúng cấp của vai trò (ttkn_province)"},
		{authz.KNAdmin, "cooperative", ""},
		{authz.KNAdmin, "mkg", ""},
	}
	for _, c := range cases {
		err := rules.ValidateNodeKind(c.role, c.kind)
		re := ruleErr(t, err)
		if (err == nil) != (c.msg == "") || (err != nil && (re.Field != "org_ref" || re.Msg != c.msg)) {
			t.Errorf("%s/%s: got %v", c.role, c.kind, err)
		}
	}
}

// Danh mục KN khớp danh mục bridge hiện hành (vai, cấp quản lý); đổi một bên phải đổi bên kia có chủ đích.
func TestKNAssignmentRulesCatalogParity(t *testing.T) {
	rules := extension.AssignmentRules()
	for _, role := range authz.KNRoles {
		if err := rules.ValidateAssignment(accport.AssignmentRuleValues{Role: role}); err != nil && ruleErr(t, err).Field == "role" {
			t.Fatalf("legacy role rejected: %s", role)
		}
	}
	if !slices.Equal(authz.KNRoles, accport.KNRolesAll) {
		t.Fatal("legacy role list changed")
	}
	for _, l := range accport.MgmtLevels {
		v := accport.AssignmentRuleValues{Role: authz.KNAdmin, MgmtLevel: l.Code}
		if err := rules.ValidateAssignment(v); err != nil {
			t.Fatalf("legacy level rejected: %s %v", l.Code, err)
		}
	}
}
