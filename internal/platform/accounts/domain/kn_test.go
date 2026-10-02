package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// FN-AD-04.04 / UR-AD-04: ràng buộc gán vai trò không phụ thuộc dữ liệu.
func TestFN_AD_04_04_NormalizeAssignment(t *testing.T) {
	now := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	org := uuid.New()
	ok, err := domain.NormalizeAssignment(domain.AssignmentInput{Role: port.KNXa, OrgRef: org, Departments: []string{"trong_trot", " trong_trot ", "bvtv_x"},
		Communes: []string{"30301", "30292", "30301"}, MgmtLevel: "chuyen_vien"}, now)
	if err != nil || len(ok.Departments) != 2 || ok.Communes[0] != "30292" || len(ok.Communes) != 2 || !ok.ValidFrom.Equal(now) {
		t.Fatalf("chuẩn hóa: %+v %v", ok, err)
	}
	past := now.Add(-time.Hour)
	for name, in := range map[string]domain.AssignmentInput{
		"vai lạ":             {Role: "kn_officer", OrgRef: org, Departments: []string{"trong_trot"}},
		"Admin có phòng ban": {Role: port.KNAdmin, Departments: []string{"trong_trot"}},
		"thiếu Nút":          {Role: port.KNTinh, Departments: []string{"trong_trot"}},
		"thiếu phòng ban":    {Role: port.KNTinh, OrgRef: org},
		"Tỉnh có xã":         {Role: port.KNTinh, OrgRef: org, Departments: []string{"trong_trot"}, Communes: []string{"30292"}},
		"mã xã lạ":           {Role: port.KNXa, OrgRef: org, Departments: []string{"trong_trot"}, Communes: []string{"xã A"}},
		"cấp quản lý lạ":     {Role: port.KNTinh, OrgRef: org, Departments: []string{"trong_trot"}, MgmtLevel: "sep"},
		"hiệu lực ngược":     {Role: port.KNTinh, OrgRef: org, Departments: []string{"trong_trot"}, ValidTo: &past},
	} {
		var fe domain.FieldError
		if _, err := domain.NormalizeAssignment(in, now); !errors.As(err, &fe) {
			t.Errorf("%s: muốn FieldError, có %v", name, err)
		}
	}
	if _, err := domain.NormalizeAssignment(domain.AssignmentInput{Role: port.KNAdmin}, now); err != nil {
		t.Fatalf("Admin không Nút, không phòng ban: %v", err)
	}
	if domain.KNRoleFitsOrg(port.KNXa, "ttkn_province") != nil || domain.KNRoleFitsOrg(port.KNTW, "ttkn_province") == nil ||
		domain.KNRoleFitsOrg(port.KNXa, "extension_team") == nil {
		t.Fatal("vai ↔ loại Nút")
	}
	if domain.DefaultRoute(port.KNXa) != "/kn/nhiem-vu/cong-viec" || domain.DefaultRoute(port.KNAdmin) != "/kn/tong-quan" {
		t.Fatal("trang mặc định theo vai trò (FN-CH-00.01)")
	}
}

func TestFN_AD_04_04_DiffOldNew(t *testing.T) {
	ch := domain.Diff([]byte(`{"a":"x","b":"1","c":null,"d":true}`), []byte(`{"a":"y","b":"1","c":"z","d":false}`))
	if len(ch) != 3 || ch[0].Field != "a" || *ch[0].Old != "x" || *ch[0].New != "y" || ch[1].Old != nil || *ch[2].New != "false" {
		t.Fatalf("diff: %+v", ch)
	}
	if got := domain.Diff(nil, []byte(`{"a":"x"}`)); len(got) != 1 || got[0].Old != nil {
		t.Fatalf("tạo mới: %+v", got)
	}
}
