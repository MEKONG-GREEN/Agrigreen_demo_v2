package app

// Job dựng feed lớp partner theo bên cấp (W5.5 — ADR-0033 §2, §5; port.FeedCooperativeDirectory,
// port.FeedPlotDirectory): với mỗi feed có nguồn (Service.PartnerSources),
//
//  1. bên nhận = client_tenant_id của khách hàng API active có đăng ký active với product = tên feed;
//  2. bên cấp = mọi tenant đang có cấp quyền loại grant_kind của hợp đồng cho một bên nhận đó (Grantors —
//     party/port/grantlookup, hai phía);
//  3. mỗi bên cấp một phân vùng (partition_key = tenant bên cấp): bản ghi lấy từ module chủ (PartnerSource) đi qua
//     cùng publish của W5.3 (ValidateRecord mọi bản ghi, upsert khi đổi, retract khi biến mất), cộng phát lại bản
//     ghi cũ hơn port.PartnerFeedRefresh và nâng as_of cả khi không đổi.
//
// Tính MỘT lần cho mỗi bên cấp, phát cho mọi bên nhận có quyền: cổng kiểm cấp quyền ở mỗi lời gọi (thu hồi → 403
// ngay), không lọc từng hàng theo bên nhận. Phân vùng của bên cấp không còn bên nhận nào có quyền thì không dựng
// nữa (không ai đọc được); dữ liệu đã phát hết hạn theo hạn giữ 30 ngày.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
)

// PartnerFeeds là các feed lớp partner có nguồn ở W5.5, theo thứ tự dựng.
var PartnerFeeds = []string{port.FeedCooperativeDirectory, port.FeedPlotDirectory}

// partnerGrantors trả các bên cấp (sắp, không trùng) cần dựng phân vùng cho feed name / loại cấp quyền kind.
func (s *Service) partnerGrantors(ctx context.Context, name, kind string) ([]uuid.UUID, error) {
	var grantees []uuid.UUID
	if err := s.operatorTx(ctx, func(tx pgx.Tx) error {
		var err error
		grantees, err = store.FeedGrantees(ctx, tx, name)
		return err
	}); err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	var out []uuid.UUID
	for _, g := range grantees {
		gs, err := s.Grantors.ActiveGrantors(ctx, g, kind, now)
		if err != nil {
			return nil, fmt.Errorf("bên cấp của bên nhận %s: %w", g, err)
		}
		for _, x := range gs {
			if !slices.Contains(out, x) {
				out = append(out, x)
			}
		}
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return out, nil
}

// BuildPartner dựng mọi phân vùng (bên cấp) của feed lớp partner dataset. Lỗi một bên cấp không chặn bên cấp khác.
func (s *Service) BuildPartner(ctx context.Context, dataset string) ([]BuildResult, error) {
	c, err := FeedContract(dataset, 0)
	if err != nil {
		return nil, err
	}
	if c.GrantClass != integration.GrantPartner || c.GrantKind == "" {
		return nil, port.ErrUnknownFeed.WithDetail(dataset + " không phải feed lớp partner")
	}
	src := s.PartnerSources[dataset]
	if src == nil || s.Grantors == nil {
		return nil, ErrDependencyUnavailable.WithDetail("chưa nối nguồn feed " + dataset + " hoặc danh sách bên cấp")
	}
	grantors, err := s.partnerGrantors(ctx, dataset, c.GrantKind)
	if err != nil {
		return nil, err
	}
	var out []BuildResult
	var errs []error
	for _, g := range grantors {
		raw, err := src.PartnerRecords(ctx, dataset, g)
		if err != nil {
			errs = append(errs, fmt.Errorf("bên cấp %s: %w", g, err))
			continue
		}
		recs, err := keyed(c, raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("bên cấp %s: %w", g, err))
			continue
		}
		res, err := s.publish(ctx, publishInput{contract: c, partition: g.String(), asOf: s.Clock.Now(), refresh: port.PartnerFeedRefresh, touch: true,
			shape: func(pgx.Tx, map[string]bool) (shaped, error) { return shaped{records: recs}, nil }})
		if err != nil {
			errs = append(errs, fmt.Errorf("bên cấp %s: %w", g, err))
			continue
		}
		out = append(out, res)
	}
	return out, errors.Join(errs...)
}

// keyed gắn entity_key (dựng từ bản ghi theo hợp đồng) cho bản ghi của nguồn; ValidateRecord chạy ở publish.
func keyed(c *integration.Contract, raw []json.RawMessage) ([]domain.KeyedRecord, error) {
	recs := make([]domain.KeyedRecord, 0, len(raw))
	for _, r := range raw {
		key, err := c.EntityKeyOf(r)
		if err != nil {
			return nil, port.ErrRecordRejected.WithDetail(fmt.Sprintf("%s: %v", c.ID(), err)).WithCause(err)
		}
		recs = append(recs, domain.KeyedRecord{EntityKey: key, Record: r})
	}
	return recs, nil
}
