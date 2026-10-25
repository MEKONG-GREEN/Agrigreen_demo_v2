// Package app: use case Kỳ KHTC (W11.62 — BRD KHTC v2.2 FN-TI-02.03, D4 KH2). Phòng KHTC (Cấp Tỉnh, phòng ban
// ke_hoach_tai_chinh) mở kỳ bằng ô chọn, hệ thống sinh MỘT phiếu cho mỗi phòng tham gia = một kỳ native cap/forms profile
// kn_khtc (đơn vị lá phòng ban, số lần gửi tối đa theo tham số — Q-081), gắn kỳ nguồn của module các phòng (bộ quy đổi
// của gói PFPUB), theo dõi tiến độ phiếu + nguồn, sửa hạn, đóng kỳ, hủy kỳ (3 máy trạng thái — khtcperiod/domain).
//
// Vì sao kỳ native cap/forms (không làm máy kỳ riêng): W11.05 đã có sẵn profile kn_khtc với đơn vị lá = phòng ban,
// giới hạn lần gửi khóa ở DB (trg_submission_limit), rà soát bốn mắt (yêu cầu bổ sung = trả lại, xác nhận = chấp
// nhận), Không nộp / Đã khóa lúc chốt, hủy kỳ là trạng thái cuối, ảnh chụp đóng băng. Kỳ KHTC chỉ thêm phần riêng của
// KHTC (tên kỳ, khóa chống trùng, ngày chốt, hạn xuất, kỳ nguồn, đóng ngoại lệ) ở sổ pf_extension (pf 20261013120542).
package app

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors.md, mục pf/extension — Kỳ KHTC).
var (
	ErrNotFound       = problem.New(http.StatusNotFound, "extension.khtc_period_not_found", "Không tìm thấy kỳ KHTC")
	ErrDuplicate      = problem.New(http.StatusConflict, "extension.khtc_period_duplicate", "Kỳ cùng loại, cùng năm, cùng số tuần/tháng/quý đã mở (chưa hủy)")
	ErrNotClosable    = problem.New(http.StatusConflict, "extension.khtc_period_not_closable", "Kỳ chưa đóng được")
	ErrNotCancellable = problem.New(http.StatusConflict, "extension.khtc_period_not_cancellable", "Kỳ không hủy được")
	ErrTerminal       = problem.New(http.StatusConflict, "extension.khtc_period_terminal", "Kỳ đã đóng hoặc đã hủy")
	ErrCancelling     = problem.New(http.StatusConflict, "extension.khtc_period_cancelling", "Kỳ đang được hủy — thử lại sau")
	ErrDependency     = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	errNotKHTC        = problem.Forbidden.WithDetail("Kỳ KHTC chỉ dành cho Phòng Kế hoạch – Tài chính (Cấp Tỉnh) và Admin xem")
	errNotKHTCWriter  = problem.Forbidden.WithDetail("chỉ Cấp Tỉnh Phòng Kế hoạch – Tài chính mở, sửa hạn, đóng, hủy kỳ")
)

// Tham số (pf/masterdata, giá trị số có hiệu lực tại ngày mở/đóng kỳ). Vắng → mặc định.
const (
	// ParamMaxSubmissions: số lần gửi tối đa của một phiếu (Q-081, D12 — mặc định = profile kn_khtc, 3; 0 = không giới hạn).
	ParamMaxSubmissions = "extension.khtc_max_submissions"
	// ParamCloseLateDays: N ngày sau hạn nộp KHTC được đóng kỳ Trễ hạn khi còn thiếu dữ liệu (BR-DL-15, mặc định 3).
	ParamCloseLateDays = "extension.khtc_close_late_days"
)

// DefaultCloseLateDays là N mặc định của BR-DL-15.
const DefaultCloseLateDays = 3

var deptRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// CancelGuard chặn Bản cuối và đóng kỳ trong lúc hủy kỳ bằng ý định hủy bền (khtcreport; P0-POOL-SHARED-2, #143 — thay
// GuardCancel giữ giao dịch khóa quanh lời gọi cap/forms). BeginCancel: domain.ErrHasFinal / domain.ErrCancelling.
type CancelGuard interface {
	BeginCancel(ctx context.Context, id uuid.UUID) (uuid.UUID, error)
	FinishCancel(ctx context.Context, id, token uuid.UUID, fn func(pgx.Tx) error) error
	AbortCancel(ctx context.Context, id, token uuid.UUID) error
}

// Service là use case Kỳ KHTC.
type Service struct {
	DB     tenant.Beginner // pool của pf/extension (db.ForModule — r_extension)
	Clock  clock.Clock
	Params params.Params // tùy chọn
	// Forms, Control: cap/forms (phát hành kỳ native; tiến độ, chốt, hủy). Thiếu → 503.
	Forms   formsport.Publisher
	Control formsport.PeriodControl
	// Departments: phòng ban đang áp dụng của đơn vị (mặc định phòng tham gia, tên phòng). Tùy chọn.
	Departments partyport.Departments
	// Crosswalk (gói PFPUB), Plan (W11.61), Reports (KH6): tùy chọn — xem extension/port/khtc.go.
	Crosswalk extport.KHTCSourceCrosswalk
	Plan      extport.KHTCPlanDepartments
	Reports   extport.KHTCReportStates
	// CancelGuard (khtcreport, tùy chọn): ý định hủy bền chặn Bản cuối/đóng kỳ trong lúc hủy kỳ (P0-POOL-SHARED-2).
	CancelGuard CancelGuard
	// MaxSubmissions: số lần gửi tối đa mặc định (gốc lắp ráp đặt = KNKHTCProfile.MaxSubmissions).
	MaxSubmissions int
	// Drafts (tùy chọn, W11.63 nối): phòng đã "Lưu nháp" phiếu ở máy chủ, kèm số lần gửi đã thấy khi lưu — nháp sau lần gửi
	// cuối, chưa gửi lần nào → trạng thái "Lưu nháp".
	Drafts func(ctx context.Context, periodID uuid.UUID) (map[string]int64, error)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

type caller struct {
	p authz.Principal
	k extport.SessionContext
}

// reader: Admin KN, hoặc phiên Cấp Tỉnh/TW của Phòng KHTC (kể cả chỉ xem).
func (s *Service) reader(ctx context.Context) (caller, error) {
	if s.DB == nil || s.Forms == nil || s.Control == nil {
		return caller{}, ErrDependency.WithDetail("chưa nối cap/forms cho Kỳ KHTC")
	}
	p, k, err := extport.RequireSession(ctx)
	if err != nil {
		return caller{}, err
	}
	if k.Role == extport.SessionRoleAdmin || (k.Department == partyport.DeptKeHoachTaiChinh && (k.Role == extport.SessionRoleProvince || k.Role == extport.SessionRoleNational)) {
		return caller{p: p, k: k}, nil
	}
	return caller{}, errNotKHTC
}

// writer: Cấp Tỉnh Phòng KHTC, không ở chế độ chỉ xem (cấp publish_by/close_by của profile kn_khtc).
func (s *Service) writer(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	if c.k.Role != extport.SessionRoleProvince || c.k.Department != partyport.DeptKeHoachTaiChinh || c.k.ReadOnly() {
		return c, errNotKHTCWriter
	}
	return c, nil
}

func (s *Service) paramInt(ctx context.Context, key string, at time.Time, def, lo, hi int) int {
	if s.Params == nil {
		return def
	}
	v, err := s.Params.At(ctx, key, at)
	if err != nil || !v.Value.IsInteger() {
		return def
	}
	n := int(v.Value.IntPart())
	if n < lo || n > hi {
		return def
	}
	return n
}

func (s *Service) maxSubmissions(ctx context.Context, at time.Time) int {
	return s.paramInt(ctx, ParamMaxSubmissions, at, s.MaxSubmissions, 0, 20)
}

func (s *Service) closeLateDays(ctx context.Context, at time.Time) int {
	return s.paramInt(ctx, ParamCloseLateDays, at, DefaultCloseLateDays, 0, 60)
}

// ---------------------------------------------------------------------------
// Mở kỳ
// ---------------------------------------------------------------------------

// OpenInput là lựa chọn ở popup "Mở kỳ" (B2–B4).
type OpenInput struct {
	Request     domain.Request
	Overrides   domain.Overrides
	Departments []string // rỗng = phòng phụ trách theo Kế hoạch năm (W11.61) hoặc mọi phòng đang áp dụng trừ KHTC
}

// Dept là một phòng tham gia (mã + tên).
type Dept struct{ Code, Name string }

// Draft là kỳ "Nháp" của popup (B3–B4): kỳ tự điền, phiếu giao các phòng, nguồn module — chưa ghi.
type Draft struct {
	Plan              domain.Plan
	Departments       []Dept
	Sources           []extport.KHTCSourceRef
	SourcesLinked     bool // bộ quy đổi kỳ nguồn (PFPUB) đã nối
	MaxSubmissions    int
	Duplicate         *store.Period // kỳ cùng loại + khóa đang mở (chưa hủy)
	TemplateVersion   int64         // 0 = phiếu cơ sở chưa cài ở cap/forms
	DueRequired       bool          // Mẫu 5 / phát sinh: KHTC phải nhập hạn nộp
	PlanDepartments   bool          // phòng mặc định lấy theo Kế hoạch năm (W11.61)
	DepartmentsLoaded bool          // đã đọc được danh sách phòng ban của đơn vị
}

func inputErr(err error) error {
	var ie domain.ErrInput
	if errors.As(err, &ie) {
		return problem.Validation.WithField(ie.Field, ie.Msg)
	}
	return err
}

func (s *Service) unitDepartments(ctx context.Context, k extport.SessionContext) (map[string]string, []string, bool) {
	names := map[string]string{}
	var order []string
	if s.Departments == nil || k.UnitRef == uuid.Nil {
		return names, order, false
	}
	ds, err := s.Departments.UnitDepartments(ctx, k.UnitRef)
	if err != nil {
		return names, order, false
	}
	for _, d := range ds {
		names[d.Code] = d.Name
		if d.Selectable() && d.Code != partyport.DeptKeHoachTaiChinh {
			order = append(order, d.Code)
		}
	}
	return names, order, true
}

func (s *Service) draft(ctx context.Context, c caller, in OpenInput, strict bool) (Draft, error) {
	plan, err := domain.PlanOf(rc.DefaultRule, in.Request, clock.Zone())
	if err != nil {
		return Draft{}, inputErr(err)
	}
	o := in.Overrides
	lenient := !strict && plan.DueRequired && o.DueAt == nil
	if lenient { // xem trước: giữ hạn gợi ý, màn yêu cầu nhập
		o.DueAt = &plan.DueAt
	}
	if plan, err = plan.Apply(o, clock.Zone()); err != nil {
		return Draft{}, inputErr(err)
	}
	d := Draft{Plan: plan, DueRequired: plan.DueRequired, MaxSubmissions: s.maxSubmissions(ctx, plan.Cutoff)}
	names, avail, loaded := s.unitDepartments(ctx, c.k)
	d.DepartmentsLoaded = loaded
	codes, err := s.pickDepartments(ctx, in.Departments, avail, loaded, plan, &d)
	if err != nil {
		return Draft{}, err
	}
	for _, code := range codes {
		d.Departments = append(d.Departments, Dept{Code: code, Name: names[code]})
	}
	if s.Crosswalk != nil {
		d.SourcesLinked = true
		if d.Sources, err = s.Crosswalk.Sources(ctx, plan.FormsKind, plan.FormsKey, plan.From, domain.QuantityEnd(plan.Type, plan.To, plan.Cutoff), codes); err != nil {
			return Draft{}, ErrDependency.WithDetail("bộ quy đổi kỳ nguồn lỗi").WithCause(err)
		}
	}
	if plan.Key != "" {
		err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			var e error
			d.Duplicate, e = store.LivePeriodByKey(ctx, tx, plan.Type, plan.Key)
			return e
		})
		if err != nil {
			return Draft{}, err
		}
	}
	ref, err := s.Forms.LatestTemplate(ctx, domain.ProfileKHTC, domain.TemplateCode)
	switch {
	case errors.Is(err, formsport.ErrTemplateNotFound):
	case err != nil:
		return Draft{}, err
	default:
		d.TemplateVersion = ref.Version
	}
	return d, nil
}

// pickDepartments chọn phòng tham gia: danh sách KHTC chọn (phải là phòng đang áp dụng của đơn vị, khác KHTC); rỗng →
// phòng phụ trách theo Kế hoạch năm (nếu W11.61 đã nối) giao với phòng đang áp dụng; không có Kế hoạch năm → mọi phòng
// đang áp dụng trừ KHTC.
func (s *Service) pickDepartments(ctx context.Context, req, avail []string, loaded bool, plan domain.Plan, d *Draft) ([]string, error) {
	var out []string
	if len(req) > 0 {
		for _, code := range req {
			code = strings.TrimSpace(code)
			switch {
			case !deptRe.MatchString(code) || code == partyport.DeptKeHoachTaiChinh:
				return nil, problem.Validation.WithField("departments", "mã phòng ban không hợp lệ: "+code)
			case loaded && !slices.Contains(avail, code):
				return nil, problem.Validation.WithField("departments", "phòng ban không đang áp dụng ở đơn vị: "+code)
			case !slices.Contains(out, code):
				out = append(out, code)
			}
		}
		return out, nil
	}
	if s.Plan != nil {
		pd, err := s.Plan.PlanDepartments(ctx, plan.Year, plan.Cutoff)
		if err != nil {
			return nil, ErrDependency.WithDetail("Kế hoạch năm lỗi").WithCause(err)
		}
		for _, code := range pd {
			if (!loaded || slices.Contains(avail, code)) && code != partyport.DeptKeHoachTaiChinh && !slices.Contains(out, code) {
				out = append(out, code)
			}
		}
		d.PlanDepartments = len(out) > 0
	}
	if len(out) == 0 { // chưa có Kế hoạch năm hiệu lực (hoặc chưa phân phòng phụ trách) → mọi phòng đang áp dụng trừ KHTC
		out = append(out, avail...)
	}
	if len(out) == 0 {
		return nil, problem.Validation.WithField("departments", "chọn ít nhất một phòng tham gia")
	}
	if len(out) > 50 {
		return nil, problem.Validation.WithField("departments", "tối đa 50 phòng")
	}
	return out, nil
}

// Preview dựng kỳ "Nháp" của popup (không ghi).
func (s *Service) Preview(ctx context.Context, in OpenInput) (Draft, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Draft{}, err
	}
	return s.draft(ctx, c, in, false)
}

func openIdem(idem string) string { return "khtc:open:" + idem }

// Open "Mở kỳ và giao phiếu" (B5): phát hành kỳ native cap/forms (một phiếu mỗi phòng), rồi ghi kỳ KHTC + kỳ nguồn
// trong một giao dịch pf_extension. Ghi kỳ KHTC lỗi → hủy bù kỳ native (không để phiếu mồ côi ở module các phòng).
// Gọi lặp cùng Idempotency-Key → trả kỳ đã mở.
func (s *Service) Open(ctx context.Context, in OpenInput, idem string) (Detail, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Detail{}, err
	}
	if idem == "" {
		return Detail{}, problem.IdempotencyRequired
	}
	var prev *store.Period
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.PeriodByIdem(ctx, tx, openIdem(idem))
		return e
	}); err != nil {
		return Detail{}, err
	}
	if prev != nil {
		return s.Get(ctx, prev.ID)
	}
	d, err := s.draft(ctx, c, in, true)
	if err != nil {
		return Detail{}, err
	}
	if d.Duplicate != nil {
		return Detail{}, ErrDuplicate.WithDetail("đã có kỳ \"" + d.Duplicate.Name + "\" — hủy kỳ đó trước nếu mở nhầm")
	}
	ref, err := s.Forms.LatestTemplate(ctx, domain.ProfileKHTC, domain.TemplateCode)
	if errors.Is(err, formsport.ErrTemplateNotFound) {
		return Detail{}, formsport.ErrTemplateNotFound.WithDetail("phiếu \"Báo cáo cho KHTC\" chưa cài — Admin cài phiếu cơ sở trước")
	}
	if err != nil {
		return Detail{}, err
	}
	p := d.Plan
	units := make([]string, 0, len(d.Departments))
	for _, dp := range d.Departments {
		units = append(units, dp.Code)
	}
	maxSub := d.MaxSubmissions
	pub, err := s.Forms.PublishBatch(ctx, formsport.BatchInput{Profile: domain.ProfileKHTC, TemplateID: ref.ID, UnitKind: formsport.UnitDepartment,
		Units: units, MaxSubmissions: &maxSub, Periods: []formsport.PeriodDef{{Label: p.Name, DueAt: p.DueAt, Kind: p.FormsKind, Key: p.FormsKey,
			From: p.From, To: p.To, Rule: p.Rule}}}, "khtc:"+idem)
	if err != nil {
		return Detail{}, err
	}
	if len(pub) != 1 {
		return Detail{}, problem.Internal.WithDetail("cap/forms trả số kỳ khác 1")
	}
	now := s.now()
	row := store.Period{ID: ids.New(), Type: p.Type, Year: p.Year, Name: p.Name, From: p.From, To: p.To, Cutoff: p.Cutoff, DueAt: p.DueAt,
		ExportDueAt: p.ExportDueAt, MaxSubmissions: maxSub, Departments: units, FormsPeriodRef: pub[0].ID, TemplateCode: ref.Code,
		Rule: p.Rule, ActorRef: c.p.ActorRef, EventTime: now}
	if p.Type == domain.TypeWeek || p.Type == domain.TypeMonth || p.Type == domain.TypeQuarter {
		no := p.No
		row.No = &no
	}
	if p.Scope != "" {
		sc := p.Scope
		row.Scope = &sc
	}
	if p.Key != "" {
		k := p.Key
		row.Key = &k
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.InsertPeriod(ctx, tx, c.p.TenantID, &row, openIdem(idem)); err != nil {
			return err
		}
		for i, src := range d.Sources {
			sr := store.Source{ID: ids.New(), PeriodID: row.ID, Department: src.Department, Source: src.Source, CalendarID: src.CalendarID,
				CalendarVersion: src.CalendarVersion, NativeKey: src.NativeKey}
			if src.Label != "" {
				l := src.Label
				sr.Label = &l
			}
			if err := store.InsertSource(ctx, tx, c.p.TenantID, &sr, c.p.ActorRef, now, openIdem(idem)+":src:"+strconv.Itoa(i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// Hủy bù kỳ native vừa phát hành (best effort — lỗi hủy bù không che lỗi gốc).
		_ = s.Control.CancelPeriod(ctx, domain.ProfileKHTC, pub[0].ID, "Hủy tự động: ghi kỳ KHTC không thành công", "khtc:"+idem+":compensate")
		return Detail{}, classify(err)
	}
	return s.Get(ctx, row.ID)
}

// classify đổi lỗi DB của sổ Kỳ KHTC sang mã lỗi.
func classify(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.ConstraintName {
	case "khtc_period_duplicate":
		return ErrDuplicate.WithCause(err)
	case "khtc_period_event_terminal":
		return ErrTerminal.WithCause(err)
	case "khtc_period_cancelling":
		return ErrCancelling.WithCause(err)
	}
	if pg.Code == "23505" {
		return problem.Conflict.WithCause(err)
	}
	return err
}

func zone() *time.Location { return clock.Zone() }

// Week là một tuần báo cáo KHTC (ô chọn "Tuần ▾" hiển thị kèm ngày).
type Week struct {
	No       int
	From, To time.Time
}

// Weeks trả các tuần báo cáo (Thứ Tư – Thứ Ba) của năm (52 hoặc 53 tuần).
func (s *Service) Weeks(ctx context.Context, year int) ([]Week, error) {
	if _, err := s.reader(ctx); err != nil {
		return nil, err
	}
	if year < 2000 || year > 2100 {
		return nil, problem.Validation.WithField("year", "năm 2000–2100")
	}
	n := rc.DefaultRule.WeeksInYear(year)
	out := make([]Week, 0, n)
	for no := 1; no <= n; no++ {
		w, err := rc.DefaultRule.Week(year, no)
		if err != nil {
			return nil, err
		}
		out = append(out, Week{No: no, From: w.From, To: w.To})
	}
	return out, nil
}

// ClosedCutoff là móc của Kế hoạch năm (W11.61 khtc/app.Service.ClosedCutoff, BR-DL-13): ngày chốt của kỳ KHTC đã đóng
// gần nhất của năm — phiên bản kế hoạch có ngày hiệu lực không sau ngày đó bị chặn. Tenant lấy từ người gọi; nil = chưa
// có kỳ đóng.
func (s *Service) ClosedCutoff(ctx context.Context, year int) (*time.Time, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	if s.DB == nil {
		return nil, nil
	}
	var t *time.Time
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		t, e = store.LastClosedCutoff(ctx, tx, year)
		return e
	})
	return t, err
}
