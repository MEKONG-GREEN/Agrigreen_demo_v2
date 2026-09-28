package app

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
)

// KN US-DIR-02: số điện thoại cho "gọi/nhắn 1 chạm" tra từ Keycloak lúc bấm — chỉ tài
// khoản đang bật, đúng tenant, đã khai số.
func TestStaffContactsPhoneOf(t *testing.T) {
	kc := keycloak.NewMemory()
	tid := uuid.New()
	ok := kc.Seed(port.NewUser{Username: "cb1", Phone: "0912345678", TenantID: tid}, true)
	noPhone := kc.Seed(port.NewUser{Username: "cb2", TenantID: tid}, true)
	locked := kc.Seed(port.NewUser{Username: "cb3", Phone: "0912000003", TenantID: tid}, false)
	other := kc.Seed(port.NewUser{Username: "cb4", Phone: "0912000004", TenantID: uuid.New()}, true)
	sc := StaffContacts{Dir: kc}
	ctx := context.Background()
	if p, err := sc.PhoneOf(ctx, tid, ok); err != nil || p != "0912345678" {
		t.Fatalf("có số: %q %v", p, err)
	}
	for name, id := range map[string]uuid.UUID{"chưa khai số": noPhone, "đã khóa": locked, "tenant khác": other, "không có": uuid.New()} {
		if _, err := sc.PhoneOf(ctx, tid, id); !errors.Is(err, extport.ErrNoPhone) {
			t.Errorf("%s: %v", name, err)
		}
	}
	kc.Fail("GetUser", port.ErrUnavailable)
	if _, err := sc.PhoneOf(ctx, tid, ok); !errors.Is(err, port.ErrUnavailable) {
		t.Fatalf("lỗi Keycloak phải trả về: %v", err)
	}
	if _, err := (StaffContacts{}).PhoneOf(ctx, tid, ok); !errors.Is(err, ErrNoDirectory) {
		t.Fatalf("chưa cấu hình Keycloak: %v", err)
	}
}
