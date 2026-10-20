package port

// Điều tra giá thành sản xuất theo vụ (W11.78, pf 20261020161354; BRD Báo giá v1.3 FN-TI-02.10…02.12, FN-TW-01.04).
// Họ tên, SĐT nông hộ và cán bộ điều tra ở kho định danh (mục đích SurveyPurpose — R-04, R-42); pf_price chỉ giữ mã chủ thể.

import (
	"net/http"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SurveyPurpose là mục đích xử lý dữ liệu cá nhân của phiếu điều tra giá thành ở kho định danh (R-42): tạo chủ thể nông
// hộ / cán bộ điều tra khi nhập tệp, đọc họ tên / SĐT cho Cấp Tỉnh rà soát (BR-03 FN-TI-02.11).
const SurveyPurpose = "price.cost_survey"

// EnvSurveyPII là biến môi trường bật nhập phiếu điều tra có dữ liệu cá nhân nông hộ ("1" bật, "0" tắt; vắng → bật ngoài
// production). Chờ DPIA (Q-005 (iv)) trước khi nhận dữ liệu thật.
const EnvSurveyPII = "AG_PRICE_SURVEY_PII"

// Lỗi điều tra giá thành (docs/errors/kn-price.md).
var (
	ErrSurveyRoundNotFound = problem.New(http.StatusNotFound, "price.survey_round_not_found", "Không có đợt điều tra này")
	ErrSurveyRoundState    = problem.New(http.StatusConflict, "price.survey_round_state",
		"Đợt điều tra không ở trạng thái cho phép thao tác này")
	ErrSurveyRoundIncomplete = problem.New(http.StatusUnprocessableEntity, "price.survey_round_incomplete",
		"Chưa chọn nhóm cây hoặc xã điều tra — không mở được đợt")
	ErrSurveyRoundDuplicate = problem.New(http.StatusConflict, "price.survey_round_duplicate",
		"Đã có đợt cùng vụ, năm, nhóm cây — xác nhận để lưu")
	ErrSurveyRoundPending = problem.New(http.StatusConflict, "price.survey_round_pending",
		"Còn phiếu chờ rà soát hoặc chờ xác minh — không chốt được đợt")
	ErrSurveyStale = problem.New(http.StatusConflict, "price.survey_stale",
		"Đợt điều tra đã được người khác cập nhật — tải lại rồi thử lại")
	ErrSurveyFormNotFound = problem.New(http.StatusNotFound, "price.survey_form_not_found", "Không có phiếu điều tra này")
	ErrSurveyFormState    = problem.New(http.StatusConflict, "price.survey_form_state",
		"Phiếu điều tra không ở trạng thái cho phép thao tác này")
	ErrSurveyImportInvalid = problem.New(http.StatusUnprocessableEntity, "price.survey_import_invalid",
		"Tệp không đọc được hoặc thiếu cột bắt buộc")
	ErrSurveyImportChanged = problem.New(http.StatusConflict, "price.survey_import_changed",
		"Tệp hoặc ánh xạ khác lần xem trước — xem trước lại rồi ghi nhận")
	ErrSurveyImportNotFound = problem.New(http.StatusNotFound, "price.survey_import_not_found", "Không có lần nhập này")
	ErrSurveyPIIDisabled    = problem.New(http.StatusServiceUnavailable, "price.survey_pii_disabled",
		"Nhập phiếu điều tra có dữ liệu cá nhân nông hộ chưa được bật (chờ DPIA) hoặc chưa nối kho định danh")
)
