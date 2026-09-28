package handler_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	syncmod "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// port.Devices (cho pf/notify gắn token đẩy): trả người đăng ký, nền tảng, trạng
// thái thu hồi; không có → ErrDeviceNotFound; cách ly tenant.
func TestDevicesPort(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	c.register()
	ctx := context.Background()
	for _, devs := range []port.Devices{f.mod.Devices(), syncmod.NewDevices(f.pool)} {
		d, err := devs.Device(ctx, f.tenant, c.device)
		if err != nil || d.ActorRef != c.actor || d.Platform != "android" || d.Revoked || d.OrgRef == nil || *d.OrgRef != c.org {
			t.Fatalf("thiết bị: %+v %v", d, err)
		}
		if _, err := devs.Device(ctx, ids.New(), c.device); !errors.Is(err, port.ErrDeviceNotFound) {
			t.Fatalf("tenant khác: %v", err)
		}
		if _, err := devs.Device(ctx, f.tenant, ids.New()); !errors.Is(err, port.ErrDeviceNotFound) {
			t.Fatalf("mã lạ: %v", err)
		}
	}
	adm := f.officer(port.RoleDeviceAdmin)
	if r := adm.adminPost("/v1/devices/"+c.device.String()+"/revoke", map[string]any{"reason": "Máy bị mất khi đi thực địa"}); r.status != http.StatusOK {
		t.Fatalf("thu hồi: %d %s", r.status, r.raw)
	}
	if d, _ := f.mod.Devices().Device(ctx, f.tenant, c.device); !d.Revoked {
		t.Fatal("phải thấy đã thu hồi")
	}
}
