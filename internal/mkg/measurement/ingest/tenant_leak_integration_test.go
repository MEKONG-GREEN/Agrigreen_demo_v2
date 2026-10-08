package ingest

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ---------------------------------------------------------------------------
// R-32 / R-20 / R-31: rò rỉ tenant qua PgBouncer transaction mode cho schema ts_weigh
// (cụm ts, ts 000001; vai ingest_ts ghi, ts_reader đọc). Bảng được LIỆT KÊ từ catalog
// (mọi bảng có cột tenant_id, trừ COMMENT 'ag:no-rls') nên bảng mới tự được kiểm.
// ---------------------------------------------------------------------------

// lkTables liệt kê bảng có tenant_id của schema mà vai của pool được SELECT; kiểm
// luôn ENABLE + FORCE RLS trên mọi bảng đó (kể cả phân vùng — ADR-0005).
func lkTables(ctx context.Context, t *testing.T, pool *pgxpool.Pool, schema string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `
	  SELECT format('%I.%I', n.nspname, c.relname), c.relrowsecurity, c.relforcerowsecurity,
	         c.relispartition, has_table_privilege(c.oid, 'SELECT')
	  FROM pg_class c
	  JOIN pg_namespace n ON n.oid = c.relnamespace
	  JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped
	  WHERE n.nspname = $1::text AND c.relkind IN ('r', 'p')
	    AND coalesce(obj_description(c.oid, 'pg_class'), '') NOT LIKE 'ag:no-rls%'
	  ORDER BY 1`, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out, skipped []string
	for rows.Next() {
		var name string
		var rls, force, part, sel bool
		if err := rows.Scan(&name, &rls, &force, &part, &sel); err != nil {
			t.Fatal(err)
		}
		if !rls || !force {
			t.Errorf("R-33: %s có tenant_id nhưng thiếu ENABLE/FORCE ROW LEVEL SECURITY", name)
		}
		switch {
		case part:
		case !sel:
			skipped = append(skipped, name)
		default:
			out = append(out, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("schema %s: không có bảng tenant nào đọc được", schema)
	}
	if len(skipped) > 0 {
		t.Logf("%s: vai không có SELECT (không đọc được thì không rò): %v", schema, skipped)
	}
	return out
}

// lkCheck: trong giao dịch tenant tid, mọi bảng chỉ có dòng của tid. Trả số dòng của tid.
func lkCheck(ctx context.Context, tx pgx.Tx, tables []string, tid uuid.UUID) (map[string]int, error) {
	own := make(map[string]int, len(tables))
	for _, tb := range tables {
		var foreign, mine, all int
		if err := tx.QueryRow(ctx, "SELECT count(*) FILTER (WHERE tenant_id <> $1), count(*) FILTER (WHERE tenant_id = $1), count(*) FROM "+tb,
			tid).Scan(&foreign, &mine, &all); err != nil {
			return nil, fmt.Errorf("%s: %w", tb, err)
		}
		if foreign != 0 || all != mine {
			return nil, fmt.Errorf("RÒ RỈ R-32: tenant %s thấy %d dòng của tenant khác trong %s (tổng %d, của mình %d)", tid, foreign, tb, all, mine)
		}
		own[tb] = mine
	}
	return own, nil
}

// lkNoContext (R-31): không ngữ cảnh tenant — autocommit và giao dịch không set_config — thấy 0 dòng.
func lkNoContext(ctx context.Context, pool *pgxpool.Pool, tables []string) error {
	for _, tb := range tables {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+tb).Scan(&n); err != nil {
			return fmt.Errorf("%s: %w", tb, err)
		}
		if n != 0 {
			return fmt.Errorf("RÒ RỈ R-31: không ngữ cảnh tenant mà thấy %d dòng trong %s", n, tb)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, tb := range tables {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+tb).Scan(&n); err != nil {
			return fmt.Errorf("%s: %w", tb, err)
		}
		if n != 0 {
			return fmt.Errorf("RÒ RỈ R-31: giao dịch không ngữ cảnh thấy %d dòng trong %s", n, tb)
		}
	}
	return nil
}

// lkPool là một vai của module cần kiểm (vai ứng dụng, vai ingest...).
type lkPool struct {
	role   string
	pool   *pgxpool.Pool
	tables []string
}

// lkRun chạy mẫu R-32: n tenant song song, mỗi vòng ghi qua đường thật của module rồi
// kiểm ngay (qua cùng pool — kết nối được dùng lại xen kẽ giữa các tenant), rồi kiểm
// không-ngữ-cảnh. Cuối cùng so số dòng tenant thấy với số dòng thật (admin, bỏ qua RLS).
func lkRun(t *testing.T, ctx context.Context, admin *pgxpool.Pool, pools []lkPool, tenants, rounds int,
	prepare func(i int, tid uuid.UUID) func(ctx context.Context, round int) error, wantData []string) {
	t.Helper()
	tids := make([]uuid.UUID, tenants)
	writes := make([]func(context.Context, int) error, tenants)
	for i := range tids {
		tids[i] = ids.New()
		writes[i] = prepare(i, tids[i])
	}
	var wg sync.WaitGroup
	errs := make(chan error, tenants)
	for i := range tids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tid := tids[i]
			for r := 0; r < rounds; r++ {
				if err := writes[i](ctx, r); err != nil {
					errs <- fmt.Errorf("tenant %d vòng %d ghi: %w", i, r, err)
					return
				}
				for _, p := range pools {
					if err := tenant.WithTenantTx(ctx, p.pool, tid, func(tx pgx.Tx) error {
						_, err := lkCheck(ctx, tx, p.tables, tid)
						return err
					}); err != nil {
						errs <- fmt.Errorf("vai %s: %w", p.role, err)
						return
					}
					if err := lkNoContext(ctx, p.pool, p.tables); err != nil {
						errs <- fmt.Errorf("vai %s: %w", p.role, err)
						return
					}
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	// Tenant thấy đúng TẤT CẢ dòng của mình (RLS không che dòng của chính tenant), và
	// có dữ liệu thật trong các bảng wantData.
	seen := map[string]int{}
	for _, p := range pools {
		for _, tid := range tids {
			var own map[string]int
			if err := tenant.WithTenantTx(ctx, p.pool, tid, func(tx pgx.Tx) error {
				var err error
				own, err = lkCheck(ctx, tx, p.tables, tid)
				return err
			}); err != nil {
				t.Fatalf("vai %s: %v", p.role, err)
			}
			for _, tb := range p.tables {
				var real int
				if err := admin.QueryRow(ctx, "SELECT count(*) FROM "+tb+" WHERE tenant_id = $1", tid).Scan(&real); err != nil {
					t.Fatal(err)
				}
				if real != own[tb] {
					t.Errorf("vai %s, %s: tenant %s thấy %d dòng, thực có %d", p.role, tb, tid, own[tb], real)
				}
				if own[tb] > 0 {
					seen[tb]++
				}
			}
		}
		if err := lkNoContext(ctx, p.pool, p.tables); err != nil {
			t.Errorf("vai %s: %v", p.role, err)
		}
	}
	for _, tb := range wantData {
		if seen[tb] == 0 {
			t.Errorf("không tenant nào có dữ liệu trong %s — đường ghi của kiểm thử không phủ bảng này", tb)
		}
	}
	var empty []string
	listed := map[string]bool{}
	for _, p := range pools {
		for _, tb := range p.tables {
			if seen[tb] == 0 && !listed[tb] {
				listed[tb] = true
				empty = append(empty, tb)
			}
		}
	}
	if len(empty) > 0 {
		t.Logf("bảng chỉ được kiểm rỗng (chưa có đường ghi trong kiểm thử): %v", empty)
	}
}

// lkSize: 24 tenant × 5 vòng qua PgBouncer (như TestTenantLeakParallel); nhẹ hơn khi nối thẳng.
func lkSize(t *testing.T, viaBouncer bool) (tenants, rounds int) {
	t.Helper()
	if viaBouncer {
		t.Log("chạy qua PgBouncer transaction mode")
		return 24, 5
	}
	t.Log("CẢNH BÁO: chạy thẳng PostgreSQL — CI phải đặt AG_TEST_PGBOUNCER_URL")
	return 8, 3
}

// lkBatch dựng và ký lô [from, from+n-1] (không dùng t — an toàn trong goroutine).
func lkBatch(key ed25519.PrivateKey, device string, from int64, n int) (edgewire.Batch, error) {
	off := 7
	b := edgewire.Batch{Format: edgewire.Format, DeviceID: device, Epoch: 1, CounterFrom: from, NTPOffsetMs: &off}
	t0 := time.Date(2026, 1, 5, 0, 14, 22, 0, time.UTC) // quá khứ so với giờ DB
	for i := 0; i < n; i++ {
		c := from + int64(i)
		b.Readings = append(b.Readings, edgewire.Reading{Counter: c, DeviceTime: edgewire.FormatDeviceTime(t0.Add(time.Duration(c) * 500 * time.Millisecond)),
			ValueKg: "1250.500", Stable: true, Unit: edgewire.UnitKg})
	}
	b.CounterTo = from + int64(n) - 1
	return b, edgewire.Sign(key, &b)
}

// R-32 (+ R-20, R-31): TenantLeak cho ts_weigh. Mỗi tenant có một cân đã duyệt; mỗi vòng
// agent biên gửi một lô ký qua Receiver (vai ingest_ts ghi reading_batch + raw_reading),
// rồi tích phân cửa sổ bằng vai ts_reader — song song, xen kẽ đọc/ghi trên cả hai pool.
func TestTSWeighTenantLeakParallel(t *testing.T) {
	r := newRig(t)
	env := pgtest.Get(t)
	tenants, rounds := lkSize(t, env.ViaBouncer)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	const schema = "ts_weigh"
	writer, reader := env.Pool(t, migrations.TS, port.IngestTSRole), env.Pool(t, migrations.TS, port.TSReaderRole)
	pools := []lkPool{
		// Kiểm RLS cả NGOÀI ngữ cảnh (lkNoContext truy vấn thẳng pool) — cần vai có quyền trực tiếp: vai cũ ingest_ts /
		// ts_reader (quyền giữ một kỳ, W2.6). Rig ghi/đọc qua app_ingest → r_measurement.
		{role: port.IngestTSRole, pool: writer, tables: lkTables(ctx, t, writer, schema)},
		{role: port.TSReaderRole, pool: reader, tables: lkTables(ctx, t, reader, schema)},
	}
	t.Logf("%s: %v (%s), %v (%s)", schema, pools[0].tables, port.IngestTSRole, pools[1].tables, port.TSReaderRole)
	key := vectorKey()
	pub := key.Public().(ed25519.PublicKey)
	in := &Integrator{Windows: r.store}

	prepare := func(i int, tid uuid.UUID) func(context.Context, int) error {
		device := fmt.Sprintf("scale-lk%02d-in", i)
		r.enroll(tid, device, 1, pub) // ghi map của rig trước khi chạy song song
		return func(ctx context.Context, round int) error {
			const n = 3
			from := int64(round*10 + 1)
			b, err := lkBatch(key, device, from, n)
			if err != nil {
				return err
			}
			body, err := json.Marshal(b)
			if err != nil {
				return err
			}
			if code, p := r.post(device, body); code != http.StatusOK {
				return fmt.Errorf("gửi lô %s@%d: HTTP %d %v", device, from, code, p)
			}
			res, err := in.Integrate(ctx, tid, port.Window{DeviceID: device, Epoch: 1, CounterFrom: from, CounterTo: from + n - 1})
			if err != nil {
				return fmt.Errorf("tích phân: %w", err)
			}
			if res.ReadingsTotal != n {
				return fmt.Errorf("tích phân %s@%d: %d số đo, muốn %d", device, from, res.ReadingsTotal, n)
			}
			return nil
		}
	}
	lkRun(t, ctx, env.AdminPool(t, migrations.TS), pools, tenants, rounds, prepare, []string{"ts_weigh.reading_batch", "ts_weigh.raw_reading"})
}
