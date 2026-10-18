package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/store"
	wfport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// sqlApprovals là cap/workflow tối thiểu cho kiểm thử (module không import cài đặt của pf/workflow): gọi thẳng hàm DB
// pf_workflow.submit/decide trong giao dịch của pf/metrics — kiểm luôn quyền EXECUTE của r_metrics (pf 000129) và
// CHECK decision_four_eyes. Lắp ráp thật dùng workflow.Module.Approvals.
type sqlApprovals struct{}

func (sqlApprovals) Submit(ctx context.Context, in wfport.SubmitInput) (wfport.Request, error) {
	tx, ok := db.JoinedTx(ctx)
	if !ok {
		return wfport.Request{}, wfport.ErrNoCallerTx
	}
	r := wfport.Request{Kind: in.Kind, Subject: in.Subject, Requester: in.Requester}
	err := tx.QueryRow(ctx, `SELECT o_request_id, o_seq, o_replayed FROM pf_workflow.submit($1, $2, $3, $4, NULL, $4, $5, $6, 'test')`,
		ids.New(), in.Kind, in.Subject, in.Requester, in.Summary, in.At).Scan(&r.ID, &r.Seq, &r.Replayed)
	return r, err
}

func (sqlApprovals) Decide(ctx context.Context, in wfport.DecideInput) (wfport.Decision, error) {
	tx, ok := db.JoinedTx(ctx)
	if !ok {
		return wfport.Decision{}, wfport.ErrNoCallerTx
	}
	var note *string
	if in.Note != "" {
		note = &in.Note
	}
	d := wfport.Decision{Outcome: in.Outcome, Decider: in.Decider}
	err := tx.QueryRow(ctx, `SELECT o_decision_id, o_request_id, o_replayed FROM pf_workflow.decide($1, $2, $3, $4, $5, NULL, $6, $7, $8, $9, 'test')`,
		ids.New(), ids.New(), in.Kind, in.Subject, in.Requester, in.Decider, string(in.Outcome), note, in.At).Scan(&d.ID, &d.RequestID, &d.Replayed)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23514" {
		return d, problem.SelfApproval.WithCause(err)
	}
	return d, err
}

type aggFx struct {
	t     *testing.T
	ctx   context.Context
	env   *pgtest.Env
	agg   *Agg
	gov   uuid.UUID
	alice uuid.UUID // người đề xuất
	bob   uuid.UUID // người duyệt
}

func newAggFx(t *testing.T) *aggFx {
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC))
	gov := ids.New()
	pool := env.Pool(t, migrations.PF, dbadmin.AppWorker)
	a := &Agg{DB: db.ForModule(pool, aggOwner), AggDB: env.Pool(t, migrations.PF, dbadmin.AppAgg), Clock: clk, Approvals: sqlApprovals{}, Governance: gov}
	return &aggFx{t: t, ctx: context.Background(), env: env, agg: a, gov: gov, alice: ids.New(), bob: ids.New()}
}

func (f *aggFx) approve(j domain.AggJob) AggProposal {
	f.t.Helper()
	p, err := f.agg.Propose(f.ctx, j, f.alice)
	if err != nil {
		f.t.Fatalf("Propose %s v%d: %v", j.ID, j.Version, err)
	}
	d, err := f.agg.Decide(f.ctx, p.ID, f.bob, wfport.OutcomeApproved, "")
	if err != nil {
		f.t.Fatalf("Decide %s v%d: %v", j.ID, j.Version, err)
	}
	return d
}

func (f *aggFx) runs(jobID string) []store.AggRunRow {
	f.t.Helper()
	var out []store.AggRunRow
	err := db.WithModuleTenantTx(f.ctx, f.agg.DB, aggOwner, f.gov, func(tx pgx.Tx) error {
		var err error
		out, err = store.AggRuns(f.ctx, tx, jobID)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// seedHarvest ghi (bằng tài khoản quản trị — dựng dữ liệu) một tenant góp dữ liệu: dòng k_ref.org + một khai thu hoạch
// ở mỗi xã trong communes cho vụ 2026 HE_THU, mass kg / 1 ha.
func (f *aggFx) seedHarvest(tid uuid.UUID, mass string, communes ...string) {
	f.t.Helper()
	admin := f.env.AdminPool(f.t, migrations.PF)
	tx, err := admin.Begin(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	if _, err := tx.Exec(f.ctx, `SELECT set_config('app.tenant_id', $1, true)`, tid.String()); err != nil {
		f.t.Fatal(err)
	}
	if _, err := tx.Exec(f.ctx, `INSERT INTO k_ref.org (tenant_id, ref, kind, code, active, source_seq) VALUES ($1, $2, 'cooperative', 'HTX', true, 1)`,
		tid, ids.New()); err != nil {
		f.t.Fatal(err)
	}
	for _, c := range communes {
		season := ids.New()
		// W3.3: sổ khai sản lượng ở pf_coop (view pf_cropcycle_pub.v1_harvest_declaration bám OID).
		if _, err := tx.Exec(f.ctx, `INSERT INTO pf_coop.harvest_declaration (id, tenant_id, stream_key, season_ref, plot_ref,
		    cooperative_ref, commune_code, season_code, season_year, variety_code, harvest_date, mass_kg, area_ha, yield_t_per_ha,
		    anomaly_confirmed, source, gps_source, location_status, on_behalf, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'harvest:' || $3::uuid::text, $3, $4, $5, $6, 'he_thu', 2026, 'OM5451', '2026-09-01', $7::numeric, 1, $7::numeric / 1000,
		          false, 'app', 'no_device', 'missing', false, now(), $8, $9, 'test')`,
			ids.New(), tid, season, ids.New(), ids.New(), c, mass, ids.New(), ids.New().String()); err != nil {
			f.t.Fatal(err)
		}
	}
	if err := tx.Commit(f.ctx); err != nil {
		f.t.Fatal(err)
	}
}

func pgCode(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}

// W6.4 (R-SEC-03, R-56): thêm job qua cap/workflow — người duyệt ≠ người đề xuất (app + CHECK ở hai sổ); một phiên bản
// một đề xuất, một quyết định; phiên bản chỉ tăng; danh mục và nhật ký chạy chỉ ghi thêm.
func TestAggRegistryFourEyes_W64(t *testing.T) {
	f := newAggFx(t)
	j := HarvestByCommune
	j.ID = "cc.fx_registry"
	p, err := f.agg.Propose(f.ctx, j, f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.agg.Decide(f.ctx, p.ID, f.alice, wfport.OutcomeApproved, ""); !IsProblem(err, problem.SelfApproval) {
		t.Fatalf("người đề xuất tự duyệt: %v", err)
	}
	if _, err := f.agg.Propose(f.ctx, j, f.bob); !IsProblem(err, ErrAggVersionNotNewer) {
		t.Fatalf("đề xuất lại cùng phiên bản: %v", err)
	}
	d, err := f.agg.Decide(f.ctx, p.ID, f.bob, wfport.OutcomeApproved, "")
	if err != nil || d.Action != "approved" || d.Replayed {
		t.Fatalf("duyệt: %+v %v", d, err)
	}
	if d, err := f.agg.Decide(f.ctx, p.ID, f.bob, wfport.OutcomeApproved, ""); err != nil || !d.Replayed {
		t.Fatalf("duyệt lặp: %+v %v", d, err)
	}
	if _, err := f.agg.Decide(f.ctx, p.ID, ids.New(), wfport.OutcomeRejected, "không cần"); !IsProblem(err, ErrAggAlreadyDecided) {
		t.Fatalf("quyết định thứ hai: %v", err)
	}
	if _, err := f.agg.Decide(f.ctx, ids.New(), f.bob, wfport.OutcomeApproved, ""); !IsProblem(err, ErrAggProposalNotFound) {
		t.Fatalf("đề xuất lạ: %v", err)
	}
	low := j
	low.Version, low.KMin = 2, 5
	if _, err := f.agg.Propose(f.ctx, low, f.alice); !IsProblem(err, ErrAggKBelowFloor) {
		t.Fatalf("k dưới sàn: %v", err)
	}

	// DB tự chặn: quyết định do chính người đề xuất (vượt app/), quyết định thứ hai, sửa/xóa sổ.
	own := f.agg.DB
	err = db.WithModuleTenantTx(f.ctx, own, aggOwner, f.gov, func(tx pgx.Tx) error {
		p2 := j
		p2.Version = 3
		row := store.AggJobRow{ID: ids.New(), JobID: p2.ID, Version: 3, DefHash: p2.Hash(), Views: p2.SortedViews(), QuerySQL: p2.Query,
			Purpose: p2.Purpose, KMin: p2.KMin, RequestRef: ids.New(), Requester: f.alice, EventTime: time.Now()}
		if err := store.InsertAggProposal(f.ctx, tx, f.gov, row, "test"); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, "SAVEPOINT s")
		if err != nil {
			return err
		}
		err = store.InsertAggDecision(f.ctx, tx, f.gov, ids.New(), row.ID, row, "approved", f.alice, nil, time.Now(), "test")
		if pgCode(err) != "23514" {
			t.Errorf("tự duyệt ở DB: %v (muốn 23514)", err)
		}
		_, _ = tx.Exec(f.ctx, "ROLLBACK TO SAVEPOINT s")
		// Dòng quyết định không tự khai định nghĩa: trigger chép từ đề xuất.
		forged := row
		forged.DefHash, forged.Version = strings.Repeat("0", 64), 99
		if err := store.InsertAggDecision(f.ctx, tx, f.gov, ids.New(), row.ID, forged, "approved", f.bob, nil, time.Now(), "test"); err != nil {
			return err
		}
		dec, err := store.AggDecisionOf(f.ctx, tx, row.ID)
		if err != nil || dec == nil || dec.DefHash != row.DefHash || dec.Version != 3 {
			t.Errorf("trigger chép đề xuất: %+v %v", dec, err)
		}
		_, _ = tx.Exec(f.ctx, "SAVEPOINT s2")
		err = store.InsertAggDecision(f.ctx, tx, f.gov, ids.New(), row.ID, row, "rejected", f.bob, ptr("lần hai"), time.Now(), "test")
		if c := pgCode(err); c != "23514" && c != "23505" { // CHECK agg_job_stream hoặc khóa idem của sổ
			t.Errorf("quyết định thứ hai ở DB: %v (muốn 23514/23505)", err)
		}
		_, _ = tx.Exec(f.ctx, "ROLLBACK TO SAVEPOINT s2")
		for _, q := range []string{`UPDATE pf_metrics.agg_job SET purpose = 'sửa tại chỗ cho đủ dài' WHERE job_id = $1`,
			`DELETE FROM pf_metrics.agg_job WHERE job_id = $1`, `UPDATE pf_metrics.agg_run SET outcome = 'failed' WHERE job_id = $1`,
			`DELETE FROM pf_metrics.agg_run WHERE job_id = $1`} {
			_, _ = tx.Exec(f.ctx, "SAVEPOINT s3")
			if _, err := tx.Exec(f.ctx, q, j.ID); err == nil {
				t.Errorf("%s: phải bị chặn", q)
			}
			_, _ = tx.Exec(f.ctx, "ROLLBACK TO SAVEPOINT s3")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Quyết định cap/workflow có cùng người duyệt (yêu cầu kind agg.job, subject = id đề xuất).
	var outcome string
	var decider uuid.UUID
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(f.ctx, `SELECT d.outcome, d.decider_ref FROM pf_workflow.decision d
	    WHERE d.tenant_id = $1 AND d.kind = 'agg.job' AND d.subject_ref = $2`, f.gov, p.ID).Scan(&outcome, &decider); err != nil ||
		outcome != "approved" || decider != f.bob {
		t.Fatalf("pf_workflow.decision: %s %s %v", outcome, decider, err)
	}
}

func ptr[T any](v T) *T { return &v }

// W6.4 (R-DB-11): job đã duyệt đọc qua view _pub của MỌI tenant (liệt kê bằng k_ref.org vượt RLS), gộp nhóm, ẩn nhóm
// có ít hơn k tenant; ngoài giao dịch job, cùng view chỉ thấy tenant của ngữ cảnh. Mỗi lần chạy một dòng nhật ký.
func TestAggRunAcrossTenants_W64(t *testing.T) {
	f := newAggFx(t)
	var tenants []uuid.UUID
	for i := range 12 {
		tid := ids.New()
		tenants = append(tenants, tid)
		communes := []string{"AGW64A"}
		if i < 3 {
			communes = append(communes, "AGW64B") // chỉ 3 tenant → dưới k = 10, bị ẩn
		}
		f.seedHarvest(tid, "1000", communes...)
	}
	f.approve(HarvestByCommune)
	res, err := f.agg.Run(f.ctx, HarvestByCommune, f.alice, 0)
	if err != nil {
		t.Fatal(err)
	}
	var a *domain.AggGroup
	for i, g := range res.Groups {
		if g.Dims[0] == "AGW64B" {
			t.Errorf("nhóm dưới k lộ ra: %+v", g)
		}
		if g.Dims[0] == "AGW64A" {
			a = &res.Groups[i]
		}
	}
	if a == nil || a.Contributors != 12 || a.Measures[0].String() != "12000" || a.Measures[1].String() != "12" ||
		a.Dims[1] != "2026" || a.Dims[2] != "HE_THU" || res.KUsed != 10 || res.GroupsSuppressed < 1 || res.Tenants < 12 {
		t.Fatalf("kết quả: %+v (nhóm A %+v)", res, a)
	}
	runs := f.runs(HarvestByCommune.ID)
	if len(runs) != 1 || runs[0].Outcome != "succeeded" || runs[0].JobRef == nil || runs[0].KUsed == nil || *runs[0].KUsed != 10 ||
		runs[0].RowsRead < 15 || runs[0].TenantsRead < 12 || runs[0].GroupsOut != len(res.Groups) || runs[0].Actor != f.alice {
		t.Fatalf("nhật ký chạy: %+v", runs)
	}

	// Ngoài giao dịch job: r_metrics đọc cùng view chỉ thấy tenant của ngữ cảnh.
	var n int
	err = db.WithModuleTenantTx(f.ctx, f.agg.DB, aggOwner, tenants[0], func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT count(DISTINCT tenant_id) FROM pf_cropcycle_pub.v1_harvest_declaration`).Scan(&n)
	})
	if err != nil || n != 1 {
		t.Fatalf("r_metrics qua _pub: %d tenant, %v", n, err)
	}
}

// probeJob trả current_user trong giao dịch job làm chiều nhóm và số dòng thấy được — chứng minh vai đã đổi.
func probeJob(version int, query string) domain.AggJob {
	if query == "" {
		query = `SELECT current_user::text, count(*)::text FROM pf_cropcycle_pub.v1_harvest_declaration`
	}
	return domain.AggJob{ID: "cc.fx_probe", Version: version, Views: []string{"pf_cropcycle_pub.v1_harvest_declaration"},
		Query: query, Dims: []string{"role"}, Measures: []string{"rows"}, Purpose: "Thăm dò vai trong giao dịch job tổng hợp",
		KMin: domain.KFloorDefault}
}

// W6.4: job chưa khai/chưa duyệt/phiên bản cũ/định nghĩa bị sửa/k dưới sàn/bị từ chối KHÔNG đổi được vai (refused, có
// nhật ký); job đã duyệt chạy dưới r_agg CHỈ trong giao dịch job — kết nối về pool ở vai đăng nhập không quyền.
func TestAggRunRefusedAndRoleScope_W64(t *testing.T) {
	f := newAggFx(t)
	for range 11 {
		f.seedHarvest(ids.New(), "10", "AGW64P")
	}
	expectRefused := func(j domain.AggJob, k int, want *problem.Problem) {
		t.Helper()
		if _, err := f.agg.Run(f.ctx, j, f.alice, k); !IsProblem(err, want) {
			t.Fatalf("%s v%d k=%d: %v, muốn %s", j.ID, j.Version, k, err, want.Code)
		}
	}
	// Chưa khai.
	expectRefused(probeJob(1, ""), 0, ErrAggNotApproved)
	// Đã đề xuất, chưa duyệt.
	if _, err := f.agg.Propose(f.ctx, probeJob(1, ""), f.alice); err != nil {
		t.Fatal(err)
	}
	expectRefused(probeJob(1, ""), 0, ErrAggNotApproved)
	p1, err := f.agg.Propose(f.ctx, probeJob(2, ""), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.agg.Decide(f.ctx, p1.ID, f.bob, wfport.OutcomeApproved, ""); err != nil {
		t.Fatal(err)
	}
	// Đã duyệt: vai trong giao dịch là r_agg, thấy mọi tenant.
	res, err := f.agg.Run(f.ctx, probeJob(2, ""), f.alice, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Dims[0] != "r_agg" || res.Groups[0].Contributors < 11 {
		t.Fatalf("thăm dò: %+v", res)
	}
	// Định nghĩa sửa tại chỗ (cùng phiên bản) → băm khác.
	expectRefused(probeJob(2, `SELECT session_user::text, count(*)::text FROM pf_cropcycle_pub.v1_harvest_declaration`), 0, ErrAggNotApproved)
	// k dưới sàn.
	expectRefused(probeJob(2, ""), 5, ErrAggKBelowFloor)
	// Phiên bản mới được duyệt → phiên bản cũ bị từ chối.
	p3, err := f.agg.Propose(f.ctx, probeJob(3, ""), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.agg.Run(f.ctx, probeJob(2, ""), f.alice, 0); err != nil {
		t.Fatalf("v3 chưa duyệt — v2 vẫn chạy: %v", err)
	}
	if _, err := f.agg.Decide(f.ctx, p3.ID, f.bob, wfport.OutcomeApproved, ""); err != nil {
		t.Fatal(err)
	}
	expectRefused(probeJob(2, ""), 0, ErrAggNotApproved)
	// Phiên bản bị từ chối.
	p4, err := f.agg.Propose(f.ctx, probeJob(4, ""), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.agg.Decide(f.ctx, p4.ID, f.bob, wfport.OutcomeRejected, "không đạt"); err != nil {
		t.Fatal(err)
	}
	expectRefused(probeJob(4, ""), 0, ErrAggNotApproved)

	runs := f.runs("cc.fx_probe")
	var got []string
	for _, r := range runs {
		code := ""
		if r.ErrorCode != nil {
			code = *r.ErrorCode
		}
		got = append(got, fmt.Sprintf("v%d:%s:%s", r.Version, r.Outcome, code))
	}
	want := "v1:refused:metrics.agg_job_not_approved v1:refused:metrics.agg_job_not_approved v2:succeeded: " +
		"v2:refused:metrics.agg_job_not_approved v2:refused:metrics.agg_k_below_floor v2:succeeded: " +
		"v2:refused:metrics.agg_job_not_approved v4:refused:metrics.agg_job_not_approved"
	if strings.Join(got, " ") != want {
		t.Fatalf("nhật ký chạy:\n%s\nmuốn\n%s", strings.Join(got, " "), want)
	}

	// Ngoài giao dịch job: kết nối của pool ở vai đăng nhập NOINHERIT — không đọc được view _pub, không BYPASSRLS.
	// app_worker không còn là thành viên r_agg (gói FC) — đổi vai bị 42501 ngay cả trong giao dịch.
	pool := f.env.Pool(t, migrations.PF, dbadmin.AppWorker)
	aggPool := f.env.Pool(t, migrations.PF, dbadmin.AppAgg)
	for _, c := range []struct {
		p    *pgxpool.Pool
		role string
	}{{pool, dbadmin.AppWorker}, {aggPool, dbadmin.AppAgg}} {
		var who string
		var bypass bool
		if err := c.p.QueryRow(f.ctx, `SELECT current_user::text, r.rolbypassrls FROM pg_roles r WHERE r.rolname = current_user`).Scan(&who, &bypass); err != nil ||
			who != c.role || bypass {
			t.Fatalf("sau job: %s bypass=%v %v", who, bypass, err)
		}
		var n int
		if err := c.p.QueryRow(f.ctx, `SELECT count(*) FROM pf_cropcycle_pub.v1_harvest_declaration`).Scan(&n); pgCode(err) != "42501" {
			t.Fatalf("%s đọc _pub ngoài giao dịch job: %v", c.role, err)
		}
	}
	// app_worker không vào được r_agg (kể cả gọi WithAggTx với pool sai); app_agg không vào được r_metrics.
	err = db.WithAggTx(f.ctx, f.agg.DB, aggOwner, f.gov, pool, func(pgx.Tx) error { return nil }, func(db.AggTx) error { return nil })
	if pgCode(err) != "42501" {
		t.Fatalf("app_worker SET ROLE r_agg phải 42501: %v", err)
	}
	err = db.WithModuleTenantTx(f.ctx, aggPool, aggOwner, f.gov, func(pgx.Tx) error { return nil })
	if pgCode(err) != "42501" {
		t.Fatalf("app_agg SET ROLE r_metrics phải 42501: %v", err)
	}
	// Mặc định của vai app_agg: chỉ đọc kể cả ngoài WithAggTx.
	var ro string
	if err := aggPool.QueryRow(f.ctx, `SHOW default_transaction_read_only`).Scan(&ro); err != nil || ro != "on" {
		t.Fatalf("app_agg default_transaction_read_only: %q %v", ro, err)
	}

	// authorize lỗi → fn không chạy, không đổi vai.
	called := false
	err = db.WithAggTx(f.ctx, f.agg.DB, aggOwner, f.gov, aggPool, func(pgx.Tx) error { return ErrAggNotApproved },
		func(db.AggTx) error { called = true; return nil })
	if !IsProblem(err, ErrAggNotApproved) || called {
		t.Fatalf("authorize lỗi: %v, fn chạy %v", err, called)
	}
	// Giao dịch job chỉ đọc; r_agg không ghi được, không đọc bảng nội bộ.
	err = db.WithAggTx(f.ctx, f.agg.DB, aggOwner, f.gov, aggPool, func(pgx.Tx) error { return nil }, func(a db.AggTx) error {
		var sess string
		if err := a.Tx().QueryRow(f.ctx, `SELECT session_user::text`).Scan(&sess); err != nil || sess != dbadmin.AppAgg {
			t.Errorf("session_user trong giao dịch job: %q %v", sess, err)
		}
		for q, code := range map[string]string{
			`SELECT count(*) FROM pf_coop.harvest_declaration`: "42501",
			`SELECT count(*) FROM pf_metrics.agg_job`:          "42501",
			`SELECT count(ref) FROM k_ref.org`:                 "42501",
			`CREATE TEMP TABLE agg_probe (a int)`:              "25006",
		} {
			_, _ = a.Tx().Exec(f.ctx, "SAVEPOINT p")
			if _, err := a.Tx().Exec(f.ctx, q); pgCode(err) != code {
				t.Errorf("%s: %v (muốn %s)", q, err, code)
			}
			_, _ = a.Tx().Exec(f.ctx, "ROLLBACK TO SAVEPOINT p")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Mọi job đã khai hợp lệ với k sàn tạm, id không trùng.
func TestDeclaredAggJobs_W64(t *testing.T) {
	seen := map[string]bool{}
	for _, j := range AggJobs {
		if err := domain.ValidateAggJob(j, domain.KFloorDefault); err != nil {
			t.Errorf("%s: %v", j.ID, err)
		}
		if seen[j.ID] {
			t.Errorf("trùng job %s", j.ID)
		}
		seen[j.ID] = true
	}
}
