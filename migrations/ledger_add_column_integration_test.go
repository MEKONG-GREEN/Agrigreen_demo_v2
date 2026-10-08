package migrations_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // driver pgx5://
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/opsjobs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// shared 000011 (ADR-0012 "Cột thêm vào sổ"): dòng sổ ghi TRƯỚC một migration ADD COLUMN trên sổ
// (mkg 000006 → mkg_fieldops.roll_count; pf 000079 → pf_geo.plot_boundary, pf_geo.plot_event) phải
// vẫn xác minh sạch sau khi nâng cấp lên đầu. Trước shared 000011, verify_chain băm lại cả dòng
// (to_jsonb có thêm khóa cột mới = null) nên báo đứt chuỗi giả ở mọi dòng cũ.

// migrateDir áp migration của một thư mục tới version (0 = tới đầu).
func migrateDir(t *testing.T, dsn, dir string, version uint) {
	t.Helper()
	src, err := iofs.New(migrations.FS, dir)
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
	defer func() { _, _ = m.Close() }()
	if version == 0 {
		err = m.Up()
	} else {
		err = m.Migrate(version)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate %s → %d: %v", dir, version, err)
	}
}

// upgradeEnv dựng database riêng cho một cụm, áp shared tới đầu và migration cụm tới before.
func upgradeEnv(t *testing.T, c migrations.Cluster, before uint) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	env := pgtest.Get(t)
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	suffix := "u" + hex.EncodeToString(b)
	if err := dbadmin.BootstrapClusters(ctx, env.AdminURL, suffix, []migrations.Cluster{c}); err != nil {
		t.Fatal(err)
	}
	dsn, err := dbadmin.RoleURL(env.DirectURL, c, suffix, dbadmin.Migrator)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if err := dbadmin.DropDatabases(context.Background(), env.AdminURL, suffix); err != nil {
			t.Errorf("xóa database nâng cấp %s: %v", suffix, err)
		}
	})
	migrateDir(t, dsn, "shared", 0)
	migrateDir(t, dsn, string(c), before)
	return pool, dsn
}

// verifyAll chạy job xác minh chuỗi trên cả database; mọi chuỗi phải sạch, trả số dòng đã kiểm theo sổ.
func verifyAll(t *testing.T, pool *pgxpool.Pool) map[string]int64 {
	t.Helper()
	checks, err := opsjobs.VerifyAllChains(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]int64{}
	for _, c := range checks {
		if !c.OK {
			at := int64(-1)
			if c.BrokenAtSeq != nil {
				at = *c.BrokenAtSeq
			}
			t.Errorf("chuỗi %s/%s đứt tại seq %d: %s", c.Ledger, c.StreamKey, at, c.Reason)
		}
		rows[c.Ledger] += c.RowsChecked
	}
	return rows
}

func TestLedgerAddColumnUpgradePF(t *testing.T) {
	ctx := context.Background()
	pool, dsn := upgradeEnv(t, migrations.PF, 78) // pf 000079 thêm cột vào plot_boundary, plot_event
	tid, plot := ids.New(), ids.New()
	stream := "plot:" + plot.String()
	schema := "pf_geo" // W3.3 (pf 000132): sau khi nâng lên đầu, bảng ở pf_coop — tên sổ giữ "pf_geo.*"
	insBoundary := func(tx pgx.Tx, extra string, args ...any) error {
		cols, vals := "", ""
		if extra != "" {
			cols, vals = ", shape, declared_area_ha", ", $8, $9"
		}
		base := []any{ids.New(), tid, stream, plot, time.Now().UTC().Add(-time.Hour), ids.New(), ids.New().String()}
		_, err := tx.Exec(ctx, `INSERT INTO `+schema+`.plot_boundary
		    (id, tenant_id, stream_key, plot_ref, geom, area_ha, point_count, source, commune_code, became_current,
		     event_time, actor_ref, idem_key, rule_version`+cols+`)
		  VALUES ($1, $2, $3, $4,
		    ST_Multi(ST_GeomFromText('POLYGON((105 10, 105.001 10, 105.001 10.001, 105 10.001, 105 10))', 4326)),
		    1.2345, 4, 'kn_web', 'xa_kt', true, $5, $6, $7, 'geo/v1'`+vals+`)`, append(base, args...)...)
		return err
	}
	insEvent := func(tx pgx.Tx, lotName *string) error {
		_, err := tx.Exec(ctx, `INSERT INTO `+schema+`.plot_event
		    (id, tenant_id, stream_key, plot_ref, action, code, commune_code, status, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, 'created', 'KT-1', 'xa_kt', 'draft', $5, $6, $7, 'geo/v1')`,
			ids.New(), tid, stream, plot, time.Now().UTC().Add(-time.Hour), ids.New(), ids.New().String())
		if err != nil || lotName == nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO `+schema+`.plot_event
		    (id, tenant_id, stream_key, plot_ref, action, code, commune_code, status, event_time, actor_ref, idem_key,
		     rule_version, lot_name, shape)
		  VALUES ($1, $2, $3, $4, 'attributes_changed', 'KT-1', 'xa_kt', 'draft', $5, $6, $7, 'geo/v2', $8, 'polygon')`,
			ids.New(), tid, stream, plot, time.Now().UTC().Add(-time.Hour), ids.New(), ids.New().String(), *lotName)
		return err
	}

	// Dữ liệu có trước pf 000079.
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
		if err := insBoundary(tx, ""); err != nil {
			return err
		}
		return insEvent(tx, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if got := verifyAll(t, pool); got["pf_geo.plot_boundary"] != 1 || got["pf_geo.plot_event"] != 1 {
		t.Fatalf("trước nâng cấp: số dòng đã kiểm %v", got)
	}

	migrateDir(t, dsn, "pf", 0) // nâng cấp lên đầu: ADD COLUMN trên hai sổ
	schema = "pf_coop"

	// Dòng mới sau nâng cấp, cùng chuỗi: có giá trị ở cột mới và cũng có dòng để cột mới NULL.
	lot := "Lô kiểm thử"
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
		if err := insBoundary(tx, "x", "polygon", "1.2000"); err != nil {
			return err
		}
		if err := insBoundary(tx, ""); err != nil {
			return err
		}
		return insEvent(tx, &lot)
	}); err != nil {
		t.Fatal(err)
	}
	if got := verifyAll(t, pool); got["pf_geo.plot_boundary"] != 3 || got["pf_geo.plot_event"] != 3 {
		t.Fatalf("sau nâng cấp: số dòng đã kiểm %v", got)
	}
}

func TestLedgerAddColumnUpgradeMKG(t *testing.T) {
	ctx := context.Background()
	pool, dsn := upgradeEnv(t, migrations.MKG, 5) // mkg 000006 thêm cột vào roll_count
	tid := ids.New()
	stream := "lot:" + ids.New().String()
	ins := func(tx pgx.Tx, order *uuid.UUID) error {
		cols, vals, args := "", "", []any{ids.New(), tid, stream, ids.New(), ids.New(), time.Now().UTC().Add(-time.Hour), ids.New(), ids.New().String()}
		if order != nil {
			cols, vals, args = ", order_id, stage", ", $9, 'bale'", append(args, *order)
		}
		_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.roll_count
		    (id, tenant_id, stream_key, plot_ref, htx_ref, rolls, event_time, actor_ref, idem_key, rule_version`+cols+`)
		  VALUES ($1, $2, $3, $4, $5, 7, $6, $7, $8, 'roll_count/v1'`+vals+`)`, args...)
		return err
	}
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
		if err := ins(tx, nil); err != nil {
			return err
		}
		return ins(tx, nil)
	}); err != nil {
		t.Fatal(err)
	}

	migrateDir(t, dsn, "mkg", 0)

	order := ids.New()
	if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error { return ins(tx, &order) }); err != nil {
		t.Fatal(err)
	}
	if got := verifyAll(t, pool); got["mkg_fieldops.roll_count"] != 3 {
		t.Fatalf("sau nâng cấp: số dòng đã kiểm %v", got)
	}
}
