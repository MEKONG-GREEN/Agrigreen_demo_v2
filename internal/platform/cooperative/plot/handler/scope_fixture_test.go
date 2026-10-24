package handler_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Seed the source, not k_ref: the production org INSERT trigger must project
// every fixture node and its real ancestors in the same transaction.
func seedSourceOrg(t *testing.T, tid, ref uuid.UUID, kind partyport.OrgKind, code, unit string, parent uuid.UUID) {
	t.Helper()
	var parentRef any
	if parent != uuid.Nil {
		parentRef = parent
	}
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	_, err := pool.Exec(context.Background(), `INSERT INTO pf_party.org
      (tenant_id, ref, kind, code, name, admin_unit_code, parent_ref, active, last_event_id, last_seq)
      VALUES ($1,$2,$3,$4,$4,$5,$6,true,$7,1)`, tid, ref, string(kind), code, unit, parentRef, ids.New())
	if err != nil {
		t.Fatalf("seed source org: %v", err)
	}
}

// HTX nodes remain independent roots, never artificial KN descendants.
func (f *fixture) seedSourceOrgs(coopACode string) {
	f.t.Helper()
	f.province92 = ids.New()
	seedSourceOrg(f.t, f.tenant, f.province92, partyport.OrgTTKNProvince, "TTKN92", "92", uuid.Nil)
	seedSourceOrg(f.t, f.tenant, f.orgA, partyport.OrgExtensionTeam, "TKN-A", "31117", f.province92)
	seedSourceOrg(f.t, f.tenant, f.orgB, partyport.OrgExtensionTeam, "TKN-B", "31120", f.province92)
	seedSourceOrg(f.t, f.tenant, f.coopA, partyport.OrgCooperative, coopACode, "31117", uuid.Nil)
	seedSourceOrg(f.t, f.tenant, f.coopB, partyport.OrgCooperative, "HTXB", "31117", uuid.Nil)
}
