// Package port là giao diện công khai DUY NHẤT của pf/mrv mà module khác được
// import (R-02): tên vai, loại minh chứng, mục đích đồng ý và sự kiện.
//
// Danh mục trường bằng chứng thương mại chưa chốt (câu hỏi mở C-1). pf/mrv lưu
// danh mục như DỮ LIỆU có phiên bản và phê duyệt; mọi minh chứng ghim phiên bản
// danh mục đã duyệt lúc thu — đổi danh mục không cần migration và không làm
// thay đổi cách đọc minh chứng cũ.
package port

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// Vai nghiệp vụ (tên vai trong Keycloak).
//
// Quyết định: phê duyệt danh mục trường dùng lại hai vai chung param_setter /
// param_approver (R-56) thay vì tạo cặp vai riêng — authz.EnsureDistinctApprover
// kiểm đúng vai param_approver, và một cặp vai SoD thống nhất cho mọi dữ liệu có
// phê duyệt dễ phân quyền hơn ở ĐTK 1.
const (
	// RoleCatalogSetter đề xuất và gửi duyệt phiên bản danh mục trường (Admin API).
	RoleCatalogSetter = authz.RoleParamSetter
	// RoleCatalogApprover duyệt/từ chối; không được là người đặt (R-56).
	RoleCatalogApprover = authz.RoleParamApprover
	// RoleCollector: cán bộ xã / Tổ KNCĐ thu minh chứng trong địa bàn (KN US-MRV-01, 03).
	RoleCollector = "mrv_collector"
	// RoleReviewer: cán bộ TTKN tỉnh duyệt minh chứng (KN US-MRV-05). Không duyệt minh chứng do chính mình thu.
	RoleReviewer = "mrv_reviewer"
	// RoleViewer: chỉ xem (vd chuyên gia thẩm định — US-MRV-05 AC-2).
	RoleViewer = "mrv_viewer"
	// RoleExporter: yêu cầu xuất gói bằng chứng và tải gói.
	RoleExporter = "mrv_exporter"
)

// ConsentPurpose là mục đích đồng ý bắt buộc trước khi nhận minh chứng gắn với
// một nông hộ (pf/party ConsentChecker, mục 10.5).
const ConsentPurpose = "mrv.evidence"

// Loại minh chứng có danh mục mẫu G1 (tạm, chờ C-1). Loại mới chỉ cần một phiên
// bản danh mục được duyệt — không có danh sách đóng trong mã.
const (
	EvidenceAWDLog         = "awd_log"         // nhật ký AWD tại SPOT (US-MRV-01)
	EvidenceFieldChecklist = "field_checklist" // kiểm tra thực địa theo danh mục (US-MRV-03)
)

// SyncEntity là entity chiều lên của sync (docs/dev/sync-contract.md).
const SyncEntity = "evidence"

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventExportRequested = "pf.mrv.export.requested"
	EventExportCompleted = "pf.mrv.export.completed"
)

// ExportConsumer là tên consumer của worker xử lý yêu cầu xuất gói (shared.inbox).
const ExportConsumer = "pf.mrv.export"

// ExportRequestedV1 là payload của pf.mrv.export.requested, schema_version 1.
// Worker nạp bộ lọc đã giải phạm vi từ bảng export_job, không từ sự kiện.
//
//ag:event pf.mrv.export.requested v1
type ExportRequestedV1 struct {
	ExportID    string `json:"export_id"`
	RequestedBy string `json:"requested_by"`
	RequestedAt string `json:"requested_at"`
}

// ExportCompletedV1 là payload của pf.mrv.export.completed, schema_version 1.
//
//ag:event pf.mrv.export.completed v1
type ExportCompletedV1 struct {
	ExportID       string `json:"export_id"`
	ObjectKey      string `json:"object_key"`
	PackageSHA256  string `json:"package_sha256"`
	SnapshotSHA256 string `json:"snapshot_sha256"`
	EvidenceCount  int    `json:"evidence_count"`
	AsOf           string `json:"as_of"`
	CompletedAt    string `json:"completed_at"`
}
