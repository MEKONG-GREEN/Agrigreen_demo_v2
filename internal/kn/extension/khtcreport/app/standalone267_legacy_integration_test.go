package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	perioddomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	periodstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	sourceapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/app"
	templateapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/app"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type standalone267Plans struct{ version int }

func (p *standalone267Plans) PlanAt(_ context.Context, year int, _ time.Time) (port.KHTCPlan, error) {
	return port.KHTCPlan{Year: year, Version: p.version, Entries: []port.KHTCPlanEntry{}}, nil
}

type standalone267Sources struct{}

func (standalone267Sources) Periods(context.Context, uuid.UUID, port.KHTCQuery) ([]port.KHTCPeriod, error) {
	return []port.KHTCPeriod{}, nil
}

func TestStandalone267FreshCaptureIgnoresLegacyAndFrozenBytesSurvive(t *testing.T) {
	base := context.Background()
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	database := db.ForModule(pool, db.Module("pf/extension"))
	tid, actor := ids.New(), ids.New()
	session, err := port.NewSessionContext(port.SessionContext{ID: ids.New(), Role: port.SessionRoleProvince, Department: "ke_hoach_tai_chinh", OrgRef: ids.New(), Scope: authz.Scope{All: true}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenant.WithID(authz.WithPrincipal(base, authz.Principal{TenantID: tid, ActorRef: actor, SelectedSession: &session}), tid)
	day := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	no, key := 41, "2026-W41"
	clk := clock.NewFixed(day)
	period := periodstore.Period{ID: ids.New(), Type: "week", Year: 2026, No: &no, Key: &key, Name: "Kỳ kiểm tra lịch sử267", From: day, To: day.AddDate(0, 0, 6), Cutoff: day.AddDate(0, 0, 6), DueAt: day.AddDate(0, 0, 7), ExportDueAt: day.AddDate(0, 0, 8), MaxSubmissions: 3, Departments: []string{"trong_trot"}, FormsPeriodRef: ids.New(), TemplateCode: perioddomain.TemplateCode, Rule: perioddomain.RuleVersion, ActorRef: actor, EventTime: day}
	if err := tenant.WithTenantTx(ctx, database, tid, func(tx pgx.Tx) error { return periodstore.InsertPeriod(ctx, tx, tid, &period, ids.New().String()) }); err != nil {
		t.Fatal(err)
	}
	sections, _ := json.Marshal(domain.Sections("mau_1"))
	template, err := templatedomain.LegacySnapshot("mau_1", domain.RuleVersion, sections, WordTemplate("mau_1"))
	if err != nil {
		t.Fatal(err)
	}
	templates := &templateapp.Service{DB: database, Clock: clk, LegacyTemplates: map[string]port.KHTCTemplateSnapshot{"mau_1": template}}
	plans := &standalone267Plans{version: 1}
	svc := Service{DB: database, Clock: clk, Forms: emptySubmittedForms{}, Plans: plans, Templates: templates, Sources: &sourceapp.Service{DB: database, Clock: clk, Sources: standalone267Sources{}}}
	original, err := svc.Get(ctx, period.ID)
	if err != nil {
		t.Fatal(err)
	}
	admin := env.AdminPool(t, migrations.PF)
	indicator, entry := ids.New(), ids.New()
	if err := tenant.WithTenantTx(ctx, admin, tid, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_ext_indicator(tenant_id,id,code,name,name_key,unit,actor_ref,idem_key) VALUES($1,$2::uuid,'old267','Chỉ tiêu nguồn cũ','chi tieu nguon cu','ha',$3,$2::text)`, tid, indicator, actor); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_ext_figure(id,tenant_id,stream_key,period_id,indicator_id,action,value,unit,agency,document,source_date,event_time,actor_ref,idem_key,rule_version) VALUES($1::uuid,$2,'khtc_ext_figure:'||$3::uuid::text||':'||$4::uuid::text,$3,$4,'set',123.5,'ha','Cơ quan cũ','Văn bản cũ','2026-09-29',$5,$6,$1::text,'legacy267')`, entry, tid, period.ID, indicator, clk.Now(), actor)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fresh, err := svc.Get(ctx, period.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Content.Data.External) != 0 || fresh.Content.Data.InputHash != original.Content.Data.InputHash {
		t.Fatal("fresh capture read retired external source")
	}
	legacy := original.Content
	legacy.Data, err = domain.WithExternal(legacy.Data, []domain.ExternalFigure{{EntryID: entry, Version: 1, IndicatorID: indicator, Code: "old267", Name: "Chỉ tiêu nguồn cũ", Value: "123.5", Unit: "ha", Agency: "Cơ quan cũ", Document: "Văn bản cũ", Evidence: []domain.ExternalEvidence{}, SourceDate: "2026-09-29", At: clk.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := RenderXLSX(legacy, domain.Filter{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	draft := store.Draft{ID: ids.New(), PeriodID: period.ID, Action: "refresh", Content: legacy, InputHash: legacy.Data.InputHash, ActorRef: actor, At: clk.Now()}
	released := store.Export{ID: ids.New(), PeriodID: period.ID, DraftID: draft.ID, Content: legacy, InputHash: legacy.Data.InputHash, FileName: "legacy267.xlsx", Body: body, BodyHash: hex.EncodeToString(hash[:]), Provisional: true, ActorRef: actor, At: clk.Now()}
	if err := tenant.WithTenantTx(ctx, database, tid, func(tx pgx.Tx) error {
		if err := store.InsertDraft(ctx, tx, tid, &draft, ids.New().String()); err != nil {
			return err
		}
		return store.InsertExport(ctx, tx, tid, &released, ids.New().String())
	}); err != nil {
		t.Fatal(err)
	}
	frozen := func() string {
		t.Helper()
		var snapshot string
		err := tenant.WithTenantTx(ctx, database, tid, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT payload::text||input_hash||encode(row_hash,'hex') FROM pf_extension.khtc_report_draft WHERE id=$1", draft.ID).Scan(&snapshot)
		})
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	stored := frozen()
	for i := 0; i < 2; i++ {
		saved, err := svc.Get(ctx, period.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(saved.Content.Data, legacy.Data) {
			t.Fatal("legacy data/input hash changed on read")
		}
		x, err := svc.Download(ctx, period.ID, released.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(x.Body, body) || x.BodyHash != released.BodyHash {
			t.Fatal("frozen Excel/hash changed")
		}

	}
	if frozen() != stored {
		t.Fatal("read/export callback rewrote saved payload/input/row hash")
	}
	revised, err := svc.Save(ctx, period.ID, SaveInput{BaseVersion: draft.Version, Texts: map[string]string{"B": "Lời văn bản mới"}}, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(revised.Content.Data, legacy.Data) || revised.Content.Texts["B"] != "Lời văn bản mới" {
		t.Fatal("narrative Save changed captured data or lost text")
	}
	in := ExportInput{BaseVersion: revised.Version, AcknowledgeProvisional: true}
	sealed, err := svc.Export(ctx, period.ID, in, "legacy267-repeat")
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Export(ctx, period.ID, in, "legacy267-repeat")
	if err != nil {
		t.Fatal(err)
	}
	if sealed.ID != again.ID || sealed.BodyHash != again.BodyHash {
		t.Fatal("legacy export idempotency changed")
	}
	refreshed, err := svc.Refresh(ctx, period.ID, revised.Version, ids.New().String())
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed.Content.Data.External) != 0 || refreshed.SourceChanged {
		t.Fatal("explicit refresh carried retired input or is immediately stale")
	}
	plans.version++
	changed, err := svc.Get(ctx, period.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !changed.SourceChanged {
		t.Fatal("changed active KN plan was hidden")
	}
	if frozen() != stored {
		t.Fatal("Save/Refresh rewrote previous legacy ledger")
	}
	var valid bool
	if err := tenant.WithTenantTx(ctx, database, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT (SELECT ok FROM shared.verify_chain('pf_extension.khtc_report_draft',$1,'khtc_report_draft:'||$2::uuid::text)) AND (SELECT ok FROM shared.verify_chain('pf_extension.khtc_report_export',$1,'khtc_report_export:'||$2::uuid::text))", tid, period.ID).Scan(&valid)
	}); err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("report immutable chains broken")
	}
}
