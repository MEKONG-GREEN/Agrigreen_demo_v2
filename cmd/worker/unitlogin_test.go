package main

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
)

// Gói FC (ADR-0028, design/cooperative.md §17.10): mọi consumer của worker chạy giao dịch inbox dưới
// SET LOCAL ROLE <vai module> trên pool app_worker — vai đó phải là membership của app_worker trong danh mục bootstrap
// (modgen chỉ thấy import trong cmd/worker, consumer liên module của assembly.Consumers cần LoginUnit.Extra).
func TestConsumerModulesInUnitLogin(t *testing.T) {
	var unit *dbadmin.LoginUnit
	for i := range dbadmin.LoginUnits {
		if dbadmin.LoginUnits[i].Unit == "worker" {
			unit = &dbadmin.LoginUnits[i]
		}
	}
	if unit == nil || unit.Role != dbadmin.AppWorker {
		t.Fatalf("worker không có vai đăng nhập đơn vị app_worker: %+v", unit)
	}
	var member []string
	for _, r := range dbadmin.Roles {
		if r.Name == unit.Role {
			member = r.MemberOf
		}
	}
	for _, c := range allConsumers(slog.New(slog.DiscardHandler)) {
		if !c.module.Valid() {
			t.Errorf("consumer %s thiếu module", c.name)
			continue
		}
		if !slices.Contains(member, c.module.Role()) {
			t.Errorf("consumer %s chạy dưới %s nhưng %s không là thành viên (thêm %s vào LoginUnit.Extra)", c.name, c.module.Role(),
				unit.Role, c.module.Name())
		}
	}
	// -agg-run: job chưa khai / thiếu người chạy bị từ chối trước khi mở pool.
	if err := runAggJob(slog.New(slog.DiscardHandler), "khong.co", "", 0); err == nil {
		t.Error("-agg-run job chưa khai phải lỗi")
	}
	// Job tổng hợp chạy trên vai đăng nhập riêng (ADR-0031 §3) — app_worker không thuộc r_agg.
	if slices.Contains(member, dbadmin.AggRole) {
		t.Errorf("app_worker không được là thành viên %s: %v", dbadmin.AggRole, member)
	}
}
