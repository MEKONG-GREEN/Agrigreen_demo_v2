package app

// W11.27 (BRD HCTH v3.0 FN-11; D3 H7): KẾ HOẠCH NHÂN LỰC NĂM trên cap/forms (profile hrm_plan, đơn vị lá = đơn vị nội bộ).
//
//   - B1 HCTH mở kỳ năm, đặt hạn và ngày chốt số liệu hiện có → kỳ native cap/forms (PublishBatch) + sổ plan_period.
//   - B2 lãnh đạo đơn vị: số hiện có, dự kiến nghỉ hưu TỰ TÍNH từ hồ sơ tại ngày chốt (BR-01); nhập dự kiến nghỉ/chuyển
//     khác, cần bổ sung, nhu cầu theo vị trí; lưu nháp (sổ plan_draft) hoặc gửi (bài nộp cap/forms; gửi sau hạn = "Trễ hạn").
//   - B3 HCTH xem bảng tổng hợp đơn vị × chỉ tiêu, xác nhận hoặc yêu cầu điều chỉnh (rà soát bốn mắt cap/forms), chốt (ảnh
//     chụp đóng băng). Sau chốt chỉ điều chỉnh có lý do (BR-02 — sổ plan_adjustment), ảnh chụp chốt giữ nguyên.
//   - Luồng phụ: sao chép từ năm trước, xuất Excel bảng tổng hợp.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// PlanDeps là cổng cap/forms do gốc lắp ráp nối (W11.27).
type PlanDeps struct {
	Forms   formsport.Publisher
	Control formsport.PeriodControl
	Entry   formsport.Entry
}

// UsePlans bật kế hoạch nhân lực năm. Chưa gọi → route kế hoạch trả hrm.plan_disabled.
func (s *Service) UsePlans(d PlanDeps) {
	if d.Forms == nil || d.Control == nil || d.Entry == nil {
		return
	}
	s.Plans = &d
}

// planCaller là người gọi kế hoạch nhân lực đã giải vai.
type planCaller struct {
	p        authz.Principal
	role     string
	unit     string // lãnh đạo đơn vị: đơn vị nội bộ của phiên
	asUnit   string // đơn vị mà người gọi được lập/gửi (lãnh đạo đơn vị, kể cả lãnh đạo Phòng HCTH)
	readOnly bool
}

func (c planCaller) center() bool { return c.role == port.PlanRoleAdmin || c.role == port.PlanRoleHCTH }

func (s *Service) planCaller(ctx context.Context) (planCaller, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return planCaller{}, problem.Unauthenticated
	}
	if s.Plans == nil {
		return planCaller{}, port.ErrPlanDisabled
	}
	if p.KN == nil {
		return planCaller{}, problem.Forbidden.WithDetail("kế hoạch nhân lực: cần ngữ cảnh phiên Khuyến nông")
	}
	role, unit, ok := planRole(s.Access, p.KN, false)
	if !ok {
		return planCaller{}, problem.Forbidden.WithDetail("kế hoạch nhân lực: chỉ Phòng HCTH, Admin và lãnh đạo các đơn vị (FN-11)")
	}
	c := planCaller{p: p, role: role, unit: unit, readOnly: p.KN.ReadOnly()}
	if r, u, _ := planRole(s.Access, p.KN, true); r == port.PlanRoleManager {
		c.asUnit = u
	}
	return c, nil
}

func (c planCaller) needCenter(write bool) error {
	if !c.center() {
		return problem.Forbidden.WithDetail("kế hoạch nhân lực: chỉ Phòng HCTH và Admin mở kỳ, tổng hợp, rà soát, chốt (FN-11)")
	}
	if write && c.readOnly {
		return problem.Forbidden.WithDetail("ngữ cảnh chỉ xem không ghi được kế hoạch")
	}
	return nil
}

func (c planCaller) needUnit(unit string) error {
	if c.asUnit == "" || c.asUnit != unit {
		return problem.Forbidden.WithDetail("kế hoạch nhân lực: chỉ lãnh đạo đơn vị lập, gửi kế hoạch của đơn vị mình (FN-11)")
	}
	if c.readOnly {
		return problem.Forbidden.WithDetail("ngữ cảnh chỉ xem không ghi được kế hoạch")
	}
	return nil
}

// --- Kiểu kết quả. ---

// PlanPeriod là kỳ kế hoạch một năm.
type PlanPeriod struct {
	Year      int
	Label     string
	PeriodRef uuid.UUID
	Status    string // open | closed | reopened | cancelled (cap/forms)
	DueAt     time.Time
	CutoffOn  time.Time
	Units     []string
	Submitted int // số đơn vị đã gửi (trong phạm vi người gọi)
	OpenedAt  time.Time
}

// PlanRetiree là người dự kiến nghỉ hưu trong năm kế hoạch (họ tên chỉ khi người gọi được xem hồ sơ — FN-20).
type PlanRetiree struct {
	StaffID  uuid.UUID
	Code     string
	FullName string
	RetireOn time.Time
}

// PlanFigures là số tự tính của một đơn vị tại ngày chốt (BR-01).
type PlanFigures struct {
	Available bool // false: hồ sơ nhân sự chưa bật/không đọc được — số tự tính = 0
	Current   int
	Retiring  int
	Unknown   int // người đang công tác thiếu giới tính/ngày sinh — không tính được tuổi nghỉ hưu
	Retirees  []PlanRetiree
}

// PlanRow là kế hoạch của một đơn vị trên bảng tổng hợp.
type PlanRow struct {
	Unit        string
	UnitName    string
	Status      string // domain.PlanStatus*
	Late        bool
	Versions    int64
	Decision    string
	ReviewNote  string
	SubmittedAt *time.Time
	Values      domain.PlanValues
	Demand      int
	Source      string // auto (chưa gửi — số tự tính tại ngày chốt) | submitted | adjusted
	Adjusted    bool
}

// PlanTotals là dòng "Toàn Trung tâm" (AC-02: cộng đúng các đơn vị).
type PlanTotals struct {
	Current, Retiring, Leaving, Additional, Demand int
}

// PlanView là bảng tổng hợp đơn vị × chỉ tiêu của một năm.
type PlanView struct {
	Period    PlanPeriod
	Rows      []PlanRow
	Totals    PlanTotals
	Figures   bool // số tự tính có sẵn (hồ sơ bật)
	CanManage bool // mở/rà soát/chốt/điều chỉnh
	MyUnit    string
}

// PlanSubmission là một lần gửi của đơn vị.
type PlanSubmission struct {
	ID          uuid.UUID
	Version     int64
	Values      domain.PlanValues
	SubmittedAt time.Time
	Late        bool
	Decision    string
	ReviewNote  string
	ReviewedAt  *time.Time
}

// PlanAdjustment là một điều chỉnh sau chốt.
type PlanAdjustment struct {
	Seq        int64
	Values     domain.PlanValues
	Reason     string
	Actor      uuid.UUID
	RecordedAt time.Time
}

// PlanDraft là bản nháp mới nhất.
type PlanDraft struct {
	Values     domain.PlanInput
	Actor      uuid.UUID
	RecordedAt time.Time
}

// PlanUnitView là chi tiết kế hoạch của một đơn vị.
type PlanUnitView struct {
	Period      PlanPeriod
	Row         PlanRow
	Figures     PlanFigures
	Draft       *PlanDraft
	Submissions []PlanSubmission // mới trước
	Adjustments []PlanAdjustment // cũ trước
	CanEdit     bool
	CanReview   bool
	CanAdjust   bool
}

// --- Nạp kỳ. ---

func (s *Service) planPeriod(ctx context.Context, tid uuid.UUID, year int) (store.PlanPeriodRow, error) {
	if !domain.ValidPlanYear(year) {
		return store.PlanPeriodRow{}, problem.Validation.WithField("year", fmt.Sprintf("năm %d–%d", domain.PlanMinYear, domain.PlanMaxYear))
	}
	var row *store.PlanPeriodRow
	err := s.tx(ctx, tid, func(tx pgx.Tx) (e error) {
		row, e = store.PlanPeriodByYear(ctx, tx, year)
		return e
	})
	if err != nil {
		return store.PlanPeriodRow{}, err
	}
	if row == nil {
		return store.PlanPeriodRow{}, port.ErrPlanNotFound
	}
	return *row, nil
}

func periodOf(r store.PlanPeriodRow, pr formsport.PeriodProgress) PlanPeriod {
	n := 0
	for _, u := range pr.Units {
		if u.Versions > 0 {
			n++
		}
	}
	return PlanPeriod{Year: r.Year, Label: domain.PlanLabel(r.Year), PeriodRef: r.PeriodRef, Status: pr.Status, DueAt: r.DueAt,
		CutoffOn: r.CutoffOn, Units: r.Units, Submitted: n, OpenedAt: r.RecordedAt}
}

// ListPlans trả các kỳ kế hoạch người gọi thấy (lãnh đạo đơn vị: kỳ có đơn vị mình), năm mới trước.
func (s *Service) ListPlans(ctx context.Context) ([]PlanPeriod, error) {
	c, err := s.planCaller(ctx)
	if err != nil {
		return nil, err
	}
	var rows []store.PlanPeriodRow
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) (e error) {
		rows, e = store.PlanPeriods(ctx, tx)
		return e
	}); err != nil {
		return nil, err
	}
	out := []PlanPeriod{}
	for _, r := range rows {
		if !c.center() && !slices.Contains(r.Units, c.unit) {
			continue
		}
		pr, err := s.Plans.Control.Progress(ctx, port.PlanProfile, r.PeriodRef)
		if err != nil {
			return nil, err
		}
		out = append(out, periodOf(r, pr))
	}
	return out, nil
}

// OpenPlanInput là yêu cầu mở kỳ kế hoạch năm (B1).
type OpenPlanInput struct {
	Year     int
	DueOn    time.Time // hạn các đơn vị gửi (hết ngày, giờ VN)
	CutoffOn time.Time // ngày chốt số liệu hiện có; zero → hôm nay
	Units    []string  // đơn vị nội bộ; rỗng → mọi đơn vị đang dùng
}

// OpenPlan mở kỳ kế hoạch năm: biểu HRM_PLAN (định nghĩa lại khi đổi trường), kỳ native cap/forms (khóa kỳ năm), sổ
// plan_period. Một kỳ mỗi năm.
func (s *Service) OpenPlan(ctx context.Context, in OpenPlanInput, idem string) (PlanPeriod, error) {
	c, err := s.planCaller(ctx)
	if err != nil {
		return PlanPeriod{}, err
	}
	if err := c.needCenter(true); err != nil {
		return PlanPeriod{}, err
	}
	if !domain.ValidPlanYear(in.Year) {
		return PlanPeriod{}, problem.Validation.WithField("year", fmt.Sprintf("năm %d–%d", domain.PlanMinYear, domain.PlanMaxYear))
	}
	cutoff := s.day(in.CutoffOn)
	if in.DueOn.IsZero() {
		return PlanPeriod{}, problem.Validation.WithField("due_on", "bắt buộc")
	}
	due := time.Date(in.DueOn.Year(), in.DueOn.Month(), in.DueOn.Day(), 23, 59, 59, 0, clock.Zone()).UTC()
	if due.Before(cutoff) {
		return PlanPeriod{}, problem.Validation.WithField("due_on", "hạn gửi không trước ngày chốt số liệu")
	}
	units, err := s.planUnits(ctx, c, in.Units)
	if err != nil {
		return PlanPeriod{}, err
	}
	if _, err := s.planPeriod(ctx, c.p.TenantID, in.Year); err == nil {
		return PlanPeriod{}, port.ErrPlanExists
	} else if !errors.Is(err, port.ErrPlanNotFound) {
		return PlanPeriod{}, err
	}
	if idem == "" {
		idem = "hrm-plan:" + ids.New().String()
	}
	tpl, err := s.planTemplate(ctx, idem)
	if err != nil {
		return PlanPeriod{}, err
	}
	from, to := time.Date(in.Year, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(in.Year, 12, 31, 0, 0, 0, 0, time.UTC)
	pub, err := s.Plans.Forms.PublishBatch(ctx, formsport.BatchInput{Profile: port.PlanProfile, TemplateID: tpl.ID,
		UnitKind: formsport.UnitDepartment, Units: units, Periods: []formsport.PeriodDef{{Label: domain.PlanLabel(in.Year), DueAt: due,
			Kind: "year", Key: domain.PlanPeriodKey(in.Year), From: from, To: to, Rule: "hrm_plan"}}}, idem+":forms")
	if err != nil {
		return PlanPeriod{}, err
	}
	row := store.PlanPeriodRow{ID: ids.New(), Year: in.Year, PeriodRef: pub[0].ID, TemplateRef: tpl.ID, CutoffOn: cutoff, DueAt: due,
		Units: units, Actor: c.p.ActorRef}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockPlans(ctx, tx, c.p.TenantID); err != nil {
			return err
		}
		ex, err := store.PlanPeriodByYear(ctx, tx, in.Year)
		if err != nil {
			return err
		}
		if ex != nil {
			return port.ErrPlanExists
		}
		return store.InsertPlanPeriod(ctx, tx, c.p.TenantID, row, s.Clock.Now(), idem)
	})
	if err != nil {
		// Bù: kỳ cap/forms vừa phát hành không có sổ HCTH → hủy (lý do ghi rõ), không để kỳ mồ côi nhận bài.
		_ = s.Plans.Control.CancelPeriod(ctx, port.PlanProfile, pub[0].ID, "Mở kỳ kế hoạch nhân lực không thành — hủy kỳ bù", idem+":cancel")
		return PlanPeriod{}, classify(err)
	}
	row.RecordedAt = s.Clock.Now()
	return PlanPeriod{Year: in.Year, Label: domain.PlanLabel(in.Year), PeriodRef: row.PeriodRef, Status: "open", DueAt: due,
		CutoffOn: cutoff, Units: units, OpenedAt: row.RecordedAt}, nil
}

// planUnits kiểm đơn vị của kỳ theo danh mục đơn vị nội bộ (W11.07); rỗng → mọi đơn vị đang dùng theo thứ tự danh mục.
func (s *Service) planUnits(ctx context.Context, c planCaller, want []string) ([]string, error) {
	var cat []port.Unit
	if s.Rec != nil && s.Rec.Units != nil && c.p.KN.UnitRef != uuid.Nil {
		us, err := s.Rec.Units.InternalUnits(ctx, c.p.KN.UnitRef)
		if err != nil {
			return nil, err
		}
		cat = us
	}
	if len(want) == 0 {
		var out []string
		for _, u := range cat {
			if u.Active {
				out = append(out, u.Code)
			}
		}
		if len(out) == 0 {
			return nil, problem.Validation.WithField("units", "chưa có danh mục đơn vị nội bộ — chọn đơn vị")
		}
		return out, nil
	}
	out := make([]string, 0, len(want))
	for _, u := range want {
		u = strings.TrimSpace(u)
		if !unitCodeOK(u) || slices.Contains(out, u) {
			return nil, problem.Validation.WithField("units", "mã đơn vị nội bộ không hợp lệ hoặc trùng: "+u)
		}
		if cat != nil && !slices.ContainsFunc(cat, func(x port.Unit) bool { return x.Code == u && x.Active }) {
			return nil, problem.Validation.WithField("units", "đơn vị nội bộ không có hoặc đã ngừng: "+u)
		}
		out = append(out, u)
	}
	if len(out) > 200 {
		return nil, problem.Validation.WithField("units", "tối đa 200 đơn vị")
	}
	return out, nil
}

func unitCodeOK(u string) bool {
	if u == "" || len(u) > 40 || u[0] < 'a' || u[0] > 'z' {
		return false
	}
	for _, r := range u {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// planTemplate trả phiên bản biểu HRM_PLAN hiện hành; chưa có hoặc trường khác định nghĩa → ghi phiên bản mới.
func (s *Service) planTemplate(ctx context.Context, idem string) (formsport.TemplateRef, error) {
	want := domain.PlanFields()
	t, err := s.Plans.Forms.LatestTemplate(ctx, port.PlanProfile, domain.PlanTemplateCode)
	if err == nil {
		var got []domain.PlanFormField
		if json.Unmarshal(t.Fields, &got) == nil && reflect.DeepEqual(got, want) {
			return t, nil
		}
	} else if !errors.Is(err, formsport.ErrTemplateNotFound) {
		return t, err
	}
	raw, err := json.Marshal(want)
	if err != nil {
		return formsport.TemplateRef{}, err
	}
	return s.Plans.Forms.DefineTemplate(ctx, formsport.TemplateDef{Profile: port.PlanProfile, Code: domain.PlanTemplateCode,
		Title: domain.PlanTemplateTitle, Fields: raw}, idem+":template")
}

// --- Số tự tính (BR-01). ---

// planFigures tính số hiện có và dự kiến nghỉ hưu năm year của các đơn vị tại ngày chốt cutoff từ hồ sơ (phép chiếu
// as-of). Cán bộ xã không tính (đơn vị quản lý HS-37 không phải biên chế đơn vị). Hồ sơ chưa bật → Available = false.
// withNames: đọc họ tên người dự kiến nghỉ hưu mà người gọi được xem (ma trận FN-20).
func (s *Service) planFigures(ctx context.Context, cutoff time.Time, year int, units []string, withNames bool) (map[string]*PlanFigures, bool, error) {
	out := map[string]*PlanFigures{}
	for _, u := range units {
		out[u] = &PlanFigures{Retirees: []PlanRetiree{}}
	}
	c, err := s.recCaller(ctx)
	if err != nil {
		if errors.Is(err, port.ErrRecordsDisabled) || errors.Is(err, client.ProblemProfilesUnsupported) || forbiddenErr(err) {
			return out, false, nil
		}
		return nil, false, err
	}
	pop, err := s.loadPopulation(ctx, c, cutoff, nil)
	if err != nil {
		return nil, false, err
	}
	type cand struct {
		h    store.StaffRow
		snap domain.Snapshot
	}
	var cands []cand
	var refs []uuid.UUID
	for _, h := range pop.heads {
		if h.Type == domain.StaffCanBoXa {
			continue
		}
		snap := domain.Project(pop.events[h.StaffID], cutoff, pop.rank)
		f, ok := out[snap.Unit]
		if snap.Status != domain.StatusActive || !ok {
			continue
		}
		f.Current++
		cands = append(cands, cand{h, snap})
		refs = append(refs, h.SubjectRef)
	}
	prof, err := s.readPII(ctx, refs, client.PurposeHRMProfile, false, client.SectionHRMProfile)
	pii := true
	if forbiddenErr(err) {
		pii, prof, err = false, nil, nil
	}
	if err != nil {
		return nil, false, err
	}
	var named []uuid.UUID
	retirees := map[uuid.UUID]*PlanRetiree{}
	for _, x := range cands {
		f := out[x.snap.Unit]
		var pp *client.BatchItem
		if it, ok := prof[x.h.SubjectRef]; ok && pii {
			pp = &it
		}
		p := person(x.h, x.snap, pop.events[x.h.StaffID], cutoff, pp, nil)
		ret, known := pop.ret.RetiringIn(p, year)
		if !known {
			f.Unknown++
			continue
		}
		if !ret {
			continue
		}
		f.Retiring++
		on, _ := pop.ret.RetireOn(p)
		f.Retirees = append(f.Retirees, PlanRetiree{StaffID: x.h.StaffID, Code: x.h.Code, RetireOn: on})
		r := &f.Retirees[len(f.Retirees)-1]
		if withNames && c.on(domain.PermView, x.h, x.snap) {
			named = append(named, x.h.SubjectRef)
			retirees[x.h.SubjectRef] = r
		}
	}
	if len(named) > 0 {
		idents, err := s.readPII(ctx, named, client.PurposeHRMProfile, true, "")
		if err != nil && !forbiddenErr(err) {
			return nil, false, err
		}
		for ref, it := range idents {
			if r := retirees[ref]; r != nil && !it.Erased {
				r.FullName = it.FullName
			}
		}
	}
	return out, true, nil
}

// --- Bảng tổng hợp và chi tiết. ---

type planState struct {
	row    store.PlanPeriodRow
	pr     formsport.PeriodProgress
	drafts map[string]store.PlanEntryRow
	adjs   map[string][]store.PlanEntryRow
	names  map[string]string
}

func (s *Service) loadPlan(ctx context.Context, c planCaller, year int) (planState, error) {
	st := planState{drafts: map[string]store.PlanEntryRow{}, adjs: map[string][]store.PlanEntryRow{}, names: map[string]string{}}
	row, err := s.planPeriod(ctx, c.p.TenantID, year)
	if err != nil {
		return st, err
	}
	st.row = row
	if !c.center() && !slices.Contains(row.Units, c.unit) {
		return st, port.ErrPlanNotFound
	}
	if st.pr, err = s.Plans.Control.Progress(ctx, port.PlanProfile, row.PeriodRef); err != nil {
		return st, err
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		ds, err := store.PlanDrafts(ctx, tx, row.PeriodRef)
		if err != nil {
			return err
		}
		for _, d := range ds {
			st.drafts[d.Unit] = d
		}
		as, err := store.PlanAdjustments(ctx, tx, row.PeriodRef, "")
		for _, a := range as {
			st.adjs[a.Unit] = append(st.adjs[a.Unit], a)
		}
		return err
	})
	if err != nil {
		return st, err
	}
	if s.Rec != nil && s.Rec.Units != nil && c.p.KN.UnitRef != uuid.Nil {
		us, err := s.Rec.Units.InternalUnits(ctx, c.p.KN.UnitRef)
		if err != nil {
			return st, err
		}
		for _, u := range us {
			st.names[u.Code] = u.Name
		}
	}
	return st, nil
}

func (st planState) closed() bool { return st.pr.Status == "closed" || st.pr.Status == "cancelled" }

func decodeValues(raw []byte) domain.PlanValues {
	var v domain.PlanValues
	_ = json.Unmarshal(raw, &v)
	if v.Positions == nil {
		v.Positions = []domain.PlanPosition{}
	}
	return v
}

// latestSubmission trả bài gửi cuối của đơn vị (nil nếu chưa gửi).
func (s *Service) latestSubmission(ctx context.Context, st planState, unit string) (*formsport.SubmissionRef, error) {
	subs, err := s.Plans.Entry.Submissions(ctx, port.PlanProfile, st.row.PeriodRef, unit)
	if err != nil || len(subs) == 0 {
		return nil, err
	}
	return &subs[0], nil
}

// rowOf dựng dòng tổng hợp của đơn vị: giá trị = điều chỉnh mới nhất > bài gửi cuối > số tự tính (chưa gửi).
func (s *Service) rowOf(ctx context.Context, st planState, u formsport.UnitProgress, fig *PlanFigures) (PlanRow, error) {
	r := PlanRow{Unit: u.Unit, UnitName: st.names[u.Unit], Late: u.Late, Versions: u.Versions, Decision: u.Decision,
		ReviewNote: u.ReviewNote, SubmittedAt: u.LastSubmittedAt, Source: "auto"}
	if r.UnitName == "" {
		r.UnitName = u.Unit
	}
	d, hasDraft := st.drafts[u.Unit]
	draftAfter := hasDraft && (u.LastSubmittedAt == nil || d.RecordedAt.After(*u.LastSubmittedAt)) &&
		(u.ReviewedAt == nil || d.RecordedAt.After(*u.ReviewedAt))
	r.Status = domain.PlanStatus(st.closed(), u.Versions, u.Decision, draftAfter)
	switch {
	case len(st.adjs[u.Unit]) > 0:
		a := st.adjs[u.Unit]
		r.Values, r.Source, r.Adjusted = decodeValues(a[len(a)-1].Values), "adjusted", true
	case u.Versions > 0:
		sub, err := s.latestSubmission(ctx, st, u.Unit)
		if err != nil {
			return r, err
		}
		if sub != nil {
			v, err := domain.PlanValuesFromForm(sub.Values)
			if err != nil {
				return r, err
			}
			r.Values, r.Source = v, "submitted"
		}
	default:
		r.Values = domain.PlanValues{PlanInput: domain.PlanInput{Positions: []domain.PlanPosition{}}}
		if fig != nil {
			r.Values.Current, r.Values.Retiring = fig.Current, fig.Retiring
		}
	}
	r.Demand = r.Values.Demand()
	return r, nil
}

// PlanSummary trả bảng tổng hợp đơn vị × chỉ tiêu (B3; lãnh đạo đơn vị: chỉ dòng đơn vị mình).
func (s *Service) PlanSummary(ctx context.Context, year int) (PlanView, error) {
	c, err := s.planCaller(ctx)
	if err != nil {
		return PlanView{}, err
	}
	st, err := s.loadPlan(ctx, c, year)
	if err != nil {
		return PlanView{}, err
	}
	units := make([]string, 0, len(st.pr.Units))
	for _, u := range st.pr.Units {
		units = append(units, u.Unit)
	}
	figs, ok, err := s.planFigures(ctx, st.row.CutoffOn, year, units, false)
	if err != nil {
		return PlanView{}, err
	}
	v := PlanView{Period: periodOf(st.row, st.pr), Rows: []PlanRow{}, Figures: ok, CanManage: c.center() && !c.readOnly, MyUnit: c.asUnit}
	order := map[string]int{}
	for i, u := range st.row.Units {
		order[u] = i
	}
	prog := slices.Clone(st.pr.Units)
	slices.SortStableFunc(prog, func(a, b formsport.UnitProgress) int { return order[a.Unit] - order[b.Unit] })
	for _, u := range prog {
		r, err := s.rowOf(ctx, st, u, figs[u.Unit])
		if err != nil {
			return PlanView{}, err
		}
		v.Rows = append(v.Rows, r)
		v.Totals.Current += r.Values.Current
		v.Totals.Retiring += r.Values.Retiring
		v.Totals.Leaving += r.Values.Leaving
		v.Totals.Additional += r.Values.Additional
		v.Totals.Demand += r.Demand
	}
	return v, nil
}

// PlanUnit trả chi tiết kế hoạch của đơn vị: số tự tính tại ngày chốt (kèm danh sách dự kiến nghỉ hưu), nháp, các lần gửi,
// điều chỉnh sau chốt, quyền thao tác.
func (s *Service) PlanUnit(ctx context.Context, year int, unit string) (PlanUnitView, error) {
	c, err := s.planCaller(ctx)
	if err != nil {
		return PlanUnitView{}, err
	}
	if !c.center() && c.unit != unit {
		return PlanUnitView{}, port.ErrPlanUnit
	}
	st, err := s.loadPlan(ctx, c, year)
	if err != nil {
		return PlanUnitView{}, err
	}
	i := slices.IndexFunc(st.pr.Units, func(u formsport.UnitProgress) bool { return u.Unit == unit })
	if i < 0 {
		return PlanUnitView{}, port.ErrPlanUnit
	}
	figs, ok, err := s.planFigures(ctx, st.row.CutoffOn, year, []string{unit}, true)
	if err != nil {
		return PlanUnitView{}, err
	}
	fig := figs[unit]
	fig.Available = ok
	row, err := s.rowOf(ctx, st, st.pr.Units[i], fig)
	if err != nil {
		return PlanUnitView{}, err
	}
	v := PlanUnitView{Period: periodOf(st.row, st.pr), Row: row, Figures: *fig, Submissions: []PlanSubmission{}, Adjustments: []PlanAdjustment{}}
	if d, ok := st.drafts[unit]; ok {
		v.Draft = &PlanDraft{Values: decodeValues(d.Values).PlanInput, Actor: d.Actor, RecordedAt: d.RecordedAt}
	}
	subs, err := s.Plans.Entry.Submissions(ctx, port.PlanProfile, st.row.PeriodRef, unit)
	if err != nil {
		return PlanUnitView{}, err
	}
	for _, sb := range subs {
		val, err := domain.PlanValuesFromForm(sb.Values)
		if err != nil {
			return PlanUnitView{}, err
		}
		v.Submissions = append(v.Submissions, PlanSubmission{ID: sb.ID, Version: sb.Version, Values: val, SubmittedAt: sb.SubmittedAt,
			Late: sb.Late, Decision: sb.Decision, ReviewNote: sb.ReviewNote, ReviewedAt: sb.ReviewedAt})
	}
	for _, a := range st.adjs[unit] {
		v.Adjustments = append(v.Adjustments, PlanAdjustment{Seq: a.Seq, Values: decodeValues(a.Values), Reason: a.Reason, Actor: a.Actor,
			RecordedAt: a.RecordedAt})
	}
	open := !st.closed()
	v.CanEdit = open && c.asUnit == unit && !c.readOnly && row.Decision != "accepted"
	v.CanReview = open && c.center() && !c.readOnly && row.Versions > 0 && row.Decision == ""
	v.CanAdjust = st.pr.Status == "closed" && c.center() && !c.readOnly
	return v, nil
}

// --- Ghi. ---

// SavePlanDraft lưu nháp kế hoạch của đơn vị (lãnh đạo đơn vị; kỳ đang nhận bài).
func (s *Service) SavePlanDraft(ctx context.Context, year int, unit string, in domain.PlanInput, idem string) (PlanDraft, error) {
	c, err := s.planCaller(ctx)
	if err != nil {
		return PlanDraft{}, err
	}
	if err := c.needUnit(unit); err != nil {
		return PlanDraft{}, err
	}
	in, err = normalizeInput(in)
	if err != nil {
		return PlanDraft{}, err
	}
	st, err := s.loadPlan(asPlanUnit(ctx), c, year)
	if err != nil {
		return PlanDraft{}, err
	}
	if !slices.ContainsFunc(st.pr.Units, func(u formsport.UnitProgress) bool { return u.Unit == unit }) {
		return PlanDraft{}, port.ErrPlanUnit
	}
	if st.closed() {
		return PlanDraft{}, port.ErrPlanClosed
	}
	raw, err := json.Marshal(domain.PlanValues{PlanInput: in})
	if err != nil {
		return PlanDraft{}, err
	}
	if idem == "" {
		idem = "hrm-plan-draft:" + ids.New().String()
	}
	now := s.Clock.Now()
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		_, e := store.InsertPlanDraft(ctx, tx, c.p.TenantID, store.PlanEntryRow{ID: ids.New(), PeriodRef: st.row.PeriodRef, Unit: unit,
			Values: raw, Actor: c.p.ActorRef}, now, idem)
		return e
	})
	if err != nil {
		return PlanDraft{}, classify(err)
	}
	return PlanDraft{Values: in, Actor: c.p.ActorRef, RecordedAt: now}, nil
}

func normalizeInput(in domain.PlanInput) (domain.PlanInput, error) {
	out, err := domain.NormalizePlanInput(in)
	var fe domain.PlanFieldError
	if errors.As(err, &fe) {
		return in, problem.Validation.WithField(fe.Field, fe.Msg)
	}
	return out, err
}

// SubmitPlan gửi kế hoạch của đơn vị cho Phòng HCTH (bài nộp cap/forms): số hiện có, dự kiến nghỉ hưu điền từ hồ sơ tại
// ngày chốt (BR-01). baseVersion = số lần gửi người dùng đã thấy (khóa lạc quan).
func (s *Service) SubmitPlan(ctx context.Context, year int, unit string, in domain.PlanInput, baseVersion int64, idem string) (PlanSubmission, error) {
	c, err := s.planCaller(ctx)
	if err != nil {
		return PlanSubmission{}, err
	}
	if err := c.needUnit(unit); err != nil {
		return PlanSubmission{}, err
	}
	in, err = normalizeInput(in)
	if err != nil {
		return PlanSubmission{}, err
	}
	uctx := asPlanUnit(ctx)
	row, err := s.planPeriod(ctx, c.p.TenantID, year)
	if err != nil {
		return PlanSubmission{}, err
	}
	figs, _, err := s.planFigures(ctx, row.CutoffOn, year, []string{unit}, false)
	if err != nil {
		return PlanSubmission{}, err
	}
	val := domain.PlanValues{Current: figs[unit].Current, Retiring: figs[unit].Retiring, PlanInput: in}
	fv, err := val.FormValues()
	var fe domain.PlanFieldError
	if errors.As(err, &fe) {
		return PlanSubmission{}, problem.Validation.WithField(fe.Field, fe.Msg)
	} else if err != nil {
		return PlanSubmission{}, err
	}
	if idem == "" {
		idem = "hrm-plan-submit:" + ids.New().String()
	}
	sb, err := s.Plans.Entry.Submit(uctx, port.PlanProfile, row.PeriodRef, unit, fv, baseVersion, idem)
	if err != nil {
		return PlanSubmission{}, err
	}
	return PlanSubmission{ID: sb.ID, Version: sb.Version, Values: val, SubmittedAt: sb.SubmittedAt, Late: sb.Late}, nil
}

// ReviewPlan: HCTH xác nhận (accepted) hoặc yêu cầu điều chỉnh (returned, ghi chú bắt buộc) bài gửi cuối của đơn vị.
// Người rà soát khác người gửi (bốn mắt cap/forms).
func (s *Service) ReviewPlan(ctx context.Context, year int, unit, decision, note, idem string) (PlanSubmission, error) {
	c, err := s.planCaller(ctx)
	if err != nil {
		return PlanSubmission{}, err
	}
	if err := c.needCenter(true); err != nil {
		return PlanSubmission{}, err
	}
	row, err := s.planPeriod(ctx, c.p.TenantID, year)
	if err != nil {
		return PlanSubmission{}, err
	}
	subs, err := s.Plans.Entry.Submissions(ctx, port.PlanProfile, row.PeriodRef, unit)
	if err != nil {
		return PlanSubmission{}, err
	}
	if len(subs) == 0 {
		return PlanSubmission{}, formsport.ErrSubmissionMissing
	}
	if idem == "" {
		idem = "hrm-plan-review:" + ids.New().String()
	}
	sb, err := s.Plans.Entry.Review(ctx, port.PlanProfile, subs[0].ID, decision, note, idem)
	if err != nil {
		return PlanSubmission{}, err
	}
	val, err := domain.PlanValuesFromForm(sb.Values)
	if err != nil {
		return PlanSubmission{}, err
	}
	return PlanSubmission{ID: sb.ID, Version: sb.Version, Values: val, SubmittedAt: sb.SubmittedAt, Late: sb.Late, Decision: sb.Decision,
		ReviewNote: sb.ReviewNote, ReviewedAt: sb.ReviewedAt}, nil
}

// ClosePlan chốt kế hoạch năm (ảnh chụp đóng băng cap/forms) — đơn vị chưa gửi thành "Không gửi".
func (s *Service) ClosePlan(ctx context.Context, year int, idem string) error {
	c, err := s.planCaller(ctx)
	if err != nil {
		return err
	}
	if err := c.needCenter(true); err != nil {
		return err
	}
	row, err := s.planPeriod(ctx, c.p.TenantID, year)
	if err != nil {
		return err
	}
	if idem == "" {
		idem = "hrm-plan-close:" + ids.New().String()
	}
	err = s.Plans.Control.ClosePeriod(ctx, port.PlanProfile, row.PeriodRef, idem)
	if errors.Is(err, formsport.ErrPeriodClosed) {
		return port.ErrPlanClosed
	}
	return err
}

// AdjustPlan ghi điều chỉnh sau chốt có lý do (BR-02): giá trị nhập mới, số hiện có/dự kiến nghỉ hưu giữ như đã chốt. Ảnh
// chụp chốt của cap/forms không đổi; bảng tổng hợp hiển thị giá trị sau điều chỉnh, lịch sử đầy đủ.
func (s *Service) AdjustPlan(ctx context.Context, year int, unit string, in domain.PlanInput, reason, idem string) (PlanAdjustment, error) {
	c, err := s.planCaller(ctx)
	if err != nil {
		return PlanAdjustment{}, err
	}
	if err := c.needCenter(true); err != nil {
		return PlanAdjustment{}, err
	}
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 10 || n > 500 {
		return PlanAdjustment{}, problem.Validation.WithField("reason", "lý do điều chỉnh 10–500 ký tự")
	}
	in, err = normalizeInput(in)
	if err != nil {
		return PlanAdjustment{}, err
	}
	st, err := s.loadPlan(ctx, c, year)
	if err != nil {
		return PlanAdjustment{}, err
	}
	i := slices.IndexFunc(st.pr.Units, func(u formsport.UnitProgress) bool { return u.Unit == unit })
	if i < 0 {
		return PlanAdjustment{}, port.ErrPlanUnit
	}
	if st.pr.Status != "closed" {
		return PlanAdjustment{}, port.ErrPlanNotClosed
	}
	base, err := s.rowOf(ctx, st, st.pr.Units[i], nil)
	if err != nil {
		return PlanAdjustment{}, err
	}
	if base.Source == "auto" {
		// Đơn vị không gửi trước chốt: số hiện có, dự kiến nghỉ hưu tính tại ngày chốt.
		figs, _, err := s.planFigures(ctx, st.row.CutoffOn, year, []string{unit}, false)
		if err != nil {
			return PlanAdjustment{}, err
		}
		base.Values.Current, base.Values.Retiring = figs[unit].Current, figs[unit].Retiring
	}
	val := domain.PlanValues{Current: base.Values.Current, Retiring: base.Values.Retiring, PlanInput: in}
	raw, err := json.Marshal(val)
	if err != nil {
		return PlanAdjustment{}, err
	}
	if idem == "" {
		idem = "hrm-plan-adjust:" + ids.New().String()
	}
	now := s.Clock.Now()
	var seq int64
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) (e error) {
		seq, e = store.InsertPlanAdjustment(ctx, tx, c.p.TenantID, store.PlanEntryRow{ID: ids.New(), PeriodRef: st.row.PeriodRef,
			Unit: unit, Values: raw, Reason: reason, Actor: c.p.ActorRef}, now, idem)
		return e
	})
	if err != nil {
		return PlanAdjustment{}, classify(err)
	}
	return PlanAdjustment{Seq: seq, Values: val, Reason: reason, Actor: c.p.ActorRef, RecordedAt: now}, nil
}

// PreviousPlan trả phần nhập của đơn vị ở kế hoạch năm trước (sau điều chỉnh, nếu có) để sao chép làm nháp — không ghi.
func (s *Service) PreviousPlan(ctx context.Context, year int, unit string) (domain.PlanInput, int, error) {
	c, err := s.planCaller(ctx)
	if err != nil {
		return domain.PlanInput{}, 0, err
	}
	if !c.center() && c.unit != unit {
		return domain.PlanInput{}, 0, port.ErrPlanUnit
	}
	st, err := s.loadPlan(ctx, c, year-1)
	if errors.Is(err, port.ErrPlanNotFound) {
		return domain.PlanInput{}, 0, port.ErrPlanNoPrevious
	}
	if err != nil {
		return domain.PlanInput{}, 0, err
	}
	i := slices.IndexFunc(st.pr.Units, func(u formsport.UnitProgress) bool { return u.Unit == unit })
	if i < 0 {
		return domain.PlanInput{}, 0, port.ErrPlanNoPrevious
	}
	r, err := s.rowOf(ctx, st, st.pr.Units[i], nil)
	if err != nil {
		return domain.PlanInput{}, 0, err
	}
	if r.Source == "auto" {
		return domain.PlanInput{}, 0, port.ErrPlanNoPrevious
	}
	return r.Values.PlanInput, year - 1, nil
}

// ExportPlan xuất bảng tổng hợp ra Excel (luồng phụ "xuất kế hoạch"): đơn vị × chỉ tiêu, dòng Toàn Trung tâm, nhu cầu theo
// vị trí. Chỉ Phòng HCTH/Admin.
func (s *Service) ExportPlan(ctx context.Context, year int) ([]byte, string, error) {
	c, err := s.planCaller(ctx)
	if err != nil {
		return nil, "", err
	}
	if err := c.needCenter(false); err != nil {
		return nil, "", err
	}
	v, err := s.PlanSummary(ctx, year)
	if err != nil {
		return nil, "", err
	}
	b, err := renderPlanXLSX(v, s.Rep, s.Clock.Now())
	return b, fmt.Sprintf("ke-hoach-nhan-luc-%d.xlsx", year), err
}

var planStatusLabel = map[string]string{domain.PlanStatusNone: "Chưa lập", domain.PlanStatusDraft: "Đang lập",
	domain.PlanStatusSubmitted: "Đã gửi", domain.PlanStatusReturned: "Yêu cầu điều chỉnh", domain.PlanStatusAccepted: "Đã xác nhận",
	domain.PlanStatusClosed: "Đã chốt", domain.PlanStatusMissed: "Không gửi"}

func renderPlanXLSX(v PlanView, cfg ReportsConfig, at time.Time) ([]byte, error) {
	agency, org := cfg.Agency, cfg.Org
	if agency == "" {
		agency = defaultAgency
	}
	if org == "" {
		org = defaultOrg
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	const sh = "KeHoach"
	if err := f.SetSheetName("Sheet1", sh); err != nil {
		return nil, err
	}
	set := func(col, row int, val any) { _ = f.SetCellValue(sh, cellName(col, row), val) }
	set(1, 1, agency)
	set(1, 2, org)
	set(1, 4, strings.ToUpper(v.Period.Label))
	set(1, 5, fmt.Sprintf("Ngày chốt số liệu hiện có: %s · Hạn gửi: %s", v.Period.CutoffOn.Format("02/01/2006"),
		v.Period.DueAt.In(clock.Zone()).Format("02/01/2006")))
	head := []string{"STT", "Đơn vị", "Hiện có", "Dự kiến nghỉ hưu", "Dự kiến nghỉ, chuyển khác", "Cần bổ sung", "Nhu cầu theo kế hoạch",
		"Trạng thái", "Nhu cầu theo vị trí"}
	for i, h := range head {
		set(i+1, 7, h)
		set(i+1, 8, fmt.Sprintf("(%d)", i+1))
	}
	r := 9
	for i, x := range v.Rows {
		var pos []string
		for _, p := range x.Values.Positions {
			pos = append(pos, fmt.Sprintf("%s (%d)", p.Title, p.Count))
		}
		st := planStatusLabel[x.Status]
		if x.Adjusted {
			st += " (đã điều chỉnh)"
		}
		for j, val := range []any{i + 1, x.UnitName, x.Values.Current, x.Values.Retiring, x.Values.Leaving, x.Values.Additional, x.Demand, st,
			strings.Join(pos, "; ")} {
			set(j+1, r, val)
		}
		r++
	}
	for j, val := range []any{"", "Toàn Trung tâm", v.Totals.Current, v.Totals.Retiring, v.Totals.Leaving, v.Totals.Additional, v.Totals.Demand} {
		set(j+1, r, val)
	}
	set(1, r+2, "Xuất lúc "+at.In(clock.Zone()).Format("15:04 02/01/2006"))
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
