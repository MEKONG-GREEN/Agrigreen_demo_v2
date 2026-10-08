package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testLedgers = map[string]bool{"pf_geo.plot_event": true}

func TestLedgerColumnsAllowed(t *testing.T) {
	src := `
SET LOCAL lock_timeout = '5s';
ALTER TABLE pf_geo.plot_event
  ADD COLUMN lot_name text NULL CHECK (lot_name IS NOT NULL OR true),
  ADD COLUMN note text,
  ADD CONSTRAINT x CHECK (note IS NULL) NOT VALID,
  DROP CONSTRAINT y;
SELECT shared.register_ledger_columns('pf_geo.plot_event', ARRAY['lot_name', 'note']);
-- bảng không phải sổ: tự do
ALTER TABLE pf_geo.plot ADD COLUMN z int NOT NULL DEFAULT 0, DROP COLUMN w;
`
	if p := CheckLedgerColumns(src, testLedgers); len(p) != 0 {
		t.Fatalf("không được báo lỗi: %v", p)
	}
}

func TestLedgerColumnsRejected(t *testing.T) {
	cases := map[string]string{
		"ALTER TABLE pf_geo.plot_event ADD COLUMN a text NULL":                          "phải đăng ký",
		"ALTER TABLE pf_geo.plot_event ADD a text":                                      "phải đăng ký",
		"ALTER TABLE pf_geo.plot_event ADD COLUMN a int NOT NULL DEFAULT 0;" + reg("a"): "không DEFAULT",
		"ALTER TABLE pf_geo.plot_event ADD COLUMN a text DEFAULT 'x';" + reg("a"):       "không DEFAULT",
		"ALTER TABLE ONLY pf_geo.plot_event DROP COLUMN note":                           "DROP COLUMN",
		"ALTER TABLE pf_geo.plot_event ALTER COLUMN note TYPE varchar(10)":              "đổi kiểu",
		"ALTER TABLE pf_geo.plot_event RENAME COLUMN note TO memo":                      "đổi tên",
		"ALTER TABLE pf_geo.plot_event ADD COLUMN a text;" + reg("b"):                   "phải đăng ký",
	}
	for sql, want := range cases {
		p := CheckLedgerColumns(sql, testLedgers)
		if len(p) == 0 || !strings.Contains(strings.Join(p, "\n"), want) {
			t.Errorf("%q: mong lỗi chứa %q, nhận %v", sql, want, p)
		}
	}
}

func reg(cols ...string) string {
	return "SELECT shared.register_ledger_columns('pf_geo.plot_event', ARRAY['" + strings.Join(cols, "', '") + "']);"
}

// Hai tệp đã thêm cột vào sổ trước quy tắc (pf 000079, mkg 000006) vi phạm nếu không được miễn —
// cột của chúng được đăng ký bù ở shared 000011.
func TestLedgerColumnsGrandfathered(t *testing.T) {
	root := filepath.Join("..", "..", "migrations")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	ledgers, err := ledgerSet(root, entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"pf/000079_pf_geo_kn_plot.up.sql", "mkg/000006_mkg_fieldops_g3.up.sql"} {
		src, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		if p := CheckLedgerColumns(string(src), ledgers); len(p) == 0 {
			t.Errorf("%s: phải bị bắt nếu không được miễn", f)
		}
	}
	shared, err := os.ReadFile(filepath.Join(root, "shared", "000011_ledger_added_columns.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []string{"mkg_fieldops.roll_count", "pf_geo.plot_boundary", "pf_geo.plot_event"} {
		if !strings.Contains(string(shared), "('"+tc+"'") {
			t.Errorf("shared 000011 thiếu đăng ký bù cho %s", tc)
		}
	}
}
