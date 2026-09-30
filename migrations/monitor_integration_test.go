package migrations_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Vai giám sát vận hành (mục 19; mkg 000013, ts 000003): mkg_monitor / ts_monitor chỉ EXECUTE hàm
// gộp SECURITY DEFINER, không đọc được bảng gốc; hàm đếm xuyên tenant, không trả tenant/khối lượng.

// Mục 19 — vai giám sát KHÔNG SELECT được bảng gốc, không BYPASSRLS, không SET ROLE sang chủ hàm.
func TestMonitorRolesCannotReadBaseTables(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	cases := []struct {
		cluster migrations.Cluster
		role    string
		owner   string
		tables  []string
	}{
		{migrations.MKG, dbadmin.MKGMonitor, dbadmin.MKGMonitorOwner, []string{
			"mkg_measurement.device_current", "mkg_measurement.reconciliation", "mkg_measurement.dispute_current",
			"mkg_measurement.weigh_ticket", "mkg_measurement.lot_mass_current", "shared.outbox",
		}},
		{migrations.TS, dbadmin.TSMonitor, dbadmin.TSMonitorOwner, []string{
			"ts_weigh.reading_batch", "ts_weigh.raw_reading", "shared.outbox",
		}},
	}
	for _, c := range cases {
		pool := env.DirectPool(t, c.cluster, c.role)
		for _, table := range c.tables {
			_, err := pool.Exec(ctx, "SELECT 1 FROM "+table+" LIMIT 1")
			if code := pgCode(err); code != "42501" {
				t.Errorf("%s: SELECT %s phải bị từ chối (42501), nhận %v", c.role, table, err)
			}
		}
		var bypass, super bool
		if err := pool.QueryRow(ctx, "SELECT rolbypassrls, rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&bypass, &super); err != nil {
			t.Fatal(err)
		}
		if bypass || super {
			t.Errorf("%s không được BYPASSRLS/SUPERUSER", c.role)
		}
		if _, err := pool.Exec(ctx, "SET ROLE "+c.owner); pgCode(err) != "42501" {
			t.Errorf("%s không được SET ROLE %s: %v", c.role, c.owner, err)
		}
	}
}

// Mục 19 — hàm giám sát trả số đếm/tuổi của MỌI tenant, không có cột tenant/khối lượng.
func TestMonitorFunctionsAggregateAcrossTenants(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	meas := env.Pool(t, migrations.MKG, "measurement_app")
	mon := env.DirectPool(t, migrations.MKG, dbadmin.MKGMonitor)
	suffix := strings.ToLower(ids.New().String()[24:])
	t1, t2 := ids.New(), ids.New()
	now := time.Now().UTC()
	devs := map[uuid.UUID]string{t1: "mon-a-" + suffix, t2: "mon-b-" + suffix}
	for tid, dev := range devs {
		err := tenant.WithTenantTx(ctx, meas, tid, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO mkg_measurement.device_current
			  (tenant_id, device_id, epoch, facility_ref, point, public_key, key_id, cert_sha256, scale_model, status, version_id,
			   proposed_by, proposed_at, decided_by, decided_at, last_seen_at, buffer_pct)
			  VALUES ($1, $2, 1, $3, 'HUB_IN', $4, '0123456789abcdef', $5, 'sim', 'active', $6, $7, $8, $9, $8, $8, 42.5)`,
				tid, dev, ids.New(), bytes.Repeat([]byte{1}, 32), strings.Repeat("ab", 32), ids.New(), ids.New(), now, ids.New()); err != nil {
				return err
			}
			for i := 0; i < 2; i++ {
				if _, err := tx.Exec(ctx, `INSERT INTO mkg_measurement.dispute_current
				  (tenant_id, dispute_id, lot_ref, status, severity, reconciliation_id, diff_kg, evidence_refs, opened_at, opened_by, last_seq)
				  VALUES ($1, $2, $3, 'open', $4, $5, '-150.000', '[]'::jsonb, $6, $7, 1)`,
					tid, ids.New(), ids.New(), "mon-"+suffix, ids.New(), now.Add(-time.Duration(i+1)*time.Hour), ids.New()); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Không có ngữ cảnh tenant: vai giám sát vẫn thấy cả hai tenant (qua hàm, không qua bảng).
	rows, err := mon.Query(ctx, "SELECT * FROM mkg_monitor.device_health()")
	if err != nil {
		t.Fatal(err)
	}
	cols := map[string]bool{}
	for _, f := range rows.FieldDescriptions() {
		cols[f.Name] = true
	}
	seen := map[string]float64{}
	for rows.Next() {
		var dev, epoch, point, status string
		var ts float64
		var buf *float64
		if err := rows.Scan(&dev, &epoch, &point, &status, &ts, &buf); err != nil {
			t.Fatal(err)
		}
		if buf != nil {
			seen[dev] = *buf
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, dev := range devs {
		if seen[dev] != 42.5 {
			t.Fatalf("device_health phải thấy thiết bị của mọi tenant: %v", seen)
		}
	}
	for _, forbidden := range []string{"tenant_id", "facility_ref", "public_key", "cert_sha256", "proposed_by", "decided_by"} {
		if cols[forbidden] {
			t.Fatalf("device_health không được trả cột %s: %v", forbidden, cols)
		}
	}

	var n float64
	if err := mon.QueryRow(ctx, "SELECT n FROM mkg_monitor.disputes_open() WHERE severity = $1", "mon-"+suffix).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("disputes_open: 2 tenant × 2 tranh chấp = 4, nhận %v", n)
	}
	var oldest float64
	if err := mon.QueryRow(ctx, "SELECT mkg_monitor.dispute_oldest_open_ts()").Scan(&oldest); err != nil {
		t.Fatal(err)
	}
	if oldest <= 0 || oldest > float64(now.Add(-2*time.Hour).Unix()+1) {
		t.Fatalf("dispute_oldest_open_ts = %v, mong ≤ %d", oldest, now.Add(-2*time.Hour).Unix())
	}
	if _, err := mon.Exec(ctx, "SELECT * FROM mkg_monitor.reconciliations_24h()"); err != nil {
		t.Fatal(err)
	}

	// Cụm ts: hai tenant, hai thiết bị.
	ingest := env.Pool(t, migrations.TS, "ingest_ts")
	tsMon := env.DirectPool(t, migrations.TS, dbadmin.TSMonitor)
	for tid, dev := range devs {
		if err := insertBatch(ctx, ingest, tid, dev, 1, 1, 2); err != nil {
			t.Fatal(err)
		}
	}
	rows, err = tsMon.Query(ctx, "SELECT device_id, epoch, last_batch_ts FROM ts_monitor.device_last_batch()")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for rows.Next() {
		var dev, epoch string
		var ts float64
		if err := rows.Scan(&dev, &epoch, &ts); err != nil {
			t.Fatal(err)
		}
		got[dev] = ts > 0
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, dev := range devs {
		if !got[dev] {
			t.Fatalf("device_last_batch phải thấy thiết bị của mọi tenant: %v", got)
		}
	}
	if err := tsMon.QueryRow(ctx, "SELECT ts_monitor.batches_1h()").Scan(&n); err != nil || n < 2 {
		t.Fatalf("batches_1h = %v (%v), mong ≥ 2", n, err)
	}
	// Chủ hàm không đăng nhập được.
	for _, owner := range []string{dbadmin.MKGMonitorOwner, dbadmin.TSMonitorOwner} {
		var login bool
		if err := tsMon.QueryRow(ctx, "SELECT rolcanlogin FROM pg_roles WHERE rolname = $1", owner).Scan(&login); err != nil {
			t.Fatal(fmt.Errorf("%s: %w", owner, err))
		}
		if login {
			t.Fatalf("%s phải NOLOGIN", owner)
		}
	}
}
