package app

// Bề mặt đối tác của pf/geo (ADR-0020): danh bạ thửa (loại plot_directory) cho tài khoản dịch vụ
// tenant bên nhận (MKG). Kiểm cấp quyền qua pf/party (port.PartnerAccess), đọc dữ liệu của tenant
// BÊN CẤP bằng pool geo_app với ngữ cảnh tenant tường minh (không vai BYPASSRLS), chỉ trường tối
// thiểu (không nông hộ), ghi sổ đọc trước khi trả dữ liệu.

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// PartnerPlotPage là một trang danh bạ thửa cho bên nhận.
type PartnerPlotPage struct {
	Items []store.PartnerPlot
	Next  *uuid.UUID
}

func (s *Service) partner(ctx context.Context, grantor uuid.UUID) (partyport.ShareGrant, error) {
	if s.Sharing == nil {
		return partyport.ShareGrant{}, partyport.ErrShareGrantMissing // chưa nối pf/party: mặc định từ chối
	}
	return s.Sharing.Authorize(ctx, partyport.KindPlotDirectory, grantor)
}

// PartnerPlots trả danh bạ thửa của bên cấp theo con trỏ ref; coop ≠ Nil lọc theo HTX.
func (s *Service) PartnerPlots(ctx context.Context, grantor, coop, after uuid.UUID, limit int, query string) (PartnerPlotPage, error) {
	g, err := s.partner(ctx, grantor)
	if err != nil {
		return PartnerPlotPage{}, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var pg PartnerPlotPage
	if err := s.inTx(ctx, g.Grantor, func(tx pgx.Tx) error {
		var e error
		pg.Items, e = store.PartnerPlots(ctx, tx, coop, after, limit+1)
		return e
	}); err != nil {
		return PartnerPlotPage{}, err
	}
	if len(pg.Items) > limit {
		pg.Items = pg.Items[:limit]
		n := pg.Items[limit-1].Ref
		pg.Next = &n
	}
	if err := s.Sharing.RecordRead(ctx, g, partyport.PartnerRead{Kind: partyport.KindPlotDirectory, Endpoint: "GET /v1/partner/plots",
		Query: query, Items: len(pg.Items)}); err != nil {
		return PartnerPlotPage{}, err
	}
	return pg, nil
}

// PartnerPlot trả một thửa của bên cấp; không có → 404 geo.plot_not_found (vẫn ghi sổ đọc).
func (s *Service) PartnerPlot(ctx context.Context, grantor, ref uuid.UUID) (store.PartnerPlot, error) {
	g, err := s.partner(ctx, grantor)
	if err != nil {
		return store.PartnerPlot{}, err
	}
	var p *store.PartnerPlot
	if err := s.inTx(ctx, g.Grantor, func(tx pgx.Tx) error {
		var e error
		p, e = store.PartnerPlotByRef(ctx, tx, ref)
		return e
	}); err != nil {
		return store.PartnerPlot{}, err
	}
	n := 0
	if p != nil {
		n = 1
	}
	if err := s.Sharing.RecordRead(ctx, g, partyport.PartnerRead{Kind: partyport.KindPlotDirectory, Endpoint: "GET /v1/partner/plots/{ref}",
		Query: "ref=" + ref.String(), Items: n}); err != nil {
		return store.PartnerPlot{}, err
	}
	if p == nil {
		return store.PartnerPlot{}, ErrPlotNotFound
	}
	return *p, nil
}
