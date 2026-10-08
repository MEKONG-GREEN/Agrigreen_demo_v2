package store

// KN v7.7 FN-XA-03.03: tên/người đại diện hiện hành của Nút HTX và Nút cha mặc định của HTX mới. Hồ sơ HTX, nông
// hộ và sổ kn_profile_event ở pf/cooperative (W3.3, partyport.CoopProfiles).
// Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant (tenant.WithTenantTx).

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UpdateOrgProfile ghi tên và người đại diện hiện hành của tổ chức (bảng chiếu, cùng giao dịch với sổ).
func UpdateOrgProfile(ctx context.Context, tx pgx.Tx, ref uuid.UUID, name string, representative uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE pf_party.org SET name = $2, representative_ref = $3, updated_at = now() WHERE ref = $1`,
		ref, name, nilIfZero(representative))
	return err
}

// CommuneParent trả Nút cha mặc định cho HTX mới của xã: Nút xã (extension_team) đang hoạt động của xã, nếu
// không có thì TTKN tỉnh đang hoạt động của tỉnh; Nil nếu không có cả hai (HTX đứng gốc — domain cho phép).
func CommuneParent(ctx context.Context, tx pgx.Tx, commune, province string) (uuid.UUID, error) {
	var ref uuid.UUID
	err := tx.QueryRow(ctx, `SELECT ref FROM pf_party.org
	    WHERE active AND ((kind = 'extension_team' AND admin_unit_code = $1) OR (kind = 'ttkn_province' AND admin_unit_code = $2 AND $2 <> ''))
	    ORDER BY (kind = 'extension_team') DESC, ref LIMIT 1`, commune, province).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return ref, err
}
