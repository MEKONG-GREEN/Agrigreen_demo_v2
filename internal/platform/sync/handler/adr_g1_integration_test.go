package handler_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	syncmod "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ADR-0006 (mục 19, C-8): gauge ag.sync.conflict.overdue đo được cho MỌI tenant ở worker —
// tìm tenant có xung đột đang mở bằng vai ag_outbox_publisher (hai cột, migration pf 000043),
// đếm quá hạn bằng vai sync_app qua RegisterConflictSLAMetrics.
func TestConflictTenantsForSLAGauge_ADR0006(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	c.register()
	results(t, c.post("/v1/sync/push", map[string]any{"device_id": c.device.String(), "records": []any{
		rec(c, 1, "plot_boundary", map[string]any{"behave": "conflict", "conflict_key": "adr6"}),
	}}))
	pub := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.OutboxPublisher)
	ts, err := syncmod.ConflictTenants(pub)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ts, f.tenant) {
		t.Fatalf("tenant có xung đột mở phải được liệt kê: %v", ts)
	}
	if _, err := syncmod.RegisterConflictSLAMetrics(f.pool, f.clk, syncmod.ConflictTenants(pub)); err != nil {
		t.Fatal(err)
	}
	f.clk.Advance(73 * time.Hour)
	if sla, err := f.mod.ConflictSLA(context.Background(), f.tenant); err != nil || sla.Overdue != 1 {
		t.Fatalf("quá 72 giờ phải đếm quá hạn: %+v %v", sla, err)
	}
}

// ADR-0006: con trỏ pull ký HMAC — production bắt buộc AG_SYNC_CURSOR_KEY; khóa ngắn bị từ chối.
func TestCursorKeyRequiredInProduction_ADR0006(t *testing.T) {
	if err := syncmod.CheckCursorKey(nil, true); !errors.Is(err, syncmod.ErrCursorKey) {
		t.Fatalf("production thiếu khóa: %v", err)
	}
	if err := syncmod.CheckCursorKey([]byte("ngắn"), false); !errors.Is(err, syncmod.ErrCursorKey) {
		t.Fatalf("khóa < 32 byte: %v", err)
	}
	if err := syncmod.CheckCursorKey(nil, false); err != nil {
		t.Fatalf("Local/Dev được sinh khóa ngẫu nhiên: %v", err)
	}
	if err := syncmod.CheckCursorKey([]byte("khoa-con-tro-kiem-thu-0123456789abcdef"), true); err != nil {
		t.Fatal(err)
	}
}

// ADR-0010: Zalo Mini App chỉ online — không đăng ký được làm thiết bị đồng bộ ngoại tuyến
// (chỉ android/ios của app Flutter), nên không pull/push/tải tệp được.
func TestOnlyFlutterPlatformsRegisterForOfflineSync_ADR0010(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	for _, p := range []string{"zalo", "zalo_mini_app", "web"} {
		r := c.post("/v1/devices", map[string]any{"device_id": ids.New().String(), "platform": p, "app_version": "1.0.0"})
		if r.status != http.StatusUnprocessableEntity {
			t.Errorf("nền tảng %q phải bị từ chối: %d %s", p, r.status, r.raw)
		}
	}
	for _, p := range []string{"android", "ios"} {
		if r := c.post("/v1/devices", map[string]any{"device_id": ids.New().String(), "platform": p, "app_version": "1.0.0"}); r.status != http.StatusCreated {
			t.Errorf("app Flutter %s: %d %s", p, r.status, r.raw)
		}
	}
}
