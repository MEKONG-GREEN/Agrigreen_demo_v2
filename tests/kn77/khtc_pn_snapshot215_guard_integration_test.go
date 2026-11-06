package kn77_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func pn215OpenProgram(t *testing.T, f *reportFx) string {
	t.Helper()
	var p khtcPeriod
	c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{"report_type": "program", "year": 2026, "as_of_date": "2026-10-13", "due_at": "2026-12-31T03:00:00Z", "departments": []string{dTT, dTTTT}})
	f.ok(c, b, &p)
	return p.ID
}

func TestPN215UnpreparedSourceAndCurrentAuthority(t *testing.T) {
	f := newReportFx(t)
	pid := pn215OpenProgram(t, f)
	base := "/v1/extension/khtc/departments/" + dTT + "/program-progress"
	actor := ids.New()
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	count := func() int {
		t.Helper()
		var n int
		if err := admin.QueryRow(context.Background(), `SELECT count(*) FROM pf_extension.khtc_report_draft WHERE period_id=$1`, pid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	c, b := f.do(http.MethodGet, base+"/"+pid, authz.KNTinh, dTT, actor, "", nil)
	var v pn215View
	f.ok(c, b, &v)
	if v.Status != "not_prepared" || v.Snapshot != nil || len(v.Rows) != 0 {
		t.Fatalf("missing M5 source was invented: %+v", v)
	}
	if count() != before {
		t.Fatal("readonly PN request created an owner report")
	}
	for _, suffix := range []string{"/periods?year=2026", "/" + pid, "/" + pid + "/excel?snapshot_id=" + ids.New().String()} {
		for _, who := range []struct{ role, dept string }{{authz.KNTinh, dTTTT}, {authz.KNXa, dTT}, {authz.KNTW, dTT}, {authz.KNTinh, khtcDept}} {
			c, b = f.do(http.MethodGet, base+suffix, who.role, who.dept, actor, "", nil)
			if c != http.StatusForbidden {
				t.Fatalf("current authority must deny %s/%s %s: status%d body%s", who.role, who.dept, suffix, c, b)
			}
		}
	}
	for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "9999"} {
		c, b = f.do(http.MethodGet, base+"/periods?year="+raw, authz.KNTinh, dTT, actor, "", nil)
		if c != http.StatusUnprocessableEntity {
			t.Fatalf("unbounded year accepted: len%d status%d body%s", len(raw), c, b)
		}
	}
	for _, key := range []string{"snapshot_id", "program"} {
		c, b = f.do(http.MethodGet, base+"/"+pid+"?"+key+"="+strings.Repeat("9", 100000), authz.KNTinh, dTT, actor, "", nil)
		if c != http.StatusUnprocessableEntity {
			t.Fatalf("unbounded identity accepted: %s status%d", key, c)
		}
	}
	own := f.tid
	f.tid = ids.New()
	defer func() { f.tid = own }()
	c, b = f.do(http.MethodGet, base+"/"+pid, authz.KNTinh, dTT, actor, "", nil)
	if c != http.StatusNotFound {
		t.Fatalf("foreign tenant must not resolve period: status%d body%s", c, b)
	}
	c, b = f.do(http.MethodGet, base+"/periods?year=2026", authz.KNTinh, dTT, actor, "", nil)
	var choices struct {
		Items []json.RawMessage `json:"items"`
	}
	f.ok(c, b, &choices)
	if len(choices.Items) != 0 {
		t.Fatalf("foreign tenant received choices: %s", b)
	}
}

func TestPN215PinnedSnapshotSurvivesOwnerDraftAdvance(t *testing.T) {
	f := newReportFx(t)
	week := f.open(41, dTT, dTTTT)
	f.submit(week.ID, "25")
	pid := pn215OpenProgram(t, f)
	d := f.refresh(pid, 0)
	base := "/v1/extension/khtc/departments/" + dTT + "/program-progress/" + pid
	actor := ids.New()
	var first pn215View
	c, b := f.do(http.MethodGet, base, authz.KNTinh, dTT, actor, "", nil)
	f.ok(c, b, &first)
	if first.Snapshot == nil {
		t.Fatal("missing real M5 snapshot")
	}
	c, b = f.call(http.MethodPost, pid, "/draft", map[string]any{"base_version": d.Version, "texts": map[string]string{"I": "Đánh giá mới của KHTC"}})
	var advanced reportDetail
	f.ok(c, b, &advanced)
	if advanced.Version <= d.Version {
		t.Fatal("owner draft did not advance")
	}
	var pinned, latest pn215View
	c, b = f.do(http.MethodGet, base+"?snapshot_id="+first.Snapshot.ID, authz.KNTinh, dTT, actor, "", nil)
	f.ok(c, b, &pinned)
	c, b = f.do(http.MethodGet, base, authz.KNTinh, dTT, actor, "", nil)
	f.ok(c, b, &latest)
	if pinned.Snapshot == nil || pinned.Snapshot.ID != first.Snapshot.ID || pinned.Snapshot.Version != first.Snapshot.Version || latest.Snapshot == nil || latest.Snapshot.Version != advanced.Version {
		t.Fatalf("selected snapshot floated after owner advance: first%+v pinned%+v latest%+v", first.Snapshot, pinned.Snapshot, latest.Snapshot)
	}
	for _, suffix := range []string{"?snapshot_id=" + ids.New().String(), "/excel?snapshot_id=" + ids.New().String()} {
		c, b = f.do(http.MethodGet, base+suffix, authz.KNTinh, dTT, actor, "", nil)
		if c != http.StatusNotFound {
			t.Fatalf("unknown snapshot must not fall back to latest: status%d body%s", c, b)
		}
	}
	c, b = f.do(http.MethodGet, base+"?program="+ids.New().String(), authz.KNTinh, dTT, actor, "", nil)
	if c != http.StatusForbidden {
		t.Fatalf("programme outside own snapshot must be403, got%d %s", c, b)
	}
}
