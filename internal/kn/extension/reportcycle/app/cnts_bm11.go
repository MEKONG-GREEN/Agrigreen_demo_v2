package app

// Báo cáo phòng CNTS BM-11 tuần/tháng/quý/năm (W11.45 — BRD CNTS v3.7 UR-TINH-16, FN-16; D4 CN5; Q-046 giả định theo
// prototype/đề xuất: giữ BM-11 gửi để LƯU, số liệu cho KHTC chỉ qua phiếu + nguồn đã chốt).
//
//   - Tạo: cán bộ Cấp Tỉnh phòng CNTS chọn loại + kỳ; hệ thống TÍNH phần tự động (BM-04 chăn nuôi, BM-17/BM-18 thủy sản của
//     W11.44, CT/DA của W11.47) chỉ từ bài Đã nộp/Đã xác thực (bỏ bài Yêu cầu điều chỉnh) hoặc số đã chốt khi mọi kỳ nguồn đã
//     chốt; chỉ tạo được sau hạn nộp của tuần cuối (BR-06) VÀ (tỷ lệ xã đã nộp của từng biểu ≥ mức Cấp Tỉnh đặt HOẶC kỳ đã
//     chốt) — quy tắc 13; ghi "Tổng hợp từ n/N xã" + danh sách xã thiếu, không ước tính (BR-04). Một báo cáo mỗi kỳ.
//   - Soạn: phần viết (mục theo loại), chủ nhiệm viết đoạn chương trình mình; "Làm mới số liệu" tính lại phần tự động (giữ phần
//     viết). Mỗi bước là một phiên bản đóng băng ở sổ (pf 20261028034853), khóa lạc quan expected_seq.
//   - Trình → Lãnh đạo phòng duyệt (tự gửi theo nơi nhận — BR-03) hoặc trả lại (bắt buộc ý kiến — AC-04); Ban Giám đốc chỉ
//     xem (BR-05). Phòng nhận (HCTH, KHTC) thấy ở hộp "Báo cáo đến" ngay khi duyệt (AC-02) và bấm "Đã nhận".
//   - Số thay đổi sau khi gửi của kỳ trước đưa vào mục "Số liệu kỳ trước nộp bổ sung" của báo cáo kỳ sau (BR-02).
//
// Mọi lời gọi module khác (cap/forms, pf/party, CT/DA, pf/export) chạy NGOÀI giao dịch KN (P0-POOL-KN).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Mã lỗi (docs/errors/pf-extension-cnts-bm11.md).
var (
	ErrBM11NotFound = problem.New(http.StatusNotFound, "extension.cnts_bm11_not_found", "Không tìm thấy báo cáo phòng BM-11")
	ErrBM11Exists   = problem.New(http.StatusConflict, "extension.cnts_bm11_exists", "Kỳ này đã có báo cáo phòng — mở báo cáo đã có")
	ErrBM11State    = problem.New(http.StatusConflict, "extension.cnts_bm11_state",
		"Báo cáo không ở trạng thái cho phép thao tác này (đã trình, đã gửi hoặc nơi nhận đã xác nhận)")
	ErrBM11Version  = problem.New(http.StatusConflict, "extension.cnts_bm11_version_conflict", "Báo cáo đã có người cập nhật — tải lại bản mới")
	ErrBM11NotReady = problem.New(http.StatusConflict, "extension.cnts_bm11_not_ready",
		"Chưa lập được báo cáo: chưa tới hạn nộp, hoặc tỷ lệ xã đã nộp chưa đạt mức Cấp Tỉnh đặt và kỳ chưa chốt")
	errBM11Reader   = problem.Forbidden.WithDetail("báo cáo phòng BM-11 dành cho Phòng Chăn nuôi – Thủy sản, Admin và phòng nhận báo cáo đã gửi")
	errBM11Approver = problem.Forbidden.WithDetail("chỉ Lãnh đạo phòng Chăn nuôi – Thủy sản (hoặc người được ủy quyền duyệt báo cáo) duyệt, trả lại")
	errBM11Receiver = problem.Forbidden.WithDetail("chỉ phòng nhận của báo cáo (Cấp Tỉnh, không chế độ chỉ xem) xác nhận Đã nhận")
	errBM11Writer   = problem.Forbidden.WithDetail("chỉ Cấp Tỉnh Phòng Chăn nuôi – Thủy sản (không chế độ chỉ xem) lập, soạn, trình báo cáo phòng")
	errBM11Program  = problem.Forbidden.WithDetail("chỉ chủ nhiệm chương trình, người lập báo cáo hoặc Lãnh đạo phòng sửa đoạn của chương trình")
)

// ParamBM11MinSubmitPct là mức tỷ lệ xã đã nộp tối thiểu (%) của từng biểu nguồn để lập báo cáo khi kỳ chưa chốt (quy tắc
// 13, Cấp Tỉnh đặt qua tham số; mặc định BM11DefaultMinPct — giả định chờ PO).
const (
	ParamBM11MinSubmitPct = "extension.cnts_bm11_min_submit_pct"
	BM11DefaultMinPct     = 80
)

// BM11ProgramSource trả CT/DA của tỉnh còn hiệu lực trong [from, to] (W11.47; gốc module nối từ cntsmodel).
type BM11ProgramSource func(ctx context.Context, province string, from, to time.Time) ([]domain.BM11Program, error)

const (
	deptHCTH = "hanh_chinh_tong_hop"
	groupBGD = "ban_giam_doc"
)

// bm11Who là người gọi và quyền của họ với báo cáo BM-11.
type bm11Who struct {
	a         actor
	cnts      bool   // Admin hoặc Cấp Tỉnh/Cấp Quốc gia phòng CNTS
	recipient string // nơi nhận của người gọi (bgd | hcth | khtc), rỗng nếu không phải phòng nhận
}

func (w bm11Who) admin() bool { return w.a.k.Role == knport.SessionRoleAdmin }

// writer: Cấp Tỉnh CNTS, không chế độ chỉ xem.
func (w bm11Who) writer() bool {
	return w.a.k.Role == knport.SessionRoleProvince && w.a.k.Department == domain.DeptChanNuoiThuySan && !w.a.k.ReadOnly()
}

// approver: Lãnh đạo phòng CNTS (Manager) hoặc người được ủy quyền "duyet_bao_cao" (BR-05: Ban Giám đốc chỉ xem).
func (w bm11Who) approver() bool {
	return w.a.k.Role == knport.SessionRoleProvince && w.a.k.Department == domain.DeptChanNuoiThuySan &&
		w.a.k.Can(knport.SessionPowerApprove)
}

func (s *Service) bm11Who(ctx context.Context) (bm11Who, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return bm11Who{}, err
	}
	w := bm11Who{a: a}
	w.cnts = a.k.Role == knport.SessionRoleAdmin ||
		(a.k.Department == domain.DeptChanNuoiThuySan && (a.k.Role == knport.SessionRoleProvince || a.k.Role == knport.SessionRoleNational))
	if a.k.Role == knport.SessionRoleProvince {
		switch {
		case a.k.ProvinceGroup == groupBGD:
			w.recipient = domain.BM11RecipientBGD
		case a.k.Department == deptHCTH:
			w.recipient = domain.BM11RecipientHCTH
		case a.k.Department == domain.DeptKeHoachTaiChinh:
			w.recipient = domain.BM11RecipientKHTC
		}
	}
	if !w.cnts && w.recipient == "" {
		return w, errBM11Reader
	}
	return w, nil
}

// sees: CNTS trong tỉnh mình (Admin/TW mọi tỉnh); phòng nhận chỉ báo cáo ĐÃ GỬI tới mình, cùng tỉnh (lesson #108: lọc phạm
// vi trước khi trả dữ liệu).
func (w bm11Who) sees(r store.BM11Report, status string) bool {
	inProv := w.a.province == "" || w.a.province == r.Province
	if w.cnts && inProv {
		return true
	}
	return w.recipient != "" && inProv && status == domain.BM11Sent && slices.Contains(r.Recipients, w.recipient)
}

func bm11Classify(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.ConstraintName {
	case "cnts_bm11_state", "cnts_bm11_received":
		return ErrBM11State.WithCause(err)
	case "cnts_bm11_report_period":
		return ErrBM11Exists.WithCause(err)
	}
	if pg.Code == "23505" {
		return problem.Conflict.WithCause(err)
	}
	return err
}

// bm11AnySeq: bước không kiểm expected_seq (xác nhận Đã nhận của phòng nhận) — so con trỏ, máy khách không giả được.
var bm11AnySeq = new(int64)

func bm11Idem(op, idem string) string {
	if idem == "" {
		idem = ids.New().String()
	}
	return "cnts_bm11:" + op + ":" + idem
}

// ---------------------------------------------------------------------------
// Kỳ + tính phần tự động
// ---------------------------------------------------------------------------

// bm11Period giải kỳ của báo cáo (khóa rỗng = kỳ vừa kết thúc gần nhất của loại).
func (s *Service) bm11Period(ctx context.Context, typ, key string) (domain.BM11Period, error) {
	if !slices.Contains(domain.BM11Types, typ) {
		return domain.BM11Period{}, problem.Validation.WithField("type", "week | month | quarter | year")
	}
	now := s.Clock.Now().In(clock.Zone())
	if typ == domain.BM11Week {
		if key == "" {
			cur, _, err := s.cntsWeek(ctx, "")
			if err != nil {
				return domain.BM11Period{}, err
			}
			cal := s.cntsCalendar(ctx, cur.From.AddDate(0, 0, -1))
			prev, err := cal.Rule.PeriodOf(domain.PeriodWeek, cur.From.AddDate(0, 0, -1), nil)
			if err != nil {
				return domain.BM11Period{}, err
			}
			key = prev.Key
		}
		w, _, err := s.cntsWeek(ctx, key)
		if err != nil {
			return domain.BM11Period{}, problem.Validation.WithField("key", "tuần dạng YYYY-Www")
		}
		label := fmt.Sprintf("Tuần %d từ ngày %s đến ngày %s", w.WeekNo, w.From.Format("02/01/2006"), w.To.Format("02/01/2006"))
		return domain.BM11Period{Type: typ, Key: w.Key, Label: label, From: w.From, To: w.To, Weeks: []domain.Span{w}}, nil
	}
	if key == "" {
		switch typ {
		case domain.BM11Month:
			key = now.AddDate(0, -1, 0).Format("2006-01")
		case domain.BM11Quarter:
			q := (int(now.Month())-1)/3 + 1
			y := now.Year()
			if q == 1 {
				q, y = 4, y-1
			} else {
				q--
			}
			key = fmt.Sprintf("%d-Q%d", y, q)
		default:
			key = fmt.Sprintf("%d", now.Year()-1)
		}
	}
	from, to, label, err := domain.BM11CalendarRange(typ, key)
	if err != nil {
		return domain.BM11Period{}, problem.Validation.WithField("key", err.Error())
	}
	cal := s.cntsCalendar(ctx, domain.Date(from.Year(), 1, 1))
	weeks, err := domain.BM11WeeksEnding(cal.Rule, from, to)
	if err != nil {
		return domain.BM11Period{}, problem.Validation.WithField("key", err.Error())
	}
	if len(weeks) == 0 {
		return domain.BM11Period{}, problem.Validation.WithField("key", "kỳ không chứa tuần CNTS nào kết thúc trong kỳ")
	}
	return domain.BM11Period{Type: typ, Key: key, Label: label, From: from, To: to, Weeks: weeks}, nil
}

// bm11Calc là kết quả tính phần tự động của một kỳ.
type bm11Calc struct {
	period    domain.BM11Period
	content   domain.BM11Content
	livestock domain.Dec
	ready     bool
	reasons   []string
	dueAt     time.Time
	minPct    int
}

func (s *Service) bm11Fishery(ctx context.Context, a actor, province string, p domain.BM11Period, basis string) (domain.BM11Catch, error) {
	last := p.Weeks[len(p.Weeks)-1]
	d, err := s.loadBM18(ctx, a, province, last.Key, basis)
	if err != nil {
		return domain.BM11Catch{}, err
	}
	keys := append(slices.Clone(domain.CatchGroups), domain.Key{Key: domain.CatchTotal})
	cur := func(c domain.Cols, k string) decimal.Decimal {
		switch p.Type {
		case domain.BM11Week:
			return c.Cur[k]
		case domain.BM11Quarter:
			return c.QTD[k]
		}
		return c.YTD[k]
	}
	catch, aqua, total := domain.Dec{}, cur(d.res.AquaOutput, domain.AquaKey), cur(d.res.Total, domain.AquaKey)
	for _, k := range keys {
		catch[k.Key] = cur(d.res.Catch, k.Key)
	}
	if p.Type == domain.BM11Month {
		// Tháng = lũy kế năm tới tuần cuối của tháng − lũy kế năm tới tuần liền trước tuần đầu của tháng (cùng năm).
		cal := s.cntsCalendar(ctx, p.Weeks[0].From.AddDate(0, 0, -1))
		prev, err := cal.Rule.PeriodOf(domain.PeriodWeek, p.Weeks[0].From.AddDate(0, 0, -1), nil)
		if err == nil && prev.To.Year() == last.To.Year() {
			pd, err := s.loadBM18(ctx, a, province, prev.Key, basis)
			if err != nil {
				return domain.BM11Catch{}, err
			}
			for _, k := range keys {
				catch[k.Key] = catch[k.Key].Sub(pd.res.Catch.YTD[k.Key])
			}
			aqua = aqua.Sub(pd.res.AquaOutput.YTD[domain.AquaKey])
			total = total.Sub(pd.res.Total.YTD[domain.AquaKey])
		}
	}
	out := domain.BM11Catch{CatchCur: catch[domain.CatchTotal].String(), CatchYTD: d.res.Catch.YTD[domain.CatchTotal].String(),
		AquaCur: aqua.String(), AquaYTD: d.res.AquaOutput.YTD[domain.AquaKey].String(), TotalCur: total.String(),
		TotalYTD: d.res.Total.YTD[domain.AquaKey].String(), Groups: map[string]string{}}
	for _, g := range domain.CatchGroups {
		out.Groups[g.Key] = catch[g.Key].String()
	}
	return out, nil
}

// bm11Compute tính phần tự động của kỳ (không kiểm quyền — bên gọi đã kiểm).
func (s *Service) bm11Compute(ctx context.Context, a actor, province string, p domain.BM11Period, programs bool) (*bm11Calc, error) {
	if s.FormsFigures == nil {
		return nil, ErrDependency.WithDetail("chưa nối cap/forms (số liệu kỳ native) cho báo cáo phòng")
	}
	nb, err := s.regionNames(ctx, province)
	if err != nil {
		return nil, err
	}
	comm := provinceCommunes(nb)
	from, to := p.Weeks[0].From, p.Weeks[len(p.Weeks)-1].To
	load := func(basis string) ([]domain.SourcePeriod, []domain.SourcePeriod, error) {
		ps04, err := s.sources(ctx, domain.CNTSBM04, from, to, basis, comm)
		if err != nil {
			return nil, nil, err
		}
		ps17, err := s.sources(ctx, domain.CNTSBM17, from, to, basis, comm)
		return ps04, ps17, err
	}
	ps04, ps17, err := load(domain.BasisSubmitted)
	if err != nil {
		return nil, err
	}
	cov := []domain.BM11Coverage{domain.BM11CoverageOf(domain.CNTSBM04, p.Weeks, ps04), domain.BM11CoverageOf(domain.CNTSBM17, p.Weeks, ps17)}
	allClosed, anySrc := true, false
	for _, c := range cov {
		if c.Periods > 0 {
			anySrc = true
			allClosed = allClosed && c.AllClosed()
		}
	}
	allClosed = allClosed && anySrc
	basis := domain.BasisSubmitted
	if allClosed {
		basis = domain.BasisClosed
		if ps04, _, err = load(domain.BasisClosed); err != nil {
			return nil, err
		}
	}
	c := &bm11Calc{period: p, livestock: domain.BM11Livestock(p.Weeks, ps04), minPct: s.paramInt(ctx, ParamBM11MinSubmitPct, BM11DefaultMinPct),
		dueAt: domain.BM11DueAt(p.Weeks[len(p.Weeks)-1], clock.Zone())}
	for i := range cov {
		cov[i].MissingNames = make([]string, 0, len(cov[i].Missing))
		for _, m := range cov[i].Missing {
			cov[i].MissingNames = append(cov[i].MissingNames, nb.commune(ctx, s, m))
		}
	}
	fish, err := s.bm11Fishery(ctx, a, province, p, basis)
	if err != nil {
		return nil, err
	}
	// Quy tắc 13 + BR-06.
	if !anySrc {
		c.reasons = append(c.reasons, "kỳ chưa có kỳ nguồn BM-04 hoặc BM-17 nào đã phát hành")
	}
	if !allClosed {
		if s.Clock.Now().Before(c.dueAt) {
			c.reasons = append(c.reasons, "chưa tới hạn nộp "+c.dueAt.In(clock.Zone()).Format("15:04 02/01/2006"))
		}
		for _, x := range cov {
			if x.Periods > 0 && x.Pct() < c.minPct {
				c.reasons = append(c.reasons, fmt.Sprintf("%s: %d/%d xã đã nộp (%d%%) dưới mức %d%%", x.Form, x.Reported, x.Expected, x.Pct(), c.minPct))
			}
		}
	}
	c.ready = len(c.reasons) == 0
	wk := make([]string, 0, len(p.Weeks))
	for _, w := range p.Weeks {
		wk = append(wk, w.Key)
	}
	c.content = domain.BM11Content{Schema: domain.BM11Schema, Basis: basis, Weeks: wk, Livestock: map[string]string{}, Fishery: fish,
		Coverage: cov, Programs: []domain.BM11Program{}, Sections: map[string]string{}, Supplement: []domain.BM11Supplement{},
		ComputedAt: s.now().Format(time.RFC3339)}
	for k, v := range c.livestock {
		c.content.Livestock[k] = v.String()
	}
	for _, sec := range domain.BM11SectionsOf(p.Type) {
		c.content.Sections[sec.Key] = ""
	}
	if programs && s.BM11Programs != nil {
		ps, err := s.BM11Programs(ctx, province, p.From, p.To)
		if err != nil {
			return nil, err
		}
		if len(ps) > domain.BM11MaxPrograms {
			ps = ps[:domain.BM11MaxPrograms]
		}
		c.content.Programs = append(c.content.Programs, ps...)
	}
	return c, nil
}

// bm11Supplement tính "Số liệu kỳ trước nộp bổ sung": báo cáo đã gửi gần nhất cùng loại trước kỳ p, so số đã gửi với số tính
// lại hiện tại (BR-02). Lỗi đọc kỳ trước không chặn việc lập báo cáo (bỏ mục).
func (s *Service) bm11Supplement(ctx context.Context, a actor, province string, p domain.BM11Period) []domain.BM11Supplement {
	var prev *store.BM11Report
	var evs []store.BM11Event
	if err := s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if prev, e = store.LastSentBM11(ctx, tx, province, p.Type, p.From); e != nil || prev == nil {
			return e
		}
		evs, e = store.BM11Events(ctx, tx, prev.ID)
		return e
	}); err != nil || prev == nil {
		return []domain.BM11Supplement{}
	}
	sent, _, ok := lastContent(evs)
	if !ok {
		return []domain.BM11Supplement{}
	}
	pp, err := s.bm11Period(ctx, prev.Type, prev.PeriodKey)
	if err != nil {
		return []domain.BM11Supplement{}
	}
	now, err := s.bm11Compute(ctx, a, province, pp, false)
	if err != nil {
		return []domain.BM11Supplement{}
	}
	out := domain.BM11Diff(prev.Label, sent, now.livestock, now.content.Fishery)
	if out == nil {
		return []domain.BM11Supplement{}
	}
	return out
}

func lastContent(evs []store.BM11Event) (domain.BM11Content, store.BM11Event, bool) {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Content != nil {
			var c domain.BM11Content
			if err := json.Unmarshal(evs[i].Content, &c); err != nil {
				return c, evs[i], false
			}
			return c, evs[i], true
		}
	}
	return domain.BM11Content{}, store.BM11Event{}, false
}

// ---------------------------------------------------------------------------
// Xem
// ---------------------------------------------------------------------------

// BM11LineView là một dòng chăn nuôi của báo cáo.
type BM11LineView struct {
	Key   string `json:"key"`
	Group string `json:"group"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
	Sub   bool   `json:"sub"`
	Value string `json:"value"`
}

// BM11SectionView là một mục phần viết.
type BM11SectionView struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

// BM11ProgramView là kết quả một CT/DA kèm quyền sửa đoạn viết.
type BM11ProgramView struct {
	domain.BM11Program
	ManagerName string `json:"manager_name"`
	CanEdit     bool   `json:"can_edit"`
}

// BM11RecipientView là trạng thái một nơi nhận.
type BM11RecipientView struct {
	Code       string  `json:"code"`
	Label      string  `json:"label"`
	Received   bool    `json:"received"`
	ReceivedAt *string `json:"received_at"`
	ActorName  string  `json:"actor_name"`
}

// BM11EventView là một bước trong lịch sử.
type BM11EventView struct {
	Seq         int64  `json:"seq"`
	Event       string `json:"event"`
	Status      string `json:"status"`
	Comment     string `json:"comment"`
	Recipient   string `json:"recipient"`
	ContentHash string `json:"content_hash"`
	ActorName   string `json:"actor_name"`
	At          string `json:"at"`
}

// BM11Summary là một dòng danh sách.
type BM11Summary struct {
	ID         string   `json:"id"`
	Province   string   `json:"province"`
	Type       string   `json:"type"`
	PeriodKey  string   `json:"period_key"`
	Label      string   `json:"label"`
	From       string   `json:"from"`
	To         string   `json:"to"`
	Recipients []string `json:"recipients"`
	Status     string   `json:"status"`
	Seq        int64    `json:"seq"`
	LastEvent  string   `json:"last_event"`
	LastAt     string   `json:"last_at"`
	CreatedBy  string   `json:"created_by"`
}

// BM11Can là thao tác người gọi được làm.
type BM11Can struct {
	Edit    bool `json:"edit"`
	Refresh bool `json:"refresh"`
	Submit  bool `json:"submit"`
	Approve bool `json:"approve"`
	Return  bool `json:"return"`
	Receive bool `json:"receive"`
	Word    bool `json:"word"`
}

// BM11View là chi tiết báo cáo (nội dung phiên bản hiện hành).
type BM11View struct {
	BM11Summary
	Version       int64                   `json:"version"` // seq của bước mang nội dung hiện hành
	ContentHash   string                  `json:"content_hash"`
	Basis         string                  `json:"basis"`
	Communes      string                  `json:"communes"` // "Tổng hợp từ n/N xã"
	Coverage      []domain.BM11Coverage   `json:"coverage"`
	Groups        []domain.Key            `json:"groups"`
	Lines         []BM11LineView          `json:"lines"`
	Fishery       domain.BM11Catch        `json:"fishery"`
	Programs      []BM11ProgramView       `json:"programs"`
	Sections      []BM11SectionView       `json:"sections"`
	Supplement    []domain.BM11Supplement `json:"supplement"`
	ReturnComment string                  `json:"return_comment"`
	RecipientList []BM11RecipientView     `json:"recipient_list"`
	History       []BM11EventView         `json:"history"`
	Can           BM11Can                 `json:"can"`
	Replayed      bool                    `json:"replayed"`
}

func bm11Lines(c domain.BM11Content) []BM11LineView {
	out := make([]BM11LineView, 0, len(domain.BM11Lines))
	for _, l := range domain.BM11Lines {
		out = append(out, BM11LineView{Key: l.Key, Group: l.Group, Label: l.Label, Unit: l.Unit, Sub: l.Sub, Value: orZero(c.Livestock[l.Key])})
	}
	return out
}

// BM11CommunesText trả "Tổng hợp từ n/N xã" (BR-04).
func BM11CommunesText(cov []domain.BM11Coverage) string {
	c, ok := domain.BM11Communes(cov)
	if !ok {
		return "Chưa có số liệu xã"
	}
	return fmt.Sprintf("Tổng hợp từ %d/%d xã", c.Reported, c.Expected)
}

func summaryOf(r store.BM11Report, last store.BM11Event, nm map[uuid.UUID]string) BM11Summary {
	return BM11Summary{ID: r.ID.String(), Province: r.Province, Type: r.Type, PeriodKey: r.PeriodKey, Label: r.Label,
		From: r.From.Format(time.DateOnly), To: r.To.Format(time.DateOnly), Recipients: r.Recipients, Status: last.Status, Seq: last.Seq,
		LastEvent: last.Event, LastAt: last.At.UTC().Format(time.RFC3339), CreatedBy: nm[r.CreatedBy]}
}

func (s *Service) bm11View(ctx context.Context, w bm11Who, r store.BM11Report, evs []store.BM11Event) (BM11View, error) {
	if len(evs) == 0 {
		return BM11View{}, ErrBM11NotFound
	}
	last := evs[len(evs)-1]
	content, cev, ok := lastContent(evs)
	if !ok {
		return BM11View{}, ErrBM11NotFound.WithDetail("báo cáo chưa có nội dung")
	}
	refs := []uuid.UUID{r.CreatedBy}
	for _, e := range evs {
		refs = append(refs, e.ActorRef)
	}
	for _, p := range content.Programs {
		if u, err := uuid.Parse(p.ManagerRef); err == nil {
			refs = append(refs, u)
		}
	}
	nm := s.names(ctx, refs)
	v := BM11View{BM11Summary: summaryOf(r, last, nm), Version: cev.Seq, ContentHash: cev.ContentHash, Basis: content.Basis,
		Communes: BM11CommunesText(content.Coverage), Coverage: content.Coverage, Groups: domain.BM11Groups, Lines: bm11Lines(content),
		Fishery: content.Fishery, Supplement: content.Supplement, RecipientList: []BM11RecipientView{}, History: []BM11EventView{}}
	if v.Supplement == nil {
		v.Supplement = []domain.BM11Supplement{}
	}
	editable := last.Status == domain.BM11Draft || last.Status == domain.BM11Returned
	for _, p := range content.Programs {
		mr, _ := uuid.Parse(p.ManagerRef)
		v.Programs = append(v.Programs, BM11ProgramView{BM11Program: p, ManagerName: nm[mr],
			CanEdit: editable && w.writer() && (mr == w.a.p.ActorRef || r.CreatedBy == w.a.p.ActorRef || w.approver())})
	}
	if v.Programs == nil {
		v.Programs = []BM11ProgramView{}
	}
	for _, sec := range domain.BM11SectionsOf(r.Type) {
		v.Sections = append(v.Sections, BM11SectionView{Key: sec.Key, Label: sec.Label, Text: content.Sections[sec.Key]})
	}
	received := map[string]store.BM11Event{}
	for _, e := range evs {
		if e.Event == domain.BM11EvReturned {
			v.ReturnComment = e.Comment
		}
		if e.Event == domain.BM11EvSubmitted {
			v.ReturnComment = ""
		}
		if e.Event == domain.BM11EvReceived {
			received[e.Recipient] = e
		}
		v.History = append(v.History, BM11EventView{Seq: e.Seq, Event: e.Event, Status: e.Status, Comment: e.Comment, Recipient: e.Recipient,
			ContentHash: e.ContentHash, ActorName: nm[e.ActorRef], At: e.At.UTC().Format(time.RFC3339)})
	}
	for _, code := range r.Recipients {
		rv := BM11RecipientView{Code: code, Label: domain.BM11RecipientLabel(code)}
		if e, ok := received[code]; ok {
			at := e.At.UTC().Format(time.RFC3339)
			rv.Received, rv.ReceivedAt, rv.ActorName = true, &at, nm[e.ActorRef]
		}
		v.RecipientList = append(v.RecipientList, rv)
	}
	v.Can = BM11Can{Edit: editable && w.writer(), Refresh: editable && w.writer(), Submit: editable && w.writer(),
		Approve: last.Status == domain.BM11Pending && w.approver(), Return: last.Status == domain.BM11Pending && w.approver(),
		Receive: last.Status == domain.BM11Sent && w.recipient != "" && w.recipient != domain.BM11RecipientBGD && !w.a.k.ReadOnly() &&
			slices.Contains(r.Recipients, w.recipient) && received[w.recipient].Seq == 0,
		Word: s.Exporter != nil}
	return v, nil
}

// bm11Load đọc báo cáo + sổ trong một giao dịch ngắn và kiểm quyền xem.
func (s *Service) bm11Load(ctx context.Context, w bm11Who, id uuid.UUID) (store.BM11Report, []store.BM11Event, error) {
	var r *store.BM11Report
	var evs []store.BM11Event
	err := s.tx(ctx, w.a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if r, e = store.BM11ReportByID(ctx, tx, id); e != nil || r == nil {
			return e
		}
		evs, e = store.BM11Events(ctx, tx, id)
		return e
	})
	if err != nil {
		return store.BM11Report{}, nil, err
	}
	if r == nil || len(evs) == 0 || !w.sees(*r, evs[len(evs)-1].Status) {
		return store.BM11Report{}, nil, ErrBM11NotFound
	}
	return *r, evs, nil
}

// GetBM11 trả chi tiết báo cáo.
func (s *Service) GetBM11(ctx context.Context, id uuid.UUID) (BM11View, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return BM11View{}, err
	}
	r, evs, err := s.bm11Load(ctx, w, id)
	if err != nil {
		return BM11View{}, err
	}
	return s.bm11View(ctx, w, r, evs)
}

// BM11ListView là danh sách báo cáo + quyền tạo.
type BM11ListView struct {
	Items     []BM11Summary `json:"items"`
	CanCreate bool          `json:"can_create"`
}

// ListBM11 liệt kê báo cáo của phòng CNTS (Admin/TW truyền province; rỗng = mọi tỉnh). Phòng nhận dùng InboxBM11.
func (s *Service) ListBM11(ctx context.Context, province, typ, status string) (BM11ListView, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return BM11ListView{}, err
	}
	if !w.cnts {
		return BM11ListView{}, errBM11Reader
	}
	if typ != "" && !slices.Contains(domain.BM11Types, typ) {
		return BM11ListView{}, problem.Validation.WithField("type", "week | month | quarter | year")
	}
	prov := w.a.province
	if prov == "" {
		prov = strings.TrimSpace(province)
	}
	return s.bm11List(ctx, w, store.BM11Filter{Province: prov, Type: typ}, status, w.writer())
}

// InboxBM11 là hộp "Báo cáo đến" của phòng nhận (HCTH, KHTC, Ban Giám đốc): báo cáo đã gửi tới phòng mình trong tỉnh.
func (s *Service) InboxBM11(ctx context.Context) (BM11ListView, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return BM11ListView{}, err
	}
	if w.recipient == "" {
		return BM11ListView{}, errBM11Receiver
	}
	return s.bm11List(ctx, bm11Who{a: w.a, recipient: w.recipient}, store.BM11Filter{Province: w.a.province, Recipient: w.recipient},
		domain.BM11Sent, false)
}

func (s *Service) bm11List(ctx context.Context, w bm11Who, f store.BM11Filter, status string, canCreate bool) (BM11ListView, error) {
	var rows []store.BM11Report
	if err := s.tx(ctx, w.a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ListBM11(ctx, tx, f)
		return e
	}); err != nil {
		return BM11ListView{}, err
	}
	refs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		refs = append(refs, r.CreatedBy)
	}
	nm := s.names(ctx, refs)
	out := BM11ListView{Items: []BM11Summary{}, CanCreate: canCreate}
	for _, r := range rows {
		if !w.sees(r, r.Last.Status) || (status != "" && r.Last.Status != status) {
			continue
		}
		out.Items = append(out.Items, summaryOf(r, *r.Last, nm))
	}
	return out, nil
}

// BM11Preview là phần tự động tính thử của một kỳ (màn tạo báo cáo).
type BM11Preview struct {
	Type       string                `json:"type"`
	PeriodKey  string                `json:"period_key"`
	Label      string                `json:"label"`
	From       string                `json:"from"`
	To         string                `json:"to"`
	Weeks      []string              `json:"weeks"`
	DueAt      string                `json:"due_at"`
	MinPct     int                   `json:"min_pct"`
	Ready      bool                  `json:"ready"`
	Reasons    []string              `json:"reasons"`
	Basis      string                `json:"basis"`
	Communes   string                `json:"communes"`
	Coverage   []domain.BM11Coverage `json:"coverage"`
	Groups     []domain.Key          `json:"groups"`
	Lines      []BM11LineView        `json:"lines"`
	Fishery    domain.BM11Catch      `json:"fishery"`
	Recipients []string              `json:"recipients"`
	ExistingID *string               `json:"existing_id"`
}

// PreviewBM11 tính thử phần tự động của kỳ (Cấp Tỉnh CNTS; Admin/TW truyền province).
func (s *Service) PreviewBM11(ctx context.Context, province, typ, key string) (BM11Preview, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return BM11Preview{}, err
	}
	if !w.cnts {
		return BM11Preview{}, errBM11Reader
	}
	prov := w.a.province
	if prov == "" {
		if prov = strings.TrimSpace(province); prov == "" {
			return BM11Preview{}, problem.Validation.WithField("province", "Cấp Quốc gia/Admin phải chọn tỉnh")
		}
	}
	p, err := s.bm11Period(ctx, typ, key)
	if err != nil {
		return BM11Preview{}, err
	}
	c, err := s.bm11Compute(ctx, w.a, prov, p, false)
	if err != nil {
		return BM11Preview{}, err
	}
	v := BM11Preview{Type: p.Type, PeriodKey: p.Key, Label: p.Label, From: p.From.Format(time.DateOnly), To: p.To.Format(time.DateOnly),
		Weeks: c.content.Weeks, DueAt: c.dueAt.UTC().Format(time.RFC3339), MinPct: c.minPct, Ready: c.ready, Reasons: c.reasons,
		Basis: c.content.Basis, Communes: BM11CommunesText(c.content.Coverage), Coverage: c.content.Coverage, Groups: domain.BM11Groups,
		Lines: bm11Lines(c.content), Fishery: c.content.Fishery, Recipients: domain.BM11DefaultRecipients(p.Type)}
	if v.Reasons == nil {
		v.Reasons = []string{}
	}
	_ = s.tx(ctx, w.a.p.TenantID, func(tx pgx.Tx) error {
		r, e := store.BM11ReportByPeriod(ctx, tx, prov, p.Type, p.Key)
		if e == nil && r != nil {
			id := r.ID.String()
			v.ExistingID = &id
		}
		return e
	})
	return v, nil
}

// ---------------------------------------------------------------------------
// Ghi
// ---------------------------------------------------------------------------

// BM11CreateInput là yêu cầu tạo báo cáo.
type BM11CreateInput struct {
	Type, Key  string
	Recipients []string
}

// CreateBM11 tạo báo cáo của kỳ (Cấp Tỉnh CNTS). Gửi lặp cùng Idempotency-Key trả báo cáo đã tạo.
func (s *Service) CreateBM11(ctx context.Context, in BM11CreateInput, idem string) (BM11View, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return BM11View{}, err
	}
	if !w.writer() {
		return BM11View{}, errBM11Writer
	}
	prov := w.a.province
	if prov == "" {
		return BM11View{}, problem.Validation.WithField("province", "Nút của ngữ cảnh chưa gắn tỉnh")
	}
	key := bm11Idem("create", idem)
	// Gửi lặp: trả báo cáo đã tạo, không tính lại.
	var replay *store.BM11Report
	if err := s.tx(ctx, w.a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		replay, e = store.BM11ReportByIdem(ctx, tx, key)
		return e
	}); err != nil {
		return BM11View{}, err
	}
	if replay != nil {
		v, err := s.GetBM11(ctx, replay.ID)
		v.Replayed = true
		return v, err
	}
	p, err := s.bm11Period(ctx, in.Type, strings.TrimSpace(in.Key))
	if err != nil {
		return BM11View{}, err
	}
	recips, err := domain.BM11NormalizeRecipients(p.Type, in.Recipients)
	if err != nil {
		return BM11View{}, problem.Validation.WithField("recipients", err.Error())
	}
	c, err := s.bm11Compute(ctx, w.a, prov, p, true)
	if err != nil {
		return BM11View{}, err
	}
	if !c.ready {
		return BM11View{}, ErrBM11NotReady.WithDetail(strings.Join(c.reasons, "; "))
	}
	c.content.Supplement = s.bm11Supplement(ctx, w.a, prov, p)
	raw, hash, err := c.content.Hash()
	if err != nil {
		return BM11View{}, err
	}
	now := s.now()
	r := store.BM11Report{ID: ids.New(), Province: prov, Type: p.Type, PeriodKey: p.Key, From: p.From, To: p.To, Label: p.Label,
		Recipients: recips, CreatedBy: w.a.p.ActorRef, CreatedAt: now, IdemKey: key}
	err = s.tx(ctx, w.a.p.TenantID, func(tx pgx.Tx) error {
		if e := store.LockBM11Period(ctx, tx, w.a.p.TenantID, prov, p.Type, p.Key); e != nil {
			return e
		}
		if x, e := store.BM11ReportByIdem(ctx, tx, key); e != nil || x != nil {
			if x != nil {
				r = *x
				return errStop
			}
			return e
		}
		if x, e := store.BM11ReportByPeriod(ctx, tx, prov, p.Type, p.Key); e != nil || x != nil {
			if x != nil {
				return ErrBM11Exists.WithDetail("báo cáo " + x.Label + " đã có (mã " + x.ID.String() + ")")
			}
			return e
		}
		if e := store.InsertBM11Report(ctx, tx, w.a.p.TenantID, &r); e != nil {
			return e
		}
		ev := store.BM11Event{ID: ids.New(), ReportID: r.ID, Event: domain.BM11EvCreated, Status: domain.BM11Draft, Content: raw,
			ContentHash: hash, ActorRef: w.a.p.ActorRef, At: now, IdemKey: key}
		return store.InsertBM11Event(ctx, tx, w.a.p.TenantID, prov, &ev)
	})
	replayed := errors.Is(err, errStop)
	if err != nil && !replayed {
		return BM11View{}, bm11Classify(err)
	}
	v, err := s.GetBM11(ctx, r.ID)
	v.Replayed = replayed
	return v, err
}

// bm11Step ghi một bước dưới khóa của báo cáo: kiểm expected_seq (khóa lạc quan), idem (gửi lặp trả bản hiện hành), rồi
// build dựng bước từ báo cáo + sổ hiện hành (nil = không có gì đổi).
func (s *Service) bm11Step(ctx context.Context, w bm11Who, id uuid.UUID, expected *int64, idem string,
	build func(r store.BM11Report, evs []store.BM11Event) (*store.BM11Event, error)) (BM11View, error) {
	if expected == nil {
		return BM11View{}, problem.Validation.WithField("expected_seq", "bắt buộc (khóa lạc quan)")
	}
	anySeq := expected == bm11AnySeq
	if _, _, err := s.bm11Load(ctx, w, id); err != nil {
		return BM11View{}, err
	}
	replayed := false
	err := s.tx(ctx, w.a.p.TenantID, func(tx pgx.Tx) error {
		if e := store.LockBM11(ctx, tx, w.a.p.TenantID, id); e != nil {
			return e
		}
		if x, e := store.BM11EventByIdem(ctx, tx, idem); e != nil || x != nil {
			if x != nil && x.ReportID == id {
				replayed = true
				return nil
			}
			if x != nil {
				return problem.Conflict.WithDetail("Idempotency-Key đã dùng cho báo cáo khác")
			}
			return e
		}
		r, e := store.BM11ReportByID(ctx, tx, id)
		if e != nil || r == nil {
			if e == nil {
				e = ErrBM11NotFound
			}
			return e
		}
		evs, e := store.BM11Events(ctx, tx, id)
		if e != nil {
			return e
		}
		if len(evs) == 0 {
			return ErrBM11NotFound
		}
		if last := evs[len(evs)-1]; !anySeq && last.Seq != *expected {
			return ErrBM11Version.WithDetail(fmt.Sprintf("phiên bản hiện hành %d, đang sửa trên %d", last.Seq, *expected))
		}
		ev, e := build(*r, evs)
		if e != nil || ev == nil {
			return e
		}
		ev.ID, ev.ReportID, ev.ActorRef, ev.At, ev.IdemKey = ids.New(), id, w.a.p.ActorRef, s.now(), idem
		return store.InsertBM11Event(ctx, tx, w.a.p.TenantID, r.Province, ev)
	})
	if err != nil {
		return BM11View{}, bm11Classify(err)
	}
	v, err := s.GetBM11(ctx, id)
	v.Replayed = replayed
	return v, err
}

func editableStatus(evs []store.BM11Event) error {
	if st := evs[len(evs)-1].Status; st != domain.BM11Draft && st != domain.BM11Returned {
		return ErrBM11State.WithDetail("chỉ sửa, làm mới, trình khi báo cáo Đang soạn hoặc bị Trả lại")
	}
	return nil
}

func withContent(ev string, c domain.BM11Content) (*store.BM11Event, error) {
	raw, hash, err := c.Hash()
	if err != nil {
		return nil, err
	}
	return &store.BM11Event{Event: ev, Status: domain.BM11Draft, Content: raw, ContentHash: hash}, nil
}

// BM11EditInput là phần viết sửa (chỉ các khóa gửi lên).
type BM11EditInput struct {
	ExpectedSeq *int64
	Sections    map[string]string // khóa mục → nội dung
	Programs    map[string]string // mã CT/DA → đoạn viết
}

// EditBM11 sửa phần viết (Cấp Tỉnh CNTS; đoạn chương trình: chủ nhiệm/người lập/Lãnh đạo phòng). Phần tự động không sửa tay.
func (s *Service) EditBM11(ctx context.Context, id uuid.UUID, in BM11EditInput, idem string) (BM11View, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return BM11View{}, err
	}
	if !w.writer() {
		return BM11View{}, errBM11Writer
	}
	if len(in.Sections)+len(in.Programs) == 0 {
		return BM11View{}, problem.Validation.WithField("sections", "chưa có mục nào để lưu")
	}
	if len(in.Sections) > 20 || len(in.Programs) > domain.BM11MaxPrograms {
		return BM11View{}, problem.Validation.WithField("sections", "quá nhiều mục trong một lần lưu")
	}
	return s.bm11Step(ctx, w, id, in.ExpectedSeq, bm11Idem("edit", idem), func(r store.BM11Report, evs []store.BM11Event) (*store.BM11Event, error) {
		if err := editableStatus(evs); err != nil {
			return nil, err
		}
		c, _, ok := lastContent(evs)
		if !ok {
			return nil, ErrBM11NotFound
		}
		changed := false
		allowed := map[string]bool{}
		for _, sec := range domain.BM11SectionsOf(r.Type) {
			allowed[sec.Key] = true
		}
		for k, v := range in.Sections {
			if !allowed[k] {
				return nil, problem.Validation.WithField("sections."+k, "mục không có trong mẫu báo cáo "+r.Type)
			}
			t, ok := domain.BM11CheckText(v)
			if !ok {
				return nil, problem.Validation.WithField("sections."+k, fmt.Sprintf("tối đa %d ký tự", domain.BM11MaxSectionRunes))
			}
			if c.Sections[k] != t {
				c.Sections[k], changed = t, true
			}
		}
		for ref, v := range in.Programs {
			i := slices.IndexFunc(c.Programs, func(p domain.BM11Program) bool { return p.Ref == ref })
			if i < 0 {
				return nil, problem.Validation.WithField("programs."+ref, "chương trình không có trong báo cáo")
			}
			mr, _ := uuid.Parse(c.Programs[i].ManagerRef)
			if mr != w.a.p.ActorRef && r.CreatedBy != w.a.p.ActorRef && !w.approver() {
				return nil, errBM11Program
			}
			t, ok := domain.BM11CheckText(v)
			if !ok {
				return nil, problem.Validation.WithField("programs."+ref, fmt.Sprintf("tối đa %d ký tự", domain.BM11MaxSectionRunes))
			}
			if c.Programs[i].Text != t {
				c.Programs[i].Text, changed = t, true
			}
		}
		if !changed {
			return nil, nil
		}
		return withContent(domain.BM11EvEdited, c)
	})
}

// RefreshBM11 tính lại phần tự động (giữ phần viết, đoạn chương trình theo mã) — báo cáo Đang soạn/Trả lại.
func (s *Service) RefreshBM11(ctx context.Context, id uuid.UUID, expected *int64, idem string) (BM11View, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return BM11View{}, err
	}
	if !w.writer() {
		return BM11View{}, errBM11Writer
	}
	r, evs, err := s.bm11Load(ctx, w, id)
	if err != nil {
		return BM11View{}, err
	}
	if err := editableStatus(evs); err != nil {
		return BM11View{}, err
	}
	p, err := s.bm11Period(ctx, r.Type, r.PeriodKey)
	if err != nil {
		return BM11View{}, err
	}
	calc, err := s.bm11Compute(ctx, w.a, r.Province, p, true) // ngoài giao dịch KN
	if err != nil {
		return BM11View{}, err
	}
	calc.content.Supplement = s.bm11Supplement(ctx, w.a, r.Province, p)
	return s.bm11Step(ctx, w, id, expected, bm11Idem("refresh", idem), func(_ store.BM11Report, evs []store.BM11Event) (*store.BM11Event, error) {
		if err := editableStatus(evs); err != nil {
			return nil, err
		}
		old, _, ok := lastContent(evs)
		if !ok {
			return nil, ErrBM11NotFound
		}
		c := calc.content
		c.Sections = old.Sections
		texts := map[string]string{}
		for _, p := range old.Programs {
			texts[p.Ref] = p.Text
		}
		for i := range c.Programs {
			c.Programs[i].Text = texts[c.Programs[i].Ref]
		}
		// Chương trình đã có đoạn viết nhưng không còn trong danh sách (đã kết thúc trước kỳ) — giữ để không mất phần viết.
		for _, p := range old.Programs {
			if p.Text != "" && !slices.ContainsFunc(c.Programs, func(x domain.BM11Program) bool { return x.Ref == p.Ref }) {
				c.Programs = append(c.Programs, p)
			}
		}
		return withContent(domain.BM11EvRefreshed, c)
	})
}

// SubmitBM11 trình báo cáo (Cấp Tỉnh CNTS).
func (s *Service) SubmitBM11(ctx context.Context, id uuid.UUID, expected *int64, idem string) (BM11View, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return BM11View{}, err
	}
	if !w.writer() {
		return BM11View{}, errBM11Writer
	}
	return s.bm11Step(ctx, w, id, expected, bm11Idem("submit", idem), func(_ store.BM11Report, evs []store.BM11Event) (*store.BM11Event, error) {
		if err := editableStatus(evs); err != nil {
			return nil, err
		}
		return &store.BM11Event{Event: domain.BM11EvSubmitted, Status: domain.BM11Pending}, nil
	})
}

// ReturnBM11 trả lại báo cáo Chờ duyệt (Lãnh đạo phòng; ý kiến bắt buộc — AC-04).
func (s *Service) ReturnBM11(ctx context.Context, id uuid.UUID, expected *int64, comment, idem string) (BM11View, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return BM11View{}, err
	}
	if !w.approver() {
		return BM11View{}, errBM11Approver
	}
	comment = strings.TrimSpace(comment)
	if comment == "" || len([]rune(comment)) > domain.BM11MaxCommentRunes {
		return BM11View{}, problem.Validation.WithField("comment", fmt.Sprintf("bắt buộc ghi ý kiến khi trả lại (1–%d ký tự)", domain.BM11MaxCommentRunes))
	}
	return s.bm11Step(ctx, w, id, expected, bm11Idem("return", idem), func(_ store.BM11Report, evs []store.BM11Event) (*store.BM11Event, error) {
		if evs[len(evs)-1].Status != domain.BM11Pending {
			return nil, ErrBM11State.WithDetail("chỉ trả lại báo cáo Chờ duyệt")
		}
		return &store.BM11Event{Event: domain.BM11EvReturned, Status: domain.BM11Returned, Comment: comment}, nil
	})
}

// ApproveBM11 duyệt báo cáo Chờ duyệt — duyệt xong tự gửi theo nơi nhận (BR-03): báo cáo có ngay trong hộp "Báo cáo đến"
// của các phòng nhận (AC-02).
func (s *Service) ApproveBM11(ctx context.Context, id uuid.UUID, expected *int64, idem string) (BM11View, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return BM11View{}, err
	}
	if !w.approver() {
		return BM11View{}, errBM11Approver
	}
	return s.bm11Step(ctx, w, id, expected, bm11Idem("approve", idem), func(_ store.BM11Report, evs []store.BM11Event) (*store.BM11Event, error) {
		if evs[len(evs)-1].Status != domain.BM11Pending {
			return nil, ErrBM11State.WithDetail("chỉ duyệt báo cáo Chờ duyệt")
		}
		return &store.BM11Event{Event: domain.BM11EvApproved, Status: domain.BM11Sent}, nil
	})
}

// ReceiveBM11 phòng nhận (HCTH, KHTC — Cấp Tỉnh, không chế độ chỉ xem) bấm "Đã nhận" (mỗi nơi nhận một lần).
func (s *Service) ReceiveBM11(ctx context.Context, id uuid.UUID, idem string) (BM11View, error) {
	w, err := s.bm11Who(ctx)
	if err != nil {
		return BM11View{}, err
	}
	if w.recipient == "" || w.a.k.ReadOnly() {
		return BM11View{}, errBM11Receiver
	}
	rw := bm11Who{a: w.a, recipient: w.recipient}
	r, evs, err := s.bm11Load(ctx, rw, id)
	if err != nil {
		return BM11View{}, err
	}
	if !slices.Contains(r.Recipients, w.recipient) {
		return BM11View{}, errBM11Receiver
	}
	// Xác nhận không cần khóa lạc quan (các phòng nhận bấm độc lập); trùng nơi nhận → trả bản hiện hành, trigger chặn ghi đôi.
	_ = evs
	return s.bm11Step(ctx, rw, id, bm11AnySeq, bm11Idem("receive", idem), func(_ store.BM11Report, evs2 []store.BM11Event) (*store.BM11Event, error) {
		if evs2[len(evs2)-1].Status != domain.BM11Sent {
			return nil, ErrBM11State
		}
		for _, e := range evs2 {
			if e.Event == domain.BM11EvReceived && e.Recipient == w.recipient {
				return nil, nil // đã xác nhận: trả bản hiện hành
			}
		}
		return &store.BM11Event{Event: domain.BM11EvReceived, Status: domain.BM11Sent, Recipient: w.recipient}, nil
	})
}
