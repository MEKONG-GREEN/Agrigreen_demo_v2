package app

import (
	"context"
	"log/slog"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/jackc/pgx/v5"
)

func websiteRequester(c caller) bool { return !c.readOnly && (c.manage || c.level == "TINH") }

// RequestWebsite records D2's disconnected state. This is deliberately separate
// from approval: Admin may acknowledge it but cannot approve a production board.
func (s *TicketService) RequestWebsite(ctx context.Context, profile string, in BoardInput, idem string) (BoardView, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return BoardView{}, err
	}
	if !prof.WebsiteDeferred {
		return BoardView{}, problem.NotFound
	}
	if !websiteRequester(c) {
		return BoardView{}, problem.Forbidden
	}
	if in.Day.IsZero() {
		return BoardView{}, problem.Validation.WithField("day", "bắt buộc")
	}
	day, now := domain.BusinessDay(in.Day), s.Clock.Now()
	var version int
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if err := store.LockBoard(ctx, tx, c.tenant, prof.Name, day); err != nil {
			return err
		}
		b, err := store.CurrentBoard(ctx, tx, prof.Name, day)
		if err != nil {
			return err
		}
		if b.Seq != in.Version {
			return port.ErrBoardStale
		}
		if b.View(now) != domain.BoardApproved {
			return port.ErrBoardState
		}
		version = b.Version
		return store.InsertWebsiteAttempt(ctx, tx, ids.New(), c.tenant, prof.Name, day, version, now, c.ref,
			idemKey(idem, "board:website"), telemetry.TraceID(ctx))
	})
	if err != nil {
		return BoardView{}, mapLedgerErr(err)
	}
	slog.InfoContext(ctx, "Website chưa kết nối", "event", "price.website.requested", "outcome", domain.WebsiteNotConnected,
		"trace_id", telemetry.TraceID(ctx), "tenant_id", c.tenant, "actor_ref", c.ref,
		"profile", prof.Name, "board_date", dayStr(day), "board_version", version)
	return s.GetBoard(ctx, prof.Name, day)
}
