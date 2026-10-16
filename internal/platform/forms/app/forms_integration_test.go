package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	wfport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
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

// sqlApprovals là cap/workflow tối thiểu cho kiểm thử của module (module không import cài đặt của pf/workflow):
// gọi thẳng hàm DB pf_workflow.submit/decide trong giao dịch của pf/forms — kiểm luôn quyền EXECUTE của r_forms
// (pf 000122) và CHECK decision_four_eyes. Lắp ráp thật dùng workflow.Module.Approvals (cmd/api-platform/assembly).
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

// fxProfile: TW → TINH → XA, Tỉnh phát hành/rà soát/chốt; cho biểu mẫu thử; chặn chốt khi còn bài chờ rà soát.
var fxProfile = port.Profile{Name: "fx", ResolveActor: fixtureActorResolver, Hierarchy: []string{"TW", "TINH", "XA"}, Roles: map[string]string{"tw": "TW", "tinh": "TINH", "xa": "XA"},
	PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"}, AllowFixtures: true, RequireReviewedBeforeClose: true}

// prodProfile: như production — không phát hành trên biểu mẫu thử (chờ Q-01).
var prodProfile = port.Profile{Name: "prod", ResolveActor: fixtureActorResolver, Hierarchy: []string{"TW", "TINH", "XA"}, Roles: map[string]string{"tinh": "TINH", "xa": "XA"},
	PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"}}

type fx struct {
	t   *testing.T
	ctx context.Context
	svc *Service
	clk *clock.Fixed
	env *pgtest.Env
	own tenant.Beginner
	tid uuid.UUID
}

func newFx(t *testing.T) *fx {
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC))
	own := db.ForModule(env.Pool(t, migrations.PF, dbadmin.AppPlatform), DBModule)
	svc, err := NewService(own, clk, sqlApprovals{}, []port.Profile{fxProfile, prodProfile})
	if err != nil {
		t.Fatal(err)
	}
	return &fx{t: t, ctx: context.Background(), svc: svc, clk: clk, env: env, own: own, tid: ids.New()}
}

// as supplies the fixture application session, including role, scope and node.
func (f *fx) as(tid, actor uuid.UUID, role string, node uuid.UUID, units ...string) context.Context {
	if actor == uuid.Nil {
		actor = ids.New()
	}
	sc := authz.Scope{Units: units}
	if role == "admin" || role == "tw" {
		sc = authz.Scope{All: true}
	}
	return fixtureContext(f.ctx, authz.Principal{ActorRef: actor, TenantID: tid}, port.ResolvedActor{Role: role, Scope: sc, Node: node})
}

func isProblem(err error, code string) bool {
	var p *problem.Problem
	return errors.As(err, &p) && p.Code == code
}

func pgCode(err error) (string, string) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code, pg.ConstraintName
	}
	return "", ""
}

func (f *fx) must(err error, what string) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("%s: %v", what, err)
	}
}

func (f *fx) want(err error, code, what string) {
	f.t.Helper()
	if !isProblem(err, code) {
		f.t.Fatalf("%s: muốn %s, có %v", what, code, err)
	}
}

func (f *fx) scalar(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.env.AdminPool(f.t, migrations.PF).QueryRow(f.ctx, q, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

var fx01 = map[string]string{"crop": "lua", "area_planned": "120.5", "area_sown": "80", "households": "42", "report_date": "2026-10-04"}

// Vòng đời đầy đủ: định nghĩa biểu (fixture) → phát hành → nộp → rà soát bốn mắt (cap/workflow) → validator trước
// chốt → chốt kèm ảnh chụp → mở lại một phần → nộp lại → chốt lại (ảnh mới, ảnh cũ nguyên vẹn); vai, phạm vi, RLS.
func TestFormsLifecycle_W62(t *testing.T) {
	f := newFx(t)
	tid, node := f.tid, ids.New()
	admin := f.as(tid, uuid.Nil, "admin", uuid.Nil)
	tinhRef, xa1Ref, xa2Ref := ids.New(), ids.New(), ids.New()
	tinh := f.as(tid, tinhRef, "tinh", node, "U1", "U2", "U3")
	xa1 := f.as(tid, xa1Ref, "xa", ids.New(), "U1")
	xa2 := f.as(tid, xa2Ref, "xa", ids.New(), "U2")

	// Biểu: chỉ define_by; fixture; phiên bản tăng.
	_, err := f.svc.InstallFixtures(tinh, "fx", "")
	f.want(err, "forbidden", "Tỉnh định nghĩa biểu")
	tpls, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "fixture")
	if len(tpls) != 2 || !tpls[0].Fixture || tpls[0].Version != 1 || tpls[0].Code != "FX-01" {
		t.Fatalf("fixture: %+v", tpls)
	}
	v2, err := f.svc.DefineTemplate(admin, TemplateInput{Profile: "fx", Code: "FX-01", Title: "Bản 2", Fields: tpls[0].Fields, Fixture: true}, "")
	f.must(err, "phiên bản 2")
	if v2.Version != 2 {
		t.Fatalf("phiên bản 2: %+v", v2)
	}
	_, err = f.svc.DefineTemplate(admin, TemplateInput{Profile: "fx", Code: "bad code", Title: "x", Fields: tpls[0].Fields}, "")
	f.want(err, "forms.template_invalid", "mã sai")
	tpl := tpls[0] // kỳ dùng phiên bản 1

	// Profile production: không phát hành trên biểu mẫu thử (Q-01).
	ptpls, err := f.svc.InstallFixtures(admin, "prod", "")
	f.must(err, "fixture prod")
	_, err = f.svc.Publish(f.as(tid, uuid.Nil, "tinh", node, "U1"), PublishInput{Profile: "prod", TemplateID: ptpls[0].ID, Label: "Kỳ", DueAt: f.clk.Now(),
		Units: []string{"U1"}}, "")
	f.want(err, "forms.fixture_template", "prod + fixture")

	// Phát hành: cấp publish_by, đơn vị trong phạm vi.
	due := f.clk.Now().Add(72 * time.Hour)
	pin := PublishInput{Profile: "fx", TemplateID: tpl.ID, Label: "Tuần 41/2026", DueAt: due, Units: []string{"U1", "U2"}}
	_, err = f.svc.Publish(xa1, pin, "")
	f.want(err, "forbidden", "Xã phát hành")
	_, err = f.svc.Publish(tinh, PublishInput{Profile: "fx", TemplateID: tpl.ID, Label: "x", DueAt: due, Units: []string{"U9"}}, "")
	f.want(err, "forbidden", "ngoài phạm vi")
	_, err = f.svc.Publish(tinh, PublishInput{Profile: "fx", TemplateID: ptpls[0].ID, Label: "x", DueAt: due, Units: []string{"U1"}}, "")
	f.want(err, "forms.template_not_found", "biểu của profile khác")
	pv, err := f.svc.Publish(tinh, pin, "")
	f.must(err, "phát hành")
	pid := pv.Period.ID
	if pv.State.Status != domain.PeriodOpen || pv.Period.TemplateVersion != 1 || pv.Period.TemplateCode != "FX-01" || len(pv.Units) != 2 ||
		pv.Units[0].Status != domain.SubNone {
		t.Fatalf("kỳ mới: %+v", pv)
	}
	// Xã chỉ thấy đơn vị của mình.
	if v, err := f.svc.Period(xa1, "fx", pid); err != nil || len(v.Units) != 1 || v.Units[0].Unit != "U1" {
		t.Fatalf("xã 1 xem kỳ: %+v %v", v.Units, err)
	}

	// Nộp: cấp lá, phạm vi, đơn vị thuộc kỳ, số liệu khớp biểu.
	_, err = f.svc.Submit(xa1, "fx", pid, "U2", fx01, "")
	f.want(err, "forbidden", "xã 1 nộp cho U2")
	_, err = f.svc.Submit(f.as(tid, uuid.Nil, "xa", uuid.Nil, "U3"), "fx", pid, "U3", fx01, "")
	f.want(err, "forms.unit_not_in_period", "U3 không thuộc kỳ")
	_, err = f.svc.Submit(xa1, "fx", pid, "U1", map[string]string{"crop": "lua", "area_planned": "1.5e3", "area_sown": "1", "report_date": "2026-10-04"}, "")
	f.want(err, "forms.values_invalid", "số mũ không phải thập phân")
	_, err = f.svc.Submit(xa1, "fx", pid, "U1", map[string]string{"crop": "lua"}, "")
	f.want(err, "forms.values_invalid", "thiếu trường bắt buộc")
	s1, err := f.svc.Submit(xa1, "fx", pid, "U1", fx01, "")
	f.must(err, "xã 1 nộp")
	s2, err := f.svc.Submit(xa2, "fx", pid, "U2", map[string]string{"crop": "ngo", "area_planned": "10", "area_sown": "9.25", "report_date": "2026-10-04"}, "")
	f.must(err, "xã 2 nộp")
	if s1.Submission.Version != 1 || f.scalar(`SELECT count(*) FROM pf_workflow.request WHERE tenant_id = $1 AND kind = 'forms.submission'`, tid) != 2 {
		t.Fatalf("nộp → yêu cầu duyệt cap/workflow: %+v", s1)
	}

	// Rà soát: cấp review_by; bốn mắt (cùng người, ngữ cảnh Tỉnh khác) → self_approval.
	_, err = f.svc.Review(xa2, "fx", s1.Submission.ID, "accepted", "", "")
	f.want(err, "forbidden", "Xã rà soát")
	_, err = f.svc.Review(f.as(tid, xa1Ref, "tinh", node, "U1"), "fx", s1.Submission.ID, "accepted", "", "")
	f.want(err, "self_approval_forbidden", "tự rà soát")
	_, err = f.svc.Review(tinh, "fx", s2.Submission.ID, "returned", "", "")
	f.want(err, "validation_failed", "trả lại thiếu ghi chú")
	r1, err := f.svc.Review(tinh, "fx", s1.Submission.ID, "accepted", "", "")
	f.must(err, "chấp nhận U1")
	if r1.Status != domain.SubAccepted || f.scalar(`SELECT count(*) FROM pf_workflow.decision WHERE tenant_id = $1 AND subject_ref = $2 AND outcome = 'approved'`,
		tid, s1.Submission.ID) != 1 {
		t.Fatalf("rà soát → quyết định cap/workflow: %+v", r1)
	}
	_, err = f.svc.Review(tinh, "fx", s1.Submission.ID, "returned", "lần hai", "")
	f.want(err, "forms.already_reviewed", "rà soát hai lần")
	_, err = f.svc.Submit(xa1, "fx", pid, "U1", fx01, "")
	f.want(err, "forms.submission_accepted", "nộp lại sau chấp nhận")

	// Validator trước chốt: U2 chờ rà soát.
	_, err = f.svc.Close(tinh, "fx", pid, "")
	f.want(err, "forms.pending_review", "chốt khi còn bài chờ")
	_, err = f.svc.Review(tinh, "fx", s2.Submission.ID, "returned", "Diện tích đã gieo cần đối chiếu", "")
	f.must(err, "trả lại U2")

	// Chốt: cấp close_by của Nút phát hành.
	_, err = f.svc.Close(xa1, "fx", pid, "")
	f.want(err, "forbidden", "Xã chốt")
	_, err = f.svc.Close(f.as(tid, uuid.Nil, "tinh", ids.New(), "U1"), "fx", pid, "")
	f.want(err, "forbidden", "Tỉnh khác Nút chốt")
	cv, err := f.svc.Close(tinh, "fx", pid, "")
	f.must(err, "chốt")
	if cv.State.Status != domain.PeriodClosed || len(cv.Snapshots) != 1 || !cv.Snapshots[0].HashValid || cv.Snapshots[0].No != 1 || len(cv.Snapshots[0].Hash) != 32 {
		t.Fatalf("chốt + ảnh chụp: %+v %+v", cv.State, cv.Snapshots)
	}
	snap1, err := f.svc.Snapshot(tinh, "fx", cv.Snapshots[0].ID)
	f.must(err, "đọc ảnh 1")
	var c1 domain.Snapshot
	f.must(json.Unmarshal(snap1.Content, &c1), "nội dung ảnh 1")
	if c1.Schema != domain.SnapshotSchema || c1.TemplateVersion != 1 || len(c1.Units) != 2 || c1.Units[0].Status != domain.SubAccepted ||
		c1.Units[0].Values["area_sown"] != "80" || c1.Units[1].Status != domain.SubReturned {
		t.Fatalf("nội dung ảnh 1: %+v", c1)
	}
	_, err = f.svc.Close(tinh, "fx", pid, "")
	f.want(err, "forms.period_closed", "chốt hai lần")
	_, err = f.svc.Submit(xa2, "fx", pid, "U2", fx01, "")
	f.want(err, "forms.period_closed", "nộp sau chốt")
	_, err = f.svc.Review(tinh, "fx", s2.Submission.ID, "accepted", "", "")
	f.want(err, "forms.period_closed", "rà soát sau chốt")

	// Mở lại: lý do bắt buộc; một phần (U2) — U1 vẫn chốt.
	f.clk.Advance(time.Hour)
	_, err = f.svc.Reopen(tinh, "fx", pid, ReopenInput{Units: []string{"U2"}, Reason: "ngắn"}, "")
	f.want(err, "validation_failed", "mở lại thiếu lý do")
	_, err = f.svc.Reopen(tinh, "fx", pid, ReopenInput{Units: []string{"U9"}, Reason: "Xã bổ sung số liệu gieo trồng"}, "")
	f.want(err, "forms.unit_not_in_period", "mở lại đơn vị lạ")
	rv, err := f.svc.Reopen(tinh, "fx", pid, ReopenInput{Units: []string{"U2"}, Reason: "Xã bổ sung số liệu gieo trồng"}, "")
	f.must(err, "mở lại")
	if rv.State.Status != domain.PeriodReopened || len(rv.Events) != 3 {
		t.Fatalf("mở lại: %+v", rv.State)
	}
	_, err = f.svc.Reopen(tinh, "fx", pid, ReopenInput{Reason: "Mở lại lần nữa cho cả kỳ"}, "")
	f.want(err, "forms.period_not_closed", "mở lại khi đang mở")
	_, err = f.svc.Submit(xa1, "fx", pid, "U1", fx01, "")
	f.want(err, "forms.unit_not_in_period", "U1 ngoài phạm vi mở lại")
	s2b, err := f.svc.Submit(xa2, "fx", pid, "U2", map[string]string{"crop": "ngo", "area_planned": "10", "area_sown": "9.5", "report_date": "2026-10-04"}, "")
	f.must(err, "nộp lại U2")
	_, err = f.svc.Review(tinh, "fx", s2.Submission.ID, "accepted", "", "")
	f.want(err, "forms.submission_superseded", "rà soát bản cũ")
	_, err = f.svc.Review(tinh, "fx", s2b.Submission.ID, "accepted", "", "")
	f.must(err, "chấp nhận U2 bản 2")
	if h, err := f.svc.Submissions(tinh, "fx", pid, "U2"); err != nil || len(h) != 2 || h[0].Submission.Version != 2 || h[1].Status != domain.SubReturned {
		t.Fatalf("lịch sử U2: %+v %v", h, err)
	}
	cv2, err := f.svc.Close(tinh, "fx", pid, "")
	f.must(err, "chốt lại")
	if len(cv2.Snapshots) != 2 || cv2.Snapshots[1].No != 2 || !cv2.Snapshots[0].HashValid || !cv2.Snapshots[1].HashValid {
		t.Fatalf("chốt lại: ảnh mới, ảnh cũ nguyên: %+v", cv2.Snapshots)
	}
	again, err := f.svc.Snapshot(tinh, "fx", snap1.ID)
	f.must(err, "đọc lại ảnh 1")
	if string(again.Content) != string(snap1.Content) || string(again.Hash) != string(snap1.Hash) {
		t.Fatal("ảnh chụp 1 đã đổi sau mở lại/chốt lại")
	}
	snap2, err := f.svc.Snapshot(tinh, "fx", cv2.Snapshots[1].ID)
	f.must(err, "đọc ảnh 2")
	var c2 domain.Snapshot
	f.must(json.Unmarshal(snap2.Content, &c2), "nội dung ảnh 2")
	if c2.SnapshotNo != 2 || c2.Units[1].Values["area_sown"] != "9.5" || c2.Units[1].Version != 2 || c2.Units[1].Status != domain.SubAccepted {
		t.Fatalf("nội dung ảnh 2: %+v", c2.Units)
	}
	if n := f.scalar(`SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = 'pf.forms.period.closed'`, tid); n != 2 {
		t.Fatalf("sự kiện pf.forms.period.closed: %d", n)
	}

	// RLS: tenant khác không thấy kỳ, ảnh chụp.
	other := f.as(ids.New(), uuid.Nil, "tinh", node, "U1", "U2")
	_, err = f.svc.Period(other, "fx", pid)
	f.want(err, "forms.period_not_found", "tenant khác xem kỳ")
	_, err = f.svc.Snapshot(other, "fx", snap1.ID)
	f.want(err, "forms.snapshot_not_found", "tenant khác xem ảnh")
	if ps, err := f.svc.Periods(other, "fx", 10); err != nil || len(ps) != 0 {
		t.Fatalf("tenant khác liệt kê: %v %v", ps, err)
	}
	// Vai không thuộc profile.
	_, err = f.svc.Period(f.as(tid, uuid.Nil, "khach", uuid.Nil), "fx", pid)
	f.want(err, "forbidden", "vai lạ")
	_, err = f.svc.Period(tinh, "khong_co", pid)
	f.want(err, "forms.profile_not_found", "profile lạ")
}

// DB (pf 000121–000122): sổ chỉ ghi thêm (ảnh chụp không sửa/xóa được, kể cả chủ bảng); r_forms không có UPDATE;
// máy trạng thái kỳ, bài nộp vào kỳ đã chốt, bốn mắt của review — kể cả khi bỏ qua app/; băm do DB tính.
func TestFormsDBInvariants_W62(t *testing.T) {
	f := newFx(t)
	tid, node := f.tid, ids.New()
	admin := f.as(tid, uuid.Nil, "admin", uuid.Nil)
	tinh := f.as(tid, uuid.Nil, "tinh", node, "U1")
	xaRef := ids.New()
	xa := f.as(tid, xaRef, "xa", uuid.Nil, "U1")
	tpls, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "fixture")
	pv, err := f.svc.Publish(tinh, PublishInput{Profile: "fx", TemplateID: tpls[1].ID, Label: "Kỳ DB", DueAt: f.clk.Now(), Units: []string{"U1"}}, "")
	f.must(err, "phát hành")
	pid := pv.Period.ID
	sb, err := f.svc.Submit(xa, "fx", pid, "U1", map[string]string{"pest": "rầy nâu", "area_infected": "3.5"}, "")
	f.must(err, "nộp")

	own := func(q string, args ...any) error {
		return tenant.WithTenantTx(f.ctx, f.own, tid, func(tx pgx.Tx) error { _, e := tx.Exec(f.ctx, q, args...); return e })
	}
	// Bốn mắt ở DB: r_forms ghi thẳng review với người rà soát = người nộp (khai người nộp giả → trigger ghi đè).
	err = own(`INSERT INTO pf_forms.review (id, tenant_id, stream_key, submission_id, period_id, unit_code, submitter_ref, reviewer_ref, decision,
	    event_time, actor_ref, idem_key, rule_version) VALUES ($1, $2, 'review:' || $3::uuid::text, $3::uuid, $3::uuid, 'x', gen_random_uuid(), $4, 'accepted',
	    now(), $4, $5, 't')`, ids.New(), tid, sb.Submission.ID, xaRef, "direct:"+ids.New().String())
	if c, n := pgCode(err); c != "23514" || n != "review_four_eyes" {
		t.Fatalf("CHECK review_four_eyes: %s %s %v", c, n, err)
	}
	// Máy trạng thái: native 'reopened' khi đang mở → AGF01.
	err = own(`INSERT INTO pf_forms.period_event (id, tenant_id, stream_key, period_ref, profile, origin, action, mode, reason, event_time, actor_ref,
	    idem_key, rule_version) VALUES ($1, $2, 'period:' || $3::uuid::text, $3::uuid, 'fx', 'native', 'reopened', 'manual', 'mở lại thẳng ở DB', now(), $4, $5, 't')`,
		ids.New(), tid, pid, xaRef, "direct:"+ids.New().String())
	if c, _ := pgCode(err); c != "AGF01" {
		t.Fatalf("máy trạng thái kỳ: %s %v", c, err)
	}
	// Chốt rồi nộp thẳng ở DB → AGF02.
	_, err = f.svc.Review(tinh, "fx", sb.Submission.ID, "accepted", "", "")
	f.must(err, "rà soát")
	cv, err := f.svc.Close(tinh, "fx", pid, "")
	f.must(err, "chốt")
	err = own(`INSERT INTO pf_forms.submission (id, tenant_id, stream_key, period_id, unit_code, profile, template_id, "values", submitter_ref, event_time,
	    actor_ref, idem_key, rule_version) VALUES ($1, $2, 'sub:' || $3::uuid::text || ':U1', $3::uuid, 'U1', '', $3::uuid, '{}', $4, now(), $4, $5, 't')`,
		ids.New(), tid, pid, xaRef, "direct:"+ids.New().String())
	if c, _ := pgCode(err); c != "AGF02" {
		t.Fatalf("nộp vào kỳ đã chốt ở DB: %s %v", c, err)
	}
	snapID := cv.Snapshots[0].ID
	// r_forms không có UPDATE/DELETE.
	for _, q := range []string{`UPDATE pf_forms.close_snapshot SET content = '{"x":1}' WHERE id = $1`, `DELETE FROM pf_forms.close_snapshot WHERE id = $1`} {
		if c, _ := pgCode(own(q, snapID)); c != "42501" {
			t.Fatalf("r_forms %q: %s", q, c)
		}
	}
	// Chủ bảng (quản trị) cũng không sửa được: trigger fn_immutable của sổ.
	if _, err := f.env.AdminPool(t, migrations.PF).Exec(f.ctx, `UPDATE pf_forms.close_snapshot SET content = '{"x":1}' WHERE id = $1`, snapID); err == nil {
		t.Fatal("ảnh chụp sửa được bằng quản trị")
	}
	// Băm do DB tính, khai băm giả bị ghi đè.
	if n := f.scalar(`SELECT count(*) FROM pf_forms.close_snapshot WHERE id = $1 AND content_hash = sha256(convert_to(content::text, 'UTF8'))`, snapID); n != 1 {
		t.Fatal("content_hash không khớp nội dung")
	}
	// Ảnh chụp chỉ gắn sự kiện closed.
	err = own(`INSERT INTO pf_forms.close_snapshot (id, tenant_id, stream_key, period_ref, profile, period_event_id, content, content_hash, event_time,
	    actor_ref, idem_key, rule_version) VALUES ($1, $2, 'snapshot:' || $3::uuid::text, $3::uuid, '', $4, '{}', '\xdead', now(), $5, 'snapshot:' || $4::uuid::text, 't')`,
		ids.New(), tid, pid, cv.Events[0].ID, xaRef)
	if c, _ := pgCode(err); c != "AGF01" {
		t.Fatalf("ảnh chụp gắn sự kiện published: %s %v", c, err)
	}
	// Không còn gì ngoài sổ: v_missing_immutability / v_missing_rls rỗng cho pf_forms.
	if n := f.scalar(`SELECT count(*) FROM shared.v_missing_immutability WHERE schema_name = 'pf_forms'`); n != 0 {
		t.Fatalf("pf_forms thiếu chặn sửa/xóa: %d", n)
	}
	if n := f.scalar(`SELECT count(*) FROM shared.v_missing_rls WHERE schema_name = 'pf_forms'`); n != 0 {
		t.Fatalf("pf_forms thiếu RLS: %d", n)
	}
}

// Ghi song song (port.Freezer, pf_forms.mirror) trong giao dịch của bên dùng — vai r_extension (app_platform + db.ForModule — đúng cả trước và sau W2.6),
// chỉ EXECUTE: chốt + ảnh chụp, gọi lặp, mở lại, chốt lại (ảnh mới), rollback, thiếu giao dịch, không quyền bảng.
func TestFormsMirror_W62(t *testing.T) {
	f := newFx(t)
	tid, period, actor := f.tid, ids.New(), ids.New()
	caller := db.ForModule(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), db.Module("pf/extension"))
	rec := func(t2 uuid.UUID, in port.MirrorInput) (port.MirrorResult, error) {
		var r port.MirrorResult
		err := tenant.WithTenantTx(f.ctx, caller, t2, func(tx pgx.Tx) error {
			var e error
			r, e = f.svc.Record(db.JoinTx(f.ctx, tx), in)
			return e
		})
		return r, err
	}
	content := json.RawMessage(`{"schema":"kn.report_period/v1","tasks":[{"task_id":"a","status":"confirmed"}]}`)
	closeIn := port.MirrorInput{Profile: "fx", PeriodRef: period, Label: "Tuần 36", Action: port.ActionClosed, Mode: port.ModeAuto, Content: content,
		Actor: actor, IdemKey: "auto-close:1"}

	if _, err := f.svc.Record(f.ctx, closeIn); !errors.Is(err, port.ErrNoCallerTx) {
		t.Fatalf("thiếu giao dịch: %v", err)
	}
	_, err := rec(tid, port.MirrorInput{Profile: "khac", PeriodRef: period, Action: port.ActionClosed, Content: content, Actor: actor, IdemKey: "x"})
	f.want(err, "forms.profile_not_found", "profile lạ")
	_, err = rec(tid, port.MirrorInput{Profile: "fx", PeriodRef: period, Action: port.ActionClosed, Content: json.RawMessage(`[1]`), Actor: actor, IdemKey: "x"})
	f.want(err, "validation_failed", "nội dung không phải object")

	r1, err := rec(tid, closeIn)
	f.must(err, "chốt")
	if r1.Replayed || r1.SnapshotID == nil || r1.SnapshotNo != 1 || len(r1.ContentHash) != 32 {
		t.Fatalf("chốt: %+v", r1)
	}
	again, err := rec(tid, closeIn)
	f.must(err, "gọi lặp")
	if !again.Replayed || again.EventID != r1.EventID || *again.SnapshotID != *r1.SnapshotID {
		t.Fatalf("gọi lặp: %+v", again)
	}
	_, err = rec(tid, port.MirrorInput{Profile: "fx", PeriodRef: period, Action: port.ActionReopened, Units: []string{"30001"}, Reason: "ngắn", Actor: actor,
		IdemKey: "r0"})
	f.want(err, "validation_failed", "mở lại thiếu lý do")
	if _, err := rec(tid, port.MirrorInput{Profile: "fx", PeriodRef: period, Action: port.ActionReopened, Units: []string{"30001"},
		Reason: "Xã phát hiện nhập nhầm số tuần 1", Actor: actor, IdemKey: "r1"}); err != nil {
		t.Fatal(err)
	}
	// Mở lại thêm khi đang mở lại (KN mở lại từng xã × biểu) — được ghi.
	if _, err := rec(tid, port.MirrorInput{Profile: "fx", PeriodRef: period, Action: port.ActionReopened, Units: []string{"30002"},
		Reason: "Xã thứ hai cần bổ sung số liệu", Actor: actor, IdemKey: "r2"}); err != nil {
		t.Fatal(err)
	}
	r3, err := rec(tid, port.MirrorInput{Profile: "fx", PeriodRef: period, Action: port.ActionClosed, Mode: port.ModeAuto, Units: []string{"30001", "30002"},
		Content: json.RawMessage(`{"schema":"kn.report_period/v1","tasks":[]}`), Actor: actor, IdemKey: "auto-reclose:1"})
	f.must(err, "chốt lại")
	if r3.SnapshotNo != 2 || r3.Seq != 4 {
		t.Fatalf("chốt lại: %+v", r3)
	}
	// Rollback của bên dùng → không có gì.
	boom := errors.New("rollback")
	rolled := ids.New()
	err = tenant.WithTenantTx(f.ctx, caller, tid, func(tx pgx.Tx) error {
		if _, e := f.svc.Record(db.JoinTx(f.ctx, tx), port.MirrorInput{Profile: "fx", PeriodRef: rolled, Action: port.ActionClosed, Content: content,
			Actor: actor, IdemKey: "rb"}); e != nil {
			return e
		}
		return boom
	})
	if !errors.Is(err, boom) || f.scalar(`SELECT count(*) FROM pf_forms.period_event WHERE period_ref = $1`, rolled) != 0 {
		t.Fatalf("rollback: %v", err)
	}
	// Bên dùng không có quyền bảng pf_forms.
	err = tenant.WithTenantTx(f.ctx, caller, tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(f.ctx, `SELECT count(*) FROM pf_forms.close_snapshot`)
		return e
	})
	if c, _ := pgCode(err); c != "42501" {
		t.Fatalf("r_extension đọc thẳng pf_forms: %s %v", c, err)
	}
	// Đọc qua /v1/forms: cấp trên cấp lá thấy ảnh chụp kỳ ghi song song; cấp lá không.
	tinh := f.as(tid, uuid.Nil, "tinh", ids.New(), "30001")
	ss, err := f.svc.Snapshots(tinh, "fx", period)
	if err != nil || len(ss) != 2 || !ss[0].HashValid || !ss[1].HashValid {
		t.Fatalf("ảnh chụp mirror: %+v %v", ss, err)
	}
	_, err = f.svc.Snapshots(f.as(tid, uuid.Nil, "xa", uuid.Nil, "30001"), "fx", period)
	f.want(err, "forbidden", "cấp lá xem ảnh mirror")
	if n := f.scalar(`SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = 'pf.forms.period.closed'`, tid); n != 2 {
		t.Fatalf("sự kiện closed của mirror: %d", n)
	}
	// Không trộn kỳ native với mirror: mirror vào mã kỳ native → AGF01 (conflict).
	admin := f.as(tid, uuid.Nil, "admin", uuid.Nil)
	tpls, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "fixture")
	pv, err := f.svc.Publish(f.as(tid, uuid.Nil, "tinh", ids.New(), "U1"), PublishInput{Profile: "fx", TemplateID: tpls[0].ID, Label: "Kỳ native",
		DueAt: f.clk.Now(), Units: []string{"U1"}}, "")
	f.must(err, "phát hành native")
	_, err = rec(tid, port.MirrorInput{Profile: "fx", PeriodRef: pv.Period.ID, Action: port.ActionClosed, Content: content, Actor: actor, IdemKey: "mix"})
	f.want(err, "conflict", "mirror vào kỳ native")
}
