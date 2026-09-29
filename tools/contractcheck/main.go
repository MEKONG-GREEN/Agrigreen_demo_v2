// Lệnh contractcheck là cổng CI cho hợp đồng:
//
//   - Sự kiện (R-64): contracts/events/<event_type>.v<N>.json khớp tên và phiên
//     bản; struct Go chú thích //ag:event <type> v<N> không có trường ngoài
//     schema, có đủ trường required, KIỂU từng trường khớp schema — đệ quy vào
//     object và mảng lồng nhau; so với nhánh gốc (-base) chỉ được thêm trường
//     tùy chọn — xóa trường, đổi kiểu, thêm required → phải tăng phiên bản.
//   - Van công bố (mục 5, R-03): contracts/publication/<tên>.json phải có owner,
//     proposed_by, approved_by (≠ proposed_by, R-56), approved_at; struct chú
//     thích //ag:publication <tên> chỉ được có trường của hợp đồng, đúng kiểu,
//     đệ quy; không có hợp đồng → hỏng (mặc định từ chối). Vùng mkg: mọi payload
//     sự kiện phải là hợp đồng van (xem mkg.go).
//
// Chạy: go run ./tools/contractcheck [-base origin/main]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
)

func main() {
	root := flag.String("root", ".", "gốc repo")
	base := flag.String("base", "", "ref git để so tương thích ngược, vd origin/main (rỗng = bỏ qua)")
	flag.Parse()
	problems, err := Check(*root, *base)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, p := range problems {
		fmt.Println(p)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
}

// Schema là phần JSON Schema mà cổng quan tâm.
type Schema struct {
	EventType     string `json:"x-event-type"`
	SchemaVersion int    `json:"x-schema-version"`
	publication.Node
	raw map[string]json.RawMessage // properties thô, để so tương thích ngược
}

var (
	eventFileRe = regexp.MustCompile(`^((?:pf|mkg|vault|edge)\.[a-z_]+\.[a-z_]+\.[a-z_]+)\.v(\d+)\.json$`)
	// Chỉ dòng chỉ thị (bắt đầu bằng //ag:, không có khoảng trắng) — nhắc tới
	// chỉ thị trong văn xuôi của chú thích không phải là chú thích hợp đồng.
	eventTagRe = regexp.MustCompile(`(?m)^//ag:event\s+(\S+)\s+v(\d+)\b`)
	pubTagRe   = regexp.MustCompile(`(?m)^//ag:publication\s+(\S+)`)
)

func parseSchema(raw []byte) (Schema, error) {
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	var r struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	_ = json.Unmarshal(raw, &r)
	s.raw = r.Properties
	return s, nil
}

// annotated là struct có chú thích hợp đồng; một struct có thể mang cả hai.
type annotated struct {
	decl        *typeDecl
	pos         string
	event       string // "<type> v<N>"
	publication string
}

// Check chạy toàn bộ kiểm tra.
func Check(root, base string) ([]string, error) {
	var problems []string
	schemas := map[string]Schema{}
	evDir := filepath.Join(root, "contracts", "events")
	files, _ := filepath.Glob(filepath.Join(evDir, "*.json"))
	for _, f := range files {
		name := filepath.Base(f)
		if strings.HasPrefix(name, "envelope.") {
			continue
		}
		m := eventFileRe.FindStringSubmatch(name)
		if m == nil {
			problems = append(problems, f+": tên tệp phải là <vùng>.<module>.<thực thể>.<động từ>.v<N>.json (R-64)")
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		s, err := parseSchema(raw)
		if err != nil {
			problems = append(problems, f+": JSON không hợp lệ: "+err.Error())
			continue
		}
		v, _ := strconv.Atoi(m[2])
		if s.EventType != m[1] || s.SchemaVersion != v {
			problems = append(problems, fmt.Sprintf("%s: x-event-type/x-schema-version (%s v%d) không khớp tên tệp", f, s.EventType, s.SchemaVersion))
		}
		for _, r := range s.Required {
			if _, ok := s.Properties[r]; !ok {
				problems = append(problems, f+": required "+r+" không có trong properties")
			}
		}
		schemas[fmt.Sprintf("%s v%d", m[1], v)] = s
		if base != "" {
			problems = append(problems, compat(root, base, f, s)...)
		}
	}

	pubs := map[string]*publication.Contract{}
	pubFiles, _ := filepath.Glob(filepath.Join(root, "contracts", "publication", "*.json"))
	for _, f := range pubFiles {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		c, err := publication.Parse(raw)
		if err != nil || c.Name == "" {
			problems = append(problems, f+": hợp đồng công bố không hợp lệ")
			continue
		}
		bad := false
		for _, mp := range c.MetaProblems() {
			problems = append(problems, f+": "+mp)
			bad = true
		}
		if want := strings.TrimSuffix(filepath.Base(f), ".json"); c.Name != want {
			problems = append(problems, fmt.Sprintf("%s: name %q phải trùng tên tệp %q", f, c.Name, want))
			bad = true
		}
		if ev, ver, ok := c.EventType(); ok {
			s, found := schemas[fmt.Sprintf("%s v%d", ev, ver)]
			if !found {
				problems = append(problems, fmt.Sprintf("%s: event %s v%d không có schema trong contracts/events/", f, ev, ver))
			} else if c.Typed() {
				// Sự kiện mang payload van không được khai trường mà van không cho qua.
				for k := range s.Properties {
					if _, ok := c.Properties[k]; !ok {
						problems = append(problems, fmt.Sprintf("%s: schema sự kiện %s v%d có trường %s ngoài hợp đồng van", f, ev, ver, k))
					}
				}
			}
		}
		if !bad { // hợp đồng thiếu người phê duyệt coi như không có: mặc định từ chối
			pubs[c.Name] = c
		}
	}

	ix, err := buildIndex(root)
	if err != nil {
		return nil, err
	}
	ann := map[string]*annotated{}
	for pkg, types := range ix.types {
		for name, d := range types {
			a := &annotated{decl: d, pos: ix.pos(d.pos)}
			if m := eventTagRe.FindStringSubmatch(d.doc); m != nil {
				a.event = m[1] + " v" + m[2]
			}
			if m := pubTagRe.FindStringSubmatch(d.doc); m != nil {
				a.publication = m[1]
			}
			if a.event != "" || a.publication != "" {
				ann[annKey(pkg, name)] = a
			}
		}
	}
	for _, a := range ann {
		st, isStruct := a.decl.expr.(*ast.StructType)
		if !isStruct {
			problems = append(problems, fmt.Sprintf("%s: %s có chú thích hợp đồng nhưng không phải struct", a.pos, a.decl.name))
			continue
		}
		if a.event != "" {
			s, ok := schemas[a.event]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: struct %s chú thích ag:event %s nhưng không có schema trong contracts/events/", a.pos, a.decl.name, a.event))
			} else {
				problems = append(problems, ix.checkStruct(a.decl.name, "", st, a.decl.file, &s.Node, "schema "+a.event, 0)...)
			}
		}
		if a.publication != "" {
			c, ok := pubs[a.publication]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: %s chú thích ag:publication %s nhưng không có hợp đồng đã phê duyệt — mặc định từ chối", a.pos, a.decl.name, a.publication))
				continue
			}
			what := "hợp đồng công bố " + a.publication
			if c.Typed() {
				problems = append(problems, ix.checkStruct(a.decl.name, "", st, a.decl.file, &c.Node, what, 0)...)
			} else {
				for _, f := range ix.jsonFields(st, a.decl.file, 0) {
					if !contains(c.Fields, f.name) {
						problems = append(problems, fmt.Sprintf("%s: %s.%s không có trong %s", a.pos, a.decl.name, f.name, what))
					}
				}
			}
			if ev, ver, ok := c.EventType(); ok && a.event != "" && a.event != fmt.Sprintf("%s v%d", ev, ver) {
				problems = append(problems, fmt.Sprintf("%s: %s chú thích ag:event %s nhưng hợp đồng %s khai event %s v%d", a.pos, a.decl.name, a.event, a.publication, ev, ver))
			}
		}
	}
	problems = append(problems, ix.mkgRules(ann)...)
	sort.Strings(problems)
	return problems, nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// compat so schema hiện tại với bản ở ref git base.
func compat(root, base, path string, cur Schema) []string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil
	}
	out, err := exec.Command("git", "-C", root, "show", base+":"+filepath.ToSlash(rel)).Output()
	if err != nil {
		return nil // tệp mới hoặc không có git: không có gì để so
	}
	old, err := parseSchema(out)
	if err != nil {
		return nil
	}
	var problems []string
	for name, oldDef := range old.raw {
		newDef, ok := cur.raw[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: xóa trường %s — phải tăng schema_version (R-64)", path, name))
			continue
		}
		if typeOf(oldDef) != typeOf(newDef) {
			problems = append(problems, fmt.Sprintf("%s: đổi kiểu trường %s (%s → %s) — phải tăng schema_version (R-64)", path, name, typeOf(oldDef), typeOf(newDef)))
		}
	}
	for _, r := range cur.Required {
		if !contains(old.Required, r) {
			problems = append(problems, fmt.Sprintf("%s: thêm trường bắt buộc %s — consumer cũ sẽ hỏng, phải tăng schema_version (R-64)", path, r))
		}
	}
	return problems
}

func typeOf(raw json.RawMessage) string {
	var d struct {
		Type any `json:"type"`
	}
	_ = json.Unmarshal(raw, &d)
	return fmt.Sprint(d.Type)
}
