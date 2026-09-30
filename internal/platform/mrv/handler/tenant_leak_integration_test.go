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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ---------------------------------------------------------------------------
// R-32 / R-20 / R-31: rò rỉ tenant qua PgBouncer transaction mode cho schema pf_mrv
// (G3: pf 000038; các bảng G1/G2 của schema cũng được kiểm). Bảng được LIỆT KÊ từ
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

// R-32 (+ R-20, R-31): TenantLeak cho pf_mrv. Mỗi tenant duyệt danh mục mẫu và phương
// pháp (hai người), rồi mỗi vòng: mở vụ + nhật ký HTX (inbox liên kết nhật ký), van bằng
// chứng thu gom MKG, minh chứng rơm + duyệt, minh chứng AWD + đối chiếu, tính lại ước
// tính — song song, xen kẽ đọc/ghi trên pool mrv_app.
func TestMRVTenantLeakParallel(t *testing.T) {
	f := newFixture(t)
	env := pgtest.Get(t)
	tenants, rounds := lkSize(t, env.ViaBouncer)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	const schema = "pf_mrv"
	pools := []lkPool{{role: mrv.DBRole, pool: f.pool, tables: lkTables(ctx, t, f.pool, schema)}}
	t.Logf("%s: %d bảng: %v", schema, len(pools[0].tables), pools[0].tables)
	now := time.Now()
	farmLog := func(ctx context.Context, tid uuid.UUID, typ string, payload any) error {
		_, err := inbox.Process(ctx, f.pool, port.FarmLogLinkConsumer, envelope(tid, typ, payload), f.mod.FarmLogLinkHandler())
		return err
	}

	prepare := func(_ int, tid uuid.UUID) func(context.Context, int) error {
		setter := f.as(tid, uuid.Nil, authz.RoleParamSetter).token
		approver := f.as(tid, uuid.Nil, authz.RoleParamApprover).token
		col := f.as(tid, f.t30001, port.RoleCollector).token
		rev := f.as(tid, f.p89, port.RoleReviewer).token
		return func(ctx context.Context, r int) error {
			post := func(base, tok, path string, body any, want ...int) (map[string]any, error) {
				return lkDo(ctx, base, tok, http.MethodPost, path, body, want...)
			}
			if r == 0 {
				seed, err := post(f.admin.URL, setter, "/v1/mrv/catalog/seed", nil, http.StatusOK)
				if err != nil {
					return err
				}
				items, _ := seed["items"].([]any)
				for _, it := range items {
					v, _ := it.(map[string]any)["version"].(map[string]any)
					if v == nil {
						continue
					}
					id, _ := v["id"].(string)
					if _, err := post(f.admin.URL, setter, "/v1/mrv/catalog/versions/"+id+"/submit", nil, http.StatusOK); err != nil {
						return err
					}
					if _, err := post(f.admin.URL, approver, "/v1/mrv/catalog/versions/"+id+"/approve", map[string]any{"note": "Duyệt tạm"}, http.StatusOK); err != nil {
						return err
					}
				}
				m, err := post(f.admin.URL, setter, "/v1/mrv/methods", map[string]any{"rule_version": port.RuleEstimate,
					"params": []any{map[string]any{"key": port.ParamCO2OpenBurning, "version": 1}}, "source_note": "Phương pháp tạm (kiểm thử rò rỉ)"},
					http.StatusCreated)
				if err != nil {
					return err
				}
				mid, _ := m["id"].(string)
				if _, err := post(f.admin.URL, approver, "/v1/mrv/methods/"+mid+"/approve", map[string]any{"note": "Duyệt"}, http.StatusOK); err != nil {
					return err
				}
			}
			s := g3Season{ref: ids.New(), plot: ids.New(), unit: "30001"}
			opened := now.Add(-72 * time.Hour)
			if err := farmLog(ctx, tid, port.EventSeasonOpened, map[string]any{
				"season_ref": s.ref, "event_id": ids.New(), "plot_ref": s.plot, "commune_code": s.unit, "season_code": "2026-DX",
				"season_year": 2026, "sowing_date": opened.Format("2006-01-02"), "actor_ref": ids.New(), "event_time": opened.Format(time.RFC3339),
			}); err != nil {
				return fmt.Errorf("mở vụ: %w", err)
			}
			logRef := ids.New()
			if err := farmLog(ctx, tid, port.EventFarmLogRecorded, logRecorded(s, logRef, ids.New(), 1, "tuoi_nuoc", now.Add(-26*time.Hour))); err != nil {
				return fmt.Errorf("nhật ký: %w", err)
			}
			mkgT := ids.New() // ADR-0020: phong bì van của tenant MKG riêng, ghi vào tenant bên cấp tid
			f.share.Grant(tid, mkgT, partyport.KindValveMRVEvidence)
			if _, err := inbox.Process(ctx, f.pool, port.CollectionEvidenceConsumer, envelope(mkgT, port.EventMKGCollectionEvid,
				collection(s, ids.New(), 1, "2000-5000")), f.mod.CollectionEvidenceHandler()); err != nil {
				return fmt.Errorf("van thu gom: %w", err)
			}
			ev, err := post(f.app.URL, col, "/v1/mrv/evidence", straw(s, now.Add(-time.Hour)), http.StatusCreated)
			if err != nil {
				return err
			}
			evID, _ := ev["id"].(string)
			if _, err := post(f.app.URL, rev, "/v1/mrv/evidence/"+evID+"/reviews", map[string]any{"decision": "accepted", "reason": "Đạt chuẩn"},
				http.StatusCreated); err != nil {
				return err
			}
			body := awd(s.unit)
			body["plot_ref"] = s.plot.String()
			aw, err := post(f.app.URL, col, "/v1/mrv/evidence", body, http.StatusCreated)
			if err != nil {
				return err
			}
			awID, _ := aw["id"].(string)
			if _, err := post(f.app.URL, col, "/v1/mrv/comparisons", map[string]any{"season_ref": s.ref, "log_ref": logRef, "evidence_id": awID,
				"result": "match"}, http.StatusCreated); err != nil {
				return err
			}
			_, err = post(f.app.URL, rev, "/v1/mrv/seasons/"+s.ref.String()+"/estimate/recompute", nil, http.StatusOK)
			return err
		}
	}
	lkRun(t, ctx, env.AdminPool(t, migrations.PF), pools, tenants, rounds, prepare, []string{
		"pf_mrv.season_link", "pf_mrv.farm_log_link", "pf_mrv.collection_evidence", "pf_mrv.log_comparison",
		"pf_mrv.method_version", "pf_mrv.estimate",
	})
}
