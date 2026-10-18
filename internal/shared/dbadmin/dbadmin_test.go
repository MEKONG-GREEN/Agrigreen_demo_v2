package dbadmin

import (
	"context"
	"fmt"
	"os"
	"reflect"
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
	for _, want := range []string{SharedUser, MKGMonitorOwner, Migrator, OutboxPublisher, OpsJobs, "platform_api", "party_app"} {
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

// Cold bootstrap phải giữ đủ vai cha được quản lý mà không tạo vai của cụm khác.
func TestRolesForMembershipClosure(t *testing.T) {
	managed := roleNames(Roles)
	for _, cluster := range migrations.Clusters {
		t.Run(string(cluster), func(t *testing.T) {
			selected := RolesFor([]migrations.Cluster{cluster})
			names := roleNames(selected)
			for _, r := range selected {
				for _, parent := range r.MemberOf {
					if slices.Contains(managed, parent) && !slices.Contains(names, parent) {
						t.Errorf("%s tham gia vai %s không được dựng", r.Name, parent)
					}
				}
			}
		})
	}
	if got := RolesFor(migrations.Clusters); !reflect.DeepEqual(got, Roles) {
		t.Fatal("dựng mọi cụm phải giữ nguyên toàn bộ vai và membership")
	}
}

func TestRolesForMemberOfIsCloned(t *testing.T) {
	migrator := slices.IndexFunc(Roles, func(r Role) bool { return r.Name == Migrator })
	before := slices.Clone(Roles[migrator].MemberOf)
	selected := RolesFor(migrations.Clusters)
	selected[migrator].MemberOf[0] = "changed"
	if !slices.Equal(Roles[migrator].MemberOf, before) {
		t.Fatal("sửa kết quả làm thay đổi Roles toàn cục")
	}
}

func TestRolesForPreservesExternalParents(t *testing.T) {
	roles := []Role{
		{Name: "pf_owner", Clusters: []migrations.Cluster{migrations.PF}},
		{Name: "ts_owner", Clusters: []migrations.Cluster{migrations.TS}},
		{Name: "migrator", Clusters: migrations.Clusters,
			MemberOf: []string{"pf_owner", "pg_read_all_stats", "ts_owner"}},
	}
	got := rolesFor(roles, []migrations.Cluster{migrations.TS})
	if want := []string{"ts_owner", "migrator"}; !slices.Equal(roleNames(got), want) {
		t.Fatalf("vai được chọn = %v, muốn %v", roleNames(got), want)
	}
	if want := []string{"pg_read_all_stats", "ts_owner"}; !slices.Equal(got[1].MemberOf, want) {
		t.Fatalf("membership = %v, muốn %v", got[1].MemberOf, want)
	}
	if want := []string{"pf_owner", "pg_read_all_stats", "ts_owner"}; !slices.Equal(roles[2].MemberOf, want) {
		t.Fatal("lọc membership làm thay đổi đầu vào")
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

// W7.1 (R-DB-12, ADR-0038): mỗi module có schema có vai chủ own_<module> NOLOGIN, tạo trước ag_migrator (thành viên).
func TestOwnerRoles_W71(t *testing.T) {
	names := roleNames(Roles)
	i := slices.Index(names, Migrator)
	if i < 0 {
		t.Fatal("thiếu ag_migrator")
	}
	mig := Roles[i]
	for _, g := range ModuleGroups() {
		o := OwnerRole(g.Module)
		j := slices.Index(names, o)
		if j < 0 {
			t.Errorf("thiếu vai %s", o)
			continue
		}
		if r := Roles[j]; r.Login || r.BypassRLS || len(r.MemberOf) != 0 {
			t.Errorf("%s phải NOLOGIN, không BYPASSRLS, không thuộc vai nào: %+v", o, r)
		}
		if j > i || !slices.Contains(mig.MemberOf, o) {
			t.Errorf("ag_migrator phải là thành viên %s (tạo trước)", o)
		}
	}
	if OwnerRole("pf/party") != "own_party" || OwnerRole("mkg/measurement") != "own_measurement" {
		t.Error("tên vai chủ")
	}
}
