package app

// Job dựng feed tham chiếu của dữ liệu ĐÃ CÔNG BỐ (W11.81 — port.FeedPriceBoardDaily; ADR-0033 §2, R-INT-05): với mỗi
// feed có nguồn (Service.PublishedSources) và đơn vị công bố (Service.Publishers), hỏi module chủ (pf/price — chỉ bảng giá
// ĐÃ DUYỆT) cho từng đơn vị, gộp thành MỘT phiên bản ở tenant đơn vị vận hành (phân vùng rỗng, grant_class reference) qua
// cùng publish của W5.3: ValidateRecord mọi bản ghi (một bản ghi sai → cả phiên bản không ghi), upsert khi đổi, retract
// khi biến mất, ảnh chụp + tổng kiểm. Một đơn vị lỗi → KHÔNG dựng phiên bản (tránh retract nhầm bản ghi của đơn vị đó).

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// PublishedFeeds là các feed tham chiếu của dữ liệu đã công bố có nguồn ở W11.81, theo thứ tự dựng.
var PublishedFeeds = []string{port.FeedPriceBoardDaily}

// BuildPublished dựng một phiên bản của feed tham chiếu dataset từ mọi đơn vị công bố.
func (s *Service) BuildPublished(ctx context.Context, dataset string) (BuildResult, error) {
	c, err := FeedContract(dataset, 0)
	if err != nil {
		return BuildResult{}, err
	}
	if c.GrantClass != integration.GrantReference {
		return BuildResult{}, port.ErrUnknownFeed.WithDetail(dataset + " không phải feed tham chiếu")
	}
	src, pubs := s.PublishedSources[dataset], s.Publishers[dataset]
	if src == nil || len(pubs) == 0 {
		return BuildResult{}, ErrDependencyUnavailable.WithDetail("chưa nối nguồn feed " + dataset + " hoặc đơn vị công bố")
	}
	var recs []domain.KeyedRecord
	for _, p := range pubs {
		raw, err := src.PublishedRecords(ctx, dataset, p.Tenant, p.Code)
		if err != nil {
			return BuildResult{}, fmt.Errorf("đơn vị công bố %s: %w", p.Code, err)
		}
		rs, err := keyed(c, raw)
		if err != nil {
			return BuildResult{}, fmt.Errorf("đơn vị công bố %s: %w", p.Code, err)
		}
		recs = append(recs, rs...)
	}
	n := len(pubs)
	return s.publish(ctx, publishInput{contract: c, asOf: s.Clock.Now(), contributors: &n, touch: true,
		shape: func(pgx.Tx, map[string]bool) (shaped, error) { return shaped{records: recs}, nil }})
}
