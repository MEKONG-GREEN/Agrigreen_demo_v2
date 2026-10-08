package handler_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// GIS FN-06 / GIS US-WH-01 AC-2 / Q-MKG-02: job hub_capacity_daily của api-internal chụp và công
// bố sức chứa MỌI cơ sở đang dùng qua van hub_capacity mỗi ngày — idempotent theo (cơ sở,
// ngày nghiệp vụ): chạy lại cùng ngày không xuất thêm, sang ngày mới xuất lại. Tìm tenant bằng
// vai ag_ops_jobs (chỉ đọc cột), ghi bằng vai hub_app, qua cửa van (valve_export + outbox).
func TestDailyCapacityPublication_GIS_US_WH_01(t *testing.T) {
	f := newFixture(t)
	a := f.approvedFacility("HUB-D1", "500000")
	b := f.approvedFacility("HUB-D2", "250000")
	pub := f.env.Pool(t, migrations.MKG, dbadmin.OpsJobs)
	mod := hub.New(f.pool, f.clk)
	ctx := context.Background()
	all, err := hub.ActiveHubTenants(pub)(ctx)
	if err != nil || !slices.Contains(all, f.tenant) {
		t.Fatalf("tìm tenant có cơ sở đang dùng: %v %v", all, err)
	}
	// Chỉ tenant của kiểm thử (các kiểm thử khác cùng database để lại tenant riêng).
	tenants := func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }
	daily := func() int {
		t.Helper()
		var n int
		f.inTenant(func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM mkg_hub.valve_export v
			  JOIN shared.outbox o ON o.id = v.id AND o.event_type = 'mkg.hub.capacity.published'
			  WHERE v.contract = 'hub_capacity' AND v.idem_key LIKE 'capacity-daily:%'`).Scan(&n)
		})
		return n
	}
	r, err := mod.PublishDailyCapacity(ctx, tenants)
	if err != nil || r.Published != 2 || r.Skipped != 0 || daily() != 2 {
		t.Fatalf("lượt đầu: %+v %v (%d bản xuất)", r, err, daily())
	}
	r, err = mod.PublishDailyCapacity(ctx, tenants)
	if err != nil || r.Published != 0 || r.Skipped != 2 || daily() != 2 {
		t.Fatalf("chạy lại cùng ngày phải bỏ qua: %+v %v", r, err)
	}
	f.clk.Advance(24 * time.Hour)
	if r, err = mod.PublishDailyCapacity(ctx, tenants); err != nil || r.Published != 2 || daily() != 4 {
		t.Fatalf("ngày mới: %+v %v", r, err)
	}
	// Payload qua van đúng hợp đồng: hub_ref của từng cơ sở.
	var refs []string
	f.inTenant(func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT payload->>'hub_ref' FROM mkg_hub.valve_export WHERE idem_key LIKE 'capacity-daily:%'`)
		if err != nil {
			return err
		}
		refs, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	slices.Sort(refs)
	want := []string{a, b}
	slices.Sort(want)
	if !slices.Equal(refs, want) {
		t.Fatalf("hub_ref trong van: %v", refs)
	}
	// Tenant không có cơ sở: không xuất gì.
	if r, err := mod.PublishDailyCapacity(ctx, func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{ids.New()}, nil }); err != nil || r.Published != 0 {
		t.Fatalf("tenant trống: %+v %v", r, err)
	}
}
