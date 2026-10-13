// Package app: use case phiếu "Báo cáo cho KHTC" (W11.63 rút gọn — BRD KHTC v2.2 FN-PN-03.01, FN-PN-03.02, FN-TI-02.04,
// FN-AD-04.02; D4 KH3). Phòng chuyên môn xem phiếu được giao, nhập theo mẫu phiếu nạp sẵn (dòng = hạng mục phòng phụ trách
// trong Kế hoạch năm hiệu lực, cột "Từ Kế hoạch năm", "Lũy kế kỳ trước"), lưu nháp ở máy chủ, gửi KHTC; Phòng KHTC rà soát
// (kết quả 3 quy tắc kiểm tra), yêu cầu bổ sung hoặc xác nhận.
//
// Vòng đời phiếu dùng lại kỳ native cap/forms profile kn_khtc của Kỳ KHTC (W11.62): mỗi lần "Gửi KHTC" niêm phong bảng số
// liệu ở sổ pf_extension.khtc_form_sheet rồi nộp bài cap/forms mang mã bảng (trường bang_so_lieu) — giới hạn 3 lần gửi,
// khóa lạc quan, yêu cầu bổ sung = trả lại, xác nhận = chấp nhận (bốn mắt qua cap/workflow), Không gửi / Đã khóa khi đóng kỳ
// đều do cap/forms giữ. "Lưu nháp" là bảng loại draft của cùng sổ (không vào báo cáo).
package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcform/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcform/store"
	pdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/domain"
	pstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcperiod/store"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors/pf-extension-khtc-form.md).
var (
	ErrFormNotFound  = problem.New(http.StatusNotFound, "extension.khtc_form_not_found", "Không có phiếu của phòng trong kỳ KHTC")
	ErrFormLocked    = problem.New(http.StatusConflict, "extension.khtc_form_locked", "Phiếu đã gửi hoặc kỳ đã đóng — không sửa được")
	ErrFormInvalid   = problem.New(http.StatusUnprocessableEntity, "extension.khtc_form_invalid", "Phiếu chưa đạt quy tắc kiểm tra — chưa gửi được")
	ErrNotReviewable = problem.New(http.StatusConflict, "extension.khtc_form_not_reviewable", "Phiếu không ở trạng thái rà soát được")
	ErrOldTemplate   = problem.New(http.StatusConflict, "extension.khtc_form_template_outdated",
		"Kỳ mở trước khi cài mẫu phiếu W11.63 (biểu KHTC-PHIEU chưa có trường bảng số liệu) — hủy và mở lại kỳ")
	ErrDependency = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	errNotDept    = problem.Forbidden.WithDetail("phiếu Báo cáo cho KHTC chỉ dành cho Cấp Tỉnh của phòng được giao, Phòng KHTC và Admin")
	errNotWriter  = problem.Forbidden.WithDetail("chỉ Cấp Tỉnh của phòng được giao nhập, lưu nháp, gửi phiếu")
	errNotKHTC    = problem.Forbidden.WithDetail("chỉ Cấp Tỉnh Phòng Kế hoạch – Tài chính rà soát phiếu")
)

// Service là use case phiếu KHTC.
type Service struct {
	DB    tenant.Beginner // pool của pf/extension (r_extension)
	Clock clock.Clock
	// Entry, Control: cap/forms (nộp, lịch sử, rà soát; tiến độ). Thiếu → 503.
	Entry formsport.Entry
	// Plans (W11.61): Kế hoạch năm hiệu lực tại ngày chốt — thiếu → phiếu chỉ có lời văn.
	Plans    extport.KHTCPlans
	Programs extport.KHTCPrograms // tên CT/DA (tùy chọn)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

// Vai người gọi đối với một phiếu.
type caller struct {
	p      authz.Principal
	k      authz.KNContext
	member bool // Cấp Tỉnh của phòng được giao
	writer bool // member và không chỉ xem
	khtc   bool // Phòng KHTC (Cấp Tỉnh/TW) hoặc Admin — xem mọi phiếu
	review bool // Cấp Tỉnh Phòng KHTC, không chỉ xem — rà soát
}

func (s *Service) caller(ctx context.Context, dept string) (caller, error) {
	if s.DB == nil || s.Entry == nil {
		return caller{}, ErrDependency.WithDetail("chưa nối cap/forms cho phiếu KHTC")
	}
	p, k, err := authz.RequireKN(ctx)
	if err != nil {
		return caller{}, err
	}
	c := caller{p: p, k: k}
	switch {
	case k.Role == authz.KNAdmin:
		c.khtc = true
	case k.Department == partyport.DeptKeHoachTaiChinh && (k.Role == authz.KNTinh || k.Role == authz.KNTW):
		c.khtc = true
		c.review = k.Role == authz.KNTinh && !k.ReadOnly()
	case k.Role == authz.KNTinh && dept != "" && k.Department == dept:
		c.member = true
		c.writer = !k.ReadOnly()
	default:
		return c, errNotDept
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// Đọc phiếu
// ---------------------------------------------------------------------------

// Submission là một lần gửi (lịch sử gửi).
type Submission struct {
	ID          uuid.UUID
	Attempt     int64
	SubmittedAt time.Time
	Late        bool
	Decision    string
	ReviewNote  string
	ReviewedAt  *time.Time
	SheetID     *uuid.UUID
}

// Form là phiếu của một phòng trong một kỳ KHTC (màn nhập FN-PN-03.02 và màn rà soát FN-TI-02.04).
type Form struct {
	Period         pstore.Period
	DueAt          time.Time
	Terminal       string // "" | closed | cancelled
	Department     string
	DepartmentName string
	Template       domain.Template
	Grid           bool // phiếu tuần có lưới hạng mục
	PlanVersion    *extport.KHTCPlan
	Tabs           []domain.Tab
	View           pdomain.FormView
	Overdue        bool
	Editable       bool // người gọi nhập/lưu nháp/gửi được ngay
	Submissions    []Submission
	// Content: nội dung đang hiển thị — nháp (người nhập, nháp sau lần gửi cuối) hoặc lần gửi cuối.
	Content       domain.Sheet
	ContentSource string // draft | submitted | empty
	DraftAt       *time.Time
	Result        domain.Result
	Prior         map[uuid.UUID]decimal.Decimal
	AdjPeriods    []domain.AdjPeriod
	// RequestNote: nội dung yêu cầu bổ sung mới nhất (dải yêu cầu bổ sung).
	RequestNote string
	CanReview   bool // người gọi được "Yêu cầu bổ sung"/"Xác nhận" (theo View)
	// PlanMissing: chưa có Kế hoạch năm hiệu lực tại ngày chốt — phiếu tuần chưa có dòng hạng mục.
	PlanMissing bool
}

func terminalOf(evs []pstore.Event) string {
	for _, e := range evs {
		switch e.Action {
		case pstore.ActionClosed:
			return pdomain.PeriodClosed
		case pstore.ActionCancelled:
			return pdomain.PeriodCancelled
		}
	}
	return ""
}

func dueOf(p pstore.Period, evs []pstore.Event) time.Time {
	due := p.DueAt
	for _, e := range evs {
		if e.Action == pstore.ActionDeadline && e.DueAt != nil {
			due = *e.DueAt
		}
	}
	return due
}

// periodCtx đọc kỳ + sự kiện; phòng không thuộc kỳ → ErrFormNotFound.
func (s *Service) periodCtx(ctx context.Context, c caller, id uuid.UUID, dept string) (pstore.Period, []pstore.Event, error) {
	var p *pstore.Period
	var evs map[uuid.UUID][]pstore.Event
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if p, e = pstore.PeriodByID(ctx, tx, id); e != nil || p == nil {
			return e
		}
		evs, e = pstore.Events(ctx, tx, []uuid.UUID{id})
		return e
	})
	if err != nil {
		return pstore.Period{}, nil, err
	}
	if p == nil || !slices.Contains(p.Departments, dept) {
		return pstore.Period{}, nil, ErrFormNotFound
	}
	return *p, evs[id], nil
}

func sheetRef(values map[string]string) *uuid.UUID {
	if id, err := uuid.Parse(values[domain.FieldSheet]); err == nil {
		return &id
	}
	return nil
}

// Get đọc phiếu của phòng dept trong kỳ KHTC id.
func (s *Service) Get(ctx context.Context, id uuid.UUID, dept string) (Form, error) {
	c, err := s.caller(ctx, dept)
	if err != nil {
		return Form{}, err
	}
	return s.load(ctx, c, id, dept)
}

func (s *Service) load(ctx context.Context, c caller, id uuid.UUID, dept string) (Form, error) {
	p, evs, err := s.periodCtx(ctx, c, id, dept)
	if err != nil {
		return Form{}, err
	}
	f := Form{Period: p, DueAt: dueOf(p, evs), Terminal: terminalOf(evs), Department: dept, DepartmentName: DeptName(dept),
		Template: domain.TemplateFor(dept), Grid: p.Type == pdomain.TypeWeek}
	now := s.now()
	f.Overdue = now.After(f.DueAt)
	if f.Terminal == pdomain.PeriodCancelled && c.member {
		return Form{}, ErrFormNotFound // BR-DL-18: phiếu của kỳ đã hủy gỡ khỏi module các phòng
	}
	subs, err := s.Entry.Submissions(ctx, pdomain.ProfileKHTC, p.FormsPeriodRef, dept)
	if err != nil {
		return Form{}, err
	}
	for _, sb := range subs {
		f.Submissions = append(f.Submissions, Submission{ID: sb.ID, Attempt: sb.Version, SubmittedAt: sb.SubmittedAt, Late: sb.Late,
			Decision: sb.Decision, ReviewNote: sb.ReviewNote, ReviewedAt: sb.ReviewedAt, SheetID: sheetRef(sb.Values)})
		if f.RequestNote == "" && sb.Decision == "returned" {
			f.RequestNote = sb.ReviewNote
		}
	}
	facts := pdomain.FormFacts{Overdue: f.Overdue, Terminal: f.Terminal, Max: p.MaxSubmissions}
	if len(subs) > 0 {
		facts.Versions, facts.Decision = subs[0].Version, subs[0].Decision
		if subs[0].Decision != "returned" {
			f.RequestNote = ""
		}
	}
	// Bảng: lần gửi cuối + nháp mới nhất.
	var last, draft *store.Sheet
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if len(f.Submissions) > 0 && f.Submissions[0].SheetID != nil {
			sh, e := store.ByID(ctx, tx, *f.Submissions[0].SheetID)
			if e != nil {
				return e
			}
			if sh != nil && sh.PeriodID == p.ID && sh.Department == dept && sh.Kind == store.KindSealed {
				last = sh
			}
		}
		var e error
		draft, e = store.Latest(ctx, tx, p.ID, dept, store.KindDraft)
		return e
	})
	if err != nil {
		return Form{}, err
	}
	// Nháp hiện hành: lưu sau khi thấy lần gửi cuối (BaseAttempt = số lần đã gửi).
	if draft != nil && int64(draft.BaseAttempt) != facts.Versions {
		draft = nil
	}
	facts.Draft = draft != nil
	f.View = pdomain.FormStateOf(facts)
	editableState := f.View.State == pdomain.FormNotStarted || f.View.State == pdomain.FormDraft || f.View.State == pdomain.FormLate ||
		f.View.State == pdomain.FormSupplement
	f.Editable = c.writer && editableState
	f.CanReview = c.review && (f.View.CanConfirm || f.View.CanRequestSupplement)
	switch {
	case draft != nil && (c.member || last == nil):
		f.Content, f.ContentSource, f.DraftAt = draft.Content.Normalize(), "draft", &draft.EventTime
	case last != nil:
		f.Content, f.ContentSource = last.Content.Normalize(), "submitted"
	default:
		f.Content, f.ContentSource = domain.Sheet{}.Normalize(), "empty"
	}
	if err := s.fillGrid(ctx, c, &f); err != nil {
		return Form{}, err
	}
	f.Result = s.evaluate(f, f.Content, true)
	return f, nil
}

func (s *Service) evaluate(f Form, sh domain.Sheet, submit bool) domain.Result {
	adj := map[uuid.UUID]domain.AdjPeriod{}
	for _, a := range f.AdjPeriods {
		adj[a.ID] = a
	}
	return domain.Evaluate(domain.Input{Template: f.Template, Rows: domain.Rows(f.Tabs), Grid: f.Grid, Prior: f.Prior, Periods: adj,
		PlanYear: f.Period.Year, Sheet: sh, Submit: submit})
}

// fillGrid dựng dòng phiếu (Kế hoạch năm tại ngày chốt), Lũy kế kỳ trước và kỳ được phép điều chỉnh (chỉ phiếu tuần).
func (s *Service) fillGrid(ctx context.Context, c caller, f *Form) error {
	f.Prior = map[uuid.UUID]decimal.Decimal{}
	if !f.Grid || len(f.Template.Columns) == 0 {
		f.Grid = false
		return nil
	}
	if s.Plans == nil {
		f.PlanMissing = true
	} else {
		plan, err := s.Plans.PlanAt(ctx, f.Period.Year, f.Period.Cutoff)
		switch {
		case errors.Is(err, extport.ErrKHTCPlanNotFound):
			f.PlanMissing = true
		case err != nil:
			return ErrDependency.WithDetail("Kế hoạch năm lỗi").WithCause(err)
		default:
			f.PlanVersion = &plan
			f.Tabs = domain.BuildTabs(plan, f.Department, f.Period.From, s.programNames(ctx))
		}
	}
	// Kỳ tuần đã đóng trước kỳ này: cùng năm (Lũy kế kỳ trước + điều chỉnh) và năm trước (chỉ điều chỉnh để lưu vết).
	var closed []pstore.Period
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		for _, y := range []int{f.Period.Year, f.Period.Year - 1} {
			ps, err := pstore.Periods(ctx, tx, y, 100)
			if err != nil {
				return err
			}
			idl := make([]uuid.UUID, 0, len(ps))
			for _, p := range ps {
				idl = append(idl, p.ID)
			}
			evs, err := pstore.Events(ctx, tx, idl)
			if err != nil {
				return err
			}
			for _, p := range ps {
				if p.Type == pdomain.TypeWeek && p.ID != f.Period.ID && p.Cutoff.Before(f.Period.From) && terminalOf(evs[p.ID]) == pdomain.PeriodClosed {
					closed = append(closed, p)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	slices.SortFunc(closed, func(a, b pstore.Period) int { return b.Cutoff.Compare(a.Cutoff) })
	var sheetIDs []uuid.UUID
	owner := map[uuid.UUID]uuid.UUID{} // bảng → kỳ
	for _, p := range closed {
		f.AdjPeriods = append(f.AdjPeriods, domain.AdjPeriod{ID: p.ID, Name: p.Name, Year: p.Year})
		if p.Year != f.Period.Year || !slices.Contains(p.Departments, f.Department) {
			continue
		}
		subs, err := s.Entry.Submissions(ctx, pdomain.ProfileKHTC, p.FormsPeriodRef, f.Department)
		if err != nil {
			return err
		}
		if len(subs) == 0 { // Không gửi: tính là 0 (BR-DL-05)
			continue
		}
		if id := sheetRef(subs[0].Values); id != nil {
			sheetIDs = append(sheetIDs, *id)
			owner[*id] = p.ID
		}
	}
	if len(sheetIDs) == 0 {
		return nil
	}
	var sheets map[uuid.UUID]store.Sheet
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		sheets, e = store.ByIDs(ctx, tx, sheetIDs)
		return e
	}); err != nil {
		return err
	}
	for id, sh := range sheets {
		if sh.PeriodID != owner[id] || sh.Department != f.Department || sh.Kind != store.KindSealed {
			continue
		}
		for ref, v := range domain.Contribution(sh.Content.Normalize(), f.Period.Year) {
			f.Prior[ref] = f.Prior[ref].Add(v)
		}
	}
	return nil
}

func (s *Service) programNames(ctx context.Context) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	if s.Programs == nil {
		return out
	}
	ps, err := s.Programs.Programs(ctx, extport.KHTCProgramFilter{IncludeRetired: true})
	if err != nil {
		return out
	}
	for _, p := range ps {
		out[p.Ref] = p.Name
	}
	return out
}

// DeptName là tên phòng hiển thị trên tiêu đề "Phiếu <phòng> · <kỳ>".
func DeptName(code string) string {
	switch code {
	case partyport.DeptTrongTrot:
		return "Phòng Trồng trọt"
	case partyport.DeptChanNuoiThuySan:
		return "Phòng Chăn nuôi - Thủy sản"
	case partyport.DeptThongTinTruyenThong:
		return "Phòng Thông tin tuyên truyền"
	case partyport.DeptHanhChinhTongHop:
		return "Phòng Hành chính - Tổng hợp"
	case partyport.DeptKeHoachTaiChinh:
		return "Phòng Kế hoạch - Tài chính"
	}
	return code
}

// ---------------------------------------------------------------------------
// Lưu nháp, gửi
// ---------------------------------------------------------------------------

// SaveInput là nội dung phiếu người nhập gửi lên (Lưu nháp / Gửi KHTC).
type SaveInput struct {
	Sheet domain.Sheet
	// BaseAttempt: số lần gửi người nhập đã thấy khi mở phiếu (khóa lạc quan).
	BaseAttempt int64
}

func (s *Service) editable(ctx context.Context, id uuid.UUID, dept string) (caller, Form, error) {
	c, err := s.caller(ctx, dept)
	if err != nil {
		return c, Form{}, err
	}
	if !c.writer {
		return c, Form{}, errNotWriter
	}
	f, err := s.load(ctx, c, id, dept)
	if err != nil {
		return c, Form{}, err
	}
	if !f.Editable {
		msg := "phiếu \"" + pdomain.FormLabels[f.View.State] + "\""
		switch f.View.State {
		case pdomain.FormSubmitted:
			msg += " — chỉ sửa, gửi lại khi KHTC yêu cầu bổ sung (BR-DL-11)"
		case pdomain.FormLocked, pdomain.FormNotSent, pdomain.FormConfirmed:
			msg += " — sai số liệu điều chỉnh ở phiếu kỳ kế tiếp (BR-DL-12)"
		}
		return c, Form{}, ErrFormLocked.WithDetail(msg)
	}
	return c, f, nil
}

// withPeriodNames ghi năm, tên kỳ được điều chỉnh vào dòng điều chỉnh (để lưu vết, BR-DL-12).
func withPeriodNames(sh domain.Sheet, adj []domain.AdjPeriod) domain.Sheet {
	for i, a := range sh.Adjustments {
		for _, p := range adj {
			if p.ID.String() == a.PeriodID {
				sh.Adjustments[i].PeriodYear, sh.Adjustments[i].PeriodName = p.Year, p.Name
			}
		}
	}
	return sh
}

func (s *Service) record(ctx context.Context, c caller, f Form, sh domain.Sheet, kind, idem string) (*store.Sheet, error) {
	row := store.Sheet{ID: ids.New(), PeriodID: f.Period.ID, FormsPeriodRef: f.Period.FormsPeriodRef, Department: f.Department, Kind: kind,
		Year: f.Period.Year, TemplateCode: f.Template.Code, TemplateVersion: f.Template.Version, BaseAttempt: int(f.View.Attempt),
		Content: sh, ActorRef: c.p.ActorRef, EventTime: s.now()}
	if f.PlanVersion != nil {
		v := f.PlanVersion.VersionID
		row.PlanVersionRef = &v
	}
	var out *store.Sheet
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		prev, err := store.ByIdem(ctx, tx, idem)
		if err != nil || prev != nil {
			out = prev
			return err
		}
		if err := store.Insert(ctx, tx, c.p.TenantID, &row, idem); err != nil {
			return err
		}
		out = &row
		return nil
	})
	return out, err
}

func idemOr(idem string) string {
	if idem == "" {
		return ids.New().String()
	}
	return idem
}

// SaveDraft "Lưu nháp" (không giới hạn, không kiểm bắt buộc — vẫn trả kết quả kiểm tra để báo tại ô).
func (s *Service) SaveDraft(ctx context.Context, id uuid.UUID, dept string, in SaveInput, idem string) (Form, error) {
	c, f, err := s.editable(ctx, id, dept)
	if err != nil {
		return Form{}, err
	}
	if in.BaseAttempt != f.View.Attempt {
		return Form{}, formsport.ErrVersionConflict.WithDetail("phiếu đã được gửi ở nơi khác — tải lại phiếu")
	}
	sh := withPeriodNames(in.Sheet.Normalize(), f.AdjPeriods)
	if len(sh.Cells) > domain.MaxCells || len(sh.Adjustments) > domain.MaxAdjustments {
		return Form{}, problem.Validation.WithDetail("bảng quá lớn")
	}
	if _, err := s.record(ctx, c, f, sh, store.KindDraft, "khtc:form:draft:"+idemOr(idem)); err != nil {
		return Form{}, err
	}
	return s.load(ctx, c, id, dept)
}

func invalid(res domain.Result) error {
	p := ErrFormInvalid
	var list []domain.Issue
	for _, i := range res.Issues {
		if i.Severity != domain.SevError {
			continue
		}
		list = append(list, i)
		field := "sheet"
		switch {
		case i.Section != "":
			field = "texts." + i.Section
		case i.Adjustment >= 0:
			field = "adjustments"
		case i.EntryRef != "":
			field = "cells." + i.EntryRef + "." + i.Letter
		}
		p = p.WithField(field, i.Message)
	}
	return p.With("issues", list)
}

// Submit "Gửi KHTC": kiểm 3 quy tắc (lỗi chặn → 422 kèm danh sách), niêm phong bảng, nộp bài cap/forms lần n (khóa lạc
// quan theo số lần đã gửi người nhập thấy). Gọi lặp cùng Idempotency-Key trả phiếu đã gửi.
func (s *Service) Submit(ctx context.Context, id uuid.UUID, dept string, in SaveInput, idem string) (Form, error) {
	if idem == "" {
		return Form{}, problem.IdempotencyRequired
	}
	c, f, err := s.editable(ctx, id, dept)
	if err != nil {
		return Form{}, err
	}
	if in.BaseAttempt != f.View.Attempt {
		return Form{}, formsport.ErrVersionConflict.WithDetail("phiếu đã được gửi ở nơi khác — tải lại phiếu")
	}
	sh := withPeriodNames(in.Sheet.Normalize(), f.AdjPeriods)
	if res := s.evaluate(f, sh, true); !res.Valid() {
		return Form{}, invalid(res)
	}
	sealed, err := s.record(ctx, c, f, sh, store.KindSealed, "khtc:form:seal:"+idem)
	if err != nil {
		return Form{}, err
	}
	_, err = s.Entry.Submit(ctx, pdomain.ProfileKHTC, f.Period.FormsPeriodRef, dept, map[string]string{domain.FieldSheet: sealed.ID.String()},
		f.View.Attempt, "khtc:form:submit:"+idem)
	var pb *problem.Problem
	if errors.As(err, &pb) && pb.Code == formsport.ErrInvalidValues.Code {
		for _, fe := range pb.Errors {
			if fe.Field == domain.FieldSheet {
				return Form{}, ErrOldTemplate.WithCause(err)
			}
		}
	}
	if err != nil {
		return Form{}, err
	}
	return s.load(ctx, c, id, dept)
}

// ---------------------------------------------------------------------------
// Rà soát (FN-TI-02.04)
// ---------------------------------------------------------------------------

// Quyết định rà soát.
const (
	DecisionReturned = "returned" // Yêu cầu bổ sung, điều chỉnh (bắt buộc nội dung)
	DecisionAccepted = "accepted" // Xác nhận phiếu
)

// Review "Yêu cầu bổ sung" / "Xác nhận phiếu" — Cấp Tỉnh Phòng KHTC; dùng rà soát bốn mắt của cap/forms (người rà soát ≠
// người gửi). Gửi lần cuối cho phép (3/3) chỉ còn Xác nhận; kỳ đã đóng thì không rà soát.
func (s *Service) Review(ctx context.Context, id uuid.UUID, dept, decision, note, idem string) (Form, error) {
	c, err := s.caller(ctx, dept)
	if err != nil {
		return Form{}, err
	}
	if !c.review {
		return Form{}, errNotKHTC
	}
	f, err := s.load(ctx, c, id, dept)
	if err != nil {
		return Form{}, err
	}
	switch decision {
	case DecisionReturned:
		if !f.View.CanRequestSupplement {
			return Form{}, ErrNotReviewable.WithDetail("\"Yêu cầu bổ sung\" chỉ khi phiếu Đã gửi (chưa đủ số lần gửi tối đa) và kỳ chưa đóng")
		}
	case DecisionAccepted:
		if !f.View.CanConfirm {
			return Form{}, ErrNotReviewable.WithDetail("\"Xác nhận\" chỉ khi phiếu Đã gửi và kỳ chưa đóng")
		}
	default:
		return Form{}, problem.Validation.WithField("decision", "returned | accepted")
	}
	if len(f.Submissions) == 0 {
		return Form{}, ErrNotReviewable
	}
	if _, err := s.Entry.Review(ctx, pdomain.ProfileKHTC, f.Submissions[0].ID, decision, note, "khtc:form:review:"+idemOr(idem)); err != nil {
		return Form{}, err
	}
	return s.load(ctx, c, id, dept)
}

// ---------------------------------------------------------------------------
// Phiếu kỳ này (FN-PN-03.01)
// ---------------------------------------------------------------------------

// Task là một phiếu trong danh sách "Phiếu kỳ này / Phiếu đã gửi" của phòng.
type Task struct {
	Period      pstore.Period
	DueAt       time.Time
	View        pdomain.FormView
	Overdue     bool
	RequestNote string
	LastAt      *time.Time
	Late        bool
}

// Tasks liệt kê phiếu KHTC giao cho phòng dept (kỳ mới trước; kỳ đã hủy bị gỡ — BR-DL-18). Cấp Tỉnh của phòng xem phòng
// mình; Phòng KHTC, Admin xem phòng bất kỳ.
func (s *Service) Tasks(ctx context.Context, dept string, limit int) ([]Task, error) {
	c, err := s.caller(ctx, dept)
	if err != nil {
		return nil, err
	}
	if dept == "" {
		return nil, problem.Validation.WithField("department", "bắt buộc")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var ps []pstore.Period
	var evs map[uuid.UUID][]pstore.Event
	var drafts map[uuid.UUID]map[string]int
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		all, e := pstore.Periods(ctx, tx, 0, 200)
		if e != nil {
			return e
		}
		for _, p := range all {
			if slices.Contains(p.Departments, dept) && len(ps) < limit {
				ps = append(ps, p)
			}
		}
		idl := make([]uuid.UUID, 0, len(ps))
		for _, p := range ps {
			idl = append(idl, p.ID)
		}
		if evs, e = pstore.Events(ctx, tx, idl); e != nil {
			return e
		}
		drafts = map[uuid.UUID]map[string]int{}
		for _, p := range ps {
			d, e := store.Latest(ctx, tx, p.ID, dept, store.KindDraft)
			if e != nil {
				return e
			}
			if d != nil {
				drafts[p.ID] = map[string]int{dept: d.BaseAttempt}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]Task, 0, len(ps))
	for _, p := range ps {
		term := terminalOf(evs[p.ID])
		if term == pdomain.PeriodCancelled {
			continue
		}
		t := Task{Period: p, DueAt: dueOf(p, evs[p.ID])}
		t.Overdue = now.After(t.DueAt)
		prog, err := s.progress(ctx, p.FormsPeriodRef, dept)
		if err != nil {
			return nil, err
		}
		facts := pdomain.FormFacts{Overdue: t.Overdue, Terminal: term, Max: p.MaxSubmissions}
		if prog != nil {
			at := prog.SubmittedAt
			facts.Versions, facts.Decision = prog.Version, prog.Decision
			t.LastAt, t.Late = &at, at.After(t.DueAt)
			if prog.Decision == "returned" {
				t.RequestNote = prog.ReviewNote
			}
		}
		if b, ok := drafts[p.ID][dept]; ok && int64(b) == facts.Versions {
			facts.Draft = true
		}
		t.View = pdomain.FormStateOf(facts)
		out = append(out, t)
	}
	return out, nil
}

// progress đọc tiến độ của đơn vị dept trong kỳ native (lần gửi cuối, rà soát) — nil nếu chưa gửi.
func (s *Service) progress(ctx context.Context, formsPeriod uuid.UUID, dept string) (*formsport.SubmissionRef, error) {
	subs, err := s.Entry.Submissions(ctx, pdomain.ProfileKHTC, formsPeriod, dept)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, nil
	}
	return &subs[0], nil
}
