package app

// W11.11 — "Báo cáo cho KHTC" phía phòng (BRD KHTC v2.2 Mục IV.3, FN-PN-03.01; D4 X5, CN11, BG10): danh sách phiếu KHTC
// giao cho MỘT phòng theo kỳ KHTC — "Phiếu kỳ này" (kỳ chưa đóng/hủy, mặc định lọc "Cần xử lý", sắp theo hạn gần nhất) và
// "Phiếu đã gửi" (phiếu đã gửi ít nhất một lần, mọi kỳ chưa hủy). Một mã dùng chung cho mọi phòng chuyên môn (Trồng trọt,
// CNTS, Thông tin tuyên truyền, HCTH) — menu mỗi phòng trỏ cùng màn, phòng lấy từ ngữ cảnh phiên.
//
// Phiếu = đơn vị lá (phòng ban) của kỳ native cap/forms kn_khtc: tiến độ đọc qua formsport.PeriodControl.Progress với ngữ
// cảnh của người gọi — cap/forms tự giới hạn cấp lá PHONG ở phòng của phiên (allowsUnit), nên phòng không đọc được phiếu
// phòng khác. Form nhập/gửi phiếu (FN-PN-03.02) thuộc W11.63: dòng chỉ mang thao tác (Nhập / Tiếp tục / Sửa, gửi lại / Xem)
// và đường dẫn ổn định tới màn đó (EntryRoute).

import (
	"context"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Góc nhìn của danh sách phiếu phòng (FN-PN-03.01).
const (
	InboxCurrent = "current" // Phiếu kỳ này — kỳ chưa đóng/hủy
	InboxSent    = "sent"    // Phiếu đã gửi — đã gửi ≥ 1 lần, mọi kỳ chưa hủy
)

// Thao tác của phòng trên một phiếu (nút ở cột cuối, FN-PN-03.01 B3).
const (
	ActionEnter    = "enter"    // Nhập — Chưa nhập, Trễ hạn
	ActionContinue = "continue" // Tiếp tục — Lưu nháp (nháp máy chủ thuộc W11.63)
	ActionResubmit = "resubmit" // Sửa, gửi lại — Yêu cầu bổ sung
	ActionView     = "view"     // Xem — Đã gửi, Đã xác nhận, Đã khóa, Không gửi
)

// ActionLabels là nhãn nút.
var ActionLabels = map[string]string{
	ActionEnter: "Nhập", ActionContinue: "Tiếp tục", ActionResubmit: "Sửa, gửi lại", ActionView: "Xem",
}

// EntryRoute là đường dẫn web-ops của màn nhập/xem phiếu (FN-PN-03.02 — gói W11.63). Phòng của phiếu đi theo ngữ cảnh
// phiên (hợp đồng đường dẫn menu: không có mã phòng trong URL), trừ Admin nhập thay — thêm ?department=.
// HỢP ĐỒNG VỚI W11.63: "/kn/khtc/phieu/{period_id}" với period_id = id kỳ KHTC (không phải id kỳ native cap/forms).
func EntryRoute(periodID uuid.UUID, department string, admin bool) string {
	r := "/kn/khtc/phieu/" + periodID.String()
	if admin {
		r += "?department=" + department
	}
	return r
}

// TypeLabels là nhãn loại báo cáo (Mục II.4).
var TypeLabels = map[string]string{
	domain.TypeWeek: "Báo cáo tuần", domain.TypeMonth: "Báo cáo tháng", domain.TypeQuarter: "Báo cáo quý", domain.TypeYear: "Báo cáo năm",
	domain.TypeProgram: "Báo cáo tiến độ các CT", domain.TypeAdhoc: "Báo cáo phát sinh",
}

// InboxQuery là tham số danh sách phiếu của phòng.
type InboxQuery struct {
	View       string // InboxCurrent (mặc định) | InboxSent
	Department string // chỉ Admin: phòng cần xem; vai khác bỏ trống hoặc = phòng của phiên
	Year       int    // năm kế hoạch, 0 = mọi năm
	Limit      int
}

// InboxRow là một phiếu của phòng trong một kỳ KHTC.
type InboxRow struct {
	Period      store.Period
	PeriodState string
	DueAt       time.Time // hạn nộp hiệu lực (sau "Sửa hạn nộp")
	Form        domain.FormView
	// NeedsAction: thuộc bộ lọc "Cần xử lý" (Chưa nhập, Lưu nháp, Trễ hạn, Yêu cầu bổ sung) và kỳ còn nhận phiếu.
	NeedsAction     bool
	Action          string
	EntryRoute      string
	LastSubmittedAt *time.Time
	ReviewedAt      *time.Time
	// SupplementNote: nội dung yêu cầu bổ sung mới nhất (chỉ khi phiếu đang "Yêu cầu bổ sung").
	SupplementNote string
	Late           bool
}

// InboxCounts là nhãn đếm trên đầu danh sách.
type InboxCounts struct {
	Total, NeedsAction, Supplement, Late, Sent int
}

// Inbox là danh sách phiếu KHTC của một phòng.
type Inbox struct {
	Department     string
	DepartmentName string
	View           string
	ReadOnly       bool // phiên chỉ xem: mọi nút là "Xem"
	Counts         InboxCounts
	Rows           []InboxRow
}

var errInboxDept = problem.Validation.WithField("department", "Admin chọn phòng cần xem")

// deptCaller: viên chức phòng chuyên môn (Cấp Tỉnh của phòng mình) hoặc Admin KN (chọn phòng). Cấp TW, Cấp Xã không có
// menu "Báo cáo cho KHTC".
func (s *Service) deptCaller(ctx context.Context, want string) (caller, string, error) {
	if s.DB == nil || s.Forms == nil || s.Control == nil {
		return caller{}, "", ErrDependency.WithDetail("chưa nối cap/forms cho Kỳ KHTC")
	}
	p, k, err := extport.RequireSession(ctx)
	if err != nil {
		return caller{}, "", err
	}
	switch k.Role {
	case extport.SessionRoleAdmin:
		if !deptRe.MatchString(want) {
			return caller{}, "", errInboxDept
		}
		return caller{p: p, k: k}, want, nil
	case extport.SessionRoleProvince:
		if k.Department == "" || (want != "" && want != k.Department) {
			return caller{}, "", problem.Forbidden.WithDetail("chỉ xem phiếu KHTC của phòng mình")
		}
		return caller{p: p, k: k}, k.Department, nil
	}
	return caller{}, "", problem.Forbidden.WithDetail("Báo cáo cho KHTC dành cho Cấp Tỉnh các phòng chuyên môn")
}

// actionOf chọn nút của phòng theo trạng thái phiếu (FN-PN-03.01 B3). Phiên chỉ xem chỉ có "Xem".
func actionOf(state string, readOnly bool) (string, bool) {
	needs := false
	act := ActionView
	switch state {
	case domain.FormNotStarted, domain.FormLate:
		act, needs = ActionEnter, true
	case domain.FormDraft:
		act, needs = ActionContinue, true
	case domain.FormSupplement:
		act, needs = ActionResubmit, true
	}
	if readOnly {
		act = ActionView
	}
	return act, needs
}

// DepartmentForms trả danh sách phiếu KHTC của phòng (FN-PN-03.01). Kỳ đã hủy bị gỡ khỏi module các phòng (BR-DL-18).
func (s *Service) DepartmentForms(ctx context.Context, q InboxQuery) (Inbox, error) {
	c, dept, err := s.deptCaller(ctx, q.Department)
	if err != nil {
		return Inbox{}, err
	}
	if q.View == "" {
		q.View = InboxCurrent
	}
	if q.View != InboxCurrent && q.View != InboxSent {
		return Inbox{}, problem.Validation.WithField("view", "current hoặc sent")
	}
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 50
	}
	var ps []store.Period
	var evs map[uuid.UUID][]store.Event
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if ps, e = store.PeriodsOfDepartment(ctx, tx, dept, q.Year, q.Limit); e != nil {
			return e
		}
		idl := make([]uuid.UUID, 0, len(ps))
		for _, p := range ps {
			idl = append(idl, p.ID)
		}
		evs, e = store.Events(ctx, tx, idl)
		return e
	})
	if err != nil {
		return Inbox{}, err
	}
	in := Inbox{Department: dept, View: q.View, ReadOnly: c.k.ReadOnly(), Rows: []InboxRow{}}
	if names, _, _ := s.unitDepartments(ctx, c.k); names[dept] != "" {
		in.DepartmentName = names[dept]
	}
	now := s.now()
	admin := c.k.Role == extport.SessionRoleAdmin
	for _, p := range ps {
		row, ok := s.inboxRow(ctx, p, evs[p.ID], dept, now, in.ReadOnly, admin)
		if !ok {
			continue
		}
		open := row.PeriodState != domain.PeriodClosed
		if (q.View == InboxCurrent && !open) || (q.View == InboxSent && row.Form.Attempt == 0) {
			continue
		}
		in.Rows = append(in.Rows, row)
		in.Counts.Total++
		if row.NeedsAction {
			in.Counts.NeedsAction++
		}
		switch row.Form.State {
		case domain.FormSupplement:
			in.Counts.Supplement++
		case domain.FormLate:
			in.Counts.Late++
		}
		if row.Form.Attempt > 0 {
			in.Counts.Sent++
		}
	}
	if q.View == InboxCurrent {
		// "Cần xử lý" trước, rồi hạn nộp gần nhất.
		slices.SortStableFunc(in.Rows, func(a, b InboxRow) int {
			if a.NeedsAction != b.NeedsAction {
				if a.NeedsAction {
					return -1
				}
				return 1
			}
			return a.DueAt.Compare(b.DueAt)
		})
	} else {
		// Phiếu đã gửi: lần gửi mới nhất trước.
		slices.SortStableFunc(in.Rows, func(a, b InboxRow) int {
			if a.LastSubmittedAt == nil || b.LastSubmittedAt == nil {
				return 0
			}
			return b.LastSubmittedAt.Compare(*a.LastSubmittedAt)
		})
	}
	return in, nil
}

// inboxRow chiếu phiếu của phòng dept trong kỳ p. false: kỳ đã hủy (phiếu bị gỡ) hoặc người gọi không đọc được phiếu.
func (s *Service) inboxRow(ctx context.Context, p store.Period, evs []store.Event, dept string, now time.Time, readOnly, admin bool) (InboxRow, bool) {
	term := terminalOf(evs)
	if term == domain.PeriodCancelled {
		return InboxRow{}, false
	}
	row := InboxRow{Period: p}
	row.DueAt, _ = deadlinesOf(p, evs)
	overdue := now.After(row.DueAt)
	prog, err := s.Control.Progress(ctx, domain.ProfileKHTC, p.FormsPeriodRef)
	if err != nil {
		return InboxRow{}, false
	}
	i := slices.IndexFunc(prog.Units, func(u formsport.UnitProgress) bool { return u.Unit == dept })
	if i < 0 {
		return InboxRow{}, false
	}
	u := prog.Units[i]
	row.LastSubmittedAt, row.ReviewedAt, row.Late = u.LastSubmittedAt, u.ReviewedAt, u.Late
	// W11.63: nháp ở máy chủ (sau lần gửi cuối, chưa gửi lần nào) → "Lưu nháp" / nút "Tiếp tục" — cùng quy tắc với my-forms.
	draft := false
	if s.Drafts != nil && term == "" && u.Versions == 0 {
		if ds, err := s.Drafts(ctx, p.ID); err == nil {
			n, ok := ds[dept]
			draft = ok && n == u.Versions
		}
	}
	row.Form = domain.FormStateOf(domain.FormFacts{Versions: u.Versions, Decision: u.Decision, Overdue: overdue, Terminal: term,
		Max: p.MaxSubmissions, Draft: draft})
	if row.Form.State == domain.FormSupplement {
		row.SupplementNote = u.ReviewNote
	}
	// Trạng thái kỳ phía phòng: chỉ cần biết kỳ còn nhận phiếu hay đã đóng (đủ dữ liệu/nguồn là việc của KHTC).
	row.PeriodState = domain.PeriodOpened
	switch {
	case term == domain.PeriodClosed:
		row.PeriodState = domain.PeriodClosed
	case overdue:
		row.PeriodState = domain.PeriodLate
	}
	row.Action, row.NeedsAction = actionOf(row.Form.State, readOnly)
	row.EntryRoute = EntryRoute(p.ID, dept, admin)
	return row, true
}
