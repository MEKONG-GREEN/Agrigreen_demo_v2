package platformdata_test

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	kn "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	rp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

type metadataReportFacts struct{ kn.ReportData }

func (metadataReportFacts) Sowing(context.Context, kn.ReportQuery) (kn.SowingData, error) {
	return kn.SowingData{Rows: []kn.SowingRow{{ProvinceCode: "89", CommuneCode: "89011", SeasonCode: "HT", VarietyCode: "OM1", SownAreaHa: decimal.RequireFromString("1.25"), Cooperatives: 1, Reports: 2}}}, nil
}
func (metadataReportFacts) ProgramProgress(context.Context, kn.ReportQuery) (kn.ProgramData, error) {
	return kn.ProgramData{Rows: []kn.ProgramRow{{ProvinceCode: "89", CommuneCode: "89011", CooperativeRef: ref(103), SeasonCode: "HT", SeasonYear: 2026, RegisteredHa: decimal.RequireFromString("2.5"), Approved: true}}}, nil
}

func TestKNReportBuildersWithPublishedMetadata(t *testing.T) {
	f := newFixture(t)
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	nativeOrgs := party.New(pool, clock.System{}).Orgs()
	clk := clock.NewFixed(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	native := extension.ReportBuilders(extension.ReportBuilderDeps{Data: metadataReportFacts{}, Orgs: nativeOrgs, Units: f.units, Clock: clk})
	published := extension.ReportBuilders(extension.ReportBuilderDeps{Data: metadataReportFacts{}, Orgs: extension.NewPublishedOrganizations(pool), Units: extension.NewPublishedAdminUnits(pool), Clock: clk})
	index := func(reg []rp.SourceBuilderRegistration) map[string]rp.SourceBuilder {
		out := map[string]rp.SourceBuilder{}
		for _, b := range reg {
			out[b.Source] = b.Build
		}
		return out
	}
	a, b := index(native), index(published)
	for _, tid := range []uuid.UUID{f.tid, f.other} {
		ctx := tenant.WithID(context.Background(), tid)
		for _, tc := range []struct{ source, level string }{{kn.SourceSowing, "commune"}, {kn.SourceProgramProgress, "cooperative"}} {
			q := rp.SourceBuildQuery{Source: tc.source, TenantID: tid, Scope: authz.Scope{All: true}, At: clk.Now(), Level: tc.level, Periods: []string{"2026-10"}, PeriodLabel: "Tháng 10/2026"}
			want, e := a[tc.source](ctx, q)
			if e != nil {
				t.Fatal(e)
			}
			got, e := b[tc.source](ctx, q)
			if e != nil {
				t.Fatal(e)
			}
			equal(t, got, want)
			if len(got.Rows) != 1 {
				t.Fatalf("metadata consumer has no rows: %+v", got)
			}
			province, commune, org := "Province A", "Commune one", "Org 103"
			if tid == f.other {
				province, commune, org = "Other tenant Province A", "Other tenant Commune one", "Other Org 103"
			}
			if got.Rows[0]["province_name"].Text != province || got.Rows[0]["commune_name"].Text != commune {
				t.Fatalf("wrong published unit labels: %+v", got.Rows)
			}
			if tc.source == kn.SourceProgramProgress && got.Rows[0]["cooperative_name"].Text != org {
				t.Fatalf("wrong published organization: %+v", got.Rows)
			}
		}
	}
}

func TestKNReportSessionWithPublishedOrganizations(t *testing.T) {
	f := newFixture(t)
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	native := extension.ReportSessionResolver(party.New(pool, clock.System{}).Orgs())
	published := extension.ReportSessionResolver(extension.NewPublishedOrganizations(pool))
	snapshot := func(a rp.SessionActor, handled bool, err error) any {
		var e string
		if err != nil {
			e = err.Error()
		}
		deps := []bool{}
		if a.AllowsDepartment != nil {
			deps = []bool{a.AllowsDepartment("trong_trot"), a.AllowsDepartment("cnts")}
		}
		return struct {
			Allowed                      bool
			Department                   string
			AllDepartments, AllProvinces bool
			Province                     string
			Unmask                       bool
			OnBehalf                     *uuid.UUID
			Departments                  []bool
			Handled                      bool
			Error                        string
		}{a.Allowed, a.Department, a.AllDepartments, a.AllProvinces, a.Province, a.UnmaskRecipients, a.OnBehalfOf, deps, handled, e}
	}
	checks := 0
	for _, tid := range []uuid.UUID{f.tid, f.other, ids.New()} {
		for _, role := range []string{authz.KNAdmin, authz.KNTW, authz.KNTinh, authz.KNXa, "invalid"} {
			for _, org := range []uuid.UUID{ref(101), ref(102), ref(106), ref(999)} {
				p := authz.Principal{TenantID: tid, ActorRef: ref(900), KN: &authz.KNContext{Role: role, OrgRef: org, Department: "trong_trot", Scope: authz.Scope{All: true}}}
				ctx := authz.WithPrincipal(tenant.WithID(context.Background(), tid), p)
				for _, op := range []string{rp.SessionReadForms, rp.SessionReadDispatch, rp.SessionWriteDispatch} {
					na, nh, ne := native(ctx, p, op)
					pa, ph, pe := published(ctx, p, op)
					equal(t, snapshot(pa, ph, pe), snapshot(na, nh, ne))
					checks++
					if tid == f.tid && role == authz.KNTinh && org == ref(102) && op == rp.SessionReadDispatch {
						if pe != nil || !ph || !pa.Allowed || pa.Province != "89" || pa.AllProvinces {
							t.Fatalf("wrong published session scope: %+v %v", pa, pe)
						}
					}
				}
			}
		}
	}
	t.Logf("%d native/published session decisions, including missing organizations/tenants and denials", checks)
}
