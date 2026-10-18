package port

// Phiếu báo giá + xác minh (W11.75, pf 000148; BRD Báo giá v1.3 FN-XA-03.01…03, 03.05, FN-TI-02.02…04, 02.09). Duyệt /
// công bố bảng giá ngày (W11.76) đọc dòng giá đã gửi qua Tickets và nối trạng thái bảng giá vào app.TicketHooks.Board.

import (
	"context"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// PriceLine là một dòng giá tính vào bảng giá ngày: phiếu Đã gửi / Nộp muộn (không tính Đã xóa, bản nháp), có giá (không
// "Không có hàng"), không bị loại khỏi bảng giá; giá = giá hiệu chỉnh nếu có (BR-05 FN-TI-02.04).
type PriceLine struct {
	PointCode  string
	RegionCode string // nhóm vùng của xã tại ngày báo giá (BR-04 FN-AD-04.02)
	ItemCode   string
	GroupCode  string
	UnitCode   string
	Min, Max   decimal.Decimal
	Mid        decimal.Decimal // P (CT-01)
	Adjusted   bool
	Late       bool
}

// DaySummary là tình hình phiếu của một ngày báo giá (khối "Tóm tắt kỳ" FN-TI-02.05, W11.76/W11.77).
type DaySummary struct {
	Day        time.Time
	Quote      bool      // là ngày báo giá
	Locked     bool      // đã qua giờ khóa
	LockAt     time.Time // giờ khóa (UTC)
	Points     int       // số xã phải báo giá
	Submitted  int       // đã gửi đúng hạn
	Late       int       // nộp muộn
	Missed     int       // không nộp (sau giờ khóa) / chưa gửi (trước giờ khóa)
	Lines      []PriceLine
	Excluded   int // số dòng đã loại
	OpenAlerts int // cảnh báo chưa xử lý (BR-03 FN-TI-02.03: phải 0 trước khi duyệt)
}

// Tickets là phiếu báo giá của tenant người gọi (tenant từ ctx) cho bảng giá ngày (W11.76) và tiến độ (W11.77).
type Tickets interface {
	Day(ctx context.Context, profile string, day time.Time) (DaySummary, error)
}

// TablePDF (tùy chọn, pf/reports qua gốc lắp ráp) dựng PDF khổ A4 có phông tiếng Việt từ một bảng chữ — "Tải PDF" phiếu
// báo giá (FN-XA-03.02). Không nối → 503 price.pdf_unavailable.
type TablePDF interface {
	RenderTablePDF(title string, meta [][2]string, header []string, rows [][]string, at time.Time) ([]byte, error)
}

// Lỗi phiếu báo giá (docs/errors.md, mục pf/price).
var (
	ErrTicketNotFound = problem.New(http.StatusNotFound, "price.ticket_not_found", "Không có phiếu báo giá này")
	ErrTicketStale    = problem.New(http.StatusConflict, "price.ticket_stale",
		"Phiếu đã được người khác cập nhật — tải lại phiếu rồi thao tác lại")
	ErrTicketLocked = problem.New(http.StatusConflict, "price.ticket_locked",
		"Đã qua giờ khóa nhập của ngày báo giá — Cấp Xã chỉ xem và phản hồi xác minh")
	ErrNotQuoteDay = problem.New(http.StatusUnprocessableEntity, "price.not_quote_day",
		"Không phải ngày báo giá đang mở nhập (chỉ nhập trong ngày báo giá, trước giờ khóa)")
	ErrTicketIncomplete = problem.New(http.StatusUnprocessableEntity, "price.ticket_incomplete",
		"Phiếu chưa đủ mặt hàng hoặc thiếu ghi chú biến động bắt buộc — lưu nháp được, chưa gửi được")
	ErrPriceInvalid = problem.New(http.StatusUnprocessableEntity, "price.price_invalid",
		"Giá không hợp lệ (số dương, thấp nhất ≤ cao nhất, tối đa 2 số lẻ)")
	ErrTicketNotLocked = problem.New(http.StatusConflict, "price.ticket_not_locked",
		"Chưa qua giờ khóa của ngày báo giá — Cấp Tỉnh chưa sửa, nhập thay, xóa được")
	ErrTicketExists = problem.New(http.StatusConflict, "price.ticket_exists",
		"Xã đã có phiếu cho ngày báo giá này (không tính phiếu Đã xóa)")
	ErrBoardApproved = problem.New(http.StatusConflict, "price.board_approved",
		"Bảng giá của ngày đã duyệt — mở duyệt trước khi sửa")
	ErrAlertNotFound = problem.New(http.StatusNotFound, "price.alert_not_found", "Không có cảnh báo này")
	ErrAlertState    = problem.New(http.StatusConflict, "price.alert_state",
		"Trạng thái cảnh báo không cho phép thao tác này")
	ErrRespondOverdue = problem.New(http.StatusConflict, "price.respond_overdue", "Quá hạn phản hồi xác minh")
	ErrReasonRequired = problem.New(http.StatusUnprocessableEntity, "price.reason_required",
		"Thiếu lý do / ghi chú / nội dung bắt buộc")
	ErrPointNotHandled = problem.New(http.StatusForbidden, "price.point_not_handled",
		"Cán bộ Cấp Tỉnh chỉ xử lý xã được phân công hoặc xã chưa phân công")
	ErrPDFUnavailable = problem.New(http.StatusServiceUnavailable, "price.pdf_unavailable",
		"Chưa nối bộ dựng PDF — dùng Excel hoặc in từ trình duyệt")
)
