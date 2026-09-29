package app

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// HTX US-DASH-03: người nhận lịch báo cáo của quản lý HTX chỉ là tài khoản đã đăng ký của
// HTX (đang bật, email đã xác minh, đúng tenant); email trả về đã che.
func TestOrgRecipients_HTX_US_DASH_03(t *testing.T) {
	kc := keycloak.NewMemory()
	tid, coop, other := uuid.New(), uuid.New(), uuid.New()
	ok := kc.Seed(port.NewUser{Username: "kt", FirstName: "Toán", LastName: "Kế", Email: "ke.toan@htx.test", TenantID: tid, OrgRef: coop}, true)
	kc.SetEmailVerified(ok, true)
	unverified := kc.Seed(port.NewUser{Username: "a", Email: "a@htx.test", TenantID: tid, OrgRef: coop}, true)
	_ = unverified
	locked := kc.Seed(port.NewUser{Username: "b", Email: "b@htx.test", TenantID: tid, OrgRef: coop}, false)
	kc.SetEmailVerified(locked, true)
	outside := kc.Seed(port.NewUser{Username: "c", Email: "c@htx.test", TenantID: tid, OrgRef: other}, true)
	kc.SetEmailVerified(outside, true)
	list, err := MailDirectory{Dir: kc}.OrgRecipients(context.Background(), tid, coop)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ActorRef != ok || list[0].DisplayName != "Kế Toán" || list[0].MaskedEmail != "ke•••@htx.test" {
		t.Fatalf("người nhận: %+v", list)
	}
	if _, err := (MailDirectory{}).OrgRecipients(context.Background(), tid, coop); err == nil {
		t.Fatal("chưa nối Keycloak phải báo lỗi")
	}
	for in, want := range map[string]string{"ab@x.vn": "a•••@x.vn", "noat": "•••", "long.name@htx.vn": "lo•••@htx.vn"} {
		if got := MaskEmail(in); got != want {
			t.Errorf("MaskEmail(%q) = %q, muốn %q", in, got, want)
		}
	}
}
