package app

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// US-DASH-03: email người nhận lịch báo cáo tra từ Keycloak lúc gửi — chỉ tài khoản
// đang bật, email đã xác minh, đúng tenant.
func TestMailDirectoryEmailOf(t *testing.T) {
	kc := keycloak.NewMemory()
	tid := uuid.New()
	ok := kc.Seed(port.NewUser{Username: "cb1", Email: "cb1@ttkn.test", TenantID: tid}, true)
	kc.SetEmailVerified(ok, true)
	unverified := kc.Seed(port.NewUser{Username: "cb2", Email: "cb2@ttkn.test", TenantID: tid}, true)
	locked := kc.Seed(port.NewUser{Username: "cb3", Email: "cb3@ttkn.test", TenantID: tid}, false)
	kc.SetEmailVerified(locked, true)
	other := kc.Seed(port.NewUser{Username: "cb4", Email: "cb4@ttkn.test", TenantID: uuid.New()}, true)
	kc.SetEmailVerified(other, true)
	md := MailDirectory{Dir: kc}
	ctx := context.Background()
	if e, err := md.EmailOf(ctx, tid, ok); err != nil || e != "cb1@ttkn.test" {
		t.Fatalf("email đã xác minh: %v", err)
	}
	for name, id := range map[string]uuid.UUID{"chưa xác minh": unverified, "đã khóa": locked, "tenant khác": other, "không có": uuid.New()} {
		if _, err := md.EmailOf(ctx, tid, id); !errors.Is(err, port.ErrNoEmail) {
			t.Errorf("%s: %v", name, err)
		}
	}
	kc.Fail("GetUser", port.ErrUnavailable)
	if _, err := md.EmailOf(ctx, tid, ok); !errors.Is(err, port.ErrUnavailable) {
		t.Fatalf("lỗi tạm thời phải trả về để giao lại: %v", err)
	}
}
