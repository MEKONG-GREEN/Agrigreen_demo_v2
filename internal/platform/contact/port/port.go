// Package port là hợp đồng của pf/contact (`cap/directory`, W11.23 — BRD HCTH v3.0 FN-13 Contact) cho module khác và gốc
// lắp ráp (R-02: module khác chỉ import gói này).
package port

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Access là cấu hình quyền của bên dùng (KN: cmd/api-platform/assembly/contact.go) — không so tên ứng dụng.
//
// FN-13 B3, BR-03, §VI.4: Cấp TW, Cấp Xã chỉ thấy họ tên, năm sinh, SĐT (hệ thống KHÔNG trả trường khác, không xem chi
// tiết, không xuất); Cấp Tỉnh, Admin thấy thêm chức danh, đơn vị, cấp, email; Cấp Tỉnh – HCTH cập nhật liên hệ ngoài tỉnh;
// Admin quản lý toàn bộ.
type Access struct {
	// FullBy, BasicBy: vai trong ngữ cảnh phiên KN (authz.KNContext.Role) xem đủ trường / chỉ ba trường.
	FullBy  []string
	BasicBy []string
	// ManageBy: vai ngữ cảnh ghi ở MỌI phòng ban (Admin). ManageDeptBy + ManageDepartment: vai ghi khi ngữ cảnh ở phòng
	// ban chủ (Cấp Tỉnh – HCTH). Ngữ cảnh chỉ xem (ReadOnly) không ghi.
	ManageBy         []string
	ManageDeptBy     []string
	ManageDepartment string
	// RealmFull, RealmBasic, RealmManage: vai realm khi request KHÔNG có ngữ cảnh KN (ứng dụng khác — Q-072 dùng chung).
	RealmFull   []string
	RealmBasic  []string
	RealmManage []string
}

// Staff là nguồn cán bộ thuộc tỉnh (KN: pf/hrm port.Directory qua gốc lắp ráp). Trả ErrStaffDisabled khi nguồn tắt
// (AG_HRM_RECORDS) — Contact khi đó chỉ có liên hệ ngoài tỉnh.
type Staff interface {
	Staff(ctx context.Context) ([]StaffEntry, error)
}

// StaffEntry là một cán bộ thuộc tỉnh — KHÔNG PII (họ tên, SĐT, năm sinh, email đọc ở kho qua SubjectRef).
type StaffEntry struct {
	SubjectRef uuid.UUID
	AccountRef uuid.UUID // Nil nếu chưa gắn tài khoản
	Level      string    // LevelTinh (viên chức, hợp đồng) | LevelXa (cán bộ khuyến nông cấp xã)
	Active     bool      // còn công tác (FN-08: chưa nghỉ việc)
	Unit       string    // mã đơn vị công tác / đơn vị quản lý (cán bộ xã)
	Commune    string    // mã xã của cán bộ xã
	Title      string    // chức danh hiển thị (tên chức vụ)
}

// ErrStaffDisabled: nguồn cán bộ thuộc tỉnh chưa bật cho dữ liệu thật (pf/hrm AG_HRM_RECORDS — Q-005).
var ErrStaffDisabled = problem.New(http.StatusForbidden, "contact.staff_disabled", "Nguồn cán bộ thuộc tỉnh chưa bật")

// Labels tra tên hiển thị cho mã đơn vị nội bộ và mã xã (gốc lắp ráp nối; nil → hiện mã).
type Labels interface {
	UnitNames(ctx context.Context) (map[string]string, error)
	CommuneNames(ctx context.Context, codes []string) (map[string]string, error)
}

// Cấp của liên hệ (FN-13 B3 "Cấp").
const (
	LevelTW   = "tw"
	LevelTinh = "tinh"
	LevelXa   = "xa"
	LevelKhac = "khac"
)

// Lỗi của module (docs/errors/hcth-contact.md).
var (
	ErrNotFound        = problem.New(http.StatusNotFound, "contact.not_found", "Không có liên hệ này trong danh bạ")
	ErrDetailForbidden = problem.New(http.StatusForbidden, "contact.detail_forbidden",
		"Vai trò này chỉ xem họ tên, năm sinh, số điện thoại trong danh sách (FN-13 BR-03)")
	ErrManageForbidden = problem.New(http.StatusForbidden, "contact.manage_forbidden",
		"Chỉ Cấp Tỉnh – Phòng Hành chính – Tổng hợp và Admin cập nhật liên hệ ngoài tỉnh")
	ErrStaffReadOnly = problem.New(http.StatusUnprocessableEntity, "contact.staff_read_only",
		"Liên hệ của cán bộ thuộc tỉnh đồng bộ từ hồ sơ nhân sự — sửa tại hồ sơ (FN-07)")
	ErrDuplicatePerson = problem.New(http.StatusConflict, "contact.duplicate_person",
		"Số điện thoại đã thuộc một người trong danh bạ (mỗi người một bản ghi — FN-13 BR-01)")
	ErrVaultUnavailable = problem.New(http.StatusServiceUnavailable, "contact.vault_unavailable",
		"Kho định danh chưa hỗ trợ danh bạ")
)

// Names tra họ tên cán bộ theo tài khoản (actor_ref) từ Contact — cùng chữ ký party/port.ActorNames. Thay nguồn tên dạng
// rõ pf_extension.staff.display_name (Q-072). Tài khoản không có hồ sơ nhân sự không có trong kết quả.
type Names interface {
	DisplayNames(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error)
}
