package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Các quy tắc (mô tả ở boundary.yaml).
const (
	RuleImportIntoKN     = "import-into-kn"
	RuleKNImportInternal = "kn-import-internal"
	RuleKNImportHTX      = "kn-import-htx"
	RuleKNWriteHTX       = "kn-write-htx"
	RuleKNSQLForeign     = "kn-sql-foreign"
	RuleSQLIntoKN        = "sql-into-kn"
	RuleKNSymbolInShared = "kn-symbol-in-shared"
	RuleRouteOwner       = "route-owner"
	RuleWebImportIntoKN  = "web-import-into-kn"
	RuleWebKNImportApp   = "web-kn-import-app"
)

// Config là boundary.yaml.
type Config struct {
	CanonicalPaths     map[string]string `yaml:"canonical_paths"`
	KNModules          []string          `yaml:"kn_modules"`
	HTXModules         []string          `yaml:"htx_modules"`
	CompositionRoots   []string          `yaml:"composition_roots"`
	TestRoots          []string          `yaml:"test_roots"`
	GoRoots            []string          `yaml:"go_roots"`
	SymbolRoots        []string          `yaml:"symbol_roots"`
	MigrationsRoot     string            `yaml:"migrations_root"`
	KNMigrationPattern string            `yaml:"kn_migration_pattern"`
	FreeSchemas        []string          `yaml:"free_schemas"`
	SymbolMarker       string            `yaml:"symbol_marker"`
	FileMarker         string            `yaml:"file_marker"`
	KNInventoryFiles   []string          `yaml:"kn_inventory_files"`
	OpenAPI            []string          `yaml:"openapi"`
	KNTags             []string          `yaml:"kn_tags"`
	KNRoutePrefixes    []string          `yaml:"kn_route_prefixes"`
	KNSharedPrefixes   []string          `yaml:"kn_shared_route_prefixes"`
	KNHTXWritePrefixes []string          `yaml:"kn_htx_write_prefixes"`
	Web                struct {
		Root        string   `yaml:"root"`
		Alias       string   `yaml:"alias"`
		KNPaths     []string `yaml:"kn_paths"`
		KNMayImport []string `yaml:"kn_may_import"`
	} `yaml:"web"`
}

// Coupling là một ghép nối KN ↔ phần còn lại. Khóa so khớp baseline: (Rule, From, To).
type Coupling struct {
	Rule string `yaml:"rule"`
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

func (c Coupling) key() string { return c.Rule + "|" + c.From + "|" + c.To }

func (c Coupling) String() string { return c.Rule + ": " + c.From + " -> " + c.To }

// LoadConfig đọc tệp cấu hình.
func LoadConfig(file string) (*Config, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if len(c.KNModules) == 0 {
		return nil, fmt.Errorf("%s: kn_modules rỗng", file)
	}
	return &c, nil
}

type checker struct {
	root     string
	cfg      *Config
	modPath  string
	schemaOf map[string]string // schema → thư mục module
	symRe    *regexp.Regexp
	fileRe   *regexp.Regexp
	knMigRe  *regexp.Regexp
	out      map[string]Coupling
}

// Scan quét repo ở root và trả mọi ghép nối hiện có (đã sắp xếp).
func Scan(root string, cfg *Config) ([]Coupling, error) {
	c := &checker{root: root, cfg: cfg, out: map[string]Coupling{}}
	var err error
	if c.modPath, err = modulePath(root); err != nil {
		return nil, err
	}
	for _, p := range []struct {
		dst **regexp.Regexp
		src string
	}{{&c.symRe, cfg.SymbolMarker}, {&c.fileRe, cfg.FileMarker}, {&c.knMigRe, cfg.KNMigrationPattern}} {
		if *p.dst, err = regexp.Compile(p.src); err != nil {
			return nil, err
		}
	}
	if c.schemaOf, err = schemaOwners(root); err != nil {
		return nil, err
	}
	steps := []func() error{c.scanGo, c.scanMigrations, c.scanSymbols, c.scanRoutes, c.scanWeb}
	for _, s := range steps {
		if err := s(); err != nil {
			return nil, err
		}
	}
	res := make([]Coupling, 0, len(c.out))
	for _, v := range c.out {
		res = append(res, v)
	}
	sortCouplings(res)
	return res, nil
}

func sortCouplings(cs []Coupling) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].key() < cs[j].key() })
}

func (c *checker) add(rule, from, to string) {
	k := Coupling{Rule: rule, From: c.canonical(from), To: c.canonical(to)}
	c.out[k.key()] = k
}

// canonical giữ định danh ghép nối khi chuyển namespace cơ học; không thay cách phân loại quyền/import.
func (c *checker) canonical(p string) string {
	best, target := "", ""
	for from, to := range c.cfg.CanonicalPaths {
		if (p == from || strings.HasPrefix(p, from+"/")) && len(from) > len(best) {
			best, target = from, to
		}
	}
	if best != "" {
		return target + strings.TrimPrefix(p, best)
	}
	return p
}

func modulePath(root string) (string, error) {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if m, ok := strings.CutPrefix(strings.TrimSpace(s.Text()), "module "); ok {
			return strings.TrimSpace(m), nil
		}
	}
	return "", fmt.Errorf("go.mod: không thấy dòng module")
}

// schemaOwners đọc owns_schema của mọi module.yaml dưới internal/ → schema → thư mục module.
func schemaOwners(root string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && skipDir(d.Name()) {
			return filepath.SkipDir
		}
		if d.IsDir() || d.Name() != "module.yaml" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var m struct {
			OwnsSchema []string `yaml:"owns_schema"`
		}
		if err := yaml.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		dir := rel(root, filepath.Dir(p))
		for _, s := range m.OwnsSchema {
			out[s] = dir
		}
		return nil
	})
	return out, err
}

func skipDir(name string) bool {
	return name == "testdata" || name == "node_modules" || name == "vendor" || strings.HasPrefix(name, ".")
}

func rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

func under(p string, dirs []string) bool {
	for _, d := range dirs {
		if p == d || strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}

func (c *checker) isKN(dir string) bool { return under(dir, c.cfg.KNModules) }

// moduleDir trả thư mục module chứa package dir (internal/platform/<m>, internal/mkg/<m>, internal/vault/<m>,
// internal/kn/<m>) hoặc "" nếu dir không thuộc module nghiệp vụ.
func moduleDir(dir string) string {
	parts := strings.Split(dir, "/")
	if len(parts) >= 3 && parts[0] == "internal" {
		switch parts[1] {
		case "platform", "mkg", "vault", "kn":
			return strings.Join(parts[:3], "/")
		}
	}
	return ""
}

// allowedFromKN: package dir (thuộc module khác) mà KN được import — port/ (mọi cấp con) hoặc client/ của vault;
// không phải module nghiệp vụ (internal/shared, lib) thì được.
func allowedFromKN(dir string) bool {
	m := moduleDir(dir)
	if m == "" {
		return true
	}
	if strings.HasPrefix(m, "internal/mkg/") {
		return false
	}
	sub := strings.TrimPrefix(strings.TrimPrefix(dir, m), "/")
	return sub == "port" || strings.HasPrefix(sub, "port/") ||
		(strings.HasPrefix(m, "internal/vault/") && (sub == "client" || strings.HasPrefix(sub, "client/")))
}

// scanGo: import và chuỗi SQL trong mọi tệp .go dưới go_roots.
func (c *checker) scanGo() error {
	for _, r := range c.cfg.GoRoots {
		base := filepath.Join(c.root, r)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") {
				return nil
			}
			return c.goFile(p)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *checker) goFile(p string) error {
	file := rel(c.root, p)
	dir := path.Dir(file)
	if under(dir, c.cfg.TestRoots) {
		return nil
	}
	src, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, src, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	fromKN := c.isKN(dir)
	for _, im := range f.Imports {
		ip, _ := strconv.Unquote(im.Path.Value)
		target, ok := strings.CutPrefix(ip, c.modPath+"/")
		if !ok {
			continue
		}
		toKN := c.isKN(target)
		switch {
		case !fromKN && toKN && !under(dir, c.cfg.CompositionRoots):
			c.add(RuleImportIntoKN, file, target)
		case fromKN && !toKN && !allowedFromKN(target):
			c.add(RuleKNImportInternal, file, target)
		case fromKN && !toKN && under(target, c.cfg.HTXModules):
			// PO 14/10/2026 (ADR-0055 §1): KN là hệ độc lập — không gọi trong tiến trình module của hệ khác, kể cả
			// port/; dữ liệu HTX chỉ qua view _pub, data product, lớp tích hợp của nền tảng dữ liệu.
			c.add(RuleKNImportHTX, file, target)
		}
	}
	// Chuỗi (không chú thích) — nơi SQL sống.
	var s scanner.Scanner
	tf := fset.AddFile(file+"#lit", -1, len(src))
	s.Init(tf, src, nil, 0)
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.STRING {
			continue
		}
		v, err := strconv.Unquote(lit)
		if err != nil {
			v = lit
		}
		c.sqlRefs(file, fromKN, v)
	}
	return nil
}

var sqlRefRe = regexp.MustCompile(`\b([a-z][a-z0-9_]*)\.([a-z_][a-z0-9_]*)\b(-?)`)

// sqlRefs ghi tham chiếu schema.đối_tượng của module khác (KN → ngoài, ngoài → KN). Chỉ xét schema có chủ là module
// (owns_schema); bỏ *_pub và free_schemas; bỏ "schema.x-…" (mã nguồn kiểu "pf_forms.bm-04", không phải SQL).
func (c *checker) sqlRefs(file string, fromKN bool, text string) {
	for _, m := range sqlRefRe.FindAllStringSubmatch(text, -1) {
		schema, obj, dash := m[1], m[2], m[3]
		owner, ok := c.schemaOf[schema]
		if !ok || dash != "" || strings.HasSuffix(schema, "_pub") || contains(c.cfg.FreeSchemas, schema) {
			continue
		}
		toKN := c.isKN(owner)
		switch {
		case fromKN && !toKN:
			c.add(RuleKNSQLForeign, file, schema+"."+obj)
		case !fromKN && toKN:
			c.add(RuleSQLIntoKN, file, schema+"."+obj)
		}
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

var sqlLineComment = regexp.MustCompile(`--[^\n]*`)

// scanMigrations: tệp .sql (bỏ chú thích --) — migration KN theo tên tệp.
func (c *checker) scanMigrations() error {
	base := filepath.Join(c.root, c.cfg.MigrationsRoot)
	if c.cfg.MigrationsRoot == "" {
		return nil
	}
	if _, err := os.Stat(base); err != nil {
		return nil
	}
	return filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".up.sql") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		c.sqlRefs(rel(c.root, p), c.knMigRe.MatchString(d.Name()), sqlLineComment.ReplaceAllString(string(b), ""))
		return nil
	})
}

// scanSymbols: tệp (không phải kiểm thử) của module dùng chung có tên hoặc khai báo cấp gói mang dấu KN, và tệp
// kiểm kê tay (kn_inventory_files) còn tồn tại.
func (c *checker) scanSymbols() error {
	for _, f := range c.cfg.KNInventoryFiles {
		if _, err := os.Stat(filepath.Join(c.root, f)); err == nil {
			c.add(RuleKNSymbolInShared, f, "(kiểm kê)")
		}
	}
	for _, r := range c.cfg.SymbolRoots {
		base := filepath.Join(c.root, r)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			file := rel(c.root, p)
			if c.isKN(path.Dir(file)) || under(path.Dir(file), c.cfg.TestRoots) {
				return nil
			}
			if c.fileRe.MatchString(d.Name()) {
				c.add(RuleKNSymbolInShared, file, "(tệp)")
			}
			f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
			if err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}
			for _, name := range topLevelNames(f) {
				if c.symRe.MatchString(lastSeg(name)) {
					c.add(RuleKNSymbolInShared, file, name)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func lastSeg(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// topLevelNames: tên khai báo cấp gói (func, Kiểu.PhươngThức, type, const, var).
func topLevelNames(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				if rt := recvType(d.Recv.List[0].Type); rt != "" {
					name = rt + "." + name
				}
			}
			out = append(out, name)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					out = append(out, s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name != "_" {
							out = append(out, n.Name)
						}
					}
				}
			}
		}
	}
	return out
}

func recvType(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvType(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvType(t.X)
	case *ast.IndexListExpr:
		return recvType(t.X)
	}
	return ""
}

var (
	oaPathRe = regexp.MustCompile(`^  (/[^:]+):\s*$`)
	oaOpRe   = regexp.MustCompile(`^    (get|post|put|patch|delete):`)
	oaTagRe  = regexp.MustCompile(`^      tags:\s*\[([^\]]*)\]`)
)

// scanRoutes: mỗi thao tác OpenAPI — route bề mặt KN mà tag không phải module KN, hoặc tag KN trên route ngoài bề mặt.
func (c *checker) scanRoutes() error {
	for _, f := range c.cfg.OpenAPI {
		fh, err := os.Open(filepath.Join(c.root, f))
		if err != nil {
			return err
		}
		var cur, tag, method string
		inOp := false
		flush := func() {
			if inOp {
				c.route(f, cur, tag)
				c.htxWrite(f, method, cur)
			}
			inOp, tag = false, ""
		}
		s := bufio.NewScanner(fh)
		s.Buffer(make([]byte, 1<<20), 1<<20)
		for s.Scan() {
			line := s.Text()
			if m := oaPathRe.FindStringSubmatch(line); m != nil {
				flush()
				cur = m[1]
				continue
			}
			if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") || !strings.HasPrefix(line, " ") && line != "" {
				flush()
				cur = ""
				continue
			}
			if cur == "" {
				continue
			}
			if m := oaOpRe.FindStringSubmatch(line); m != nil {
				flush()
				inOp, method = true, m[1]
				continue
			}
			if m := oaTagRe.FindStringSubmatch(line); m != nil && inOp && tag == "" {
				tag = strings.TrimSpace(strings.Split(m[1], ",")[0])
			}
		}
		flush()
		_ = fh.Close()
		if err := s.Err(); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	return nil
}

func (c *checker) route(file, p, tag string) {
	owned, shared := hasPrefix(p, c.cfg.KNRoutePrefixes), hasPrefix(p, c.cfg.KNSharedPrefixes)
	if tag == "" {
		if owned || !shared {
			return // không tag: chủ suy từ tiền tố của KN; ngoài bề mặt KN thì không xét
		}
		c.add(RuleRouteOwner, path.Base(file)+" "+p, "(không tag)")
		return
	}
	if (owned || shared) != contains(c.cfg.KNTags, tag) {
		c.add(RuleRouteOwner, path.Base(file)+" "+p, tag)
	}
}

// htxWrite: thao tác ghi (POST/PUT/PATCH/DELETE) trên route KN ghi vào dữ liệu do HTX sở hữu. PO 14/10/2026
// (Q-KNS-04, ADR-0055 §4): KN không có đường ghi vào schema hay API của HTX; dữ liệu KN tạo "cho HTX" là bản ghi riêng
// của KN.
func (c *checker) htxWrite(file, method, p string) {
	if method == "get" || !hasPrefix(p, c.cfg.KNHTXWritePrefixes) {
		return
	}
	c.add(RuleKNWriteHTX, path.Base(file)+" "+strings.ToUpper(method)+" "+p, "dữ liệu HTX")
}

func hasPrefix(p string, prefixes []string) bool {
	for _, pre := range prefixes {
		if strings.HasPrefix(p, pre) || strings.TrimSuffix(pre, "/") == p {
			return true
		}
	}
	return false
}

var tsImportRe = regexp.MustCompile(`(?:\bfrom\s*|\bimport\s*\(\s*|\bimport\s+|\bvi\.mock\(\s*)["']([^"']+)["']`)

// scanWeb: import giữa vùng KN và phần còn lại của web-ops.
func (c *checker) scanWeb() error {
	w := c.cfg.Web
	if w.Root == "" {
		return nil
	}
	base := filepath.Join(c.root, w.Root)
	if _, err := os.Stat(base); err != nil {
		return nil
	}
	inKN := func(p string) bool {
		for _, k := range w.KNPaths {
			if strings.HasPrefix(p, k) {
				return true
			}
		}
		return false
	}
	return filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := path.Ext(d.Name())
		if ext != ".ts" && ext != ".tsx" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		srcRel := rel(base, p) // tương đối src
		fromKN := inKN(srcRel)
		for _, m := range tsImportRe.FindAllStringSubmatch(string(b), -1) {
			spec := m[1]
			var target string
			switch {
			case strings.HasPrefix(spec, w.Alias):
				target = strings.TrimPrefix(spec, w.Alias)
			case strings.HasPrefix(spec, "."):
				target = path.Clean(path.Join(path.Dir(srcRel), spec))
				if strings.HasPrefix(target, "..") {
					continue
				}
			default:
				continue // gói npm
			}
			toKN := inKN(target) || inKN(target+"/")
			file := w.Root + "/" + srcRel
			switch {
			case !fromKN && toKN:
				c.add(RuleWebImportIntoKN, file, target)
			case fromKN && !toKN && !mayImport(target, w.KNMayImport):
				c.add(RuleWebKNImportApp, file, target)
			}
		}
		return nil
	})
}

func mayImport(target string, allowed []string) bool {
	for _, a := range allowed {
		if strings.HasPrefix(target, a) || target == strings.TrimSuffix(a, "/") {
			return true
		}
	}
	return false
}
