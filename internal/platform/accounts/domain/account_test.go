package domain

import (
	"errors"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

func TestCheckReason(t *testing.T) {
	if CheckReason("   ngắn quá   ") == nil {
		t.Fatal("lý do dưới 20 ký tự phải bị chặn")
	}
	// 20 ký tự có dấu (đếm theo rune, không theo byte).
	if err := CheckReason("Chuyển công tác tỉnh"); err != nil {
		t.Fatal(err)
	}
}

func TestProfileNormalize(t *testing.T) {
	p, err := Profile{Email: " An.Nguyen@Vi-Du.VN ", FirstName: "An", Phone: "+84 912.345.678"}.Normalize()
	if err != nil || p.Email != "an.nguyen@vi-du.vn" || p.Username != p.Email || p.Phone != "0912345678" {
		t.Fatalf("chuẩn hóa: %v", err)
	}
	cases := map[string]Profile{
		MsgEmailMissing: {FirstName: "An"},
		MsgEmailFormat:  {Email: "khong-phai-email", FirstName: "An"},
		MsgNameMissing:  {Email: "a@vi-du.vn"},
		MsgPhoneFormat:  {Email: "a@vi-du.vn", FirstName: "An", Phone: "12345"},
		MsgUsernameBad:  {Email: "a@vi-du.vn", FirstName: "An", Username: "Có Dấu"},
	}
	for msg, in := range cases {
		var fe FieldError
		if _, err := in.Normalize(); !errors.As(err, &fe) || fe.Msg != msg {
			t.Errorf("%s: nhận %v", msg, err)
		}
	}
}

func TestSplitFullName(t *testing.T) {
	if f, l := SplitFullName(" Nguyễn  Văn   An "); f != "An" || l != "Nguyễn Văn" {
		t.Fatalf("%q %q", f, l)
	}
}

func TestRolesAndActions(t *testing.T) {
	r, err := NormalizeRoles([]string{"kn_officer", " kn_officer ", "coop_manager"})
	if err != nil || !slices.Equal(r, []string{"coop_manager", "kn_officer"}) {
		t.Fatalf("%v %v", r, err)
	}
	if _, err := NormalizeRoles([]string{"platform_admin"}); err == nil {
		t.Fatal("platform_admin không do module quản lý")
	}
	if _, err := NormalizeRoles(nil); err == nil {
		t.Fatal("thiếu vai")
	}
	if a := RequiredActions([]string{"kn_officer"}); slices.Contains(a, port.ActionConfigureTOTP) {
		t.Fatalf("vai thường không bắt TOTP: %v", a)
	}
	if a := RequiredActions([]string{"registry_admin"}); !slices.Equal(a, []string{port.ActionVerifyEmail, port.ActionUpdatePassword, port.ActionConfigureTOTP}) {
		t.Fatalf("vai [MFA] phải bắt TOTP: %v", a)
	}
	var fe FieldError
	if err := RolesFitOrg([]string{"coop_manager"}, "extension_team"); !errors.As(err, &fe) || fe.Msg != MsgCoopRequired {
		t.Fatalf("coop_manager cần HTX: %v", err)
	}
	if err := RolesFitOrg([]string{"coop_manager", "plot_editor"}, "cooperative"); err != nil {
		t.Fatal(err)
	}
}
