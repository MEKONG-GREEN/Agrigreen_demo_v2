package handler_test

import (
	"context"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
)

// Real source INSERTs exercise the synchronous projection trigger. HTX nodes
// are independent roots; the KN actor is not their organization parent.
func (f *fixture) seedSourceOrgs() {
	f.t.Helper()
	province := ids.New()
	for _, o := range []partyport.Org{
		{Ref: province, Kind: partyport.OrgTTKNProvince, Code: "TTKN92", AdminUnitCode: "92"},
		{Ref: f.knA, Kind: partyport.OrgExtensionTeam, Code: "TKN-A", AdminUnitCode: "31117", ParentRef: province},
		{Ref: f.coopA, Kind: partyport.OrgCooperative, Code: "HTXA", AdminUnitCode: "31117"},
		{Ref: f.coopB, Kind: partyport.OrgCooperative, Code: "HTXB", AdminUnitCode: "31120"},
	} {
		var parent any
		if o.ParentRef != uuid.Nil {
			parent = o.ParentRef
		}
		if _, err := pgtest.Get(f.t).AdminPool(f.t, migrations.PF).Exec(context.Background(), `INSERT INTO pf_party.org
          (tenant_id,ref,kind,code,name,admin_unit_code,parent_ref,active,last_event_id,last_seq)
          VALUES ($1,$2,$3,$4,$4,$5,$6,true,$7,1)`, f.tenant, o.Ref, string(o.Kind), o.Code, o.AdminUnitCode, parent, ids.New()); err != nil {
			f.t.Fatalf("seed source org: %v", err)
		}
	}
}
