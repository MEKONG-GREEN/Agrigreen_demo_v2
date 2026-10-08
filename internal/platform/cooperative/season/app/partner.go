package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// HarvestDatesPage là một trang ngày thu hoạch của vụ cho bên nhận; Next = sync_seq cuối.
type HarvestDatesPage struct {
	Items []store.HarvestDates
	Next  int64
}

// PartnerHarvestDates trả ngày gieo / thu hoạch của vụ ở tenant bên cấp (cấp quyền
// season_harvest_dates, ADR-0020; proto FM-01/02), con trỏ sync_seq, ghi sổ đọc.
func (s *Service) PartnerHarvestDates(ctx context.Context, grantor, plot uuid.UUID, after int64, limit int, query string) (HarvestDatesPage, error) {
	g, err := s.partnerGrant(ctx, partyport.KindSeasonHarvestDates, grantor)
	if err != nil {
		return HarvestDatesPage{}, err
	}
	if after < 0 {
		return HarvestDatesPage{}, problem.Validation.WithField("after", "≥ 0")
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	pg := HarvestDatesPage{Next: after}
	if err := s.inTx(ctx, g.Grantor, func(tx pgx.Tx) error {
		var e error
		pg.Items, e = store.SeasonHarvestDates(ctx, tx, plot, after, limit)
		return e
	}); err != nil {
		return HarvestDatesPage{}, err
	}
	if n := len(pg.Items); n > 0 {
		pg.Next = pg.Items[n-1].SyncSeq
	}
	if err := s.Sharing.RecordRead(ctx, g, partyport.PartnerRead{Kind: partyport.KindSeasonHarvestDates,
		Endpoint: "GET /v1/partner/season-harvest-dates", Query: query, Items: len(pg.Items)}); err != nil {
		return HarvestDatesPage{}, err
	}
	return pg, nil
}

// partnerGrant kiểm cấp quyền chia sẻ loại kind cho tenant người gọi (ADR-0020). Chưa nối pf/party → 403
// (mặc định từ chối).
func (s *Service) partnerGrant(ctx context.Context, kind string, grantor uuid.UUID) (partyport.ShareGrant, error) {
	if s.Sharing == nil {
		if _, err := authz.Require(ctx, partyport.RolePartnerSync); err != nil {
			return partyport.ShareGrant{}, err
		}
		return partyport.ShareGrant{}, partyport.ErrShareGrantMissing
	}
	return s.Sharing.Authorize(ctx, kind, grantor)
}
