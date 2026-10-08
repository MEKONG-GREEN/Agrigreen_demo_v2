// Package kref là module lõi pf/kref (`k/ref`, kind kernel — ADR-0029, kiến trúc v4.1 §17.1, R-DB-05):
// bản chiếu CHỈ ĐỌC `k_ref` của dữ liệu lõi trong cụm (Nút, bao đóng cây Nút, đơn vị hành chính có hiệu lực,
// cấp phép chia sẻ đang hiệu lực, phạm vi chia sẻ mở rộng) cho chính sách RLS và module khác đọc tại chỗ.
//
//   - Worker `pf.kref` (cmd/worker) dựng từ sự kiện — kể cả khi nguồn cùng cụm, để một cơ chế chạy được ở
//     mọi cụm (ADR-0029 §3): pf.party.org.created/moved/deactivated/reactivated,
//     pf.masterdata.admin_unit.changed, pf.party.share_grant.approved/revoked. Idempotent qua shared.inbox;
//     mỗi sự kiện áp trong MỘT giao dịch (Nút di chuyển → dựng lại bao đóng của cả nhánh cùng giao dịch).
//   - Rebuild dựng lại toàn bộ k_ref của từng tenant từ nguồn (xóa + chèn trong một giao dịch): sau phục hồi,
//     sau triển khai lần đầu (Nút có trước W2.5 chưa có sự kiện created), và trong diễn tập (cmd/drill kref).
//   - CHỈ module này ghi k_ref, bằng vai r_kref (SET LOCAL ROLE qua db.ForModule); mọi r_* khác chỉ SELECT
//     (pf 000092). share_scope tạo RỖNG — chưa có bên dựng (W4.2).
//   - Chỉ số ag.k_ref.lag (giây, theo `relation`; Prometheus ag_k_ref_lag_seconds) — từ lúc sự kiện nguồn
//     được ghi (outbox recorded_at) tới lúc k_ref phản ánh; cảnh báo p99 > 5 giây.
//
// Module khác chỉ import kref/port (R-02). Tệp này là gốc lắp ráp cho cmd/worker, cmd/drill; cài đặt ở app/.
package kref

import "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref/app"

// Kiểu và hàm của cài đặt (app/), xuất cho gốc lắp ráp.
type (
	// Module là pf/kref đã nối.
	Module = app.Module
	// Projection là nội dung k_ref của một tenant.
	Projection = app.Projection
	// ClosureRow là một dòng k_ref.node_closure.
	ClosureRow = app.ClosureRow
	// Digest là số dòng + băm của một quan hệ.
	Digest = app.Digest
	// RebuildResult tổng kết một lần dựng lại.
	RebuildResult = app.RebuildResult
)

// Tên consumer và sự kiện tiêu thụ.
const (
	Consumer               = app.Consumer
	EventOrgCreated        = app.EventOrgCreated
	EventOrgMoved          = app.EventOrgMoved
	EventOrgDeactivated    = app.EventOrgDeactivated
	EventOrgReactivated    = app.EventOrgReactivated
	EventAdminUnitChanged  = app.EventAdminUnitChanged
	EventShareGrantApprove = app.EventShareGrantApprove
	EventShareGrantRevoke  = app.EventShareGrantRevoke
)

var (
	// DBModule: mọi giao dịch của module chạy SET LOCAL ROLE r_kref (ADR-0028).
	DBModule = app.DBModule
	// Events là danh sách sự kiện của consumer pf.kref.
	Events = app.Events
	// ErrNotYetProjected: sự kiện tham chiếu Nút chưa có trong k_ref (giao lại; vẫn thiếu → DLQ, chạy dựng lại).
	ErrNotYetProjected = app.ErrNotYetProjected

	// New tạo module trên pool vai đăng nhập của đơn vị (app_worker).
	New = app.New
	// Expected đọc nguồn của một tenant và trả bản chiếu mong đợi (tính trong Go).
	Expected = app.Expected
	// Project dựng bản chiếu từ dữ liệu nguồn đã đọc.
	Project = app.Project
	// Diff trả các quan hệ mà hai bản chiếu khác nhau.
	Diff = app.Diff
	// ProjectedTenants liệt kê tenant đang có dòng trong k_ref (pool ag_ops_jobs).
	ProjectedTenants = app.ProjectedTenants
	// UnionTenants gộp kết quả của nhiều hàm tìm tenant.
	UnionTenants = app.UnionTenants
)
