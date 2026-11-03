package adhocintegration_test

import (
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	periodstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	pa "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDashboard189Mau5NotStarvedByUnrelatedMetadata(t *testing.T) {
	f := newReportFx(t)
	week := f.open(41, dTT)
	code, body := f.send(week.ID, dTT, "submit", 0, ttSheet(f.item, "7"))
	f.ok(code, body, nil)
	var program khtcPeriod
	code, body = f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{"report_type": "program", "year": 2026, "as_of_date": "2026-10-13", "departments": []string{dTT}, "due_at": "2026-10-20T03:00:00Z", "export_due_at": "2026-10-21T09:00:00Z"})
	f.ok(code, body, &program)
	code, body = f.send(program.ID, dTT, "submit", 0, map[string]any{"texts": map[string]string{"cong_tac_khac": "Báo cáo tiến độ Mẫu5"}})
	f.ok(code, body, nil)
	read := func() pa.Dashboard {
		t.Helper()
		var d pa.Dashboard
		code, body := f.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4&department="+dTT, f.khtcUser, "", nil)
		f.ok(code, body, &d)
		if d.ProgressPeriod == nil || d.ProgressPeriod.ID.String() != program.ID {
			t.Fatal("real M5 period missing")
		}
		for _, row := range d.Progress.Rows {
			if row.Ref.String() == f.item {
				if row.Missing || row.YearToDate != "7" {
					t.Fatalf("M5 lost submitted week: %+v", row)
				}
				return d
			}
		}
		t.Fatal("M5 numeric item missing")
		return d
	}
	before := read()
	metadata := f.openAdhoc("Nguồn metadata ngoài phạm vi M5")
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	ctx := adhocOwnerContext(t, f, authz.KNTinh, khtcDept, f.khtcUser, false, false, uuid.Nil)
	// Owned metadata only, intentionally outside department/source selection. No
	// native submissions or values are fabricated. Neither paging nor capture
	// should call a native port for these unrelated ledger references.
	for start := 1; start <= 10001; start += 500 {
		end := min(start+499, 10001)
		err := tenant.WithTenantTx(ctx, pool, f.tid, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_period
 (id,tenant_id,stream_key,report_type,plan_year,name,period_from,period_to,cutoff_date,due_at,export_due_at,max_submissions,departments,forms_period_ref,template_code,period_rule,event_time,actor_ref,idem_key,rule_version)
 SELECT x.id,p.tenant_id,'khtc_period:'||x.id::text,'adhoc',p.plan_year,'Metadata CNTS ngoài nguồn M5',p.period_from,p.period_to,p.cutoff_date,p.due_at,p.export_due_at,p.max_submissions,ARRAY['chan_nuoi_thuy_san'],gen_random_uuid(),p.template_code,p.period_rule,p.event_time,p.actor_ref,'dashboard189:flood:'||x.id::text,p.rule_version
 FROM pf_extension.khtc_period p CROSS JOIN LATERAL(
 SELECT ('ffffffff-ffff-7000-8000-'||lpad(n::text,12,'0'))::uuid AS id FROM generate_series($2::int,$3::int)n
 )x WHERE p.id=$1`, uuid.MustParse(metadata.ID), start, end)
			return err
		})
		if err != nil {
			t.Fatal(fmt.Errorf("owned metadata fixture%d: %w", start, err))
		}
	}
	after := read()
	if after.Progress.Items != before.Progress.Items || after.Progress.Achieved != before.Progress.Achieved || after.Progress.Slow != before.Progress.Slow || after.Progress.Missing != before.Progress.Missing {
		t.Fatalf("unrelated metadata changed M5 KPI before%+v after%+v", before.Counts, after.Counts)
	}
	if !reflect.DeepEqual(before.Progress, after.Progress) {
		t.Fatal("unrelated metadata altered exact M5 progress/mean")
	}
	for _, query := range []string{"year=1e100000", "year=2026&quarter=1e-100000", "year=2026&quarter=4&department=" + strings.Repeat("9", 10000), "year=2026&quarter=4&field=" + strings.Repeat("9", 10000)} {
		code, body = f.khtc(http.MethodGet, "/dashboard?"+query, f.khtcUser, "", nil)
		if code != http.StatusUnprocessableEntity {
			t.Fatalf("malformed dashboard filter admitted: %d %s", code, body)
		}
	}

}

func TestDashboard189ReportInputsKeepProgramCutoffAndCancellation(t *testing.T) {
	f := newReportFx(t)
	cancelled := f.open(40, dTT)
	week := f.open(41, dTT)
	f.open(42, dTT)
	code, body := f.khtc(http.MethodPost, "/periods/"+cancelled.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ nguồn để kiểm bộ lọc báo cáo"})
	f.ok(code, body, nil)
	var program khtcPeriod
	code, body = f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{"report_type": "program", "year": 2026, "as_of_date": "2026-10-13", "cutoff_date": "2026-10-10", "departments": []string{dTT}, "due_at": "2026-10-20T03:00:00Z", "export_due_at": "2026-10-21T09:00:00Z"})
	f.ok(code, body, &program)
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	ctx := adhocOwnerContext(t, f, authz.KNTinh, khtcDept, f.khtcUser, false, false, uuid.Nil)
	end := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	err := tenant.WithTenantTx(ctx, pool, f.tid, func(tx pgx.Tx) error {
		rows, err := periodstore.ReportInputPeriods(ctx, tx, 2026, uuid.MustParse(program.ID), end, true)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID.String() != program.ID {
			t.Fatalf("current program must survive its earlier quantity cutoff: %+v", rows)
		}
		rows, err = periodstore.ReportInputPeriods(ctx, tx, 2026, uuid.MustParse(program.ID), end, false)
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Fatal("non-program future current leaked")
		}
		rows, err = periodstore.ReportInputPeriods(ctx, tx, 2026, uuid.MustParse(program.ID), end.AddDate(0, 0, 3), true)
		if err != nil {
			return err
		}
		found := map[uuid.UUID]bool{}
		for _, row := range rows {
			found[row.ID] = true
		}
		if len(rows) != 2 || !found[uuid.MustParse(week.ID)] || !found[uuid.MustParse(program.ID)] {
			t.Fatalf("cancelled/future week included or valid week dropped: %+v", rows)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
