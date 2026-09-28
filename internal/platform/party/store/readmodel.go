package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

func uuidStrings(refs []uuid.UUID) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.String())
	}
	return out
}

// MemberCounts đếm hộ thành viên đang mở (hộ còn hoạt động) của từng HTX (G-03).
func MemberCounts(ctx context.Context, tx pgx.Tx, coops []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	if len(coops) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT m.org_ref, count(*) FROM pf_party.household_membership m
	    JOIN pf_party.household h ON h.tenant_id = m.tenant_id AND h.household_ref = m.household_ref
	  WHERE m.org_ref = ANY($1::text[]::uuid[]) AND m.until_date IS NULL AND h.active GROUP BY m.org_ref`, uuidStrings(coops))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref uuid.UUID
		var n int64
		if err := rows.Scan(&ref, &n); err != nil {
			return nil, err
		}
		out[ref] = n
	}
	return out, rows.Err()
}

// ConsentState là trạng thái hiện tại của một đồng ý theo mục đích.
type ConsentState struct {
	Purpose   string
	Status    string
	EventTime time.Time
}

// ConsentStates đọc bảng chiếu đồng ý của nhiều chủ thể (G-04), theo mục đích.
func ConsentStates(ctx context.Context, tx pgx.Tx, subjects []uuid.UUID) (map[uuid.UUID][]ConsentState, error) {
	out := map[uuid.UUID][]ConsentState{}
	if len(subjects) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT subject_ref, purpose, status, event_time FROM pf_party.consent_current
	  WHERE subject_ref = ANY($1::text[]::uuid[]) ORDER BY subject_ref, purpose`, uuidStrings(subjects))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s uuid.UUID
		var c ConsentState
		if err := rows.Scan(&s, &c.Purpose, &c.Status, &c.EventTime); err != nil {
			return nil, err
		}
		out[s] = append(out[s], c)
	}
	return out, rows.Err()
}

// CooperativesByUnit đếm HTX đang hoạt động và hộ thành viên đang mở theo đơn vị (G-02).
func CooperativesByUnit(ctx context.Context, tx pgx.Tx, all bool, unitCodes []string) ([]port.UnitCooperatives, error) {
	rows, err := tx.Query(ctx, `SELECT o.admin_unit_code, count(DISTINCT o.ref), count(h.household_ref)
	  FROM pf_party.org o
	  LEFT JOIN pf_party.household_membership m ON m.tenant_id = o.tenant_id AND m.org_ref = o.ref AND m.until_date IS NULL
	  LEFT JOIN pf_party.household h ON h.tenant_id = m.tenant_id AND h.household_ref = m.household_ref AND h.active
	  WHERE o.kind = 'cooperative' AND o.active AND o.admin_unit_code IS NOT NULL
	    AND ($1::boolean OR o.admin_unit_code = ANY($2::text[]))
	  GROUP BY o.admin_unit_code ORDER BY o.admin_unit_code`, all, units(unitCodes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []port.UnitCooperatives
	for rows.Next() {
		var u port.UnitCooperatives
		if err := rows.Scan(&u.UnitCode, &u.Cooperatives, &u.Households); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// AdminOrgFilter là điều kiện liệt kê tổ chức trên Admin API (G-08).
type AdminOrgFilter struct {
	OrgFilter
	ParentRef *uuid.UUID // khác nil: chỉ con trực tiếp của tổ chức này
}

// ListOrgsAdmin liệt kê tổ chức theo loại, cha, trạng thái; phân trang cursor theo ref.
func ListOrgsAdmin(ctx context.Context, tx pgx.Tx, f AdminOrgFilter) ([]port.Org, error) {
	rows, err := tx.Query(ctx, "SELECT "+orgCols+` FROM pf_party.org
	    WHERE ($1::boolean OR admin_unit_code = ANY($2::text[]))
	      AND ($3 = '' OR kind = $3)
	      AND ($4::boolean OR active)
	      AND ($5::uuid IS NULL OR parent_ref = $5::uuid)
	      AND ref > $6
	    ORDER BY ref LIMIT $7`, f.All, units(f.Units), f.Kind, f.IncludeInactive, f.ParentRef, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []port.Org
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
