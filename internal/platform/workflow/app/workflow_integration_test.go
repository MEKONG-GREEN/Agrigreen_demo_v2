package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fx struct {
	t      *testing.T
	ctx    context.Context
	svc    *Service
	caller tenant.Beginner // module gọi port trong giao dịch của nó (vai cũ masterdata_app → r_masterdata)
	own    tenant.Beginner // giao dịch riêng của pf/workflow (app_platform → r_workflow)
	now    time.Time
}

func newFx(t *testing.T) *fx {
	env := pgtest.Get(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	own := db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), DBModule)
	return &fx{t: t, ctx: context.Background(), svc: &Service{DB: own, Clock: clock.NewFixed(now)},
		caller: env.Pool(t, migrations.PF, "masterdata_app"), own: own, now: now}
}

// inTx chạy fn trong giao dịch của bên gọi ở tenant tid.
func (f *fx) inTx(tid uuid.UUID, fn func(tx pgx.Tx) error) error {
	return tenant.WithTenantTx(f.ctx, f.caller, tid, fn)
}

func pgCode(err error) (string, string) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code, pg.ConstraintName
	}
	return "", ""
}

func isProblem(err error, code string) bool {
	var p *problem.Problem
	return errors.As(err, &p) && p.Code == code
}

// Luồng port: lập (idempotent) → quyết định (gọi lặp trả lại) → trạng thái; bốn mắt; lập bù khi chưa có yêu cầu;
// lệch người lập; quyết định khác → already_decided; RLS: tenant khác không thấy.
func TestApprovalsFlow_W61(t *testing.T) {
	f := newFx(t)
	tid, subj, setter, approver, ben := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	const kind = "test.object"

	var req port.Request
	for i := 0; i < 2; i++ {
		if err := f.inTx(tid, func(tx pgx.Tx) error {
			var e error
			req, e = f.svc.Submit(db.JoinTx(f.ctx, tx), port.SubmitInput{Kind: kind, Subject: subj, Requester: setter, Beneficiary: &ben, Summary: "k"})
			return e
		}); err != nil {
			t.Fatal(err)
		}
		if req.Replayed != (i == 1) || req.Seq != 1 || req.Requester != setter || req.Beneficiary == nil || *req.Beneficiary != ben {
			t.Fatalf("submit lần %d: %+v", i, req)
		}
	}
	// Lệch người lập.
	err := f.inTx(tid, func(tx pgx.Tx) error {
		_, e := f.svc.Submit(db.JoinTx(f.ctx, tx), port.SubmitInput{Kind: kind, Subject: subj, Requester: approver, Beneficiary: &ben})
		return e
	})
	if !isProblem(err, "workflow.request_mismatch") {
		t.Fatalf("lệch người lập: %v", err)
	}
	// Bốn mắt ở app/: người lập, người hưởng không được duyệt.
	for _, who := range []uuid.UUID{setter, ben} {
		err := f.inTx(tid, func(tx pgx.Tx) error {
			_, e := f.svc.Decide(db.JoinTx(f.ctx, tx), port.DecideInput{Kind: kind, Subject: subj, Requester: setter, Beneficiary: &ben, Decider: who,
				Outcome: port.OutcomeApproved})
			return e
		})
		if !isProblem(err, "self_approval_forbidden") {
			t.Fatalf("bốn mắt (%v): %v", who, err)
		}
	}
	var d port.Decision
	for i := 0; i < 2; i++ {
		if err := f.inTx(tid, func(tx pgx.Tx) error {
			var e error
			d, e = f.svc.Decide(db.JoinTx(f.ctx, tx), port.DecideInput{Kind: kind, Subject: subj, Requester: setter, Beneficiary: &ben, Decider: approver,
				Outcome: port.OutcomeApproved})
			return e
		}); err != nil {
			t.Fatal(err)
		}
		if d.Replayed != (i == 1) || d.RequestID != req.ID || d.Outcome != port.OutcomeApproved || d.Decider != approver {
			t.Fatalf("decide lần %d: %+v", i, d)
		}
	}
	err = f.inTx(tid, func(tx pgx.Tx) error {
		_, e := f.svc.Decide(db.JoinTx(f.ctx, tx), port.DecideInput{Kind: kind, Subject: subj, Requester: setter, Beneficiary: &ben, Decider: ids.New(),
			Outcome: port.OutcomeRejected, Note: "khác"})
		return e
	})
	if !isProblem(err, "workflow.already_decided") {
		t.Fatalf("quyết định thứ hai: %v", err)
	}
	st, err := f.svc.Status(tenant.WithID(f.ctx, tid), kind, subj)
	if err != nil || st.Request.ID != req.ID || st.Decision == nil || st.Decision.ID != d.ID {
		t.Fatalf("status: %+v %v", st, err)
	}
	if _, err := f.svc.Status(tenant.WithID(f.ctx, ids.New()), kind, subj); !errors.Is(err, port.ErrRequestNotFound) {
		t.Fatalf("tenant khác phải không thấy: %v", err)
	}

	// Lập bù (đối tượng gửi duyệt trước W6.1): Decide khi chưa có yêu cầu.
	legacy := ids.New()
	if err := f.inTx(tid, func(tx pgx.Tx) error {
		_, e := f.svc.Decide(db.JoinTx(f.ctx, tx), port.DecideInput{Kind: kind, Subject: legacy, Requester: setter, Decider: approver,
			Outcome: port.OutcomeRejected, Note: "lý do"})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if st, err := f.svc.Status(tenant.WithID(f.ctx, tid), kind, legacy); err != nil || st.Request.Requester != setter ||
		st.Decision == nil || st.Decision.Outcome != port.OutcomeRejected {
		t.Fatalf("lập bù: %+v %v", st, err)
	}
	// Hủy: người lập tự hủy được.
	cancel := ids.New()
	if err := f.inTx(tid, func(tx pgx.Tx) error {
		if _, e := f.svc.Submit(db.JoinTx(f.ctx, tx), port.SubmitInput{Kind: kind, Subject: cancel, Requester: setter}); e != nil {
			return e
		}
		_, e := f.svc.Decide(db.JoinTx(f.ctx, tx), port.DecideInput{Kind: kind, Subject: cancel, Requester: setter, Decider: setter,
			Outcome: port.OutcomeCancelled, Note: "rút đề xuất"})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// Giao dịch bên gọi rollback → không còn yêu cầu (ghi cùng giao dịch, R-21).
	rolled := ids.New()
	boom := errors.New("rollback")
	if err := f.inTx(tid, func(tx pgx.Tx) error {
		if _, e := f.svc.Submit(db.JoinTx(f.ctx, tx), port.SubmitInput{Kind: kind, Subject: rolled, Requester: setter}); e != nil {
			return e
		}
		return boom
	}); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := f.svc.Status(tenant.WithID(f.ctx, tid), kind, rolled); !errors.Is(err, port.ErrRequestNotFound) {
		t.Fatalf("rollback: %v", err)
	}
}

// DB (pf 000118): bốn mắt bằng CHECK kể cả khi bỏ qua app/; requester/beneficiary của decision chép từ request
// (không giả được); quyết định không có yêu cầu → 23503; một yêu cầu một quyết định; sổ chỉ ghi thêm; tenant thiếu.
func TestWorkflowDBConstraints_W61(t *testing.T) {
	f := newFx(t)
	env := pgtest.Get(t)
	tid, subj, setter, ben := ids.New(), ids.New(), ids.New(), ids.New()
	const kind = "test.dbcheck"
	var reqID uuid.UUID
	if err := f.inTx(tid, func(tx pgx.Tx) error {
		r, e := f.svc.Submit(db.JoinTx(f.ctx, tx), port.SubmitInput{Kind: kind, Subject: subj, Requester: setter, Beneficiary: &ben})
		reqID = r.ID
		return e
	}); err != nil {
		t.Fatal(err)
	}
	callDecide := func(decider uuid.UUID, requester uuid.UUID, outcome string) error {
		return f.inTx(tid, func(tx pgx.Tx) error {
			_, e := tx.Exec(f.ctx, `SELECT * FROM pf_workflow.decide($1, $2, $3, $4, $5, $6, $7, $8, 'ghi chú', now(), 't')`,
				ids.New(), ids.New(), kind, subj, requester, ben, decider, outcome)
			return e
		})
	}
	// Gọi thẳng hàm DB (bỏ qua app/): người lập / người hưởng duyệt → CHECK decision_four_eyes.
	for _, who := range []uuid.UUID{setter, ben} {
		if c, n := pgCode(callDecide(who, setter, "approved")); c != "23514" || n != "decision_four_eyes" {
			t.Fatalf("CHECK bốn mắt (%v): %s %s", who, c, n)
		}
	}
	// Khai sai người lập ở hàm → AGW01 (không lập yêu cầu thứ hai).
	if c, _ := pgCode(callDecide(ids.New(), ids.New(), "approved")); c != "AGW01" {
		t.Fatalf("lệch người lập: %s", c)
	}
	// r_workflow ghi thẳng decision với requester giả = người duyệt: trigger chép requester thật → vẫn qua bốn mắt;
	// khai người lập giả là người khác để lách → trigger ghi đè, decider = setter vẫn bị CHECK chặn.
	ownInsert := func(t2, request uuid.UUID, decider, fakeRequester uuid.UUID) error {
		return tenant.WithTenantTx(f.ctx, f.own, t2, func(tx pgx.Tx) error {
			_, e := tx.Exec(f.ctx, `INSERT INTO pf_workflow.decision (id, tenant_id, stream_key, request_id, kind, subject_ref, requester_ref,
			    beneficiary_ref, decider_ref, outcome, event_time, actor_ref, idem_key, rule_version)
			  VALUES ($1, $2, 'request:' || $3::uuid::text, $3::uuid, 'x.y', gen_random_uuid(), $5, NULL, $4, 'approved', now(), $4, $6, 't')`,
				ids.New(), t2, request, decider, fakeRequester, "direct:"+ids.New().String())
			return e
		})
	}
	if c, n := pgCode(ownInsert(tid, reqID, setter, ids.New())); c != "23514" || n != "decision_four_eyes" {
		t.Fatalf("ghi thẳng giả người lập: %s %s", c, n)
	}
	if c, _ := pgCode(ownInsert(tid, ids.New(), ids.New(), ids.New())); c != "23503" {
		t.Fatalf("quyết định không có yêu cầu: %s", c)
	}
	// Yêu cầu của tenant khác: trigger không thấy (RLS) → 23503.
	if c, _ := pgCode(ownInsert(ids.New(), reqID, ids.New(), ids.New())); c != "23503" {
		t.Fatalf("yêu cầu tenant khác: %s", c)
	}
	// Một yêu cầu một quyết định: quyết định đầu qua, quyết định thứ hai (khóa chống trùng khác) → seq 2 bị CHECK.
	if err := ownInsert(tid, reqID, ids.New(), ids.New()); err != nil {
		t.Fatalf("quyết định hợp lệ ghi thẳng: %v", err)
	}
	if c, n := pgCode(ownInsert(tid, reqID, ids.New(), ids.New())); c != "23514" || n != "decision_one_per_request" {
		t.Fatalf("quyết định thứ hai: %s %s", c, n)
	}
	// Một đối tượng một yêu cầu: r_workflow ghi thẳng yêu cầu thứ hai cùng đối tượng.
	err := tenant.WithTenantTx(f.ctx, f.own, tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(f.ctx, `INSERT INTO pf_workflow.request (id, tenant_id, stream_key, kind, subject_ref, requester_ref, event_time,
		    actor_ref, idem_key, rule_version) VALUES ($1, $2, 'subject:' || $3::text || ':' || $4::uuid::text, $3::text, $4::uuid, $5, now(), $5, $6, 't')`,
			ids.New(), tid, kind, subj, setter, "direct:"+ids.New().String())
		return e
	})
	if c, n := pgCode(err); c != "23514" || n != "request_one_per_subject" {
		t.Fatalf("yêu cầu thứ hai: %s %s %v", c, n, err)
	}
	// Chỉ ghi thêm: r_workflow không có UPDATE/DELETE; chủ bảng bị trigger chặn.
	for _, q := range []string{"UPDATE pf_workflow.request SET summary = 'x'", "DELETE FROM pf_workflow.decision"} {
		err := tenant.WithTenantTx(f.ctx, f.own, tid, func(tx pgx.Tx) error { _, e := tx.Exec(f.ctx, q); return e })
		if c, _ := pgCode(err); c != "42501" {
			t.Errorf("r_workflow %q phải 42501: %v", q, err)
		}
	}
	admin := env.Pool(t, migrations.PF, dbadmin.Migrator)
	for _, q := range []string{"UPDATE pf_workflow.request SET summary = 'x'", "DELETE FROM pf_workflow.decision"} {
		err := tenant.WithTenantTx(f.ctx, admin, tid, func(tx pgx.Tx) error { _, e := tx.Exec(f.ctx, q); return e })
		if err == nil {
			t.Errorf("chủ bảng %q phải bị trigger chặn", q)
		}
	}
	// Thiếu tenant → 42501.
	tx, err := f.caller.BeginTx(f.ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	_, err = tx.Exec(f.ctx, `SELECT * FROM pf_workflow.submit(gen_random_uuid(), 'a.b', gen_random_uuid(), gen_random_uuid(), NULL, gen_random_uuid(), NULL, now(), 't')`)
	if c, _ := pgCode(err); c != "42501" {
		t.Fatalf("thiếu tenant: %v", err)
	}
}

// Quyền (pf 000118, ADR-0028): r_workflow SELECT/INSERT hai sổ; r_masterdata, r_party chỉ EXECUTE hai hàm (không quyền
// bảng); vai khác không EXECUTE; app_platform SET ROLE r_workflow được, app_worker thì không; sổ đạt mẫu bất biến.
func TestWorkflowGrants_W61(t *testing.T) {
	f := newFx(t)
	env := pgtest.Get(t)
	admin := env.AdminPool(t, migrations.PF)
	const submitSig = "pf_workflow.submit(uuid, text, uuid, uuid, uuid, uuid, text, timestamptz, text)"
	checks := []struct {
		q    string
		want bool
	}{
		{"has_table_privilege('r_workflow', 'pf_workflow.request', 'SELECT')", true},
		{"has_table_privilege('r_workflow', 'pf_workflow.decision', 'INSERT')", true},
		{"has_table_privilege('r_workflow', 'pf_workflow.decision', 'UPDATE')", false},
		{"has_table_privilege('r_workflow', 'pf_workflow.request', 'DELETE')", false},
		{"has_table_privilege('r_workflow', 'k_ref.node_closure', 'SELECT')", true},
		{"has_table_privilege('r_masterdata', 'pf_workflow.request', 'SELECT')", false},
		{"has_table_privilege('r_party', 'pf_workflow.decision', 'INSERT')", false},
		{"has_function_privilege('r_masterdata', '" + submitSig + "', 'EXECUTE')", true},
		{"has_function_privilege('r_party', '" + submitSig + "', 'EXECUTE')", true},
		{"has_function_privilege('masterdata_app', '" + submitSig + "', 'EXECUTE')", true},
		{"has_function_privilege('r_content', '" + submitSig + "', 'EXECUTE')", false},
		{"has_function_privilege('r_geo', 'pf_workflow.decide(uuid, uuid, text, uuid, uuid, uuid, uuid, text, text, timestamptz, text)', 'EXECUTE')", false},
		{"(SELECT count(*) = 0 FROM shared.v_missing_immutability WHERE schema_name = 'pf_workflow')", true},
		{"(SELECT relforcerowsecurity FROM pg_class WHERE oid = 'pf_workflow.decision'::regclass)", true},
	}
	for _, c := range checks {
		var got bool
		if err := admin.QueryRow(f.ctx, "SELECT "+c.q).Scan(&got); err != nil || got != c.want {
			t.Errorf("%s = %v (muốn %v): %v", c.q, got, c.want, err)
		}
	}
	tid := ids.New()
	if err := db.WithModuleTenantTx(f.ctx, env.Pool(t, migrations.PF, dbadmin.AppPlatform), DBModule, tid, func(pgx.Tx) error { return nil }); err != nil {
		t.Fatalf("app_platform SET ROLE r_workflow: %v", err)
	}
	err := db.WithModuleTenantTx(f.ctx, env.Pool(t, migrations.PF, dbadmin.AppWorker), DBModule, tid, func(pgx.Tx) error { return nil })
	if c, _ := pgCode(err); c != "42501" {
		t.Fatalf("app_worker SET ROLE r_workflow phải 42501: %v", err)
	}
}
