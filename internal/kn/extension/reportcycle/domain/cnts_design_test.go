package domain

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"strings"
	"testing"
)

func TestParseCNTSDesign(t *testing.T) {
	valid := []string{"so_con", "Số con", "integer", "true", "con", "Tổng đàn", "true", ""}
	parse := func(rows [][]string) DesignImport {
		tb := &tabular.Table{Keys: DesignHeaders}
		for i, r := range rows {
			tb.Rows = append(tb.Rows, tabular.Row{Line: i + 2, Cells: r})
		}
		return ParseDesign(tb)
	}
	if r := parse([][]string{valid}); !r.Valid || len(r.Fields) != 1 || !r.Fields[0].Required || !r.Fields[0].NonNegative {
		t.Fatalf("valid: %+v", r)
	}
	cases := []struct {
		name  string
		col   int
		value string
	}{{"key", 0, "bad key"}, {"key long", 0, strings.Repeat("a", 65)}, {"label", 1, ""}, {"label long", 1, strings.Repeat("a", 301)}, {"type", 2, "money"}, {"unit", 4, strings.Repeat("a", 81)}, {"group", 5, strings.Repeat("a", 201)}, {"choice on numeric", 7, "A|B"}}
	for _, v := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "999999999999999999999999999999"} {
		cases = append(cases, struct {
			name  string
			col   int
			value string
		}{"hostile required", 3, v}, struct {
			name  string
			col   int
			value string
		}{"hostile nonnegative", 6, v})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := append([]string(nil), valid...)
			r[tc.col] = tc.value
			out := parse([][]string{valid, r})
			if out.Valid || len(out.Fields) != 0 || len(out.Issues) == 0 || out.Issues[0].Line != 3 {
				t.Fatalf("invalid must return no usable fields: %+v", out)
			}
		})
	}
	for name, rows := range map[string][][]string{"empty": {}, "duplicate": {valid, valid}, "too many": make([][]string, 501), "choice empty": {{"kind", "Loại", "choice", "false", "", "", "false", ""}}, "choice duplicate": {{"kind", "Loại", "choice", "false", "", "", "false", "A|A"}}, "text nonnegative": {{"note", "Ghi chú", "text", "false", "", "", "true", ""}}} {
		t.Run(name, func(t *testing.T) {
			r := parse(rows)
			if r.Valid || len(r.Fields) != 0 || len(r.Issues) == 0 {
				t.Fatalf("invalid: %+v", r)
			}
		})
	}
	bad := ParseDesign(&tabular.Table{Keys: []string{"label", "key"}})
	if bad.Valid || len(bad.Issues) != 1 || bad.Issues[0].Line != 1 {
		t.Fatal(bad)
	}
}
