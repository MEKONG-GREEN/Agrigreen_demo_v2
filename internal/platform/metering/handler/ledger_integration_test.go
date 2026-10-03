package handler_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func codeOf(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

var oct = time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC)

// ADR-0023 mục 1–2 (A-2, M-01, M-03): mỗi lời gọi đã xác thực một dòng usage_event ở tenant đơn vị vận
// hành (kể cả 4xx sau xác thực), đơn vị metering.units.v1 ghim rule_version; lời gọi phục vụ dữ liệu ghi
// thêm egress_event kênh public_api ở tenant CHỦ DỮ LIỆU cùng giao dịch (L-02); Usage chỉ của khách hàng.
func TestRecordOneRowPerCall_EgressInDataTenant_ADR0023(t *testing.T) {
	f := newFixture(t, port.ModeObserve)
	ctx := context.Background()
	client, clientTenant, dataTenant := ids.New(), ids.New(), ids.New()
	l := f.mod.Ledger()

	ok := call(client, clientTenant, dataTenant, oct)
	rec, err := l.Record(ctx, f.op, ok)
	if err != nil || rec.Units != 1 || rec.RuleVersion != port.RuleUnitsV1 || rec.DailyRemaining != apiclientport.DefaultDailyUnits-1 {
		t.Fatalf("đọc 200: %+v %v", rec, err)
	}
	forbidden := call(client, clientTenant, uuid.Nil, oct.Add(time.Minute))
	forbidden.Status, forbidden.ProblemCode, forbidden.Rows, forbidden.Bytes = 403, "apiclient.forbidden", 0, 0
	if rec, err := l.Record(ctx, f.op, forbidden); err != nil || rec.Units != 0 {
		t.Fatalf("403 sau xác thực phải ghi sổ với 0 đơn vị: %+v %v", rec, err)
	}
	dl := call(client, clientTenant, dataTenant, oct.Add(2*time.Minute))
	dl.Kind, dl.Rows, dl.RouteTemplate = port.CallFileDownload, 2500, "GET /v1/jobs/{id}/file"
	if rec, err := l.Record(ctx, f.op, dl); err != nil || rec.Units != 3 {
		t.Fatalf("tải 2 500 dòng = 3 đơn vị: %+v %v", rec, err)
	}

	if n := f.count(f.op, `SELECT count(*) FROM pf_metering.usage_event`); n != 3 {
		t.Fatalf("muốn 3 dòng sổ sử dụng, có %d", n)
	}
	if n := f.count(f.op, `SELECT count(*) FROM pf_metering.usage_event WHERE rule_version = $1 AND route_template NOT LIKE '%?%'`, port.RuleUnitsV1); n != 3 {
		t.Fatal("rule_version phải ghim metering.units.v1, route là MẪU")
	}
	if n := f.count(f.op, `SELECT count(*) FROM pf_metering.egress_event`); n != 0 {
		t.Fatal("tenant đơn vị vận hành không được thấy dòng egress của chủ dữ liệu")
	}
	if n := f.count(dataTenant, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = 'public_api' AND counterparty_ref = $1`, client); n != 2 {
		t.Fatalf("muốn 2 dòng egress ở tenant chủ dữ liệu, có %d", n)
	}
	if n := f.count(dataTenant, `SELECT count(*) FROM pf_metering.usage_event`); n != 0 {
		t.Fatal("tenant chủ dữ liệu không thấy sổ sử dụng của đơn vị vận hành")
	}
	days, err := f.mod.Usage().UsageOf(ctx, f.op, client, oct, oct)
	if err != nil || len(days) != 1 || days[0].Calls != 3 || days[0].Units != 4 || days[0].Date != "2026-10-15" {
		t.Fatalf("UsageOf: %+v %v", days, err)
	}
	if other, _ := f.mod.Usage().UsageOf(ctx, f.op, ids.New(), oct, oct); len(other) != 0 {
		t.Fatal("UsageOf chỉ của chính khách hàng (M-05)")
	}
	rem, err := f.mod.Quotas().Remaining(ctx, f.op, client, "collection_volume", oct)
	if err != nil || rem.DailyUnits != apiclientport.DefaultDailyUnits-4 || rem.MonthlyUnits != apiclientport.DefaultMonthlyUnits-4 {
		t.Fatalf("Remaining: %+v %v", rem, err)
	}
}

// R-51 / ADR-0023 mục 2 (P-13): gọi lặp idempotent → 0 đơn vị; cùng request_id ghi hai lần → một dòng,
// trả lại kết quả lần đầu.
func TestRecordIdempotentReplayZeroUnits_ADR0023(t *testing.T) {
	f := newFixture(t, port.ModeObserve)
	ctx := context.Background()
	client, ct, dt := ids.New(), ids.New(), ids.New()
	c := call(client, ct, dt, oct)
	r1, err := f.mod.Ledger().Record(ctx, f.op, c)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := f.mod.Ledger().Record(ctx, f.op, c)
	if err != nil || r2.EventID != r1.EventID || r2.Units != 1 {
		t.Fatalf("cùng request_id phải trả dòng cũ: %+v %+v %v", r1, r2, err)
	}
	replay := call(client, ct, dt, oct.Add(time.Second))
	replay.Replayed = true
	if r3, err := f.mod.Ledger().Record(ctx, f.op, replay); err != nil || r3.Units != 0 {
		t.Fatalf("gọi lặp Idempotency-Key = 0 đơn vị: %+v %v", r3, err)
	}
	if n := f.count(f.op, `SELECT count(*) FROM pf_metering.usage_event`); n != 2 {
		t.Fatalf("muốn 2 dòng, có %d", n)
	}
	if n := f.count(f.op, `SELECT units FROM pf_metering.usage_counter_current WHERE product = '*' AND window_kind = 'day'`); n != 1 {
		t.Fatalf("bộ đếm ngày = 1 đơn vị, có %d", n)
	}
	if n := f.count(dt, `SELECT count(*) FROM pf_metering.egress_event`); n != 2 {
		t.Fatalf("egress: một dòng mỗi request_id (lặp request_id không thêm), có %d", n)
	}
}

// ADR-0023 mục 3 (P-10, M-04): hạn mức gói chính xác dưới song song — N lời gọi đồng thời không bao giờ
// vượt daily_units; phần vượt vẫn ghi sổ với status 429, units 0, trả metering.quota_exceeded.
func TestQuotaExactUnderConcurrency_ADR0023(t *testing.T) {
	f := newFixture(t, port.ModeObserve)
	ctx := context.Background()
	client, ct, dt := ids.New(), ids.New(), ids.New()
	const quota, n = 25, 60
	f.plans.plans[client] = apiclientport.Plan{ClientID: client, DailyUnits: quota, MonthlyUnits: 1000}
	var wg sync.WaitGroup
	var mu sync.Mutex
	okN, denied := 0, 0
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec, err := f.mod.Ledger().Record(ctx, f.op, call(client, ct, dt, oct.Add(time.Duration(i)*time.Second)))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil && rec.Units == 1:
				okN++
			case codeOf(err) == "metering.quota_exceeded" && rec.Units == 0:
				denied++
			default:
				errs <- fmt.Errorf("kết quả lạ: %+v %w", rec, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if okN != quota || denied != n-quota {
		t.Fatalf("muốn %d được / %d bị từ chối, có %d / %d", quota, n-quota, okN, denied)
	}
	if got := f.count(f.op, `SELECT units FROM pf_metering.usage_counter_current WHERE product = '*' AND window_kind = 'day'`); got != quota {
		t.Fatalf("bộ đếm = %d", got)
	}
	if got := f.count(f.op, `SELECT count(*) FROM pf_metering.usage_event WHERE status = 429 AND units = 0 AND problem_code = 'metering.quota_exceeded'`); got != n-quota {
		t.Fatalf("dòng 429 = %d", got)
	}
	if got := f.count(dt, `SELECT count(*) FROM pf_metering.egress_event`); got != quota {
		t.Fatalf("chỉ lời gọi được phục vụ mới ghi egress: %d", got)
	}
	rem, err := f.mod.Quotas().Remaining(ctx, f.op, client, "collection_volume", oct)
	if err != nil || rem.DailyUnits != 0 {
		t.Fatalf("Remaining sau khi hết: %+v %v", rem, err)
	}
	if f.mod.Metrics().Count("quota_exceeded") != n-quota {
		t.Fatalf("metric quota_exceeded = %d", f.mod.Metrics().Count("quota_exceeded"))
	}
}

type brokenDB struct{}

func (brokenDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("db down")
}

// ADR-0023 mục 1 (M-02): ghi sổ lỗi → 503 metering.unavailable (api-public không gửi byte nào).
func TestRecordFailsClosed_ADR0023(t *testing.T) {
	m := metering.New(brokenDB{}, clock.NewFixed(oct), metering.Deps{})
	_, err := m.Ledger().Record(context.Background(), ids.New(), call(ids.New(), ids.New(), ids.New(), oct))
	if codeOf(err) != "metering.unavailable" {
		t.Fatalf("muốn metering.unavailable, nhận %v", err)
	}
	if m.Metrics().Count("ledger_errors") != 1 {
		t.Fatal("thiếu metric ag_public_ledger_errors")
	}
	bad := call(ids.New(), ids.New(), ids.New(), oct)
	bad.RequestID = uuid.Nil
	if _, err := m.Ledger().Record(context.Background(), ids.New(), bad); codeOf(err) != "metering.unavailable" {
		t.Fatalf("lời gọi thiếu request_id phải đóng khi lỗi: %v", err)
	}
}

// RB-1 / R-20 (X-01, M-10, L-01): sổ usage_event, egress_event, statement có chuỗi băm liền; mọi bảng chỉ
// ghi thêm của pf_metering chặn UPDATE / DELETE / TRUNCATE bằng vai metering_app VÀ owner.
func TestMeteringLedgersAppendOnlySixWays_R20(t *testing.T) {
	f := newFixture(t, port.ModeObserve)
	ctx := context.Background()
	client, ct := ids.New(), ids.New()
	for i := 0; i < 3; i++ {
		if _, err := f.mod.Ledger().Record(ctx, f.op, call(client, ct, f.op, oct.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	// Bất thường + bảng kê để mọi bảng chỉ ghi thêm có dữ liệu.
	f.params.set(f.op, port.EgressParam(port.ParamEgressAnomalyRecordsFmt, port.ChannelPublicAPI), "1")
	if _, err := f.mod.Ledger().Record(ctx, f.op, call(client, ct, f.op, oct.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	st := f.draftAndApprove(client, "2026-10")
	_ = st
	owner := f.env.Pool(t, migrations.PF, dbadmin.Migrator)
	tables := []string{"usage_event", "egress_event", "statement", "egress_anomaly", "statement_snapshot", "statement_line"}
	for _, who := range []struct {
		name string
		pool *pgxpool.Pool
	}{{port.DBRole, f.pool}, {"owner ag_migrator", owner}} {
		for _, tb := range tables {
			for op, sql := range map[string]string{
				"UPDATE":   "UPDATE pf_metering." + tb + " SET tenant_id = tenant_id",
				"DELETE":   "DELETE FROM pf_metering." + tb,
				"TRUNCATE": "TRUNCATE pf_metering." + tb,
			} {
				err := tenant.WithTenantTx(ctx, who.pool, f.op, func(tx pgx.Tx) error {
					var n int
					if err := tx.QueryRow(ctx, "SELECT count(*) FROM pf_metering."+tb).Scan(&n); err != nil {
						return err
					}
					if n == 0 {
						return fmt.Errorf("%s rỗng — luồng không phủ", tb)
					}
					_, err := tx.Exec(ctx, sql)
					return err
				})
				if !errors.Is(ledger.Classify(err), ledger.ErrImmutable) {
					t.Errorf("%s %s bằng %s phải bị chặn (42501), nhận %v", op, tb, who.name, err)
				}
			}
		}
	}
	f.in(f.op, func(tx pgx.Tx) error {
		for _, l := range []struct{ table, stream string }{
			{"pf_metering.usage_event", "client:" + client.String() + ":2026-10"},
			{"pf_metering.egress_event", "egress:public_api:2026-10-15"},
			{"pf_metering.statement", "statement:" + client.String() + ":2026-10"},
		} {
			r, err := ledger.Verify(ctx, tx, l.table, f.op, l.stream)
			if err != nil || !r.OK || r.RowsChecked < 2 {
				t.Fatalf("verify_chain %s: %+v %v", l.table, r, err)
			}
		}
		return nil
	})
	var missing int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM shared.v_missing_immutability WHERE schema_name = 'pf_metering'`).Scan(&missing); err != nil || missing != 0 {
		t.Fatalf("v_missing_immutability pf_metering: %d %v", missing, err)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM shared.v_missing_rls WHERE schema_name = 'pf_metering'`).Scan(&missing); err != nil || missing != 0 {
		t.Fatalf("v_missing_rls pf_metering: %d %v", missing, err)
	}
}

// RB-2 / R-32 (X-02): cách ly tenant khi nhiều tenant đơn vị vận hành / chủ dữ liệu ghi song song trên
// cùng pool metering_app (qua PgBouncer nếu có) — mỗi tenant chỉ thấy dòng của mình.
func TestMeteringTenantIsolationParallel_RB2(t *testing.T) {
	f := newFixture(t, port.ModeObserve)
	ctx := context.Background()
	const tenants, rounds = 6, 8
	ops, data := make([]uuid.UUID, tenants), make([]uuid.UUID, tenants)
	var wg sync.WaitGroup
	errs := make(chan error, tenants*rounds*2)
	for i := 0; i < tenants; i++ {
		ops[i], data[i] = ids.New(), ids.New()
		wg.Add(1)
		go func(op, dt uuid.UUID) {
			defer wg.Done()
			client := ids.New()
			for r := 0; r < rounds; r++ {
				if _, err := f.mod.Ledger().Record(ctx, op, call(client, ids.New(), dt, oct.Add(time.Duration(r)*time.Minute))); err != nil {
					errs <- err
				}
				if _, err := f.mod.Checkpoint().Pass(ctx, port.Egress{SourceID: ids.New(), Channel: port.ChannelReportExport, DataTenantID: dt,
					CounterpartyKind: port.CounterpartyActor, CounterpartyRef: ids.New(), Product: "t", Records: 5, OccurredAt: oct}); err != nil {
					errs <- err
				}
			}
		}(ops[i], data[i])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	for i := 0; i < tenants; i++ {
		if n := f.count(ops[i], `SELECT count(*) FROM pf_metering.usage_event`); n != rounds {
			t.Fatalf("tenant vận hành %d thấy %d dòng sổ sử dụng", i, n)
		}
		if n := f.count(ops[i], `SELECT count(*) FROM pf_metering.usage_counter_current WHERE product = '*'`); n != 2 {
			t.Fatalf("tenant vận hành %d thấy %d dòng bộ đếm tổng", i, n)
		}
		if n := f.count(data[i], `SELECT count(*) FROM pf_metering.egress_event`); n != 2*rounds {
			t.Fatalf("tenant dữ liệu %d thấy %d dòng egress", i, n)
		}
	}
	// Không ngữ cảnh tenant → không thấy gì (R-31).
	var n int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM pf_metering.usage_event`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("không ngữ cảnh tenant thấy %d dòng (%v)", n, err)
	}
	// Admin API: metering_viewer của tenant dữ liệu A không thấy dòng của B (L-11).
	r := f.as(data[0], port.RoleMeteringViewer).do("GET", "/v1/metering/egress?limit=200", nil, "")
	want(t, r, 200, "", "sổ xuất tenant A")
	if items, _ := r.body["items"].([]any); len(items) != 2*rounds {
		t.Fatalf("viewer A thấy %d dòng", len(items))
	}
	want(t, f.as(data[0], port.RoleBillingOfficer).do("GET", "/v1/metering/egress", nil, ""), 403, "forbidden", "billing_officer không đọc sổ xuất")
}
