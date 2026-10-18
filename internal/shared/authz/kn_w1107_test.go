package authz_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// W11.07: quyền nghiệp vụ, cờ nhiệm vụ, ngữ cảnh chỉ xem, phạm vi Cấp Xã theo cơ sở/hộ.
func TestW1107_KNContextPowersAndTargets(t *testing.T) {
	lead := authz.KNContext{Role: authz.KNTinh, Manager: true, Duties: []string{authz.DutyPriceAdmin}}
	if !lead.Can(authz.PowerReopenPeriod) || !lead.HasDuty(authz.DutyPriceAdmin) || lead.HasDuty(authz.DutyFormOwner) {
		t.Fatalf("lãnh đạo: %+v", lead)
	}
	viewer := lead
	viewer.ViewOnly = true
	if !viewer.ReadOnly() || viewer.Can(authz.PowerReopenPeriod) || viewer.HasDuty(authz.DutyPriceAdmin) {
		t.Fatal("ngữ cảnh chỉ xem không có quyền, không có nhiệm vụ")
	}
	staff := authz.KNContext{Role: authz.KNTinh, Powers: []string{authz.PowerReopenPeriod}}
	if !staff.Can(authz.PowerReopenPeriod) || staff.Can(authz.PowerApprove) {
		t.Fatal("cán bộ chỉ có quyền được ủy quyền")
	}
	if (authz.KNContext{Role: authz.KNXa, Manager: true}).Can(authz.PowerReopenPeriod) {
		t.Fatal("Cấp Xã không có quyền lãnh đạo phòng")
	}
	if (authz.KNContext{Role: authz.KNTW}).Can(authz.PowerApprove) {
		t.Fatal("Cấp TW chỉ xem")
	}
	f, h := uuid.New(), uuid.New()
	open := authz.KNContext{Role: authz.KNXa}
	if !open.AllowsFacility(f) || !open.AllowsHousehold(h) {
		t.Fatal("không giới hạn → mọi cơ sở/hộ (vẫn kiểm Scope theo xã)")
	}
	lim := authz.KNContext{Role: authz.KNXa, Facilities: []uuid.UUID{f}}
	if !lim.AllowsFacility(f) || lim.AllowsFacility(uuid.New()) || lim.AllowsHousehold(h) {
		t.Fatal("giới hạn cơ sở → cơ sở/hộ ngoài danh sách bị chặn")
	}
}
