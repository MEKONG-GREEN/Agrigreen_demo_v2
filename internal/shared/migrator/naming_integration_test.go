package migrator_test

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // driver pgx5://
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/migrator"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// upV1 chạy golang-migrate (bộ chạy mặc định) trên fsys cho shared/ rồi thư mục cụm — như dbadmin.MigrateV1 nhưng
// đọc được FS chồng.
func upV1(t *testing.T, fsys fs.FS, dsn string, c migrations.Cluster) {
	t.Helper()
	for _, dir := range []string{migrator.SharedDir, string(c)} {
		src, err := iofs.New(fsys, dir)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		u.Scheme = "pgx5"
		q := u.Query()
		q.Set("x-migrations-table", "schema_migrations_"+dir)
		u.RawQuery = q.Encode()
		m, err := migrate.NewWithSourceInstance("iofs", src, u.String())
		if err != nil {
			t.Fatal(err)
		}
		err = m.Up()
		_, _ = m.Close()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			t.Fatalf("v1 %s: %v", dir, err)
		}
	}
}

func v1Version(t *testing.T, admin string, c migrations.Cluster, sfx string) int64 {
	t.Helper()
	var v int64
	if err := connDB(t, admin, dbadmin.DatabaseName(c, sfx)).QueryRow(context.Background(), "SELECT version FROM schema_migrations_"+string(c)).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func tableExists(t *testing.T, admin string, c migrations.Cluster, sfx, tbl string) bool {
	t.Helper()
	var ok bool
	if err := connDB(t, admin, dbadmin.DatabaseName(c, sfx)).QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", tbl).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

// WH-B: tên tệp mốc UTC (YYYYMMDDHHMMSS_…) chạy đúng với cả golang-migrate (v1, mặc định) và bộ chạy v2 — DB mới
// áp sau mọi số cũ, DB có sẵn chỉ áp tệp mới (sổ cũ giữ nguyên); tệp số cũ thêm SAU một tệp mốc bị golang-migrate bỏ
// qua im lặng (vì vậy migratecheck -base từ chối nó). Cụm vault (nhỏ nhất).
func TestTimestampMigrations_WHB(t *testing.T) {
	admin := adminURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	const c = migrations.Vault
	// Mốc thăm dò phải sau MỌI tệp thật của cụm (vault có tệp mốc thật từ W11.23: 20261013165158) — dùng năm 2099.
	const probeV = 20991231000000
	const probe = "vault/20991231000000_vault_whb_ts_probe.up.sql"
	const late = "vault/000005_vault_whb_late.up.sql"
	withProbe := overlay(t, map[string]string{probe: "CREATE TABLE vault.whb_ts_probe (id int PRIMARY KEY);\n"})
	withLate := overlay(t, map[string]string{
		probe: "CREATE TABLE vault.whb_ts_probe (id int PRIMARY KEY);\n",
		late:  "CREATE TABLE vault.whb_late (id int PRIMARY KEY);\n",
	})
	sfx := map[string]string{}
	for _, k := range []string{"v1new", "v2new", "v1old", "v2old"} {
		s := suffix(t, "whb"+k[:2]+k[2:3])
		sfx[k] = s
		t.Cleanup(func() { _ = dbadmin.DropDatabases(context.Background(), admin, s) })
		// Bootstrap đủ cụm (database rỗng, rẻ): BootstrapClusters chỉ vault trên cụm PostgreSQL mới thiếu vai own_* của
		// cụm khác (GRANT own_acctbridge … hỏng) — giới hạn có sẵn của dbadmin, ngoài phạm vi WH-B.
		if err := dbadmin.Bootstrap(ctx, admin, s); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("fresh_db", func(t *testing.T) {
		upV1(t, withProbe, migratorDSN(t, admin, c, sfx["v1new"]), c)
		if v := v1Version(t, admin, c, sfx["v1new"]); v != probeV || !tableExists(t, admin, c, sfx["v1new"], "vault.whb_ts_probe") {
			t.Fatalf("v1: phiên bản %d", v)
		}
		res, err := migrator.Up(ctx, migratorDSN(t, admin, c, sfx["v2new"]), c, migrator.Options{FS: withProbe})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Applied) == 0 || res.Applied[len(res.Applied)-1] != probe {
			t.Fatalf("v2: tệp mốc phải áp sau cùng: %v", res.Applied)
		}
		var v int64
		var mod string
		if err := connDB(t, admin, dbadmin.DatabaseName(c, sfx["v2new"])).QueryRow(ctx,
			"SELECT version, module FROM shared.schema_migrations WHERE dir = 'vault' AND file = $1", probe[6:]).Scan(&v, &mod); err != nil || v != probeV {
			t.Fatalf("sổ v2: %d %q %v", v, mod, err)
		}
		if v := v1Version(t, admin, c, sfx["v2new"]); v != probeV {
			t.Errorf("v2 phải đồng bộ phiên bản golang-migrate: %d", v)
		}
	})

	t.Run("existing_db", func(t *testing.T) {
		// DB đã áp toàn bộ tệp số cũ (trạng thái thật hôm nay), rồi gộp tệp mốc.
		if err := dbadmin.MigrateV1(admin, c, sfx["v1old"]); err != nil {
			t.Fatal(err)
		}
		before := v1Version(t, admin, c, sfx["v1old"])
		upV1(t, withProbe, migratorDSN(t, admin, c, sfx["v1old"]), c)
		if v := v1Version(t, admin, c, sfx["v1old"]); before >= probeV || v != probeV || !tableExists(t, admin, c, sfx["v1old"], "vault.whb_ts_probe") {
			t.Fatalf("v1: %d → %d", before, v)
		}
		// Chuyển DB đó sang v2: chỉ ghi sổ (import), không chạy lại gì — kể cả tệp mốc.
		res, err := migrator.Up(ctx, migratorDSN(t, admin, c, sfx["v1old"]), c, migrator.Options{FS: withProbe})
		if err != nil || len(res.Applied) != 0 || !slices.Contains(res.Imported, probe) {
			t.Fatalf("v1 → v2: %+v %v", res, err)
		}

		dsn := migratorDSN(t, admin, c, sfx["v2old"])
		if _, err := migrator.Up(ctx, dsn, c, migrator.Options{}); err != nil {
			t.Fatal(err)
		}
		st, err := migrator.Inspect(ctx, dsn, c, migrator.Options{})
		if err != nil {
			t.Fatal(err)
		}
		rows := len(st.Rows)
		res, err = migrator.Up(ctx, dsn, c, migrator.Options{FS: withProbe})
		if err != nil || !slices.Equal(res.Applied, []string{probe}) || len(res.Imported) != 0 {
			t.Fatalf("v2 DB có sẵn: %+v %v", res, err)
		}
		st2, err := migrator.Inspect(ctx, dsn, c, migrator.Options{FS: withProbe})
		if err != nil || len(st2.Rows) != rows+1 || st2.Recon.Blocking() || len(st2.Recon.Apply) != 0 {
			t.Fatalf("sổ sau: %d dòng (trước %d) %+v %v", len(st2.Rows), rows, st2.Recon, err)
		}
	})

	t.Run("legacy_number_after_timestamp_is_skipped_by_v1", func(t *testing.T) {
		// Đây là lỗi migratecheck -base chặn: số 000005 xếp trước mốc đã áp → golang-migrate không áp, không báo.
		upV1(t, withLate, migratorDSN(t, admin, c, sfx["v1new"]), c)
		if tableExists(t, admin, c, sfx["v1new"], "vault.whb_late") {
			t.Fatal("golang-migrate được cho là bỏ qua tệp số nhỏ hơn — hành vi đã đổi?")
		}
		st, err := migrator.Inspect(ctx, migratorDSN(t, admin, c, sfx["v1new"]), c, migrator.Options{FS: withLate})
		if err != nil {
			t.Fatal(err)
		}
		// Tệ hơn: khi chuyển DB này sang v2, sổ còn trống nên quy tắc chuyển trạng thái (số ≤ phiên bản golang-migrate
		// → đã áp) ghi sổ "import" tệp CHƯA từng chạy. Không bộ chạy nào sửa được sau khi gộp — phải chặn trước gộp.
		var imported bool
		for _, f := range st.Recon.Import {
			imported = imported || f.Key() == late
		}
		if !imported {
			t.Errorf("tệp bị v1 bỏ qua được cho là bị v2 ghi sổ import khi chuyển: import %d tệp, apply %d", len(st.Recon.Import), len(st.Recon.Apply))
		}
	})
}
