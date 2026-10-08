package app

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
)

// ChecksumPage là một trang tổng kiểm theo phiên bản tập dữ liệu (R-INT-10, ADR-0033 §4; W5.7): mỗi phiên bản có
// số bản ghi + SHA-256 của ảnh chụp NDJSON (đúng tổng kiểm ghi lúc dựng, bất biến).
type ChecksumPage struct {
	Versions []store.DatasetVersion
	// Next: phiên bản bắt đầu trang kế (0 = hết).
	Next int64
}

// DatasetChecksums đọc tối đa limit phiên bản của dataset từ phiên bản from (≥ 1) ở tenant đơn vị vận hành.
func (s *Service) DatasetChecksums(ctx context.Context, dataset string, from int64, limit int) (ChecksumPage, error) {
	var p ChecksumPage
	if from < 1 {
		from = 1
	}
	err := s.operatorTx(ctx, func(tx pgx.Tx) error {
		vs, err := store.DatasetVersionsFrom(ctx, tx, dataset, from, limit+1)
		if err != nil {
			return err
		}
		if len(vs) > limit {
			p.Next, vs = vs[limit].Version, vs[:limit]
		}
		p.Versions = vs
		return nil
	})
	return p, err
}
