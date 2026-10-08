package main

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

// NoAppName cấm mã module kind: capability và kernel rẽ nhánh theo danh tính ứng dụng (W9.1, ADR-0027
// mục 3, kiến trúc v4.1 §7): so sánh ==/!=, nhãn case của switch, strings.EqualFold với hằng chuỗi
// (literal hoặc hằng có tên) trùng mã ứng dụng trong app/*/app.yaml (KN, HTX, CGH, SIM, MKG, NH, PF).
// Năng lực dùng chung phải được CẤU HÌNH (tham số, cờ module, cấp quyền) chứ không hỏi "đây là app nào".
//
// So khớp không phân biệt hoa thường, trừ hằng có kiểu đặt tên mà tên kiểu không chứa "app" (vd
// party/port.OrgKind "mkg" là loại tổ chức của miền, không phải mã ứng dụng). Tệp _test.go được miễn
// (dữ liệu mẫu kiểm thử có thể nhắc tên ứng dụng).
var NoAppName = &analysis.Analyzer{
	Name: "agnoappname",
	Doc:  "cấm so sánh với mã ứng dụng (app/*/app.yaml) trong module kind: capability/kernel — năng lực phải được cấu hình, không rẽ nhánh theo ứng dụng (W9.1)",
	Run:  runNoAppName,
}

type naCache struct {
	once sync.Once
	apps map[string]bool // mã ứng dụng, chữ thường
	kind map[string]modreg.Kind
	mu   sync.Mutex
}

var (
	naCachesMu sync.Mutex
	naCaches   = map[string]*naCache{}
)

func naCacheFor(root string) *naCache {
	naCachesMu.Lock()
	defer naCachesMu.Unlock()
	c := naCaches[root]
	if c == nil {
		c = &naCache{kind: map[string]modreg.Kind{}}
		naCaches[root] = c
	}
	return c
}

// rootOf suy gốc repo từ thư mục tệp và đường dẫn package (gốc + đường dẫn tương đối của package).
func rootOf(fileDir, pkg string) string {
	rel, ok := strings.CutPrefix(pkg, modulePath+"/")
	if !ok {
		return ""
	}
	dir := filepath.ToSlash(fileDir)
	r, ok := strings.CutSuffix(dir, "/"+rel)
	if !ok {
		return ""
	}
	return filepath.FromSlash(r)
}

func (c *naCache) appIDs(root string) (map[string]bool, error) {
	var err error
	c.once.Do(func() {
		var apps []*modreg.App
		apps, err = modreg.ReadApps(os.DirFS(root))
		c.apps = map[string]bool{}
		for _, a := range apps {
			if a.App != "" {
				c.apps[strings.ToLower(a.App)] = true
			}
		}
	})
	return c.apps, err
}

func (c *naCache) moduleKind(root, zone, module string) (modreg.Kind, error) {
	key := zone + "/" + module
	c.mu.Lock()
	defer c.mu.Unlock()
	if k, ok := c.kind[key]; ok {
		return k, nil
	}
	p := path.Join("internal", zone, module, modreg.FileName)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p))) //nolint:gosec // tệp trong repo
	if err != nil {
		c.kind[key] = ""
		return "", nil //nolint:nilerr // không có module.yaml: không phải module (vd internal/shared)
	}
	m, err := modreg.Parse(data, p)
	if err != nil {
		return "", err
	}
	c.kind[key] = m.Kind
	return m.Kind, nil
}

func runNoAppName(pass *analysis.Pass) (any, error) {
	if len(pass.Files) == 0 {
		return nil, nil
	}
	pkg := strings.TrimSuffix(pass.Pkg.Path(), "_test")
	from := locate(pkg)
	if !from.internal || !isBusinessZone(from.zone) || from.module == "" {
		return nil, nil
	}
	root := rootOf(filepath.Dir(pass.Fset.File(pass.Files[0].Pos()).Name()), pkg)
	if root == "" {
		return nil, nil
	}
	c := naCacheFor(root)
	kind, err := c.moduleKind(root, from.zone, from.module)
	if err != nil {
		return nil, err
	}
	if kind != modreg.KindCapability && kind != modreg.KindKernel {
		return nil, nil
	}
	apps, err := c.appIDs(root)
	if err != nil {
		return nil, err
	}
	if len(apps) == 0 {
		return nil, nil
	}
	appOf := func(e ast.Expr) string {
		tv, ok := pass.TypesInfo.Types[e]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
			return ""
		}
		v := constant.StringVal(tv.Value)
		if !apps[strings.ToLower(v)] {
			return ""
		}
		if n, ok := types.Unalias(tv.Type).(*types.Named); ok &&
			!strings.Contains(strings.ToLower(n.Obj().Name()), "app") {
			return "" // enum miền có kiểu riêng (vd OrgKind "mkg"), không phải mã ứng dụng
		}
		return v
	}
	report := func(pos token.Pos, v string) {
		pass.Reportf(pos, "W9.1: module %s/%s (kind: %s) không được rẽ nhánh theo mã ứng dụng %q — đưa khác biệt vào cấu hình (tham số, cờ module, cấp quyền)", from.zone, from.module, kind, v)
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.File(f.Pos()).Name(), "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BinaryExpr:
				if n.Op != token.EQL && n.Op != token.NEQ {
					return true
				}
				for _, side := range []ast.Expr{n.X, n.Y} {
					if v := appOf(side); v != "" {
						report(side.Pos(), v)
						return true
					}
				}
			case *ast.SwitchStmt:
				if n.Tag == nil {
					return true
				}
				for _, s := range n.Body.List {
					for _, e := range s.(*ast.CaseClause).List {
						if v := appOf(e); v != "" {
							report(e.Pos(), v)
						}
					}
				}
			case *ast.CallExpr:
				sel, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "EqualFold" {
					return true
				}
				if fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func); !ok || fn.Pkg() == nil ||
					(fn.Pkg().Path() != "strings" && fn.Pkg().Path() != "bytes") {
					return true
				}
				for _, a := range n.Args {
					if v := appOf(a); v != "" {
						report(a.Pos(), v)
					}
				}
			}
			return true
		})
	}
	return nil, nil
}
