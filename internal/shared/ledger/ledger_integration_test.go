package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

const rollCount = "mkg_fieldops.roll_count"

func insertRoll(ctx context.Context, tx pgx.Tx, tid uuid.UUID, stream, idem string, rolls int) (uuid.UUID, error) {
	id := ids.New()
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.roll_count
	  (id, tenant_id, stream_key, plot_ref, htx_ref, rolls, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'roll_count/v1')`,
		id, tid, stream, ids.New(), ids.New(), rolls, time.Now().UTC().Add(-time.Hour), ids.New(), idem)
	return id, ledger.Classify(err)
}

func appPool(t *testing.T) *pgxpool.Pool {
	return pgtest.Get(t).Pool(t, migrations.MKG, "fieldops_app")
}

// R-20: UPDATE, DELETE, TRUNCATE bằng vai ứng dụng và bằng owner — cả sáu phải thất bại.
func TestAppendOnlySixWays(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	app := appPool(t)
	owner := env.Pool(t, migrations.MKG, dbadmin.Migrator)
	tid := ids.New()

	var rowID uuid.UUID
	if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		var err error
		rowID, err = insertRoll(ctx, tx, tid, "lot:"+ids.New().String(), ids.New().String(), 12)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	stmts := map[string]string{
		"UPDATE":   "UPDATE mkg_fieldops.roll_count SET rolls = 999 WHERE id = $1",
		"DELETE":   "DELETE FROM mkg_fieldops.roll_count WHERE id = $1",
		"TRUNCATE": "TRUNCATE mkg_fieldops.roll_count",
	}
	for _, who := range []struct {
		name string
		pool *pgxpool.Pool
	}{{"fieldops_app", app}, {"owner ag_migrator", owner}} {
		for op, sql := range stmts {
			t.Run(who.name+"/"+op, func(t *testing.T) {
				// Đặt đúng tenant để dòng hiện ra dưới RLS — nếu không, UPDATE 0 dòng
				// sẽ "thành công" mà không chạm trigger.
				err := tenant.WithTenantTx(ctx, who.pool, tid, func(tx pgx.Tx) error {
					var err error
					if op == "TRUNCATE" {
						_, err = tx.Exec(ctx, sql)
					} else {
						_, err = tx.Exec(ctx, sql, rowID)
					}
					return err
				})
				if err == nil {
					t.Fatalf("%s bằng %s phải thất bại", op, who.name)
				}
				if !errors.Is(ledger.Classify(err), ledger.ErrImmutable) {
					t.Fatalf("muốn lỗi quyền (42501), nhận %v", err)
				}
			})
		}
	}

	// Truy cập thẳng phân vùng: trigger TRUNCATE mức câu lệnh không tự nhân bản.
	t.Run("owner/TRUNCATE partition", func(t *testing.T) {
		_, err := owner.Exec(ctx, "TRUNCATE mkg_fieldops.roll_count_default")
		if !errors.Is(ledger.Classify(err), ledger.ErrImmutable) {
			t.Fatalf("TRUNCATE phân vùng phải thất bại, nhận %v", err)
		}
	})

	// Dòng vẫn còn nguyên.
	if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		var rolls int
		if err := tx.QueryRow(ctx, "SELECT rolls FROM mkg_fieldops.roll_count WHERE id = $1", rowID).Scan(&rolls); err != nil {
			return err
		}
		if rolls != 12 {
			return fmt.Errorf("rolls = %d", rolls)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Cổng CI: không bảng sổ nào thiếu trigger, không bảng nghiệp vụ nào thiếu RLS.
func TestCatalogGates(t *testing.T) {
	env := pgtest.Get(t)
	for _, c := range migrations.Clusters {
		owner := env.Pool(t, c, dbadmin.Migrator)
		for _, view := range []string{"shared.v_missing_immutability", "shared.v_missing_rls"} {
			rows, err := owner.Query(context.Background(), "SELECT schema_name, table_name, problem FROM "+view)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var s, tb, p string
				if err := rows.Scan(&s, &tb, &p); err != nil {
					t.Fatal(err)
				}
				t.Errorf("%s/%s: %s.%s — %s", c, view, s, tb, p)
			}
			rows.Close()
		}
	}
}

// Kiểm thử ngược: view cổng phải BẮT được bảng sổ thiếu trigger và bảng nghiệp vụ thiếu RLS.
func TestCatalogGatesDetectViolations(t *testing.T) {
	ctx := context.Background()
	owner := pgtest.Get(t).Pool(t, migrations.MKG, dbadmin.Migrator)
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `CREATE SCHEMA mkg_gate_probe;
	  CREATE TABLE mkg_gate_probe.bad_ledger (id uuid, tenant_id uuid, row_hash bytea, prev_hash bytea);
	  CREATE TABLE mkg_gate_probe.no_rls (id uuid, tenant_id uuid);
	  CREATE TABLE mkg_gate_probe.no_tenant (id uuid)`); err != nil {
		t.Fatal(err)
	}
	count := func(view string) int {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+view+" WHERE schema_name = 'mkg_gate_probe'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("shared.v_missing_immutability"); n != 1 {
		t.Errorf("v_missing_immutability phải bắt 1 bảng sổ thiếu trigger, bắt %d", n)
	}
	if n := count("shared.v_missing_rls"); n != 3 {
		t.Errorf("v_missing_rls phải bắt 3 bảng, bắt %d", n)
	}
}

func TestChainSequenceAndTamperDetection(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	app := appPool(t)
	tid := ids.New()
	stream := "lot:" + ids.New().String()

	var target uuid.UUID
	for i := 1; i <= 5; i++ {
		if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
			id, err := insertRoll(ctx, tx, tid, stream, ids.New().String(), i*10)
			if i == 3 {
				target = id
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}

	verify := func() ledger.VerifyResult {
		var r ledger.VerifyResult
		if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
			var err error
			r, err = ledger.Verify(ctx, tx, rollCount, tid, stream)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := verify(); !r.OK || r.RowsChecked != 5 {
		t.Fatalf("chuỗi phải hợp lệ với 5 dòng: %+v", r)
	}

	// Kẻ có quyền superuser vượt trigger để sửa dữ liệu — chuỗi băm phải phát hiện.
	admin := env.AdminPool(t, migrations.MKG)
	conn, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET session_replication_role = replica"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "UPDATE mkg_fieldops.roll_count SET rolls = 999 WHERE id = $1", target); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "RESET session_replication_role"); err != nil {
		t.Fatal(err)
	}

	r := verify()
	if r.OK || r.BrokenAtSeq == nil || *r.BrokenAtSeq != 3 {
		t.Fatalf("phải báo đứt tại seq 3: %+v", r)
	}
}

func TestDuplicateIdemKey(t *testing.T) {
	ctx := context.Background()
	app := appPool(t)
	tid := ids.New()
	idem := ids.New().String()
	stream := "lot:" + ids.New().String()
	write := func() error {
		return tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
			_, err := insertRoll(ctx, tx, tid, stream, idem, 5)
			return err
		})
	}
	if err := write(); err != nil {
		t.Fatal(err)
	}
	if err := write(); !errors.Is(err, ledger.ErrDuplicate) {
		t.Fatalf("muốn ErrDuplicate, nhận %v", err)
	}
	// Cùng idem_key ở tenant khác là hợp lệ.
	other := ids.New()
	if err := tenant.WithTenantTx(ctx, app, other, func(tx pgx.Tx) error {
		_, err := insertRoll(ctx, tx, other, stream, idem, 5)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// W2.1 (DB-11, ADR-0012): dòng đăng ký idem_key có hạn mặc định recorded_at + 30 ngày.
func TestLedgerIdemExpiryDefault(t *testing.T) {
	ctx := context.Background()
	owner := pgtest.Get(t).Pool(t, migrations.MKG, dbadmin.Migrator)
	tid := ids.New()
	idem := ids.New().String()
	if err := tenant.WithTenantTx(ctx, appPool(t), tid, func(tx pgx.Tx) error {
		_, err := insertRoll(ctx, tx, tid, "lot:"+ids.New().String(), idem, 3)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var ttlSec int64
	if err := owner.QueryRow(ctx, `SELECT extract(epoch FROM expires_at - recorded_at)::bigint
	    FROM shared.ledger_idem WHERE ledger = $1 AND tenant_id = $2 AND idem_key = $3`, rollCount, tid, idem).Scan(&ttlSec); err != nil {
		t.Fatal(err)
	}
	if ttl := time.Duration(ttlSec) * time.Second; ttl != 30*24*time.Hour {
		t.Fatalf("hạn chống trùng phải là 30 ngày, nhận %v", ttl)
	}
}

// W2.1: trong hạn → ErrDuplicate; hết hạn (dù job dọn chưa chạy) → ghi như bản ghi mới,
// dòng đăng ký trỏ sang bản ghi mới và có hạn mới; khóa vẫn tra được ở cột idem_key của sổ.
func TestDuplicateIdemKeyExpiresAfter30Days(t *testing.T) {
	ctx := context.Background()
	app := appPool(t)
	owner := pgtest.Get(t).Pool(t, migrations.MKG, dbadmin.Migrator)
	tid := ids.New()
	idem := ids.New().String()
	stream := "lot:" + ids.New().String()
	write := func() (uuid.UUID, error) {
		var id uuid.UUID
		err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
			var err error
			id, err = insertRoll(ctx, tx, tid, stream, idem, 5)
			return err
		})
		return id, err
	}
	first, err := write()
	if err != nil {
		t.Fatal(err)
	}
	// Gần hết hạn (còn 1 phút) vẫn là trùng.
	setExpiry := func(expr string) {
		t.Helper()
		if _, err := owner.Exec(ctx, `UPDATE shared.ledger_idem SET expires_at = `+expr+`
		    WHERE ledger = $1 AND tenant_id = $2 AND idem_key = $3`, rollCount, tid, idem); err != nil {
			t.Fatal(err)
		}
	}
	setExpiry("now() + interval '1 minute'")
	if _, err := write(); !errors.Is(err, ledger.ErrDuplicate) {
		t.Fatalf("trong hạn phải là ErrDuplicate, nhận %v", err)
	}
	// Quá 30 ngày: nhận như mới.
	setExpiry("now() - interval '1 second'")
	second, err := write()
	if err != nil {
		t.Fatalf("sau hạn 30 ngày, cùng idem_key phải được nhận như bản ghi mới: %v", err)
	}
	var rowID uuid.UUID
	var leftSec int64
	if err := owner.QueryRow(ctx, `SELECT row_id, extract(epoch FROM expires_at - now())::bigint
	    FROM shared.ledger_idem WHERE ledger = $1 AND tenant_id = $2 AND idem_key = $3`, rollCount, tid, idem).Scan(&rowID, &leftSec); err != nil {
		t.Fatal(err)
	}
	if rowID != second || rowID == first || time.Duration(leftSec)*time.Second < 29*24*time.Hour {
		t.Fatalf("dòng đăng ký phải trỏ bản ghi mới với hạn mới: row=%s first=%s second=%s còn=%ds", rowID, first, second, leftSec)
	}
	// Lần ghi mới lại được chống trùng trong 30 ngày tiếp theo.
	if _, err := write(); !errors.Is(err, ledger.ErrDuplicate) {
		t.Fatalf("bản ghi mới phải được chống trùng, nhận %v", err)
	}
	if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM mkg_fieldops.roll_count WHERE idem_key = $1", idem).Scan(&n); err != nil {
			return err
		}
		if n != 2 {
			return fmt.Errorf("sổ phải giữ cả hai bản ghi cùng idem_key, có %d", n)
		}
		r, err := ledger.Verify(ctx, tx, rollCount, tid, stream)
		if err != nil {
			return err
		}
		if !r.OK || r.RowsChecked != 2 {
			return fmt.Errorf("chuỗi: %+v", r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordedAtIsSystemOwned(t *testing.T) {
	ctx := context.Background()
	app := appPool(t)
	tid := ids.New()
	err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.roll_count
		  (id, tenant_id, stream_key, plot_ref, htx_ref, rolls, event_time, recorded_at, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'lot:x', $3, $4, 1, now(), now() - interval '10 days', $5, $6, 'v1')`,
			ids.New(), tid, ids.New(), ids.New(), ids.New(), ids.New().String())
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "recorded_at") {
		t.Fatalf("truyền recorded_at lùi ngày phải bị fn_chain từ chối, nhận %v", err)
	}
}

func TestConcurrentAppendsSameStream(t *testing.T) {
	ctx := context.Background()
	app := appPool(t)
	tid := ids.New()
	stream := "lot:" + ids.New().String()
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
				_, err := insertRoll(ctx, tx, tid, stream, ids.New().String(), 1)
				return err
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		r, err := ledger.Verify(ctx, tx, rollCount, tid, stream)
		if err != nil {
			return err
		}
		if !r.OK || r.RowsChecked != n {
			return fmt.Errorf("chuỗi sau ghi song song: %+v", r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Băm không được phụ thuộc múi giờ phiên của người ghi hay người xác minh.
func TestHashIndependentOfSessionTimezone(t *testing.T) {
	ctx := context.Background()
	app := appPool(t)
	tid := ids.New()
	stream := "lot:" + ids.New().String()
	if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL timezone = 'Asia/Ho_Chi_Minh'"); err != nil {
			return err
		}
		_, err := insertRoll(ctx, tx, tid, stream, ids.New().String(), 7)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL timezone = 'America/New_York'"); err != nil {
			return err
		}
		r, err := ledger.Verify(ctx, tx, rollCount, tid, stream)
		if err != nil {
			return err
		}
		if !r.OK {
			return fmt.Errorf("chuỗi phải hợp lệ bất kể múi giờ: %+v", r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
