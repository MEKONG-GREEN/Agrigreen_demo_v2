package app

// Phiếu báo giá + xác minh (W11.75, pf 000148; BRD Báo giá nông sản v1.3 FN-XA-03.01…03, 03.05, FN-TI-02.02…04, 02.09;
// Mục II.3, II.4). Tệp này: TicketService, điểm nối, ảnh một ngày, màn nhập (phiếu của một xã một ngày), cán bộ Xã
// nhập / lưu nháp / gửi, sinh cảnh báo tạm tính. ticket_review.go: Cấp Tỉnh nhập thay, hiệu chỉnh, sửa ngày, xóa phiếu,
// yêu cầu xác minh, quyết định; Xã phản hồi. ticket_read.go: danh sách, lịch sử, nhật ký, xuất/in, port.Tickets, hook.
//
// Quyền:
//   - Cấp Xã: xem, nhập phiếu của xã mình phụ trách chính TẠI NGÀY báo giá (W11.72 phân công); chỉ nhập ngày báo giá
//     hôm nay, trước giờ khóa (port DayPlan.Locked — chặn đúng giờ, không đợi job); phản hồi xác minh của xã mình.
//   - Cấp Tỉnh: xem mọi xã (BR-01 FN-TI-02.02); sửa / nhập thay / xóa / yêu cầu xác minh / quyết định chỉ xã được phân
//     công hoặc chưa phân công (Point.HandledBy — BR-02 FN-TI-02.02, BR-04 FN-TI-02.03, 02.09).
//   - Vai quản trị (Admin): như Cấp Tỉnh với mọi xã (hỗ trợ theo đề nghị của Phòng TTTT — BR-01 FN-TI-02.04).
//   - Cấp TW: không đọc phiếu (xem bảng giá đã duyệt — W11.76).

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// TicketHooks là điểm nối của gói kế tiếp (W11.76 duyệt / công bố bảng giá ngày). Trường nil → mặc định ghi bên cạnh.
type TicketHooks struct {
	// Board: trạng thái bảng giá ngày day (domain.BoardOpen | BoardApproving | BoardApproved | BoardReopened) trong giao
	// dịch tx. nil → BoardOpen mọi ngày (chưa có duyệt): Cấp Tỉnh sửa được mọi ngày đã khóa; giá tham chiếu (CT-02) lấy
	// từ mọi phiếu đã gửi của N ngày báo giá trước. Nối → giá tham chiếu chỉ từ ngày đã có bảng giá duyệt.
	Board func(ctx context.Context, tx pgx.Tx, day time.Time) (string, error)
	// Approved (W11.76): ngày day đã có bảng giá duyệt (vN ≥ 1 — kể cả đang mở duyệt / đang duyệt lại) — giá tham chiếu
	// CT-02 chỉ lấy ngày này. nil → dùng Board ≠ Chưa duyệt.
	Approved func(ctx context.Context, tx pgx.Tx, day time.Time) (bool, error)
}

// TicketService cài đặt phiếu báo giá, port.Tickets và các hook ItemHooks.HasPrice/HasSheets, PointHooks.Submitted.
type TicketService struct {
	*Service
	Hooks TicketHooks
	// PDF: bộ dựng PDF (pf/reports qua gốc lắp ráp) cho "Tải PDF" phiếu. nil → 503 price.pdf_unavailable.
	PDF port.TablePDF
	// Weeks (W11.76): lịch kỳ hệ thống (pf/extension qua gốc lắp ráp) cho khóa tuần lưu lúc duyệt bảng giá. nil → quy tắc
	// mặc định QĐ-6 (domain.DefaultWeekOf).
	Weeks port.Weeks
	// Egress (W11.77, tùy chọn — G5 ADR-0024, quyết định A5): điểm kiểm soát xuất dùng chung (pf/metering) cho "Xuất
	// Excel / PDF" báo cáo giá — xuất NỘI BỘ: ghi egress_event, không bao giờ chặn. nil → không ghi.
	Egress     meter.Checkpoint
	EgressMode meter.Mode
	EgressLog  *slog.Logger
}

var _ port.Tickets = (*TicketService)(nil)

// NewTicketService dựng TicketService trên Service của module.
func NewTicketService(s *Service) *TicketService { return &TicketService{Service: s} }

func (s *TicketService) board(ctx context.Context, tx pgx.Tx, day time.Time) (string, error) {
	if s.Hooks.Board == nil {
		return domain.BoardOpen, nil
	}
	b, err := s.Hooks.Board(ctx, tx, day)
	if b == "" {
		b = domain.BoardOpen
	}
	return b, err
}

// dayPlan là kế hoạch lịch W11.74 của ngày day cho bên dùng prof (trong giao dịch).
func (s *Service) dayPlan(ctx context.Context, tx pgx.Tx, prof port.Profile, day time.Time) (domain.DayPlan, error) {
	st, err := s.calendarState(ctx, tx, prof)
	if err != nil {
		return domain.DayPlan{}, err
	}
	ps, err := s.planRange(ctx, tx, prof.Name, st, day, 1)
	if err != nil {
		return domain.DayPlan{}, err
	}
	return ps[0], nil
}

// dayCat là danh mục mặt hàng nhìn từ một ngày (dùng chung cho nhiều xã trong một giao dịch).
type dayCat struct {
	day     time.Time
	states  map[string]domain.ItemState
	assigns map[string]domain.Ver[domain.Assignment]
	gate    map[string]bool
	order   map[string]int
	groups  map[string]string
	units   map[string]string
}

func loadDayCat(ctx context.Context, tx pgx.Tx, day time.Time) (*dayCat, error) {
	sn, err := loadSnap(ctx, tx)
	if err != nil {
		return nil, err
	}
	d := &dayCat{day: day, states: domain.ItemStates(sn.vers, day), assigns: domain.EffectiveAt(sn.assigns, day),
		gate: domain.Gate(domain.EffectiveAt(sn.sets, day)), order: sn.groupOrder(day), groups: map[string]string{}, units: map[string]string{}}
	gs, err := store.LoadEntries(ctx, tx, domain.KindGroup)
	if err != nil {
		return nil, err
	}
	for code, g := range domain.EffectiveAt(gs, day) {
		d.groups[code] = g.V.Name
	}
	us, err := store.LoadEntries(ctx, tx, domain.KindUnit)
	if err != nil {
		return nil, err
	}
	for code, u := range domain.EffectiveAt(us, day) {
		d.units[code] = u.V.Name
	}
	return d, nil
}

// pointItems trả mặt hàng xã point phải báo giá tại ngày (bên gọi đã kiểm xã đang hoạt động).
func (d *dayCat) pointItems(point string) []domain.ItemState {
	return domain.PointItems(d.states, d.assigns, d.gate, point, d.order)
}

func (d *dayCat) groupName(code string) string {
	if n, ok := d.groups[code]; ok {
		return n
	}
	return code
}

func (d *dayCat) unitName(code string) string {
	if n, ok := d.units[code]; ok {
		return n
	}
	return code
}

// ---------------------------------------------------------------------------------------------------------------------
// Giá tham chiếu (CT-02)
// ---------------------------------------------------------------------------------------------------------------------

// RefPrice là giá tham chiếu của một mặt hàng tại một xã: P (và cặp giá dùng cho "Lấy giá kỳ trước" — BR-05) của lần báo
// giá gần nhất trong N ngày báo giá liền trước.
type RefPrice struct {
	Min, Max, Mid decimal.Decimal
	Day           time.Time
}

type alertKey struct {
	ticket uuid.UUID
	item   string
}

// alertBook là các phiên bản cảnh báo của một nhóm phiếu: phiên bản mới nhất và lịch sử mỗi chuỗi.
type alertBook struct {
	latest  map[alertKey]store.Alert
	history map[alertKey][]store.Alert
}

func bookAlerts(as []store.Alert) alertBook {
	b := alertBook{latest: map[alertKey]store.Alert{}, history: map[alertKey][]store.Alert{}}
	for _, a := range as {
		k := alertKey{a.TicketID, a.ItemCode}
		b.history[k] = append(b.history[k], a)
		b.latest[k] = a // LoadAlerts sắp theo seq
	}
	return b
}

func (b alertBook) excluded(t uuid.UUID, item string) bool {
	a, ok := b.latest[alertKey{t, item}]
	return ok && a.Status == domain.AlertExcluded
}

// lookback trả N (số ngày báo giá tìm giá kỳ trước — CT-02).
func lookback(set domain.ThresholdSet) int {
	if set.Default.Params != nil && set.Default.Params.LookbackDays > 0 {
		return set.Default.Params.LookbackDays
	}
	return 7
}

// refPrices trả giá tham chiếu của mọi mặt hàng xã point có giá trong N ngày báo giá liền trước day (CT-02): phiếu Đã
// gửi / Nộp muộn, giá sau hiệu chỉnh nếu có, bỏ dòng Đã loại, Không có hàng; khi bảng giá ngày đã nối (W11.76) chỉ lấy
// ngày đã có bảng giá duyệt. Không có → mặt hàng vắng ở kết quả ("Giá mới").
func (s *TicketService) refPrices(ctx context.Context, tx pgx.Tx, prof port.Profile, set domain.ThresholdSet, point string,
	day time.Time) (map[string]RefPrice, error) {
	n := lookback(set)
	window := n*7 + 14
	st, err := s.calendarState(ctx, tx, prof)
	if err != nil {
		return nil, err
	}
	plans, err := s.planRange(ctx, tx, prof.Name, st, day.AddDate(0, 0, -window), window)
	if err != nil {
		return nil, err
	}
	var days []time.Time
	for _, p := range plans {
		if p.Quote {
			days = append(days, p.Day)
		}
	}
	if len(days) > n {
		days = days[len(days)-n:]
	}
	out := map[string]RefPrice{}
	if len(days) == 0 {
		return out, nil
	}
	allowed := map[time.Time]bool{}
	for _, d := range days {
		allowed[d] = true
		switch {
		case s.Hooks.Approved != nil:
			ok, err := s.Hooks.Approved(ctx, tx, d)
			if err != nil {
				return nil, err
			}
			allowed[d] = ok
		case s.Hooks.Board != nil:
			b, err := s.board(ctx, tx, d)
			if err != nil {
				return nil, err
			}
			allowed[d] = b != domain.BoardOpen
		}
	}
	ts, err := store.LoadTickets(ctx, tx, store.TicketFilter{Point: point, From: days[0], To: days[len(days)-1], Live: true})
	if err != nil {
		return nil, err
	}
	var sent []store.Ticket
	for _, t := range ts {
		if domain.Sent(t.Status) && allowed[t.Day] {
			sent = append(sent, t)
		}
	}
	if len(sent) == 0 {
		return out, nil
	}
	sort.Slice(sent, func(i, j int) bool { return sent[i].Day.After(sent[j].Day) })
	idList := make([]uuid.UUID, 0, len(sent))
	for _, t := range sent {
		idList = append(idList, t.ID)
	}
	lines, err := store.LoadLines(ctx, tx, idList)
	if err != nil {
		return nil, err
	}
	as, err := store.LoadAlerts(ctx, tx, idList)
	if err != nil {
		return nil, err
	}
	book := bookAlerts(as)
	byTicket := map[uuid.UUID][]store.Line{}
	for _, l := range lines {
		byTicket[l.TicketID] = append(byTicket[l.TicketID], l)
	}
	for _, t := range sent {
		for _, l := range byTicket[t.ID] {
			if _, have := out[l.Line.ItemCode]; have || book.excluded(t.ID, l.Line.ItemCode) {
				continue
			}
			lo, hi, ok := domain.Effective(l.Line, l.AdjMin, l.AdjMax)
			if !ok {
				continue
			}
			out[l.Line.ItemCode] = RefPrice{Min: lo, Max: hi, Mid: domain.Mid(lo, hi), Day: t.Day}
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------------------------------------------------
// Màn nhập / chi tiết phiếu
// ---------------------------------------------------------------------------------------------------------------------

// AlertInfo là một cảnh báo của dòng giá: phiên bản mới nhất, trạng thái nhìn tại lúc đọc, trao đổi xác minh.
type AlertInfo struct {
	store.Alert
	View     string
	History  []store.Alert
	Response *store.Alert // phản hồi gần nhất của Xã (phiên bản 'responded'), nếu có
	Request  *store.Alert // yêu cầu xác minh gần nhất, nếu có
}

// SheetLine là một mặt hàng trên phiếu.
type SheetLine struct {
	Item      domain.ItemState
	GroupName string
	UnitName  string
	Ref       *RefPrice
	Threshold decimal.Decimal
	Entry     domain.Line // giá cán bộ Xã gửi (hoặc nhập thay); trống = chưa nhập
	Entered   bool
	AdjMin    *decimal.Decimal
	AdjMax    *decimal.Decimal
	AdjReason string
	Alert     *AlertInfo
	// Eval: so giá đang ghi với giá tham chiếu (hiển thị ▲/▼ ngay ở màn nhập — BR-08 FN-TI-02.03).
	Eval domain.Evaluation
}

// SheetCan là thao tác người gọi làm được trên phiếu tại lúc đọc.
type SheetCan struct {
	Enter   bool // Cấp Xã nhập / lưu nháp / gửi
	Proxy   bool // nhập thay (Không nộp)
	Correct bool // hiệu chỉnh
	Redate  bool
	Delete  bool
	Review  bool // yêu cầu xác minh / quyết định cảnh báo
	Respond bool // Cấp Xã phản hồi xác minh
}

// Sheet là phiếu báo giá của một xã một ngày (màn Nhập báo giá FN-XA-03.01, Sửa phiếu FN-TI-02.04, Nhập thay 02.09).
type Sheet struct {
	Point       port.Point
	RegionName  string
	PointActive bool
	Day         time.Time
	Plan        domain.DayPlan
	Locked      bool
	Board       string
	Now         time.Time
	Ticket      *store.Ticket
	View        string
	Lines       []SheetLine
	Can         SheetCan
	OfficerName string
	OwnerName   string
	ActorName   string // người nhập thay / người ghi gần nhất (Cấp Tỉnh, Admin)
}

// handles cho biết người gọi xử lý được xã (Cấp Tỉnh phân công / chưa phân công; vai quản trị mọi xã).
func handles(c caller, p port.Point) bool {
	return c.manage || (c.level == "TINH" && p.HandledBy(c.ref))
}

// sheetCtx là ảnh dùng chung khi dựng phiếu trong một giao dịch.
type sheetCtx struct {
	prof port.Profile
	c    caller
	ps   *pointSnap
	now  time.Time
	set  domain.ThresholdSet
}

func (s *TicketService) newSheetCtx(ctx context.Context, tx pgx.Tx, prof port.Profile, c caller) (*sheetCtx, error) {
	ps, err := loadPointSnap(ctx, tx)
	if err != nil {
		return nil, err
	}
	set, _, _, err := s.thresholdSet(ctx, tx, prof)
	if err != nil {
		return nil, err
	}
	return &sheetCtx{prof: prof, c: c, ps: ps, now: s.Clock.Now(), set: set}, nil
}

// pointAt trả xã báo giá tại ngày (cả khi không hoạt động — phiếu cũ của xã đã ngưng vẫn xem được).
func (sc *sheetCtx) pointAt(code string, day time.Time) (port.Point, bool, bool) {
	st, ok := sc.ps.states(day)[code]
	if !ok {
		return port.Point{}, false, false
	}
	return sc.ps.portPoint(st, day), true, st.State == domain.PointStateActive
}

// canView kiểm quyền xem phiếu của xã p tại ngày: Cấp Xã chỉ xã mình phụ trách tại ngày; TW không xem phiếu.
func canView(c caller, p port.Point) error {
	switch {
	case c.manage, c.level == "TINH":
		return nil
	case c.level == "XA":
		if p.Officer == c.ref {
			return nil
		}
		return port.ErrPointNotAssigned
	}
	return problem.Forbidden.WithDetail("Cấp Quốc gia xem bảng giá đã duyệt, không xem phiếu báo giá")
}

// liveTicket trả phiếu còn hiệu lực của xã tại ngày (nil = chưa có).
func liveTicket(ctx context.Context, tx pgx.Tx, point string, day time.Time) (*store.Ticket, error) {
	ts, err := store.LoadTickets(ctx, tx, store.TicketFilter{Point: point, From: day, To: day, Live: true})
	if err != nil || len(ts) == 0 {
		return nil, err
	}
	t := ts[len(ts)-1]
	return &t, nil
}

func ticketByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (store.Ticket, error) {
	ts, err := store.LoadTickets(ctx, tx, store.TicketFilter{IDs: []uuid.UUID{id}})
	if err != nil {
		return store.Ticket{}, err
	}
	if len(ts) == 0 {
		return store.Ticket{}, port.ErrTicketNotFound
	}
	return ts[0], nil
}

// buildSheet dựng phiếu của xã point ngày day; t = phiếu cụ thể (nil → phiếu còn hiệu lực của ngày, nếu có).
func (s *TicketService) buildSheet(ctx context.Context, tx pgx.Tx, sc *sheetCtx, point string, day time.Time, t *store.Ticket) (Sheet, error) {
	p, ok, active := sc.pointAt(point, day)
	if !ok {
		return Sheet{}, port.ErrPointNotFound.WithDetail("xã báo giá " + point)
	}
	if err := canView(sc.c, p); err != nil {
		return Sheet{}, err
	}
	plan, err := s.dayPlan(ctx, tx, sc.prof, day)
	if err != nil {
		return Sheet{}, err
	}
	b, err := s.board(ctx, tx, day)
	if err != nil {
		return Sheet{}, err
	}
	sh := Sheet{Point: p, PointActive: active, Day: day, Plan: plan, Locked: plan.Locked(sc.now), Board: b, Now: sc.now}
	if r, ok := domain.EffectiveAt(sc.ps.regions, day)[p.RegionCode]; ok {
		sh.RegionName = r.V.Name
	}
	if t == nil {
		if t, err = liveTicket(ctx, tx, point, day); err != nil {
			return Sheet{}, err
		}
	}
	sh.Ticket = t
	status := ""
	if t != nil {
		status = t.Status
	}
	sh.View = domain.TicketView(status, sh.Locked, b)
	cat, err := loadDayCat(ctx, tx, day)
	if err != nil {
		return Sheet{}, err
	}
	var items []domain.ItemState
	if active {
		items = cat.pointItems(point)
	}
	entries := map[string]store.Line{}
	book := bookAlerts(nil)
	if t != nil {
		ls, err := store.LoadLines(ctx, tx, []uuid.UUID{t.ID})
		if err != nil {
			return Sheet{}, err
		}
		for _, l := range ls {
			entries[l.Line.ItemCode] = l
		}
		as, err := store.LoadAlerts(ctx, tx, []uuid.UUID{t.ID})
		if err != nil {
			return Sheet{}, err
		}
		book = bookAlerts(as)
		// Mặt hàng có trên phiếu nhưng không còn trong danh sách của xã (đổi gán / bộ theo vụ sau khi nhập): vẫn hiện.
		seen := map[string]bool{}
		for _, it := range items {
			seen[it.Code] = true
		}
		var extra []domain.ItemState
		for code, l := range entries {
			if seen[code] || (!l.Line.Filled() && l.AdjMin == nil) {
				continue
			}
			it, ok := cat.states[code]
			if !ok {
				it = domain.ItemState{Item: domain.Item{Code: code, Name: code}}
			}
			extra = append(extra, it)
		}
		domain.SortItems(extra, cat.order)
		items = append(items, extra...)
	}
	refs, err := s.refPrices(ctx, tx, sc.prof, sc.set, point, day)
	if err != nil {
		return Sheet{}, err
	}
	for _, it := range items {
		ln := SheetLine{Item: it, GroupName: cat.groupName(it.GroupCode), UnitName: cat.unitName(it.UnitCode),
			Threshold: sc.set.For(it.GroupCode, it.Code).Pct, Entry: domain.Line{ItemCode: it.Code}}
		if r, ok := refs[it.Code]; ok {
			r := r
			ln.Ref = &r
		}
		if l, ok := entries[it.Code]; ok {
			ln.Entry, ln.Entered, ln.AdjMin, ln.AdjMax, ln.AdjReason = l.Line, true, l.AdjMin, l.AdjMax, l.AdjReason
		}
		if ln.Entry.Priced() && ln.Ref != nil {
			ln.Eval = domain.Evaluate(*ln.Entry.Min, *ln.Entry.Max, ln.Ref.Mid, ln.Threshold)
		}
		if t != nil {
			k := alertKey{t.ID, it.Code}
			if a, ok := book.latest[k]; ok {
				ai := &AlertInfo{Alert: a, History: book.history[k], View: alertView(a, sh.Locked, sc.now)}
				for i := range ai.History {
					h := ai.History[i]
					switch h.Status {
					case domain.AlertResponded:
						ai.Response = &h
					case domain.AlertRequested:
						ai.Request = &h
					}
				}
				ln.Alert = ai
			}
		}
		sh.Lines = append(sh.Lines, ln)
	}
	c := sc.c
	editable := domain.BoardEditable(b)
	today := domain.BusinessDay(sc.now)
	sh.Can.Enter = c.level == "XA" && p.Officer == c.ref && active && day.Equal(today) && plan.Quote && !sh.Locked && editable &&
		(t == nil || (t.Source == domain.SourceXa && (t.Status == domain.TicketDraft || t.Status == domain.TicketSubmitted)))
	review := handles(c, p) && editable
	sh.Can.Review = review
	if review && sh.Locked {
		sh.Can.Proxy = active && (t == nil || t.Status == domain.TicketDraft)
		sh.Can.Correct = t != nil && domain.Sent(t.Status)
		sh.Can.Redate = sh.Can.Correct
		sh.Can.Delete = t != nil && domain.Live(t.Status)
	}
	sh.Can.Respond = c.level == "XA" && p.Officer == c.ref && editable
	return sh, nil
}

func alertView(a store.Alert, locked bool, now time.Time) string {
	var rb time.Time
	if a.RespondBy != nil {
		rb = *a.RespondBy
	}
	return domain.AlertView(a.Status, locked, rb, now)
}

// GetSheet đọc phiếu của xã point ngày day (zero → hôm nay): màn Nhập báo giá (Xã), Sửa phiếu / Nhập thay (Tỉnh).
func (s *TicketService) GetSheet(ctx context.Context, profile, point string, day time.Time) (Sheet, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return Sheet{}, err
	}
	day = s.dayOrToday(day)
	var sh Sheet
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sc, err := s.newSheetCtx(ctx, tx, prof, c)
		if err != nil {
			return err
		}
		if point == "" && c.level == "XA" { // Cấp Xã phụ trách đúng một xã → xã đó
			mineList := callerPoints(sc.ps, c.ref, day)
			if len(mineList) != 1 {
				return problem.Validation.WithField("point", "chọn xã báo giá")
			}
			point = mineList[0]
		}
		sh, err = s.buildSheet(ctx, tx, sc, point, day, nil)
		return err
	})
	if err == nil {
		s.fillNames(ctx, prof, c, &sh)
	}
	return sh, err
}

// GetTicket đọc một phiếu theo mã (kể cả phiếu Đã xóa) — chi tiết phiếu ở Lịch sử báo giá, Chi tiết báo giá.
func (s *TicketService) GetTicket(ctx context.Context, profile string, id uuid.UUID) (Sheet, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return Sheet{}, err
	}
	var sh Sheet
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		t, err := ticketByID(ctx, tx, id)
		if err != nil {
			return err
		}
		sc, err := s.newSheetCtx(ctx, tx, prof, c)
		if err != nil {
			return err
		}
		sh, err = s.buildSheet(ctx, tx, sc, t.PointCode, t.Day, &t)
		return err
	})
	if err == nil {
		s.fillNames(ctx, prof, c, &sh)
	}
	return sh, err
}

func (s *TicketService) fillNames(ctx context.Context, prof port.Profile, c caller, sh *Sheet) {
	names := s.officerNames(ctx, prof, c)
	sh.OfficerName, sh.OwnerName = names[sh.Point.Officer], names[sh.Point.Owner]
	if sh.Ticket != nil {
		sh.ActorName = names[sh.Ticket.ActorRef]
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Cán bộ Xã nhập / lưu nháp / gửi (FN-XA-03.01)
// ---------------------------------------------------------------------------------------------------------------------

// EntryInput là nội dung một lần lưu phiếu (Xã nhập hoặc Cấp Tỉnh nhập thay).
type EntryInput struct {
	Point   string
	Day     time.Time // zero → hôm nay (Xã); bắt buộc khi nhập thay
	Version int64     // phiên bản đầu phiếu đã đọc (0 = phiếu mới) — khóa lạc quan (BR-06 FN-TI-02.04)
	Submit  bool      // true = "Gửi báo giá" (Xã) / "Lưu" chính thức (nhập thay); false = lưu nháp
	Reason  string    // lý do nộp muộn (nhập thay — bắt buộc)
	Lines   []domain.Line
}

func checkVersion(cur *store.Ticket, v int64) error {
	switch {
	case cur == nil && v != 0:
		return port.ErrTicketStale.WithDetail("phiếu không còn (đã xóa hoặc đổi ngày)")
	case cur != nil && v != cur.Seq:
		return port.ErrTicketStale.WithDetail(fmt.Sprintf("phiên bản %d, hiện là %d", v, cur.Seq))
	}
	return nil
}

// mergeLines kiểm và gộp dòng gửi lên với dòng đã có: mặt hàng phải thuộc danh sách của xã; trả dòng sau gộp và dòng cần ghi.
func mergeLines(items []domain.ItemState, cur map[string]store.Line, in []domain.Line) (map[string]domain.Line, []domain.Line, error) {
	allowed := map[string]bool{}
	for _, it := range items {
		allowed[it.Code] = true
	}
	merged := map[string]domain.Line{}
	for code, l := range cur {
		merged[code] = l.Line
	}
	var changed []domain.Line
	var bad []string
	seen := map[string]bool{}
	for _, l := range in {
		l.Note = strings.TrimSpace(l.Note)
		if seen[l.ItemCode] {
			bad = append(bad, l.ItemCode+": trùng dòng")
			continue
		}
		seen[l.ItemCode] = true
		if !allowed[l.ItemCode] {
			bad = append(bad, l.ItemCode+": không thuộc danh sách mặt hàng của xã tại ngày báo giá")
			continue
		}
		if err := domain.ValidateLine(l); err != nil {
			bad = append(bad, err.Error())
			continue
		}
		old, had := cur[l.ItemCode]
		if had && old.Line.Same(l) {
			continue
		}
		if !had && !l.Filled() && l.Note == "" {
			continue // dòng trống chưa từng ghi
		}
		merged[l.ItemCode] = l
		changed = append(changed, l)
	}
	if len(bad) > 0 {
		return nil, nil, port.ErrPriceInvalid.WithDetail(strings.Join(bad, "; "))
	}
	return merged, changed, nil
}

// missingOnSubmit trả mặt hàng chưa cập nhật và dòng bất thường chưa có ghi chú (BR-03, BR-04 FN-XA-03.01).
func missingOnSubmit(items []domain.ItemState, merged map[string]domain.Line, refs map[string]RefPrice, set domain.ThresholdSet) []string {
	var miss []string
	for _, it := range items {
		l, ok := merged[it.Code]
		if !ok || !l.Filled() {
			miss = append(miss, it.Code+" ("+it.Name+"): chưa nhập giá hoặc Không có hàng")
			continue
		}
		if !l.Priced() {
			continue
		}
		if r, ok := refs[it.Code]; ok {
			ev := domain.Evaluate(*l.Min, *l.Max, r.Mid, set.For(it.GroupCode, it.Code).Pct)
			if ev.Direction != domain.AlertNone && l.Note == "" {
				miss = append(miss, it.Code+" ("+it.Name+"): giá bất thường "+signedPct(ev.ChangePct)+" — ghi chú biến động bắt buộc")
			}
		}
	}
	return miss
}

func signedPct(p decimal.Decimal) string {
	s := p.Round(1).String() + "%"
	if p.IsPositive() {
		return "+" + s
	}
	return s
}

// SaveEntry: cán bộ Xã lưu nháp hoặc gửi phiếu của xã mình cho ngày báo giá hôm nay, trước giờ khóa (FN-XA-03.01).
// Gửi: đủ mặt hàng, dòng bất thường có ghi chú; sinh / cập nhật / tự hủy cảnh báo Tạm tính (FN-TI-02.03 B1).
func (s *TicketService) SaveEntry(ctx context.Context, profile string, in EntryInput, idem string) (Sheet, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return Sheet{}, err
	}
	if c.level != "XA" {
		return Sheet{}, problem.Forbidden.WithDetail("chỉ cán bộ Cấp Xã nhập báo giá của xã; Cấp Tỉnh dùng Nhập thay sau giờ khóa")
	}
	day := s.dayOrToday(in.Day)
	var sh Sheet
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if err := store.LockPointTickets(ctx, tx, c.tenant, in.Point); err != nil {
			return err
		}
		sc, err := s.newSheetCtx(ctx, tx, prof, c)
		if err != nil {
			return err
		}
		p, ok, active := sc.pointAt(in.Point, day)
		if !ok {
			return port.ErrPointNotFound.WithDetail("xã báo giá " + in.Point)
		}
		if !active {
			return port.ErrPointInactive.WithDetail("xã báo giá " + in.Point + " không hoạt động ngày " + day.Format(time.DateOnly))
		}
		if p.Officer != c.ref { // BR-01: chỉ nhập cho xã được gán (tại ngày báo giá)
			return port.ErrPointNotAssigned
		}
		plan, err := s.dayPlan(ctx, tx, prof, day)
		if err != nil {
			return err
		}
		if !day.Equal(domain.BusinessDay(sc.now)) || !plan.Quote {
			return port.ErrNotQuoteDay.WithDetail(day.Format(time.DateOnly))
		}
		if plan.Locked(sc.now) { // AC-04: đến giờ khóa (giờ máy chủ) dữ liệu chưa gửi không được nhận
			return port.ErrTicketLocked.WithDetail("khóa lúc " + plan.LockAt.Format(time.RFC3339))
		}
		if b, err := s.board(ctx, tx, day); err != nil {
			return err
		} else if !domain.BoardEditable(b) {
			return port.ErrBoardApproved
		}
		cur, err := liveTicket(ctx, tx, in.Point, day)
		if err != nil {
			return err
		}
		if err := checkVersion(cur, in.Version); err != nil {
			return err
		}
		if cur != nil && cur.Source != domain.SourceXa {
			return port.ErrTicketExists.WithDetail("phiếu do Cấp Tỉnh nhập thay")
		}
		if cur != nil && cur.Status == domain.TicketSubmitted && !in.Submit {
			return problem.Conflict.WithDetail("phiếu đã gửi — sửa rồi bấm Gửi báo giá để gửi lại (không lưu nháp đè phiếu đã gửi)")
		}
		_, err = s.writeEntry(ctx, tx, sc, p, day, cur, in, domain.SourceXa, idem)
		if err != nil {
			return err
		}
		sh, err = s.buildSheet(ctx, tx, sc, in.Point, day, nil)
		return err
	})
	if err == nil {
		s.fillNames(ctx, prof, c, &sh)
	}
	return sh, mapLedgerErr(err)
}

// writeEntry ghi một lần lưu phiếu (Xã hoặc nhập thay) đã qua kiểm quyền / thời điểm: dòng thay đổi, đầu phiếu, cảnh báo
// khi gửi. Trả phiếu đã ghi.
func (s *TicketService) writeEntry(ctx context.Context, tx pgx.Tx, sc *sheetCtx, p port.Point, day time.Time, cur *store.Ticket,
	in EntryInput, source, idem string) (store.Ticket, error) {
	cat, err := loadDayCat(ctx, tx, day)
	if err != nil {
		return store.Ticket{}, err
	}
	items := cat.pointItems(p.Code)
	curLines := map[string]store.Line{}
	if cur != nil {
		ls, err := store.LoadLines(ctx, tx, []uuid.UUID{cur.ID})
		if err != nil {
			return store.Ticket{}, err
		}
		for _, l := range ls {
			curLines[l.Line.ItemCode] = l
		}
	}
	merged, changed, err := mergeLines(items, curLines, in.Lines)
	if err != nil {
		return store.Ticket{}, err
	}
	refs, err := s.refPrices(ctx, tx, sc.prof, sc.set, p.Code, day)
	if err != nil {
		return store.Ticket{}, err
	}
	if in.Submit {
		if miss := missingOnSubmit(items, merged, refs, sc.set); len(miss) > 0 {
			return store.Ticket{}, port.ErrTicketIncomplete.WithDetail(strings.Join(miss, "; "))
		}
	}
	now, c := sc.now, sc.c
	t := store.Ticket{ID: ids.New(), PointCode: p.Code, Day: day, Status: domain.TicketDraft, Source: source, Op: domain.OpSave,
		OfficerRef: p.Officer}
	if cur != nil {
		t.ID = cur.ID
		t.SubmittedAt = cur.SubmittedAt
	}
	switch {
	case source == domain.SourceProxy && in.Submit:
		t.Status, t.Op, t.Reason, t.SubmittedAt = domain.TicketLate, domain.OpProxySubmit, strings.TrimSpace(in.Reason), &now
	case source == domain.SourceProxy:
		t.Op, t.Reason = domain.OpProxySave, strings.TrimSpace(in.Reason)
	case in.Submit:
		t.Status, t.Op, t.SubmittedAt = domain.TicketSubmitted, domain.OpSubmit, &now
	}
	if t.Status == domain.TicketDraft {
		t.SubmittedAt = nil
	}
	seq, err := store.InsertTicket(ctx, tx, ids.New(), c.tenant, t, now, c.ref, idemKey(idem, "ticket"))
	if err != nil {
		return store.Ticket{}, err
	}
	t.Seq = seq
	for _, l := range changed {
		if _, err := store.InsertLine(ctx, tx, ids.New(), c.tenant, store.Line{TicketID: t.ID, Line: l}, now, c.ref,
			idemKey(idem, "line:"+l.ItemCode)); err != nil {
			return store.Ticket{}, err
		}
	}
	if in.Submit {
		raised, err := s.syncAlerts(ctx, tx, sc, t.ID, items, merged, refs, idem)
		if err != nil {
			return store.Ticket{}, err
		}
		if source == domain.SourceXa { // NOTIF: Tạm tính mới → Cấp Tỉnh phụ trách (nhập thay: Cấp Tỉnh đã biết)
			if err := emitAlertRaised(ctx, tx, sc, p, day, t.ID, raised); err != nil {
				return store.Ticket{}, err
			}
		}
		// W11.77: lần lưu chính thức đầu tiên của phiếu nhập thay → cán bộ Xã nhận thông báo (FN-TI-02.09 hậu điều kiện).
		if source == domain.SourceProxy && (cur == nil || cur.Status != domain.TicketLate) {
			if err := emitProxied(ctx, tx, sc, p, day, t.ID, len(items)); err != nil {
				return store.Ticket{}, err
			}
		}
	}
	return t, nil
}

// syncAlerts sinh / cập nhật / tự hủy cảnh báo khi phiếu được gửi (FN-TI-02.03 B1; Mục II.3.2): dòng vượt ngưỡng →
// Tạm tính (hoặc giữ trạng thái đã yêu cầu xác minh / Xã đã phản hồi, cập nhật giá); dòng về dưới ngưỡng, hết giá →
// tự hủy. Ngưỡng là ngưỡng đang hiệu lực lúc gửi (FN-AD-04.06 BR-03). Trả mã mặt hàng có cảnh báo Tạm tính MỚI (chưa có
// hoặc đã tự hủy trước đó) — cho thông báo Cấp Tỉnh (NOTIF).
func (s *TicketService) syncAlerts(ctx context.Context, tx pgx.Tx, sc *sheetCtx, ticket uuid.UUID, items []domain.ItemState,
	merged map[string]domain.Line, refs map[string]RefPrice, idem string) ([]string, error) {
	var raised []string
	as, err := store.LoadAlerts(ctx, tx, []uuid.UUID{ticket})
	if err != nil {
		return nil, err
	}
	book := bookAlerts(as)
	now, c := sc.now, sc.c
	write := func(a store.Alert, suffix string) error {
		_, err := store.InsertAlert(ctx, tx, ids.New(), c.tenant, a, now, c.ref, idemKey(idem, "alert:"+a.ItemCode+":"+suffix))
		return err
	}
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.Code] = true
		k := alertKey{ticket, it.Code}
		cur, has := book.latest[k]
		l := merged[it.Code]
		r, hasRef := refs[it.Code]
		ev := domain.Evaluation{}
		if l.Priced() && hasRef {
			ev = domain.Evaluate(*l.Min, *l.Max, r.Mid, sc.set.For(it.GroupCode, it.Code).Pct)
		}
		if ev.Direction == domain.AlertNone {
			if has && domain.AlertOpenBeforeLock(cur.Status) {
				x := store.Alert{TicketID: ticket, ItemCode: it.Code, Status: domain.AlertCancelled, Direction: cur.Direction, Ref: cur.Ref,
					Price: cur.Price, ChangePct: cur.ChangePct, Threshold: cur.Threshold}
				if err := write(x, "cancel"); err != nil {
					return nil, err
				}
			}
			continue
		}
		a := store.Alert{TicketID: ticket, ItemCode: it.Code, Status: domain.AlertProvisional, Direction: ev.Direction, Ref: ev.Ref,
			Price: ev.Price, ChangePct: ev.ChangePct, Threshold: ev.Threshold}
		switch {
		case !has || cur.Status == domain.AlertCancelled:
			raised = append(raised, it.Code)
		case domain.AlertOpenBeforeLock(cur.Status):
			if cur.Direction == a.Direction && cur.Price.Equal(a.Price) && cur.Ref.Equal(a.Ref) && cur.Threshold.Equal(a.Threshold) {
				continue
			}
			a.Status, a.Message, a.RespondBy = cur.Status, cur.Message, cur.RespondBy
			a.Response, a.ProposedMin, a.ProposedMax, a.Explanation = cur.Response, cur.ProposedMin, cur.ProposedMax, cur.Explanation
		default:
			continue // đã xử lý (sau giờ khóa) — không sinh lại
		}
		if err := write(a, "sync"); err != nil {
			return nil, err
		}
	}
	// Cảnh báo của mặt hàng không còn trên danh sách: tự hủy.
	for k, cur := range book.latest {
		if seen[k.item] || !domain.AlertOpenBeforeLock(cur.Status) {
			continue
		}
		x := store.Alert{TicketID: ticket, ItemCode: k.item, Status: domain.AlertCancelled, Direction: cur.Direction, Ref: cur.Ref,
			Price: cur.Price, ChangePct: cur.ChangePct, Threshold: cur.Threshold}
		if err := write(x, "cancel"); err != nil {
			return nil, err
		}
	}
	return raised, nil
}

// mapLedgerErr đổi lỗi DB của sổ phiếu sang mã lỗi chung (trùng khóa → ghi đồng thời).
func mapLedgerErr(err error) error {
	if err == nil {
		return nil
	}
	return classify(err)
}
