package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
	"golang.org/x/tools/go/analysis"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

// ModGraph (W1.2, ADR-0027 mục 5, R-MOD-05) kiểm đồ thị module khai trong module.yaml:
//
//	cycle    đồ thị depends không chu trình
//	tier     phụ thuộc chỉ đi xuống theo kind: kernel ← capability ← domain (cùng tầng được nếu không chu trình)
//	depends  mã không phải kiểm thử import package của module khác (port/, vault client/) thì phải khai trong depends
//	version  ràng buộc phiên bản trong depends trỏ tới module có thật và thỏa phiên bản hiện có
//	schema   owns_schema khớp schema migration tạo; migration của module này không tạo/sửa đối tượng
//	         trong schema của module khác; migration không gắn module (tên không bắt đầu bằng
//	         <cụm>_<module>) không đụng schema của module nào
//
// Vi phạm hiện có được liệt kê trong tệp ngoại lệ (modgraph_exceptions.yaml), mỗi mục có lý do và
// gói kế hoạch sẽ gỡ. Vi phạm MỚI và ngoại lệ LỖI THỜI (không còn xảy ra) đều làm hỏng cổng, nên
// danh sách chỉ có thể co lại. Quy trình: docs/dev/modules.md.
var ModGraph = &analysis.Analyzer{
	Name: "agmodgraph",
	Doc:  "đồ thị module.yaml: không chu trình, chỉ đi xuống theo kind, import có trong depends, phiên bản thỏa, owns_schema khớp migration (W1.2)",
	Run:  runModGraph,
}

// Cờ của analyzer (multichecker thêm tiền tố agmodgraph.).
var (
	mgRootFlag       string
	mgExceptionsFlag = "tools/archlint/modgraph_exceptions.yaml"
)

func init() {
	ModGraph.Flags.StringVar(&mgRootFlag, "root", "", "gốc repo (mặc định: thư mục chứa go.mod gần nhất)")
	ModGraph.Flags.StringVar(&mgExceptionsFlag, "exceptions", mgExceptionsFlag, "tệp ngoại lệ, tương đối gốc repo")
}

// Các quy tắc.
const (
	ruleCycle   = "cycle"
	ruleTier    = "tier"
	ruleDepends = "depends"
	ruleVersion = "version"
	ruleSchema  = "schema"
)

// exceptableRules: depends không có ngoại lệ — modgen giữ depends khớp import.
var exceptableRules = []string{ruleCycle, ruleTier, ruleVersion, ruleSchema}

type mgViolation struct {
	Rule   string
	From   string // module, hoặc đường dẫn migration
	To     string // module, hoặc schema
	Msg    string
	File   string // depends: tệp .go (tương đối gốc, gạch xuôi)
	Import string // depends: đường dẫn import
	Anchor string // module để báo lỗi ("" = toàn cục)
}

func (v mgViolation) key() string { return v.Rule + "|" + v.From + "|" + v.To }

func (v mgViolation) String() string {
	return fmt.Sprintf("agmodgraph[%s] %s → %s: %s", v.Rule, v.From, v.To, v.Msg)
}

type mgException struct {
	Rule   string `yaml:"rule"`
	From   string `yaml:"from"`
	To     string `yaml:"to"`
	Reason string `yaml:"reason"`
	Owner  string `yaml:"owner"` // gói kế hoạch sẽ gỡ, vd W3.3
	Until  string `yaml:"until"`
}

func (e mgException) key() string { return e.Rule + "|" + e.From + "|" + e.To }

// mgOpsSchema là schema vận hành/dbadmin (vd vai giám sát sql_exporter) do migration tạo,
// không thuộc module nào. Được phép lâu dài, nhưng phải có lý do và còn được tạo.
type mgOpsSchema struct {
	Schema string `yaml:"schema"`
	Reason string `yaml:"reason"`
}

type mgExceptionsFile struct {
	OpsSchemas []mgOpsSchema `yaml:"ops_schemas"`
	Exceptions []mgException `yaml:"exceptions"`
}

type mgResult struct {
	Violations []mgViolation // chưa được ngoại lệ che
	Stale      []mgException // ngoại lệ không còn xảy ra
	StaleOps   []string      // ops_schemas lỗi thời (không còn tạo, hoặc đã có chủ module)
	Excepted   int
	anchorOf   map[string]string // khóa ngoại lệ lỗi thời → module để báo
}

var reOwner = regexp.MustCompile(`^W[0-9]+\.[0-9]+$`)

func loadExceptions(p string) (*mgExceptionsFile, error) {
	var f mgExceptionsFile
	data, err := os.ReadFile(p) //nolint:gosec // tệp cấu hình trong repo
	if errors.Is(err, fs.ErrNotExist) {
		return &f, nil
	}
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) { // tệp rỗng: không ngoại lệ
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	var errs []error
	seen := map[string]bool{}
	for i, e := range f.Exceptions {
		bad := func(msg string) {
			errs = append(errs, fmt.Errorf("%s: mục %d (%s %s → %s): %s", p, i+1, e.Rule, e.From, e.To, msg))
		}
		if !slices.Contains(exceptableRules, e.Rule) {
			bad("rule phải thuộc " + strings.Join(exceptableRules, ", "))
		}
		if e.From == "" || e.To == "" {
			bad("from, to bắt buộc")
		}
		if strings.TrimSpace(e.Reason) == "" {
			bad("reason bắt buộc")
		}
		if !reOwner.MatchString(e.Owner) {
			bad("owner phải là gói kế hoạch dạng W<n>.<m>")
		}
		if strings.TrimSpace(e.Until) == "" {
			bad("until bắt buộc")
		}
		if seen[e.key()] {
			bad("lặp")
		}
		seen[e.key()] = true
	}
	for _, o := range f.OpsSchemas {
		if o.Schema == "" || strings.TrimSpace(o.Reason) == "" {
			errs = append(errs, fmt.Errorf("%s: ops_schemas %q: schema, reason bắt buộc", p, o.Schema))
		}
		if seen["ops|"+o.Schema] {
			errs = append(errs, fmt.Errorf("%s: ops_schemas %q lặp", p, o.Schema))
		}
		seen["ops|"+o.Schema] = true
	}
	return &f, errors.Join(errs...)
}

// checkModGraph chạy mọi quy tắc trên repo ở root. modPath là đường dẫn Go module.
func checkModGraph(root, modPath, exceptionsPath string) (*mgResult, error) {
	ms, err := modreg.ReadAll(os.DirFS(root))
	if err != nil {
		return nil, err
	}
	byName := map[string]*modreg.Manifest{}
	var verrs []error
	for _, m := range ms {
		if err := m.Validate(); err != nil {
			verrs = append(verrs, err)
		}
		byName[m.Name] = m
	}
	if err := errors.Join(verrs...); err != nil {
		return nil, fmt.Errorf("module.yaml không hợp lệ (chạy go run ./tools/modgen -check):\n%w", err)
	}
	ef, err := loadExceptions(filepath.Join(root, filepath.FromSlash(exceptionsPath)))
	if err != nil {
		return nil, err
	}
	exc := ef.Exceptions

	var vs []mgViolation
	vs = append(vs, graphViolations(ms, byName)...)
	dv, err := dependsViolations(root, modPath, ms)
	if err != nil {
		return nil, err
	}
	vs = append(vs, dv...)
	sv, staleOps, err := schemaViolations(root, ms, ef.OpsSchemas)
	if err != nil {
		return nil, err
	}
	vs = append(vs, sv...)

	res := &mgResult{anchorOf: map[string]string{}, StaleOps: staleOps}
	excByKey := map[string]mgException{}
	for _, e := range exc {
		excByKey[e.key()] = e
	}
	used := map[string]bool{}
	seen := map[string]bool{}
	for _, v := range vs {
		k := v.key()
		if v.Rule == ruleDepends {
			k += "|" + v.File
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		if _, ok := excByKey[v.key()]; ok {
			used[v.key()] = true
			res.Excepted++
			continue
		}
		res.Violations = append(res.Violations, v)
	}
	schemaOwner := map[string]string{}
	for _, m := range ms {
		for _, s := range m.OwnsSchema {
			schemaOwner[s] = m.Name
		}
	}
	for _, e := range exc {
		if used[e.key()] {
			continue
		}
		res.Stale = append(res.Stale, e)
		switch {
		case byName[e.From] != nil:
			res.anchorOf[e.key()] = e.From
		case byName[e.To] != nil:
			res.anchorOf[e.key()] = e.To
		default:
			res.anchorOf[e.key()] = schemaOwner[e.To]
		}
	}
	return res, nil
}

// graphViolations: cycle, tier, version.
func graphViolations(ms []*modreg.Manifest, byName map[string]*modreg.Manifest) []mgViolation {
	var out []mgViolation
	for _, m := range ms {
		for _, dep := range m.DependNames() {
			d := byName[dep]
			if d == nil {
				out = append(out, mgViolation{Rule: ruleVersion, From: m.Name, To: dep, Anchor: m.Name, Msg: "depends trỏ tới module không có"})
				continue
			}
			c, err := modreg.ParseConstraint(m.Depends[dep])
			if err != nil {
				out = append(out, mgViolation{Rule: ruleVersion, From: m.Name, To: dep, Anchor: m.Name, Msg: err.Error()})
			} else if dv := d.SemVer(); !c.Allows(dv) {
				out = append(out, mgViolation{Rule: ruleVersion, From: m.Name, To: dep, Anchor: m.Name,
					Msg: fmt.Sprintf("ràng buộc %q không thỏa phiên bản hiện có %s", c, dv)})
			}
			if m.Kind.Rank() < d.Kind.Rank() {
				out = append(out, mgViolation{Rule: ruleTier, From: m.Name, To: dep, Anchor: m.Name,
					Msg: fmt.Sprintf("phụ thuộc đi lên: %s (%s) không được phụ thuộc %s (%s); chỉ kernel ← capability ← domain (R-MOD-05)", m.Name, m.Kind, dep, d.Kind)})
			}
		}
	}
	comp := sccs(ms, byName)
	members := map[int][]string{}
	for n, c := range comp {
		members[c] = append(members[c], n)
	}
	for _, m := range ms {
		for _, dep := range m.DependNames() {
			if byName[dep] == nil || comp[m.Name] != comp[dep] {
				continue
			}
			ring := members[comp[m.Name]]
			sort.Strings(ring)
			out = append(out, mgViolation{Rule: ruleCycle, From: m.Name, To: dep, Anchor: m.Name,
				Msg: fmt.Sprintf("cạnh nằm trong chu trình {%s}", strings.Join(ring, ", "))})
		}
	}
	return out
}

// sccs trả chỉ số thành phần liên thông mạnh (Tarjan) cho mỗi module; thành phần một
// đỉnh (không tự vòng — modreg cấm) không phải chu trình nên được gán chỉ số riêng âm.
func sccs(ms []*modreg.Manifest, byName map[string]*modreg.Manifest) map[string]int {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	comp := map[string]int{}
	next, nComp := 0, 0
	var strong func(v string)
	strong = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range byName[v].DependNames() {
			if byName[w] == nil {
				continue
			}
			if _, ok := index[w]; !ok {
				strong(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			var group []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				group = append(group, w)
				if w == v {
					break
				}
			}
			id := nComp
			if len(group) == 1 {
				id = -1 - nComp
			}
			for _, w := range group {
				comp[w] = id
			}
			nComp++
		}
	}
	for _, m := range ms {
		if _, ok := index[m.Name]; !ok {
			strong(m.Name)
		}
	}
	return comp
}

// dataContractOnly: tệp f chỉ dùng hợp đồng dữ liệu của gói ip — hằng (tên sự kiện, tên vai, khóa tham
// số, giá trị liệt kê) và kiểu payload //ag:event / //ag:internal-event. Import như vậy không tạo
// depends (§7.1 tách consumes khỏi depends; modgen phân loại cùng quy tắc và ghi consumes.events).
// Import trống/chấm, hoặc dùng định danh khác (interface, hàm, biến, kiểu thường) → false.
func dataContractOnly(root, modPath string, f *ast.File, imp *ast.ImportSpec, ip string) (bool, error) {
	name := ip[strings.LastIndex(ip, "/")+1:]
	if imp.Name != nil {
		name = imp.Name.Name
	}
	if name == "_" || name == "." {
		return false, nil
	}
	data, err := portDataSymbols(root, modPath, ip)
	if err != nil {
		return false, err
	}
	only := true
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == name && x.Obj == nil && !data[sel.Sel.Name] {
				only = false
			}
		}
		return only
	})
	return only, nil
}

var (
	portDataMu       sync.Mutex
	portDataCache    = map[string]map[string]bool{}
	reEventDirective = regexp.MustCompile(`^//ag:(event|internal-event)\s`)
)

// portDataSymbols trả các định danh hợp đồng dữ liệu (hằng, kiểu payload sự kiện) của gói ip.
func portDataSymbols(root, modPath, ip string) (map[string]bool, error) {
	portDataMu.Lock()
	defer portDataMu.Unlock()
	if d, ok := portDataCache[ip]; ok {
		return d, nil
	}
	dir := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(ip, modPath), "/")))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	out := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				switch sp := spec.(type) {
				case *ast.ValueSpec:
					if gd.Tok == token.CONST {
						for _, n := range sp.Names {
							out[n.Name] = true
						}
					}
				case *ast.TypeSpec:
					doc := sp.Doc
					if doc == nil && len(gd.Specs) == 1 {
						doc = gd.Doc
					}
					if doc != nil && slices.ContainsFunc(doc.List, func(c *ast.Comment) bool { return reEventDirective.MatchString(c.Text) }) {
						out[sp.Name.Name] = true
					}
				}
			}
		}
	}
	portDataCache[ip] = out
	return out, nil
}

// dependsViolations: import (mã không phải kiểm thử) tới module khác phải có trong depends, trừ import chỉ
// dùng hợp đồng dữ liệu (dataContractOnly).
func dependsViolations(root, modPath string, ms []*modreg.Manifest) ([]mgViolation, error) {
	var out []mgViolation
	fset := token.NewFileSet()
	for _, m := range ms {
		dir := filepath.Join(root, filepath.FromSlash(m.Dir()))
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			for _, imp := range f.Imports {
				ip, _ := strconv.Unquote(imp.Path.Value)
				dep := moduleOfImport(modPath, ip)
				if dep == "" || dep == m.Name {
					continue
				}
				if _, ok := m.Depends[dep]; ok {
					continue
				}
				dataOnly, err := dataContractOnly(root, modPath, f, imp, ip)
				if err != nil {
					return err
				}
				if !dataOnly {
					out = append(out, mgViolation{Rule: ruleDepends, From: m.Name, To: dep, File: rel, Import: ip, Anchor: m.Name,
						Msg: fmt.Sprintf("%s import %s nhưng module.yaml không khai depends %s (chạy go run ./tools/modgen)", rel, ip, dep)})
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// moduleOfImport trả tên module (pf/x, mkg/x, vault/x) của một import trong repo, hoặc "".
func moduleOfImport(modPath, ip string) string {
	rel, ok := strings.CutPrefix(ip, modPath+"/")
	if !ok {
		return ""
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 3 {
		return ""
	}
	return modreg.NameForDir(strings.Join(parts[:3], "/"))
}

const sqlIdent = `"?([a-z_][a-z0-9_]*)"?\s*\.\s*"?[a-z_]`

var (
	reSQLCreateSchema = regexp.MustCompile(`(?i)\bCREATE\s+SCHEMA\s+(?:IF\s+NOT\s+EXISTS\s+)?"?([a-z_][a-z0-9_]*)`)
	// reSQLTouch: câu DDL tạo/sửa/xóa đối tượng trong một schema (nhóm 1 là schema).
	reSQLTouch = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:UNLOGGED\s+)?(?:TABLE|VIEW|MATERIALIZED\s+VIEW|FUNCTION|PROCEDURE|SEQUENCE|TYPE|DOMAIN|AGGREGATE)\s+(?:IF\s+NOT\s+EXISTS\s+)?` + sqlIdent),
		regexp.MustCompile(`(?i)\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(?:[a-z_][a-z0-9_]*\s+)?ON\s+(?:ONLY\s+)?` + sqlIdent),
		regexp.MustCompile(`(?i)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:CONSTRAINT\s+)?TRIGGER\s+[a-z_][a-z0-9_]*\s+[^;]*?\bON\s+` + sqlIdent),
		regexp.MustCompile(`(?i)\bCREATE\s+POLICY\s+[a-z_][a-z0-9_]*\s+ON\s+` + sqlIdent),
		regexp.MustCompile(`(?i)\bALTER\s+(?:TABLE|VIEW|MATERIALIZED\s+VIEW|FUNCTION|PROCEDURE|SEQUENCE|TYPE|DOMAIN|INDEX)\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?` + sqlIdent),
		regexp.MustCompile(`(?i)\bDROP\s+(?:TABLE|VIEW|MATERIALIZED\s+VIEW|FUNCTION|PROCEDURE|SEQUENCE|TYPE|DOMAIN|INDEX)\s+(?:CONCURRENTLY\s+)?(?:IF\s+EXISTS\s+)?` + sqlIdent),
		regexp.MustCompile(`(?i)\bDROP\s+(?:TRIGGER|POLICY)\s+(?:IF\s+EXISTS\s+)?[a-z_][a-z0-9_]*\s+ON\s+` + sqlIdent),
		regexp.MustCompile(`(?i)\bCOMMENT\s+ON\s+(?:TABLE|COLUMN|VIEW|MATERIALIZED\s+VIEW|FUNCTION|INDEX|SEQUENCE|TYPE)\s+` + sqlIdent),
		regexp.MustCompile(`(?i)\b(?:ALTER|DROP)\s+SCHEMA\s+(?:IF\s+EXISTS\s+)?"?([a-z_][a-z0-9_]*)`),
	}
	reSQLLineComment  = regexp.MustCompile(`--[^\n]*`)
	reSQLBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reMigrationName   = regexp.MustCompile(`^[0-9]+_([a-z0-9_]+)\.up\.sql$`)
)

// libraryCluster: migration của thư viện internal/shared (ADR-0027 mục 1) — schema nó tạo không cần chủ module.
const libraryCluster = "shared"

type migration struct {
	path    string // tương đối gốc, gạch xuôi
	cluster string
	module  string // module gắn theo tên tệp, "" nếu không gắn
	creates []string
	touches []string
}

func readMigrations(root string, ms []*modreg.Manifest) ([]migration, error) {
	dir := filepath.Join(root, "migrations")
	clusters, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, c := range clusters {
		if !c.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(dir, c.Name(), "*.up.sql"))
		sort.Strings(files)
		for _, f := range files {
			data, err := os.ReadFile(f) //nolint:gosec // migration trong repo
			if err != nil {
				return nil, err
			}
			sql := reSQLBlockComment.ReplaceAllString(string(data), " ")
			sql = reSQLLineComment.ReplaceAllString(sql, " ")
			mg := migration{path: "migrations/" + c.Name() + "/" + filepath.Base(f), cluster: c.Name()}
			if mm := reMigrationName.FindStringSubmatch(filepath.Base(f)); mm != nil {
				mg.module = migrationModule(mm[1], ms)
			}
			for _, mm := range reSQLCreateSchema.FindAllStringSubmatch(sql, -1) {
				mg.creates = appendUnique(mg.creates, strings.ToLower(mm[1]))
			}
			mg.touches = slices.Clone(mg.creates)
			for _, re := range reSQLTouch {
				for _, mm := range re.FindAllStringSubmatch(sql, -1) {
					mg.touches = appendUnique(mg.touches, strings.ToLower(mm[1]))
				}
			}
			out = append(out, mg)
		}
	}
	return out, nil
}

// migrationModule gắn migration "<cụm>_<module>[_…]" với module dài nhất khớp tiền tố.
func migrationModule(stem string, ms []*modreg.Manifest) string {
	best := ""
	for _, m := range ms {
		key := strings.Replace(m.Name, "/", "_", 1)
		if (stem == key || strings.HasPrefix(stem, key+"_")) && len(m.Name) > len(best) {
			best = m.Name
		}
	}
	return best
}

func appendUnique(xs []string, x string) []string {
	if slices.Contains(xs, x) {
		return xs
	}
	return append(xs, x)
}

// schemaViolations: owns_schema ↔ CREATE SCHEMA; migration không đụng schema của module khác.
func schemaViolations(root string, ms []*modreg.Manifest, ops []mgOpsSchema) ([]mgViolation, []string, error) {
	migs, err := readMigrations(root, ms)
	if err != nil {
		return nil, nil, err
	}
	owner := map[string]string{}
	for _, m := range ms {
		for _, s := range m.OwnsSchema {
			owner[s] = m.Name
		}
	}
	isOps := map[string]bool{}
	for _, o := range ops {
		isOps[o.Schema] = true
	}
	for i := range migs {
		inferModule(&migs[i], owner)
	}
	created := map[string]bool{}
	var out []mgViolation
	for _, mg := range migs {
		for _, s := range mg.creates {
			created[s] = true
			if owner[s] == "" && mg.cluster != libraryCluster && !isOps[s] {
				out = append(out, mgViolation{Rule: ruleSchema, From: mg.path, To: s, Anchor: mg.module,
					Msg: "migration tạo schema không thuộc owns_schema của module nào"})
			}
		}
		for _, s := range mg.touches {
			o := owner[s]
			if o == "" || o == mg.module {
				continue
			}
			who := "migration không gắn module (tên tệp không bắt đầu bằng <cụm>_<module>)"
			anchor := o
			if mg.module != "" {
				who = "migration của " + mg.module
				anchor = mg.module
			}
			out = append(out, mgViolation{Rule: ruleSchema, From: mg.path, To: s, Anchor: anchor,
				Msg: fmt.Sprintf("%s tạo/sửa đối tượng trong schema %s của %s", who, s, o)})
		}
	}
	for _, m := range ms {
		for _, s := range m.OwnsSchema {
			if !created[s] {
				out = append(out, mgViolation{Rule: ruleSchema, From: m.Name, To: s, Anchor: m.Name,
					Msg: "owns_schema khai schema mà không migration nào CREATE SCHEMA"})
			}
		}
	}
	var staleOps []string
	for _, o := range ops {
		switch {
		case !created[o.Schema]:
			staleOps = append(staleOps, o.Schema+": không migration nào CREATE SCHEMA")
		case owner[o.Schema] != "":
			staleOps = append(staleOps, o.Schema+": đã thuộc owns_schema của "+owner[o.Schema])
		}
	}
	return out, staleOps, nil
}

// inferModule: migration không gắn module theo tên mà chỉ đụng schema của MỘT module
// (vd pf 000010_pf_files → pf/sync, ts 000001_ts_weigh → mkg/measurement) được gắn module đó.
func inferModule(mg *migration, owner map[string]string) {
	if mg.module != "" {
		return
	}
	only := ""
	for _, s := range mg.touches {
		o := owner[s]
		if o == "" {
			continue
		}
		if only != "" && only != o {
			return
		}
		only = o
	}
	mg.module = only
}

// --- analyzer ---

var (
	mgCacheMu sync.Mutex
	mgCache   = map[string]*mgCached{}
)

type mgCached struct {
	res        *mgResult
	err        error
	globalOnce sync.Once
}

func findRepoRoot(file string) string {
	for d := filepath.Dir(file); ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			return ""
		}
		d = p
	}
}

var zonePrefix = map[string]string{"platform": "pf", "mkg": "mkg", "vault": "vault"}

func runModGraph(pass *analysis.Pass) (any, error) {
	if len(pass.Files) == 0 {
		return nil, nil
	}
	root := mgRootFlag
	if root == "" {
		root = findRepoRoot(pass.Fset.File(pass.Files[0].Pos()).Name())
		if root == "" {
			return nil, nil
		}
	}
	mgCacheMu.Lock()
	c := mgCache[root]
	if c == nil {
		c = &mgCached{}
		c.res, c.err = checkModGraph(root, modulePath, mgExceptionsFlag)
		mgCache[root] = c
	}
	mgCacheMu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	var global []string
	for _, v := range c.res.Violations {
		if v.Anchor == "" {
			global = append(global, v.String())
		}
	}
	for _, e := range c.res.Stale {
		if c.res.anchorOf[e.key()] == "" {
			global = append(global, staleMsg(e))
		}
	}
	for _, s := range c.res.StaleOps {
		global = append(global, "agmodgraph: ops_schemas lỗi thời "+s+" — xóa khỏi "+mgExceptionsFlag)
	}
	if len(global) > 0 {
		var err error
		c.globalOnce.Do(func() { err = errors.New(strings.Join(global, "\n")) })
		if err != nil {
			return nil, err
		}
	}

	pkgPath := pass.Pkg.Path()
	if strings.HasSuffix(pkgPath, "_test") {
		return nil, nil // gói kiểm thử ngoài: import chỉ của kiểm thử
	}
	from := locate(pkgPath)
	if !from.internal || zonePrefix[from.zone] == "" || from.module == "" {
		return nil, nil
	}
	mod := zonePrefix[from.zone] + "/" + from.module

	var firstFile *token.Pos
	for _, f := range pass.Files {
		name := pass.Fset.File(f.Pos()).Name()
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if firstFile == nil {
			p := f.Name.Pos()
			firstFile = &p
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, v := range c.res.Violations {
			if v.Rule != ruleDepends || v.File != rel {
				continue
			}
			for _, spec := range f.Imports {
				if ip, _ := strconv.Unquote(spec.Path.Value); ip == v.Import {
					pass.Reportf(spec.Pos(), "%s", v)
				}
			}
		}
	}
	if from.rest != "" || firstFile == nil {
		return nil, nil
	}
	// Gốc module: báo vi phạm đồ thị/schema và ngoại lệ lỗi thời của module này.
	for _, v := range c.res.Violations {
		if v.Rule != ruleDepends && v.Anchor == mod {
			pass.Reportf(*firstFile, "%s", v)
		}
	}
	for _, e := range c.res.Stale {
		if c.res.anchorOf[e.key()] == mod {
			pass.Reportf(*firstFile, "%s", staleMsg(e))
		}
	}
	return nil, nil
}

func staleMsg(e mgException) string {
	return fmt.Sprintf("agmodgraph: ngoại lệ lỗi thời [%s] %s → %s (%s) không còn xảy ra — xóa khỏi %s", e.Rule, e.From, e.To, e.Owner, mgExceptionsFlag)
}
