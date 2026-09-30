package dbadmin

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func roleNames(rs []Role) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

// G4 (RB-5, AG_CLUSTERS): chỉ vai của cụm đã chọn + vai nhóm NOLOGIN; mọi cụm = danh mục đầy đủ (hành vi cũ).
func TestRolesForSelectedClusters_G4(t *testing.T) {
	if got := RolesFor(migrations.Clusters); len(got) != len(Roles) {
		t.Fatalf("mọi cụm phải giữ đủ %d vai, có %d", len(Roles), len(got))
	}
	pf := roleNames(RolesFor([]migrations.Cluster{migrations.PF}))
	for _, want := range []string{SharedUser, MKGMonitorOwner, Migrator, OutboxPublisher, "platform_api", "party_app"} {
		if !slices.Contains(pf, want) {
			t.Errorf("cụm pf thiếu vai %s", want)
		}
	}
	for _, not := range []string{"hub_app", "logistics_app", "mkg_api", "worker_internal", MKGMonitor} {
		if slices.Contains(pf, not) {
			t.Errorf("cụm pf không được có vai %s (của cụm mkg)", not)
		}
	}
	mkg := roleNames(RolesFor([]migrations.Cluster{migrations.MKG}))
	if !slices.Contains(mkg, "costing_app") || slices.Contains(mkg, "party_app") {
		t.Errorf("cụm mkg: %v", mkg)
	}
	// Thứ tự giữ nguyên: vai nhóm trước vai thành viên.
	if slices.Index(mkg, SharedUser) > slices.Index(mkg, "costing_app") {
		t.Error("ag_shared_user phải tạo trước vai thành viên")
	}
}

// G4: BootstrapClusters chỉ tạo database của cụm đã chọn (database kiểm thử riêng, dọn khi xong).
func TestBootstrapClustersOnlySelected_G4(t *testing.T) {
	admin := os.Getenv("AG_TEST_PG_ADMIN_URL")
	if admin == "" {
		t.Skip("không có AG_TEST_PG_ADMIN_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	suffix := fmt.Sprintf("bc%d", time.Now().UnixNano()%1_000_000_000)
	if err := BootstrapClusters(ctx, admin, suffix, []migrations.Cluster{migrations.TS}); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	t.Cleanup(func() {
		for _, c := range migrations.Clusters {
			_, _ = conn.Exec(context.Background(), "DROP DATABASE IF EXISTS "+ident(DatabaseName(c, suffix)))
		}
	})
	for _, c := range migrations.Clusters {
		var n int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_database WHERE datname = $1", DatabaseName(c, suffix)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if want := map[bool]int{true: 1, false: 0}[c == migrations.TS]; n != want {
			t.Errorf("database %s: %d (muốn %d)", DatabaseName(c, suffix), n, want)
		}
	}
	if err := BootstrapClusters(ctx, admin, suffix, nil); err == nil {
		t.Error("không có cụm phải lỗi")
	}
}
