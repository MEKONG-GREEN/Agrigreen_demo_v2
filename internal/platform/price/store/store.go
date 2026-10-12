// Package store là SQL của pf/price (schema pf_price, pf 000138). Chạy trong giao dịch của module (vai r_price,
// db.ForModule); RLS p_tenant lọc tenant. Sổ chỉ ghi thêm: không có UPDATE/DELETE.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
)

// Row là phiên bản đang hiệu lực của một mã danh mục.
type Row struct {
	Code      string
	Name      string
	SortOrder int
	Version   int64
	ValidFrom time.Time
}

// table trả bảng sổ của loại danh mục (hằng — không ghép chuỗi từ đầu vào).
func table(k domain.Kind) (string, error) {
	switch k {
	case domain.KindUnit:
		return "pf_price.unit", nil
	case domain.KindGroup:
		return "pf_price.commodity_group", nil
	case domain.KindRegion: // W11.72 nhóm vùng (pf 000144)
		return "pf_price.region_group", nil
	}
	return "", fmt.Errorf("price: loại danh mục %q", k)
}

// Effective trả phiên bản có hiệu lực tại ngày at của mỗi mã (valid_from lớn nhất ≤ at, cùng ngày lấy seq lớn nhất),
// bỏ mã đã ngưng (status 'retired'), sắp theo sort_order rồi mã.
func Effective(ctx context.Context, tx pgx.Tx, k domain.Kind, at time.Time) ([]Row, error) {
	t, err := table(k)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT code, name, sort_order, seq, valid_from FROM (
	    SELECT DISTINCT ON (code) code, name, sort_order, seq, valid_from, status FROM `+t+`
	     WHERE valid_from <= $1::date ORDER BY code, valid_from DESC, seq DESC) v
	  WHERE status = 'active' ORDER BY sort_order, code`, at.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Row, error) {
		var x Row
		err := r.Scan(&x.Code, &x.Name, &x.SortOrder, &x.Version, &x.ValidFrom)
		return x, err
	})
}

// Codes trả mọi mã đã có của loại danh mục (mọi phiên bản, mọi trạng thái).
func Codes(ctx context.Context, tx pgx.Tx, k domain.Kind) (map[string]bool, error) {
	t, err := table(k)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT code FROM `+t)
	if err != nil {
		return nil, err
	}
	codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(codes))
	for _, c := range codes {
		out[c] = true
	}
	return out, nil
}

// Count trả số mã đang hiệu lực tại ngày at.
func Count(ctx context.Context, tx pgx.Tx, k domain.Kind, at time.Time) (int, error) {
	rs, err := Effective(ctx, tx, k, at)
	return len(rs), err
}

// Insert ghi một phiên bản danh mục (status 'active'); seq, chuỗi băm do trigger fn_chain đặt.
func Insert(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, e domain.Entry, at time.Time, actor uuid.UUID, idem string) (int64, error) {
	t, err := table(e.Kind)
	if err != nil {
		return 0, err
	}
	var seq int64
	err = tx.QueryRow(ctx, `INSERT INTO `+t+` (id, tenant_id, stream_key, code, name, sort_order, status, valid_from, event_time,
	    actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3 || ':' || $4::text, $4::text, $5, $6, 'active', $7::date, $8, $9, $10, $11) RETURNING seq`,
		id, tid, string(e.Kind), e.Code, e.Name, e.SortOrder, e.ValidFrom.Format(time.DateOnly), at.UTC(), actor, idem,
		domain.RuleVersion).Scan(&seq)
	return seq, err
}
