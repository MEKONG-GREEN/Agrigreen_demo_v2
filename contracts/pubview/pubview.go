// Package pubview nạp hợp đồng cột của view công bố `_pub` (R-DB-10, W6.3) dành cho bên đọc trong cùng tenant —
// trước hết là "nguồn module" của KHTC (W11.10, BRD KN 10/2026 X6, BG10). Dạng tệp: README.md cùng thư mục.
//
// Cưỡng chế:
//   - contracts/pubview (kiểm thử đơn vị): siêu dữ liệu đúng; hợp đồng "published" có chỉ thị //ag:pubview (đã sinh vào
//     provides.publications của module chủ), hợp đồng "planned" CHƯA được khai (view chưa dựng);
//   - internal/platform/metrics/handler (kiểm thử tích hợp TestPubViewsDeclaredAndPrivileges_W63): cột của view thật
//     (tên, kiểu format_type, thứ tự) trùng hợp đồng "published".
package pubview

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

//go:embed *.json
var files embed.FS

// Status của hợp đồng.
const (
	StatusPublished = "published" // view đã dựng, khai //ag:pubview
	StatusPlanned   = "planned"   // hợp đồng đã chốt cột, view do gói sau dựng
)

// Column là một cột của view.
type Column struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // kiểu PostgreSQL theo format_type (vd "timestamp with time zone", "text[]")
	Nullable    bool   `json:"nullable"`
	Description string `json:"description"`
}

// Contract là hợp đồng cột của một view _pub.
type Contract struct {
	View         string   `json:"view"`      // <schema>_pub.v<N>_<tên>
	Directive    string   `json:"directive"` // <schema>_pub.<tên>/v<N> — như provides.publications
	Owner        string   `json:"owner"`     // module chủ, vd pf/extension
	Status       string   `json:"status"`
	Package      string   `json:"package"`
	Migration    string   `json:"migration"`
	Consumers    []string `json:"consumers"`
	Purpose      string   `json:"purpose"`
	Grain        string   `json:"grain"`
	Key          []string `json:"key"`
	ClosedRule   string   `json:"closed_rule"`
	PersonalData bool     `json:"personal_data"`
	// Family: họ hợp đồng <tên>/v<N> (ADR-0052) — nhiều view của NHIỀU module chủ cùng một hợp đồng cột (vd
	// closed_figures/v1: nguồn vòng đời báo cáo cũ và kỳ native cap/forms). Mọi thành viên phải có cột giống hệt (tên,
	// kiểu, nullable, thứ tự) — FamilyProblems. Rỗng = view đứng một mình.
	Family  string   `json:"family,omitempty"`
	Columns []Column `json:"columns"`
}

var (
	reView      = regexp.MustCompile(`^([a-z][a-z0-9_]*_pub)\.v([1-9][0-9]*)_([a-z][a-z0-9_]*)$`)
	reDirective = regexp.MustCompile(`^([a-z][a-z0-9_]*_pub)\.([a-z][a-z0-9_]*)/v([1-9][0-9]*)$`)
	reColumn    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
)

// Problems trả lỗi siêu dữ liệu (rỗng = hợp lệ).
func (c Contract) Problems() []string {
	var p []string
	vm := reView.FindStringSubmatch(c.View)
	dm := reDirective.FindStringSubmatch(c.Directive)
	switch {
	case vm == nil:
		p = append(p, "view phải dạng <schema>_pub.v<N>_<tên>")
	case dm == nil:
		p = append(p, "directive phải dạng <schema>_pub.<tên>/v<N>")
	case vm[1] != dm[1] || vm[2] != dm[3] || vm[3] != dm[2]:
		p = append(p, fmt.Sprintf("view %s không khớp directive %s", c.View, c.Directive))
	}
	if !strings.HasPrefix(c.Owner, "pf/") {
		p = append(p, "owner phải là module pf/<tên>")
	}
	if c.Status != StatusPublished && c.Status != StatusPlanned {
		p = append(p, "status phải là published | planned")
	}
	if c.PersonalData {
		p = append(p, "view _pub không được có dữ liệu cá nhân (R-14)")
	}
	if strings.TrimSpace(c.Purpose) == "" || strings.TrimSpace(c.Grain) == "" || len(c.Consumers) == 0 {
		p = append(p, "thiếu purpose, grain hoặc consumers")
	}
	seen := map[string]bool{}
	for _, col := range c.Columns {
		if !reColumn.MatchString(col.Name) || seen[col.Name] {
			p = append(p, "cột sai tên hoặc trùng: "+col.Name)
		}
		seen[col.Name] = true
		if strings.TrimSpace(col.Type) == "" || strings.TrimSpace(col.Description) == "" {
			p = append(p, "cột thiếu type/description: "+col.Name)
		}
		if strings.Contains(col.Type, "double") || strings.Contains(col.Type, "real") {
			p = append(p, "cột số thực bị cấm (R-CODE-01): "+col.Name)
		}
	}
	if len(c.Columns) == 0 || !seen["tenant_id"] {
		p = append(p, "phải có cột, gồm tenant_id")
	}
	for _, k := range c.Key {
		if !seen[k] {
			p = append(p, "khóa không phải cột: "+k)
		}
	}
	return p
}

var reFamily = regexp.MustCompile(`^[a-z][a-z0-9_]*/v[1-9][0-9]*$`)

// FamilyProblems kiểm họ hợp đồng (ADR-0052): tên họ đúng dạng, đuôi phiên bản trùng phiên bản view, mỗi họ có ít nhất
// hai view của các module chủ khác nhau, mọi thành viên có cột giống hệt (tên, kiểu, nullable, thứ tự).
func FamilyProblems(cs []Contract) []string {
	var p []string
	byFamily := map[string][]Contract{}
	for _, c := range cs {
		if c.Family == "" {
			continue
		}
		if !reFamily.MatchString(c.Family) || lastVersion(c.Family) != lastVersion(c.Directive) {
			p = append(p, fmt.Sprintf("%s: họ %q phải dạng <tên>/v<N> cùng phiên bản với view", c.View, c.Family))
		}
		byFamily[c.Family] = append(byFamily[c.Family], c)
	}
	names := make([]string, 0, len(byFamily))
	for f := range byFamily {
		names = append(names, f)
	}
	sort.Strings(names)
	for _, f := range names {
		ms := byFamily[f]
		owners := map[string]bool{}
		for _, c := range ms {
			owners[c.Owner] = true
		}
		if len(ms) < 2 || len(owners) != len(ms) {
			p = append(p, fmt.Sprintf("họ %s cần ít nhất hai view của các module chủ khác nhau", f))
		}
		ref := ms[0]
		for _, c := range ms[1:] {
			if !sameColumns(ref.Columns, c.Columns) {
				p = append(p, fmt.Sprintf("họ %s: cột của %s khác %s", f, c.View, ref.View))
			}
		}
	}
	return p
}

func sameColumns(a, b []Column) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Type != b[i].Type || a[i].Nullable != b[i].Nullable {
			return false
		}
	}
	return true
}

// All trả mọi hợp đồng nhúng, sắp theo view.
func All() ([]Contract, error) {
	names, err := fs.Glob(files, "*.json")
	if err != nil {
		return nil, err
	}
	out := make([]Contract, 0, len(names))
	for _, n := range names {
		b, err := files.ReadFile(n)
		if err != nil {
			return nil, err
		}
		var c Contract
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		if want := strings.TrimSuffix(c.Directive, "/v"+lastVersion(c.Directive)) + ".v" + lastVersion(c.Directive) + ".json"; n != want {
			return nil, fmt.Errorf("%s: tên tệp phải là %s", n, want)
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].View < out[j].View })
	return out, nil
}

func lastVersion(directive string) string {
	if i := strings.LastIndex(directive, "/v"); i >= 0 {
		return directive[i+2:]
	}
	return ""
}
