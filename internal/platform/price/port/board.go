package port

// Duyệt / mở duyệt bảng giá ngày có phiên bản (W11.76, pf 000151/000152; BRD Báo giá nông sản v1.3 FN-TI-02.05, 02.06,
// Mục II.3.3, II.4 CT-04). Bảng giá đã duyệt là "chuỗi giá có hiệu lực" công bố của cap/price (v4.1 §26 Series /
// Publish): module khác đọc qua Boards, KHTC đọc view pf_price_pub.v1_board_daily / v1_ticket_week, website (W11.81)
// nghe pf.price.board.published.

import (
	"context"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// BoardSettings là cấu hình duyệt bảng giá của bên dùng (Profile.Board).
type BoardSettings struct {
	// ReopenDays: chỉ mở duyệt ngày báo giá trong n ngày gần nhất (BR-06 FN-TI-02.06; 0 → 30 — Q-086 tạm).
	ReopenDays int
	// AdminApproves: vai quản trị (ManageBy) được duyệt / mở duyệt — chỉ bật ở giai đoạn thử nghiệm (BR-03 FN-TI-02.05,
	// AC-04: Admin không thấy nút "Duyệt bảng giá" khi vận hành chính thức). Mặc định false.
	AdminApproves bool
}

// Week là tuần báo cáo chứa một ngày theo lịch kỳ hệ thống (W11.03).
type Week struct {
	Key  string // vd "2026-W45"
	Year int
	From time.Time
	To   time.Time
}

// Weeks (tùy chọn, gốc lắp ráp — pf/extension lịch kỳ hệ thống) trả tuần báo cáo chứa ngày day cho tenant của ctx.
// Không nối → quy tắc mặc định QĐ-6 (tuần Thứ Tư – Thứ Ba, số tuần theo năm).
type Weeks interface {
	WeekOf(ctx context.Context, day time.Time) (Week, error)
}

// BoardLine là một mặt hàng của bảng giá đã duyệt (CT-04, mức toàn tỉnh).
type BoardLine struct {
	ItemCode  string
	ItemName  string
	GroupCode string
	UnitCode  string
	Min, Max  decimal.Decimal
	Avg       decimal.Decimal // trung bình cộng P các xã
	Points    int             // số xã có giá
}

// PublishedBoard là phiên bản đã duyệt hiện hành của bảng giá một ngày.
type PublishedBoard struct {
	Day        time.Time
	Version    int
	ApprovedAt time.Time
	Reopened   bool // đang mở duyệt — số có thể đổi ở phiên bản sau ("Đang cập nhật", BR-04 FN-TI-02.06)
	WeekKey    string
	Lines      []BoardLine
}

// Boards là bảng giá đã duyệt của tenant người gọi (tenant từ ctx) — Bảng giá tổng hợp, dashboard, báo cáo (W11.77).
type Boards interface {
	// Published trả phiên bản đã duyệt hiện hành của ngày day; ok = false nếu ngày chưa duyệt lần nào.
	Published(ctx context.Context, profile string, day time.Time) (b PublishedBoard, ok bool, err error)
}

// EventBoardPublished: bảng giá ngày vừa duyệt (phiên bản mới) — website (W11.81), thông báo.
const EventBoardPublished = "pf.price.board.published"

// BoardPublishedV1 là payload của pf.price.board.published: một phiên bản bảng giá đã duyệt (FN-TI-02.05 BR-06 — kích hoạt
// đăng website Đợt 2, cập nhật Bảng giá tổng hợp / Dashboard / Báo cáo). Bên nhận đọc dòng giá qua Boards hoặc view
// pf_price_pub.v1_board_daily. Không dữ liệu cá nhân.
//
//ag:event pf.price.board.published v1
type BoardPublishedV1 struct {
	Profile    string `json:"profile"`
	BoardDate  string `json:"board_date"`
	Version    int    `json:"version"`
	ApprovedAt string `json:"approved_at"`
	WeekKey    string `json:"week_key"`
	WeekYear   int    `json:"week_year"`
	Items      int    `json:"items"`
	Tickets    int    `json:"tickets"`
	Reapproval bool   `json:"reapproval"` // duyệt lại sau mở duyệt (version ≥ 2)
}

// Lỗi bảng giá ngày (docs/errors.md, mục pf/price).
var (
	ErrBoardBusy = problem.New(http.StatusConflict, "price.board_busy",
		"Bảng giá của ngày đang được cán bộ khác duyệt / mở duyệt — không thao tác trùng")
	ErrBoardState = problem.New(http.StatusConflict, "price.board_state",
		"Trạng thái bảng giá không cho phép thao tác này")
	ErrBoardStale = problem.New(http.StatusConflict, "price.board_stale",
		"Bảng giá đã được người khác cập nhật — tải lại rồi thao tác lại")
	ErrBoardNotLocked = problem.New(http.StatusConflict, "price.board_not_locked",
		"Chưa qua giờ khóa nhập của ngày báo giá — chưa duyệt được")
	ErrBoardOpenAlerts = problem.New(http.StatusConflict, "price.board_open_alerts",
		"Còn cảnh báo biến động chưa xử lý — xử lý hết trước khi duyệt")
	ErrBoardEmpty = problem.New(http.StatusUnprocessableEntity, "price.board_empty",
		"Ngày không có dòng giá nào để công bố — không duyệt được")
	ErrReopenExpired = problem.New(http.StatusUnprocessableEntity, "price.reopen_expired",
		"Ngày báo giá quá giới hạn mở duyệt")
	ErrBoardChanged = problem.New(http.StatusConflict, "price.board_changed",
		"Số liệu đã được sửa sau khi mở duyệt — duyệt lại thay vì hủy mở duyệt")
)
