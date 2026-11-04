package adhocintegration_test

import (
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	report "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func dashboard202Ledger(t *testing.T, f *reportFx) [6]int64 {
	t.Helper()
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	ctx := adhocOwnerContext(t, f, authz.KNTinh, khtcDept, f.khtcUser, false, false, uuid.Nil)
	var counts [6]int64
	err := tenant.WithTenantTx(ctx, pool, f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT
		 (SELECT count(*) FROM pf_extension.khtc_period),
		 (SELECT count(*) FROM pf_extension.khtc_period_event),
		 (SELECT count(*) FROM pf_extension.khtc_snapshot),
		 (SELECT count(*) FROM pf_extension.khtc_report_draft),
		 (SELECT count(*) FROM pf_extension.khtc_report_export),
		 (SELECT count(*) FROM pf_extension.khtc_form_sheet)`).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5])
	})
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

func TestDashboard202NoProgramPeriodReadsSentWeeklySheet(t *testing.T) {
	f := newReportFx(t)
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
	for _, row := range d.Progress.Rows {
		if row.Ref.String() == f.item {
			if row.Missing || row.YearToDate != "7" {
				t.Fatalf("real sent weekly sheet lost without M5 period: %+v", row)
			}
			return
		}
	}
	t.Fatal("real sent weekly item absent solely because no M5 period was opened")
}
