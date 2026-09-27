package main

import (
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// modulePath là đường dẫn Go module của repo; đổi được bằng cờ để kiểm thử.
var modulePath = "github.com/MEKONG-GREEN/Agrigreen_demo_v2"

// vaultClient là package DUY NHẤT của vault được phép import từ ngoài (R-04).
const vaultClient = "internal/vault/identity/client"

// Boundary cưỡng chế R-02, R-03, R-04 và "shared không chứa logic nghiệp vụ".
var Boundary = &analysis.Analyzer{
	Name: "agboundary",
	Doc:  "ranh giới module: chỉ import port/ của module khác (R-02); platform và mkg không import nhau (R-03); vault chỉ qua client (R-04)",
	Run:  runBoundary,
}

func init() {
	Boundary.Flags.StringVar(&modulePath, "module", modulePath, "đường dẫn Go module")
}

// loc mô tả vị trí một package trong internal/: vùng, module, phần còn lại.
type loc struct {
	internal bool
	zone     string // platform | mkg | vault | shared
	module   string
	rest     string // đường dẫn sau module, vd "port/x"
}

func locate(pkg string) loc {
	rel, ok := strings.CutPrefix(pkg, modulePath+"/")
	if !ok {
		return loc{}
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || parts[0] != "internal" {
		return loc{}
	}
	l := loc{internal: true, zone: parts[1]}
	if len(parts) >= 3 {
		l.module = parts[2]
	}
	if len(parts) >= 4 {
		l.rest = strings.Join(parts[3:], "/")
	}
	return l
}

func isBusinessZone(z string) bool { return z == "platform" || z == "mkg" || z == "vault" }

func runBoundary(pass *analysis.Pass) (any, error) {
	from := locate(strings.TrimSuffix(pass.Pkg.Path(), "_test"))
	for _, f := range pass.Files {
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			to := locate(imp)
			if !to.internal {
				continue
			}
			if msg := violation(from, to, imp); msg != "" {
				pass.Reportf(spec.Pos(), "%s", msg)
			}
		}
	}
	return nil, nil
}

func violation(from, to loc, imp string) string {
	// shared không phụ thuộc vùng nghiệp vụ nào.
	if from.internal && from.zone == "shared" && isBusinessZone(to.zone) {
		return "shared/ không được import " + imp + ": shared không chứa logic nghiệp vụ"
	}
	if to.zone == "shared" {
		return ""
	}
	// R-04: vault chỉ qua client.
	if to.zone == "vault" && !(from.internal && from.zone == "vault") {
		if !strings.HasPrefix(imp, modulePath+"/"+vaultClient) {
			return "R-04: chỉ được gọi kho định danh qua " + vaultClient + ", không import " + imp
		}
		return ""
	}
	if !from.internal {
		// cmd/, tools/: gốc lắp ráp — được import gốc module (module.go) và port/.
		if to.module != "" && to.rest != "" && !isPort(to.rest) {
			return "R-02: gốc lắp ráp chỉ được import gốc module hoặc port/, không import " + imp
		}
		return ""
	}
	// R-03: platform ↔ mkg.
	if (from.zone == "platform" && to.zone == "mkg") || (from.zone == "mkg" && to.zone == "platform") {
		return "R-03: " + from.zone + "/ không được import " + to.zone + "/ (" + imp + ") — chỉ qua API công khai hoặc van công bố"
	}
	// R-02: khác module → chỉ port/.
	if from.zone == to.zone && from.module != to.module && !isPort(to.rest) {
		return "R-02: chỉ được import port/ của module khác, không import " + imp
	}
	return ""
}

func isPort(rest string) bool { return rest == "port" || strings.HasPrefix(rest, "port/") }
