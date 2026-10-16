package main

import (
	"go/ast"
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
	Doc: "ranh giới module: chỉ import port/ của module khác (R-02); platform và mkg không import nhau (R-03); vault chỉ qua client (R-04); " +
		"vault không import platform/mkg (mục 4); port/ chỉ interface + DTO (mục 8)",
	Run: runBoundary,
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
	if l.zone == "kn" {
		l.zone = "platform"
	} // same DB zone, independent application namespace (KNSPLIT-1)
	if len(parts) >= 3 {
		l.module = parts[2]
	}
	if len(parts) >= 4 {
		l.rest = strings.Join(parts[3:], "/")
	}
	return l
}

func isBusinessZone(z string) bool { return z == "platform" || z == "mkg" || z == "vault" }

// generatedTestPackage recognizes the toolchain driver, not application source.
// Its mandatory self-import is not a cross-module dependency (root module tests).
func generatedTestPackage(pass *analysis.Pass) string {
	if pass.Pkg.Name() != "main" || !strings.HasSuffix(pass.Pkg.Path(), ".test") || len(pass.Files) != 1 || !ast.IsGenerated(pass.Files[0]) {
		return ""
	}
	return strings.TrimSuffix(pass.Pkg.Path(), ".test")
}

func runBoundary(pass *analysis.Pass) (any, error) {
	testedPackage := generatedTestPackage(pass)
	from := locate(strings.TrimSuffix(pass.Pkg.Path(), "_test"))
	for _, f := range pass.Files {
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if testedPackage != "" && (imp == testedPackage || imp == testedPackage+"_test") {
				continue
			}
			if msg := edgeViolation(from.path, imp); msg != "" {
				pass.Reportf(spec.Pos(), "%s", msg)
				continue
			}
			if msg := contractDriverViolation(from, imp); msg != "" {
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
	// G5 (ADR-0022 mục 1, RB-3): Public API cho bên thứ ba không bao giờ chạm kho định danh — kể cả client.
	if to.zone == "vault" && !from.internal {
		for _, r := range noVaultRoots {
			if under(from.path, r) {
				return "RB-3: " + r + " (Public API cho bên thứ ba) không được import kho định danh (" + imp + ") — bên thứ ba không bao giờ thấy dữ liệu cá nhân"
			}
		}
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
	// Mục 4, RB-3: kho định danh hộ là vùng chủ quyền riêng (cụm vault, đơn vị identity-vault) — không phụ
	// thuộc mã nghiệp vụ của vùng nền tảng hay nội bộ MKG.
	if from.zone == "vault" && (to.zone == "platform" || to.zone == "mkg") {
		return "RB-3: vault/ là vùng chủ quyền riêng (mục 4) — không được import " + to.zone + "/ (" + imp + ")"
	}
	// Mục 8: port/ (và client/ của vault) chỉ có interface + DTO — không kéo lớp trong của chính module.
	if isContract(from) && !contractExempt(from.path) && from.zone == to.zone && from.module == to.module && innerLayer(to.rest) {
		return "mục 8: " + from.module + "/" + from.rest + " chỉ chứa interface + DTO — không import " + imp + " (đặt bộ điều hợp ở adapter/ hoặc app/)"
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
	// G4: công cụ diễn tập dựng lại bảng chiếu cụm mkg (R-22) — chỉ nối module mkg.
	{"cmd/rebuilddrill", "platform"},
	// G5: đơn vị triển khai Public API (ADR-0022) — gốc lắp ráp vùng nền tảng, chỉ nối platform/*.
	// W5.2 (ADR-0033): cổng tích hợp cmd/integration-gateway; cmd/api-public còn là bí danh một kỳ.
	{"cmd/integration-gateway", "mkg"},
	{"cmd/api-public", "mkg"},
	// worker-platform (mục 4: consumer + job của vùng nền tảng), diễn tập phục hồi cụm pf và bộ nạp mẫu
	// báo cáo Excel — chỉ nối platform/*.
	{"cmd/worker", "mkg"},
	{"cmd/drill", "mkg"},
	{"cmd/formimport", "mkg"},
	// Kho định danh hộ (mục 4, RB-3): đơn vị riêng ở vùng định danh — không nối platform/* lẫn mkg/*.
	{"cmd/identity-vault", "platform"},
	{"cmd/identity-vault", "mkg"},
}

// pgxPrefix là driver DB: port/ và client/ của vault không được dùng (hợp đồng không kéo hạ tầng, mục 8).
var pgxPrefix = "github.com/jackc/pgx"

// innerLayers là lớp trong của một module mà port/ không được import (domain/ thuần, được phép).
var innerLayers = []string{"store", "app", "handler", "adapter"}

// contractExemptPaths: ngoại lệ có tên của luật port/ — chỉ được co lại.
var contractExemptPaths = []string{
	// đang chuyển ra adapter (review M9)
	"internal/platform/party/port/grantlookup",
}

func innerLayer(rest string) bool {
	first, _, _ := strings.Cut(rest, "/")
	for _, l := range innerLayers {
		if first == l {
			return true
		}
	}
	return false
}

// isContract: package hợp đồng của module — port/** của mọi module, client/** của vault.
func isContract(l loc) bool {
	if !l.internal || l.module == "" {
		return false
	}
	if isPort(l.rest) {
		return true
	}
	return l.zone == "vault" && (l.rest == "client" || strings.HasPrefix(l.rest, "client/"))
}

func contractExempt(pkg string) bool {
	for _, e := range contractExemptPaths {
		if under(pkg, e) {
			return true
		}
	}
	return false
}

// contractDriverViolation: port/ (client/ của vault) không import driver DB pgx (mục 8).
func contractDriverViolation(from loc, imp string) string {
	if imp != pgxPrefix && !strings.HasPrefix(imp, pgxPrefix+"/") {
		return ""
	}
	if !isContract(from) || contractExempt(from.path) {
		return ""
	}
	return "mục 8: " + from.module + "/" + from.rest + " chỉ chứa interface + DTO — không import driver DB " + imp
}

// noVaultRoots là gốc lắp ráp không được import BẤT KỲ package nào của vault, kể cả client (RB-3).
var noVaultRoots = []string{"cmd/integration-gateway", "cmd/api-public"}

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
