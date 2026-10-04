// Lệnh modgen tính lại các trường SINH của module.yaml (ADR-0027, gói W1.1) từ mã
// nguồn và migration, rồi ghi lại (mặc định) hoặc chỉ kiểm (-check, dùng trong CI):
//
//   - depends: module khác mà mã KHÔNG phải kiểm thử import (port/, vault client/).
//     Phụ thuộc mới nhận ràng buộc mặc định ">=MAJOR.MINOR <MAJOR+1" theo phiên bản
//     hiện có của module được phụ thuộc; ràng buộc đã sửa tay được giữ.
//   - provides.ports: interface xuất trong port/ (vault: client/), gói con có tiền tố.
//   - provides.events / internal_events / publications / data_products: chỉ thị
//     //ag:event, //ag:internal-event, //ag:publication, //ag:dataproduct.
//   - permissions: hằng Role* kiểu chuỗi trong port/ (vault: client/).
//   - owns_schema: schema do migration tạo (CREATE SCHEMA) có tên <cụm>_<module>,
//     hoặc được duy nhất module này nhắc trong chuỗi SQL của mã không phải kiểm thử.
//
// Trường TAY (name, aka, kind, zones, version, shares, enable) giữ nguyên. Tệp chưa
// có được tạo khung; kind/zones/enable phải điền tay rồi chạy lại.
//
// Sau đó kiểm app/*/app.yaml (W1.3, viết tay, không sinh): đủ bảy ứng dụng, tham chiếu
// đúng (module, phiên bản, vai/client realm, cmd/*, apps/*) và mỗi cmd/<đơn vị> chỉ ghép
// module thuộc hợp `modules` của các ứng dụng liệt kê nó trong deployment_units.
//
// Chạy: go run ./tools/modgen [-check] [-root .]
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

const modulePath = "github.com/MEKONG-GREEN/Agrigreen_demo_v2"

func main() {
	check := flag.Bool("check", false, "chỉ kiểm, không ghi; lỗi nếu module.yaml lỗi thời")
	root := flag.String("root", ".", "gốc repo")
	flag.Parse()
	stale, err := run(*root, *check)
	for _, s := range stale {
		fmt.Fprintln(os.Stderr, "lỗi thời:", s)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *check && len(stale) > 0 {
		fmt.Fprintln(os.Stderr, "chạy: go run ./tools/modgen")
		os.Exit(1)
	}
}

// run tính lại mọi manifest; trả danh sách tệp khác bản tính lại.
func run(root string, check bool) ([]string, error) {
	dirs, err := moduleDirs(root)
	if err != nil {
		return nil, err
	}
	created, err := createdSchemas(filepath.Join(root, "migrations"))
	if err != nil {
		return nil, err
	}
	scans := map[string]*scan{}
	for _, d := range dirs {
		s, err := scanModule(root, d)
		if err != nil {
			return nil, err
		}
		scans[s.name] = s
	}
	owners := schemaOwners(scans, created)

	existing := map[string]*modreg.Manifest{}
	var errs []error
	for _, d := range dirs {
		p := filepath.Join(root, filepath.FromSlash(d), modreg.FileName)
		data, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		m, err := modreg.Parse(data, d+"/"+modreg.FileName)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		existing[modreg.NameForDir(d)] = m
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	var stale []string
	var all []*modreg.Manifest
	for _, d := range dirs {
		name := modreg.NameForDir(d)
		s := scans[name]
		m := existing[name]
		if m == nil {
			m = &modreg.Manifest{Name: name, Version: "1.0.0", Path: d + "/" + modreg.FileName}
		}
		next := *m
		next.Depends = map[string]string{}
		for _, dep := range s.deps {
			c, ok := m.Depends[dep]
			if !ok {
				c = "*"
				if dm := existing[dep]; dm != nil {
					if v, err := modreg.ParseVersion(dm.Version); err == nil {
						c = modreg.DefaultConstraint(v)
					}
				}
			}
			next.Depends[dep] = c
		}
		next.Provides = modreg.Provides{
			Ports:          s.ports,
			Events:         s.directives["event"],
			InternalEvents: s.directives["internal-event"],
			Publications:   s.directives["publication"],
			DataProducts:   s.directives["dataproduct"],
		}
		next.Permissions = s.roles
		next.OwnsSchema = owners[name]
		all = append(all, &next)

		want := modreg.Format(&next)
		p := filepath.Join(root, filepath.FromSlash(d), modreg.FileName)
		got, err := os.ReadFile(p)
		if err == nil && bytes.Equal(normalizeEOL(got), want) {
			continue
		}
		stale = append(stale, d+"/"+modreg.FileName)
		if !check {
			if err := os.WriteFile(p, want, 0o644); err != nil { //nolint:gosec // tệp nguồn trong repo
				return stale, err
			}
		}
	}
	if !check {
		stale = nil
	}
	reg, err := modreg.New(all)
	if err != nil {
		return stale, fmt.Errorf("module.yaml không hợp lệ:\n%w", err)
	}
	if err := checkApps(root, reg); err != nil {
		return stale, err
	}
	return stale, nil
}

// checkApps là cổng W1.3: app/*/app.yaml hợp lệ, đủ bảy ứng dụng, và mỗi cmd/<đơn vị>
// chỉ ghép module thuộc hợp các app.yaml liệt kê nó (modreg.Apps.CheckAssembly).
func checkApps(root string, reg *modreg.Registry) error {
	fsys := os.DirFS(root)
	apps, env, err := modreg.LoadApps(fsys, reg)
	if err != nil {
		return fmt.Errorf("app.yaml không hợp lệ:\n%w", err)
	}
	var errs []error
	for _, n := range modreg.AppNames {
		if _, ok := apps.Get(n); !ok {
			errs = append(errs, fmt.Errorf("thiếu app/%s/%s", n, modreg.AppFileName))
		}
	}
	units := make([]string, 0, len(env.Units))
	for u := range env.Units {
		units = append(units, u)
	}
	wired, err := wiredModules(root, units)
	if err != nil {
		return err
	}
	errs = append(errs, apps.CheckAssembly(reg, wired)...)
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("app.yaml không khớp cmd/*:\n%w", err)
	}
	return nil
}

// wiredModules trả đơn vị → module được ghép TRONG TIẾN TRÌNH: mã không phải kiểm thử của
// cmd/<đơn vị> (kể cả gói cmd/internal/* nó import, bắc cầu) import gói của module mà không
// phải port/ hay client/ (port là hợp đồng; client của vault là gọi qua mạng sang cụm khác).
func wiredModules(root string, units []string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, u := range units {
		mods := map[string]bool{}
		seen := map[string]bool{}
		queue := []string{"cmd/" + u}
		for len(queue) > 0 {
			dir := queue[0]
			queue = queue[1:]
			if seen[dir] {
				continue
			}
			seen[dir] = true
			imps, err := dirImports(root, dir, dir == "cmd/"+u)
			if err != nil {
				return nil, err
			}
			for _, ip := range imps {
				rel, ok := strings.CutPrefix(ip, modulePath+"/")
				if !ok {
					continue
				}
				if strings.HasPrefix(rel, "cmd/internal/") {
					queue = append(queue, rel)
					continue
				}
				if m := moduleOf(ip); m != "" && inProcess(rel) {
					mods[m] = true
				}
			}
		}
		out[u] = keys(mods)
	}
	return out, nil
}

// inProcess: đường dẫn internal/<vùng>/<module>[/gói] không thuộc port/ hay client/.
func inProcess(rel string) bool {
	parts := strings.Split(rel, "/")
	return len(parts) < 4 || (parts[3] != "port" && parts[3] != "client")
}

// dirImports trả import của các tệp .go không phải kiểm thử trong dir (đệ quy nếu recursive,
// bỏ testdata).
func dirImports(root, dir string, recursive bool) ([]string, error) {
	var out []string
	fset := token.NewFileSet()
	base := filepath.Join(root, filepath.FromSlash(dir))
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != base && (!recursive || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			ip, _ := strconv.Unquote(imp.Path.Value)
			out = append(out, ip)
		}
		return nil
	})
	return out, err
}

func normalizeEOL(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

// moduleDirs trả internal/<vùng>/<module> (gạch chéo xuôi) có ít nhất một tệp .go.
func moduleDirs(root string) ([]string, error) {
	var out []string
	for _, zone := range []string{"platform", "mkg", "vault"} {
		entries, err := os.ReadDir(filepath.Join(root, "internal", zone))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			gos, _ := filepath.Glob(filepath.Join(root, "internal", zone, e.Name(), "*.go"))
			if len(gos) > 0 {
				out = append(out, "internal/"+zone+"/"+e.Name())
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

var reCreateSchema = regexp.MustCompile(`(?i)CREATE\s+SCHEMA\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)

// createdSchemas trả schema → cụm từ migrations/<cụm>/*.up.sql.
func createdSchemas(dir string) (map[string]string, error) {
	out := map[string]string{}
	clusters, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, c := range clusters {
		if !c.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(dir, c.Name(), "*.up.sql"))
		for _, f := range files {
			data, err := os.ReadFile(f) //nolint:gosec // tệp migration trong repo
			if err != nil {
				return nil, err
			}
			for _, mm := range reCreateSchema.FindAllStringSubmatch(string(data), -1) {
				out[strings.ToLower(mm[1])] = c.Name()
			}
		}
	}
	return out, nil
}

type scan struct {
	name       string
	deps       []string
	ports      []string
	roles      []string
	directives map[string][]string
	literals   map[string]bool // các chuỗi trong mã không phải kiểm thử (để tìm schema)
}

var reDirective = regexp.MustCompile(`^//ag:(event|internal-event|publication|dataproduct)\s+(\S+)(?:\s+(v[0-9]+))?`)

func scanModule(root, dir string) (*scan, error) {
	s := &scan{name: modreg.NameForDir(dir), directives: map[string][]string{}, literals: map[string]bool{}}
	portDir := dir + "/port"
	if strings.HasPrefix(dir, "internal/vault/") {
		portDir = dir + "/client"
	}
	selfPrefix := modulePath + "/" + dir + "/"
	deps := map[string]bool{}
	ports := map[string]bool{}
	roles := map[string]bool{}
	dirs := map[string]map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(dir)), func(p string, d fs.DirEntry, err error) error {
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
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			ip, _ := strconv.Unquote(imp.Path.Value)
			if dep := moduleOf(ip); dep != "" && !strings.HasPrefix(ip+"/", selfPrefix) {
				deps[dep] = true
			}
		}
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				mm := reDirective.FindStringSubmatch(c.Text)
				if mm == nil {
					continue
				}
				v := mm[2]
				if mm[3] != "" {
					v += "/" + mm[3]
				}
				if dirs[mm[1]] == nil {
					dirs[mm[1]] = map[string]bool{}
				}
				dirs[mm[1]][v] = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if v, err := strconv.Unquote(lit.Value); err == nil {
					s.literals[v] = true
				}
			}
			return true
		})
		fileDir := filepath.ToSlash(filepath.Dir(rel))
		if fileDir == portDir || strings.HasPrefix(fileDir, portDir+"/") {
			sub := strings.TrimPrefix(strings.TrimPrefix(fileDir, portDir), "/")
			collectPort(f, sub, ports, roles)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.deps = keys(deps)
	s.ports = keys(ports)
	s.roles = keys(roles)
	for k, v := range dirs {
		s.directives[k] = keys(v)
	}
	return s, nil
}

// moduleOf trả tên module của một đường dẫn import thuộc internal/{platform,mkg,vault}.
func moduleOf(ip string) string {
	rel, ok := strings.CutPrefix(ip, modulePath+"/")
	if !ok {
		return ""
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 3 {
		return ""
	}
	return modreg.NameForDir(strings.Join(parts[:3], "/"))
}

func collectPort(f *ast.File, sub string, ports, roles map[string]bool) {
	qual := func(n string) string {
		if sub == "" {
			return n
		}
		return strings.ReplaceAll(sub, "/", ".") + "." + n
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			switch sp := spec.(type) {
			case *ast.TypeSpec:
				if _, isIface := sp.Type.(*ast.InterfaceType); isIface && sp.Name.IsExported() && sp.TypeParams == nil {
					ports[qual(sp.Name.Name)] = true
				}
			case *ast.ValueSpec:
				if gd.Tok != token.CONST || sub != "" {
					continue
				}
				for i, n := range sp.Names {
					if !strings.HasPrefix(n.Name, "Role") || i >= len(sp.Values) {
						continue
					}
					if lit, ok := sp.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						v, _ := strconv.Unquote(lit.Value)
						roles[v] = true
					}
				}
			}
		}
	}
}

// schemaOwners gán schema cho module: trùng tên <cụm>_<module> trước, nếu không thì
// module DUY NHẤT nhắc "<schema>." trong chuỗi của mã không phải kiểm thử.
func schemaOwners(scans map[string]*scan, created map[string]string) map[string][]string {
	out := map[string][]string{}
	byName := map[string]string{}
	for name := range scans {
		zone, mod, _ := strings.Cut(name, "/")
		byName[zone+"_"+mod] = name
	}
	byName["vault"] = "vault/identity"
	for schema := range created {
		if owner, ok := byName[schema]; ok && scans[owner] != nil {
			out[owner] = append(out[owner], schema)
			continue
		}
		var refs []string
		for name, s := range scans {
			for lit := range s.literals {
				if mentionsSchema(lit, schema) {
					refs = append(refs, name)
					break
				}
			}
		}
		if len(refs) == 1 {
			out[refs[0]] = append(out[refs[0]], schema)
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

func mentionsSchema(lit, schema string) bool {
	for i := 0; ; {
		j := strings.Index(lit[i:], schema+".")
		if j < 0 {
			return false
		}
		j += i
		if j == 0 || !isIdent(lit[j-1]) {
			return true
		}
		i = j + 1
	}
}

func isIdent(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
