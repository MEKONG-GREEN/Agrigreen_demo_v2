package dbadmin

// Vai theo ADR-0028: vai nhóm r_<module> NOLOGIN mang quyền chạy của module, vai đăng nhập app_<đơn vị> LOGIN
// NOINHERIT là thành viên các r_* của module ghép trong đơn vị, `SET LOCAL ROLE` trong giao dịch
// (internal/shared/db.ForModule / WithContextTx).
//
// W2.3 dựng khung cho cụm pf; W2.6 chuyển hết (mkg, ts, vault), bỏ quyền trực tiếp của vai cũ:
//   - danh sách r_* suy từ registry (modreg.Catalog: module có owns_schema), KHÔNG phụ thuộc vai cũ còn tồn tại;
//     cụm của r_* = zones của module;
//   - vai cũ <module>_app (kỳ chuyển cuối, xóa ở bản phát hành sau): không còn quyền trực tiếp nào (pf 000119,
//     mkg 000035, vault 000003 thu hồi) nhưng VẪN là thành viên r_<module> (INHERIT) — nhị phân cũ chạy được
//     khi lùi bản; giữ tên cho userlist PgBouncer;
//   - vai đăng nhập không theo module (platform_api, public_api, worker_platform, mkg_api, worker_internal,
//     ingest_mkg, ingest_ts, ts_reader): quyền chép sang r_ops / r_measurement (FoldedLogins), vai giữ nguyên
//     một kỳ cho lùi bản, mã mới không dùng.

import (
	"slices"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Vai đăng nhập theo đơn vị triển khai (ADR-0028 §1).
const (
	AppPlatform       = "app_platform"
	AppWorker         = "app_worker"
	AppPublic         = "app_public"
	AppSync           = "app_sync"
	AppInternal       = "app_internal"
	AppWorkerInternal = "app_worker_internal" //nolint:gosec // G101: tên vai DB, không phải mật khẩu
	AppIngest         = "app_ingest"
	AppVault          = "app_vault"
)

// OpsGroup là vai nhóm vận hành NOLOGIN (db.OpsRole): bảng vận hành schema shared qua ag_shared_user.
const OpsGroup = "r_ops"

// ModuleGroup nối module với vai nhóm r_* và vai đăng nhập cũ của nó.
type ModuleGroup struct {
	Module string // tên module.yaml, vd pf/content
	Role   string // r_<module>
	Legacy string // <module>_app (kỳ chuyển; rỗng nếu module sinh sau ADR-0028, vd pf/kref)
	// Pub: vai đọc view công bố r_<module>_pub (§14, ADR-0031 §2, W6.3) — chỉ khi module sở hữu schema
	// <cụm>_<module>_pub; rỗng nếu không. Quyền (USAGE schema, SELECT view) do migration công bố cấp.
	Pub      string
	Clusters []migrations.Cluster
}

// AnalyticsModule là module phân tích (cap/analytics, aka của pf/metrics — ADR-0027): vai nhóm của nó là thành
// viên mọi r_<module>_pub (R-DB-10, W6.3). r_agg (W6.4) là bên đọc thứ hai.
const AnalyticsModule = "pf/metrics"

// AggRole là vai tổng hợp vượt đơn vị (W6.4, ADR-0031 §3, R-DB-11): NOLOGIN, BYPASSRLS, INHERIT, thành viên mọi
// r_<module>_pub (chỉ SELECT view công bố); không thuộc ag_shared_user; migration pf 000129 cấp thêm đúng
// SELECT (tenant_id) trên k_ref.org (liệt kê tenant). Thành viên DUY NHẤT là vai đăng nhập riêng AppAgg.
const AggRole = "r_agg"

// AppAgg là vai đăng nhập riêng của bộ chạy job tổng hợp (ADR-0031 §3, gói FC): LOGIN NOINHERIT, chỉ là thành viên
// r_agg (INHERIT FALSE, SET TRUE) — không r_* nào khác, không ag_shared_user. Chỉ có quyền r_agg sau SET LOCAL ROLE
// trong db.WithAggTx (shared/db), sau khi job đã khai được kiểm là đã duyệt bốn mắt (giao dịch riêng trên pool
// app_worker → r_metrics). Mặc định của vai: giao dịch chỉ đọc, statement_timeout (AggRoleSettings). Pool nhỏ của
// cmd/worker (-agg-run); pg_hba chỉ cho kết nối từ đơn vị worker (docs/runbooks/agg-jobs.md).
const AppAgg = "app_agg"

// AggRoleSettings là tham số mặc định của AppAgg (ALTER ROLE … SET, bootstrap): chạy trên cụm chính khi chưa có
// bản sao chỉ đọc (ADR-0031 §3). WithAggTx vẫn đặt cục bộ — đây là lớp chặn thứ hai.
var AggRoleSettings = map[string]string{"default_transaction_read_only": "on", "statement_timeout": "10min"}

// LoginUnit là vai đăng nhập của một đơn vị triển khai.
type LoginUnit struct {
	Role string
	Unit string // cmd/<đơn vị>; module ghép lấy từ modreg.UnitModules (modgen sinh từ import)
	// Tools: lệnh vận hành một lần (drill, formimport, rebuilddrill) dùng chung vai đăng nhập của đơn vị —
	// module chúng ghép có trong membership (kiểm thử TestEveryUnitHasLogin_ADR0028).
	Tools []string
	// Aliases: tên cũ của đơn vị còn giữ một kỳ (cùng nhị phân, cùng vai) — vd api-public của integration-gateway
	// (W5.2, ADR-0033 §7.5). Module bí danh ghép cũng vào membership.
	Aliases []string
	// Extra: module đơn vị mở pool theo TÊN mà không import gốc module (không thấy trong import),
	// hoặc đơn vị chưa ghép module trong tiến trình nhưng ADR-0028 gán (engine đồng bộ).
	Extra []string
	// Ops: đơn vị cần r_ops (idempotency HTTP, consumer không thuộc module).
	Ops      bool
	Clusters []migrations.Cluster
}

var (
	pfOnly    = []migrations.Cluster{migrations.PF}
	mkgOnly   = []migrations.Cluster{migrations.MKG}
	vaultOnly = []migrations.Cluster{migrations.Vault}
)

// LoginUnits là vai đăng nhập theo đơn vị (ADR-0028 §1). Mỗi cụm còn ag_migrator, ag_outbox_publisher, ag_ops_jobs
// (job vận hành, không theo module) và vai giám sát (mkg_monitor/ts_monitor): bí mật pf 7, mkg 7, ts 5, vault 4.
var LoginUnits = []LoginUnit{
	{Role: AppPlatform, Unit: "api-platform", Ops: true, Clusters: pfOnly},
	// worker chạy consumer liên module của cmd/api-platform/assembly.Consumers (gói không thuộc cmd/internal nên modgen
	// không thấy): van thu gom pf/cropcycle (r_cropcycle), van hub pf/geo (r_geo) và ảnh chụp pf/simulation
	// (r_simulation, consumer pf.simulation.snapshot — thiếu từ trước W3.3, gói FC) — khai Extra để có membership.
	// Kiểm thử cmd/worker TestConsumerModulesInUnitLogin: mọi consumer của worker có vai module trong app_worker.
	// Job tổng hợp (r_agg) KHÔNG chạy trên vai này — vai đăng nhập riêng AppAgg (ADR-0031 §3).
	{Role: AppWorker, Unit: "worker", Tools: []string{"drill", "formimport"}, Extra: []string{"pf/cropcycle", "pf/geo", "pf/simulation"}, Ops: true, Clusters: pfOnly},
	// Cổng tích hợp (W5.2, ADR-0033 §1: vai app_public, không thêm vai) mở pool party theo tên
	// (cmd/integration-gateway/assembly: không import gốc pf/party). api-public là bí danh một kỳ.
	{Role: AppPublic, Unit: "integration-gateway", Aliases: []string{"api-public"}, Extra: []string{"pf/party"}, Ops: true, Clusters: pfOnly},
	// Engine đồng bộ (pf/sync) và objsync — objsync chưa ghép module nào trong tiến trình.
	{Role: AppSync, Unit: "objsync", Extra: []string{"pf/sync"}, Clusters: pfOnly},
	{Role: AppInternal, Unit: "api-internal", Ops: true, Clusters: mkgOnly},
	{Role: AppWorkerInternal, Unit: "worker-internal", Tools: []string{"rebuilddrill"}, Ops: true, Clusters: mkgOnly},
	// ingest: sổ thiết bị + phiếu cân dẫn xuất ở cụm mkg, chuỗi thời gian ts_weigh ở cụm ts (mkg/measurement).
	{Role: AppIngest, Unit: "ingest", Clusters: []migrations.Cluster{migrations.MKG, migrations.TS}},
	{Role: AppVault, Unit: "identity-vault", Clusters: vaultOnly},
}

// legacyLogin: vai đăng nhập cũ của module có tên khác <đoạn cuối>_app.
var legacyLogin = map[string]string{"vault/identity": "vault_app"}

// FoldedLogins: vai đăng nhập cũ KHÔNG theo module → vai nhóm nhận quyền của nó (W2.6, mkg 000035, ts 000004).
// Các vai này không là thành viên vai nhóm (tránh mở rộng quyền khi lùi bản) và giữ nguyên quyền một kỳ.
var FoldedLogins = map[string]string{
	"platform_api":    OpsGroup,
	"worker_platform": OpsGroup,
	"public_api":      OpsGroup,
	"mkg_api":         OpsGroup,
	"worker_internal": OpsGroup,
	// ingest là một phần của mkg/measurement (owns_schema mkg_measurement, ts_weigh).
	"ingest_mkg": "r_measurement",
	"ingest_ts":  "r_measurement",
	"ts_reader":  "r_measurement",
}

// ModuleGroups trả vai nhóm của MỌI module có schema (owns_schema) trong registry (modreg.Catalog), sắp theo tên
// module. Cụm = zones của module. Legacy là vai đăng nhập cũ nếu còn trong danh mục vai (kỳ chuyển).
func ModuleGroups() []ModuleGroup {
	var out []ModuleGroup
	for _, e := range modreg.Catalog() {
		if len(e.OwnsSchema) == 0 {
			continue
		}
		_, seg, _ := strings.Cut(e.Name, "/")
		legacy, ok := legacyLogin[e.Name]
		if !ok {
			legacy = seg + "_app"
		}
		if !slices.ContainsFunc(baseRoles, func(r Role) bool { return r.Name == legacy }) {
			legacy = ""
		}
		cs := make([]migrations.Cluster, 0, len(e.Zones))
		for _, z := range e.Zones {
			cs = append(cs, migrations.Cluster(z))
		}
		pub := ""
		if slices.ContainsFunc(e.OwnsSchema, func(s string) bool { return strings.HasSuffix(s, "_pub") }) {
			pub = modreg.PubRole(e.Name)
		}
		out = append(out, ModuleGroup{Module: e.Name, Role: modreg.DBRole(e.Name), Legacy: legacy, Pub: pub, Clusters: cs})
	}
	return out
}

// ClusterGroups trả vai nhóm r_* có ở cụm c (đã sắp theo module).
func ClusterGroups(c migrations.Cluster) []ModuleGroup {
	var out []ModuleGroup
	for _, g := range ModuleGroups() {
		if slices.Contains(g.Clusters, c) {
			out = append(out, g)
		}
	}
	return out
}

// UnitModules trả module mà vai đăng nhập của đơn vị phục vụ: module ghép của đơn vị, các lệnh Tools và bí danh
// Aliases, cộng Extra.
func UnitModules(u LoginUnit) []string {
	mods := slices.Clone(modreg.UnitModules(u.Unit))
	for _, t := range slices.Concat(u.Tools, u.Aliases) {
		mods = append(mods, modreg.UnitModules(t)...)
	}
	return append(mods, u.Extra...)
}

// UnitGroupRoles trả các vai nhóm mà vai đăng nhập của đơn vị là thành viên (đã sắp).
func UnitGroupRoles(u LoginUnit) []string {
	mods := UnitModules(u)
	var out []string
	for _, g := range ModuleGroups() {
		if slices.Contains(mods, g.Module) && !slices.Contains(out, g.Role) {
			out = append(out, g.Role)
		}
	}
	if u.Ops {
		out = append(out, OpsGroup)
	}
	slices.Sort(out)
	return out
}

// LoginRoles trả tên mọi vai đăng nhập theo đơn vị, cộng vai đăng nhập riêng của job tổng hợp (AppAgg).
func LoginRoles() []string {
	out := make([]string, 0, len(LoginUnits)+1)
	for _, u := range LoginUnits {
		out = append(out, u.Role)
	}
	return append(out, AppAgg)
}

// ExclusiveMembers: vai nhóm đặc quyền → thành viên DUY NHẤT được phép. Bootstrap thu hồi (REVOKE) mọi thành viên
// khác đang có trong pg_auth_members — gỡ grant cũ khi danh mục đổi (gói FC: r_agg rời app_worker sang app_agg)
// mà không cần migration (ag_migrator không có CREATEROLE).
var ExclusiveMembers = map[string][]string{AggRole: {AppAgg}}

// PubReaders (ADR-0053 §4, gói PFPUB): module KHÔNG phải cap/analytics được đọc view công bố _pub — vai nhóm r_<module>
// là thành viên r_<chủ>_pub (chỉ USAGE schema _pub + SELECT view công bố, không quyền nào trên schema nội bộ của chủ;
// cổng cô lập schema W9.1 đã miễn *_pub). Khóa: module đọc; giá trị: module chủ view _pub. Mỗi mục mới rà soát như thay
// đổi hợp đồng (bên đọc ghi vào consumers của contracts/pubview). Bootstrap cấp — migration không cấp được thành viên
// (ag_migrator không có CREATEROLE), và cấp thẳng SELECT view cho r_<module> bị TestPubViewsDeclaredAndPrivileges_W63 chặn.
var PubReaders = map[string][]string{
	// KHTC (Phòng Kế hoạch – Tài chính, cài trong pf/extension — W11.61…W11.69, port extension/port.KHTCSources) đọc số
	// đã chốt của Trồng trọt (pf_extension_pub), CNTS (pf_forms_pub, họ closed_figures/v1) và Báo giá (pf_price_pub).
	"pf/extension": {"pf/extension", "pf/forms", "pf/price"},
}

// pubReaderRoles trả r_<chủ>_pub mà vai nhóm của module là thành viên theo PubReaders (chủ không có schema _pub → bỏ).
func pubReaderRoles(module string, groups []ModuleGroup) []string {
	var out []string
	for _, owner := range PubReaders[module] {
		for _, g := range groups {
			if g.Module == owner && g.Pub != "" {
				out = append(out, g.Pub)
			}
		}
	}
	return out
}

// OwnerRole trả vai chủ schema của module: own_<đoạn cuối>, vd pf/party → own_party (cùng đoạn với r_<module>).
func OwnerRole(module string) string { return "own_" + strings.TrimPrefix(modreg.DBRole(module), "r_") }

func buildRoles() []Role {
	groups := ModuleGroups()
	out := []Role{baseRoles[0]} // ag_shared_user: đích MemberOf của mọi vai sau
	// r_<module>_pub trước r_* (đích MemberOf của vai phân tích); không thuộc ag_shared_user — chỉ đọc view _pub.
	var pubs []string
	for _, g := range groups {
		if g.Pub != "" {
			pubs = append(pubs, g.Pub)
			out = append(out, Role{Name: g.Pub, Clusters: g.Clusters})
		}
	}
	// r_agg sau r_<module>_pub (đích MemberOf của nó), trước vai đăng nhập (W6.4).
	if len(pubs) > 0 {
		out = append(out, Role{Name: AggRole, BypassRLS: true, MemberOf: slices.Clone(pubs), Clusters: pfOnly})
	}
	for _, g := range groups {
		// r_* thuộc ag_shared_user (outbox, inbox, idempotency như vai cũ); NOLOGIN nên Clusters chỉ để lọc
		// theo AG_CLUSTERS, không GRANT CONNECT.
		member := []string{SharedUser}
		if g.Module == AnalyticsModule {
			member = append(member, pubs...)
		}
		member = append(member, pubReaderRoles(g.Module, groups)...)
		out = append(out, Role{Name: g.Role, MemberOf: member, Clusters: g.Clusters})
	}
	out = append(out, Role{Name: OpsGroup, MemberOf: []string{SharedUser}, Clusters: []migrations.Cluster{migrations.PF, migrations.MKG}})
	// own_<module> (W7.1, R-DB-12, ADR-0038): vai NOLOGIN chủ schema của module, chỉ pipeline migration dùng —
	// ag_migrator là thành viên. Bước này chỉ dựng vai; chuyển quyền sở hữu đối tượng sẵn có và chạy migration
	// bằng SET ROLE own_<module> là bước sau (cần thư mục migration theo module).
	var owners []string
	for _, g := range groups {
		owners = append(owners, OwnerRole(g.Module))
		out = append(out, Role{Name: OwnerRole(g.Module), Clusters: g.Clusters})
	}
	for _, r := range baseRoles[1:] {
		if r.Name == Migrator {
			r.MemberOf = append(slices.Clone(r.MemberOf), owners...)
		}
		for _, g := range groups {
			if g.Legacy == r.Name {
				r.MemberOf = append(slices.Clone(r.MemberOf), g.Role)
			}
		}
		out = append(out, r)
	}
	for _, u := range LoginUnits {
		// Không thuộc ag_shared_user: tự vai đăng nhập không có quyền bảng nào (kể cả shared.*).
		out = append(out, Role{Name: u.Role, Login: true, NoInherit: true, MemberOf: UnitGroupRoles(u), Clusters: u.Clusters})
	}
	if len(pubs) > 0 {
		// app_agg (ADR-0031 §3): chỉ r_agg, không thuộc ag_shared_user; tham số mặc định chỉ đọc + statement_timeout.
		out = append(out, Role{Name: AppAgg, Login: true, NoInherit: true, MemberOf: []string{AggRole}, Settings: AggRoleSettings, Clusters: pfOnly})
	}
	return out
}
