package store

import (
	"context"

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

// ConsentStates đọc bảng chiếu đồng ý của nhiều chủ thể (G-04), theo mục đích.
func ConsentStates(ctx context.Context, tx pgx.Tx, subjects []uuid.UUID) (map[uuid.UUID][]port.ConsentState, error) {
	out := map[uuid.UUID][]port.ConsentState{}
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
		var c port.ConsentState
		if err := rows.Scan(&s, &c.Purpose, &c.Status, &c.EventTime); err != nil {
			return nil, err
		}
		out[s] = append(out[s], c)
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
