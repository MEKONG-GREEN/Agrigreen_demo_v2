package app

// KN-STANDALONE-5-TT-DATA (#271): ô "Địa điểm: HTX" của Lịch công tác (Trồng trọt FN-XA-03.08, FN-TI-02.07) chọn HTX từ
// hồ sơ KN riêng (pf_extension.kn_producer, kind cooperative — FN-XA-03.03), không đọc HTX nền tảng của module HTX.

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// checkKNCooperative kiểm hồ sơ KN loại HTX: có, đang hoạt động, thuộc xã đã chọn.
func (s *Service) checkKNCooperative(ctx context.Context, ref uuid.UUID, commune string) error {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return problem.Unauthenticated
	}
	var kind, event, unit string
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT kind, event, commune_code FROM pf_extension.kn_producer
		  WHERE producer_ref = $1::uuid ORDER BY seq DESC LIMIT 1`, ref).Scan(&kind, &event, &unit)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCooperativeInvalid.WithField("cooperative_ref", "không có hồ sơ HTX")
	}
	if err != nil {
		return err
	}
	if kind != "cooperative" || event == "deactivated" || unit != commune {
		return ErrCooperativeInvalid.WithField("cooperative_ref", "không phải hồ sơ HTX đang hoạt động của xã này")
	}
	return nil
}
