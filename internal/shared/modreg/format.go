package modreg

import (
	"sort"
	"strconv"
	"strings"
)

// header đứng đầu mọi module.yaml do tools/modgen ghi.
const header = `# Tệp khai báo module — ADR-0027, kiến trúc v4.1 §7.1, docs/dev/modules.md.
# Trường SINH (go run ./tools/modgen): depends (khóa), provides, permissions, owns_schema.
# Trường TAY: name, aka, kind, zones, version, ràng buộc trong depends, shares, enable.
`

// Format ghi manifest theo dạng chuẩn (thứ tự trường cố định, danh sách đã sắp).
// tools/modgen -check so sánh tệp trên đĩa với Format của bản tính lại.
func Format(m *Manifest) []byte {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("name: " + m.Name + "\n")
	b.WriteString("aka:" + flow(m.Aka, false) + "\n")
	b.WriteString("kind: " + string(m.Kind) + "\n")
	b.WriteString("zones:" + flow(m.Zones, false) + "\n")
	b.WriteString("version: " + m.Version + "\n")
	if len(m.Depends) == 0 {
		b.WriteString("depends: {}\n")
	} else {
		b.WriteString("depends:\n")
		for _, d := range m.DependNames() {
			b.WriteString("  " + d + ": " + strconv.Quote(m.Depends[d]) + "\n")
		}
	}
	b.WriteString("provides:\n")
	b.WriteString("  ports:" + flow(m.Provides.Ports, true) + "\n")
	b.WriteString("  events:" + flow(m.Provides.Events, true) + "\n")
	b.WriteString("  internal_events:" + flow(m.Provides.InternalEvents, true) + "\n")
	b.WriteString("  publications:" + flow(m.Provides.Publications, true) + "\n")
	b.WriteString("  data_products:" + flow(m.Provides.DataProducts, true) + "\n")
	b.WriteString("permissions:" + flow(m.Permissions, true) + "\n")
	b.WriteString("shares:" + flow(m.Shares, true) + "\n")
	b.WriteString("owns_schema:" + flow(m.OwnsSchema, true) + "\n")
	b.WriteString("enable: " + string(m.Enable) + "\n")
	return []byte(b.String())
}

// flow ghi danh sách dạng " [a, b]" (có dấu cách đầu); danh sách dài xuống dòng mỗi
// phần tử một dòng.
func flow(xs []string, sorted bool) string {
	if len(xs) == 0 {
		return " []"
	}
	if sorted {
		xs = append([]string(nil), xs...)
		sort.Strings(xs)
	}
	one := " [" + strings.Join(xs, ", ") + "]"
	if len(one) <= 80 {
		return one
	}
	var b strings.Builder
	for _, x := range xs {
		b.WriteString("\n    - " + x)
	}
	return b.String()
}
