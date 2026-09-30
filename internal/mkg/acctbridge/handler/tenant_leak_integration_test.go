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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// lkTables liệt kê bảng có tenant_id của schema mà vai của pool đọc được; kiểm ENABLE + FORCE RLS.
func lkTables(ctx context.Context, t *testing.T, pool *pgxpool.Pool, schema string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `
	  SELECT format('%I.%I', n.nspname, c.relname), c.relrowsecurity, c.relforcerowsecurity, c.relispartition, has_table_privilege(c.oid, 'SELECT')
	  FROM pg_class c
	  JOIN pg_namespace n ON n.oid = c.relnamespace
	  JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped
	  WHERE n.nspname = $1::text AND c.relkind IN ('r', 'p') ORDER BY 1`, schema)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		var rls, force, part, sel bool
		if err := rows.Scan(&name, &rls, &force, &part, &sel); err != nil {
			t.Fatal(err)
		}
		if !rls || !force {
			t.Errorf("R-33: %s có tenant_id nhưng thiếu ENABLE/FORCE ROW LEVEL SECURITY", name)
		}
		if !part && sel {
			out = append(out, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func lkCheck(ctx context.Context, tx pgx.Tx, tables []string, tid uuid.UUID) (map[string]int, error) {
	own := map[string]int{}
	for _, tb := range tables {
		var foreign, mine, all int
		if err := tx.QueryRow(ctx, "SELECT count(*) FILTER (WHERE tenant_id <> $1), count(*) FILTER (WHERE tenant_id = $1), count(*) FROM "+tb,
			tid).Scan(&foreign, &mine, &all); err != nil {
			return nil, fmt.Errorf("%s: %w", tb, err)
		}
		if foreign != 0 || all != mine {
			return nil, fmt.Errorf("RÒ RỈ R-32: tenant %s thấy %d dòng của tenant khác trong %s", tid, foreign, tb)
		}
		own[tb] = mine
	}
	return own, nil
}

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
	return nil
}

func lkDo(ctx context.Context, base, token, method, path string, body any, want int) (map[string]any, error) {
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
	dec.UseNumber() // R-10
	_ = dec.Decode(&m)
	if r.StatusCode != want {
		return m, fmt.Errorf("%s %s: HTTP %d (muốn %d): %s", method, path, r.StatusCode, want, raw)
	}
	return m, nil
}

// R-32 (+ R-20, R-31, RB-2): TenantLeak cho mkg_acctbridge — mỗi tenant song song đề xuất/duyệt ánh xạ, nhận
// sự kiện đóng kỳ, dựng bút toán (worker), duyệt, đẩy (bộ giả) — xen kẽ đọc/ghi trên cùng pool acctbridge_app
// (qua PgBouncer transaction mode khi CI đặt AG_TEST_PGBOUNCER_URL).
func TestAcctbridgeTenantLeakParallel(t *testing.T) {
	f := newFixture(t, nil)
	tenants, rounds := 8, 3
	if f.env.ViaBouncer {
		tenants, rounds = 24, 4
	} else {
		t.Log("CẢNH BÁO: chạy thẳng PostgreSQL — CI phải đặt AG_TEST_PGBOUNCER_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	tables := lkTables(ctx, t, f.pool, "mkg_acctbridge")
	t.Logf("mkg_acctbridge: %v", tables)
	tids := make([]uuid.UUID, tenants)
	for i := range tids {
		tids[i] = ids.New()
	}
	sign := func(tid uuid.UUID, role string) string {
		return f.iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: []string{role}, MFA: true})
	}
	var wg sync.WaitGroup
	errs := make(chan error, tenants)
	for i := range tids {
		tid := tids[i]
		op, chief := sign(tid, port.RoleAccountingBridgeOperator), sign(tid, port.RoleChiefAccountant)
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := lkDo(ctx, f.admin.URL, op, http.MethodPost, "/v1/acctbridge/mappings", map[string]any{"reason": "Bảng ánh xạ tạm theo VAS, chờ khách",
				"effective_from": "2026-01-01"}, http.StatusCreated)
			if err != nil {
				errs <- err
				return
			}
			mm, _ := m["mapping"].(map[string]any)
			v, _ := mm["version_id"].(string)
			if _, err := lkDo(ctx, f.admin.URL, chief, http.MethodPost, "/v1/acctbridge/mappings/"+v+"/decision", map[string]any{"decision": "approved"},
				http.StatusCreated); err != nil {
				errs <- err
				return
			}
			for r := 0; r < rounds; r++ {
				period := ids.New()
				s := summary(period, ids.New(), hashOf(fmt.Sprint(r)))
				f.periods.put(tid, s)
				if _, err := f.processClosed(ctx, tid, s, ids.New(), ids.New()); err != nil {
					errs <- err
					return
				}
				if _, err := drainTenant(ctx, f.pool, f.svc, tid); err != nil {
					errs <- err
					return
				}
				l, err := lkDo(ctx, f.srv.URL, op, http.MethodGet, "/v1/acctbridge/journals?period_id="+period.String(), nil, http.StatusOK)
				if err != nil {
					errs <- err
					return
				}
				items, _ := l["items"].([]any)
				if len(items) != 1 {
					errs <- fmt.Errorf("tenant %s kỳ %s: %d bút toán", tid, period, len(items))
					return
				}
				jid, _ := items[0].(map[string]any)["journal_id"].(string)
				if _, err := lkDo(ctx, f.admin.URL, chief, http.MethodPost, "/v1/acctbridge/journals/"+jid+"/approve", nil, http.StatusAccepted); err != nil {
					errs <- err
					return
				}
				if _, err := drainTenant(ctx, f.pool, f.svc, tid); err != nil {
					errs <- err
					return
				}
				if err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error { _, err := lkCheck(ctx, tx, tables, tid); return err }); err != nil {
					errs <- err
					return
				}
				if err := lkNoContext(ctx, f.pool, tables); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	admin := f.env.AdminPool(t, migrations.MKG)
	seen := map[string]int{}
	for _, tid := range tids {
		var own map[string]int
		if err := tenant.WithTenantTx(ctx, f.pool, tid, func(tx pgx.Tx) error {
			var err error
			own, err = lkCheck(ctx, tx, tables, tid)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		for _, tb := range tables {
			var real int
			if err := admin.QueryRow(ctx, "SELECT count(*) FROM "+tb+" WHERE tenant_id = $1", tid).Scan(&real); err != nil {
				t.Fatal(err)
			}
			if real != own[tb] {
				t.Errorf("%s: tenant %s thấy %d dòng, thực có %d", tb, tid, own[tb], real)
			}
			if own[tb] > 0 {
				seen[tb]++
			}
		}
	}
	for _, tb := range tables {
		if seen[tb] == 0 {
			t.Errorf("không tenant nào có dữ liệu trong %s — đường ghi của kiểm thử không phủ bảng này", tb)
		}
	}
}
