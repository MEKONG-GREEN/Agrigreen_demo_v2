package kn77_test

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"net/http"
	"strings"
	"testing"
)

type archivePeriodChoices struct {
	Items []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"items"`
	HasMore bool `json:"has_more"`
	Offset  int  `json:"offset"`
}

func TestArchive190PeriodChoicesAndExactFilter(t *testing.T) {
	f := newReportFx(t)
	p40 := f.open(40, dTT, dTTTT)
	p41 := f.open(41, dTT, dTTTT)
	p42 := f.open(42, dTT, dTTTT) // This period has no exports and must never be offered.
	readChoices := func(query string) archivePeriodChoices {
		t.Helper()
		var out archivePeriodChoices
		c, b := f.do(http.MethodGet, "/v1/extension/khtc/exported-reports/periods?year=2026"+query, authz.KNTW, khtcDept, f.khtcUser, "", nil)
		f.ok(c, b, &out)
		return out
	}
	if got := readChoices(""); len(got.Items) != 0 {
		t.Fatal("draft periods leaked", got)
	}
	d := f.refresh(p40.ID, 0)
	x40 := f.export(p40.ID, d.Version, true)
	f.export(p40.ID, d.Version, true) // two exports still make one period choice
	d = f.refresh(p41.ID, 0)
	x41 := f.export(p41.ID, d.Version, true)
	got := readChoices("")
	if len(got.Items) != 2 {
		t.Fatalf("expected two distinct exported periods: %+v", got)
	}
	for _, p := range got.Items {
		if p.ID == p42.ID || p.Name == "" {
			t.Fatalf("invalid choice %+v", p)
		}
	}
	first := readChoices("&limit=1")
	second := readChoices("&limit=1&offset=1")
	if len(first.Items) != 1 || !first.HasMore || len(second.Items) != 1 || second.HasMore || first.Items[0].ID == second.Items[0].ID {
		t.Fatalf("choice pagination %+v %+v", first, second)
	}
	if got = readChoices("&q=40"); len(got.Items) != 1 || got.Items[0].ID != p40.ID {
		t.Fatalf("period name search %+v", got)
	}
	if got = readChoices("&kind=year"); len(got.Items) != 0 {
		t.Fatal("kind filter", got)
	}
	read := func(id, tail string) archiveResult {
		t.Helper()
		var out archiveResult
		c, b := f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026&period_id="+id+tail, authz.KNTW, khtcDept, f.khtcUser, "", nil)
		f.ok(c, b, &out)
		return out
	}
	if xs := read(p41.ID, ""); len(xs.Items) != 1 || xs.Items[0].ID.String() != x41.ID {
		t.Fatalf("exact period filter %+v", xs)
	}
	if xs := read(p40.ID, "&versions=true"); len(xs.Items) != 2 {
		t.Fatalf("versions within selected period %+v", xs)
	}
	if xs := read(p40.ID, "&versions=true&limit=1&offset=1"); len(xs.Items) != 1 || xs.Items[0].ID.String() != x40.ID || xs.HasMore {
		t.Fatalf("export pagination within period %+v", xs)
	}
	if xs := read(p42.ID, ""); len(xs.Items) != 0 {
		t.Fatal("draft period exports", xs)
	}
	if xs := read(ids.New().String(), ""); len(xs.Items) != 0 {
		t.Fatal("unknown period broadened scope", xs)
	}
	c, b := f.khtc(http.MethodPost, "/periods/"+p40.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ mở nhầm"})
	f.ok(c, b, nil)
	if got = readChoices(""); len(got.Items) != 1 || got.Items[0].ID != p41.ID {
		t.Fatal("cancelled choice leaked", got)
	}
	if xs := read(p40.ID, "&versions=true"); len(xs.Items) != 0 {
		t.Fatal("cancelled exports leaked", xs)
	}
	old := f.tid
	f.tid = ids.New()
	if got = readChoices(""); len(got.Items) != 0 {
		t.Fatal("cross-tenant choices leaked", got)
	}
	if xs := read(p41.ID, ""); len(xs.Items) != 0 {
		t.Fatal("cross-tenant exports leaked", xs)
	}
	f.tid = old
}

func TestArchive190PeriodChoiceGuards(t *testing.T) {
	f := newReportFx(t)
	choices := "/v1/extension/khtc/exported-reports/periods?year=2026"
	for _, role := range []string{authz.KNXa, authz.KNTinh} {
		c, b := f.do(http.MethodGet, choices, role, dTT, f.khtcUser, "", nil)
		if c != 403 {
			t.Fatalf("unauthorized choices %s: %d %s", role, c, b)
		}
	}
	for _, raw := range []string{"not-uuid", "00000000-0000-0000-0000-000000000000", "1e100000", "1e-100000", strings.Repeat("9", 10000)} {
		c, b := f.do(http.MethodGet, "/v1/extension/khtc/exported-reports?year=2026&period_id="+raw, authz.KNTW, khtcDept, f.khtcUser, "", nil)
		if c != 422 {
			t.Fatalf("invalid period HTTP %d %s", c, b)
		}
	}
	for _, query := range []string{"year=1e100000", "year=1e-100000", "year=" + strings.Repeat("9", 10000), "year=2026&limit=51", "year=2026&offset=-1", "year=2026&kind=secret", "year=2026&q=" + strings.Repeat("x", 201)} {
		c, b := f.do(http.MethodGet, "/v1/extension/khtc/exported-reports/periods?"+query, authz.KNTW, khtcDept, f.khtcUser, "", nil)
		if c != 422 {
			t.Fatalf("unbounded choices HTTP %d %s", c, b)
		}
	}
}
