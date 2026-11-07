package handler_test

import (
	"context"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// FN-19 B1, BR-01/AC-04: KN owns its sites, independently of HTX publication.
func TestCNTSModelStandaloneWithoutHTX(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "empty"
		if missing {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			pool := pgtest.Get(t).AdminPool(t, migrations.PF)
			var count int
			if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pf_cooperative_pub.v1_cooperatives`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("isolated HTX publication must be empty: count=%d, err=%v", count, err)
			}
			if missing {
				if _, err := pool.Exec(context.Background(), `ALTER SCHEMA pf_cooperative_pub RENAME TO cnts266_cooperative_pub`); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, err := pool.Exec(context.Background(), `ALTER SCHEMA cnts266_cooperative_pub RENAME TO pf_cooperative_pub`); err != nil {
						t.Error(err)
					}
				})
				var absent bool
				if err := pool.QueryRow(context.Background(), `SELECT to_regnamespace('pf_cooperative_pub') IS NULL`).Scan(&absent); err != nil || !absent {
					t.Fatalf("HTX namespace must be absent: %v, %v", absent, err)
				}
			}
			f := newFix(t)
			owner := f.user(tinh("chu_nhiem"))
			other := f.user(tinh("chu_nhiem"))
			project := want(t, owner.post(base+"projects", map[string]any{
				"code": "KNQG-" + name, "name": "Dự án KN độc lập", "kind": "knqg", "start_date": "2027-03-01",
			}), 201, "", "create KN-owned project")
			body := map[string]any{"project_ref": str(project, "ref"), "commune_code": "30001", "name": "Điểm KN độc lập",
				"model_type_code": "GA_TP", "cooperative_ref": ids.New().String()}
			want(t, other.post(base+"demo-sites", body), 403, "", "another manager cannot create site")
			want(t, owner.post(base+"demo-sites", merge(body, map[string]any{"commune_code": "31001"})),
				403, "extension.out_of_scope", "outside province remains forbidden")
			want(t, owner.post(base+"demo-sites", merge(body, map[string]any{"cooperative_ref": "invalid-uuid"})),
				422, "validation_failed", "reference syntax remains validated")
			idem := key()
			site := want(t, owner.postK(base+"demo-sites", idem, body), 201, "", "create without HTX lookup")
			ref := str(site, "ref")
			if site["cooperative_ref"] != body["cooperative_ref"] {
				t.Fatal("opaque legacy reference was not preserved")
			}
			replay := want(t, owner.postK(base+"demo-sites", idem, body), 201, "", "idempotent create")
			if str(replay, "ref") != ref || num(replay, "version") != "1" {
				t.Fatal("idempotent site create appended a version")
			}
			update := merge(body, map[string]any{"base_version": 1, "name": "Điểm KN đã sửa", "cooperative_ref": ids.New().String()})
			want(t, other.post(base+"demo-sites/"+ref, update), 403, "", "another manager cannot update site")
			updated := want(t, owner.post(base+"demo-sites/"+ref, update), 200, "", "update without HTX lookup")
			if num(updated, "version") != "2" || len(list(updated, "versions")) != 2 || updated["cooperative_ref"] != update["cooperative_ref"] {
				t.Fatalf("site ledger/reference: %v", updated)
			}
			want(t, owner.post(base+"demo-sites/"+ref, update), 409, "extension.cnts_site_version_conflict", "stale version rejected")
			want(t, f.user(xa("30001")).get(base+"demo-sites/"+ref), 200, "", "commune sees own site")
			want(t, f.user(xa("30002")).get(base+"demo-sites/"+ref), 404, "extension.cnts_site_not_found", "other commune sees no site")
		})
	}
}
