package app

// Phiếu báo giá — thao tác của Cấp Tỉnh / Admin sau giờ khóa và trao đổi xác minh hai chiều (W11.75; BRD Báo giá v1.3
// FN-TI-02.03 cảnh báo + yêu cầu xác minh + quyết định, FN-TI-02.04 hiệu chỉnh / sửa ngày / xóa phiếu, FN-TI-02.09 nhập
// thay, FN-XA-03.05 Xã phản hồi). Mọi thay đổi là phiên bản mới ở sổ chỉ ghi thêm (người, thời điểm, giá trị cũ → mới,
// lý do — BR-07 FN-TI-02.03, AC-03 FN-TI-02.04).

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

var errNotReviewer = problem.Forbidden.WithDetail("chỉ Cấp Tỉnh hoặc vai quản trị kiểm tra, sửa số liệu báo giá")

func reviewer(c caller) bool { return c.manage || c.level == "TINH" }

// reviewGate kiểm điều kiện chung của thao tác sau giờ khóa trên ngày day của xã p: người gọi xử lý được xã, đã qua giờ
// khóa, bảng giá ngày Chưa duyệt / Đang mở duyệt.
func (s *TicketService) reviewGate(ctx context.Context, tx pgx.Tx, sc *sheetCtx, p port.Point, day time.Time) (domain.DayPlan, error) {
	if !handles(sc.c, p) {
		return domain.DayPlan{}, port.ErrPointNotHandled.WithDetail("xã báo giá " + p.Code)
	}
	plan, err := s.dayPlan(ctx, tx, sc.prof, day)
	if err != nil {
		return plan, err
	}
	if !plan.Locked(sc.now) {
		return plan, port.ErrTicketNotLocked.WithDetail(day.Format(time.DateOnly))
	}
	b, err := s.board(ctx, tx, day)
	if err != nil {
		return plan, err
	}
	if !domain.BoardEditable(b) {
		return plan, port.ErrBoardApproved.WithDetail(day.Format(time.DateOnly))
	}
	return plan, nil
}

// SaveProxy: Cấp Tỉnh nhập thay phiếu của xã "Không nộp" (FN-TI-02.09): sau giờ khóa, bảng giá chưa duyệt / đang mở
// duyệt, xã được phân công hoặc chưa phân công; lý do bắt buộc; lưu nháp được; lưu chính thức → "Nộp muộn", sinh cảnh
// báo (đọc là Chờ rà soát). Không nhập thay đè phiếu đã gửi (BR-01).
func (s *TicketService) SaveProxy(ctx context.Context, profile string, in EntryInput, idem string) (Sheet, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return Sheet{}, err
	}
	if !reviewer(c) {
		return Sheet{}, errNotReviewer
	}
	if in.Day.IsZero() {
		return Sheet{}, problem.Validation.WithField("day", "bắt buộc")
	}
	if !domain.TextRequired(in.Reason, domain.MaxNote) {
		return Sheet{}, port.ErrReasonRequired.WithDetail("lý do nộp muộn")
	}
	day := dayOf(in.Day)
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
		plan, err := s.reviewGate(ctx, tx, sc, p, day)
		if err != nil {
			return err
		}
		if !plan.Quote {
			return port.ErrNotQuoteDay.WithDetail(day.Format(time.DateOnly))
		}
		cur, err := liveTicket(ctx, tx, in.Point, day)
		if err != nil {
			return err
		}
		if cur != nil && domain.Sent(cur.Status) {
			return port.ErrTicketExists.WithDetail("xã đã có phiếu đã gửi — không nhập thay đè (BR-01 FN-TI-02.09)")
		}
		if err := checkVersion(cur, in.Version); err != nil {
			return err
		}
		if _, err := s.writeEntry(ctx, tx, sc, p, day, cur, in, domain.SourceProxy, idem); err != nil {
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

// Adjustment là giá hiệu chỉnh của một dòng (FN-TI-02.04 B2).
type Adjustment struct {
	ItemCode string
	Min, Max decimal.Decimal
}

// ticketOp nạp phiếu id dưới khóa của xã, kiểm phiên bản đã đọc và điều kiện sau giờ khóa; trả phiếu, xã.
func (s *TicketService) ticketOp(ctx context.Context, tx pgx.Tx, sc *sheetCtx, id uuid.UUID, version int64) (store.Ticket, port.Point, error) {
	t, err := ticketByID(ctx, tx, id)
	if err != nil {
		return t, port.Point{}, err
	}
	if err := store.LockPointTickets(ctx, tx, sc.c.tenant, t.PointCode); err != nil {
		return t, port.Point{}, err
	}
	if t, err = ticketByID(ctx, tx, id); err != nil { // đọc lại dưới khóa
		return t, port.Point{}, err
	}
	if !domain.Live(t.Status) {
		return t, port.Point{}, problem.Conflict.WithDetail("phiếu đã xóa")
	}
	if t.Seq != version {
		return t, port.Point{}, checkVersion(&t, version)
	}
	p, ok, _ := sc.pointAt(t.PointCode, t.Day)
	if !ok {
		return t, p, port.ErrPointNotFound.WithDetail("xã báo giá " + t.PointCode)
	}
	if _, err := s.reviewGate(ctx, tx, sc, p, t.Day); err != nil {
		return t, p, err
	}
	return t, p, nil
}

// writeHeader ghi phiên bản đầu phiếu mới (giữ trạng thái, đổi thao tác / lý do / ngày).
func writeHeader(ctx context.Context, tx pgx.Tx, sc *sheetCtx, t store.Ticket, op, reason string, idem string) (store.Ticket, error) {
	t.Op, t.Reason = op, reason
	seq, err := store.InsertTicket(ctx, tx, ids.New(), sc.c.tenant, t, sc.now, sc.c.ref, idemKey(idem, "ticket"))
	t.Seq = seq
	return t, err
}

// Correct: Cấp Tỉnh hiệu chỉnh giá thấp nhất / cao nhất của các dòng (FN-TI-02.04 B2, B4): lý do bắt buộc; lưu cả giá Xã
// gửi và giá hiệu chỉnh (BR-05); cảnh báo của dòng chuyển "Đã hiệu chỉnh". Bảng giá và giá tham chiếu dùng giá hiệu chỉnh.
func (s *TicketService) Correct(ctx context.Context, profile string, id uuid.UUID, version int64, reason string, adj []Adjustment,
	idem string) (Sheet, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return Sheet{}, err
	}
	if !reviewer(c) {
		return Sheet{}, errNotReviewer
	}
	reason = strings.TrimSpace(reason)
	if !domain.TextRequired(reason, domain.MaxNote) {
		return Sheet{}, port.ErrReasonRequired.WithDetail("lý do hiệu chỉnh")
	}
	if len(adj) == 0 {
		return Sheet{}, problem.Validation.WithField("lines", "chọn ít nhất một dòng hiệu chỉnh")
	}
	var sh Sheet
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sc, err := s.newSheetCtx(ctx, tx, prof, c)
		if err != nil {
			return err
		}
		t, p, err := s.ticketOp(ctx, tx, sc, id, version)
		if err != nil {
			return err
		}
		if !domain.Sent(t.Status) {
			return problem.Conflict.WithDetail("chỉ hiệu chỉnh phiếu đã gửi / nộp muộn")
		}
		ls, err := store.LoadLines(ctx, tx, []uuid.UUID{t.ID})
		if err != nil {
			return err
		}
		cur := map[string]store.Line{}
		for _, l := range ls {
			cur[l.Line.ItemCode] = l
		}
		as, err := store.LoadAlerts(ctx, tx, []uuid.UUID{t.ID})
		if err != nil {
			return err
		}
		book := bookAlerts(as)
		var bad []string
		seen := map[string]bool{}
		for _, a := range adj {
			l, ok := cur[a.ItemCode]
			switch {
			case seen[a.ItemCode]:
				bad = append(bad, a.ItemCode+": trùng dòng")
			case !ok || !l.Line.Priced():
				bad = append(bad, a.ItemCode+": dòng không có giá Xã gửi (Không có hàng / chưa nhập)")
			default:
				if err := domain.ValidPair(a.Min, a.Max); err != nil {
					bad = append(bad, a.ItemCode+": "+err.Error())
				}
			}
			seen[a.ItemCode] = true
		}
		if len(bad) > 0 {
			return port.ErrPriceInvalid.WithDetail(strings.Join(bad, "; "))
		}
		for _, a := range adj {
			l := cur[a.ItemCode]
			lo, hi := a.Min, a.Max
			l.AdjMin, l.AdjMax, l.AdjReason = &lo, &hi, reason
			if _, err := store.InsertLine(ctx, tx, ids.New(), c.tenant, l, sc.now, c.ref, idemKey(idem, "line:"+a.ItemCode)); err != nil {
				return err
			}
			if al, ok := book.latest[alertKey{t.ID, a.ItemCode}]; ok && al.Status != domain.AlertCancelled {
				x := store.Alert{TicketID: t.ID, ItemCode: a.ItemCode, Status: domain.AlertAdjusted, Direction: al.Direction, Ref: al.Ref,
					Price: al.Price, ChangePct: al.ChangePct, Threshold: al.Threshold, Message: reason}
				if _, err := store.InsertAlert(ctx, tx, ids.New(), c.tenant, x, sc.now, c.ref, idemKey(idem, "alert:"+a.ItemCode)); err != nil {
					return err
				}
			}
		}
		if t, err = writeHeader(ctx, tx, sc, t, domain.OpCorrect, reason, idem); err != nil {
			return err
		}
		codes := make([]string, 0, len(adj))
		for _, a := range adj {
			codes = append(codes, a.ItemCode)
		}
		if err := emitDecided(ctx, tx, sc, p, t.Day, t.ID, codes, port.DecisionAdjusted); err != nil { // NOTIF → cán bộ Xã
			return err
		}
		sh, err = s.buildSheet(ctx, tx, sc, t.PointCode, t.Day, &t)
		return err
	})
	if err == nil {
		s.fillNames(ctx, prof, c, &sh)
	}
	return sh, mapLedgerErr(err)
}

// Redate: Cấp Tỉnh sửa ngày phiếu nhập nhầm ngày (FN-TI-02.04 B3): lý do bắt buộc; chặn ngày xã đã có phiếu (không tính
// Đã xóa — AC-04), ngày đã duyệt, ngày không phải ngày báo giá, ngày tương lai / chưa qua giờ khóa.
func (s *TicketService) Redate(ctx context.Context, profile string, id uuid.UUID, version int64, newDay time.Time, reason,
	idem string) (Sheet, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return Sheet{}, err
	}
	if !reviewer(c) {
		return Sheet{}, errNotReviewer
	}
	reason = strings.TrimSpace(reason)
	if !domain.TextRequired(reason, domain.MaxNote) {
		return Sheet{}, port.ErrReasonRequired.WithDetail("lý do sửa ngày phiếu")
	}
	if newDay.IsZero() {
		return Sheet{}, problem.Validation.WithField("day", "bắt buộc")
	}
	newDay = dayOf(newDay)
	var sh Sheet
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sc, err := s.newSheetCtx(ctx, tx, prof, c)
		if err != nil {
			return err
		}
		t, _, err := s.ticketOp(ctx, tx, sc, id, version)
		if err != nil {
			return err
		}
		if newDay.Equal(t.Day) {
			return problem.Validation.WithField("day", "trùng ngày hiện tại của phiếu")
		}
		p, ok, active := sc.pointAt(t.PointCode, newDay)
		if !ok || !active {
			return port.ErrPointInactive.WithDetail("xã báo giá " + t.PointCode + " không hoạt động ngày " + newDay.Format(time.DateOnly))
		}
		plan, err := s.reviewGate(ctx, tx, sc, p, newDay)
		if err != nil {
			return err
		}
		if !plan.Quote {
			return port.ErrNotQuoteDay.WithDetail(newDay.Format(time.DateOnly))
		}
		if other, err := liveTicket(ctx, tx, t.PointCode, newDay); err != nil {
			return err
		} else if other != nil {
			return port.ErrTicketExists.WithDetail("xã đã có phiếu ngày " + newDay.Format(time.DateOnly))
		}
		t.Day, t.OfficerRef = newDay, p.Officer
		if t, err = writeHeader(ctx, tx, sc, t, domain.OpRedate, reason, idem); err != nil {
			return err
		}
		sh, err = s.buildSheet(ctx, tx, sc, t.PointCode, t.Day, &t)
		return err
	})
	if err == nil {
		s.fillNames(ctx, prof, c, &sh)
	}
	return sh, mapLedgerErr(err)
}

// Delete: Cấp Tỉnh xóa mềm phiếu sai / trùng (FN-TI-02.04 luồng phụ; BR-04): lý do bắt buộc; phiếu "Đã xóa" không tính
// vào bảng giá, thống kê; vẫn xem được ở lịch sử.
func (s *TicketService) Delete(ctx context.Context, profile string, id uuid.UUID, version int64, reason, idem string) (Sheet, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return Sheet{}, err
	}
	if !reviewer(c) {
		return Sheet{}, errNotReviewer
	}
	reason = strings.TrimSpace(reason)
	if !domain.TextRequired(reason, domain.MaxNote) {
		return Sheet{}, port.ErrReasonRequired.WithDetail("lý do xóa phiếu")
	}
	var sh Sheet
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sc, err := s.newSheetCtx(ctx, tx, prof, c)
		if err != nil {
			return err
		}
		t, _, err := s.ticketOp(ctx, tx, sc, id, version)
		if err != nil {
			return err
		}
		t.Status = domain.TicketDeleted
		if t, err = writeHeader(ctx, tx, sc, t, domain.OpDelete, reason, idem); err != nil {
			return err
		}
		sh, err = s.buildSheet(ctx, tx, sc, t.PointCode, t.Day, &t)
		return err
	})
	if err == nil {
		s.fillNames(ctx, prof, c, &sh)
	}
	return sh, mapLedgerErr(err)
}

// ---------------------------------------------------------------------------------------------------------------------
// Xác minh hai chiều (FN-TI-02.03 B3–B6, FN-XA-03.05)
// ---------------------------------------------------------------------------------------------------------------------

// AlertRef chỉ một cảnh báo: phiếu + mặt hàng.
type AlertRef struct {
	TicketID uuid.UUID
	ItemCode string
}

// alertOp nạp cảnh báo (phiên bản mới nhất), phiếu, xã của một tham chiếu.
func (s *TicketService) alertOp(ctx context.Context, tx pgx.Tx, sc *sheetCtx, r AlertRef) (store.Alert, store.Ticket, port.Point, error) {
	t, err := ticketByID(ctx, tx, r.TicketID)
	if err != nil {
		return store.Alert{}, t, port.Point{}, err
	}
	if !domain.Live(t.Status) {
		return store.Alert{}, t, port.Point{}, port.ErrAlertNotFound.WithDetail("phiếu đã xóa")
	}
	as, err := store.LoadAlerts(ctx, tx, []uuid.UUID{t.ID})
	if err != nil {
		return store.Alert{}, t, port.Point{}, err
	}
	a, ok := bookAlerts(as).latest[alertKey{t.ID, r.ItemCode}]
	if !ok {
		return a, t, port.Point{}, port.ErrAlertNotFound.WithDetail(r.ItemCode)
	}
	p, ok, _ := sc.pointAt(t.PointCode, t.Day)
	if !ok {
		return a, t, p, port.ErrPointNotFound.WithDetail("xã báo giá " + t.PointCode)
	}
	return a, t, p, nil
}

// respondBy trả hạn phản hồi của yêu cầu xác minh gửi lúc now cho ngày có kế hoạch plan: hạn của lịch (mặc định 13:30 —
// BR-04 FN-XA-03.05); yêu cầu gửi sau hạn đó (phiếu nhập thay buổi chiều) → now + (hạn − giờ khóa), tối thiểu 30 phút
// (giả định — BRD không nêu).
func respondBy(plan domain.DayPlan, now time.Time) time.Time {
	if plan.Quote && now.Before(plan.RespondBy) {
		return plan.RespondBy
	}
	d := plan.RespondBy.Sub(plan.LockAt)
	if d < 30*time.Minute {
		d = 30 * time.Minute
	}
	return now.Add(d)
}

// RequestVerification: Cấp Tỉnh gửi yêu cầu xác minh cho một hoặc nhiều cảnh báo (FN-TI-02.03 B3; trước hoặc sau giờ
// khóa): nội dung bắt buộc (BR-05); cảnh báo Tạm tính / Chờ rà soát / Xã đã phản hồi (hỏi lại) → "Đã yêu cầu xác minh".
func (s *TicketService) RequestVerification(ctx context.Context, profile string, refs []AlertRef, message, idem string) (int, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return 0, err
	}
	if !reviewer(c) {
		return 0, errNotReviewer
	}
	message = strings.TrimSpace(message)
	if !domain.TextRequired(message, domain.MaxMessage) {
		return 0, port.ErrReasonRequired.WithDetail("nội dung yêu cầu xác minh")
	}
	if len(refs) == 0 || len(refs) > 500 {
		return 0, problem.Validation.WithField("alerts", "chọn 1–500 cảnh báo")
	}
	n := 0
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sc, err := s.newSheetCtx(ctx, tx, prof, c)
		if err != nil {
			return err
		}
		reqs := map[uuid.UUID]*port.VerificationRequestedV1{} // NOTIF: một sự kiện mỗi phiếu
		var order []uuid.UUID
		for _, r := range refs {
			a, t, p, err := s.alertOp(ctx, tx, sc, r)
			if err != nil {
				return err
			}
			if !handles(c, p) {
				return port.ErrPointNotHandled.WithDetail("xã báo giá " + p.Code)
			}
			plan, err := s.dayPlan(ctx, tx, prof, t.Day)
			if err != nil {
				return err
			}
			if b, err := s.board(ctx, tx, t.Day); err != nil {
				return err
			} else if !domain.BoardEditable(b) {
				return port.ErrBoardApproved
			}
			switch alertView(a, plan.Locked(sc.now), sc.now) {
			case domain.AlertProvisional, domain.AlertViewPending, domain.AlertResponded:
			default:
				return port.ErrAlertState.WithDetail(r.ItemCode + ": " + a.Status)
			}
			rb := respondBy(plan, sc.now)
			x := store.Alert{TicketID: t.ID, ItemCode: a.ItemCode, Status: domain.AlertRequested, Direction: a.Direction, Ref: a.Ref,
				Price: a.Price, ChangePct: a.ChangePct, Threshold: a.Threshold, Message: message, RespondBy: &rb}
			if _, err := store.InsertAlert(ctx, tx, ids.New(), c.tenant, x, sc.now, c.ref,
				idemKey(idem, "request:"+t.ID.String()+":"+a.ItemCode)); err != nil {
				return err
			}
			n++
			q, ok := reqs[t.ID]
			if !ok {
				q = &port.VerificationRequestedV1{Profile: prof.Name, Day: dayStr(t.Day), PointCode: p.Code, PointName: p.Name,
					TicketID: t.ID, RespondBy: rb.UTC().Format(time.RFC3339), OfficerRef: refPtr(p.Officer), RequesterRef: c.ref}
				reqs[t.ID] = q
				order = append(order, t.ID)
			}
			q.ItemCodes = append(q.ItemCodes, a.ItemCode)
		}
		for _, id := range order {
			q := reqs[id]
			q.ItemCodes = sortedCodes(q.ItemCodes)
			if err := enqueue(ctx, tx, c.tenant, sc.now, port.EventVerificationRequested, *q); err != nil {
				return err
			}
		}
		return nil
	})
	return n, mapLedgerErr(err)
}

// ResponseInput là phản hồi của cán bộ Xã (FN-XA-03.05 B3–B4).
type ResponseInput struct {
	AlertRef
	Choice      string // domain.ResponseKeep | ResponsePropose
	Min, Max    *decimal.Decimal
	Explanation string
}

// Respond: cán bộ Xã phản hồi yêu cầu xác minh của xã mình, cả sau giờ khóa, trước hạn phản hồi (FN-XA-03.05): giữ giá
// kèm giải trình hoặc đề xuất giá đúng kèm giải trình; không đổi giá trên phiếu (BR-02); mỗi yêu cầu một phản hồi (BR-03).
func (s *TicketService) Respond(ctx context.Context, profile string, in ResponseInput, idem string) error {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return err
	}
	if c.level != "XA" {
		return problem.Forbidden.WithDetail("chỉ cán bộ Cấp Xã phản hồi yêu cầu xác minh")
	}
	in.Explanation = strings.TrimSpace(in.Explanation)
	if !domain.TextRequired(in.Explanation, domain.MaxMessage) {
		return port.ErrReasonRequired.WithDetail("giải trình")
	}
	switch in.Choice {
	case domain.ResponseKeep:
		in.Min, in.Max = nil, nil
	case domain.ResponsePropose:
		if in.Min == nil || in.Max == nil {
			return port.ErrPriceInvalid.WithDetail("nhập giá đề xuất thấp nhất, cao nhất")
		}
		if err := domain.ValidPair(*in.Min, *in.Max); err != nil {
			return port.ErrPriceInvalid.WithDetail(err.Error())
		}
	default:
		return problem.Validation.WithField("choice", "keep hoặc propose")
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sc, err := s.newSheetCtx(ctx, tx, prof, c)
		if err != nil {
			return err
		}
		a, t, p, err := s.alertOp(ctx, tx, sc, in.AlertRef)
		if err != nil {
			return err
		}
		if p.Officer != c.ref { // BR-01: chỉ phản hồi yêu cầu của xã mình
			return port.ErrPointNotAssigned
		}
		if b, err := s.board(ctx, tx, t.Day); err != nil {
			return err
		} else if !domain.BoardEditable(b) {
			return port.ErrBoardApproved
		}
		if a.Status != domain.AlertRequested {
			return port.ErrAlertState.WithDetail("cảnh báo không ở trạng thái Đã yêu cầu xác minh (" + a.Status + ")")
		}
		if a.RespondBy != nil && !sc.now.Before(*a.RespondBy) {
			return port.ErrRespondOverdue.WithDetail("hạn " + a.RespondBy.Format(time.RFC3339))
		}
		x := a
		x.Status, x.Response, x.ProposedMin, x.ProposedMax, x.Explanation = domain.AlertResponded, in.Choice, in.Min, in.Max, in.Explanation
		if _, err = store.InsertAlert(ctx, tx, ids.New(), c.tenant, x, sc.now, c.ref, idemKey(idem, "respond")); err != nil {
			return err
		}
		return enqueue(ctx, tx, c.tenant, sc.now, port.EventVerificationResponded, port.VerificationRespondedV1{Profile: prof.Name, // NOTIF
			Day: dayStr(t.Day), PointCode: p.Code, PointName: p.Name, TicketID: t.ID, ItemCode: a.ItemCode, Choice: in.Choice,
			OwnerRef: refPtr(p.Owner), ResponderRef: c.ref})
	})
	return mapLedgerErr(err)
}

// Decide: Cấp Tỉnh chốt một cảnh báo sau giờ khóa (FN-TI-02.03 B6): "Giữ nguyên" (ghi chú bắt buộc) hoặc "Loại khỏi bảng
// giá" (lý do bắt buộc) từ Chờ rà soát / Xã đã phản hồi / Quá hạn phản hồi ("Hiệu chỉnh" = Correct); "Mở lại" một cảnh
// báo đã xử lý chỉ khi bảng giá ngày đang mở duyệt (W11.76).
func (s *TicketService) Decide(ctx context.Context, profile string, r AlertRef, decision, note, idem string) error {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return err
	}
	if !reviewer(c) {
		return errNotReviewer
	}
	note = strings.TrimSpace(note)
	if !domain.TextRequired(note, domain.MaxMessage) {
		return port.ErrReasonRequired.WithDetail("ghi chú / lý do của quyết định")
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		sc, err := s.newSheetCtx(ctx, tx, prof, c)
		if err != nil {
			return err
		}
		a, t, p, err := s.alertOp(ctx, tx, sc, r)
		if err != nil {
			return err
		}
		if _, err := s.reviewGate(ctx, tx, sc, p, t.Day); err != nil {
			return err
		}
		x := store.Alert{TicketID: t.ID, ItemCode: a.ItemCode, Direction: a.Direction, Ref: a.Ref, Price: a.Price, ChangePct: a.ChangePct,
			Threshold: a.Threshold, Message: note}
		view := alertView(a, true, sc.now)
		switch decision {
		case domain.DecisionKeep, domain.DecisionExclude:
			if view != domain.AlertViewPending && view != domain.AlertResponded && view != domain.AlertViewOverdue {
				return port.ErrAlertState.WithDetail("chỉ chốt cảnh báo Chờ rà soát, Xã đã phản hồi, Quá hạn phản hồi (" + view + ")")
			}
			x.Status = domain.AlertKept
			if decision == domain.DecisionExclude {
				x.Status = domain.AlertExcluded
			}
		case domain.DecisionReopen:
			b, err := s.board(ctx, tx, t.Day)
			if err != nil {
				return err
			}
			if b != domain.BoardReopened || !domain.AlertResolved(a.Status) || a.Status == domain.AlertCancelled {
				return port.ErrAlertState.WithDetail("chỉ mở lại cảnh báo đã xử lý khi bảng giá ngày đang mở duyệt")
			}
			x.Status = domain.AlertReopened
		default:
			return problem.Validation.WithField("decision", "keep, exclude hoặc reopen")
		}
		if _, err = store.InsertAlert(ctx, tx, ids.New(), c.tenant, x, sc.now, c.ref, idemKey(idem, "decide")); err != nil {
			return err
		}
		return emitDecided(ctx, tx, sc, p, t.Day, t.ID, []string{a.ItemCode}, decisionOf(decision)) // NOTIF → cán bộ Xã
	})
	return mapLedgerErr(err)
}
