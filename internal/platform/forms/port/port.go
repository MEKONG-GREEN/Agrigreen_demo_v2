// Package port là giao diện công khai DUY NHẤT của pf/forms (`cap/forms`, kiến trúc v4.1 §26) mà module khác được
// import (R-02): cấu hình bên dùng (Profile), lối ghi song song chốt/mở lại kỳ kèm ảnh chụp đóng băng (Freezer),
// sự kiện pf.forms.period.closed và mã lỗi.
//
// Bên dùng cấu hình module (kiến trúc v4.1 §7.2: `cap/forms: {hierarchy, publish_by, close_by}`) bằng Profile; gốc
// lắp ráp truyền danh sách Profile khi dựng module (KN: KNProfile của cmd/api-platform/assembly).
package port

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Profile là cấu hình của MỘT bên dùng (ứng dụng) — tương đương mục `cap/forms: {…}` của app.yaml (§7.2).
type Profile struct {
	// Name: mã bên dùng ([a-z][a-z0-9_]*), vd "kn". Biểu, kỳ, ảnh chụp gắn profile.
	Name string
	// Hierarchy: các cấp từ trên xuống, vd [TW, TINH, XA]. Cấp cuối (lá) là cấp nộp bài.
	Hierarchy []string
	// Roles: vai → cấp. Vai là vai ngữ cảnh phiên KN (authz.KNContext.Role) hoặc vai realm.
	Roles map[string]string
	// PublishBy: cấp được phát hành kỳ (KN: TINH). CloseBy: cấp được chốt/mở lại kỳ (KN: TINH).
	PublishBy string
	CloseBy   string
	// ReviewBy: cấp rà soát bài nộp (rỗng = PublishBy). Người rà soát ≠ người nộp (bốn mắt, cap/workflow).
	ReviewBy string
	// DefineBy: vai được định nghĩa phiên bản biểu (ngữ cảnh KN hoặc realm).
	DefineBy []string
	// AllowFixtures: cho phát hành kỳ trên biểu mẫu thử (fixture). Profile production để false — biểu thật
	// (KN: 14 biểu của Bộ, câu hỏi Q-01) phải được định nghĩa trước khi phát hành.
	AllowFixtures bool
	// RequireReviewedBeforeClose: validator trước chốt — chặn chốt khi còn bài nộp chưa rà soát.
	RequireReviewedBeforeClose bool
	// UnitKinds: loại đơn vị lá được phát hành kỳ (W11.05, D4 X4) — UnitCommune, UnitFacility, UnitHousehold,
	// UnitDepartment; phần tử đầu là mặc định. Rỗng = [UnitCommune] (hành vi trước W11.05). UnitPerson chưa hỗ trợ.
	UnitKinds []string
	// Departments: phòng ban KN được dùng profile (rỗng = mọi phòng ban). Admin KN luôn qua (định nghĩa biểu).
	Departments []string
	// MaxSubmissions: số lần gửi tối đa mặc định của một đơn vị trong kỳ (0 = không giới hạn); kỳ đặt riêng được khi
	// phát hành. Lần gửi thứ MaxSubmissions khóa đơn vị (KHTC FN-TI-02.03, Q-081).
	MaxSubmissions int
}

// Loại đơn vị lá (W11.05). Đơn vị = người và luồng nhiều bước (HCTH, D3 §2.5) CHỪA CHỖ: hằng số có, cấu hình và
// sổ (pf 000142 CHECK unit_kind, review_flow) chưa nhận — thêm khi HCTH chọn phương án (a) "mở rộng cap/forms".
const (
	UnitCommune    = "commune"    // xã (KN Trồng trọt, CNTS phần lớn biểu)
	UnitFacility   = "facility"   // cơ sở giết mổ (CNTS BM-01) — cha = mã xã
	UnitHousehold  = "household"  // hộ mô hình / điểm trình diễn — cha = mã xã
	UnitDepartment = "department" // phòng ban (KHTC: phiếu "Báo cáo cho KHTC") — mã phòng ban
	UnitPerson     = "person"     // CHỪA CHỖ (HCTH: người được đánh giá) — chưa hỗ trợ
)

// Luồng rà soát: hiện chỉ một bước bốn mắt. ReviewFlowSteps (tự đánh giá → quản lý → HCTH → tập thể) CHỪA CHỖ.
const (
	ReviewFlowSingle = "single"
	ReviewFlowSteps  = "steps" // CHỪA CHỖ — chưa hỗ trợ
)

// Kinds trả loại đơn vị lá hiệu lực (mặc định [UnitCommune]).
func (p Profile) Kinds() []string {
	if len(p.UnitKinds) == 0 {
		return []string{UnitCommune}
	}
	return p.UnitKinds
}

// LevelOf trả cấp của vai role ở phòng ban department: khóa "vai@phòng_ban" của Roles được ưu tiên (KHTC: kn_tinh
// của Phòng KHTC là cấp phát hành, kn_tinh phòng khác là cấp lá), rồi tới khóa "vai".
func (p Profile) LevelOf(role, department string) string {
	if department != "" {
		if l, ok := p.Roles[role+"@"+department]; ok {
			return l
		}
	}
	return p.Roles[role]
}

// Reviewer trả cấp rà soát hiệu lực.
func (p Profile) Reviewer() string {
	if p.ReviewBy != "" {
		return p.ReviewBy
	}
	return p.PublishBy
}

// Leaf trả cấp lá (cấp nộp bài).
func (p Profile) Leaf() string {
	if len(p.Hierarchy) == 0 {
		return ""
	}
	return p.Hierarchy[len(p.Hierarchy)-1]
}

// Hành động của sự kiện kỳ ghi song song.
const (
	ActionClosed    = "closed"
	ActionReopened  = "reopened"
	ActionCancelled = "cancelled" // hủy kỳ (W11.05): lý do bắt buộc, không ảnh chụp
)

// Cách chốt.
const (
	ModeManual = "manual"
	ModeAuto   = "auto"
)

// MirrorInput là một lần chốt/mở lại kỳ của bên dùng có vòng đời riêng (KN extension/reportcycle).
type MirrorInput struct {
	Profile   string
	PeriodRef uuid.UUID // mã kỳ của bên dùng
	Label     string    // nhãn kỳ (≤ 200 ký tự)
	Action    string    // ActionClosed | ActionReopened | ActionCancelled
	Mode      string    // ModeManual | ModeAuto
	Units     []string  // reopened: phạm vi mở lại (rỗng = cả kỳ)
	Reason    string    // reopened, cancelled: bắt buộc 10–500 ký tự
	// Content: nội dung ảnh chụp đóng băng (bắt buộc với closed) — object JSON; DB tính băm SHA-256.
	Content json.RawMessage
	Actor   uuid.UUID
	At      time.Time
	IdemKey string // khóa idempotent của bên dùng (gọi lặp → Replayed)
}

// MirrorResult là sự kiện (và ảnh chụp, nếu chốt) đã ghi.
type MirrorResult struct {
	EventID     uuid.UUID
	Seq         int64
	SnapshotID  *uuid.UUID
	SnapshotNo  int64
	ContentHash []byte
	Replayed    bool
}

// Freezer ghi chốt/mở lại kỳ của bên dùng vào sổ pf_forms TRONG GIAO DỊCH CỦA BÊN DÙNG: ctx phải mang giao dịch
// đang mở (db.JoinTx), đã đặt tenant; thiếu → ErrNoCallerTx. Vai của bên dùng chỉ cần EXECUTE pf_forms.mirror
// (pf 000122 cấp cho r_extension). Chốt kỳ và ảnh chụp cùng commit với thay đổi của bên dùng (R-21).
type Freezer interface {
	Record(ctx context.Context, in MirrorInput) (MirrorResult, error)
}

// EventProducerName là tên module phát sự kiện.
const EventProducerName = "pf/forms"

// EventPeriodClosed: kỳ đã chốt, ảnh chụp đóng băng đã ghi (native và mirror).
const EventPeriodClosed = "pf.forms.period.closed"

// PeriodClosedV1 là payload của pf.forms.period.closed.
//
//ag:event pf.forms.period.closed v1
type PeriodClosedV1 struct {
	PeriodRef   string `json:"period_ref"`
	Profile     string `json:"profile"`
	Origin      string `json:"origin"`
	Mode        string `json:"mode"`
	SnapshotID  string `json:"snapshot_id"`
	SnapshotNo  int64  `json:"snapshot_no"`
	ContentHash string `json:"content_hash"`
	ActorRef    string `json:"actor_ref"`
	EventTime   string `json:"event_time"`
}

// Mã lỗi của module (docs/errors.md, mục pf/forms). Bốn mắt trả problem.SelfApproval.
var (
	// ErrNoCallerTx: gọi Freezer mà ctx không mang giao dịch của bên gọi (lỗi lắp ráp/lập trình).
	ErrNoCallerTx        = errors.New("forms: ctx không mang giao dịch của bên gọi (db.JoinTx)")
	ErrProfileNotFound   = problem.New(http.StatusNotFound, "forms.profile_not_found", "Không có cấu hình biểu cho bên dùng này")
	ErrTemplateNotFound  = problem.New(http.StatusNotFound, "forms.template_not_found", "Không tìm thấy phiên bản biểu")
	ErrPeriodNotFound    = problem.New(http.StatusNotFound, "forms.period_not_found", "Không tìm thấy kỳ")
	ErrSubmissionMissing = problem.New(http.StatusNotFound, "forms.submission_not_found", "Không tìm thấy bài nộp")
	ErrSnapshotNotFound  = problem.New(http.StatusNotFound, "forms.snapshot_not_found", "Không tìm thấy ảnh chụp")
	ErrFixtureTemplate   = problem.New(http.StatusConflict, "forms.fixture_template",
		"Biểu mẫu thử không được phát hành cho bên dùng này (chờ biểu thật — Q-01)")
	ErrPeriodClosed     = problem.New(http.StatusConflict, "forms.period_closed", "Kỳ đã chốt — mở lại kỳ trước khi nộp hoặc chốt lại")
	ErrPeriodNotClosed  = problem.New(http.StatusConflict, "forms.period_not_closed", "Kỳ chưa chốt")
	ErrUnitNotInPeriod  = problem.New(http.StatusUnprocessableEntity, "forms.unit_not_in_period", "Đơn vị không thuộc kỳ hoặc ngoài phạm vi mở lại")
	ErrSubmissionLocked = problem.New(http.StatusConflict, "forms.submission_accepted", "Bài nộp đã được chấp nhận — mở lại kỳ để nộp lại")
	ErrSuperseded       = problem.New(http.StatusConflict, "forms.submission_superseded", "Đã có phiên bản bài nộp mới hơn")
	ErrAlreadyReviewed  = problem.New(http.StatusConflict, "forms.already_reviewed", "Phiên bản bài nộp đã được rà soát")
	ErrPendingReview    = problem.New(http.StatusConflict, "forms.pending_review", "Còn bài nộp chưa rà soát — không chốt được kỳ")
	ErrInvalidValues    = problem.New(http.StatusUnprocessableEntity, "forms.values_invalid", "Số liệu không khớp định nghĩa biểu")
	ErrInvalidTemplate  = problem.New(http.StatusUnprocessableEntity, "forms.template_invalid", "Định nghĩa biểu không hợp lệ")
	ErrPeriodCancelled  = problem.New(http.StatusConflict, "forms.period_cancelled", "Kỳ đã hủy — không nộp, chốt hay mở lại được")
	ErrSubmissionLimit  = problem.New(http.StatusConflict, "forms.submission_limit", "Đơn vị đã gửi đủ số lần tối đa của kỳ")
)
