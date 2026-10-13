package port

// Sự kiện vòng đời phiếu / cảnh báo / giờ khóa / bảng giá cho thông báo (gói NOTIF — G1, quyết định D5; BRD Báo giá v1.3
// FN-TI-02.03 B1/B3/B4/B6, FN-XA-03.05, FN-TI-02.04, FN-TI-02.06; NFR "Thông báo"). pf/price chỉ phát sự việc kèm mã
// tham chiếu người liên quan (cán bộ Xã phụ trách chính, cán bộ Cấp Tỉnh được phân công của xã tại ngày báo giá);
// pf/notify chọn người nhận (thêm Quản trị nghiệp vụ, mọi Cấp Tỉnh khi xã chưa phân công), dựng chuông + email. Không
// chứa dữ liệu cá nhân (tên xã là tên ĐVHC; không nội dung yêu cầu / giải trình / lý do — người nhận đọc trong ứng dụng).

import "github.com/google/uuid"

// Sự kiện vòng đời (schema trong contracts/events/).
const (
	// EventAlertRaised: Xã gửi / gửi lại phiếu làm phát sinh cảnh báo "Tạm tính" mới (FN-TI-02.03 B1, AC-01 "trong 1 phút
	// cán bộ Tỉnh phụ trách nhận thông báo").
	EventAlertRaised = "pf.price.alert.raised"
	// EventVerificationRequested: Cấp Tỉnh gửi yêu cầu xác minh (FN-TI-02.03 B3) — một sự kiện mỗi phiếu.
	EventVerificationRequested = "pf.price.verification.requested"
	// EventVerificationResponded: cán bộ Xã phản hồi yêu cầu xác minh (FN-XA-03.05 B4).
	EventVerificationResponded = "pf.price.verification.responded"
	// EventVerificationDecided: Cấp Tỉnh chốt cảnh báo — giữ nguyên, loại, mở lại (FN-TI-02.03 B6) hoặc hiệu chỉnh
	// (FN-TI-02.04) — một sự kiện mỗi phiếu.
	EventVerificationDecided = "pf.price.verification.decided"
	// EventLockSummarized: mốc khóa nhập của ngày báo giá đã ghi (job price_calendar) kèm số liệu tổng hợp cho Cấp Tỉnh
	// (FN-TI-02.03 B4 "12h00 … gửi thông báo tổng hợp cho Cấp Tỉnh").
	EventLockSummarized = "pf.price.lock.summarized"
	// EventBoardReopened: Cấp Tỉnh mở duyệt bảng giá ngày đã duyệt (FN-TI-02.06). Bên phát: ReopenBoard của W11.76.
	EventBoardReopened = "pf.price.board.reopened"
)

// Quyết định trong EventVerificationDecided.
const (
	DecisionKept     = "kept"     // Giữ nguyên
	DecisionExcluded = "excluded" // Loại khỏi bảng giá
	DecisionReopened = "reopened" // Mở lại cảnh báo đã xử lý (bảng giá đang mở duyệt)
	DecisionAdjusted = "adjusted" // Hiệu chỉnh giá (FN-TI-02.04)
)

// AlertRaisedV1 là payload của pf.price.alert.raised: các mặt hàng của phiếu vừa sinh cảnh báo Tạm tính. owner_ref =
// cán bộ Cấp Tỉnh được phân công xã tại ngày báo giá (null = xã chưa phân công — mọi Cấp Tỉnh xử lý).
//
//ag:event pf.price.alert.raised v1
type AlertRaisedV1 struct {
	Profile   string     `json:"profile"`
	Day       string     `json:"day"`
	PointCode string     `json:"point_code"`
	PointName string     `json:"point_name"`
	TicketID  uuid.UUID  `json:"ticket_id"`
	ItemCodes []string   `json:"item_codes"`
	OwnerRef  *uuid.UUID `json:"owner_ref"`
}

// VerificationRequestedV1 là payload của pf.price.verification.requested: yêu cầu xác minh các mặt hàng của một phiếu.
// officer_ref = cán bộ Xã phụ trách chính của xã tại ngày báo giá (null = "Chưa có cán bộ Xã" — không ai nhận).
//
//ag:event pf.price.verification.requested v1
type VerificationRequestedV1 struct {
	Profile      string     `json:"profile"`
	Day          string     `json:"day"`
	PointCode    string     `json:"point_code"`
	PointName    string     `json:"point_name"`
	TicketID     uuid.UUID  `json:"ticket_id"`
	ItemCodes    []string   `json:"item_codes"`
	RespondBy    string     `json:"respond_by"`
	OfficerRef   *uuid.UUID `json:"officer_ref"`
	RequesterRef uuid.UUID  `json:"requester_ref"`
}

// VerificationRespondedV1 là payload của pf.price.verification.responded: cán bộ Xã đã phản hồi (choice = keep |
// propose). owner_ref như AlertRaisedV1.
//
//ag:event pf.price.verification.responded v1
type VerificationRespondedV1 struct {
	Profile      string     `json:"profile"`
	Day          string     `json:"day"`
	PointCode    string     `json:"point_code"`
	PointName    string     `json:"point_name"`
	TicketID     uuid.UUID  `json:"ticket_id"`
	ItemCode     string     `json:"item_code"`
	Choice       string     `json:"choice"`
	OwnerRef     *uuid.UUID `json:"owner_ref"`
	ResponderRef uuid.UUID  `json:"responder_ref"`
}

// VerificationDecidedV1 là payload của pf.price.verification.decided: kết quả xử lý cảnh báo của một phiếu.
//
//ag:event pf.price.verification.decided v1
type VerificationDecidedV1 struct {
	Profile    string     `json:"profile"`
	Day        string     `json:"day"`
	PointCode  string     `json:"point_code"`
	PointName  string     `json:"point_name"`
	TicketID   uuid.UUID  `json:"ticket_id"`
	ItemCodes  []string   `json:"item_codes"`
	Decision   string     `json:"decision"`
	OfficerRef *uuid.UUID `json:"officer_ref"`
	DeciderRef uuid.UUID  `json:"decider_ref"`
}

// LockSummarizedV1 là payload của pf.price.lock.summarized: số xã phải báo giá, đã gửi đúng hạn, nộp muộn, không nộp và
// cảnh báo chưa xử lý tại mốc khóa (mỗi (tenant, bên dùng, ngày) đúng một lần — cùng giao dịch với dòng nhật ký job).
//
//ag:event pf.price.lock.summarized v1
type LockSummarizedV1 struct {
	Profile    string `json:"profile"`
	Day        string `json:"day"`
	LockAt     string `json:"lock_at"`
	Points     int    `json:"points"`
	Submitted  int    `json:"submitted"`
	Late       int    `json:"late"`
	Missed     int    `json:"missed"`
	OpenAlerts int    `json:"open_alerts"`
}

// BoardReopenedV1 là payload của pf.price.board.reopened: bảng giá ngày board_date (phiên bản đã duyệt version) đang mở
// duyệt. Không có lý do (văn bản tự do — đọc trong ứng dụng).
//
//ag:event pf.price.board.reopened v1
type BoardReopenedV1 struct {
	Profile    string    `json:"profile"`
	BoardDate  string    `json:"board_date"`
	Version    int       `json:"version"`
	ReopenedAt string    `json:"reopened_at"`
	ActorRef   uuid.UUID `json:"actor_ref"`
}
