package port

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"net/http"
)

// Weekly report errors are documented in docs/errors/pf-price.md.
var (
	ErrWeeklyNotFound      = problem.New(http.StatusNotFound, "price.weekly_not_found", "Không có chương trình hoặc báo cáo tuần này")
	ErrWeeklyClosed        = problem.New(http.StatusConflict, "price.weekly_closed", "Báo cáo đã chốt — chỉ xem, không thay đổi số liệu hoặc hạng mục")
	ErrWeeklyStale         = problem.New(http.StatusConflict, "price.weekly_stale", "Báo cáo đã được cập nhật — tải lại số liệu mới trước khi tiếp tục")
	ErrWeeklyConfirm       = problem.New(http.StatusConflict, "price.weekly_confirmation", "Cần xác nhận thao tác ảnh hưởng báo cáo tuần")
	ErrWeeklyImportChanged = problem.New(http.StatusConflict, "price.weekly_import_changed", "Tệp khác lần xem trước — xem trước lại trước khi ghi nhận")
)
