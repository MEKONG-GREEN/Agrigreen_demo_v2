package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// ADR-0010 + mục 11: danh sách vai bắt buộc MFA của backend (accounts/port.MFARoles — tạo
// tài khoản gắn CONFIGURE_TOTP, kích hoạt bằng liên kết một lần, không mật khẩu tạm) phải
// khớp đúng các vai đánh dấu "[MFA]" trong realm; vai hiện trường của nông dân / HTX
// (đăng nhập ngoại tuyến + PIN trên app Flutter) không bị Admin API đòi amr MFA. Lưu ý D24
// (13/10/2026): cán bộ KN (kể cả kn_officer) phải nhập TOTP lúc ĐĂNG NHẬP (realm, port.LoginTOTPRoles —
// TestRealmLoginTOTPByRole_D24); MFARoles ở đây chỉ là vai Admin API kiểm amr ở backend.
func TestMFARolesMatchRealm_ADR0010(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "compose", "keycloak", "agrigreen-realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	var realm realmFile
	if err := json.Unmarshal(raw, &realm); err != nil {
		t.Fatal(err)
	}
	var marked []string
	for _, r := range realm.Roles.Realm {
		if strings.HasPrefix(r.Description, "[MFA]") {
			marked = append(marked, r.Name)
		}
	}
	for _, r := range marked {
		if !slices.Contains(accport.MFARoles, r) {
			t.Errorf("vai %s đánh dấu [MFA] trong realm nhưng thiếu ở accounts/port.MFARoles", r)
		}
	}
	for _, r := range accport.MFARoles {
		if !slices.Contains(marked, r) {
			t.Errorf("vai %s ở MFARoles nhưng realm không đánh dấu [MFA]", r)
		}
	}
	for _, role := range []string{"farmer", "coop_manager", "kn_officer"} {
		if accport.NeedsMFA([]string{role}) {
			t.Errorf("vai hiện trường %s không được bắt buộc MFA (ADR-0010: app Flutter, PIN ngoại tuyến)", role)
		}
	}
}
