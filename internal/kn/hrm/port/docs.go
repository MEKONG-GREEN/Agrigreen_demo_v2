package port

import (
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// --- W11.24: kho tài liệu nhân sự (FN-14). ---

// Bộ ba chủ thể của tệp tài liệu ở pf/evidence (R-DB-07): tệp gắn THƯ MỤC lúc tải lên — pf/hrm phân giải và kiểm quyền
// xem thư mục (gốc lắp ráp nối Resolver cho pf/evidence).
const (
	SubjectModule     = "pf/hrm"
	SubjectTypeFolder = "hr_folder"
)

// Lỗi kho tài liệu (docs/errors/pf-hrm.md).
var (
	ErrFolderNotFound  = problem.New(http.StatusNotFound, "hrm.folder_not_found", "Không có thư mục tài liệu này")
	ErrFolderExists    = problem.New(http.StatusConflict, "hrm.folder_exists", "Đã có thư mục cùng tên ở cùng cấp")
	ErrFolderNotEmpty  = problem.New(http.StatusConflict, "hrm.folder_not_empty", "Thư mục còn thư mục con hoặc tài liệu đang hiện")
	ErrDocNotFound     = problem.New(http.StatusNotFound, "hrm.doc_not_found", "Không có tài liệu này")
	ErrDocFormat       = problem.New(http.StatusUnprocessableEntity, "hrm.doc_format", "Định dạng tệp không được phép")
	ErrDocTooLarge     = problem.New(http.StatusRequestEntityTooLarge, "hrm.doc_too_large", "Tệp vượt dung lượng cho phép")
	ErrDocsUnavailable = problem.New(http.StatusServiceUnavailable, "hrm.docs_unavailable", "Kho tài liệu chưa nối kho tệp hoặc kho định danh")
)
