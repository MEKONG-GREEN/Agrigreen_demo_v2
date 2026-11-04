package adhocintegration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	report "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	native "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/knadhoc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func dashboard202Ledger(t *testing.T, f *reportFx) [8]int64 {
	t.Helper()
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	ctx := adhocOwnerContext(t, f, authz.KNTinh, khtcDept, f.khtcUser, false, false, uuid.Nil)
	var counts [8]int64
	err := tenant.WithTenantTx(ctx, pool, f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT
		 (SELECT count(*) FROM pf_extension.khtc_period),
		 (SELECT count(*) FROM pf_extension.khtc_period_event),
		 (SELECT count(*) FROM pf_extension.khtc_snapshot),
		 (SELECT count(*) FROM pf_extension.khtc_report_draft),
		 (SELECT count(*) FROM pf_extension.khtc_report_export),
		 (SELECT count(*) FROM pf_extension.khtc_form_sheet),
		 (SELECT count(*) FROM pf_extension.khtc_plan_version),
		 (SELECT count(*) FROM pf_extension.khtc_plan_entry)`).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5], &counts[6], &counts[7])
	})
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

type dashboard202ActualReader struct {
	extport.KHTCSources
	queries []extport.KHTCQuery
}

func (r *dashboard202ActualReader) Periods(ctx context.Context, tenant uuid.UUID, q extport.KHTCQuery) ([]extport.KHTCPeriod, error) {
	r.queries = append(r.queries, q)
	return r.KHTCSources.Periods(ctx, tenant, q)
}

func TestDashboard202ActualPublicClosedSourcesAndCutoff(t *testing.T) {
	f := newReportFx(t)
	reader := &dashboard202ActualReader{KHTCSources: f.ext.KHTCSources()}
	f.ext.UseKHTCSourceReader(reader)
	// Source setup goes through the real KN-owned native Forms profile and
	// capability ports. No private Forms SQL or fabricated closed figures.
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	fm, err := native.New(pool, f.clk, native.Deps{Profiles: extension.FormProfiles()})
	if err != nil {
		t.Fatal(err)
	}
	admin := adhocOwnerContext(t, f, authz.KNAdmin, "", uuid.New(), true, false, uuid.Nil)
	province := adhocOwnerContext(t, f, authz.KNTinh, dCNTS, uuid.New(), false, false, uuid.Nil)
	commune := adhocOwnerContext(t, f, authz.KNXa, dCNTS, uuid.New(), false, false, uuid.Nil)
	if err := tenant.WithTenantTx(admin, pgtest.Get(t).AdminPool(t, migrations.PF), f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(admin, `INSERT INTO k_ref.org(tenant_id,ref,kind,code,admin_unit_code,active,source_seq) VALUES($1,$2,'ttkn_province','TTKN202','89',true,1)`, f.tid, f.node)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	ref, err := fm.Publisher().DefineTemplate(admin, formsport.TemplateDef{Profile: "kn_cnts", Code: "LIVE202", Title: "Nguồn số đã chốt dashboard202", Fields: json.RawMessage(`[{"key":"amount","label":"Số thực hiện","type":"decimal","required":true}]`)}, "live202.define")
	if err != nil {
		t.Fatal(err)
	}
	date := func(raw string) time.Time {
		v, e := time.Parse(time.DateOnly, raw)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	periods, err := fm.Publisher().PublishBatch(province, formsport.BatchInput{Profile: "kn_cnts", TemplateID: ref.ID, UnitKind: formsport.UnitCommune, Units: []string{"X1"}, Periods: []formsport.PeriodDef{{Label: "Tuần41/2026", Kind: "week", Key: "2026-W41", From: date("2026-10-06"), To: date("2026-10-12"), DueAt: f.clk.Now().Add(48 * time.Hour), Rule: "kn.period_rule/v2;week_start=tue;week=year;month=week_end;cal=chan_nuoi_thuy_san:v3"}}}, "live202.publish")
	if err != nil || len(periods) != 1 {
		t.Fatalf("real native publish %+v %v", periods, err)
	}
	if _, err := fm.Entry().Submit(commune, "kn_cnts", periods[0].ID, "X1", map[string]string{"amount": "11"}, 0, "live202.submit"); err != nil {
		t.Fatal(err)
	}
	f.planBody["base_version"] = 2
	f.planBody["decision_no"] = "03/QD-LIVE202"
	for _, e := range f.planBody["entries"].([]map[string]any) {
		if e["ref"] == f.itemMod {
			e["department_code"] = dCNTS
			e["source_key"] = "sum:chan_nuoi_thuy_san:live202:amount"
		}
	}
	code, body := f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", f.planBody)
	f.ok(code, body, nil)
	read := func(raw string) report.Dashboard {
		t.Helper()
		before := dashboard202Ledger(t, f)
		var d report.Dashboard
		code, body := f.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4&through_date="+raw+"&department="+dCNTS, f.khtcUser, "", nil)
		f.ok(code, body, &d)
		if after := dashboard202Ledger(t, f); after != before {
			t.Fatalf("live read wrote owned ledger %v %v", before, after)
		}
		return d
	}
	row := func(d report.Dashboard) (bool, string) {
		t.Helper()
		for _, r := range d.Progress.Rows {
			if r.Ref.String() == f.itemMod {
				return r.Missing, r.YearToDate
			}
		}
		t.Fatal("real module item absent")
		return false, ""
	}
	if missing, value := row(read("2026-10-13")); !missing || value != "0" {
		t.Fatal("unclosed native source contributed a live quantity")
	}
	if err := fm.Control().ClosePeriod(province, "kn_cnts", periods[0].ID, "live202.close"); err != nil {
		t.Fatal(err)
	}
	if missing, n := row(read("2026-10-13")); missing || n != "11" {
		t.Fatalf("real public closed source lost missing%v value%s", missing, n)
	}
	currentPlan := read("2026-10-13").ProgressWindow.PlanVersionID
	if currentPlan == nil {
		t.Fatal("effective real plan version missing")
	}
	f.planBody["base_version"] = 3
	f.planBody["decision_no"] = "04/QD-LIVE202"
	f.planBody["decision_date"] = "2026-10-14"
	f.planBody["effective_from"] = "2026-10-20"
	for _, e := range f.planBody["entries"].([]map[string]any) {
		if e["ref"] == f.itemMod {
			e["planned_qty"] = "99"
		}
	}
	code, body = f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", f.planBody)
	f.ok(code, body, nil)
	if missing, _ := row(read("2026-10-06")); !missing {
		t.Fatal("future source week crossed selected quantity cutoff")
	}
	d := read("2026-12-31")
	if d.ProgressPeriod != nil || d.ProgressWindow == nil || d.ProgressWindow.ThroughDate != "2026-12-31" || d.ProgressWindow.QuantityTo != "2026-10-13" {
		t.Fatalf("future request invented period/forecast %+v", d.ProgressWindow)
	}
	if d.ProgressWindow.PlanVersionID == nil || *d.ProgressWindow.PlanVersionID != *currentPlan {
		t.Fatal("future request forecast a future plan version")
	}
	for _, r := range d.Progress.Rows {
		if r.Ref.String() == f.itemMod && (r.Planned == nil || *r.Planned != "5000") {
			t.Fatal("future plan target used before effective actual date")
		}
	}
	if len(reader.queries) == 0 {
		t.Fatal("actual public reader not called")
	}
	q := reader.queries[len(reader.queries)-1]
	if q.PriceAt == nil || !q.PriceAt.Equal(date("2026-10-14")) || q.ConfirmationAt == nil || !q.ConfirmationAt.Equal(f.clk.Now()) || q.Target.CalendarID != khtcDept {
		t.Fatalf("actual source cutoff/target %+v", q)
	}
	beforeCalls := len(reader.queries)
	code, body = f.khtc(http.MethodGet, "/dashboard?year=2027&quarter=1", f.khtcUser, "", nil)
	var future report.Dashboard
	f.ok(code, body, &future)
	if future.ProgressWindow != nil || len(reader.queries) != beforeCalls {
		t.Fatal("future empty quarter read sources or invented window")
	}
	beforePlanless := dashboard202Ledger(t, f)
	code, body = f.khtc(http.MethodGet, "/dashboard?year=2025&quarter=4", f.khtcUser, "", nil)
	var planless report.Dashboard
	f.ok(code, body, &planless)
	if planless.ProgressWindow == nil || planless.ProgressWindow.PlanVersionID != nil || len(planless.Progress.Rows) != 0 || planless.Counts.Items != 0 || dashboard202Ledger(t, f) != beforePlanless {
		t.Fatal("historical no-plan window invented a plan/item/write")
	}
	for _, raw := range []string{"through_date=1e100000", "through_date=1e-100000", "through_date=" + strings.Repeat("9", 100000), "year=" + strings.Repeat("9", 100000), "year=1e100000", "year=1e-100000", "quarter=1e100000", "quarter=1e-100000", "quarter=" + strings.Repeat("9", 100000), "through_date=2026-11-31", "through_date=2026-10-13&through_date=2026-10-06"} {
		code, _ = f.khtc(http.MethodGet, "/dashboard?"+raw, f.khtcUser, "", nil)
		if code != 422 {
			t.Fatalf("hostile query admitted %d (body omitted)", code)
		}
	}
	code, _ = f.do(http.MethodGet, "/v1/extension/khtc/dashboard?year=2026&quarter=4", authz.KNTinh, dTT, f.khtcUser, "", nil)
	if code != 403 {
		t.Fatalf("PN reader widened: %d", code)
	}
	other := newReportFx(t)
	other.ext.UseKHTCSourceReader(other.ext.KHTCSources())
	code, body = other.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4&through_date="+url.QueryEscape("2026-10-13"), other.khtcUser, "", nil)
	var foreign report.Dashboard
	other.ok(code, body, &foreign)
	for _, r := range foreign.Progress.Rows {
		if r.Ref.String() == f.itemMod {
			t.Fatal("cross-tenant source/plan leaked")
		}
	}
}

func TestDashboard202NoProgramPeriodReadsSentWeeklySheet(t *testing.T) {
	f := newReportFx(t)
	// Use the actual public-view reader for the live path, never the golden port
	// used by older report fixtures. This test's asserted number is native Forms.
	reader := &dashboard202ActualReader{KHTCSources: f.ext.KHTCSources()}
	f.ext.UseKHTCSourceReader(reader)
	week := f.open(41, dTT)
	code, body := f.send(week.ID, dTT, "submit", 0, ttSheet(f.item, "7"))
	f.ok(code, body, nil)
	before := dashboard202Ledger(t, f)
	var d report.Dashboard
	code, body = f.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4&through_date=2026-10-13&department="+dTT, f.khtcUser, "", nil)
	f.ok(code, body, &d)
	if d.ProgressPeriod != nil {
		t.Fatal("dashboard invented a reporting-period identity")
	}
	if after := dashboard202Ledger(t, f); after != before {
		t.Fatalf("GET appended ledger: before%v after%v", before, after)
	}
	found := false
	for _, row := range d.Progress.Rows {
		if row.Ref.String() == f.item {
			if row.Missing || row.YearToDate != "7" {
				t.Fatalf("real sent weekly sheet lost without M5 period: %+v", row)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("real sent weekly item absent solely because no M5 period was opened")
	}
	// The real national session retains its metadata reader, but does not gain
	// native kn_khtc submission permission as a side effect of the live feature.
	tw := adhocOwnerContext(t, f, authz.KNTW, khtcDept, f.khtcUser, false, false, uuid.Nil)
	_, denied := f.nativeEntry.Submissions(tw, "kn_khtc", uuid.MustParse(week.FormsPeriodID), dTT)
	var forbidden *problem.Problem
	if !errors.As(denied, &forbidden) || forbidden.Status != http.StatusForbidden || forbidden.Code != problem.Forbidden.Code {
		t.Fatalf("national native permission changed: %v", denied)
	}
	queries := len(reader.queries)
	for _, query := range []string{"year=2026&quarter=4", "year=2027&quarter=1"} {
		var national report.Dashboard
		code, body = f.do(http.MethodGet, "/v1/extension/khtc/dashboard?"+query, authz.KNTW, khtcDept, f.khtcUser, "", nil)
		f.ok(code, body, &national)
		if len(national.Progress.Rows) != 0 || len(national.Progress.Programs) != 0 || national.Progress.Missing != 0 || national.Counts.Items != 0 || national.Counts.Achieved != 0 || national.Counts.Slow != 0 || national.CanAct {
			t.Fatal("national unavailable live read leaked submitted numbers or claimed zero attainment")
		}
		if strings.Contains(query, "2026") {
			if national.ProgressWindow == nil || national.ProgressWindow.UnavailableReason != "native_forms_access_unavailable" || national.ProgressWindow.PlanVersionID != nil || national.Counts.Open != 1 || national.Counts.Forms != 1 || len(national.Status) != 1 || national.Status[0].PeriodID.String() != week.ID {
				t.Fatal("national metadata/unavailable window changed")
			}
		} else if national.ProgressWindow != nil {
			t.Fatal("future national selection invented a window to report unavailability")
		}
		if len(reader.queries) != queries || dashboard202Ledger(t, f) != before {
			t.Fatal("national unavailable/future read performed new source reads or appended ledger")
		}
	}
}

func TestDashboard202ExistingInvalidPlanKeysRemainMissing(t *testing.T) {
	// Preserve existing accepted legacy keys; do not rewrite the old fixtures or
	// broaden ParseSourceKey merely to make a dashboard read succeed.
	f := &reportFx{kfFx: newKHTCFormFxWith(t, nil)}
	reader := &dashboard202ActualReader{KHTCSources: f.ext.KHTCSources()}
	f.ext.UseKHTCSourceReader(reader)
	week := f.open(41, dTT)
	code, body := f.send(week.ID, dTT, "submit", 0, ttSheet(f.item, "7"))
	f.ok(code, body, nil)
	f.cw.unreadable = true
	before := dashboard202Ledger(t, f)
	code, body = f.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4", f.khtcUser, "", nil)
	var d report.Dashboard
	f.ok(code, body, &d)
	manual, unknown := false, 0
	for _, row := range d.Progress.Rows {
		if row.Ref.String() == f.item {
			manual = !row.Missing && row.YearToDate == "7"
		}
		if row.Ref.String() == f.itemMod || row.Ref.String() == f.price {
			if !row.Missing || row.YearToDate != "0" {
				t.Fatal("invalid legacy key fabricated a known module value")
			}
			unknown++
		}
	}
	if !manual || unknown != 2 || len(reader.queries) != 0 || dashboard202Ledger(t, f) != before {
		t.Fatal("invalid keys blocked actual manual items, requested unused sources, or wrote state")
	}
	known := 0
	for _, program := range d.Progress.Programs {
		known += program.Known
		if program.Percent != nil && *program.Percent != "7" {
			t.Fatal("unknown module inputs were averaged as real zeroes")
		}
	}
	if known != 1 || d.Progress.Missing < 2 {
		t.Fatal("invalid module inputs became known attainment")
	}
}

func TestDashboard202DraftReturnedCancelledFutureAndMetadata(t *testing.T) {
	f := newReportFx(t)
	f.ext.UseKHTCSourceReader(f.ext.KHTCSources())
	week := f.open(41, dTT)
	read := func(want string, missing bool) {
		t.Helper()
		before := dashboard202Ledger(t, f)
		var d report.Dashboard
		code, body := f.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4&through_date=2026-10-13&department="+dTT, f.khtcUser, "", nil)
		f.ok(code, body, &d)
		if d.ProgressPeriod != nil || d.ProgressWindow == nil {
			t.Fatal("no-period live identity/window wrong")
		}
		if after := dashboard202Ledger(t, f); after != before {
			t.Fatalf("GET wrote ledger %v %v", before, after)
		}
		for _, r := range d.Progress.Rows {
			if r.Ref.String() == f.item {
				if r.YearToDate != want || r.Missing != missing {
					t.Fatalf("live sent/cutoff policy got%s missing%v want%s missing%v", r.YearToDate, r.Missing, want, missing)
				}
				return
			}
		}
		t.Fatal("live item absent")
	}
	code, body := f.send(week.ID, dTT, "draft", 0, ttSheet(f.item, "50"))
	f.ok(code, body, nil)
	read("0", true)
	code, body = f.send(week.ID, dTT, "submit", 0, ttSheet(f.item, "7"))
	f.ok(code, body, nil)
	read("7", false)
	future := f.open(42, dTT)
	code, body = f.send(future.ID, dTT, "submit", 0, ttSheet(f.item, "80"))
	f.ok(code, body, nil)
	cancelled := f.open(40, dTT)
	code, body = f.send(cancelled.ID, dTT, "submit", 0, ttSheet(f.item, "3"))
	f.ok(code, body, nil)
	code, body = f.khtc(http.MethodPost, "/periods/"+cancelled.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy nguồn tuần trong regression dashboard202"})
	f.ok(code, body, nil)
	read("7", false)
	code, body = f.khtc(http.MethodPost, "/periods/"+week.ID+"/forms/"+dTT+"/review", f.khtcUser, "", map[string]any{"decision": "returned", "note": "Bổ sung lại số liệu dashboard202"})
	f.ok(code, body, nil)
	read("0", true)
	code, body = f.send(week.ID, dTT, "submit", 1, ttSheet(f.item, "9"))
	f.ok(code, body, nil)
	read("9", false)
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	ctx := adhocOwnerContext(t, f, authz.KNTinh, khtcDept, f.khtcUser, false, false, uuid.Nil)
	for start := 1; start <= 10001; start += 500 {
		end := min(start+499, 10001)
		if err := tenant.WithTenantTx(ctx, pool, f.tid, func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_period
 (id,tenant_id,stream_key,report_type,plan_year,name,period_from,period_to,cutoff_date,due_at,export_due_at,max_submissions,departments,forms_period_ref,template_code,period_rule,event_time,actor_ref,idem_key,rule_version)
 SELECT x.id,p.tenant_id,'khtc_period:'||x.id::text,'adhoc',p.plan_year,'Metadata ngoài live202',p.period_from,p.period_to,p.cutoff_date,p.due_at,p.export_due_at,p.max_submissions,ARRAY['chan_nuoi_thuy_san'],gen_random_uuid(),p.template_code,p.period_rule,p.event_time,p.actor_ref,'dashboard202:flood:'||x.id::text,p.rule_version
 FROM pf_extension.khtc_period p CROSS JOIN LATERAL(SELECT ('ffffffff-2020-7000-8000-'||lpad(n::text,12,'0'))::uuid id FROM generate_series($2::int,$3::int)n)x WHERE p.id=$1`, uuid.MustParse(week.ID), start, end)
			return e
		}); err != nil {
			t.Fatal(fmt.Errorf("owned unrelated metadata %d: %w", start, err))
		}
	}
	read("9", false)
	code, body = f.khtc(http.MethodPost, "/periods/"+week.ID+"/forms/"+dTT+"/review", f.khtcUser, "", map[string]any{"decision": "returned", "note": "Xác minh giá trị 0 thực tế dashboard202"})
	f.ok(code, body, nil)
	code, body = f.send(week.ID, dTT, "submit", 2, ttSheet(f.item, "0"))
	f.ok(code, body, nil)
	read("0", false)
}
