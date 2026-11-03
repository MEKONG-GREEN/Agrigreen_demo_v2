package port

// Cổng quản trị danh mục phòng ban (FN-AD-04.03) cho chủ bề mặt HTTP (KNSPLIT-2 #118). pf/party giữ dữ liệu, sổ
// pf_party.department_event, luật đơn vị/trạng thái/vai và đếm sử dụng; binding HTTP, DTO và đăng ký route do module chủ
// (internal/kn/extension/departments) nối qua cổng này — party không còn đăng ký /v1/departments, /v1/departments/{id}*.
// Tenant, người gọi và vai lấy từ context (chuỗi xác thực → phiên hiện hành → Idempotency-Key của gốc lắp ráp); mọi
// thao tác tự kiểm vai, người gọi không suy quyền từ header hay realm.

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ErrDepartmentMissing là lỗi HTTP 404 party.department_not_found (cùng giá trị với lỗi của use case phòng ban): id
// không có trong tenant người gọi hoặc không phải UUID.
var ErrDepartmentMissing = problem.New(http.StatusNotFound, "party.department_not_found", "Không tìm thấy phòng ban")

// DepartmentAdmin là cổng quản trị phòng ban. Thao tác ghi và đọc Admin chỉ cho platform_admin/registry_admin;
// ListDepartments với admin = false cho mọi người đã xác thực (bước 2 đăng nhập, đơn vị mặc định theo Nút người gọi).
type DepartmentAdmin interface {
	ListDepartments(ctx context.Context, unit uuid.UUID, includeRetired, admin bool) ([]DepartmentView, uuid.UUID, error)
	CreateDepartment(ctx context.Context, in DepartmentInput, idem string) (Department, bool, error)
	GetDepartment(ctx context.Context, id uuid.UUID) (DepartmentView, error)
	UpdateDepartment(ctx context.Context, id uuid.UUID, in DepartmentInput) (Department, error)
	ChangeDepartmentStatus(ctx context.Context, id uuid.UUID, status, reason, idem string) (DepartmentStatusResult, error)
	RetireDepartment(ctx context.Context, id uuid.UUID, reason, idem string) (Department, bool, error)
	DepartmentLog(ctx context.Context, id uuid.UUID, after int64, limit int) ([]ChangeLogEntry, error)
}

// DepartmentInput là dữ liệu tạo/sửa phòng ban.
type DepartmentInput struct {
	OrgRef      uuid.UUID
	Code        string
	Name        string
	Description string
	SortOrder   int
	Status      string // tạo: rỗng = not_open
	MenuProfile string
}

// DepartmentView là phòng ban kèm số tài khoản/bản ghi (nil khi chưa nối nguồn đếm hoặc ở Application API).
type DepartmentView struct {
	Department
	Usage *Usage
}

// DepartmentStatusResult là kết quả đổi trạng thái kèm cảnh báo (FN-AD-04.03: ngưng phòng ban có tài khoản/dữ liệu →
// cảnh báo, dữ liệu cũ giữ nguyên).
type DepartmentStatusResult struct {
	Department Department
	Replayed   bool
	Usage      *Usage
	Warnings   []string
}

// ChangeLogEntry là một dòng khung Log của màn Detail (người, thời điểm, cũ → mới, lý do).
type ChangeLogEntry struct {
	Seq          int64
	Event        string
	ActorRef     uuid.UUID
	ActorDisplay string
	At           time.Time // event_time
	RecordedAt   time.Time
	Changes      []FieldChange
	Reason       *string
}

// FieldChange là một dòng "giá trị cũ → mới" của Log.
type FieldChange struct {
	Field string  `json:"field"`
	Old   *string `json:"old"`
	New   *string `json:"new"`
}

// DepartmentStatusLabel là nhãn tiếng Việt của trạng thái phòng ban.
func DepartmentStatusLabel(s string) string {
	switch s {
	case DeptActive:
		return "Đang áp dụng"
	case DeptNotOpen:
		return "Chưa mở"
	case DeptSuspended:
		return "Ngưng hoạt động"
	}
	return s
}
