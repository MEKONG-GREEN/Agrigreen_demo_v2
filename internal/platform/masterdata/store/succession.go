package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// InsertSuccession ghi một quan hệ kế thừa (bảng chỉ ghi thêm, pf 000089). Ghi lặp
// cùng (cặp, basis_status) không đổi gì. Trigger kiểm tiền thân đã đóng đúng ngày
// effective_on và kế thừa có hiệu lực ngày đó.
func InsertSuccession(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, s port.Succession, actor uuid.UUID) error {
	var share *string
	if s.AreaShare != nil {
		v := s.AreaShare.String()
		share = &v
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_masterdata.admin_unit_succession
	    (tenant_id, predecessor_code, predecessor_valid_from, successor_code, successor_valid_from,
	     effective_on, area_share, legal_basis, basis_status, actor_ref)
	  VALUES ($1, $2, $3::date, $4, $5::date, $6::date, $7::numeric, $8, $9, $10)
	  ON CONFLICT DO NOTHING`,
		tenantID, s.PredecessorCode, day(s.PredecessorValidFrom), s.SuccessorCode, day(s.SuccessorValidFrom),
		day(s.EffectiveOn), share, s.LegalBasis, s.BasisStatus, actor)
	return err
}

// SuccessionsFrom đọc các quan hệ kế thừa của phiên bản tiền thân (code, from), theo
// mã kế thừa; mỗi cặp có thể có cả dòng 'official' và 'pending_c9' (domain.PreferOfficial).
func SuccessionsFrom(ctx context.Context, tx pgx.Tx, code string, from time.Time) ([]port.Succession, error) {
	rows, err := tx.Query(ctx, `SELECT predecessor_code, predecessor_valid_from, successor_code, successor_valid_from,
	       effective_on, area_share::text, legal_basis, basis_status
	  FROM pf_masterdata.admin_unit_succession
	 WHERE predecessor_code = $1 AND predecessor_valid_from = $2::date
	 ORDER BY successor_code, successor_valid_from, basis_status`, code, day(from))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []port.Succession
	for rows.Next() {
		var s port.Succession
		var share *string
		if err := rows.Scan(&s.PredecessorCode, &s.PredecessorValidFrom, &s.SuccessorCode, &s.SuccessorValidFrom,
			&s.EffectiveOn, &share, &s.LegalBasis, &s.BasisStatus); err != nil {
			return nil, err
		}
		if share != nil {
			d, err := decimal.NewFromString(*share)
			if err != nil {
				return nil, err
			}
			s.AreaShare = &d
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
