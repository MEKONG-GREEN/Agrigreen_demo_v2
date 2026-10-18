package platformdata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	pp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }
func ref(n int) uuid.UUID   { return uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", n)) }

type fixture struct {
	t          *testing.T
	admin      *pgxpool.Pool
	tid, other uuid.UUID
	units      pp.AdminUnits
	dir        pp.OrgDirectory
}

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	units := masterdata.New(pool, clock.System{}).AdminUnits()
	f := &fixture{t: t, admin: env.AdminPool(t, migrations.PF), tid: ids.New(), other: ids.New(), units: units, dir: party.New(pool, clock.System{}, party.Deps{AdminUnits: units}).OrgDirectory()}
	f.seed(f.tid, false)
	f.seed(f.other, true)
	return f
}
func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, e := f.admin.Exec(context.Background(), q, args...); e != nil {
		f.t.Fatal(e)
	}
}
func (f *fixture) seed(tid uuid.UUID, other bool) {
	for _, u := range []struct {
		code, name, level, parent, from, to string
		next                                []string
	}{
		{"89", "Province A", "province", "", "2020-01-01", "", nil},
		{"92", "Province B", "province", "", "2020-01-01", "", nil},
		{"89001", "Old commune", "commune", "89", "2020-01-01", "2025-07-01", []string{"89011", "89012"}},
		{"89011", "Commune one", "commune", "89", "2025-07-01", "", nil},
		{"89012", "Commune two old", "commune", "89", "2025-07-01", "2026-07-01", []string{"89013"}},
		{"89013", "Commune two current", "commune", "89", "2026-07-01", "", nil},
		{"89100", "Previous name", "commune", "89", "2020-01-01", "2024-01-01", []string{"89100"}},
		{"89100", "Current name", "commune", "89", "2024-01-01", "", nil},
		{"92001", "Second province commune", "commune", "92", "2020-01-01", "", nil},
	} {
		if other {
			u.name = "Other tenant " + u.name
		}
		if u.next == nil {
			u.next = []string{}
		}
		f.exec(`INSERT INTO pf_masterdata.admin_unit(tenant_id,code,valid_from,valid_to,name,level,parent_code,successor_codes,source,actor_ref)
   VALUES($1,$2,$3::date,NULLIF($4,'')::date,$5,$6,NULLIF($7,''),$8::text[],'native-platform-fixture',$9)`,
			tid, u.code, u.from, u.to, u.name, u.level, u.parent, u.next, ref(900))
	}
	for _, n := range []int{101, 102, 106, 103, 104, 105, 107} {
		kind, unit, parent, active := "cooperative", "89011", 102, true
		switch n {
		case 101:
			kind, unit, parent = "ttkn_national", "", 0
		case 102:
			kind, unit, parent = "ttkn_province", "89", 101
		case 104:
			unit, active = "89012", false
		case 105:
			unit, parent = "", 0
		case 106:
			kind, unit, parent = "ttkn_province", "92", 101
		case 107:
			unit, parent = "92001", 106
		}
		if other && n == 103 {
			unit, parent, active = "92001", 106, false
		}
		var pr any
		if parent != 0 {
			pr = ref(parent)
		}
		name := fmt.Sprintf("Org %d", n)
		if other {
			name = "Other " + name
		}
		f.exec(`INSERT INTO pf_party.org(tenant_id,ref,kind,code,name,admin_unit_code,parent_ref,active,last_event_id,last_seq)
   VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,1)`, tid, ref(n), kind, fmt.Sprintf("O%d", n), name, unit, pr, active, ref(n+1000))
	}
}
func result(v any, e error) any {
	if e != nil {
		return map[string]string{"error": e.Error()}
	}
	return v
}

func TestLegacyPlatformReadContract(t *testing.T) {
	f := newFixture(t)
	ctx := tenant.WithID(context.Background(), f.tid)
	snap := map[string]any{}
	tree := f.units.(pp.AdminUnitTree)
	dates := []string{"2023-01-01T00:00:00Z", "2025-06-30T16:59:59Z", "2025-06-30T17:00:00Z", "2026-06-30T16:59:59Z", "2026-06-30T17:00:00Z"}
	for i, ds := range dates {
		at, e := time.Parse(time.RFC3339, ds)
		if e != nil {
			t.Fatal(e)
		}
		for _, code := range []string{"89", "89001", "89011", "89012", "89013", "89100", "99999"} {
			u, e := f.units.Get(ctx, code, at)
			snap[fmt.Sprintf("get/%d/%s", i, code)] = result(u, e)
		}
		for _, code := range []string{"89001", "89100", "99999"} {
			u, e := f.units.Current(ctx, code, at)
			snap[fmt.Sprintf("current/%d/%s", i, code)] = result(u, e)
		}
		for _, code := range []string{"89", "99999"} {
			u, e := f.units.Descendants(ctx, code, at)
			snap[fmt.Sprintf("desc/%d/%s", i, code)] = result(u, e)
		}
		for _, parent := range []string{"", "89", "99999"} {
			u, e := tree.Children(ctx, parent, at)
			snap[fmt.Sprintf("children/%d/%s", i, parent)] = result(u, e)
		}
	}
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.FixedZone("Asia/Ho_Chi_Minh", 7*3600))
	codes, e := f.units.Current(ctx, "89001", at)
	if e != nil || len(codes) != 2 || codes[0] != "89011" || codes[1] != "89013" {
		t.Fatalf("split chain: %v %v", codes, e)
	}
	before := time.Date(2025, 6, 30, 16, 59, 59, 0, time.UTC)
	after := before.Add(time.Second)
	_, eb := f.units.Get(ctx, "89001", before)
	_, ea := f.units.Get(ctx, "89001", after)
	if eb != nil || ea == nil {
		t.Fatalf("VN exclusive date boundary: %v/%v", eb, ea)
	}
	for _, c := range []struct {
		name string
		ctx  context.Context
	}{{"main", ctx}, {"other", tenant.WithID(context.Background(), f.other)}, {"emptyTenant", tenant.WithID(context.Background(), ids.New())}} {
		for _, units := range []struct {
			name string
			v    []string
		}{{"all", nil}, {"empty", []string{}}, {"one", []string{"89011"}}, {"old", []string{"89012"}}, {"unknown", []string{"99999"}}} {
			for _, active := range []bool{false, true} {
				v, e := f.dir.OrgsOfKind(c.ctx, pp.OrgCooperative, units.v, active)
				if e != nil {
					t.Fatal(e)
				}
				snap[fmt.Sprintf("orgs/%s/%s/%t", c.name, units.name, active)] = v
			}
		}
		v, e := f.dir.OrgsByRefs(c.ctx, []uuid.UUID{ref(101), ref(103), ref(104), ref(105), ref(999)})
		if e != nil {
			t.Fatal(e)
		}
		snap["refs/"+c.name] = v
		u, e := f.units.Get(c.ctx, "89011", at)
		snap["tenantUnit/"+c.name] = result(u, e)
	}
	raw, e := json.MarshalIndent(snap, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	raw = append(raw, '\n')
	want, e := os.ReadFile(filepath.Join("..", "golden", "platform-read-native.json"))
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(raw, want) {
		t.Fatal("native platform contract differs from frozen baseline")
	}
}
