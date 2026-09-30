package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ---------------------------------------------------------------------------
// R-32 / R-20 / R-31: rò rỉ tenant qua PgBouncer transaction mode cho schema
// pf_metrics (pf 000036, 000037). Bảng được LIỆT KÊ từ catalog (mọi bảng có cột
// tenant_id, trừ COMMENT 'ag:no-rls') nên bảng mới tự được kiểm.
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

// lkDo gọi HTTP không dùng t (an toàn trong goroutine); lỗi khi status ∉ want.
func lkDo(ctx context.Context, base, token, method, path string, body any, want ...int) (map[string]any, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Idempotency-Key", "k-"+ids.New().String())
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // R-10: không float trong mkg
	_ = dec.Decode(&m)
	for _, w := range want {
		if r.StatusCode == w {
			return m, nil
		}
	}
	return m, fmt.Errorf("%s %s: HTTP %d (muốn %v): %s", method, path, r.StatusCode, want, raw)
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

// R-32 (+ R-20, R-31): TenantLeak cho pf_metrics. Mỗi tenant ghi sổ giá trị ngày (ghim
// định nghĩa lần đầu), nhận sự kiện lớp GIS qua inbox (nhật ký lớp), đọc giá trị qua API,
// và vòng cuối chụp ảnh lớp cuối ngày — song song, xen kẽ đọc/ghi trên pool metrics_app.
func TestMetricsTenantLeakParallel(t *testing.T) {
	now := time.Now()
	f := newFixture(t, clock.NewFixed(now.Add(48*time.Hour))) // hôm nay (thật) đã "kết thúc" để chụp ảnh lớp
	tenants, rounds := lkSize(t, pgtest.Get(t).ViaBouncer)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	const schema = "pf_metrics"
	pools := []lkPool{{role: metrics.DBRole, pool: f.pool, tables: lkTables(ctx, t, f.pool, schema)}}
	t.Logf("%s: %d bảng: %v", schema, len(pools[0].tables), pools[0].tables)
	svc := f.mod.Service()
	key := port.Key("kn.sown_area_ha")
	f.src.set(key, "", "", "120.5")
	today := clock.BusinessDate(now)

	prepare := func(i int, tid uuid.UUID) func(context.Context, int) error {
		viewer := f.as(tid, all, port.RoleMetricsViewer).token
		return func(ctx context.Context, r int) error {
			d := domain.AddDays(today, -1-r)
			if _, err := svc.RecordDay(ctx, tid, d, []port.Key{key}); err != nil {
				return fmt.Errorf("RecordDay %s: %w", d, err)
			}
			coop := ids.New()
			raw, err := json.Marshal(geoport.PlotCreatedV1{PlotRef: ids.New(), Code: fmt.Sprintf("TH%03d%04d", i, r), CommuneCode: "80001",
				CooperativeRef: &coop, Status: "active", BoundaryID: ids.New(), AreaHa: "1.2500", Source: "kn_web", ActorRef: ids.New(),
				EventTime: now.UTC().Format(time.RFC3339)})
			if err != nil {
				return err
			}
			env := outbox.Envelope{EventID: ids.New(), EventType: geoport.EventPlotCreated, SchemaVersion: 1, TenantID: tid, OccurredAt: now.UTC(),
				RecordedAt: now.UTC(), Producer: "pf/test", Payload: raw}
			if _, err := inbox.Process(ctx, f.pool, port.LayerLogConsumer, env, f.mod.LayerLogHandler()); err != nil {
				return fmt.Errorf("nhật ký lớp: %w", err)
			}
			if _, err := lkDo(ctx, f.srv.URL, viewer, http.MethodGet, "/v1/metrics/"+string(key)+"/values?from="+d.String()+"&to="+d.String(), nil,
				http.StatusOK); err != nil {
				return err
			}
			if r == rounds-1 {
				if _, err := svc.BuildSnapshots(ctx, tid, today); err != nil {
					return fmt.Errorf("ảnh chụp lớp: %w", err)
				}
			}
			return nil
		}
	}
	lkRun(t, ctx, f.owner, pools, tenants, rounds, prepare, []string{
		"pf_metrics.definition_pin", "pf_metrics.daily_value", "pf_metrics.layer_event", "pf_metrics.layer_snapshot",
	})
}
