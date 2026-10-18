package app

// Duyệt / mở duyệt bảng giá ngày có phiên bản (W11.76, pf 000151; BRD Báo giá nông sản v1.3 FN-TI-02.05, 02.06, Mục
// II.3.3 trạng thái bảng giá ngày, II.4 CT-04). Tệp này: trạng thái + hook TicketHooks.Board/Approved, đọc màn Kiểm duyệt
// (tóm tắt kỳ, xem trước, phiên bản đã duyệt, nhật ký), khóa "Đang duyệt", duyệt (ảnh chụp dòng + phiếu, khóa tuần lưu
// lúc duyệt, sự kiện pf.price.board.published), mở duyệt, hủy mở duyệt, port.Boards.
//
// Quyền (BR-02, BR-03 FN-TI-02.05):
//   - Duyệt / mở duyệt / hủy: cán bộ Cấp Tỉnh có cờ Quản trị nghiệp vụ báo giá (Profile.StewardDuty — D11, CEO quyết),
//     mọi xã kể cả xã không thuộc phân công; Cấp Tỉnh không có cờ → 403 price.board_steward_required; vai quản trị chỉ khi
//     Profile.Board.AdminApproves (giai đoạn thử nghiệm). Mỗi lần một người: khóa "Đang duyệt" có hạn (domain.BoardLockTTL)
//     + khóa cố vấn theo (tenant, bên dùng, ngày) trong giao dịch.
//   - Xem: Cấp Tỉnh, Admin đầy đủ (xem trước, nhật ký); Cấp TW, Cấp Xã chỉ trạng thái + phiên bản đã duyệt (bảng giá chưa
//     duyệt chưa xem được — FN-TI-02.05 Mô tả).

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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

var _ port.Boards = (*TicketService)(nil)

// boardProfile là bên dùng của hook phiếu: phiếu báo giá thuộc tenant (không theo bên dùng) → bảng giá của bên dùng đầu
// tiên của gốc lắp ráp (KN = kn — hiện chỉ một bên dùng chạy phiếu xã).
func (s *TicketService) boardProfile() string {
	p, _ := s.calendarProfile("")
	return p.Name
}

// BoardState (TicketHooks.Board): trạng thái nhìn của bảng giá ngày day tại lúc đọc.
func (s *TicketService) BoardState(ctx context.Context, tx pgx.Tx, day time.Time) (string, error) {
	b, err := store.CurrentBoard(ctx, tx, s.boardProfile(), day)
	if err != nil {
		return domain.BoardOpen, err
	}
	return b.View(s.Clock.Now()), nil
}

// BoardApproved (TicketHooks.Approved): ngày day đã có bảng giá duyệt — giá tham chiếu CT-02 (BR-05 FN-TI-02.05).
func (s *TicketService) BoardApproved(ctx context.Context, tx pgx.Tx, day time.Time) (bool, error) {
	b, err := store.CurrentBoard(ctx, tx, s.boardProfile(), day)
	return b.Published(), err
}

// WireBoard nối trạng thái bảng giá ngày vào phiếu (Cấp Tỉnh chỉ sửa khi Chưa duyệt / Đang mở duyệt; giá tham chiếu chỉ
// từ ngày đã duyệt).
func (s *TicketService) WireBoard() {
	s.Hooks.Board, s.Hooks.Approved = s.BoardState, s.BoardApproved
}

// approver cho biết người gọi duyệt / mở duyệt được bảng giá: cán bộ Cấp Tỉnh có cờ Quản trị nghiệp vụ báo giá
// (Profile.StewardDuty — quyết định D11 của CEO, thay BR-02 "mọi cán bộ Cấp Tỉnh"); vai quản trị chỉ khi
// Profile.Board.AdminApproves (BR-03 FN-TI-02.05).
func approver(prof port.Profile, c caller) bool {
	return (c.level == "TINH" && c.steward) || (c.manage && prof.Board.AdminApproves)
}

// weekOf trả tuần báo cáo (lịch kỳ hệ thống) chứa ngày day; tenant lấy từ ctx (middleware xác thực đã gắn).
func (s *TicketService) weekOf(ctx context.Context, day time.Time) (domain.Week, error) {
	if s.Weeks == nil {
		return domain.DefaultWeekOf(day), nil
	}
	w, err := s.Weeks.WeekOf(ctx, day)
	if err != nil {
		return domain.Week{}, err
	}
	if w.Key == "" || w.From.After(day) || w.To.Before(day) {
		return domain.DefaultWeekOf(day), nil
	}
	return domain.Week{Key: w.Key, Year: w.Year, From: domain.BusinessDay(w.From), To: domain.BusinessDay(w.To)}, nil
}

// BoardLineView là một mặt hàng của bảng giá (xem trước hoặc phiên bản đã duyệt).
type BoardLineView struct {
	store.BoardLineRow
	GroupName string
	UnitName  string
}

// BoardLogEntry là một lần ghi sổ bảng giá (nhật ký thao tác của ngày).
type BoardLogEntry struct {
	domain.Board
	ActorName string
}

// BoardCan là thao tác người gọi làm được tại lúc đọc.
type BoardCan struct {
	Approve      bool // "Duyệt bảng giá" (khóa) / xác nhận duyệt
	Unlock       bool // hủy khóa "Đang duyệt" của mình
	Reopen       bool
	CancelReopen bool
}

// BoardView là bảng giá của một ngày ở màn Kiểm duyệt & công bố.
type BoardView struct {
	Day          time.Time
	Plan         domain.DayPlan
	Locked       bool
	Now          time.Time
	State        string // open | approving | approved | reopened (nhìn tại Now)
	Board        domain.Board
	LockedBy     uuid.UUID
	LockedByName string
	Summary      port.DaySummary // Cấp Tỉnh / Admin; dòng giá không trả (Preview là tổng hợp)
	PriceLines   int
	Preview      []BoardLineView // tổng hợp hiện tại (CT-04) — Cấp Tỉnh / Admin
	Published    *port.PublishedBoard
	PublishedRow []BoardLineView
	Log          []BoardLogEntry
	Can          BoardCan
	Blockers     []string // lý do chưa duyệt được (hiển thị cạnh nút vô hiệu)
	ReopenDays   int
	Full         bool // người gọi xem được xem trước / nhật ký
	// points: dòng giá theo xã của số liệu hiện tại (W11.77 — chụp vào board_point lúc duyệt); không trả ra ngoài.
	points []port.PriceLine
}

// boardRead gom dữ liệu màn Kiểm duyệt trong giao dịch.
func (s *TicketService) boardRead(ctx context.Context, tx pgx.Tx, prof port.Profile, c caller, day time.Time) (BoardView, []sentTicket, error) {
	now := s.Clock.Now()
	v := BoardView{Day: day, Now: now, Full: c.manage || c.level == "TINH", ReopenDays: prof.Board.ReopenDays}
	if v.ReopenDays <= 0 {
		v.ReopenDays = domain.BoardReopenDays
	}
	b, err := store.CurrentBoard(ctx, tx, prof.Name, day)
	if err != nil {
		return v, nil, err
	}
	v.Board, v.State, v.LockedBy = b, b.View(now), b.LockedBy(now)
	if v.Plan, err = s.dayPlan(ctx, tx, prof, day); err != nil {
		return v, nil, err
	}
	v.Locked = v.Plan.Locked(now)
	cat, err := loadDayCat(ctx, tx, day)
	if err != nil {
		return v, nil, err
	}
	named := func(rows []store.BoardLineRow) []BoardLineView {
		out := make([]BoardLineView, 0, len(rows))
		for _, r := range rows {
			out = append(out, BoardLineView{BoardLineRow: r, GroupName: cat.groupName(r.GroupCode), UnitName: cat.unitName(r.UnitCode)})
		}
		return out
	}
	if b.Published() {
		pb, rows, err := s.published(ctx, tx, prof.Name, day, b, now)
		if err != nil {
			return v, nil, err
		}
		v.Published, v.PublishedRow = &pb, named(rows)
	}
	var tickets []sentTicket
	if v.Full {
		v.Summary, tickets, err = s.daySummary(ctx, tx, prof, day, v.State)
		if err != nil {
			return v, nil, err
		}
		v.PriceLines = len(v.Summary.Lines)
		v.Preview = named(previewRows(cat, v.Summary.Lines))
		v.points, v.Summary.Lines = v.Summary.Lines, nil
		hist, err := store.BoardHistory(ctx, tx, prof.Name, day)
		if err != nil {
			return v, nil, err
		}
		for _, h := range hist {
			v.Log = append(v.Log, BoardLogEntry{Board: h})
		}
	}
	can := approver(prof, c)
	v.Blockers = boardBlockers(v)
	switch v.State {
	case domain.BoardOpen, domain.BoardReopened:
		v.Can.Approve = can && len(v.Blockers) == 0
		v.Can.CancelReopen = can && v.State == domain.BoardReopened
	case domain.BoardApproving:
		v.Can.Approve = can && v.LockedBy == c.ref && len(v.Blockers) == 0
		v.Can.Unlock = can && v.LockedBy == c.ref
	case domain.BoardApproved:
		v.Can.Reopen = can && domain.ReopenWithin(day, domain.BusinessDay(now), v.ReopenDays)
	}
	return v, tickets, nil
}

// boardBlockers là lý do chưa duyệt được (FN-TI-02.05 luồng ngoại lệ; chỉ khi bảng giá đang chờ duyệt).
func boardBlockers(v BoardView) []string {
	if v.State == domain.BoardApproved || !v.Full {
		return nil
	}
	var out []string
	if !v.Plan.Quote {
		out = append(out, "Không phải ngày báo giá")
	}
	if !v.Locked {
		out = append(out, "Chưa qua giờ khóa nhập")
	}
	if n := v.Summary.OpenAlerts; n > 0 {
		out = append(out, "Còn "+strconv.Itoa(n)+" cảnh báo chưa xử lý")
	}
	if v.PriceLines == 0 {
		out = append(out, "Không có dòng giá nào để công bố")
	}
	return out
}

// previewRows tổng hợp dòng giá của ngày theo CT-04 (tên, nhóm, ĐVT theo phiên bản mặt hàng tại ngày), theo nhóm rồi mã.
func previewRows(cat *dayCat, lines []port.PriceLine) []store.BoardLineRow {
	src := make([]domain.BoardSource, 0, len(lines))
	for _, l := range lines {
		src = append(src, domain.BoardSource{Point: l.PointCode, Item: l.ItemCode, Min: l.Min, Max: l.Max, Mid: l.Mid})
	}
	agg := domain.AggregateBoard(src)
	out := make([]store.BoardLineRow, 0, len(agg))
	for _, a := range agg {
		it := cat.states[a.Item]
		name := strings.TrimSpace(it.Name + " " + it.Variety)
		if name == "" {
			name = a.Item
		}
		out = append(out, store.BoardLineRow{Item: a.Item, ItemName: name, GroupCode: it.GroupCode, UnitCode: it.UnitCode, Min: a.Min,
			Max: a.Max, Avg: a.Avg, Points: a.Points})
	}
	rank := func(g string) int {
		if o, ok := cat.order[g]; ok {
			return o
		}
		return 1 << 30
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i].GroupCode), rank(out[j].GroupCode); ri != rj {
			return ri < rj
		}
		return out[i].Item < out[j].Item
	})
	return out
}

// published đọc phiên bản đã duyệt hiện hành của ngày (b.Version ≥ 1).
func (s *TicketService) published(ctx context.Context, tx pgx.Tx, profile string, day time.Time, b domain.Board, now time.Time) (port.PublishedBoard,
	[]store.BoardLineRow, error) {
	rows, err := store.LoadBoardLines(ctx, tx, profile, day, b.Version)
	if err != nil {
		return port.PublishedBoard{}, nil, err
	}
	pb := port.PublishedBoard{Day: day, Version: b.Version}
	if b.ApprovedAt != nil {
		pb.ApprovedAt = *b.ApprovedAt
	}
	pb.Reopened = reopenedView(b, now)
	wk, err := approveWeek(ctx, tx, profile, day, b.Version)
	if err != nil {
		return port.PublishedBoard{}, nil, err
	}
	pb.WeekKey = wk
	for _, r := range rows {
		pb.Lines = append(pb.Lines, port.BoardLine{ItemCode: r.Item, ItemName: r.ItemName, GroupCode: r.GroupCode, UnitCode: r.UnitCode,
			Min: r.Min, Max: r.Max, Avg: r.Avg, Points: r.Points})
	}
	// W11.77: giá theo xã của chính phiên bản (ảnh chụp board_point; phiên bản cũ dựng lại khi không mở duyệt).
	pts, err := store.BoardPointsBetween(ctx, tx, profile, day, day)
	if err != nil {
		return port.PublishedBoard{}, nil, err
	}
	for _, r := range pts {
		if r.Version == b.Version {
			pb.PointLines = append(pb.PointLines, pointLineOf(r))
		}
	}
	if len(pb.PointLines) == 0 && len(pb.Lines) > 0 {
		if prof, ok := s.calendarProfile(profile); ok {
			if pb.PointLines, err = s.rebuildPoints(ctx, tx, prof, day, b, now); err != nil {
				return port.PublishedBoard{}, nil, err
			}
		}
	}
	return pb, rows, nil
}

// approveWeek đọc khóa tuần lưu lúc duyệt phiên bản version.
func approveWeek(ctx context.Context, tx pgx.Tx, profile string, day time.Time, version int) (string, error) {
	var wk string
	err := tx.QueryRow(ctx, `SELECT coalesce(max(week_key), '') FROM pf_price.board WHERE profile = $1 AND board_date = $2::date
	  AND action = 'approve' AND board_version = $3`, profile, day.Format(time.DateOnly), version).Scan(&wk)
	return wk, err
}

// GetBoard đọc bảng giá ngày day (zero → hôm nay) cho màn Kiểm duyệt & công bố / bảng giá đã duyệt.
func (s *TicketService) GetBoard(ctx context.Context, profile string, day time.Time) (BoardView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return BoardView{}, err
	}
	day = s.dayOrToday(day)
	var v BoardView
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		v, _, err = s.boardRead(ctx, tx, prof, c, day)
		return err
	})
	if err != nil {
		return BoardView{}, err
	}
	s.boardNames(ctx, prof, c, &v)
	return v, nil
}

func (s *TicketService) boardNames(ctx context.Context, prof port.Profile, c caller, v *BoardView) {
	if !v.Full {
		return
	}
	names := s.officerNames(ctx, prof, c)
	v.LockedByName = names[v.LockedBy]
	for i := range v.Log {
		v.Log[i].ActorName = names[v.Log[i].Actor]
	}
}

// BoardDay là trạng thái bảng giá một ngày trong danh sách.
type BoardDay struct {
	Day        time.Time
	Quote      bool
	State      string
	Version    int
	ApprovedAt *time.Time
}

// ListBoards trả trạng thái bảng giá các ngày trong [from, to] (tối đa 62 ngày) — mọi vai của bên dùng.
func (s *TicketService) ListBoards(ctx context.Context, profile string, from, to time.Time) ([]BoardDay, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	today := domain.BusinessDay(s.Clock.Now())
	if to.IsZero() {
		to = today
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -13)
	}
	n := int(to.Sub(from)/(24*time.Hour)) + 1
	if n < 1 || n > 62 {
		return nil, problem.Validation.WithDetail("khoảng ngày từ 1 đến 62 ngày")
	}
	now := s.Clock.Now()
	var out []BoardDay
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		st, err := s.calendarState(ctx, tx, prof)
		if err != nil {
			return err
		}
		plans, err := s.planRange(ctx, tx, prof.Name, st, from, n)
		if err != nil {
			return err
		}
		bs, err := store.BoardsBetween(ctx, tx, prof.Name, from, to)
		if err != nil {
			return err
		}
		for _, p := range plans {
			b, ok := bs[p.Day]
			if !p.Quote && !ok {
				continue
			}
			out = append(out, BoardDay{Day: p.Day, Quote: p.Quote, State: b.View(now), Version: b.Version, ApprovedAt: b.ApprovedAt})
		}
		return nil
	})
	return out, err
}

// BoardInput là một thao tác trên bảng giá: Version = seq hiện hành người gọi đã thấy (khóa lạc quan; 0 = chưa có sổ).
type BoardInput struct {
	Day     time.Time
	Version int64
	Reason  string
}

// boardOp chạy một thao tác ghi dưới khóa của bảng giá ngày: đọc lại, kiểm phiên bản, gọi fn.
func (s *TicketService) boardOp(ctx context.Context, profile string, in BoardInput, fn func(tx pgx.Tx, prof port.Profile, c caller,
	v BoardView, tickets []sentTicket) error) (BoardView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return BoardView{}, err
	}
	if !approver(prof, c) {
		if c.manage {
			return BoardView{}, problem.Forbidden.WithDetail("Admin không duyệt bảng giá khi vận hành chính thức")
		}
		if c.level == "TINH" {
			return BoardView{}, port.ErrBoardStewardRequired
		}
		return BoardView{}, problem.Forbidden.WithDetail("chỉ cán bộ Cấp Tỉnh có cờ Quản trị nghiệp vụ báo giá duyệt / mở duyệt bảng giá")
	}
	if in.Day.IsZero() {
		return BoardView{}, problem.Validation.WithField("day", "bắt buộc")
	}
	day := domain.BusinessDay(in.Day)
	var v BoardView
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if err := store.LockBoard(ctx, tx, c.tenant, prof.Name, day); err != nil {
			return err
		}
		cur, tickets, err := s.boardRead(ctx, tx, prof, c, day)
		if err != nil {
			return err
		}
		if cur.Board.Seq != in.Version {
			return port.ErrBoardStale
		}
		if err := fn(tx, prof, c, cur, tickets); err != nil {
			return err
		}
		v, _, err = s.boardRead(ctx, tx, prof, c, day)
		return err
	})
	if err != nil {
		return BoardView{}, mapLedgerErr(err)
	}
	s.boardNames(ctx, prof, c, &v)
	return v, nil
}

// busy trả lỗi "đang duyệt bởi …" khi người khác giữ khóa còn hạn.
func (s *TicketService) busy(ctx context.Context, prof port.Profile, c caller, v BoardView) error {
	if v.State == domain.BoardApproving && v.LockedBy != c.ref {
		name := s.officerNames(ctx, prof, c)[v.LockedBy]
		if name == "" {
			name = "cán bộ khác"
		}
		return port.ErrBoardBusy.WithDetail("Đang duyệt bởi " + name)
	}
	return nil
}

// ready kiểm điều kiện duyệt (FN-TI-02.05 điều kiện tiên quyết, luồng ngoại lệ).
func ready(v BoardView) error {
	switch {
	case !v.Plan.Quote:
		return port.ErrNotQuoteDay
	case !v.Locked:
		return port.ErrBoardNotLocked
	case v.Summary.OpenAlerts > 0:
		return port.ErrBoardOpenAlerts.WithDetail("còn " + strconv.Itoa(v.Summary.OpenAlerts) + " cảnh báo")
	case v.PriceLines == 0:
		return port.ErrBoardEmpty
	}
	return nil
}

func boardWrite(prof port.Profile, v BoardView, action, status string) store.BoardWrite {
	return store.BoardWrite{Profile: prof.Name, Day: v.Day, Action: action, Status: status, Version: v.Board.Version,
		ApprovedAt: v.Board.ApprovedAt}
}

// LockBoard "Duyệt bảng giá" (FN-TI-02.05 B4): chuyển Đang duyệt — giữ khóa tới khi xác nhận / hủy / hết hạn.
func (s *TicketService) LockBoard(ctx context.Context, profile string, in BoardInput, idem string) (BoardView, error) {
	return s.boardOp(ctx, profile, in, func(tx pgx.Tx, prof port.Profile, c caller, v BoardView, _ []sentTicket) error {
		if err := s.busy(ctx, prof, c, v); err != nil {
			return err
		}
		base := v.State
		if v.State == domain.BoardApproving { // khóa của mình — gia hạn
			base = v.Board.BaseStatus
		}
		if base != domain.BoardOpen && base != domain.BoardReopened {
			return port.ErrBoardState.WithDetail("bảng giá đã duyệt — mở duyệt trước khi duyệt lại")
		}
		if err := ready(v); err != nil {
			return err
		}
		until := v.Now.Add(domain.BoardLockTTL)
		w := boardWrite(prof, v, domain.BoardLock, domain.BoardApproving)
		w.BaseStatus, w.LockUntil = base, &until
		_, err := store.InsertBoard(ctx, tx, ids.New(), c.tenant, w, v.Now, c.ref, idemKey(idem, "board:lock"))
		return err
	})
}

// UnlockBoard hủy "Đang duyệt" của mình (FN-TI-02.05: Xác nhận duyệt / hủy) → trạng thái trước khóa.
func (s *TicketService) UnlockBoard(ctx context.Context, profile string, in BoardInput, idem string) (BoardView, error) {
	return s.boardOp(ctx, profile, in, func(tx pgx.Tx, prof port.Profile, c caller, v BoardView, _ []sentTicket) error {
		if err := s.busy(ctx, prof, c, v); err != nil {
			return err
		}
		if v.State != domain.BoardApproving {
			return port.ErrBoardState.WithDetail("bảng giá không ở trạng thái Đang duyệt")
		}
		_, err := store.InsertBoard(ctx, tx, ids.New(), c.tenant, boardWrite(prof, v, domain.BoardUnlock, v.Board.BaseStatus), v.Now, c.ref,
			idemKey(idem, "board:unlock"))
		return err
	})
}

// ApproveBoard xác nhận duyệt (FN-TI-02.05): bảng giá Chưa duyệt / Đang mở duyệt / Đang duyệt (khóa của mình) → Đã duyệt
// phiên bản mới; ảnh chụp dòng bảng giá (CT-04) và phiếu tính vào; khóa tuần + số ngày báo giá theo lịch của tuần lưu lúc
// duyệt; phát pf.price.board.published (outbox, cùng giao dịch).
func (s *TicketService) ApproveBoard(ctx context.Context, profile string, in BoardInput, idem string) (BoardView, error) {
	return s.boardOp(ctx, profile, in, func(tx pgx.Tx, prof port.Profile, c caller, v BoardView, tickets []sentTicket) error {
		if err := s.busy(ctx, prof, c, v); err != nil {
			return err
		}
		if v.State == domain.BoardApproved {
			return port.ErrBoardState.WithDetail("bảng giá đã duyệt — mở duyệt trước khi duyệt lại")
		}
		if err := ready(v); err != nil {
			return err
		}
		wk, err := s.weekOf(ctx, v.Day)
		if err != nil {
			return err
		}
		st, err := s.calendarState(ctx, tx, prof)
		if err != nil {
			return err
		}
		plans, err := s.planRange(ctx, tx, prof.Name, st, wk.From, int(wk.To.Sub(wk.From)/(24*time.Hour))+1)
		if err != nil {
			return err
		}
		scheduled := 0
		for _, p := range plans {
			if p.Quote {
				scheduled++
			}
		}
		version := v.Board.Version + 1
		approvedAt := v.Now
		w := boardWrite(prof, v, domain.BoardApprove, domain.BoardApproved)
		w.Version, w.ApprovedAt = version, &approvedAt
		w.WeekKey, w.WeekYear, w.WeekFrom, w.WeekTo, w.DaysScheduled = wk.Key, wk.Year, wk.From, wk.To, scheduled
		w.LinesCount, w.TicketsCount = len(v.Preview), len(tickets)
		if _, err := store.InsertBoard(ctx, tx, ids.New(), c.tenant, w, v.Now, c.ref, idemKey(idem, "board:approve")); err != nil {
			return err
		}
		for _, l := range v.Preview {
			if err := store.InsertBoardLine(ctx, tx, ids.New(), c.tenant, prof.Name, v.Day, version, l.BoardLineRow, v.Now, c.ref,
				idemKey(idem, "bline:"+l.Item)); err != nil {
				return err
			}
		}
		for _, l := range v.points { // W11.77: giá theo xã của phiên bản (Bảng giá tổng hợp theo vùng / xã, dashboard)
			if err := store.InsertBoardPoint(ctx, tx, ids.New(), c.tenant, prof.Name, v.Day, version, boardPointRow(l), v.Now, c.ref,
				idemKey(idem, "bpoint:"+l.PointCode+":"+l.ItemCode)); err != nil {
				return err
			}
		}
		for _, t := range tickets {
			if err := store.InsertBoardTicket(ctx, tx, ids.New(), c.tenant, prof.Name, v.Day, version, store.BoardTicketRow{TicketID: t.ID,
				TicketSeq: t.Seq, PointCode: t.Point, CommuneCode: t.Commune, Late: t.Late}, v.Now, c.ref, idemKey(idem, "bticket:"+t.ID.String())); err != nil {
				return err
			}
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: c.tenant, Type: port.EventBoardPublished, SchemaVersion: 1,
			OccurredAt: v.Now, TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName, Payload: port.BoardPublishedV1{
				Profile: prof.Name, BoardDate: v.Day.Format(time.DateOnly), Version: version, ApprovedAt: approvedAt.UTC().Format(time.RFC3339),
				WeekKey: wk.Key, WeekYear: wk.Year, Items: len(v.Preview), Tickets: len(tickets), Reapproval: version > 1}})
	})
}

// ReopenBoard "Mở duyệt" (FN-TI-02.06): bảng giá Đã duyệt của ngày trong giới hạn (Profile.Board.ReopenDays) → Đang mở
// duyệt; lý do bắt buộc. Phiên bản đã duyệt vẫn công bố (board_reopened) tới khi duyệt lại.
func (s *TicketService) ReopenBoard(ctx context.Context, profile string, in BoardInput, idem string) (BoardView, error) {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return BoardView{}, port.ErrReasonRequired.WithDetail("lý do mở duyệt")
	}
	return s.boardOp(ctx, profile, in, func(tx pgx.Tx, prof port.Profile, c caller, v BoardView, _ []sentTicket) error {
		if err := s.busy(ctx, prof, c, v); err != nil {
			return err
		}
		if v.State != domain.BoardApproved {
			return port.ErrBoardState.WithDetail("chỉ mở duyệt bảng giá Đã duyệt")
		}
		if !domain.ReopenWithin(v.Day, domain.BusinessDay(v.Now), v.ReopenDays) {
			return port.ErrReopenExpired.WithDetail("chỉ mở duyệt ngày báo giá trong " + strconv.Itoa(v.ReopenDays) + " ngày gần nhất")
		}
		w := boardWrite(prof, v, domain.BoardReopen, domain.BoardReopened)
		w.Reason = reason
		if _, err := store.InsertBoard(ctx, tx, ids.New(), c.tenant, w, v.Now, c.ref, idemKey(idem, "board:reopen")); err != nil {
			return err
		}
		// NOTIF: chuông Cấp Tỉnh "bảng giá đang mở duyệt" (FN-TI-02.06) — không kèm lý do (văn bản tự do).
		return enqueue(ctx, tx, c.tenant, v.Now, port.EventBoardReopened, port.BoardReopenedV1{Profile: prof.Name,
			BoardDate: dayStr(v.Day), Version: v.Board.Version, ReopenedAt: v.Now.UTC().Format(time.RFC3339), ActorRef: c.ref})
	})
}

// CancelReopen "Hủy mở duyệt" (FN-TI-02.06 luồng phụ): chỉ khi chưa sửa gì từ lúc mở duyệt → Đã duyệt phiên bản cũ.
func (s *TicketService) CancelReopen(ctx context.Context, profile string, in BoardInput, idem string) (BoardView, error) {
	return s.boardOp(ctx, profile, in, func(tx pgx.Tx, prof port.Profile, c caller, v BoardView, _ []sentTicket) error {
		if err := s.busy(ctx, prof, c, v); err != nil {
			return err
		}
		if v.State != domain.BoardReopened {
			return port.ErrBoardState.WithDetail("bảng giá không ở trạng thái Đang mở duyệt")
		}
		var since time.Time
		for _, h := range v.Log {
			if h.Action == domain.BoardReopen {
				since = h.RecordedAt
			}
		}
		changed, err := store.DayChangedSince(ctx, tx, v.Day, since)
		if err != nil {
			return err
		}
		if changed {
			return port.ErrBoardChanged
		}
		w := boardWrite(prof, v, domain.BoardCancelReopen, domain.BoardApproved)
		w.Reason = strings.TrimSpace(in.Reason)
		_, err = store.InsertBoard(ctx, tx, ids.New(), c.tenant, w, v.Now, c.ref, idemKey(idem, "board:cancel_reopen"))
		return err
	})
}

// Published (port.Boards) trả phiên bản đã duyệt hiện hành của ngày day cho tenant của ctx (không kiểm vai).
func (s *TicketService) Published(ctx context.Context, profile string, day time.Time) (port.PublishedBoard, bool, error) {
	prof, ok := s.calendarProfile(profile)
	if !ok {
		return port.PublishedBoard{}, false, port.ErrProfileNotFound
	}
	day = domain.BusinessDay(day)
	var (
		out   port.PublishedBoard
		found bool
	)
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		b, err := store.CurrentBoard(ctx, tx, prof.Name, day)
		if err != nil || !b.Published() {
			return err
		}
		out, _, err = s.published(ctx, tx, prof.Name, day, b, s.Clock.Now())
		found = err == nil
		return err
	})
	return out, found, err
}
