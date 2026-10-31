package port

import (
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

var (
	ErrAppraisalNotFound         = problem.New(http.StatusNotFound, "hrm.appraisal_not_found", "Không có đối tượng đánh giá trong phạm vi được cấp")
	ErrAppraisalConflict         = problem.New(http.StatusConflict, "hrm.appraisal_conflict", "Phiên bản hoặc trạng thái đánh giá đã thay đổi")
	ErrAppraisalBlocked          = problem.New(http.StatusUnprocessableEntity, "hrm.appraisal_blocked", "Đánh giá chưa đủ điều kiện thực hiện thao tác")
	ErrAppraisalGuestUnavailable = problem.New(http.StatusServiceUnavailable, "hrm.appraisal_guest_unavailable", "Liên kết khách có OTP chưa sẵn sàng")
	ErrAppraisalUnavailable      = problem.New(http.StatusServiceUnavailable, "hrm.appraisal_unavailable", "Phụ thuộc đánh giá nhân sự chưa sẵn sàng")
)
