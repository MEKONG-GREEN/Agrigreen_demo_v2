package port

// Báo cáo giá (W11.77; BRD Báo giá nông sản v1.3 FN-TW-01.01…03, FN-TI-02.01, FN-TI-02.08): đọc bảng giá đã duyệt qua
// Boards (Published, PublishedBetween — dòng toàn tỉnh và giá theo xã của từng phiên bản), phiếu qua Tickets, lịch qua
// Calendar. Khóa tháng thống kê phiếu (Q-085): chỉ Quản trị nghiệp vụ báo giá (D11).

import (
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Lỗi báo cáo giá (docs/errors/pf-price.md).
var (
	ErrReportEmpty = problem.New(http.StatusUnprocessableEntity, "price.report_empty",
		"Không có dữ liệu phù hợp với bộ lọc để xuất")
	ErrStatMonthOpen = problem.New(http.StatusConflict, "price.stat_month_open",
		"Tháng chưa kết thúc (còn ngày báo giá chưa qua giờ khóa) — chưa khóa được thống kê")
	ErrStatMonthState = problem.New(http.StatusConflict, "price.stat_month_state",
		"Trạng thái khóa tháng không cho phép thao tác này")
)
