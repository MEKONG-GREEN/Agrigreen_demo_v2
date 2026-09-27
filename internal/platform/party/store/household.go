package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

const householdCols = `h.household_ref, h.commune_code, h.cultivated_area_ha::text, h.active,
  coalesce(m.org_ref, '00000000-0000-0000-0000-000000000000'::uuid), m.since_date::text, h.created_at`

const householdFrom = ` FROM pf_party.household h
  LEFT JOIN pf_party.household_membership m
    ON m.tenant_id = h.tenant_id AND m.household_ref = h.household_ref AND m.until_date IS NULL`

func scanHousehold(row pgx.Row) (domain.Household, error) {
	var h domain.Household
	var area, since *string
	if err := row.Scan(&h.Ref, &h.CommuneCode, &area, &h.Active, &h.CooperativeRef, &since, &h.CreatedAt); err != nil {
		return h, err
	}
	var err error
	if h.CultivatedAreaHa, err = parseDec(area); err != nil {
		return h, err
	}
	if since != nil {
		t, err := time.Parse(time.DateOnly, *since)
		if err != nil {
			return h, err
		}
		h.MemberSince = &t
	}
	return h, nil
}

// HouseholdByRef đọc nông hộ kèm HTX hiện tại.
func HouseholdByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*domain.Household, error) {
	h, err := scanHousehold(tx.QueryRow(ctx, "SELECT "+householdCols+householdFrom+" WHERE h.household_ref = $1", ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// InsertHousehold ghi nông hộ mới và (nếu có) thành viên HTX mở.
// ON CONFLICT: hai yêu cầu song song cùng chủ hộ — bên thua nhận inserted = false.
func InsertHousehold(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, h domain.Household, actor uuid.UUID) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_party.household (tenant_id, household_ref, commune_code, cultivated_area_ha, created_by)
	  VALUES ($1, $2, $3, $4::numeric, $5) ON CONFLICT DO NOTHING`,
		tenantID, h.Ref, h.CommuneCode, decPtr(h.CultivatedAreaHa), actor)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	if h.CooperativeRef == uuid.Nil {
		return true, nil
	}
	since := h.CreatedAt
	if h.MemberSince != nil {
		since = *h.MemberSince
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_party.household_membership (tenant_id, id, household_ref, org_ref, since_date)
	  VALUES ($1, $2, $3, $4, $5::date)`, tenantID, ids.New(), h.Ref, h.CooperativeRef, since.Format(time.DateOnly))
	return err == nil, err
}

// ListHouseholdsByCooperative liệt kê nông hộ đang là thành viên của HTX, phân trang cursor.
func ListHouseholdsByCooperative(ctx context.Context, tx pgx.Tx, coop, after uuid.UUID, limit int) ([]domain.Household, error) {
	rows, err := tx.Query(ctx, "SELECT "+householdCols+householdFrom+`
	    WHERE m.org_ref = $1 AND h.household_ref > $2 ORDER BY h.household_ref LIMIT $3`, coop, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Household
	for rows.Next() {
		h, err := scanHousehold(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// CountActiveHouseholds đếm nông hộ đang là thành viên HTX (cảnh báo phạm vi ảnh hưởng, US-HTXSTATUS-01 AC-1).
func CountActiveHouseholds(ctx context.Context, tx pgx.Tx, coop uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_party.household_membership m
	    JOIN pf_party.household h ON h.tenant_id = m.tenant_id AND h.household_ref = m.household_ref
	  WHERE m.org_ref = $1 AND m.until_date IS NULL AND h.active`, coop).Scan(&n)
	return n, err
}
