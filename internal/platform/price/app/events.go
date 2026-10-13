package app

// Phát sự kiện vòng đời phiếu / cảnh báo / giờ khóa cho pf/notify (gói NOTIF — G1, D5). Mọi sự kiện ghi outbox trong
// CÙNG giao dịch với dòng sổ gây ra nó (R-61): giao dịch hủy → không có sự kiện; phát lại thao tác cùng Idempotency-Key
// bị sổ chặn trước khi tới đây.

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// enqueue ghi một sự kiện của module vào outbox trong tx.
func enqueue(ctx context.Context, tx pgx.Tx, tid uuid.UUID, now time.Time, typ string, payload any) error {
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: typ, SchemaVersion: 1, OccurredAt: now,
		TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName, Payload: payload})
}

// refPtr: uuid.Nil → nil (JSON null).
func refPtr(r uuid.UUID) *uuid.UUID {
	if r == uuid.Nil {
		return nil
	}
	return &r
}

// sortedCodes trả bản sao đã sắp, không trùng (payload ổn định cho kiểm thử, không rỗng-nil).
func sortedCodes(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, c := range in {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

func dayStr(d time.Time) string { return d.Format(time.DateOnly) }

// emitAlertRaised: cảnh báo Tạm tính mới của phiếu (FN-TI-02.03 B1) → Cấp Tỉnh phụ trách xã.
func emitAlertRaised(ctx context.Context, tx pgx.Tx, sc *sheetCtx, p port.Point, day time.Time, ticket uuid.UUID, items []string) error {
	if len(items) == 0 {
		return nil
	}
	return enqueue(ctx, tx, sc.c.tenant, sc.now, port.EventAlertRaised, port.AlertRaisedV1{Profile: sc.prof.Name, Day: dayStr(day),
		PointCode: p.Code, PointName: p.Name, TicketID: ticket, ItemCodes: sortedCodes(items), OwnerRef: refPtr(p.Owner)})
}

// emitDecided: kết quả xử lý cảnh báo của một phiếu (FN-TI-02.03 B6, FN-TI-02.04) → cán bộ Xã.
func emitDecided(ctx context.Context, tx pgx.Tx, sc *sheetCtx, p port.Point, day time.Time, ticket uuid.UUID, items []string,
	decision string) error {
	if len(items) == 0 {
		return nil
	}
	return enqueue(ctx, tx, sc.c.tenant, sc.now, port.EventVerificationDecided, port.VerificationDecidedV1{Profile: sc.prof.Name,
		Day: dayStr(day), PointCode: p.Code, PointName: p.Name, TicketID: ticket, ItemCodes: sortedCodes(items), Decision: decision,
		OfficerRef: refPtr(p.Officer), DeciderRef: sc.c.ref})
}

// decisionOf đổi quyết định của API (domain.Decision*) sang giá trị của sự kiện.
func decisionOf(d string) string {
	switch d {
	case domain.DecisionKeep:
		return port.DecisionKept
	case domain.DecisionExclude:
		return port.DecisionExcluded
	}
	return port.DecisionReopened
}

// LockSummary (Service.LockSummary, NOTIF) đếm tình hình phiếu của ngày tại mốc khóa (trong tx của job): xã phải báo giá, đã gửi, nộp muộn, không
// nộp, cảnh báo chưa xử lý — cùng quy tắc với port.Tickets.Day (FN-TI-02.05 "Tóm tắt kỳ").
func (s *TicketService) LockSummary(ctx context.Context, tx pgx.Tx, day time.Time) (port.DaySummary, error) {
	out := port.DaySummary{Day: day, Quote: true, Locked: true}
	ps, err := loadPointSnap(ctx, tx)
	if err != nil {
		return out, err
	}
	b, err := s.board(ctx, tx, day)
	if err != nil {
		return out, err
	}
	rows, err := s.dayRows(ctx, tx, ps, day, true, b)
	if err != nil {
		return out, err
	}
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
			out.OpenAlerts += r.OpenAlerts
		}
	}
	return out, nil
}
