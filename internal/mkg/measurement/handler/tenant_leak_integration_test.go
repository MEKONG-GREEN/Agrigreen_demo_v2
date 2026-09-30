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

	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ---------------------------------------------------------------------------
// R-32 / R-20 / R-31: rò rỉ tenant qua PgBouncer transaction mode cho schema
// mkg_measurement (mkg 000007, 000008, 000010). Bảng được LIỆT KÊ từ catalog (mọi
// bảng có cột tenant_id, trừ COMMENT 'ag:no-rls') nên bảng mới tự được kiểm.
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

// R-32 (+ R-20, R-31): TenantLeak cho mkg_measurement. Mỗi tenant đăng ký/duyệt cân,
// đặt dung sai/định mức, mở/đóng phiên cân kèm lệnh gom, ghi phiếu edge qua vai
// ingest_mkg, phiếu tay HUB_OUT/CUSTOMER_IN (đối soát, tranh chấp), điều khoản lô,
// van MRV, nhập số cân khách, liên kết lô ↔ lệnh gom — song song, xen kẽ đọc/ghi.
func TestMeasurementTenantLeakParallel(t *testing.T) {
	f := newFixture(t)
	tenants, rounds := lkSize(t, f.env.ViaBouncer)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	const schema = "mkg_measurement"
	pools := []lkPool{
		{role: "measurement_app", pool: f.pool, tables: lkTables(ctx, t, f.pool, schema)},
		{role: port.IngestRole, pool: f.ingestPool, tables: lkTables(ctx, t, f.ingestPool, schema)},
	}
	t.Logf("%s: %d bảng (measurement_app), %d bảng (%s): %v", schema, len(pools[0].tables), len(pools[1].tables), port.IngestRole, pools[0].tables)
	allRoles := []string{port.RoleScaleSupervisor, port.RoleDeviceApprover, port.RoleReconciliationOfficer, port.RoleDisputeOfficer, port.RoleToleranceApprover}
	now := f.clk.Now()
	ts := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }

	prepare := func(i int, tid uuid.UUID) func(context.Context, int) error {
		// Hai người khác nhau (R-56: người đề xuất ≠ người duyệt), đủ vai.
		a := f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: allRoles, MFA: true})
		b := f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: allRoles, MFA: true})
		dev := newScale(t, fmt.Sprintf("SCALE-LK%02d-IN", i))
		epoch := 0
		return func(ctx context.Context, r int) error {
			app := func(tok, method, path string, body any, want ...int) (map[string]any, error) {
				return lkDo(ctx, f.app.URL, tok, method, path, body, want...)
			}
			adm := func(tok, method, path string, body any, want ...int) (map[string]any, error) {
				return lkDo(ctx, f.admin.URL, tok, method, path, body, want...)
			}
			if r == 0 {
				p, err := adm(a, http.MethodPost, "/v1/measurement/devices", dev.body("HUB_IN"), http.StatusCreated)
				if err != nil {
					return err
				}
				ep, err := p["epoch"].(json.Number).Int64()
				if err != nil {
					return err
				}
				epoch = int(ep)
				if _, err := adm(b, http.MethodPost, fmt.Sprintf("/v1/measurement/devices/%s/epochs/%d/decision", dev.id, epoch),
					map[string]any{"decision": "approved"}, http.StatusCreated); err != nil {
					return err
				}
				// Dung sai chặt (cờ 3 %) để CUSTOMER_IN −8,6 % mở tranh chấp.
				if _, err := adm(a, http.MethodPost, "/v1/measurement/tolerance-versions", map[string]any{"commercial_flag_pct": "3.00",
					"commercial_critical_pct": "20.00", "roll_flag_pct": "5", "roll_critical_pct": "15", "effective_from": ts(-2 * time.Hour),
					"reason": "Biên bản kiểm thử rò rỉ tenant (tạm)"}, http.StatusCreated); err != nil {
					return err
				}
				if _, err := adm(b, http.MethodPost, "/v1/measurement/tolerance-versions/1/decision", map[string]any{"decision": "approved"},
					http.StatusCreated); err != nil {
					return err
				}
				if _, err := adm(a, http.MethodPost, "/v1/measurement/loss-norm-versions", map[string]any{
					"bands":            []map[string]string{{"moisture_from": "0", "moisture_to": "100", "loss_pct": "3"}},
					"default_loss_pct": "3", "abnormal_alert_pct": "1", "effective_from": ts(-2 * time.Hour), "reason": "Định mức sấy tạm chờ khách"},
					http.StatusCreated); err != nil {
					return err
				}
				if _, err := adm(b, http.MethodPost, "/v1/measurement/loss-norm-versions/1/decision",
					map[string]any{"decision": "rejected", "reason": "Chưa có số liệu mẻ sấy thực tế"}, http.StatusCreated); err != nil {
					return err
				}
			}
			lot, order := ids.New(), ids.New()
			from, to := int64(r*100+1), int64(r*100+50)
			s, err := app(a, http.MethodPost, "/v1/measurement/sessions", map[string]any{"lot_ref": lot.String(), "device_id": dev.id,
				"epoch": epoch, "counter_from": from, "collection_order_ref": order.String()}, http.StatusCreated)
			if err != nil {
				return err
			}
			sid, _ := s["id"].(string)
			if _, err := app(a, http.MethodPost, "/v1/measurement/sessions/"+sid+"/close", map[string]any{"counter_to": to}, http.StatusCreated); err != nil {
				return err
			}
			// Phiếu edge qua vai ingest_mkg (như cmd/ingest sau khi tích phân).
			if _, err := f.ingest.Tickets.Record(ctx, tid, port.TicketDraft{
				ID: ids.New(), IdemKey: "ingest:" + dev.id + ":" + itoa(int(from)) + ":" + ids.New().String(), LotRef: lot, Point: port.PointHubIn,
				Route: port.RouteHub, Source: port.SourceEdge, MassKg: mass.MustKg("96800"), MoisturePct: "18.00", EventTime: now.Add(-time.Hour),
				ActorRef: ids.New(),
				Integration: &port.Integration{Window: port.Window{DeviceID: dev.id, Epoch: epoch, CounterFrom: from, CounterTo: to},
					NetKg: mass.MustKg("96800"), ReadingsUsed: int(to - from + 1), ReadingsTotal: int(to - from + 1), BatchIDs: []uuid.UUID{ids.New()},
					RuleVersion: "weigh.integrate.v1"},
			}); err != nil {
				return fmt.Errorf("phiếu edge: %w", err)
			}
			if _, err := app(a, http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", map[string]any{"point": "HUB_OUT", "mass_kg": "93000",
				"weighed_at": ts(-40 * time.Minute), "evidence_ref": ids.New().String(), "reason": "Cân hub hỏng, nhập tay theo biên bản"},
				http.StatusCreated); err != nil {
				return err
			}
			c, err := app(a, http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", map[string]any{"point": "CUSTOMER_IN", "mass_kg": "85000",
				"weighed_at": ts(-20 * time.Minute), "evidence_ref": ids.New().String(), "customer_ref": ids.New().String(),
				"reason": "Phiếu cân nhà máy khách (C-5)"}, http.StatusCreated)
			if err != nil {
				return err
			}
			if rec, _ := c["reconciliation"].(map[string]any); rec != nil {
				if did, _ := rec["dispute_id"].(string); did != "" {
					if _, err := app(b, http.MethodPost, "/v1/measurement/disputes/"+did+"/actions", map[string]any{"action": "add_evidence",
						"evidence_refs": []string{ids.New().String()}}, http.StatusCreated); err != nil {
						return err
					}
				}
			}
			if _, err := app(a, http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/terms", map[string]any{"pricing_basis": "per_bale"},
				http.StatusCreated); err != nil {
				return err
			}
			f.lots.mu.Lock()
			f.lots.m[lot] = hubport.LotInfo{LotID: lot, Sources: []hubport.LotSource{{Kind: "plot", Ref: ids.New(), Ratio: "1.000000"}}}
			f.lots.mu.Unlock()
			if _, err := app(a, http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/mrv-evidence", map[string]any{}, http.StatusCreated); err != nil {
				return err
			}
			direct := ids.New()
			if _, err := app(a, http.MethodPost, "/v1/measurement/customer-weight-imports", map[string]any{"channel": "file", "evidence_ref": ids.New().String(),
				"customer_ref": ids.New().String(), "rows": []map[string]any{{"lot_ref": direct.String(), "route": "DIRECT", "mass_kg": "84200",
					"weighed_at": ts(-30 * time.Minute), "customer_ticket_no": fmt.Sprintf("NM-%02d-%d", i, r)}}}, http.StatusCreated); err != nil {
				return err
			}
			if _, err := app(a, http.MethodPost, "/v1/measurement/lots/"+direct.String()+"/collection", map[string]any{"collection_order_ref": ids.New().String()},
				http.StatusCreated); err != nil {
				return err
			}
			return nil
		}
	}
	lkRun(t, ctx, f.env.AdminPool(t, migrations.MKG), pools, tenants, rounds, prepare, []string{
		"mkg_measurement.device_version", "mkg_measurement.device_decision", "mkg_measurement.device_current",
		"mkg_measurement.weighing_session", "mkg_measurement.weighing_session_current",
		"mkg_measurement.weigh_ticket", "mkg_measurement.lot_mass_current",
		"mkg_measurement.lot_terms", "mkg_measurement.lot_terms_current",
		"mkg_measurement.tolerance_version", "mkg_measurement.loss_norm_version", "mkg_measurement.param_decision", "mkg_measurement.param_current",
		"mkg_measurement.reconciliation", "mkg_measurement.lot_yield_current",
		"mkg_measurement.dispute_event", "mkg_measurement.dispute_current",
		"mkg_measurement.customer_weight_import", "mkg_measurement.valve_export",
		"mkg_measurement.lot_collection", "mkg_measurement.lot_collection_current",
	})
}
