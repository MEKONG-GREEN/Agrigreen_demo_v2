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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ---------------------------------------------------------------------------
// R-32 / R-20 / R-31 (K-02): rò rỉ tenant qua PgBouncer transaction mode cho schema mkg_costing
// (mkg 000017, 000018). Bảng liệt kê từ catalog — bảng mới tự được kiểm. Trợ giúp chép từ
// mkg/measurement (tenant_leak_integration_test.go).
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

// deliverCtx giao sự kiện nội bộ không dùng t (an toàn trong goroutine).
func (f *fixture) deliverCtx(ctx context.Context, tid uuid.UUID, consumer string, h inbox.Handler, eventType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = inbox.Process(ctx, f.pool, consumer, outbox.Envelope{EventID: ids.New(), EventType: eventType, SchemaVersion: 1, TenantID: tid,
		OccurredAt: f.clk.Now(), Producer: "test", Payload: raw}, h)
	return err
}

// K-02 (RB-2, R-32): TenantLeak cho mkg_costing — mỗi tenant song song: consumer (phiếu cân, đếm cuộn,
// chuyến, giao nhận, đối soát), bút toán tay + điều chỉnh, tham số (đặt ≠ duyệt), quyết toán (lập →
// duyệt), doanh thu carbon, đóng kỳ (job) — xen kẽ đọc/ghi qua cùng pool.
func TestCostingTenantLeakParallel(t *testing.T) {
	f := newFixture(t)
	tenants, rounds := lkSize(t, f.env.ViaBouncer)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	pools := []lkPool{{role: "costing_app", pool: f.pool, tables: lkTables(ctx, t, f.pool, "mkg_costing")}}
	code, from, _ := f.prevMonth()
	at := from.Add(96 * time.Hour)
	prepare := func(i int, tid uuid.UUID) func(context.Context, int) error {
		acct := f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{port.RoleCostAccountant}, MFA: true})
		chief := f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{port.RoleChiefAccountant}, MFA: true})
		closer := f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{port.RolePeriodCloser}, MFA: true})
		return func(ctx context.Context, r int) error {
			app := func(tok, method, path string, body any, want ...int) (map[string]any, error) {
				return lkDo(ctx, f.app.URL, tok, method, path, body, want...)
			}
			adm := func(tok, method, path string, body any, want ...int) (map[string]any, error) {
				return lkDo(ctx, f.admin.URL, tok, method, path, body, want...)
			}
			order, lot, htx := ids.New(), ids.New(), ids.New()
			f.contracts.set(htx, ordport.PricingPerTonne, "150000")
			if err := f.deliverCtx(ctx, tid, port.RollCountConsumer, f.cons.RollCount(), fieldport.EventRollCountRecorded, fieldport.RollCountRecordedV1{
				RollCountID: ids.New(), LotRef: order, PlotRef: ids.New(), HTXRef: htx, Rolls: 400 + r, Stage: fieldport.StageBoatLoad, EventTime: at}); err != nil {
				return err
			}
			st, err := app(acct, http.MethodPost, "/v1/costing/settlements", map[string]any{"collection_order_ref": order.String(), "htx_ref": htx.String(),
				"harvest_date": "2026-09-01"}, http.StatusCreated)
			if err != nil {
				return err
			}
			if err := f.deliverCtx(ctx, tid, port.WeighTicketConsumer, f.cons.WeighTicket(), measport.EventWeighTicketRecorded, measport.WeighTicketRecordedV1{
				TicketID: ids.New(), LotRef: lot, Point: measport.PointHubIn, Route: measport.RouteHub, Source: measport.SourceEdge,
				MassKg: mass.MustKg(fmt.Sprintf("%d", 9000+r)), EventTime: at, CollectionOrderRef: &order}); err != nil {
				return err
			}
			if err := f.deliverCtx(ctx, tid, port.TripConsumer, f.cons.Trip(), logport.EventTripCompleted, logport.TripCompletedV1{TripID: ids.New(),
				Mode: logport.ModeRoad, Cargo: []logport.TripCargoV1{{LotRef: lot, MassKg: mass.MustKg("9000")}}, ActualCost: money.MustAmount("450000.00"),
				CostSource: logport.CostByParamRate, EventTime: at}); err != nil {
				return err
			}
			if err := f.deliverCtx(ctx, tid, port.DeliveryConsumer, f.cons.Delivery(), ordport.EventDeliveryAccepted, ordport.DeliveryAcceptedV1{
				DeliveryID: ids.New(), CustomerRef: ids.New(), Lots: []ordport.DeliveryLotV1{{LotRef: lot, MassKg: mass.MustKg("8800")}},
				Amount: money.MustAmount("17600000.00"), EventTime: at}); err != nil {
				return err
			}
			hub, loss := mass.MustKg("9000"), mass.MustKg("90")
			if err := f.deliverCtx(ctx, tid, port.ReconciliationConsumer, f.cons.Reconciliation(), measport.EventReconciliationCompleted,
				measport.ReconciliationCompletedV1{ReconciliationID: ids.New(), LotRef: lot, HubInKg: &hub, AbnormalLossKg: &loss, EventTime: at}); err != nil {
				return err
			}
			id, _ := st["id"].(string)
			if _, err := adm(chief, http.MethodPost, "/v1/costing/settlements/"+id+"/approve", nil, http.StatusCreated); err != nil {
				return err
			}
			e, err := app(acct, http.MethodPost, "/v1/costing/entries", map[string]any{"lot_ref": lot.String(), "category": "handling",
				"amount": "50000.00"}, http.StatusCreated)
			if err != nil {
				return err
			}
			eid, _ := e["id"].(string)
			if _, err := app(acct, http.MethodPost, "/v1/costing/entries/"+eid+"/adjust", map[string]any{"amount": "40000.00",
				"reason": "Biên bản bốc xếp thực tế"}, http.StatusCreated); err != nil {
				return err
			}
			if _, err := app(acct, http.MethodPost, "/v1/costing/carbon-revenue", map[string]any{"amount": "1000.00"}, http.StatusCreated); err != nil {
				return err
			}
			if r != 0 {
				return nil
			}
			if _, err := adm(acct, http.MethodPost, "/v1/costing/params", map[string]any{"key": port.ParamSettlementTermDays, "value": "30",
				"reason": "Hạn thanh toán theo hợp đồng khung"}, http.StatusCreated); err != nil {
				return err
			}
			if _, err := adm(chief, http.MethodPost, "/v1/costing/params/"+port.ParamSettlementTermDays+"/versions/1/decision",
				map[string]any{"decision": "approved"}, http.StatusCreated); err != nil {
				return err
			}
			p, err := app(closer, http.MethodGet, "/v1/costing/periods", nil, http.StatusOK)
			if err != nil {
				return err
			}
			var pid string
			items, _ := p["items"].([]any)
			for _, it := range items {
				if m, _ := it.(map[string]any); m["code"] == code {
					pid, _ = m["id"].(string)
				}
			}
			j, err := app(closer, http.MethodPost, "/v1/costing/periods/"+pid+"/close", nil, http.StatusAccepted)
			if err != nil {
				return err
			}
			jid, _ := j["job_id"].(string)
			return f.deliverCtx(ctx, tid, port.PeriodCloseConsumer, f.cons.PeriodClose(), port.EventPeriodCloseRequested,
				port.PeriodCloseRequestedV1{JobID: uuid.MustParse(jid), PeriodID: uuid.MustParse(pid), RequestedAt: f.clk.Now()})
		}
	}
	lkRun(t, ctx, f.env.AdminPool(t, migrations.MKG), pools, tenants, rounds, prepare, []string{
		"mkg_costing.param_version", "mkg_costing.param_decision", "mkg_costing.input_event", "mkg_costing.collection_current",
		"mkg_costing.cost_entry", "mkg_costing.lot_cost_current", "mkg_costing.conversion_result", "mkg_costing.conversion_current",
		"mkg_costing.allocation_result", "mkg_costing.allocation_current", "mkg_costing.period_event", "mkg_costing.period_current",
		"mkg_costing.close_job", "mkg_costing.period_summary", "mkg_costing.settlement_event", "mkg_costing.settlement_current",
		"mkg_costing.carbon_revenue_event",
	})
}
