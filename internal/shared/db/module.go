package db

// Vai DB theo module (ADR-0028, R-DB-02, R-DB-03): một vai đăng nhập NOINHERIT mỗi đơn vị triển khai
// (app_platform, app_worker, …) là thành viên các vai nhóm NOLOGIN r_<module>; mỗi giao dịch của module
// chạy `SET LOCAL ROLE r_<module>` nên SQL của module chỉ có quyền của chính nó. SET LOCAL hết hiệu lực khi
// COMMIT/ROLLBACK — kết nối trả về pool luôn ở vai đăng nhập, an toàn với PgBouncer transaction mode.
// Đây là nơi DUY NHẤT trong mã được viết chuỗi SET ROLE (archlint agnosessionset).

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ModuleID định danh một module trong registry (module.yaml qua modreg/catalog_gen.go). Vai DB suy ra
// từ tên module lúc khởi tạo (r_<module>), mã nghiệp vụ không truyền tên vai. Giá trị rỗng không hợp lệ.
type ModuleID struct {
	name string
	role string
}

// Module trả ModuleID của một module có trong registry; module lạ → panic (lỗi lắp ráp, lộ ra lúc khởi
// động hoặc ở kiểm thử, không phải lúc chạy request). Dùng như biến gói: `var DBModule = db.Module("pf/content")`.
func Module(name string) ModuleID {
	if _, ok := modreg.Entry(name); !ok {
		panic(fmt.Sprintf("db: module %q không có trong registry (module.yaml, go run ./tools/modgen)", name))
	}
	return ModuleID{name: name, role: modreg.DBRole(name)}
}

// OpsRole là vai nhóm vận hành NOLOGIN của đơn vị triển khai (W2.6, ADR-0028 §1): chỉ bảng vận hành schema
// shared (idempotency, inbox của consumer không thuộc module) qua ag_shared_user — thay các vai đăng nhập
// không theo module platform_api, public_api, worker_platform, mkg_api, worker_internal.
const OpsRole = "r_ops"

// Ops là "module" vận hành của đơn vị: giao dịch mở qua ForModule(pool, Ops) chạy SET LOCAL ROLE r_ops.
// Không thuộc registry module (không có module.yaml) — dùng cho idempotency HTTP, consumer mẫu G0.
var Ops = ModuleID{name: "shared/ops", role: OpsRole}

// Name là tên module (vd pf/content).
func (m ModuleID) Name() string { return m.name }

// Role là vai nhóm NOLOGIN của module (vd r_content).
func (m ModuleID) Role() string { return m.role }

// Valid cho biết m được tạo bằng Module.
func (m ModuleID) Valid() bool { return m.role != "" }

// SetLocalRole đặt vai của module cho phần còn lại của giao dịch tx (SET LOCAL ROLE). Chỉ dùng trong
// giao dịch; ngoài giao dịch PostgreSQL bỏ qua SET LOCAL kèm cảnh báo — vì vậy luôn đi qua
// ForModule/WithContextTx.
func SetLocalRole(ctx context.Context, tx pgx.Tx, m ModuleID) error {
	if !m.Valid() {
		return fmt.Errorf("db: ModuleID rỗng — dùng db.Module(\"<vùng>/<module>\")")
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{m.role}.Sanitize()); err != nil {
		return fmt.Errorf("db: SET LOCAL ROLE %s: %w", m.role, err)
	}
	return nil
}

// moduleDB mở giao dịch rồi chuyển sang vai của module.
type moduleDB struct {
	b tenant.Beginner
	m ModuleID
}

// ForModule bọc pool (vai đăng nhập của đơn vị triển khai, hoặc vai <module>_app cũ trong kỳ chuyển — là
// thành viên r_<module>) để MỌI giao dịch mở qua nó chạy `SET LOCAL ROLE r_<module>` ngay sau BEGIN
// (dạng set_config('role', …, true), cùng câu lệnh với các biến ngữ cảnh app.* — W2.4).
// Kết quả dùng được ở mọi chỗ nhận tenant.Beginner (tenant.WithTenantTx, inbox.Process, …), nên module
// chuyển sang r_* chỉ cần bọc pool một lần ở gốc module.
func ForModule(b tenant.Beginner, m ModuleID) tenant.Beginner {
	if b == nil {
		return nil
	}
	if md, ok := b.(moduleDB); ok && md.m == m {
		return md
	}
	return moduleDB{b: b, m: m}
}

func (d moduleDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.b.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	if err := setLocalContext(ctx, tx, d.m); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

// setLocalContext đặt vai của module và biến ngữ cảnh app.* (ctxvars.go) trong một câu lệnh — nơi duy
// nhất mọi giao dịch của module (ForModule, WithContextTx, WithModuleTenantTx) đi qua. Principal người dùng có
// vai/cấp gắn Nút mà không có Nút → ErrNodeScopeRequired (403 node_scope_required, FD — ctxvars.go).
func setLocalContext(ctx context.Context, tx pgx.Tx, m ModuleID) error {
	if !m.Valid() {
		return fmt.Errorf("db: ModuleID rỗng — dùng db.Module(\"<vùng>/<module>\")")
	}
	v := ContextVarsFrom(ctx)
	if err := checkNodeScope(ctx, v); err != nil {
		return err
	}
	args := append([]any{m.role}, v.args()...)
	if _, err := tx.Exec(ctx, setConfigSQL, args...); err != nil {
		return fmt.Errorf("db: SET LOCAL ROLE %s + biến ngữ cảnh: %w", m.role, err)
	}
	return nil
}

// WithContextTx là cửa giao dịch của module theo ADR-0028 §2: BEGIN → SET LOCAL ROLE r_<module> →
// set_config('app.tenant_id', tenant của ctx, true) → fn → COMMIT (lỗi → ROLLBACK). Tenant lấy từ
// context đã xác thực (tenant.WithID), không từ tham số. Cùng câu lệnh với vai, ForModule đặt các biến
// app.node_id, app.dept, app.xa_ids, app.purpose, app.actor_ref, app.on_behalf_of_ref từ ctx (W2.4, ctxvars.go).
func WithContextTx(ctx context.Context, b tenant.Beginner, m ModuleID, fn func(pgx.Tx) error) error {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return err
	}
	return WithModuleTenantTx(ctx, b, m, tid, fn)
}

// WithModuleTenantTx như WithContextTx nhưng tenant truyền tường minh — cho consumer/job đã có tenant
// của sự kiện (tương đương tenant.WithTenantTx của kỳ chuyển).
func WithModuleTenantTx(ctx context.Context, b tenant.Beginner, m ModuleID, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if !m.Valid() {
		return fmt.Errorf("db: ModuleID rỗng — dùng db.Module(\"<vùng>/<module>\")")
	}
	return tenant.WithTenantTx(ctx, ForModule(b, m), tid, fn)
}
