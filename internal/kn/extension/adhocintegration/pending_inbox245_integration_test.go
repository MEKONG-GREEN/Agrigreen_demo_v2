package adhocintegration_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	periodapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	perioddomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	periodstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestKHTCPendingInbox245_AdhocLatestUnreviewed(t *testing.T) {
	f := newReportFx(t)
	p := f.openAdhoc("Phát sinh hộp phiếu 245")
	code, body := f.send(p.ID, dTT, "submit", 0, map[string]any{"texts": map[string]string{"cong_tac_khac": "Số liệu phát sinh đã đối chiếu"}})
	f.ok(code, body, nil)
	var out inbox243
	code, body = f.khtc(http.MethodGet, "/pending-review?report_type=adhoc&period_id="+p.ID+"&department="+dTT, f.khtcUser, "", nil)
	f.ok(code, body, &out)
	if len(out.Rows) != 1 || out.Rows[0].PeriodID != p.ID || out.Rows[0].Department != dTT || out.Rows[0].Attempt != 1 || out.Rows[0].SubmissionID == "" {
		t.Fatalf("actual latest unreviewed adhoc submission missing: %+v", out)
	}
}

func readPending245(t *testing.T, f *reportFx, filter string) inbox243 {
	t.Helper()
	var out inbox243
	code, body := f.khtc(http.MethodGet, "/pending-review?report_type=adhoc"+filter, f.khtcUser, "", nil)
	f.ok(code, body, &out)
	if out.Rows == nil {
		t.Fatal("pending rows must be an array")
	}
	return out
}

func TestKHTCPendingInbox245_RequestLatestDraftDecisionsCancellationAndTenant(t *testing.T) {
	f := newReportFx(t)
	p := f.openAdhoc("Phát sinh vòng đời 245")
	code, body := f.send(p.ID, dTT, "draft", 0, map[string]any{"texts": map[string]string{"cong_tac_khac": "Nháp chưa gửi"}})
	f.ok(code, body, nil)
	if out := readPending245(t, f, ""); len(out.Rows) != 0 {
		t.Fatalf("draft counted: %+v", out)
	}
	code, body = f.send(p.ID, dTT, "submit", 0, map[string]any{"texts": map[string]string{"cong_tac_khac": "Số liệu đã gửi"}})
	f.ok(code, body, nil)
	first := readPending245(t, f, "&period_id="+p.ID+"&department="+dTT)
	if len(first.Rows) != 1 {
		t.Fatalf("native first submission missing: %+v", first)
	}
	if out := readInbox243(t, f.kfFx, ""); len(out.Rows) != 0 {
		t.Fatal("adhoc leaked into regular default")
	}
	if out := readPending245(t, f, "&department="+dHCTH); len(out.Rows) != 0 {
		t.Fatal("unassigned department leaked")
	}
	r := f.request(p.ID, "pending245-request")
	if out := readPending245(t, f, ""); len(out.Rows) != 0 {
		t.Fatalf("returned native request stayed pending: %+v", out)
	}
	code, body = f.send(p.ID, dTT, "draft", 1, answerSheet(r.ID, "2"))
	f.ok(code, body, nil)
	if out := readPending245(t, f, ""); len(out.Rows) != 0 {
		t.Fatal("answer draft counted as submitted")
	}
	code, body = f.send(p.ID, dTT, "submit", 1, answerSheet(r.ID, "2"))
	f.ok(code, body, nil)
	latest := readPending245(t, f, "&period_id="+p.ID)
	if len(latest.Rows) != 1 || latest.Rows[0].Attempt != 2 || latest.Rows[0].SubmissionID == first.Rows[0].SubmissionID {
		t.Fatalf("request completion did not use actual latest identity: %+v", latest)
	}
	ctx := adhocOwnerContext(t, f, authz.KNTinh, khtcDept, f.khtcUser, false, false, uuid.Nil)
	native, err := f.nativeControl.Progress(ctx, perioddomain.ProfileKHTC, uuid.MustParse(p.FormsPeriodID))
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, unit := range native.Units {
		if unit.Unit == dTT && unit.LastSubmissionID != nil && unit.LastSubmissionID.String() == latest.Rows[0].SubmissionID {
			matched = true
		}
	}
	if !matched {
		t.Fatal("inbox identity is not the actual native latest submission")
	}
	code, body = f.reviewForm241(p.ID, dTT, "accepted", "")
	f.ok(code, body, nil)
	if out := readPending245(t, f, ""); len(out.Rows) != 0 {
		t.Fatal("accepted remained pending")
	}
	p = f.openAdhoc("Phát sinh hủy 245")
	r = f.request(p.ID, "cancel245-request")
	code, body = f.send(p.ID, dTT, "submit", 0, answerSheet(r.ID, "3"))
	f.ok(code, body, nil)
	if out := readPending245(t, f, "&period_id="+p.ID); len(out.Rows) != 1 {
		t.Fatalf("pre-cancel native response missing: %+v", out)
	}
	code, body = f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ phát sinh kiểm thử hộp phiếu"})
	f.ok(code, body, nil)
	if out := readPending245(t, f, "&period_id="+p.ID); len(out.Rows) != 0 {
		t.Fatal("cancelled period/request stayed in pending inbox")
	}
	code, body = f.do(http.MethodGet, formPath(p.ID, dTT), authz.KNTinh, dTT, uuid.Nil, "", nil)
	if code != http.StatusNotFound {
		t.Fatalf("cancelled member form exposed: HTTP%d %s", code, body)
	}
	other := newReportFx(t)
	if out := readPending245(t, other, "&period_id="+p.ID); len(out.Rows) != 0 {
		t.Fatal("tenant leaked")
	}
}

type pendingProbe245 struct {
	forms.PeriodControl
	t                      *testing.T
	pool                   *pgxpool.Pool
	tenant, actor, session uuid.UUID
	calls                  int
	deny                   bool
	change                 func(*forms.PeriodProgress)
}

func (p *pendingProbe245) Progress(ctx context.Context, profile string, id uuid.UUID) (forms.PeriodProgress, error) {
	p.t.Helper()
	p.calls++
	if p.pool.Stat().AcquiredConns() != 0 {
		p.t.Fatal("native Progress ran inside KN transaction")
	}
	principal, k, err := ext.RequireSession(ctx)
	if err != nil || principal.TenantID != p.tenant || principal.ActorRef != p.actor || k.ID != p.session || k.Role != ext.SessionRoleProvince || k.Department != khtcDept || principal.MFA || k.ReadOnly() {
		p.t.Fatal("native Progress caller was changed", err)
	}
	if p.deny {
		return forms.PeriodProgress{}, forms.ErrPeriodNotFound
	}
	out, err := p.PeriodControl.Progress(ctx, profile, id)
	if err == nil && p.change != nil {
		out.Units = append([]forms.UnitProgress(nil), out.Units...)
		p.change(&out)
	}
	return out, err
}

func TestKHTCPendingInbox245_FiltersBeforeLimitBudgetPool1AndNativeAuthority(t *testing.T) {
	f := newReportFx(t)
	first := f.openAdhoc("Phát sinh cũ nhất cần rà soát")
	code, body := f.send(first.ID, dTT, "submit", 0, map[string]any{"texts": map[string]string{"cong_tac_khac": "Đã gửi"}})
	f.ok(code, body, nil)
	for i := 0; i < 26; i++ {
		var p khtcPeriod
		code, body = f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{"report_type": "adhoc", "year": 2026, "name": fmt.Sprintf("Phát sinh phòng khác %d", i), "from_date": "2026-10-01", "to_date": "2026-10-13", "due_at": "2026-10-20T03:00:00Z", "departments": []string{dTTTT}})
		f.ok(code, body, &p)
	}
	// Regular periods cannot consume an adhoc window either.
	f.open(41, dTT)
	pool := pgtest.Get(t).SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	ctx := adhocOwnerContext(t, f, authz.KNTinh, khtcDept, f.khtcUser, false, false, uuid.Nil)
	_, session, err := ext.RequireSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	probe := &pendingProbe245{PeriodControl: f.nativeControl, t: t, pool: pool, tenant: f.tid, actor: f.khtcUser, session: session.ID}
	svc := periodapp.Service{DB: db.ForModule(pool, extension.DBModule), Clock: f.clk, Forms: f.nativeForms, Control: probe}
	run := func(q periodapp.PendingReviewQuery) periodapp.PendingReviewPage {
		t.Helper()
		before := probe.calls
		out, e := svc.PendingReviewFiltered(ctx, q)
		if e != nil {
			t.Fatal(e)
		}
		if probe.calls-before > 25 {
			t.Fatal("native read budget increased")
		}
		return out
	}
	q := periodapp.PendingReviewQuery{ReportType: "adhoc"}
	out := run(q)
	if len(out.Rows) != 0 || out.NextCursor == nil || probe.calls != 25 {
		t.Fatalf("empty native window/cursor/budget: %+v calls%d", out, probe.calls)
	}
	q.Before = *out.NextCursor
	out = run(q)
	if len(out.Rows) != 1 || out.Rows[0].PeriodID.String() != first.ID || out.NextCursor != nil {
		t.Fatalf("later native page lost: %+v", out)
	}
	q = periodapp.PendingReviewQuery{ReportType: "adhoc", Department: dTT}
	before := probe.calls
	out = run(q)
	if len(out.Rows) != 1 || out.NextCursor != nil || probe.calls-before != 1 {
		t.Fatalf("department filter happened after LIMIT: %+v calls%d", out, probe.calls-before)
	}
	q = periodapp.PendingReviewQuery{ReportType: "adhoc", PeriodID: uuid.MustParse(first.ID)}
	before = probe.calls
	out = run(q)
	if len(out.Rows) != 1 || probe.calls-before != 1 {
		t.Fatal("exact period filter happened after LIMIT")
	}
	probe.deny = true
	_, err = svc.PendingReviewFiltered(ctx, q)
	assertAdhocStatus(t, err, http.StatusNotFound)
	probe.deny = false
	for _, change := range []func(*forms.PeriodProgress){
		func(v *forms.PeriodProgress) { v.ID = ids.New() }, func(v *forms.PeriodProgress) { v.Profile = "other" },
		func(v *forms.PeriodProgress) { v.Status = "unknown" },
		func(v *forms.PeriodProgress) { v.Units[0].Status = "unknown" },
		func(v *forms.PeriodProgress) { v.Units[0].Versions = 0 }, func(v *forms.PeriodProgress) { v.Units[0].LastSubmissionID = nil },
		func(v *forms.PeriodProgress) { v.Units = append(v.Units, v.Units[0]) },
	} {
		probe.change = change
		_, err = svc.PendingReviewFiltered(ctx, q)
		assertAdhocStatus(t, err, http.StatusServiceUnavailable)
	}
	probe.change = nil
	expired := adhocLeaseContext(t, ctx, ids.New(), f.clk.Now().Add(-time.Second), nil)
	before = probe.calls
	_, err = svc.PendingReviewFiltered(expired, q)
	assertAdhocStatus(t, err, http.StatusForbidden)
	if probe.calls != before {
		t.Fatal("expired session reached native reader")
	}
	if out = run(q); len(out.Rows) != 1 {
		t.Fatal("native authority recovery used stale facts")
	}
}

func TestKHTCPendingInbox245_AuthorityQueriesAndUncertainRequestFailClosed(t *testing.T) {
	f := newReportFx(t)
	p := f.openAdhoc("Phát sinh quyền và yêu cầu chưa đối chiếu")
	code, body := f.send(p.ID, dTT, "submit", 0, map[string]any{"texts": map[string]string{"cong_tac_khac": "Đã gửi"}})
	f.ok(code, body, nil)
	path := "/v1/extension/khtc/pending-review?report_type=adhoc&period_id=" + p.ID
	for _, tc := range []struct {
		role, dept    string
		readonly, mfa bool
		status        int
	}{
		{authz.KNTinh, khtcDept, false, false, 200}, {authz.KNTinh, khtcDept, true, false, 403},
		{authz.KNAdmin, "", false, true, 200}, {authz.KNAdmin, "", true, true, 403}, {authz.KNAdmin, "", false, false, 403},
		{authz.KNTinh, dTT, false, false, 403}, {authz.KNTW, khtcDept, false, false, 403}, {authz.KNXa, dTT, false, false, 403},
	} {
		ctx := adhocOwnerContext(t, f, tc.role, tc.dept, f.khtcUser, tc.mfa, tc.readonly, uuid.Nil)
		code, body = recoveryHTTP(t, f, ctx, "GET", path, nil)
		if code != tc.status {
			t.Fatalf("authority %s/%s readonly%t mfa%t HTTP%d %s", tc.role, tc.dept, tc.readonly, tc.mfa, code, body)
		}
		if code != 200 && bytes.Contains(body, []byte(`"rows"`)) {
			t.Fatal("denial exposed rows")
		}
	}
	for _, bad := range []string{"report_type=other", "report_type=adhoc&report_type=regular", "report_type=", "department=UPPER", "department=trong_trot&department=trong_trot", "department=" + strings.Repeat("a", 100000), "period_id=1e100000", "period_id=00000000-0000-0000-0000-000000000000", "period_id=" + p.ID + "&period_id=" + p.ID, "before=" + strings.Repeat("9", 100000)} {
		code, body = f.khtc(http.MethodGet, "/pending-review?"+bad, f.khtcUser, "", nil)
		if code != 400 || bytes.Contains(body, []byte(`"rows"`)) {
			t.Fatalf("bad filter was not rejected before query: HTTP%d", code)
		}
	}
	// Own immutable request intent cannot hide a real undecided native target.
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	err := tenant.WithTenantTx(context.Background(), pool, f.tid, func(tx pgx.Tx) error {
		req := periodstore.AdhocRequest{Indicators: []perioddomain.AdhocIndicator{}, ID: ids.New(), PeriodID: uuid.MustParse(p.ID), Department: dTT, Instruction: "Đang phát hành yêu cầu", DueAt: f.clk.Now().Add(time.Hour), PayloadHash: strings.Repeat("a", 64), BaseAttempt: 1, ActorRef: f.khtcUser, At: f.clk.Now(), IdemKey: "pending245-intent"}
		return periodstore.InsertAdhocRequest(context.Background(), tx, f.tid, &req)
	})
	if err != nil {
		t.Fatal(err)
	}
	code, body = f.khtc(http.MethodGet, "/pending-review?report_type=adhoc&period_id="+p.ID, f.khtcUser, "", nil)
	if code != 503 || bytes.Contains(body, []byte(`"rows"`)) {
		t.Fatalf("uncertain request became empty/native proof: HTTP%d %s", code, body)
	}
}
