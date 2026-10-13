package app

// Phiếu báo giá — đọc (W11.75): phiếu của các xã trong một ngày (Chi tiết báo giá FN-TI-02.02, khối xã của Kiểm duyệt),
// danh sách cảnh báo (FN-TI-02.03 B2, Yêu cầu xác minh của Xã FN-XA-03.05), lịch sử báo giá của xã (FN-XA-03.03), nhật
// ký phiếu, xuất Excel chi tiết, in phiếu File 2 (FN-XA-03.02), port.Tickets cho W11.76/W11.77, hook "đã có giá / đã có
// phiếu gửi" cho W11.72/W11.73.

import (
	"bytes"
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// TicketRow là một xã trong ngày báo giá.
type TicketRow struct {
	Point       port.Point
	RegionName  string
	OfficerName string
	OwnerName   string
	Mine        bool
	Ticket      *store.Ticket
	View        string
	Items       int // số mặt hàng phải báo giá
	Priced      int
	NoGoods     int
	Adjusted    int
	Excluded    int
	High, Low   int // cảnh báo ▲ / ▼ (trừ tự hủy)
	OpenAlerts  int // chưa xử lý
}

// DayView là phiếu của các xã trong một ngày báo giá.
type DayView struct {
	Day    time.Time
	Plan   domain.DayPlan
	Locked bool
	Board  string
	Now    time.Time
	Rows   []TicketRow
}

// DayFilter lọc phiếu trong ngày.
type DayFilter struct {
	Day    time.Time
	Region string
	Mine   bool // Cấp Tỉnh: xã được phân công (mặc định ở màn — BR-01 FN-TI-02.02 AC-01)
}

func countLines(t *store.Ticket, lines []store.Line, book alertBook, r *TicketRow) {
	for _, l := range lines {
		switch {
		case l.Line.NoGoods:
			r.NoGoods++
		case l.Line.Priced():
			r.Priced++
		}
		if l.AdjMin != nil {
			r.Adjusted++
		}
	}
	for k, a := range book.latest {
		if k.ticket != t.ID || a.Status == domain.AlertCancelled {
			continue
		}
		if a.Direction == domain.AlertHigh {
			r.High++
		} else {
			r.Low++
		}
		switch a.Status {
		case domain.AlertExcluded:
			r.Excluded++
		}
		if !domain.AlertResolved(a.Status) {
			r.OpenAlerts++
		}
	}
}

// dayRows dựng các xã đang hoạt động trong ngày với phiếu còn hiệu lực của mỗi xã.
func (s *TicketService) dayRows(ctx context.Context, tx pgx.Tx, ps *pointSnap, day time.Time, locked bool, board string) ([]TicketRow, error) {
	cat, err := loadDayCat(ctx, tx, day)
	if err != nil {
		return nil, err
	}
	ts, err := store.LoadTickets(ctx, tx, store.TicketFilter{From: day, To: day, Live: true})
	if err != nil {
		return nil, err
	}
	byPoint := map[string]store.Ticket{}
	idList := make([]uuid.UUID, 0, len(ts))
	for _, t := range ts {
		byPoint[t.PointCode] = t
		idList = append(idList, t.ID)
	}
	lines, err := store.LoadLines(ctx, tx, idList)
	if err != nil {
		return nil, err
	}
	byTicket := map[uuid.UUID][]store.Line{}
	for _, l := range lines {
		byTicket[l.TicketID] = append(byTicket[l.TicketID], l)
	}
	as, err := store.LoadAlerts(ctx, tx, idList)
	if err != nil {
		return nil, err
	}
	book := bookAlerts(as)
	regions := domain.EffectiveAt(ps.regions, day)
	var rows []TicketRow
	for _, st := range ps.states(day) {
		if st.State != domain.PointStateActive {
			continue
		}
		p := ps.portPoint(st, day)
		r := TicketRow{Point: p, Items: len(cat.pointItems(p.Code))}
		if rg, ok := regions[p.RegionCode]; ok {
			r.RegionName = rg.V.Name
		}
		status := ""
		if t, ok := byPoint[p.Code]; ok {
			t := t
			r.Ticket, status = &t, t.Status
			countLines(&t, byTicket[t.ID], book, &r)
		}
		r.View = domain.TicketView(status, locked, board)
		rows = append(rows, r)
	}
	order := map[string]int{}
	for code, rg := range regions {
		order[code] = rg.V.SortOrder
	}
	sort.Slice(rows, func(i, j int) bool {
		ri, rj := rank(order, rows[i].Point.RegionCode), rank(order, rows[j].Point.RegionCode)
		if ri != rj {
			return ri < rj
		}
		return rows[i].Point.Code < rows[j].Point.Code
	})
	return rows, nil
}

// DayTickets đọc phiếu của mọi xã báo giá trong ngày (Cấp Tỉnh, Admin; Cấp Xã chỉ thấy xã mình phụ trách tại ngày).
func (s *TicketService) DayTickets(ctx context.Context, profile string, f DayFilter) (DayView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return DayView{}, err
	}
	if !reviewer(c) && c.level != "XA" {
		return DayView{}, canView(c, port.Point{})
	}
	day := s.dayOrToday(f.Day)
	v := DayView{Day: day, Now: s.Clock.Now()}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		if v.Plan, err = s.dayPlan(ctx, tx, prof, day); err != nil {
			return err
		}
		v.Locked = v.Plan.Locked(v.Now)
		if v.Board, err = s.board(ctx, tx, day); err != nil {
			return err
		}
		rows, err := s.dayRows(ctx, tx, ps, day, v.Locked, v.Board)
		if err != nil {
			return err
		}
		for _, r := range rows {
			switch c.level {
			case "XA":
				r.Mine = r.Point.Officer == c.ref
				if !r.Mine {
					continue
				}
			case "TINH":
				r.Mine = r.Point.Owner == c.ref
			}
			if (f.Region != "" && r.Point.RegionCode != f.Region) || (f.Mine && !r.Mine) {
				continue
			}
			v.Rows = append(v.Rows, r)
		}
		return nil
	})
	if err != nil {
		return DayView{}, err
	}
	names := s.officerNames(ctx, prof, c)
	for i := range v.Rows {
		v.Rows[i].OfficerName, v.Rows[i].OwnerName = names[v.Rows[i].Point.Officer], names[v.Rows[i].Point.Owner]
	}
	return v, nil
}

// AlertRow là một cảnh báo trong danh sách (FN-TI-02.03 B2; FN-XA-03.05 danh sách yêu cầu).
type AlertRow struct {
	AlertInfo
	Day        time.Time
	Point      port.Point
	RegionName string
	Item       domain.ItemState
	GroupName  string
	UnitName   string
	Entry      domain.Line
	AdjText    string // giá hiệu chỉnh "thấp – cao" (rỗng = chưa hiệu chỉnh)
	Ticket     store.Ticket
	Mine       bool
	CanAct     bool // Cấp Tỉnh xử lý được (xã phân công / chưa phân công); Cấp Xã phản hồi được
}

// AlertFilter lọc cảnh báo.
type AlertFilter struct {
	From, To time.Time // ngày báo giá (zero → hôm nay)
	Point    string
	Mine     bool
	View     string // trạng thái nhìn (rỗng = mọi trạng thái trừ tự hủy)
}

// ListAlerts đọc cảnh báo của các phiếu còn hiệu lực trong khoảng ngày: Cấp Tỉnh, Admin mọi xã (mặc định lọc xã phân
// công ở màn — BR-04); Cấp Xã chỉ xã mình (FN-XA-03.05 BR-01).
func (s *TicketService) ListAlerts(ctx context.Context, profile string, f AlertFilter) ([]AlertRow, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	if !reviewer(c) && c.level != "XA" {
		return nil, canView(c, port.Point{})
	}
	from, to := s.dayOrToday(f.From), s.dayOrToday(f.To)
	if f.To.IsZero() && !f.From.IsZero() {
		to = from
	}
	if to.Before(from) || to.Sub(from) > 93*24*time.Hour {
		return nil, problem.Validation.WithField("to", "khoảng ngày 0–93 ngày")
	}
	now := s.Clock.Now()
	var out []AlertRow
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		ts, err := store.LoadTickets(ctx, tx, store.TicketFilter{Point: f.Point, From: from, To: to, Live: true})
		if err != nil {
			return err
		}
		idList := make([]uuid.UUID, 0, len(ts))
		for _, t := range ts {
			idList = append(idList, t.ID)
		}
		as, err := store.LoadAlerts(ctx, tx, idList)
		if err != nil {
			return err
		}
		book := bookAlerts(as)
		lines, err := store.LoadLines(ctx, tx, idList)
		if err != nil {
			return err
		}
		lineOf := map[alertKey]store.Line{}
		for _, l := range lines {
			lineOf[alertKey{l.TicketID, l.Line.ItemCode}] = l
		}
		cats := map[time.Time]*dayCat{}
		plans := map[time.Time]domain.DayPlan{}
		for _, t := range ts {
			p, ok, _ := (&sheetCtx{ps: ps}).pointAt(t.PointCode, t.Day)
			if !ok {
				continue
			}
			mine := (c.level == "XA" && p.Officer == c.ref) || (c.level == "TINH" && p.Owner == c.ref)
			if (c.level == "XA" && !mine) || (f.Mine && !mine) {
				continue
			}
			if _, ok := cats[t.Day]; !ok {
				if cats[t.Day], err = loadDayCat(ctx, tx, t.Day); err != nil {
					return err
				}
				if plans[t.Day], err = s.dayPlan(ctx, tx, prof, t.Day); err != nil {
					return err
				}
			}
			cat, plan := cats[t.Day], plans[t.Day]
			locked := plan.Locked(now)
			var regionName string
			if rg, ok := domain.EffectiveAt(ps.regions, t.Day)[p.RegionCode]; ok {
				regionName = rg.V.Name
			}
			for k, a := range book.latest {
				if k.ticket != t.ID || a.Status == domain.AlertCancelled {
					continue
				}
				view := alertView(a, locked, now)
				if f.View != "" && view != f.View {
					continue
				}
				it, ok := cat.states[a.ItemCode]
				if !ok {
					it = domain.ItemState{Item: domain.Item{Code: a.ItemCode, Name: a.ItemCode}}
				}
				ai := AlertInfo{Alert: a, View: view, History: book.history[k]}
				for i := range ai.History {
					h := ai.History[i]
					switch h.Status {
					case domain.AlertResponded:
						ai.Response = &h
					case domain.AlertRequested:
						ai.Request = &h
					}
				}
				row := AlertRow{AlertInfo: ai, Day: t.Day, Point: p, RegionName: regionName, Item: it, GroupName: cat.groupName(it.GroupCode),
					UnitName: cat.unitName(it.UnitCode), Entry: lineOf[k].Line, Ticket: t, Mine: mine}
				if l := lineOf[k]; l.AdjMin != nil {
					row.AdjText = pairStr(l.AdjMin, l.AdjMax)
				}
				if c.level == "XA" {
					row.CanAct = view == domain.AlertRequested
				} else {
					row.CanAct = handles(c, p)
				}
				out = append(out, row)
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Day.Equal(out[j].Day) {
			return out[i].Day.After(out[j].Day)
		}
		if out[i].Point.Code != out[j].Point.Code {
			return out[i].Point.Code < out[j].Point.Code
		}
		return out[i].ItemCode < out[j].ItemCode
	})
	return out, err
}

// HistoryRow là một ngày báo giá của xã (Lịch sử báo giá — FN-XA-03.03).
type HistoryRow struct {
	Day    time.Time
	Ticket *store.Ticket
	View   string
	TicketRow
}

// History đọc lịch sử báo giá của xã point trong khoảng [from, to] (≤ 12 tháng): mỗi ngày báo giá một dòng (Chưa nhập /
// Không nộp khi không có phiếu) và các phiếu Đã xóa. Cấp Xã chỉ thấy ngày mình phụ trách xã (BR-01).
func (s *TicketService) History(ctx context.Context, profile, point string, from, to time.Time) ([]HistoryRow, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	if !reviewer(c) && c.level != "XA" {
		return nil, canView(c, port.Point{})
	}
	to = s.dayOrToday(to)
	if from.IsZero() {
		from = to.AddDate(0, 0, -30)
	}
	from = dayOf(from)
	if to.Before(from) || to.Sub(from) > 366*24*time.Hour {
		return nil, problem.Validation.WithField("from", "khoảng thời gian tối đa 12 tháng — thu hẹp lại")
	}
	now := s.Clock.Now()
	var out []HistoryRow
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		if point == "" && c.level == "XA" {
			mineList := callerPoints(ps, c.ref, to)
			if len(mineList) != 1 {
				return problem.Validation.WithField("point", "chọn xã báo giá")
			}
			point = mineList[0]
		}
		if point == "" {
			return problem.Validation.WithField("point", "bắt buộc")
		}
		st, err := s.calendarState(ctx, tx, prof)
		if err != nil {
			return err
		}
		n := int(to.Sub(from)/(24*time.Hour)) + 1
		plans, err := s.planRange(ctx, tx, prof.Name, st, from, n)
		if err != nil {
			return err
		}
		ts, err := store.LoadTickets(ctx, tx, store.TicketFilter{Point: point, From: from, To: to})
		if err != nil {
			return err
		}
		idList := make([]uuid.UUID, 0, len(ts))
		for _, t := range ts {
			idList = append(idList, t.ID)
		}
		lines, err := store.LoadLines(ctx, tx, idList)
		if err != nil {
			return err
		}
		byTicket := map[uuid.UUID][]store.Line{}
		for _, l := range lines {
			byTicket[l.TicketID] = append(byTicket[l.TicketID], l)
		}
		as, err := store.LoadAlerts(ctx, tx, idList)
		if err != nil {
			return err
		}
		book := bookAlerts(as)
		live := map[time.Time]store.Ticket{}
		var deleted []store.Ticket
		for _, t := range ts {
			if t.Status == domain.TicketDeleted {
				deleted = append(deleted, t)
			} else {
				live[t.Day] = t
			}
		}
		sc := &sheetCtx{ps: ps}
		allowed := func(d time.Time) (port.Point, bool) {
			p, ok, _ := sc.pointAt(point, d)
			if !ok {
				return p, false
			}
			return p, c.level != "XA" || p.Officer == c.ref
		}
		add := func(d time.Time, t *store.Ticket, locked bool, board string) {
			p, ok := allowed(d)
			if !ok {
				return
			}
			r := HistoryRow{Day: d, Ticket: t}
			r.Point = p
			status := ""
			if t != nil {
				status = t.Status
				countLines(t, byTicket[t.ID], book, &r.TicketRow)
			}
			r.View = domain.TicketView(status, locked, board)
			out = append(out, r)
		}
		for _, pl := range plans {
			t, has := live[pl.Day]
			_, ok, active := sc.pointAt(point, pl.Day)
			if !has && (!pl.Quote || !ok || !active) {
				continue
			}
			b, err := s.board(ctx, tx, pl.Day)
			if err != nil {
				return err
			}
			var tp *store.Ticket
			if has {
				tp = &t
			}
			add(pl.Day, tp, pl.Locked(now), b)
		}
		for i := range deleted {
			add(deleted[i].Day, &deleted[i], true, domain.BoardOpen)
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Day.After(out[j].Day) })
	return out, err
}

// TicketLog là nhật ký một phiếu: mọi phiên bản đầu phiếu, dòng giá, cảnh báo (người, thời điểm, giá trị cũ → mới, lý do).
type TicketLog struct {
	Ticket []store.Ticket
	Lines  []store.Line
	Alerts []store.Alert
	Names  map[uuid.UUID]string
}

// Log đọc nhật ký một phiếu (UR-TI-02 "lịch sử hiệu chỉnh, trao đổi xác minh, nhật ký thao tác"; AC-03 FN-TI-02.02).
func (s *TicketService) Log(ctx context.Context, profile string, id uuid.UUID) (TicketLog, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return TicketLog{}, err
	}
	var lg TicketLog
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		t, err := ticketByID(ctx, tx, id)
		if err != nil {
			return err
		}
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		p, ok, _ := (&sheetCtx{ps: ps}).pointAt(t.PointCode, t.Day)
		if !ok {
			return port.ErrPointNotFound
		}
		if err := canView(c, p); err != nil {
			return err
		}
		if lg.Ticket, err = store.TicketHistory(ctx, tx, id); err != nil {
			return err
		}
		if lg.Lines, err = store.LineHistory(ctx, tx, id); err != nil {
			return err
		}
		lg.Alerts, err = store.LoadAlerts(ctx, tx, []uuid.UUID{id})
		return err
	})
	if err == nil {
		lg.Names = s.officerNames(ctx, prof, c)
	}
	return lg, err
}

// ---------------------------------------------------------------------------------------------------------------------
// Xuất Excel chi tiết, in phiếu File 2
// ---------------------------------------------------------------------------------------------------------------------

// Export là tệp xuất.
type Export struct {
	Body        []byte
	ContentType string
	FileName    string
}

var ticketViewVI = map[string]string{
	domain.ViewNotEntered: "Chưa nhập", domain.ViewDraft: "Bản nháp", domain.ViewSubmitted: "Đã gửi", domain.ViewLocked: "Đã khóa",
	domain.ViewMissed: "Không nộp", domain.ViewLate: "Nộp muộn", domain.ViewApproved: "Đã duyệt", domain.ViewDeleted: "Đã xóa",
}

var alertViewVI = map[string]string{
	domain.AlertProvisional: "Tạm tính", domain.AlertViewPending: "Chờ rà soát", domain.AlertRequested: "Đã yêu cầu xác minh",
	domain.AlertResponded: "Xã đã phản hồi", domain.AlertViewOverdue: "Quá hạn phản hồi", domain.AlertAdjusted: "Đã hiệu chỉnh",
	domain.AlertKept: "Giữ nguyên", domain.AlertExcluded: "Đã loại", domain.AlertCancelled: "Tự hủy",
}

func vnDate(d time.Time) string { return d.Format("02/01/2006") }

// lineStatusVI là "trạng thái dòng" của Chi tiết báo giá (B2 FN-TI-02.02).
func lineStatusVI(l SheetLine) string {
	switch {
	case l.Alert != nil && l.Alert.Status == domain.AlertExcluded:
		return "Đã loại"
	case l.AdjMin != nil:
		return "Đã hiệu chỉnh"
	case l.Entry.NoGoods:
		return "Không có hàng"
	case !l.Entry.Filled():
		return "Chưa nhập"
	case l.Eval.Direction == domain.AlertHigh:
		return "▲ Cao bất thường"
	case l.Eval.Direction == domain.AlertLow:
		return "▼ Thấp bất thường"
	}
	return "Bình thường"
}

func pairStr(lo, hi interface{ String() string }) string { return lo.String() + " – " + hi.String() }

var exportHeader = []string{"STT", "Ngày", "Xã báo giá", "Nhóm vùng", "Nhóm hàng", "Mặt hàng", "Giống/phẩm cấp", "ĐVT", "Giá tham chiếu",
	"Giá thấp nhất", "Giá cao nhất", "Δ%", "Trạng thái dòng", "Cảnh báo", "Giá hiệu chỉnh", "Ghi chú biến động", "Trạng thái phiếu", "Thời điểm gửi"}

// ExportDay xuất Excel chi tiết báo giá của một ngày theo bộ lọc (FN-TI-02.02 luồng phụ) — dòng giá của mọi phiếu còn
// hiệu lực; Cấp Tỉnh, Admin.
func (s *TicketService) ExportDay(ctx context.Context, profile string, f DayFilter) (Export, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return Export{}, err
	}
	if !reviewer(c) {
		return Export{}, errNotReviewer
	}
	day := s.dayOrToday(f.Day)
	var rows [][]string
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sc, err := s.newSheetCtx(ctx, tx, prof, c)
		if err != nil {
			return err
		}
		ts, err := store.LoadTickets(ctx, tx, store.TicketFilter{From: day, To: day, Live: true})
		if err != nil {
			return err
		}
		n := 0
		for i := range ts {
			t := ts[i]
			sh, err := s.buildSheet(ctx, tx, sc, t.PointCode, t.Day, &t)
			if err != nil {
				return err
			}
			if (f.Region != "" && sh.Point.RegionCode != f.Region) || (f.Mine && sh.Point.Owner != c.ref) {
				continue
			}
			sent := ""
			if t.SubmittedAt != nil {
				sent = t.SubmittedAt.In(clock.Zone()).Format("02/01/2006 15:04")
			}
			for _, l := range sh.Lines {
				n++
				ref, lo, hi, pct, adj := "", "", "", "", ""
				if l.Ref != nil {
					ref = l.Ref.Mid.String()
				}
				if l.Entry.Priced() {
					lo, hi = l.Entry.Min.String(), l.Entry.Max.String()
				}
				if l.Eval.HasRef {
					pct = signedPct(l.Eval.ChangePct)
				}
				if l.AdjMin != nil {
					adj = pairStr(l.AdjMin, l.AdjMax)
				}
				alert := ""
				if l.Alert != nil && l.Alert.Status != domain.AlertCancelled {
					alert = alertViewVI[l.Alert.View]
				}
				rows = append(rows, []string{strconv.Itoa(n), vnDate(day), sh.Point.Name + " (" + sh.Point.Code + ")", sh.RegionName,
					l.GroupName, l.Item.Name, l.Item.Variety, l.UnitName, ref, lo, hi, pct, lineStatusVI(l), alert, adj, l.Entry.Note,
					ticketViewVI[sh.View], sent})
			}
		}
		return nil
	})
	if err != nil {
		return Export{}, err
	}
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Chi tiết báo giá", exportHeader, rows); err != nil {
		return Export{}, err
	}
	return Export{Body: buf.Bytes(), ContentType: tabular.ContentTypeXLSX, FileName: "chi-tiet-bao-gia-" + day.Format(time.DateOnly) + ".xlsx"}, nil
}

var printHeader = []string{"STT", "Nhóm hàng", "Mặt hàng", "ĐVT", "Giá thấp nhất", "Giá cao nhất", "Ghi chú"}

// printNote là ghi chú in của một dòng (BR-02 FN-XA-03.02): giá in là giá Xã gửi; dòng hiệu chỉnh "Tỉnh hiệu chỉnh: …";
// dòng đã loại "Không đưa vào bảng giá"; Không có hàng.
func printNote(l SheetLine) string {
	var parts []string
	if l.Entry.NoGoods {
		parts = append(parts, "Không có hàng")
	}
	if l.Entry.Note != "" {
		parts = append(parts, l.Entry.Note)
	}
	if l.AdjMin != nil {
		parts = append(parts, "Tỉnh hiệu chỉnh: "+pairStr(l.AdjMin, l.AdjMax))
	}
	if l.Alert != nil && l.Alert.Status == domain.AlertExcluded {
		parts = append(parts, "Không đưa vào bảng giá")
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "; "
		}
		out += p
	}
	return out
}

// PrintTickets in một hoặc nhiều phiếu (in hàng loạt gộp một tệp — AC-03 FN-XA-03.02) theo bố cục File 2 dạng bảng:
// PDF (bộ dựng pf/reports) hoặc Excel. Phiếu Không nộp / Đã xóa không in (luồng ngoại lệ); phiếu chưa khóa có nhãn
// "Chưa khóa"; Nộp muộn có nhãn "Nộp muộn – nhập thay".
func (s *TicketService) PrintTickets(ctx context.Context, profile string, idList []uuid.UUID, format string) (Export, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return Export{}, err
	}
	if format == "" {
		format = "pdf"
	}
	if format != "pdf" && format != "xlsx" {
		return Export{}, problem.Validation.WithField("format", "pdf hoặc xlsx")
	}
	if len(idList) == 0 || len(idList) > 62 {
		return Export{}, problem.Validation.WithField("ticket", "chọn 1–62 phiếu")
	}
	if format == "pdf" && s.PDF == nil {
		return Export{}, port.ErrPDFUnavailable
	}
	var (
		rows  [][]string
		meta  [][2]string
		first Sheet
	)
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sc, err := s.newSheetCtx(ctx, tx, prof, c)
		if err != nil {
			return err
		}
		for i, id := range idList {
			t, err := ticketByID(ctx, tx, id)
			if err != nil {
				return err
			}
			sh, err := s.buildSheet(ctx, tx, sc, t.PointCode, t.Day, &t)
			if err != nil {
				return err
			}
			if t.Status == domain.TicketDeleted || sh.View == domain.ViewMissed {
				return problem.Conflict.WithDetail("phiếu " + t.PointCode + " ngày " + vnDate(t.Day) + " " + ticketViewVI[sh.View] + " — không in được")
			}
			if i == 0 {
				first = sh
			}
			label := ticketViewVI[sh.View]
			switch {
			case !sh.Locked:
				label = "Chưa khóa"
			case t.Status == domain.TicketLate:
				label = "Nộp muộn – nhập thay"
			}
			if len(idList) > 1 {
				rows = append(rows, []string{"", "PHIẾU " + vnDate(t.Day), sh.Point.Name + " (" + sh.Point.Code + ")", "", "", "", label})
			} else {
				meta = append(meta, [2]string{"Trạng thái", label})
			}
			for j, l := range sh.Lines {
				lo, hi := "", ""
				if l.Entry.Priced() {
					lo, hi = l.Entry.Min.String(), l.Entry.Max.String()
				}
				rows = append(rows, []string{strconv.Itoa(j + 1), l.GroupName, l.Item.Name + " " + l.Item.Variety, l.UnitName, lo, hi, printNote(l)})
			}
		}
		return nil
	})
	if err != nil {
		return Export{}, err
	}
	names := s.officerNames(ctx, prof, c)
	officer := names[first.Point.Officer]
	if officer == "" && first.Ticket != nil {
		officer = names[first.Ticket.OfficerRef]
	}
	head := [][2]string{{"Xã báo giá", first.Point.Name + " (" + first.Point.Code + ")"}, {"Cán bộ Xã", officer}}
	if len(idList) == 1 {
		head = append(head, [2]string{"Ngày báo giá", vnDate(first.Day)})
	}
	meta = append(append(head, meta...), [2]string{"Người lập phiếu (ký, ghi rõ họ tên)", "………………………………"})
	fname := "phieu-bao-gia-" + first.Point.Code + "-" + first.Day.Format(time.DateOnly)
	if format == "pdf" {
		b, err := s.PDF.RenderTablePDF("PHIẾU BÁO GIÁ NÔNG SẢN", meta, printHeader, rows, s.Clock.Now().In(clock.Zone()))
		if err != nil {
			return Export{}, err
		}
		return Export{Body: b, ContentType: "application/pdf", FileName: fname + ".pdf"}, nil
	}
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Phiếu báo giá", printHeader, rows); err != nil {
		return Export{}, err
	}
	return Export{Body: buf.Bytes(), ContentType: tabular.ContentTypeXLSX, FileName: fname + ".xlsx"}, nil
}

// ---------------------------------------------------------------------------------------------------------------------
// port.Tickets (W11.76, W11.77) và hook của W11.72 / W11.73
// ---------------------------------------------------------------------------------------------------------------------

// Day (port.Tickets) trả tình hình phiếu và dòng giá tính vào bảng giá của ngày day cho tenant của ctx (không kiểm vai).
func (s *TicketService) Day(ctx context.Context, profile string, day time.Time) (port.DaySummary, error) {
	prof, ok := s.calendarProfile(profile)
	if !ok {
		return port.DaySummary{}, port.ErrProfileNotFound
	}
	day = domain.BusinessDay(day)
	out := port.DaySummary{Day: day}
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		ps, err := loadPointSnap(ctx, tx)
		if err != nil {
			return err
		}
		plan, err := s.dayPlan(ctx, tx, prof, day)
		if err != nil {
			return err
		}
		now := s.Clock.Now()
		out.Quote, out.LockAt, out.Locked = plan.Quote, plan.LockAt, plan.Locked(now)
		b, err := s.board(ctx, tx, day)
		if err != nil {
			return err
		}
		rows, err := s.dayRows(ctx, tx, ps, day, out.Locked, b)
		if err != nil {
			return err
		}
		var sent []uuid.UUID
		region := map[uuid.UUID]string{}
		late := map[uuid.UUID]bool{}
		for _, r := range rows {
			out.Points++
			switch {
			case r.Ticket != nil && r.Ticket.Status == domain.TicketSubmitted:
				out.Submitted++
			case r.Ticket != nil && r.Ticket.Status == domain.TicketLate:
				out.Late++
			default:
				out.Missed++
			}
			if r.Ticket != nil && domain.Sent(r.Ticket.Status) {
				sent = append(sent, r.Ticket.ID)
				region[r.Ticket.ID] = r.Point.RegionCode
				late[r.Ticket.ID] = r.Ticket.Status == domain.TicketLate
				out.Excluded += r.Excluded
				out.OpenAlerts += r.OpenAlerts
			}
		}
		if len(sent) == 0 {
			return nil
		}
		lines, err := store.LoadLines(ctx, tx, sent)
		if err != nil {
			return err
		}
		as, err := store.LoadAlerts(ctx, tx, sent)
		if err != nil {
			return err
		}
		book := bookAlerts(as)
		cat, err := loadDayCat(ctx, tx, day)
		if err != nil {
			return err
		}
		ts, err := store.LoadTickets(ctx, tx, store.TicketFilter{IDs: sent})
		if err != nil {
			return err
		}
		pointOf := map[uuid.UUID]string{}
		for _, t := range ts {
			pointOf[t.ID] = t.PointCode
		}
		for _, l := range lines {
			if book.excluded(l.TicketID, l.Line.ItemCode) {
				continue
			}
			lo, hi, ok := domain.Effective(l.Line, l.AdjMin, l.AdjMax)
			if !ok {
				continue
			}
			it := cat.states[l.Line.ItemCode]
			out.Lines = append(out.Lines, port.PriceLine{PointCode: pointOf[l.TicketID], RegionCode: region[l.TicketID], ItemCode: l.Line.ItemCode,
				GroupCode: it.GroupCode, UnitCode: it.UnitCode, Min: lo, Max: hi, Mid: domain.Mid(lo, hi), Adjusted: l.AdjMin != nil,
				Late: late[l.TicketID]})
		}
		return nil
	})
	return out, err
}

// HasPrice (ItemHooks.HasPrice, W11.73): mặt hàng đã có giá trên một phiếu đã gửi chính thức.
func (s *TicketService) HasPrice(ctx context.Context, tx pgx.Tx, item string) (bool, error) {
	return store.HasPricedLine(ctx, tx, item)
}

// Submitted (ItemHooks.HasSheets, PointHooks.Submitted — W11.72/W11.73): ngày day đã có phiếu Đã gửi / Nộp muộn của xã
// point (rỗng = bất kỳ xã).
func (s *TicketService) Submitted(ctx context.Context, tx pgx.Tx, day time.Time, point string) (bool, error) {
	ts, err := store.LoadTickets(ctx, tx, store.TicketFilter{Point: point, From: day, To: day, Live: true})
	if err != nil {
		return false, err
	}
	for _, t := range ts {
		if domain.Sent(t.Status) {
			return true, nil
		}
	}
	return false, nil
}
