package migrator

import (
	"slices"
	"testing"
	"testing/fstest"
	"time"
)

func TestParseName_WHB(t *testing.T) {
	cases := []struct {
		in     string
		ok     bool
		v      int64
		stem   string
		scheme Scheme
	}{
		{"000150_pf_hrm_staff.up.sql", true, 150, "pf_hrm_staff", SchemeLegacy},
		{"20261013093012_pf_price_approval.up.sql", true, 20261013093012, "pf_price_approval", SchemeTimestamp},
		{"1_x.up.sql", false, 0, "", 0},
		{"0001500_x.up.sql", false, 0, "", 0},          // 7 chữ số
		{"2026101309301_x.up.sql", false, 0, "", 0},    // 13 chữ số
		{"20261013093012_X.up.sql", false, 0, "", 0},   // chữ hoa
		{"20261013093012_x.down.sql", false, 0, "", 0}, // chỉ tiến
	}
	for _, c := range cases {
		n, ok := ParseName(c.in)
		if ok != c.ok || (ok && (n.Version != c.v || n.Stem != c.stem || n.Scheme != c.scheme)) {
			t.Errorf("%s: %+v %v", c.in, n, ok)
		}
	}
	if n, _ := ParseName("20261332093012_pf_x.up.sql"); n.Scheme == SchemeTimestamp {
		if _, err := n.Time(); err == nil {
			t.Error("tháng 13 phải lỗi")
		}
	}
	n, _ := ParseName("20261013093012_pf_x.up.sql")
	if tm, err := n.Time(); err != nil || !tm.Equal(time.Date(2026, 10, 13, 9, 30, 12, 0, time.UTC)) {
		t.Errorf("Time: %v %v", tm, err)
	}
	ict := time.FixedZone("ICT", 7*3600)
	if got := NewFileName(time.Date(2026, 10, 13, 16, 30, 12, 0, ict), "pf_price_x"); got != "20261013093012_pf_price_x.up.sql" {
		t.Errorf("NewFileName phải dùng UTC: %s", got)
	}
}

// Tệp mốc thời gian xếp sau mọi tệp số cũ (cùng module theo số; tệp toàn cụm cũ vẫn là rào chắn), gắn module theo
// tiền tố như tệp cũ; sổ có sẵn các tệp cũ → chỉ áp tệp mới, không ghi sổ lại tệp cũ.
func TestTimestampFilesOrderAfterLegacy_WHB(t *testing.T) {
	fsys := fstest.MapFS{
		"pf/000010_pf_party_a.up.sql":               {Data: []byte("CREATE SCHEMA pf_party;")},
		"pf/000011_pf_geo_a.up.sql":                 {Data: []byte("CREATE SCHEMA pf_geo;")},
		"pf/000012_pf_roles.up.sql":                 {Data: []byte("CREATE TABLE pf_party.r (id int); CREATE TABLE pf_geo.r (id int);")},
		"pf/20261013090000_pf_party_b.up.sql":       {Data: []byte("CREATE TABLE pf_party.b (id int);")},
		"pf/20261012080000_pf_geo_b.up.sql":         {Data: []byte("CREATE TABLE pf_geo.b (id int);")},
		"pf/20261013100000_pf_cropcycle.up.sql":     {Data: []byte("CREATE SCHEMA pf_cropcycle; CREATE TABLE pf_cropcycle.x AS SELECT * FROM pf_party.b;")},
		"pf/20261013110000_pf_party_c.up.sql":       {Data: []byte("CREATE TABLE pf_party.c (id int);")},
		"pf/20261013120000_pf_geo_and_party.up.sql": {Data: []byte("CREATE TABLE pf_party.d (id int); CREATE TABLE pf_geo.d (id int);")},
	}
	files, err := LoadDir(fsys, "pf", testMods)
	if err != nil {
		t.Fatal(err)
	}
	owner := map[string]string{}
	for _, f := range files {
		owner[f.Name] = f.Owner
	}
	if owner["20261013090000_pf_party_b.up.sql"] != "pf/party" || owner["20261013100000_pf_cropcycle.up.sql"] != "pf/cropcycle" {
		t.Errorf("gắn module: %v", owner)
	}
	got := keys(Order(files, testMods))
	want := []string{
		"pf/000011_pf_geo_a.up.sql",
		"pf/000010_pf_party_a.up.sql",
		"pf/000012_pf_roles.up.sql",
		"pf/20261012080000_pf_geo_b.up.sql",
		"pf/20261013090000_pf_party_b.up.sql",
		"pf/20261013110000_pf_party_c.up.sql",
		// gắn pf/geo theo tiền tố (hạng 0) — không ràng buộc với cropcycle (hạng 2) nên đứng trước dù mốc lớn hơn.
		"pf/20261013120000_pf_geo_and_party.up.sql",
		"pf/20261013100000_pf_cropcycle.up.sql",
	}
	if !slices.Equal(got, want) {
		t.Errorf("thứ tự:\n got %v\nwant %v", got, want)
	}

	// DB đã có: mọi tệp cũ trong sổ, golang-migrate ở 12 → chỉ áp tệp mốc thời gian, không import.
	var rows []Row
	for _, f := range files {
		if f.Version < 1000000 {
			rows = append(rows, Row{Dir: f.Dir, File: f.Name, Version: f.Version, Checksum: f.Checksum})
		}
	}
	plan := Order(files, testMods)
	r := Reconcile(plan, rows, map[string]V1State{"pf": {Exists: true, Version: 12}})
	if len(r.Import) != 0 || len(r.Apply) != 5 || r.Blocking() {
		t.Fatalf("DB có sẵn: import %v apply %v", keys(r.Import), keys(r.Apply))
	}
	// golang-migrate (v1) đã áp tới mốc 20261013120000 mà sổ v2 chưa có → ghi sổ, không chạy lại.
	r = Reconcile(plan, rows, map[string]V1State{"pf": {Exists: true, Version: 20261013120000}})
	if len(r.Import) != 5 || len(r.Apply) != 0 {
		t.Fatalf("v1 đã áp mốc: import %v apply %v", keys(r.Import), keys(r.Apply))
	}
}
