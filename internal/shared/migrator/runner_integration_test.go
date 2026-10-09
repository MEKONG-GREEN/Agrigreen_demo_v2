package migrator_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/migrator"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func adminURL(t *testing.T) string {
	t.Helper()
	admin := os.Getenv("AG_TEST_PG_ADMIN_URL")
	if admin == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("CI bắt buộc AG_TEST_PG_ADMIN_URL")
		}
		t.Skip("chưa đặt AG_TEST_PG_ADMIN_URL — bỏ qua kiểm thử tích hợp")
	}
	return admin
}

func suffix(t *testing.T, p string) string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return p + hex.EncodeToString(b)
}

func connDB(t *testing.T, admin, db string) *pgx.Conn {
	t.Helper()
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + db
	conn, err := pgx.Connect(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func migratorDSN(t *testing.T, admin string, c migrations.Cluster, sfx string) string {
	t.Helper()
	dsn, err := dbadmin.RoleURL(admin, c, sfx, dbadmin.Migrator)
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}

// overlay chép migrations.FS và thêm/sửa tệp.
func overlay(t *testing.T, extra map[string]string) fs.FS {
	t.Helper()
	m := fstest.MapFS{}
	err := fs.WalkDir(migrations.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(migrations.FS, p)
		m[p] = &fstest.MapFile{Data: b}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for p, s := range extra {
		m[p] = &fstest.MapFile{Data: []byte(s)}
	}
	return m
}

// freeNumber trả số nhỏ nhất chưa dùng trong thư mục (giữa các số đã có — "ngoài thứ tự").
func freeNumber(t *testing.T, dir string) int {
	t.Helper()
	names, _ := fs.Glob(migrations.FS, dir+"/*.sql")
	used := map[int]bool{}
	for _, n := range names {
		var v int
		if _, err := fmt.Sscanf(strings.TrimPrefix(n, dir+"/"), "%06d_", &v); err == nil {
			used[v] = true
		}
	}
	for v := 2; v < 999999; v++ {
		if !used[v] {
			return v
		}
	}
	t.Fatal("hết số")
	return 0
}

// W7.1: database mới chạy bằng v1 (golang-migrate) và v2 (sổ theo module) cho lược đồ giống hệt; DB của v1
// chuyển sang v2 không chạy lại tệp nào; tệp số nhỏ hơn số đã áp (ngoài thứ tự) được v2 áp; tệp đã áp bị sửa
// thì dừng.
func TestRunnerV1V2_W71(t *testing.T) {
	admin := adminURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	s1, s2 := suffix(t, "w71a"), suffix(t, "w71b")
	for _, s := range []string{s1, s2} {
		t.Cleanup(func() { _ = dbadmin.DropDatabases(context.Background(), admin, s) })
		if err := dbadmin.Bootstrap(ctx, admin, s); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range migrations.Clusters {
		if err := dbadmin.MigrateV1(admin, c, s1); err != nil {
			t.Fatalf("v1 %s: %v", c, err)
		}
		if err := dbadmin.MigrateV2(ctx, admin, c, s2, nil); err != nil {
			t.Fatalf("v2 %s: %v", c, err)
		}
	}
	mods := migrator.CatalogModules()

	t.Run("fresh_identical", func(t *testing.T) {
		for _, c := range migrations.Clusters {
			a, err := migrator.Fingerprint(ctx, connDB(t, admin, dbadmin.DatabaseName(c, s1)))
			if err != nil {
				t.Fatal(err)
			}
			b, err := migrator.Fingerprint(ctx, connDB(t, admin, dbadmin.DatabaseName(c, s2)))
			if err != nil {
				t.Fatal(err)
			}
			if d := diff(a, b); len(d) > 0 {
				t.Errorf("%s: lược đồ v1 ≠ v2 (%d dòng):\n%s", c, len(d), strings.Join(d[:min(len(d), 40)], "\n"))
			}
			if len(a) < 50 {
				t.Errorf("%s: dấu vân tay quá ít dòng (%d)", c, len(a))
			}
		}
	})

	t.Run("v2_ledgers", func(t *testing.T) {
		for _, c := range migrations.Clusters {
			st, err := migrator.Inspect(ctx, migratorDSN(t, admin, c, s2), c, migrator.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if len(st.Rows) != len(st.Plan.Files) || len(st.Recon.Apply) != 0 || len(st.Recon.Import) != 0 || st.Recon.Blocking() {
				t.Errorf("%s: sổ %d dòng, kế hoạch %d, recon %+v", c, len(st.Rows), len(st.Plan.Files), st.Recon)
			}
			for _, r := range st.Rows {
				if r.Method != "apply" {
					t.Errorf("%s: %s method %s", c, r.Key(), r.Method)
				}
				if r.Schema != "shared" {
					t.Errorf("%s: %s ở sổ %s, muốn shared (bước 1)", c, r.Key(), r.Schema)
				}
			}
			// Phiên bản golang-migrate đồng bộ = số lớn nhất mỗi thư mục (quay về v1 được).
			for d, v := range st.V1 {
				var want int64
				for _, f := range st.Plan.Files {
					if f.Dir == d {
						want = max(want, f.Version)
					}
				}
				if !v.Exists || v.Version != want || v.Dirty {
					t.Errorf("%s: golang-migrate %s = %+v, muốn %d", c, d, v, want)
				}
			}
			// Cổng catalog (migrate verify): bảng sổ không làm bẩn v_missing_rls / v_missing_immutability.
			conn := connDB(t, admin, dbadmin.DatabaseName(c, s2))
			for _, view := range []string{"shared.v_missing_immutability", "shared.v_missing_rls"} {
				var n int
				if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+view+" WHERE table_name = 'schema_migrations'").Scan(&n); err != nil {
					t.Fatal(err)
				}
				if n != 0 {
					t.Errorf("%s: %s có bảng sổ", c, view)
				}
			}
		}
		// Lịch sử theo module: cột module; sổ chỉ ghi thêm.
		conn := connDB(t, admin, dbadmin.DatabaseName(migrations.PF, s2))
		var n int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM shared.schema_migrations WHERE module = 'pf/party'").Scan(&n); err != nil || n == 0 {
			t.Errorf("sổ pf/party: %d %v", n, err)
		}
		if _, err := conn.Exec(ctx, "UPDATE shared.schema_migrations SET checksum = checksum"); err == nil {
			t.Error("sổ phải chặn UPDATE")
		}
	})

	t.Run("upgrade_v1_to_v2_no_rerun", func(t *testing.T) {
		for _, c := range migrations.Clusters {
			dsn := migratorDSN(t, admin, c, s1)
			st, err := migrator.Inspect(ctx, dsn, c, migrator.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if len(st.Rows) != 0 || len(st.Recon.Import) != len(st.Plan.Files) || len(st.Recon.Apply) != 0 {
				t.Fatalf("%s trước chuyển: sổ %d, import %d, apply %d", c, len(st.Rows), len(st.Recon.Import), len(st.Recon.Apply))
			}
			res, err := migrator.Up(ctx, dsn, c, migrator.Options{Modules: mods})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Applied) != 0 || len(res.Imported) != len(st.Plan.Files) {
				t.Errorf("%s: chuyển phải chỉ ghi sổ: %d áp, %d ghi sổ / %d", c, len(res.Applied), len(res.Imported), len(st.Plan.Files))
			}
			res, err = migrator.Up(ctx, dsn, c, migrator.Options{})
			if err != nil || len(res.Applied)+len(res.Imported) != 0 {
				t.Errorf("%s: lần hai phải không làm gì: %+v %v", c, res, err)
			}
			// Quay về v1 trong kỳ chuyển: không chạy lại.
			if err := dbadmin.MigrateV1(admin, c, s1); err != nil {
				t.Errorf("%s: v1 sau v2: %v", c, err)
			}
		}
		// Sau chuyển, lược đồ vẫn như DB v2 mới (sổ bỏ qua khi so).
		a, _ := migrator.Fingerprint(ctx, connDB(t, admin, dbadmin.DatabaseName(migrations.PF, s1)))
		b, _ := migrator.Fingerprint(ctx, connDB(t, admin, dbadmin.DatabaseName(migrations.PF, s2)))
		if d := diff(a, b); len(d) > 0 {
			t.Errorf("pf sau chuyển: %v", d[:min(len(d), 20)])
		}
	})

	t.Run("out_of_order_applied", func(t *testing.T) {
		n := freeNumber(t, "pf")
		key := fmt.Sprintf("pf/%06d_pf_notify_w71_out_of_order.up.sql", n)
		fsys := overlay(t, map[string]string{key: "CREATE TABLE pf_notify.w71_out_of_order (id int PRIMARY KEY);\n"})
		dsn := migratorDSN(t, admin, migrations.PF, s1)
		st, err := migrator.Inspect(ctx, dsn, migrations.PF, migrator.Options{FS: fsys})
		if err != nil {
			t.Fatal(err)
		}
		if got := migrator.SkippedByV1(st.Recon.Apply, st.V1); len(got) != 1 || got[0].Key() != key {
			t.Fatalf("verify-runner phải báo tệp golang-migrate bỏ qua: %v", got)
		}
		res, err := migrator.Up(ctx, dsn, migrations.PF, migrator.Options{FS: fsys})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(res.Applied, []string{key}) {
			t.Fatalf("áp %v, muốn %s", res.Applied, key)
		}
		conn := connDB(t, admin, dbadmin.DatabaseName(migrations.PF, s1))
		var mod string
		if err := conn.QueryRow(ctx, "SELECT module FROM shared.schema_migrations WHERE file = $1", key[3:]).Scan(&mod); err != nil || mod != "pf/notify" {
			t.Errorf("sổ: module %q, muốn pf/notify %v", mod, err)
		}
		var v int64
		if err := conn.QueryRow(ctx, "SELECT version FROM schema_migrations_pf").Scan(&v); err != nil || v <= int64(n) {
			t.Errorf("phiên bản golang-migrate không được lùi: %d %v", v, err)
		}
		if res, err := migrator.Up(ctx, dsn, migrations.PF, migrator.Options{FS: fsys}); err != nil || len(res.Applied) != 0 {
			t.Errorf("lần hai: %+v %v", res, err)
		}
	})

	t.Run("checksum_drift_blocks", func(t *testing.T) {
		const p = "vault/000001_vault_schema.up.sql"
		b, err := fs.ReadFile(migrations.FS, p)
		if err != nil {
			t.Fatal(err)
		}
		fsys := overlay(t, map[string]string{p: string(b) + "\n-- sửa sau khi áp\n"})
		_, err = migrator.Up(ctx, migratorDSN(t, admin, migrations.Vault, s2), migrations.Vault, migrator.Options{FS: fsys})
		if err == nil || !strings.Contains(err.Error(), p) {
			t.Errorf("tệp đã áp bị sửa phải dừng: %v", err)
		}
	})
}

func diff(a, b []string) []string {
	ma := map[string]int{}
	for _, x := range a {
		ma[x]++
	}
	for _, x := range b {
		ma[x]--
	}
	var out []string
	for x, n := range ma {
		switch {
		case n > 0:
			out = append(out, "- v1 "+x)
		case n < 0:
			out = append(out, "+ v2 "+x)
		}
	}
	slices.Sort(out)
	return out
}
