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

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ---------------------------------------------------------------------------
// R-32 / R-20 / R-31: rò rỉ tenant qua PgBouncer transaction mode cho schema mkg_hub
// (G3: mkg 000009; các bảng G1/G2 của schema cũng được kiểm). Bảng được LIỆT KÊ từ
// catalog (mọi bảng có cột tenant_id, trừ COMMENT 'ag:no-rls') nên bảng mới tự được kiểm.
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

// R-32 (+ R-20, R-31): TenantLeak cho mkg_hub. Mỗi tenant dựng hai cơ sở (hai người),
// duyệt tham số cân, rồi mỗi vòng: nhận lô, consumer phiếu cân / đối soát / đếm cuộn
// (inbox, vai hub_app), chuyển kho A → B và nhận ở B, kiểm kê + duyệt — song song, xen
// kẽ đọc/ghi trên cùng pool hub_app.
func TestHubTenantLeakParallel(t *testing.T) {
	f := newFixture(t)
	tenants, rounds := lkSize(t, f.env.ViaBouncer)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	const schema = "mkg_hub"
	pools := []lkPool{{role: hub.DBRole, pool: f.pool, tables: lkTables(ctx, t, f.pool, schema)}}
	t.Logf("%s: %d bảng: %v", schema, len(pools[0].tables), pools[0].tables)
	now := f.clk.Now()
	ts := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	cons := hub.NewConsumers(f.clk)
	deliver := func(ctx context.Context, tid uuid.UUID, consumer string, h inbox.Handler, typ string, payload any) error {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		env := outbox.Envelope{EventID: ids.New(), EventType: typ, SchemaVersion: 1, TenantID: tid, OccurredAt: now, Producer: "mkg/measurement", Payload: raw}
		_, err = inbox.Process(ctx, f.pool, consumer, env, h)
		return err
	}
	kg := func(s string) *mass.Kg { k := mass.MustKg(s); return &k }

	prepare := func(i int, tid uuid.UUID) func(context.Context, int) error {
		roles := []string{port.RoleHubOperator, port.RoleHubApprover}
		a := f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: roles, MFA: true})
		b := f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: roles, MFA: true})
		var facA, facB string
		return func(ctx context.Context, r int) error {
			app := func(tok, path string, body any, want ...int) (map[string]any, error) {
				return lkDo(ctx, f.app.URL, tok, http.MethodPost, path, body, want...)
			}
			adm := func(tok, path string, body any, want ...int) (map[string]any, error) {
				return lkDo(ctx, f.admin.URL, tok, http.MethodPost, path, body, want...)
			}
			if r == 0 {
				for k, x := range []*string{&facA, &facB} {
					p, err := adm(a, "/v1/hub/facilities", f.facilityBody(fmt.Sprintf("HUB-LK%02d-%d", i, k), "500000"), http.StatusCreated)
					if err != nil {
						return err
					}
					*x, _ = p["facility_id"].(string)
					if _, err := adm(b, "/v1/hub/facilities/"+*x+"/versions/1/decision", map[string]any{"decision": "approved"}, http.StatusCreated); err != nil {
						return err
					}
				}
				if _, err := adm(a, "/v1/hub/params/weigh/versions", map[string]any{
					"params": map[string]any{"tolerance_pct": "3.00", "tolerance_min_kg": "0", "ticket_required": false, "kg_per_roll_default": "20"},
					"reason": "Tham số cân tạm cho kiểm thử rò rỉ tenant"}, http.StatusCreated); err != nil {
					return err
				}
				if _, err := adm(b, "/v1/hub/params/weigh/versions/1/decision", map[string]any{"decision": "approved"}, http.StatusCreated); err != nil {
					return err
				}
			}
			receive := func(kgs string) (uuid.UUID, error) {
				rc, err := app(a, "/v1/hub/facilities/"+facA+"/receipts", map[string]any{"product": "straw_bale", "mass_kg": kgs, "event_time": ts(-time.Hour),
					"sources": []map[string]any{{"kind": "cooperative", "ref": ids.New().String(), "mass_kg": kgs}}}, http.StatusCreated)
				if err != nil {
					return uuid.Nil, err
				}
				lot, _ := rc["lot"].(map[string]any)
				s, _ := lot["id"].(string)
				return uuid.Parse(s)
			}
			lot, err := receive("1000")
			if err != nil {
				return err
			}
			rv := "weigh.integrate.v1"
			if err := deliver(ctx, tid, port.WeighTicketConsumer, cons.WeighTicket(), measport.EventWeighTicketRecorded, measport.WeighTicketRecordedV1{
				TicketID: ids.New(), LotRef: lot, Point: measport.PointHubIn, Route: measport.RouteHub, Source: measport.SourceEdge,
				MassKg: mass.MustKg("1100"), EventTime: now.Add(-time.Minute), RuleVersion: &rv}); err != nil {
				return fmt.Errorf("phiếu cân: %w", err)
			}
			if err := deliver(ctx, tid, port.ReconciliationConsumer, cons.Reconciliation(), measport.EventReconciliationCompleted, measport.ReconciliationCompletedV1{
				ReconciliationID: ids.New(), LotRef: lot, Route: measport.RouteHub, HubInKg: kg("1100"), HubOutKg: kg("1000"), ExpectedLossKg: kg("60"),
				AbnormalLossKg: kg("40"), Outcome: measport.OutcomeLossAlert, RuleVersion: "measurement.recon.v1", EventTime: now.Add(-time.Hour)}); err != nil {
				return fmt.Errorf("đối soát: %w", err)
			}
			if err := deliver(ctx, tid, port.RollCountConsumer, cons.RollCount(), fieldport.EventRollCountRecorded, fieldport.RollCountRecordedV1{
				RollCountID: ids.New(), LotRef: ids.New(), PlotRef: ids.New(), HTXRef: ids.New(), Rolls: 50, Stage: fieldport.StageBale,
				RuleVersion: "fieldops.v1", EventTime: now}); err != nil {
				return fmt.Errorf("đếm cuộn: %w", err)
			}
			if _, err := receive("400"); err != nil {
				return err
			}
			tr, err := app(a, "/v1/hub/facilities/"+facA+"/transfers", map[string]any{"to_facility_id": facB, "mass_kg": "300"}, http.StatusCreated)
			if err != nil {
				return err
			}
			trID, _ := tr["id"].(string)
			if _, err := app(a, "/v1/hub/facilities/"+facB+"/transfers/"+trID+"/receipt", map[string]any{"mass_kg": "290"}, http.StatusCreated); err != nil {
				return err
			}
			st, err := app(a, "/v1/hub/facilities/"+facA+"/stocktakes", map[string]any{
				"lines": []map[string]any{{"product": "straw_bale", "counted_kg": "100"}}}, http.StatusCreated)
			if err != nil {
				return err
			}
			stID, _ := st["id"].(string)
			_, err = app(b, "/v1/hub/stocktakes/"+stID+"/decision", map[string]any{"decision": "approved"}, http.StatusCreated)
			return err
		}
	}
	lkRun(t, ctx, f.env.AdminPool(t, migrations.MKG), pools, tenants, rounds, prepare, []string{
		"mkg_hub.param_version", "mkg_hub.param_decision", "mkg_hub.param_current",
		"mkg_hub.weigh_link", "mkg_hub.lot_weigh_current",
		"mkg_hub.stocktake", "mkg_hub.stocktake_decision", "mkg_hub.stocktake_current",
		"mkg_hub.transfer_event", "mkg_hub.transfer_current",
		"mkg_hub.lot_loss", "mkg_hub.lot_loss_current", "mkg_hub.expected_inbound",
		"mkg_hub.lot", "mkg_hub.stock_movement",
	})
}
