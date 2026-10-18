package db

// Biến ngữ cảnh trong DB (W2.4, ADR-0028 §2, ADR-0029, kiến trúc v4.1 §14 + §17.2). Mỗi giao dịch mở qua
// ForModule đặt — cùng một câu lệnh với vai của module — các biến app.* bằng set_config(…, true) (tức
// SET LOCAL: hết hiệu lực khi COMMIT/ROLLBACK, an toàn với PgBouncer transaction mode). Biến không có giá
// trị trong ctx được đặt rỗng (không bao giờ giữ giá trị cũ, kể cả trong savepoint lồng). SQL đọc qua
// k_ref.ctx_uuid / k_ref.ctx_text (pf 000091), rỗng → NULL. app.tenant_id vẫn do tenant.WithTenantTx đặt.

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Tên biến ngữ cảnh (khóa của set_config / k_ref.ctx_*).
const (
	VarNodeID        = "app.node_id"
	VarDept          = "app.dept"
	VarXaIDs         = "app.xa_ids"
	VarPurpose       = "app.purpose"
	VarActorRef      = "app.actor_ref"
	VarOnBehalfOfRef = "app.on_behalf_of_ref"
)

// ContextVars là giá trị các biến app.* (ngoài tenant) của một giao dịch. Mã tham chiếu là uuid.UUID —
// không có đường nào đưa tên, SĐT, email… vào biến DB.
type ContextVars struct {
	NodeID        uuid.UUID // Nút của ngữ cảnh phiên KN; không có KN → đơn vị công tác của token (org_ref)
	Dept          string    // mã phòng ban của ngữ cảnh phiên KN
	XaIDs         []string  // mã xã phụ trách (Cấp Xã)
	Purpose       string    // mã mục đích xử lý (authz.WithPurpose)
	ActorRef      uuid.UUID // sub của token — mã tham chiếu bút danh
	OnBehalfOfRef uuid.UUID // tài khoản được hỗ trợ (chế độ hỗ trợ Admin, ADR-0037)
}

// ContextVarsFrom suy biến ngữ cảnh từ authz.Principal (kèm KNContext) và mục đích trong ctx. Không có
// Principal (consumer, job) → mọi biến rỗng.
func ContextVarsFrom(ctx context.Context) ContextVars {
	v := ContextVars{Purpose: authz.PurposeFrom(ctx)}
	p, ok := authz.FromContext(ctx)
	if !ok {
		return v
	}
	v.ActorRef = p.ActorRef
	if p.KN == nil {
		v.NodeID = p.OrgRef
		return v
	}
	// Đã chọn ngữ cảnh phiên: chỉ dùng giá trị của ngữ cảnh (Admin không gắn Nút → rỗng, không lùi về org_ref).
	v.NodeID = p.KN.OrgRef
	v.Dept = p.KN.Department
	v.XaIDs = p.KN.Communes
	if p.KN.Support != nil {
		v.OnBehalfOfRef = p.KN.Support.OnBehalfOf
	}
	return v
}

// ErrNodeScopeRequired: Principal người dùng có vai/cấp gắn Nút nhưng ngữ cảnh không có app.node_id (FD, rủi ro R-5
// của W3.4, docs/design/cooperative.md §18.4). Chính sách p_scope có nhánh "app.node_id rỗng → cả tenant" cho
// consumer/job/Admin; người dùng HTX/xã rơi vào nhánh đó sẽ thấy cả tenant, nên ForModule từ chối mở giao dịch.
// Trả ra ngoài dưới dạng problem.NodeScopeRequired (403 node_scope_required); errors.Is(err, ErrNodeScopeRequired).
var ErrNodeScopeRequired = errors.New("db: người dùng gắn Nút nhưng ngữ cảnh không có app.node_id")

// Ánh xạ vai → "phải có Nút" (FD). Nguồn: app/HTX/app.yaml (roles: coop_manager — quản lý HTX, phạm vi = HTX của
// org_ref), app/KN/app.yaml (roles: kn_officer — cán bộ KN, phạm vi tính từ org_ref; context_roles: kn_xa — Cấp
// Xã), authz/kn.go. Vai cấp Tỉnh/Quốc gia/Admin (kn_tinh, kn_tw, admin, kn_province_admin, kn_national_admin,
// platform_admin) giữ nhánh Nút rỗng như trước. Hằng chuỗi (không import port của module — archlint);
// TestNodeBoundRolesDeclared_FD kiểm các vai này còn khai trong app.yaml.
var (
	// NodeBoundRoles: vai realm (Principal không có ngữ cảnh phiên KN) bắt buộc có org_ref.
	NodeBoundRoles = []string{"coop_manager", "kn_officer"}
	// NodeBoundContextRoles: vai ngữ cảnh phiên KN bắt buộc có Nút của lần gán.
	NodeBoundContextRoles = []string{authz.KNXa}
	// NodeExemptRoles: vai realm phạm vi tỉnh/toàn quốc/toàn hệ thống — có vai này thì không bắt buộc Nút dù có
	// thêm vai trong NodeBoundRoles.
	NodeExemptRoles = []string{authz.RolePlatformAdmin, "kn_national_admin", "kn_province_admin"}
)

// RequiresNode cho biết Principal người dùng phải có Nút (app.node_id) khi mở giao dịch của module. Có ngữ
// cảnh phiên KN thì chỉ xét vai của ngữ cảnh (vai realm không dùng cho phạm vi KN v7.7).
func RequiresNode(p authz.Principal) bool {
	if p.KN != nil {
		return slices.Contains(NodeBoundContextRoles, p.KN.Role)
	}
	if slices.ContainsFunc(NodeExemptRoles, p.Has) {
		return false
	}
	return slices.ContainsFunc(NodeBoundRoles, p.Has)
}

// checkNodeScope chặn giao dịch của Principal gắn Nút mà v không có Nút. Không có Principal (consumer, job,
// worker) → luôn qua. db.WithoutNodeScope (xóa Nút tạm trong giao dịch đã mở) không đi qua đây.
func checkNodeScope(ctx context.Context, v ContextVars) error {
	if v.NodeID != uuid.Nil {
		return nil
	}
	p, ok := authz.FromContext(ctx)
	if !ok || !RequiresNode(p) {
		return nil
	}
	return problem.NodeScopeRequired.WithCause(ErrNodeScopeRequired)
}

// setConfigSQL đặt vai của module ($1, tương đương SET LOCAL ROLE — kiểm membership như nhau, lỗi 42501)
// và các biến app.* ($2…$7) trong MỘT lượt khứ hồi. Thứ tự tham số khớp ContextVars.args.
const setConfigSQL = "SELECT set_config('role', $1, true), " +
	"set_config('" + VarNodeID + "', $2, true), " +
	"set_config('" + VarDept + "', $3, true), " +
	"set_config('" + VarXaIDs + "', $4, true), " +
	"set_config('" + VarPurpose + "', $5, true), " +
	"set_config('" + VarActorRef + "', $6, true), " +
	"set_config('" + VarOnBehalfOfRef + "', $7, true)"

// Settings trả giá trị chuỗi của từng biến theo tên (rỗng = không có) — dạng đặt vào set_config.
func (v ContextVars) Settings() map[string]string {
	a := v.values()
	return map[string]string{
		VarNodeID: a[0], VarDept: a[1], VarXaIDs: a[2], VarPurpose: a[3], VarActorRef: a[4], VarOnBehalfOfRef: a[5],
	}
}

func (v ContextVars) values() [6]string {
	return [6]string{uuidText(v.NodeID), v.Dept, textArray(v.XaIDs), v.Purpose, uuidText(v.ActorRef), uuidText(v.OnBehalfOfRef)}
}

func (v ContextVars) args() []any {
	a := v.values()
	out := make([]any, len(a))
	for i, s := range a {
		out[i] = s
	}
	return out
}

func uuidText(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// textArray viết danh sách thành literal mảng PostgreSQL ({"a","b"}), đọc được bằng
// k_ref.ctx_text('app.xa_ids')::text[]. Danh sách rỗng → "" (biến không có → NULL, không phải '{}').
func textArray(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, x := range xs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		for _, r := range x {
			if r == '"' || r == '\\' {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// WithoutNodeScope chạy fn trong giao dịch tx với app.node_id và app.xa_ids tạm rỗng, rồi đặt lại giá trị cũ
// (W3.4). Chính sách p_scope (RESTRICTIVE, pf 000134) khi đó chỉ còn lọc tenant (p_tenant vẫn áp). CHỈ dùng
// cho kiểm toàn vẹn phải nhìn cả tenant — vd chồng lấn thửa giữa các HTX — và không trả dòng ngoài phạm vi cho
// người gọi; mọi đọc/ghi khác giữ phạm vi Nút. fn lỗi → trả lỗi đó (giao dịch thường đã hỏng, không đặt lại).
func WithoutNodeScope(ctx context.Context, tx pgx.Tx, fn func() error) error {
	var node, xa string
	if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting('"+VarNodeID+"', true), ''), "+
		"coalesce(current_setting('"+VarXaIDs+"', true), '')").Scan(&node, &xa); err != nil {
		return err
	}
	const set = "SELECT set_config('" + VarNodeID + "', $1, true), set_config('" + VarXaIDs + "', $2, true)"
	if _, err := tx.Exec(ctx, set, "", ""); err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, set, node, xa)
	return err
}
