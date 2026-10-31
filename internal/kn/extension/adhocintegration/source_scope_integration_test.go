package adhocintegration_test

import (
	"encoding/json"
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	report "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/http"
	"testing"
)

func TestW1168AdhocSourcesNotStarvedByUnrelatedPeriods(t *testing.T) {
	f := newReportFx(t)
	week := f.open(41, dTT)
	code, body := f.send(week.ID, dTT, "submit", 0, ttSheet(f.item, "7"))
	f.ok(code, body, nil)
	target := f.openAdhoc("Nguồn tuần không bị giới hạn kỳ ngoài che khuất")
	read := func(version int64) int64 {
		t.Helper()
		code, body := f.do(http.MethodPost, "/v1/extension/khtc/periods/"+target.ID+"/report/refresh", authz.KNTinh, khtcDept, f.khtcUser, "", map[string]any{"base_version": version})
		f.ok(code, body, nil)
		var dto struct {
			Version int64        `json:"version"`
			Content report.Draft `json:"content"`
		}
		if e := json.Unmarshal(body, &dto); e != nil {
			t.Fatal(e)
		}
		for _, row := range dto.Content.Data.Rows {
			if row.Ref.String() == f.item {
				if row.InPeriod != "7" {
					t.Fatalf("live weekly source disappeared: %s", row.InPeriod)
				}
				return dto.Version
			}
		}
		t.Fatal("manual source row missing")
		return 0
	}
	version := read(0)
	pool := db.ForModule(pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform), extension.DBModule)
	ctx := adhocOwnerContext(t, f, authz.KNTinh, khtcDept, f.khtcUser, false, false, uuid.Nil)
	// Synthetic OWN-ledger metadata only: no fabricated native submissions enter
	// capture. IDs sort after the real week, reproducing the old LIMIT10000 bug.
	for start := 1; start <= 10001; start += 500 {
		end := min(start+499, 10001)
		e := tenant.WithTenantTx(ctx, pool, f.tid, func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_period
 (id,tenant_id,stream_key,report_type,plan_year,name,period_from,period_to,cutoff_date,due_at,export_due_at,max_submissions,departments,forms_period_ref,template_code,period_rule,event_time,actor_ref,idem_key,rule_version)
 SELECT x.id,p.tenant_id,'khtc_period:'||x.id::text,'adhoc',p.plan_year,'Kỳ metadata ngoài nguồn kiểm thử',p.period_from,p.period_to,p.cutoff_date,p.due_at,p.export_due_at,p.max_submissions,p.departments,gen_random_uuid(),p.template_code,p.period_rule,p.event_time,p.actor_ref,'w1168:flood:'||x.id::text,p.rule_version
 FROM pf_extension.khtc_period p CROSS JOIN LATERAL(
 SELECT ('ffffffff-ffff-7000-8000-'||lpad(n::text,12,'0'))::uuid AS id FROM generate_series($2::int,$3::int)n
 )x WHERE p.id=$1`, uuid.MustParse(target.ID), start, end)
			return e
		})
		if e != nil {
			t.Fatal(fmt.Errorf("owned metadata fixture %d: %w", start, e))
		}
	}
	read(version)
}
