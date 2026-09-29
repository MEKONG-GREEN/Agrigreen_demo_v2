package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
)

// ---------------------------------------------------------------------------
// Liên kết tài khoản nông dân ↔ nông hộ (pf_accounts.farmer_link)
// ---------------------------------------------------------------------------

const linkCols = `id, user_ref, household_ref, cooperative_ref, commune_code, linked_by, linked_at, idem_key`

func scanLink(row pgx.Row) (domain.FarmerLink, error) {
	var l domain.FarmerLink
	err := row.Scan(&l.ID, &l.UserRef, &l.HouseholdRef, &l.CooperativeRef, &l.CommuneCode, &l.LinkedBy, &l.LinkedAt, &l.IdemKey)
	return l, err
}

func collectLinks(rows pgx.Rows, err error) ([]domain.FarmerLink, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.FarmerLink{}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// InsertLink ghi liên kết; nông hộ đã có liên kết đang dùng → (false, nil).
func InsertLink(ctx context.Context, tx pgx.Tx, tid uuid.UUID, l domain.FarmerLink) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_accounts.farmer_link
	    (tenant_id, id, user_ref, household_ref, cooperative_ref, commune_code, linked_by, linked_at, idem_key)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT DO NOTHING`,
		tid, l.ID, l.UserRef, l.HouseholdRef, l.CooperativeRef, l.CommuneCode, l.LinkedBy, l.LinkedAt.UTC(), l.IdemKey)
	return err == nil && tag.RowsAffected() == 1, err
}

// LinkByHousehold đọc liên kết đang dùng của nông hộ; nil nếu chưa có.
func LinkByHousehold(ctx context.Context, tx pgx.Tx, household uuid.UUID) (*domain.FarmerLink, error) {
	l, err := scanLink(tx.QueryRow(ctx, "SELECT "+linkCols+` FROM pf_accounts.farmer_link
	  WHERE household_ref = $1 AND unlinked_at IS NULL`, household))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// LinkByIdemKey đọc liên kết theo khóa idem (gọi lặp).
func LinkByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.FarmerLink, error) {
	l, err := scanLink(tx.QueryRow(ctx, "SELECT "+linkCols+" FROM pf_accounts.farmer_link WHERE idem_key = $1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// LinksByUser liệt kê liên kết đang dùng của tài khoản.
func LinksByUser(ctx context.Context, tx pgx.Tx, user uuid.UUID) ([]domain.FarmerLink, error) {
	return collectLinks(tx.Query(ctx, "SELECT "+linkCols+` FROM pf_accounts.farmer_link
	  WHERE user_ref = $1 AND unlinked_at IS NULL ORDER BY linked_at, id`, user))
}

// LinksByCooperative liệt kê liên kết đang dùng của một HTX (phân trang theo id).
func LinksByCooperative(ctx context.Context, tx pgx.Tx, coop, after uuid.UUID, limit int) ([]domain.FarmerLink, error) {
	return collectLinks(tx.Query(ctx, "SELECT "+linkCols+` FROM pf_accounts.farmer_link
	  WHERE cooperative_ref = $1 AND unlinked_at IS NULL AND id > $2 ORDER BY id LIMIT $3`, coop, after, limit))
}

// SubjectsForHouseholds trả user_ref (không trùng) có liên kết đang dùng với các nông hộ.
func SubjectsForHouseholds(ctx context.Context, tx pgx.Tx, households []uuid.UUID) ([]uuid.UUID, error) {
	if len(households) == 0 {
		return []uuid.UUID{}, nil
	}
	s := make([]string, 0, len(households))
	for _, h := range households {
		s = append(s, h.String())
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT user_ref FROM pf_accounts.farmer_link
	  WHERE household_ref = ANY($1::uuid[]) AND unlinked_at IS NULL ORDER BY user_ref`, s)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// FarmerUsers trả tập user_ref có liên kết nông dân đang dùng trong refs.
func FarmerUsers(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(refs) == 0 {
		return out, nil
	}
	s := make([]string, 0, len(refs))
	for _, r := range refs {
		s = append(s, r.String())
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT user_ref FROM pf_accounts.farmer_link
	  WHERE user_ref = ANY($1::uuid[]) AND unlinked_at IS NULL`, s)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u uuid.UUID
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out[u] = true
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Lượt kích hoạt (pf_accounts.activation_grant) — chỉ siêu dữ liệu, không token.
// ---------------------------------------------------------------------------

const grantCols = `id, user_ref, actions, channel, issued_by, issued_at, expires_at, used_at, revoked_at,
  revoked_by, coalesce(revoke_reason, ''), replaced_by, idem_key`

func scanGrant(row pgx.Row) (domain.Grant, error) {
	var g domain.Grant
	err := row.Scan(&g.ID, &g.UserRef, &g.Actions, &g.Channel, &g.IssuedBy, &g.IssuedAt, &g.ExpiresAt, &g.UsedAt,
		&g.RevokedAt, &g.RevokedBy, &g.RevokeReason, &g.ReplacedBy, &g.IdemKey)
	return g, err
}

func oneGrant(row pgx.Row) (*domain.Grant, error) {
	g, err := scanGrant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// OpenGrant đọc lượt kích hoạt đang mở (chưa dùng, chưa thu hồi — có thể đã hết hạn), khóa dòng.
func OpenGrant(ctx context.Context, tx pgx.Tx, user uuid.UUID) (*domain.Grant, error) {
	return oneGrant(tx.QueryRow(ctx, "SELECT "+grantCols+` FROM pf_accounts.activation_grant
	  WHERE user_ref = $1 AND revoked_at IS NULL AND used_at IS NULL FOR UPDATE`, user))
}

// LatestGrant đọc lượt kích hoạt mới nhất của tài khoản.
func LatestGrant(ctx context.Context, tx pgx.Tx, user uuid.UUID) (*domain.Grant, error) {
	return oneGrant(tx.QueryRow(ctx, "SELECT "+grantCols+` FROM pf_accounts.activation_grant
	  WHERE user_ref = $1 ORDER BY issued_at DESC, id DESC LIMIT 1`, user))
}

// GrantByIdemKey đọc lượt theo khóa idem (gọi lặp).
func GrantByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Grant, error) {
	return oneGrant(tx.QueryRow(ctx, "SELECT "+grantCols+" FROM pf_accounts.activation_grant WHERE idem_key = $1", key))
}

// Grants liệt kê mọi lượt của tài khoản, mới nhất trước.
func Grants(ctx context.Context, tx pgx.Tx, user uuid.UUID) ([]domain.Grant, error) {
	rows, err := tx.Query(ctx, "SELECT "+grantCols+` FROM pf_accounts.activation_grant
	  WHERE user_ref = $1 ORDER BY issued_at DESC, id DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Grant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// InsertGrant ghi lượt mới.
func InsertGrant(ctx context.Context, tx pgx.Tx, tid uuid.UUID, g domain.Grant) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.activation_grant
	    (tenant_id, id, user_ref, actions, channel, issued_by, issued_at, expires_at, idem_key)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		tid, g.ID, g.UserRef, g.Actions, g.Channel, g.IssuedBy, g.IssuedAt.UTC(), g.ExpiresAt.UTC(), g.IdemKey)
	return err
}

// RevokeGrant thu hồi lượt đang mở (phát lại hoặc khóa tài khoản).
func RevokeGrant(ctx context.Context, tx pgx.Tx, id, by uuid.UUID, reason string, replacedBy *uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_accounts.activation_grant SET revoked_at = $2, revoked_by = $3, revoke_reason = $4, replaced_by = $5
	  WHERE id = $1 AND revoked_at IS NULL AND used_at IS NULL`, id, at.UTC(), by, reason, replacedBy)
	return err
}

// MarkGrantUsed ghi nhận người dùng đã hoàn tất liên kết (đọc từ Keycloak).
func MarkGrantUsed(ctx context.Context, tx pgx.Tx, id uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_accounts.activation_grant SET used_at = $2
	  WHERE id = $1 AND revoked_at IS NULL AND used_at IS NULL`, id, at.UTC())
	return err
}
