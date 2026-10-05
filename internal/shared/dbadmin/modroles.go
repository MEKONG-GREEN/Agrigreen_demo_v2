package dbadmin

// Vai theo ADR-0028 (W2.3): vai nhóm r_<module> NOLOGIN mang quyền chạy của module, vai đăng nhập
// app_<đơn vị> LOGIN NOINHERIT là thành viên các r_* của module ghép trong đơn vị, `SET LOCAL ROLE` trong
// giao dịch (internal/shared/db.ForModule / WithContextTx).
//
// Kỳ chuyển (W2.3 → W2.6): vai đăng nhập cũ <module>_app vẫn chạy, là thành viên r_<module> (INHERIT, SET)
// — mã chưa chuyển chạy như cũ, mã đã chuyển `SET LOCAL ROLE r_<module>` được trên cả pool cũ lẫn pool
// app_<đơn vị>. Quyền bảng hiện có của <module>_app được chép sang r_<module> ở pf 000090; migration mới
// GRANT cho r_<module> (vai cũ thừa hưởng qua membership). W2.6 bỏ quyền bảng trực tiếp của <module>_app.

import (
	"slices"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Vai đăng nhập theo đơn vị triển khai (ADR-0028 §1, cụm pf).
const (
	AppPlatform = "app_platform"
	AppWorker   = "app_worker"
	AppPublic   = "app_public"
	AppSync     = "app_sync"
)

// ModuleGroup nối module với vai nhóm r_* và vai đăng nhập cũ của nó.
type ModuleGroup struct {
	Module string // tên module.yaml, vd pf/content
	Role   string // r_<module>
	Legacy string // <module>_app (kỳ chuyển)
}

// LoginUnit là vai đăng nhập của một đơn vị triển khai.
type LoginUnit struct {
	Role string
	Unit string // cmd/<đơn vị>; module ghép lấy từ modreg.UnitModules (modgen sinh từ import)
	// Extra: module đơn vị mở pool theo TÊN vai mà không import gốc module (không thấy trong import),
	// hoặc đơn vị chưa ghép module trong tiến trình nhưng ADR-0028 gán (engine đồng bộ).
	Extra []string
}

// LoginUnits là vai đăng nhập cụm pf (ADR-0028 §1). mkg/ts/vault: W2.6.
var LoginUnits = []LoginUnit{
	{Role: AppPlatform, Unit: "api-platform"},
	{Role: AppWorker, Unit: "worker"},
	// api-public mở pool party_app theo tên (cmd/api-public/assembly: không import gốc pf/party).
	{Role: AppPublic, Unit: "api-public", Extra: []string{"pf/party"}},
	// Engine đồng bộ (pf/sync) và objsync — objsync chưa ghép module nào trong tiến trình.
	{Role: AppSync, Unit: "objsync", Extra: []string{"pf/sync"}},
}

// ModuleGroups trả vai nhóm của mọi module pf có vai đăng nhập cũ <module>_app (đã sắp theo tên module).
// Suy từ registry (modreg.Catalog) + baseRoles: thêm module pf mới có vai <module>_app thì r_* tự có.
func ModuleGroups() []ModuleGroup {
	var out []ModuleGroup
	for _, e := range modreg.Catalog() {
		zone, seg, ok := strings.Cut(e.Name, "/")
		if !ok || zone != "pf" {
			continue
		}
		legacy := seg + "_app"
		if !slices.ContainsFunc(baseRoles, func(r Role) bool { return r.Name == legacy }) {
			continue
		}
		out = append(out, ModuleGroup{Module: e.Name, Role: modreg.DBRole(e.Name), Legacy: legacy})
	}
	return out
}

// UnitGroupRoles trả các r_* mà vai đăng nhập của đơn vị là thành viên (đã sắp).
func UnitGroupRoles(u LoginUnit) []string {
	mods := append(modreg.UnitModules(u.Unit), u.Extra...)
	var out []string
	for _, g := range ModuleGroups() {
		if slices.Contains(mods, g.Module) && !slices.Contains(out, g.Role) {
			out = append(out, g.Role)
		}
	}
	slices.Sort(out)
	return out
}

func buildRoles() []Role {
	groups := ModuleGroups()
	pf := []migrations.Cluster{migrations.PF}
	out := []Role{baseRoles[0]} // ag_shared_user: đích MemberOf của mọi vai sau
	for _, g := range groups {
		// r_* thuộc ag_shared_user (outbox, inbox, idempotency như vai cũ); NOLOGIN nên Clusters chỉ để lọc
		// theo AG_CLUSTERS, không GRANT CONNECT.
		out = append(out, Role{Name: g.Role, MemberOf: []string{SharedUser}, Clusters: pf})
	}
	for _, r := range baseRoles[1:] {
		for _, g := range groups {
			if g.Legacy == r.Name {
				r.MemberOf = append(slices.Clone(r.MemberOf), g.Role)
			}
		}
		out = append(out, r)
	}
	for _, u := range LoginUnits {
		// Không thuộc ag_shared_user: tự vai đăng nhập không có quyền bảng nào (kể cả shared.*).
		out = append(out, Role{Name: u.Role, Login: true, NoInherit: true, MemberOf: UnitGroupRoles(u), Clusters: pf})
	}
	return out
}
