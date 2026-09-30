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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ---------------------------------------------------------------------------
// R-32 / R-20 / R-31: rò rỉ tenant qua PgBouncer transaction mode cho schema
// mkg_fieldops (G3: mkg 000006 + cột ticket_ref của weighed_loading_current ở 000010;
// các bảng G1/G2 của schema cũng được kiểm). Bảng được LIỆT KÊ từ catalog (mọi bảng
// có cột tenant_id, trừ COMMENT 'ag:no-rls') nên bảng mới tự được kiểm.
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

// R-32 (+ R-20, R-31): TenantLeak cho mkg_fieldops. Mỗi tenant lập/duyệt lệnh gom (hai
// giám sát), giao tổ, mở ca tổ gom, đếm cuộn đủ ba công đoạn (bảng kê qua van, đối soát
// chiều mua), HTX xác nhận, và consumer phiếu cân ghi bảng đệm kg/cuộn — song song, xen
// kẽ đọc/ghi trên cùng pool fieldops_app.
func TestFieldopsTenantLeakParallel(t *testing.T) {
	f := newFixture(t, nil)
	tenants, rounds := lkSize(t, f.env.ViaBouncer)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	const schema = "mkg_fieldops"
	pools := []lkPool{{role: port.DBRole, pool: f.pool, tables: lkTables(ctx, t, f.pool, schema)}}
	t.Logf("%s: %d bảng: %v", schema, len(pools[0].tables), pools[0].tables)
	now := f.clk.Now()
	ts := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }

	prepare := func(i int, tid uuid.UUID) func(context.Context, int) error {
		sign := func(roles ...string) string {
			return f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: roles})
		}
		supA, supB := sign(port.RoleFieldSupervisor), sign(port.RoleFieldSupervisor)
		colActor := ids.New()
		col := f.iss.Sign(t, authztest.Token{Subject: colActor, TenantID: tid, Roles: []string{port.RoleFieldCollector}})
		htx, plot := ids.New(), ids.New()
		f.dir.AddHTX(htx, "31117")
		f.dir.AddPlot(plot, htx, "31117")
		return func(ctx context.Context, r int) error {
			post := func(tok, path string, body any, want ...int) (map[string]any, error) {
				return lkDo(ctx, f.srv.URL, tok, http.MethodPost, path, body, want...)
			}
			o, err := post(supA, "/v1/fieldops/orders", map[string]any{"htx_ref": htx.String(), "window_from": now.Format("2006-01-02"),
				"window_to": now.AddDate(0, 0, 5).Format("2006-01-02"), "plot_refs": []string{plot.String()}}, http.StatusCreated)
			if err != nil {
				return err
			}
			order, _ := o["order"].(map[string]any)
			id, _ := order["order_id"].(string)
			if _, err := post(supB, "/v1/fieldops/orders/"+id+"/decision", map[string]any{"decision": "approved"}, http.StatusCreated); err != nil {
				return err
			}
			// Sổ tổ gom (FM-06/07): mỗi vòng một tổ mới (mã theo vòng), thành viên là tài khoản tổ gom.
			c, err := post(supA, "/v1/fieldops/crews", map[string]any{"code": fmt.Sprintf("TO-%d-%d", i, r), "name": "Tổ gom thử",
				"member_refs": []string{colActor.String()}, "capacity_unit": "bales", "capacity_per_day": "5000"}, http.StatusCreated)
			if err != nil {
				return err
			}
			cm, _ := c["crew"].(map[string]any)
			crew, _ := cm["crew_id"].(string)
			if _, err := post(supA, "/v1/fieldops/orders/"+id+"/assign", map[string]any{"crew_ref": crew, "expected_bales": 120}, http.StatusCreated); err != nil {
				return err
			}
			if _, err := post(col, "/v1/fieldops/orders/"+id+"/crew-shifts", map[string]any{"crew_ref": crew, "kind": "opened", "headcount": 6,
				"equipment_refs": []string{"ghe-" + fmt.Sprint(i)}, "event_time": ts(-10 * time.Minute)}, http.StatusCreated); err != nil {
				return err
			}
			for _, stage := range []string{port.StageBale, port.StageGather, port.StageBoatLoad} {
				if _, err := post(col, "/v1/fieldops/orders/"+id+"/roll-counts", map[string]any{"plot_ref": plot.String(), "stage": stage,
					"rolls": 120, "event_time": ts(-5 * time.Minute)}, http.StatusCreated); err != nil {
					return err
				}
			}
			if _, err := post(supA, "/v1/fieldops/orders/"+id+"/htx-confirmations", map[string]any{"plot_ref": plot.String(), "event_time": ts(-time.Minute),
				"outcome": "confirmed", "statement_revision": 1}, http.StatusCreated); err != nil {
				return err
			}
			// Consumer mkg.measurement.weigh_ticket.recorded (giao dịch inbox của vai fieldops_app).
			orderID, err := uuid.Parse(id)
			if err != nil {
				return err
			}
			raw, err := json.Marshal(mport.WeighTicketRecordedV1{TicketID: ids.New(), LotRef: orderID, Point: mport.PointHubIn, Route: mport.RouteHub,
				Source: mport.SourceEdge, MassKg: mass.MustKg("2400"), EventTime: now.Add(-time.Minute)})
			if err != nil {
				return err
			}
			env := outbox.Envelope{EventID: ids.New(), EventType: mport.EventWeighTicketRecorded, SchemaVersion: 1, TenantID: tid, OccurredAt: now,
				Producer: "mkg/measurement", Payload: raw}
			return tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error { return f.svc.HandleWeighTicket(ctx, tx, env) })
		}
	}
	lkRun(t, ctx, f.env.AdminPool(t, migrations.MKG), pools, tenants, rounds, prepare, []string{
		"mkg_fieldops.collection_order_event", "mkg_fieldops.collection_order_current", "mkg_fieldops.crew_shift",
		"mkg_fieldops.roll_total_current", "mkg_fieldops.htx_confirmation", "mkg_fieldops.buyside_reconciliation_current",
		"mkg_fieldops.weighed_loading_current", "mkg_fieldops.valve_export", "mkg_fieldops.roll_count",
		"mkg_fieldops.crew_event", "mkg_fieldops.crew_current", "mkg_fieldops.crew_assignment", "mkg_fieldops.crew_assignment_current",
		"mkg_fieldops.roll_count_harvest", "mkg_fieldops.order_plot_harvest_current",
	})
}
