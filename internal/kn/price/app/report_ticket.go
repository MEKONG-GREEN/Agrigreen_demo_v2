package app

// Báo cáo từ phiếu báo giá + lịch (W11.77; BRD Báo giá nông sản v1.3 FN-TI-02.01 Tiến độ nhập giá, FN-TI-02.08 Thống kê
// phiếu báo giá; Q-085 khóa tháng). Chỉ Cấp Tỉnh, Admin xem (Mục IV.2). Trạng thái phiếu chiếu cùng quy tắc với Chi tiết
// báo giá (domain.TicketView — AC-02 FN-TI-02.08), lần nhắc dựng lại từ nhật ký job lịch (W11.74).

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ---------------------------------------------------------------------------------------------------------------------
// FN-TI-02.01 — Tiến độ nhập giá
// ---------------------------------------------------------------------------------------------------------------------

// Bộ lọc trạng thái của Tiến độ nhập giá ngoài mã trạng thái phiếu (domain.View*).
const (
	ProgressIncomplete = "incomplete" // chưa hoàn thiện: Chưa nhập, Bản nháp, Không nộp (BR-01)
	ProgressUnassigned = "unassigned" // xã chưa phân công cán bộ Tỉnh
	ProgressNoOfficer  = "no_officer" // xã chưa có cán bộ Xã
)

// ProgressFilter là bộ lọc Tiến độ nhập giá (B3).
type ProgressFilter struct {
	Day    time.Time
	Region string
	Status string // mã domain.View* hoặc Progress*
	Point  string
	Mine   bool
}

// ProgressRow là một xã trên Tiến độ nhập giá.
type ProgressRow struct {
	TicketRow
	UpdatedAt *time.Time  // lần ghi phiếu gần nhất
	Reminders []time.Time // lần hệ thống đã nhắc trong ngày (AC-03)
}

// ProgressCards là hàng thẻ chỉ số (B2) — đếm trên CÙNG các dòng của bảng sau bộ lọc (AC-02).
type ProgressCards struct {
	Points     int // số xã phải báo giá
	Submitted  int // đã gửi (đủ mặt hàng — BR-03)
	Draft      int
	NotEntered int // chưa nhập (trước giờ khóa)
	Missed     int // không nộp (sau giờ khóa)
	Late       int
	High, Low  int // số mặt hàng cao / thấp bất thường
	Unassigned int
	NoOfficer  int
}

// ProgressView là Tiến độ nhập giá của một ngày.
type ProgressView struct {
	Requested time.Time
	Day       time.Time // ngày báo giá hiển thị (ngày không phải ngày báo giá → ngày báo giá gần nhất trước đó)
	NoQuote   bool      // ngày chọn không phải ngày báo giá ("Không có kỳ báo giá")
	Plan      domain.DayPlan
	Locked    bool
	Board     string
	Now       time.Time
	Cards     ProgressCards
	Rows      []ProgressRow
	Regions   []Ref
}

// progressMatch cho biết dòng khớp bộ lọc trạng thái.
func progressMatch(r ProgressRow, status string) bool {
	switch status {
	case "":
		return true
	case ProgressIncomplete:
		return r.View == domain.ViewNotEntered || r.View == domain.ViewDraft || r.View == domain.ViewMissed
	case ProgressUnassigned:
		return r.Point.Owner == uuid.Nil
	case ProgressNoOfficer:
		return r.Point.Officer == uuid.Nil
	case domain.ViewSubmitted:
		return r.Ticket != nil && r.Ticket.Status == domain.TicketSubmitted
	}
	return r.View == status
}

// reminderTimes dựng lần nhắc của xã trong ngày từ nhật ký job (W11.74 PointReminders): lần 1 tới mọi xã đang hoạt động
// có cán bộ Xã; lần 2 bỏ xã đã gửi phiếu trước lúc nhắc. Xã "Chưa có cán bộ Xã" không được nhắc (luồng ngoại lệ).
func reminderTimes(runs []store.Run, r TicketRow) []time.Time {
	var out []time.Time
	if r.Point.Officer == uuid.Nil {
		return out
	}
	for _, run := range runs {
		switch run.Kind {
		case domain.RunRemind1:
		case domain.RunRemind2:
			if r.Ticket != nil && domain.Sent(r.Ticket.Status) && r.Ticket.SubmittedAt != nil && !r.Ticket.SubmittedAt.After(run.RanAt) {
				continue
			}
		default:
			continue
		}
		if run.Recipients == 0 && run.Targets == 0 {
			continue // lần chạy không gửi tới ai (vd tắt kênh)
		}
		out = append(out, run.RanAt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// Progress đọc Tiến độ nhập giá (FN-TI-02.01): mọi xã đang hoạt động trong ngày báo giá, trạng thái phiếu chiếu tại lúc
// đọc (AC-01, AC-04), số mặt hàng cao / thấp bất thường, lần hệ thống đã nhắc (AC-03); thẻ chỉ số đếm trên bảng đã lọc
// (AC-02). Ngày không phải ngày báo giá → ngày báo giá gần nhất trước đó, NoQuote.
func (s *TicketService) Progress(ctx context.Context, profile string, f ProgressFilter) (ProgressView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return ProgressView{}, err
	}
	if !reviewer(c) {
		return ProgressView{}, errNotReviewer
	}
	v := ProgressView{Requested: s.dayOrToday(f.Day), Now: s.Clock.Now()}
	var runs []store.Run
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		st, err := s.calendarState(ctx, tx, prof)
		if err != nil {
			return err
		}
		const back = 31
		plans, err := s.planRange(ctx, tx, prof.Name, st, v.Requested.AddDate(0, 0, -back), back+1)
		if err != nil {
			return err
		}
		v.Day, v.Plan = v.Requested, plans[len(plans)-1]
		if !v.Plan.Quote {
			v.NoQuote = true
			for i := len(plans) - 2; i >= 0; i-- {
				if plans[i].Quote {
					v.Day, v.Plan = plans[i].Day, plans[i]
					break
				}
			}
		}
		v.Locked = v.Plan.Locked(v.Now)
		if v.Board, err = s.board(ctx, tx, v.Day); err != nil {
			return err
		}
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		regions := domain.EffectiveAt(ps.regions, v.Day)
		rc := reportCtx{regions: regions}
		v.Regions = rc.regionRefs()
		rows, err := s.dayRows(ctx, tx, ps, v.Day, v.Locked, v.Board)
		if err != nil {
			return err
		}
		if runs, err = store.Runs(ctx, tx, prof.Name, v.Day, v.Day); err != nil {
			return err
		}
		for _, r := range rows {
			if c.level == "TINH" {
				r.Mine = r.Point.Owner == c.ref
			}
			pr := ProgressRow{TicketRow: r, Reminders: reminderTimes(runs, r)}
			if r.Ticket != nil {
				at := r.Ticket.EventTime
				pr.UpdatedAt = &at
			}
			if (f.Region != "" && r.Point.RegionCode != f.Region) || (f.Point != "" && r.Point.Code != f.Point) || (f.Mine && !r.Mine) ||
				!progressMatch(pr, f.Status) {
				continue
			}
			v.Rows = append(v.Rows, pr)
		}
		return nil
	})
	if err != nil {
		return ProgressView{}, err
	}
	names := s.officerNames(ctx, prof, c)
	for i := range v.Rows {
		r := &v.Rows[i]
		r.OfficerName, r.OwnerName = names[r.Point.Officer], names[r.Point.Owner]
		v.Cards.add(*r)
	}
	return v, nil
}

func (pc *ProgressCards) add(r ProgressRow) {
	pc.Points++
	switch {
	case r.Ticket != nil && r.Ticket.Status == domain.TicketSubmitted:
		pc.Submitted++
	case r.Ticket != nil && r.Ticket.Status == domain.TicketLate:
		pc.Late++
	case r.View == domain.ViewDraft:
		pc.Draft++
	case r.View == domain.ViewNotEntered:
		pc.NotEntered++
	default:
		pc.Missed++
	}
	pc.High += r.High
	pc.Low += r.Low
	if r.Point.Owner == uuid.Nil {
		pc.Unassigned++
	}
	if r.Point.Officer == uuid.Nil {
		pc.NoOfficer++
	}
}

var progressHeader = []string{"STT", "Nhóm vùng", "Xã báo giá", "Cán bộ Xã", "Cán bộ Tỉnh phụ trách", "Số mặt hàng phải báo giá",
	"Số mặt hàng đã cập nhật", "Không có hàng", "▲ Cao bất thường", "▼ Thấp bất thường", "Trạng thái", "Thời điểm cập nhật",
	"Hệ thống đã nhắc"}

func hm(t time.Time) string { return t.In(clock.Zone()).Format("15:04") }

// ExportProgress xuất danh sách Tiến độ nhập giá theo bộ lọc (luồng phụ FN-TI-02.01).
func (s *TicketService) exportProgressFile(ctx context.Context, profile string, f ProgressFilter) (Export, error) {
	v, err := s.Progress(ctx, profile, f)
	if err != nil {
		return Export{}, err
	}
	rows := make([][]string, 0, len(v.Rows))
	for i, r := range v.Rows {
		officer, owner := r.OfficerName, r.OwnerName
		if r.Point.Officer == uuid.Nil {
			officer = "Chưa có cán bộ Xã"
		}
		if r.Point.Owner == uuid.Nil {
			owner = "Chưa phân công"
		}
		updated := ""
		if r.UpdatedAt != nil {
			updated = r.UpdatedAt.In(clock.Zone()).Format("02/01/2006 15:04")
		}
		var rem []string
		for _, t := range r.Reminders {
			rem = append(rem, hm(t))
		}
		view := ticketViewVI[r.View]
		if r.View == domain.ViewNotEntered {
			view = "Chưa nhập"
		}
		rows = append(rows, []string{strconv.Itoa(i + 1), r.RegionName, r.Point.Name + " (" + r.Point.Code + ")", officer, owner,
			strconv.Itoa(r.Items), strconv.Itoa(r.Priced + r.NoGoods), strconv.Itoa(r.NoGoods), strconv.Itoa(r.High), strconv.Itoa(r.Low),
			view, updated, strings.Join(rem, ", ")})
	}
	return xlsxExport("Tiến độ nhập giá", progressHeader, rows, "tien-do-nhap-gia-"+v.Day.Format(time.DateOnly)+".xlsx")
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TI-02.08 — Thống kê phiếu báo giá (theo cán bộ Xã) + khóa tháng (Q-085)
// ---------------------------------------------------------------------------------------------------------------------

// StatsFilter là bộ lọc Thống kê phiếu (B1): tháng hoặc từ ngày – đến ngày.
type StatsFilter struct {
	Month    time.Time // ngày bất kỳ trong tháng (ưu tiên nếu khác zero)
	From, To time.Time
	Region   string
	Point    string
}

// StatLine là một dòng thống kê đã gắn tên.
type StatLine struct {
	domain.StatRow
	OfficerName string
	PointName   string
	RegionName  string
}

// StatsCards là hàng thẻ chỉ số (B2).
type StatsCards struct {
	QuoteDays int // số ngày báo giá trong khoảng (theo lịch và ngoại lệ)
	Required  int
	OnTime    int
	Late      int
	Missed    int
}

// StatsView là Thống kê phiếu báo giá.
type StatsView struct {
	From, To  time.Time
	Monthly   bool              // khoảng là đúng một tháng dương lịch
	Month     *domain.StatMonth // trạng thái khóa tháng (Monthly)
	Snapshot  bool              // số liệu đọc từ ảnh chụp tháng đã khóa
	Pending   int               // ngày báo giá trong khoảng chưa qua giờ khóa (chưa tính)
	Cards     StatsCards
	Rows      []StatLine
	CanClose  bool
	CanReopen bool
	Regions   []Ref
}

// statsRange chuẩn hóa khoảng thống kê.
func (s *TicketService) statsRange(f StatsFilter) (time.Time, time.Time, bool, error) {
	if !f.Month.IsZero() {
		from, to := domain.MonthOf(dayOf(f.Month))
		return from, to, true, nil
	}
	if f.From.IsZero() && f.To.IsZero() {
		from, to := domain.MonthOf(domain.BusinessDay(s.Clock.Now()))
		return from, to, true, nil
	}
	from, to := dayOf(f.From), dayOf(f.To)
	if f.From.IsZero() || f.To.IsZero() || to.Before(from) {
		return from, to, false, problem.Validation.WithField("to", "cần từ ngày ≤ đến ngày")
	}
	if to.Sub(from) > 366*24*time.Hour {
		return from, to, false, problem.Validation.WithField("to", "khoảng tối đa 366 ngày")
	}
	mf, mt := domain.MonthOf(from)
	return from, to, from.Equal(mf) && to.Equal(mt), nil
}

// statDays dựng mọi ngày phải nộp trong [from, to] (BR-01: ngày báo giá đã qua giờ khóa, xã hoạt động, có cán bộ Xã phụ
// trách; phiếu nhập thay tính cho cán bộ phụ trách tại ngày — BR-02). Trả số ngày báo giá và số ngày chưa qua giờ khóa.
func (s *TicketService) statDays(ctx context.Context, tx pgx.Tx, prof port.Profile, ps *pointSnap, from, to, now time.Time) ([]domain.StatDay,
	int, int, error) {
	st, err := s.calendarState(ctx, tx, prof)
	if err != nil {
		return nil, 0, 0, err
	}
	plans, err := s.planRange(ctx, tx, prof.Name, st, from, int(to.Sub(from)/(24*time.Hour))+1)
	if err != nil {
		return nil, 0, 0, err
	}
	ts, err := store.LoadTickets(ctx, tx, store.TicketFilter{From: from, To: to})
	if err != nil {
		return nil, 0, 0, err
	}
	type pd struct {
		point string
		day   string
	}
	live := map[pd]store.Ticket{}
	deleted := map[pd]int{}
	var liveIDs []uuid.UUID
	for _, t := range ts {
		k := pd{t.PointCode, t.Day.Format(time.DateOnly)}
		if t.Status == domain.TicketDeleted {
			deleted[k]++
			continue
		}
		live[k] = t
		liveIDs = append(liveIDs, t.ID)
	}
	lines, err := store.LoadLines(ctx, tx, liveIDs)
	if err != nil {
		return nil, 0, 0, err
	}
	adjusted := map[uuid.UUID]int{}
	for _, l := range lines {
		if l.AdjMin != nil {
			adjusted[l.TicketID]++
		}
	}
	var (
		out             []domain.StatDay
		quote, unlocked int
	)
	for _, p := range plans {
		if !p.Quote {
			continue
		}
		quote++
		if !p.Locked(now) {
			unlocked++
			continue
		}
		states := ps.states(p.Day)
		codes := make([]string, 0, len(states))
		for code, st := range states {
			if st.State == domain.PointStateActive {
				codes = append(codes, code)
			}
		}
		sort.Strings(codes)
		for _, code := range codes {
			pt := ps.portPoint(states[code], p.Day)
			if pt.Officer == uuid.Nil {
				continue
			}
			k := pd{code, p.Day.Format(time.DateOnly)}
			d := domain.StatDay{Day: p.Day, Point: code, Region: pt.RegionCode, Officer: pt.Officer, Result: domain.StatMissed, Deleted: deleted[k]}
			if t, ok := live[k]; ok {
				d.Result, d.Ticket, d.Adjusted = domain.StatOf(t.Status), t.ID, adjusted[t.ID]
			}
			out = append(out, d)
		}
	}
	return out, quote, unlocked, nil
}

// Stats đọc Thống kê phiếu báo giá theo cán bộ Xã (FN-TI-02.08). Tháng đã khóa (Q-085) đọc ảnh chụp lúc khóa.
func (s *TicketService) Stats(ctx context.Context, profile string, f StatsFilter) (StatsView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return StatsView{}, err
	}
	if !reviewer(c) {
		return StatsView{}, errNotReviewer
	}
	from, to, monthly, err := s.statsRange(f)
	if err != nil {
		return StatsView{}, err
	}
	now := s.Clock.Now()
	v := StatsView{From: from, To: to, Monthly: monthly}
	var rows []domain.StatRow
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		rc := reportCtx{regions: domain.EffectiveAt(ps.regions, to), points: ps.states(to)}
		v.Regions = rc.regionRefs()
		days, quote, pending, err := s.statDays(ctx, tx, prof, ps, from, to, now)
		if err != nil {
			return err
		}
		v.Cards.QuoteDays, v.Pending = quote, pending
		rows = domain.TallyStats(days)
		if monthly {
			m, err := store.CurrentStatMonth(ctx, tx, prof.Name, from)
			if err != nil {
				return err
			}
			v.Month = &m
			if m.Closed() {
				if rows, err = store.LoadStatLines(ctx, tx, prof.Name, from, m.Version); err != nil {
					return err
				}
				v.Snapshot = true
			}
			can := approver(prof, c)
			v.CanClose = can && !m.Closed() && pending == 0 && to.Before(domain.BusinessDay(now))
			v.CanReopen = can && m.Closed()
		}
		for _, r := range rows {
			if (f.Region != "" && r.Region != f.Region) || (f.Point != "" && r.Point != f.Point) {
				continue
			}
			pt := ps.states(to)[r.Point]
			name := pt.Name
			if name == "" {
				name = r.Point
			}
			v.Rows = append(v.Rows, StatLine{StatRow: r, PointName: name, RegionName: rc.regionName(r.Region)})
			v.Cards.Required += r.Required
			v.Cards.OnTime += r.OnTime
			v.Cards.Late += r.Late
			v.Cards.Missed += r.Missed
		}
		return nil
	})
	if err != nil {
		return StatsView{}, err
	}
	names := s.officerNames(ctx, prof, c)
	for i := range v.Rows {
		v.Rows[i].OfficerName = names[v.Rows[i].Officer]
	}
	return v, nil
}

// StatDayRow là một ngày phải nộp của một cán bộ tại một xã (luồng phụ — danh sách phiếu theo ngày và trạng thái).
type StatDayRow struct {
	domain.StatDay
	View string // trạng thái phiếu như Chi tiết báo giá (AC-02)
}

// StatDetail trả các ngày phải nộp của cán bộ officer tại xã point trong khoảng (số liệu hiện tại, kể cả tháng đã khóa).
func (s *TicketService) StatDetail(ctx context.Context, profile string, f StatsFilter, officer uuid.UUID) ([]StatDayRow, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	if !reviewer(c) {
		return nil, errNotReviewer
	}
	from, to, _, err := s.statsRange(f)
	if err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	var out []StatDayRow
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		days, _, _, err := s.statDays(ctx, tx, prof, ps, from, to, now)
		if err != nil {
			return err
		}
		bs, err := store.BoardsBetween(ctx, tx, prof.Name, from, to)
		if err != nil {
			return err
		}
		boardOf := map[string]string{}
		for d, b := range bs {
			boardOf[d.Format(time.DateOnly)] = b.View(now)
		}
		for _, d := range days {
			if d.Officer != officer || (f.Point != "" && d.Point != f.Point) {
				continue
			}
			status := ""
			switch d.Result {
			case domain.StatOnTime:
				status = domain.TicketSubmitted
			case domain.StatLate:
				status = domain.TicketLate
			case domain.StatMissed:
				if d.Ticket != uuid.Nil {
					status = domain.TicketDraft
				}
			}
			out = append(out, StatDayRow{StatDay: d, View: domain.TicketView(status, true, boardOf[d.Day.Format(time.DateOnly)])})
		}
		return nil
	})
	return out, err
}

var statsHeader = []string{"STT", "Cán bộ Xã", "Xã báo giá", "Nhóm vùng", "Phải nộp", "Đúng hạn", "Nộp muộn", "Không nộp", "Bị xóa",
	"Số dòng bị hiệu chỉnh"}

// ExportStats xuất Thống kê phiếu ra Excel (B3).
func (s *TicketService) exportStatsFile(ctx context.Context, profile string, f StatsFilter) (Export, error) {
	v, err := s.Stats(ctx, profile, f)
	if err != nil {
		return Export{}, err
	}
	rows := make([][]string, 0, len(v.Rows))
	for i, r := range v.Rows {
		name := r.OfficerName
		if name == "" {
			name = r.Officer.String()
		}
		rows = append(rows, []string{strconv.Itoa(i + 1), name, r.PointName + " (" + r.Point + ")", r.RegionName, strconv.Itoa(r.Required),
			strconv.Itoa(r.OnTime), strconv.Itoa(r.Late), strconv.Itoa(r.Missed), strconv.Itoa(r.Deleted), strconv.Itoa(r.AdjustedLines)})
	}
	return xlsxExport("Thống kê phiếu báo giá", statsHeader, rows,
		"thong-ke-phieu-bao-gia-"+v.From.Format(time.DateOnly)+"_"+v.To.Format(time.DateOnly)+".xlsx")
}

// StatMonthInput là thao tác khóa / mở lại tháng: Seq = phiên bản hiện hành người gọi đã thấy (0 = chưa khóa lần nào).
type StatMonthInput struct {
	Month  time.Time
	Seq    int64
	Reason string
}

// statMonthOp chạy một thao tác khóa tháng dưới khóa của (tenant, bên dùng, tháng). Chỉ Quản trị nghiệp vụ báo giá (D11 —
// cùng quy tắc duyệt bảng giá; Admin chỉ khi Profile.Board.AdminApproves).
func (s *TicketService) statMonthOp(ctx context.Context, profile string, in StatMonthInput, fn func(tx pgx.Tx, prof port.Profile, c caller,
	month time.Time, cur domain.StatMonth, now time.Time) error) (StatsView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return StatsView{}, err
	}
	if !approver(prof, c) {
		if c.level == "TINH" {
			return StatsView{}, port.ErrBoardStewardRequired.WithDetail("chỉ Quản trị nghiệp vụ báo giá khóa / mở lại thống kê tháng")
		}
		return StatsView{}, problem.Forbidden.WithDetail("chỉ Quản trị nghiệp vụ báo giá khóa / mở lại thống kê tháng")
	}
	if in.Month.IsZero() {
		return StatsView{}, problem.Validation.WithField("month", "bắt buộc")
	}
	month, _ := domain.MonthOf(dayOf(in.Month))
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if err := store.LockStatMonth(ctx, tx, c.tenant, prof.Name, month); err != nil {
			return err
		}
		cur, err := store.CurrentStatMonth(ctx, tx, prof.Name, month)
		if err != nil {
			return err
		}
		if cur.Seq != in.Seq {
			return problem.Conflict.WithDetail("trạng thái khóa tháng đã đổi — tải lại")
		}
		return fn(tx, prof, c, month, cur, s.Clock.Now())
	})
	if err != nil {
		return StatsView{}, mapLedgerErr(err)
	}
	return s.Stats(ctx, profile, StatsFilter{Month: month})
}

// CloseStatMonth khóa thống kê phiếu của một tháng đã kết thúc (Q-085): chụp số liệu theo cán bộ Xã thành phiên bản mới.
func (s *TicketService) CloseStatMonth(ctx context.Context, profile string, in StatMonthInput, idem string) (StatsView, error) {
	return s.statMonthOp(ctx, profile, in, func(tx pgx.Tx, prof port.Profile, c caller, month time.Time, cur domain.StatMonth, now time.Time) error {
		if cur.Closed() {
			return port.ErrStatMonthState.WithDetail("tháng đã khóa")
		}
		_, to := domain.MonthOf(month)
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		days, _, pending, err := s.statDays(ctx, tx, prof, ps, month, to, now)
		if err != nil {
			return err
		}
		if pending > 0 || !to.Before(domain.BusinessDay(now)) {
			return port.ErrStatMonthOpen
		}
		rows := domain.TallyStats(days)
		version := cur.Version + 1
		n := len(rows)
		if err := store.InsertStatMonth(ctx, tx, ids.New(), c.tenant, prof.Name, month, domain.StatMonthClose, domain.StatMonthClosed, version,
			"", &n, now, c.ref, idemKey(idem, "statmonth:close")); err != nil {
			return err
		}
		for _, r := range rows {
			if err := store.InsertStatLine(ctx, tx, ids.New(), c.tenant, prof.Name, month, version, r, now, c.ref,
				idemKey(idem, "statline:"+r.Officer.String()+":"+r.Point)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReopenStatMonth mở lại tháng đã khóa (lý do bắt buộc) — thống kê tính lại từ phiếu tới lần khóa sau.
func (s *TicketService) ReopenStatMonth(ctx context.Context, profile string, in StatMonthInput, idem string) (StatsView, error) {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return StatsView{}, port.ErrReasonRequired.WithDetail("lý do mở lại thống kê tháng")
	}
	return s.statMonthOp(ctx, profile, in, func(tx pgx.Tx, prof port.Profile, c caller, month time.Time, cur domain.StatMonth, now time.Time) error {
		if !cur.Closed() {
			return port.ErrStatMonthState.WithDetail("tháng chưa khóa")
		}
		return store.InsertStatMonth(ctx, tx, ids.New(), c.tenant, prof.Name, month, domain.StatMonthReopen, domain.StatMonthOpen, cur.Version,
			reason, nil, now, c.ref, idemKey(idem, "statmonth:reopen"))
	})
}
