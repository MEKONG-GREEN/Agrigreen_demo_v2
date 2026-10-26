package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// CNTS-SCOPE-001: các tỉnh có thể cùng tenant trong thời gian chuyển đổi.
// Cảnh báo chỉ có tỉnh (không gắn xã) vẫn phải chịu phạm vi của phiên.
func TestProvinceAlertScope_W1143(t *testing.T) {
	officer := caller{k: port.SessionContext{Role: port.SessionRoleProvince,
		Scope: authz.Scope{Units: []string{"89", "30001"}}}}
	for _, tc := range []struct {
		name  string
		alert store.Alert
		want  bool
	}{
		{"tỉnh phụ trách", store.Alert{ProvinceCode: "89"}, true},
		{"tỉnh khác", store.Alert{ProvinceCode: "91"}, false},
		{"thiếu địa bàn", store.Alert{}, false},
		{"xã phụ trách", store.Alert{ProvinceCode: "89", CommuneCode: "30001"}, true},
		{"xã ngoài phạm vi", store.Alert{ProvinceCode: "89", CommuneCode: "30002"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := officer.sees(tc.alert); got != tc.want {
				t.Fatalf("quyền xem = %v, muốn %v", got, tc.want)
			}
		})
	}
	empty := caller{k: port.SessionContext{Role: port.SessionRoleProvince}}
	if empty.sees(store.Alert{ProvinceCode: "89"}) {
		t.Fatal("phạm vi rỗng không được thấy cảnh báo tỉnh")
	}
	admin := caller{k: port.SessionContext{Role: port.SessionRoleAdmin}}
	if !admin.sees(store.Alert{ProvinceCode: "91"}) {
		t.Fatal("Admin giữ quyền xem các tỉnh trong tenant")
	}
}
