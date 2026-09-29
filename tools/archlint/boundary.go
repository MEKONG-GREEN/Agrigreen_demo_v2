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

// vaultRoot là gốc lắp ráp của vùng vault: dịch vụ identity-vault chạy riêng ở
// vùng định danh (mục 17). Chỉ nó được import gốc module vault (module.go) và
// port/ — vẫn theo quy tắc gốc lắp ráp R-02.
const vaultRoot = "cmd/identity-vault"

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
	path     string // đường dẫn package đầy đủ
	internal bool
	zone     string // platform | mkg | vault | shared
	module   string
	rest     string // đường dẫn sau module, vd "port/x"
}

func locate(pkg string) loc {
	rel, ok := strings.CutPrefix(pkg, modulePath+"/")
	if !ok {
		return loc{path: pkg}
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || parts[0] != "internal" {
		return loc{path: pkg}
	}
	l := loc{path: pkg, internal: true, zone: parts[1]}
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
			if msg := edgeViolation(from.path, imp); msg != "" {
				pass.Reportf(spec.Pos(), "%s", msg)
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
	// R-04: vault chỉ qua client — trừ gốc lắp ráp của vault (theo quy tắc gốc lắp ráp bên dưới).
	isVaultRoot := from.path == modulePath+"/"+vaultRoot || strings.HasPrefix(from.path, modulePath+"/"+vaultRoot+"/")
	if to.zone == "vault" && (!from.internal || from.zone != "vault") {
		if strings.HasPrefix(imp, modulePath+"/"+vaultClient) {
			return ""
		}
		if !isVaultRoot {
			return "R-04: chỉ được gọi kho định danh qua " + vaultClient + ", không import " + imp
		}
	}
	if !from.internal {
		// R-03 ở gốc lắp ráp: mỗi vùng một gốc — api-platform chỉ nối platform/*,
		// api-internal chỉ nối mkg/* (mục 5: hai đơn vị triển khai, hai cụm DB).
		for _, zr := range zoneRoots {
			if under(from.path, zr.root) && to.zone == zr.forbidden {
				return "R-03: gốc lắp ráp " + zr.root + " không được import " + to.zone + "/ (" + imp + ") — hai vùng chỉ nói chuyện qua API công khai hoặc van công bố"
			}
		}
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

// zoneRoots là gốc lắp ráp theo vùng và vùng nó không được nối.
var zoneRoots = []struct{ root, forbidden string }{
	{"cmd/api-platform", "mkg"},
	{"cmd/api-internal", "platform"},
	// G3: đơn vị triển khai của vùng mkg (mục 5) — ingest (measurement), agent biên,
	// worker-internal (consumer vhost mkg.internal, ADR-0018).
	{"cmd/ingest", "platform"},
	{"cmd/edge-agent", "platform"},
	{"cmd/worker-internal", "platform"},
}

// edgeAgentRoot là nhị phân tĩnh ở vùng biên OT (mục 14, 17): không DB, không
// mã nghiệp vụ, chỉ đi ra tới ingest qua mTLS.
const edgeAgentRoot = "cmd/edge-agent"

// edgeWire là hợp đồng dây edge-agent ↔ ingest: chỉ thư viện chuẩn.
const edgeWire = "internal/mkg/measurement/port/edgewire"

// edgeAgentAllowed là các package của repo mà edge-agent được import (ngoài thư viện chuẩn).
var edgeAgentAllowed = []string{"internal/shared/clock", "internal/shared/ids", "internal/shared/mass", edgeWire}

// isStdlib: đường dẫn import của thư viện chuẩn không có dấu chấm ở phần đầu.
func isStdlib(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")
	return !strings.Contains(first, ".")
}

// edgeViolation: cmd/edge-agent chỉ import thư viện chuẩn + edgeAgentAllowed (không pgx,
// không DB, không module nghiệp vụ); edgewire chỉ import thư viện chuẩn (ADR-0019).
func edgeViolation(from, imp string) string {
	if isStdlib(imp) {
		return ""
	}
	if under(from, edgeWire) {
		return "ADR-0019: " + edgeWire + " chỉ được dùng thư viện chuẩn, không import " + imp
	}
	if !under(from, edgeAgentRoot) {
		return ""
	}
	for _, a := range edgeAgentAllowed {
		if imp == modulePath+"/"+a {
			return ""
		}
	}
	return "ADR-0019: " + edgeAgentRoot + " chỉ được import thư viện chuẩn, internal/shared/{clock,ids,mass} và " + edgeWire + " — không import " + imp
}

func under(pkg, root string) bool {
	p := modulePath + "/" + root
	return pkg == p || strings.HasPrefix(pkg, p+"/")
}

func isPort(rest string) bool { return rest == "port" || strings.HasPrefix(rest, "port/") }
