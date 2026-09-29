package migrations_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/opsjobs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Kiểm thử lược đồ cụm ts (ADR-0017, ADR-0019) — ts/000001_ts_weigh.

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// insertBatch ghi một lô [from, to] của device (kỳ epoch) và các số đo của nó trong một giao dịch.
func insertBatch(ctx context.Context, pool *pgxpool.Pool, tid uuid.UUID, device string, epoch int, from, to int64) error {
	return tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
		id := ids.New()
		dt := time.Date(2026, 12, 3, 7, 14, 22, 0, time.UTC)
		if _, err := tx.Exec(ctx, `INSERT INTO ts_weigh.reading_batch
		  (id, tenant_id, device_id, epoch, counter_from, counter_to, reading_count, key_id, payload_sha256, signature,
		   device_time_first, device_time_last, ntp_offset_ms)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, '0123456789abcdef', $8, $9, $10, $11, 12)`,
			id, tid, device, epoch, from, to, to-from+1, bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 64), dt, dt.Add(time.Second)); err != nil {
			return err
		}
		for c := from; c <= to; c++ {
			if _, err := tx.Exec(ctx, `INSERT INTO ts_weigh.raw_reading
			  (tenant_id, batch_id, device_id, epoch, counter, device_time, value_kg, stable, unit)
			  VALUES ($1, $2, $3, $4, $5, $6, '1250.500', true, 'kg')`, tid, id, device, epoch, c, dt); err != nil {
				return err
			}
		}
		return nil
	})
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// R-20 / R-71: số đo thô không sửa, không xóa — vai ứng dụng và owner đều bị chặn.
func TestTSWeighAppendOnly(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	ingest := env.Pool(t, migrations.TS, "ingest_ts")
	owner := env.Pool(t, migrations.TS, dbadmin.Migrator)
	tid := ids.New()
	if err := insertBatch(ctx, ingest, tid, "scale-hub01-in", 1, 1, 3); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"ts_weigh.raw_reading", "ts_weigh.reading_batch"} {
		for _, p := range []struct {
			name string
			pool *pgxpool.Pool
		}{{"ingest_ts", ingest}, {"owner", owner}} {
			for _, stmt := range []string{
				"UPDATE " + table + " SET device_id = 'x'",
				"DELETE FROM " + table,
				"TRUNCATE " + table,
			} {
				err := tenant.WithTenantTx(ctx, p.pool, tid, func(tx pgx.Tx) error {
					_, err := tx.Exec(ctx, stmt)
					return err
				})
				if err == nil {
					t.Errorf("%s: %q phải thất bại", p.name, stmt)
				}
			}
		}
	}
	// ts_reader chỉ đọc.
	reader := env.Pool(t, migrations.TS, "ts_reader")
	if err := insertBatch(ctx, reader, tid, "scale-hub01-in", 1, 4, 4); pgCode(err) != "42501" {
		t.Fatalf("ts_reader không được ghi: %v", err)
	}
	var n int
	if err := tenant.WithTenantTx(ctx, reader, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM ts_weigh.raw_reading WHERE device_id = 'scale-hub01-in'").Scan(&n)
	}); err != nil || n != 3 {
		t.Fatalf("ts_reader phải đọc được 3 số đo: %d %v", n, err)
	}
}

// ADR-0019: bộ đếm đơn điệu theo (device_id, epoch); (device_id, epoch, counter_from) là khóa idempotency.
func TestTSWeighCounterMonotonicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	ingest := pgtest.Get(t).Pool(t, migrations.TS, "ingest_ts")
	tid := ids.New()
	dev := "scale-hub02-out"
	if err := insertBatch(ctx, ingest, tid, dev, 1, 1, 10); err != nil {
		t.Fatal(err)
	}
	// Gửi lặp cùng lô → trùng khóa (ingest đọc lại lô cũ và so payload_sha256).
	if err := insertBatch(ctx, ingest, tid, dev, 1, 1, 10); pgCode(err) != "23505" {
		t.Fatalf("gửi lặp phải unique_violation: %v", err)
	}
	// Chồng lấn / lùi bộ đếm → từ chối.
	for _, r := range [][2]int64{{5, 12}, {10, 11}, {3, 3}} {
		if err := insertBatch(ctx, ingest, tid, dev, 1, r[0], r[1]); pgCode(err) != "23514" || !strings.Contains(err.Error(), "không đơn điệu") {
			t.Fatalf("lô %v phải bị từ chối vì không đơn điệu: %v", r, err)
		}
	}
	// Khoảng trống được nhận (giám sát báo mất lô); kỳ đăng ký mới đếm lại từ 1.
	if err := insertBatch(ctx, ingest, tid, dev, 1, 15, 20); err != nil {
		t.Fatalf("khoảng trống phải được nhận: %v", err)
	}
	if err := insertBatch(ctx, ingest, tid, dev, 2, 1, 5); err != nil {
		t.Fatalf("kỳ mới đếm từ 1: %v", err)
	}
	// reading_count phải khớp khoảng bộ đếm.
	err := tenant.WithTenantTx(ctx, ingest, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO ts_weigh.reading_batch
		  (id, tenant_id, device_id, epoch, counter_from, counter_to, reading_count, key_id, payload_sha256, signature,
		   device_time_first, device_time_last) VALUES ($1, $2, 'scale-x', 1, 1, 5, 4, '0123456789abcdef', $3, $4, now(), now())`,
			ids.New(), tid, bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 64))
		return err
	})
	if pgCode(err) != "23514" {
		t.Fatalf("reading_count sai phải bị CHECK chặn: %v", err)
	}
}

// R-13: received_at do hệ đặt.
func TestTSWeighReceivedAtIsSystemTime(t *testing.T) {
	ctx := context.Background()
	ingest := pgtest.Get(t).Pool(t, migrations.TS, "ingest_ts")
	tid := ids.New()
	err := tenant.WithTenantTx(ctx, ingest, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO ts_weigh.raw_reading
		  (tenant_id, batch_id, device_id, epoch, counter, device_time, value_kg, stable, unit, received_at)
		  VALUES ($1, $2, 'scale-y', 1, 1, now(), '1.000', true, 'kg', now() - interval '1 day')`, tid, ids.New())
		return err
	})
	if pgCode(err) != "23514" || !strings.Contains(err.Error(), "received_at") {
		t.Fatalf("received_at truyền vào phải bị từ chối: %v", err)
	}
}

// RB-2: tenant khác không thấy số đo.
func TestTSWeighTenantIsolation(t *testing.T) {
	ctx := context.Background()
	ingest := pgtest.Get(t).Pool(t, migrations.TS, "ingest_ts")
	a, b := ids.New(), ids.New()
	if err := insertBatch(ctx, ingest, a, "scale-iso", 1, 1, 2); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"ts_weigh.raw_reading", "ts_weigh.reading_batch"} {
		var n int
		if err := tenant.WithTenantTx(ctx, ingest, b, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n)
		}); err != nil || n != 0 {
			t.Fatalf("%s: tenant khác phải thấy 0 dòng, thấy %d (%v)", table, n, err)
		}
	}
	// Không ghi được hộ tenant khác.
	err := tenant.WithTenantTx(ctx, ingest, b, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO ts_weigh.raw_reading
		  (tenant_id, batch_id, device_id, epoch, counter, device_time, value_kg, stable, unit)
		  VALUES ($1, $2, 'scale-iso', 1, 9, now(), '1.000', true, 'kg')`, a, ids.New())
		return err
	})
	if pgCode(err) != "42501" {
		t.Fatalf("ghi hộ tenant khác phải bị RLS chặn: %v", err)
	}
}

// migrate maintain tạo tiếp phân vùng tháng cho raw_reading (bảng chỉ ghi thêm không chuỗi băm).
func TestTSPartitionMaintenance(t *testing.T) {
	ctx := context.Background()
	owner := pgtest.Get(t).Pool(t, migrations.TS, dbadmin.Migrator)
	tables, err := opsjobs.AppendOnlyPartitionedTables(ctx, owner)
	if err != nil || len(tables) != 1 || tables[0] != "ts_weigh.raw_reading" {
		t.Fatalf("phải thấy đúng ts_weigh.raw_reading: %v %v", tables, err)
	}
	created := func(rs []opsjobs.PartitionReport) int {
		n := 0
		for _, r := range rs {
			n += r.Created
		}
		return n
	}
	r1, err := opsjobs.EnsurePartitions(ctx, owner, 24)
	if err != nil || created(r1) != 0 {
		t.Fatalf("apply_append_only đã tạo 24 tháng: %d %v", created(r1), err)
	}
	r2, err := opsjobs.EnsurePartitions(ctx, owner, 26)
	if err != nil || created(r2) != 2 {
		t.Fatalf("mở rộng 26 tháng phải tạo 2 phân vùng: %d %v", created(r2), err)
	}
	for _, view := range []string{"shared.v_missing_immutability", "shared.v_missing_rls"} {
		var n int
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM "+view).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s phải rỗng: %d %v", view, n, err)
		}
	}
}

// Cổng R-20 mở rộng: bảng ts_* thiếu trigger chặn phải bị view bắt (kiểm thử ngược).
func TestTSGateDetectsMissingImmutability(t *testing.T) {
	ctx := context.Background()
	owner := pgtest.Get(t).Pool(t, migrations.TS, dbadmin.Migrator)
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, "CREATE TABLE ts_weigh.bad (tenant_id uuid NOT NULL, v int)"); err != nil {
		t.Fatal(err)
	}
	var problem string
	if err := tx.QueryRow(ctx, "SELECT problem FROM shared.v_missing_immutability WHERE schema_name = 'ts_weigh' AND table_name = 'bad'").Scan(&problem); err != nil {
		t.Fatalf("view phải bắt bảng ts_* thiếu trigger: %v", err)
	}
	if !strings.Contains(problem, "thiếu chặn UPDATE") || strings.Contains(problem, "chuỗi băm") {
		t.Fatalf("bảng ts_* không có row_hash chỉ cần chặn sửa/xóa, không cần chuỗi băm: %q", problem)
	}
}
