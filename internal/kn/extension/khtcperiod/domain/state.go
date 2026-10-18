package domain

// Ba máy trạng thái của Kỳ KHTC (BRD KHTC v2.2 Mục II.5): kỳ báo cáo (5.1), phiếu số liệu của phòng (5.2), bản báo
// cáo của KHTC (5.3). Trạng thái kỳ và phiếu là PHÉP CHIẾU từ sổ (sự kiện cuối của kỳ KHTC + tiến độ kỳ native
// cap/forms + trạng thái nguồn); chỉ mở kỳ, sửa hạn, đóng kỳ, hủy kỳ là sự kiện ghi sổ.

import (
	"errors"
	"slices"
	"time"
)

// ---------------------------------------------------------------------------
// 5.1 Kỳ báo cáo
// ---------------------------------------------------------------------------

// Trạng thái kỳ báo cáo.
const (
	PeriodDraft      = "draft"      // Nháp — đã khai báo ở popup, chưa giao phiếu (xem trước, không lưu; "Xóa" = đóng popup)
	PeriodOpened     = "opened"     // Đã mở — phiếu đã sinh, chưa phiếu nào gửi, chưa quá hạn nộp
	PeriodCollecting = "collecting" // Đang thu thập — có phiếu đã gửi, còn thiếu phiếu/nguồn, chưa quá hạn
	PeriodLate       = "late"       // Trễ hạn — quá hạn nộp, còn thiếu; phòng vẫn gửi được
	PeriodComplete   = "complete"   // Đủ dữ liệu — mọi phiếu đã gửi, mọi nguồn module đã chốt
	PeriodClosed     = "closed"     // Đã đóng — kết thúc, không mở lại
	PeriodCancelled  = "cancelled"  // Đã hủy — kết thúc
)

// PeriodStates theo thứ tự hiển thị.
var PeriodStates = []string{PeriodDraft, PeriodOpened, PeriodCollecting, PeriodLate, PeriodComplete, PeriodClosed, PeriodCancelled}

// PeriodLabels là nhãn tiếng Việt của trạng thái kỳ.
var PeriodLabels = map[string]string{
	PeriodDraft: "Nháp", PeriodOpened: "Đã mở", PeriodCollecting: "Đang thu thập", PeriodLate: "Trễ hạn",
	PeriodComplete: "Đủ dữ liệu", PeriodClosed: "Đã đóng", PeriodCancelled: "Đã hủy",
}

// Facts là dữ kiện để chiếu trạng thái kỳ đang mở.
type Facts struct {
	Forms         int  // số phiếu (phòng tham gia)
	FormsSent     int  // phiếu đang ở "Đã gửi" hoặc "Đã xác nhận" (Yêu cầu bổ sung KHÔNG tính đủ)
	AnySent       bool // đã có ít nhất một lần gửi
	Sources       int  // số kỳ nguồn module đã gắn
	SourcesClosed int  // số kỳ nguồn đã chốt
	Overdue       bool // đã quá hạn nộp
}

// Complete: mọi phiếu đã gửi và mọi nguồn module đã chốt.
func (f Facts) Complete() bool {
	return f.Forms > 0 && f.FormsSent == f.Forms && f.SourcesClosed >= f.Sources
}

// PeriodStateOf chiếu trạng thái kỳ: terminal = sự kiện kết thúc đã ghi (PeriodClosed, PeriodCancelled hoặc rỗng).
// Đủ dữ liệu ưu tiên Trễ hạn ("phần còn thiếu đã đủ" → Đủ dữ liệu); Đủ dữ liệu quay lại Đang thu thập/Trễ hạn khi
// dữ liệu không còn đủ (yêu cầu bổ sung, nguồn mở chốt lại) — phép chiếu tự xử lý.
func PeriodStateOf(terminal string, f Facts) string {
	switch {
	case terminal == PeriodClosed || terminal == PeriodCancelled:
		return terminal
	case f.Complete():
		return PeriodComplete
	case f.Overdue:
		return PeriodLate
	case !f.AnySent:
		return PeriodOpened
	}
	return PeriodCollecting
}

// Lỗi chuyển trạng thái kỳ (app đổi sang problem).
var (
	ErrNotClosable    = errors.New("kỳ chỉ đóng được khi Đủ dữ liệu, hoặc Trễ hạn đã quá hạn nộp từ N ngày (kèm lý do)")
	ErrReasonRequired = errors.New("lý do bắt buộc, 10–500 ký tự")
	ErrNotCancellable = errors.New("chỉ hủy được kỳ Đã mở, Đang thu thập, Trễ hạn, Đủ dữ liệu")
	ErrHasFinal       = errors.New("báo cáo của kỳ đã có Bản cuối — không hủy được kỳ")
	ErrTerminal       = errors.New("kỳ đã đóng hoặc đã hủy")
)

// CloseDecision là kết quả kiểm "Đóng kỳ".
type CloseDecision struct {
	Forced bool   // đóng ngoại lệ khi còn thiếu dữ liệu (BR-DL-15)
	Reason string // lý do đã chuẩn hóa (bắt buộc khi Forced)
}

// CanClose kiểm "Đóng kỳ" (BR-DL-15): Đủ dữ liệu → đóng (lý do tùy chọn); Trễ hạn đã quá hạn nộp từ lateDays ngày trở
// lên → đóng ngoại lệ, bắt buộc lý do; trạng thái khác → từ chối.
func CanClose(state string, due, now time.Time, lateDays int, reason string) (CloseDecision, error) {
	why, ok := ValidReason(reason)
	switch state {
	case PeriodComplete:
		if !ok {
			why = ""
		}
		return CloseDecision{Reason: why}, nil
	case PeriodLate:
		if now.Before(due.Add(time.Duration(lateDays) * 24 * time.Hour)) {
			return CloseDecision{}, ErrNotClosable
		}
		if !ok {
			return CloseDecision{}, ErrReasonRequired
		}
		return CloseDecision{Forced: true, Reason: why}, nil
	case PeriodClosed, PeriodCancelled:
		return CloseDecision{}, ErrTerminal
	}
	return CloseDecision{}, ErrNotClosable
}

// CanCancel kiểm "Hủy kỳ" (BR-DL-18): kỳ Đã mở, Đang thu thập, Trễ hạn, Đủ dữ liệu; báo cáo chưa có Bản cuối; lý do bắt
// buộc. Trả lý do đã chuẩn hóa.
func CanCancel(state string, hasFinal bool, reason string) (string, error) {
	if state == PeriodClosed || state == PeriodCancelled {
		return "", ErrTerminal
	}
	if !slices.Contains([]string{PeriodOpened, PeriodCollecting, PeriodLate, PeriodComplete}, state) {
		return "", ErrNotCancellable
	}
	if hasFinal {
		return "", ErrHasFinal
	}
	why, ok := ValidReason(reason)
	if !ok {
		return "", ErrReasonRequired
	}
	return why, nil
}

// CanChangeDeadline: "Sửa hạn nộp" chỉ với kỳ chưa kết thúc.
func CanChangeDeadline(state string) error {
	if state == PeriodClosed || state == PeriodCancelled {
		return ErrTerminal
	}
	return nil
}

// ---------------------------------------------------------------------------
// 5.2 Phiếu số liệu (trong module phòng)
// ---------------------------------------------------------------------------

// Trạng thái phiếu.
const (
	FormNotStarted = "not_started"   // Chưa nhập
	FormDraft      = "draft"         // Lưu nháp — nháp phiếu ở máy chủ (W11.63, sổ pf_extension.khtc_form_sheet)
	FormLate       = "late"          // Trễ hạn — quá hạn, chưa gửi lần nào; vẫn gửi được
	FormSubmitted  = "submitted"     // Đã gửi (lần n)
	FormSupplement = "supplement"    // Yêu cầu bổ sung — số liệu lần n vẫn dùng cho báo cáo đến khi gửi lại
	FormConfirmed  = "confirmed"     // Đã xác nhận — kết thúc
	FormLocked     = "locked"        // Đã khóa — kỳ đã đóng — kết thúc
	FormNotSent    = "not_sent"      // Không gửi — kỳ đóng mà chưa gửi lần nào — kết thúc
	FormWithdrawn  = "withdrawn"     // Đã gỡ — kỳ đã hủy (BR-DL-18: phiếu bị gỡ khỏi module các phòng)
	FormUnknown    = "not_available" // không đọc được tiến độ kỳ native
)

// FormLabels là nhãn tiếng Việt của trạng thái phiếu.
var FormLabels = map[string]string{
	FormNotStarted: "Chưa nhập", FormDraft: "Lưu nháp", FormLate: "Trễ hạn", FormSubmitted: "Đã gửi", FormSupplement: "Yêu cầu bổ sung",
	FormConfirmed: "Đã xác nhận", FormLocked: "Đã khóa", FormNotSent: "Không gửi", FormWithdrawn: "Đã gỡ", FormUnknown: "Không đọc được",
}

// FormFacts là dữ kiện của một phiếu.
type FormFacts struct {
	Versions int64  // số lần đã gửi
	Decision string // rà soát của lần gửi cuối: "" | accepted | returned
	Overdue  bool   // quá hạn nộp của kỳ KHTC
	Terminal string // sự kiện kết thúc của kỳ: "" | PeriodClosed | PeriodCancelled
	Max      int    // số lần gửi tối đa (0 = không giới hạn)
	Draft    bool   // W11.63: phòng đã "Lưu nháp" ở máy chủ sau lần gửi cuối (chưa gửi lần nào → Lưu nháp)
}

// FormView là trạng thái phiếu kèm thao tác KHTC được làm.
type FormView struct {
	State   string
	Attempt int64 // lần gửi hiện hành (0 = chưa gửi)
	// LimitReached: đã gửi đủ số lần tối đa — lần gửi thứ Max tự khóa: không "Yêu cầu bổ sung" nữa (Q-081, D12).
	LimitReached         bool
	CanRequestSupplement bool
	CanConfirm           bool
	CountsAsSent         bool // tính vào "mọi phiếu đã gửi" của kỳ
}

// FormStateOf chiếu trạng thái phiếu (Mục II.5.2).
func FormStateOf(f FormFacts) FormView {
	v := FormView{Attempt: f.Versions, LimitReached: f.Max > 0 && f.Versions >= int64(f.Max)}
	switch {
	case f.Terminal == PeriodCancelled:
		v.State = FormWithdrawn
	case f.Terminal == PeriodClosed:
		switch {
		case f.Versions == 0:
			v.State = FormNotSent
		case f.Decision == "accepted":
			v.State = FormConfirmed
		default: // Đã gửi, Yêu cầu bổ sung (giữ số liệu lần n) → Đã khóa
			v.State = FormLocked
		}
	case f.Versions == 0 && f.Overdue:
		v.State = FormLate
	case f.Versions == 0 && f.Draft:
		v.State = FormDraft
	case f.Versions == 0:
		v.State = FormNotStarted
	case f.Decision == "accepted":
		v.State = FormConfirmed
	case f.Decision == "returned":
		v.State = FormSupplement
	default:
		v.State = FormSubmitted
	}
	v.CanConfirm = v.State == FormSubmitted
	v.CanRequestSupplement = v.State == FormSubmitted && !v.LimitReached
	v.CountsAsSent = v.State == FormSubmitted || v.State == FormConfirmed
	return v
}

// ---------------------------------------------------------------------------
// 5.3 Bản báo cáo (màn soạn — lưu trữ thuộc KH6; ở đây là bảng chuyển trạng thái để KH6 dùng lại)
// ---------------------------------------------------------------------------

// Trạng thái bản báo cáo.
const (
	ReportNotStarted    = "not_started"    // Chưa soạn
	ReportDrafting      = "drafting"       // Đang soạn
	ReportExported      = "exported"       // Đã xuất vN
	ReportSourceChanged = "source_changed" // Nguồn đã thay đổi
	ReportFinal         = "final"          // Bản cuối — kết thúc
)

// ReportLabels là nhãn tiếng Việt của trạng thái bản báo cáo.
var ReportLabels = map[string]string{
	ReportNotStarted: "Chưa soạn", ReportDrafting: "Đang soạn", ReportExported: "Đã xuất", ReportSourceChanged: "Nguồn đã thay đổi",
	ReportFinal: "Bản cuối",
}

// Sự kiện của bản báo cáo.
const (
	ReportCompose       = "compose"        // KHTC bấm "Soạn"
	ReportEdit          = "edit"           // KHTC sửa lời văn
	ReportRefresh       = "refresh"        // KHTC "Cập nhật số liệu"
	ReportExport        = "export"         // KHTC "Xuất Word"
	ReportSourceChanges = "source_changes" // phiếu gửi lại / nguồn chốt lại sau khi xuất
	ReportMarkFinal     = "mark_final"     // KHTC đánh dấu Bản cuối (BR-DL-16)
)

// ErrReportTransition: sự kiện không hợp lệ ở trạng thái hiện tại.
var ErrReportTransition = errors.New("thao tác không hợp lệ với trạng thái bản báo cáo")

// ErrProvisional: còn mục Tạm tính — chưa đánh dấu Bản cuối được (BR-DL-16).
var ErrProvisional = errors.New("bản báo cáo còn mục Tạm tính — chưa đánh dấu Bản cuối được")

// NextReport trả trạng thái sau sự kiện ev (Mục II.5.3). periodState: kỳ Đã hủy không soạn báo cáo; provisional: còn
// mục Tạm tính.
func NextReport(state, ev, periodState string, provisional bool) (string, error) {
	if periodState == PeriodCancelled || periodState == PeriodDraft {
		return state, ErrReportTransition
	}
	switch state {
	case ReportNotStarted:
		if ev == ReportCompose {
			return ReportDrafting, nil
		}
	case ReportDrafting:
		switch ev {
		case ReportEdit, ReportRefresh:
			return ReportDrafting, nil
		case ReportExport:
			return ReportExported, nil
		}
	case ReportExported:
		switch ev {
		case ReportEdit, ReportRefresh:
			return ReportDrafting, nil
		case ReportSourceChanges:
			return ReportSourceChanged, nil
		case ReportMarkFinal:
			if provisional {
				return state, ErrProvisional
			}
			return ReportFinal, nil
		}
	case ReportSourceChanged:
		if ev == ReportRefresh {
			return ReportDrafting, nil
		}
	}
	return state, ErrReportTransition
}
