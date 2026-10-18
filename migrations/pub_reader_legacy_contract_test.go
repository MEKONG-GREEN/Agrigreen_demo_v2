package migrations_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestLegacyPublishedReaderAccessContract(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	groups := dbadmin.ModuleGroups()
	readers := []string{dbadmin.AggRole}
	schemas := []string{}
	for _, g := range groups {
		if !slices.Contains(g.Clusters, migrations.PF) {
			continue
		}
		readers = append(readers, g.Role)

	}
	for _, entry := range modreg.Catalog() {
		if !slices.Contains(entry.Zones, "pf") {
			continue
		}
		for _, schema := range entry.OwnsSchema {
			if strings.HasSuffix(schema, "_pub") {
				schemas = append(schemas, schema)
			}
		}
	}
	slices.Sort(readers)
	readers = slices.Compact(readers)
	slices.Sort(schemas)
	schemas = slices.Compact(schemas)
	if len(readers) < 20 || len(schemas) < 5 {
		t.Fatal("incomplete legacy role/schema inventory")
	}
	type roleFlags struct {
		Role                      string
		Inherit, BypassRLS, Login bool
	}
	type access struct{ Reader, View string }
	type membership struct{ Reader, Granted string }
	flags := []roleFlags{}
	rows, err := pool.Query(ctx, "SELECT rolname,rolinherit,rolbypassrls,rolcanlogin FROM pg_roles WHERE rolname=ANY($1) ORDER BY rolname", readers)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var f roleFlags
		if err := rows.Scan(&f.Role, &f.Inherit, &f.BypassRLS, &f.Login); err != nil {
			t.Fatal(err)
		}
		flags = append(flags, f)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	allowed := []access{}
	rows, err = pool.Query(ctx, `SELECT r.rolname,n.nspname||'.'||c.relname
      FROM pg_roles r CROSS JOIN pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
      WHERE r.rolname=ANY($1) AND n.nspname=ANY($2) AND c.relkind='v'
        AND has_schema_privilege(r.rolname,n.oid,'USAGE') AND has_table_privilege(r.rolname,c.oid,'SELECT')
      ORDER BY r.rolname,n.nspname,c.relname`, readers, schemas)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a access
		if err := rows.Scan(&a.Reader, &a.View); err != nil {
			t.Fatal(err)
		}
		allowed = append(allowed, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	members := []membership{}
	rows, err = pool.Query(ctx, `SELECT r.rolname,g.rolname FROM pg_auth_members m
      JOIN pg_roles r ON r.oid=m.member JOIN pg_roles g ON g.oid=m.roleid
      WHERE r.rolname=ANY($1) AND right(g.rolname,4)='_pub'
      ORDER BY r.rolname,g.rolname`, readers)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var m membership
		if err := rows.Scan(&m.Reader, &m.Granted); err != nil {
			t.Fatal(err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(flags) != len(readers) || len(allowed) == 0 || len(members) == 0 {
		t.Fatal("missing actual ACL snapshot")
	}
	type snapshot struct {
		Roles       []roleFlags
		Access      []access
		Memberships []membership
	}
	wantRaw, err := os.ReadFile("../tests/golden/pub-reader-access.json")
	if err != nil {
		t.Fatal(err)
	}
	var want, additions snapshot
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatal(err)
	}
	// Keep all frozen OLD flags/access/memberships and permit only separately reviewed additions.
	addRaw, err := os.ReadFile("../tests/golden/htx-published-access-additions.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(addRaw, &additions); err != nil {
		t.Fatal(err)
	}
	if len(additions.Roles) != 0 {
		t.Fatal("role flag changes are not approved additions")
	}
	want.Access = append(want.Access, additions.Access...)
	want.Memberships = append(want.Memberships, additions.Memberships...)
	slices.SortFunc(want.Access, func(a, b access) int {
		if d := strings.Compare(a.Reader, b.Reader); d != 0 {
			return d
		}
		return strings.Compare(a.View, b.View)
	})
	slices.SortFunc(want.Memberships, func(a, b membership) int {
		if d := strings.Compare(a.Reader, b.Reader); d != 0 {
			return d
		}
		return strings.Compare(a.Granted, b.Granted)
	})
	got := snapshot{flags, allowed, members}
	if !reflect.DeepEqual(want, got) {
		raw, _ := json.Marshal(got)
		t.Fatalf("publication access differs from frozen OLD plus reviewed additions: %s", raw)
	}
}
