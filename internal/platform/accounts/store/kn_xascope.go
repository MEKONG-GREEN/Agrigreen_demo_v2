package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// XaScopesAt gộp lần gán Cấp Xã còn hiệu lực tại now của phòng ban dept theo tài khoản: xã phụ trách và giới hạn cơ
// sở/hộ (assignment_attr_current). Một lần gán không giới hạn → cả tài khoản không giới hạn (hợp phạm vi).
func XaScopesAt(ctx context.Context, tx pgx.Tx, dept string, now time.Time) ([]port.XaScope, error) {
	rows, err := tx.Query(ctx, `SELECT a.user_ref, a.commune_codes, coalesce(x.facility_refs::text[], '{}'),
	    coalesce(x.household_refs::text[], '{}')
	  FROM pf_accounts.role_assignment_current a
	  LEFT JOIN pf_accounts.assignment_attr_current x ON x.tenant_id = a.tenant_id AND x.assignment_id = a.assignment_id
	  WHERE a.active AND a.valid_from <= $2 AND (a.valid_to IS NULL OR a.valid_to > $2)
	    AND a.role = $3 AND ($1 = '' OR $1 = ANY (a.department_codes))
	  ORDER BY a.user_ref, a.created_at`, dept, now, port.KNXa)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []port.XaScope
	idx := map[uuid.UUID]int{}
	unlimited := map[uuid.UUID]bool{}
	for rows.Next() {
		var user uuid.UUID
		var communes, fac, hh []string
		if err := rows.Scan(&user, &communes, &fac, &hh); err != nil {
			return nil, err
		}
		f, err := parseRefs(fac)
		if err != nil {
			return nil, err
		}
		h, err := parseRefs(hh)
		if err != nil {
			return nil, err
		}
		i, ok := idx[user]
		if !ok {
			i = len(out)
			idx[user] = i
			out = append(out, port.XaScope{ActorRef: user, Communes: []string{}, Facilities: []uuid.UUID{}, Households: []uuid.UUID{}})
		}
		x := &out[i]
		for _, c := range communes {
			if !containsStr(x.Communes, c) {
				x.Communes = append(x.Communes, c)
			}
		}
		if len(f) == 0 && len(h) == 0 {
			unlimited[user] = true
		}
		x.Facilities = append(x.Facilities, f...)
		x.Households = append(x.Households, h...)
	}
	for i := range out {
		if unlimited[out[i].ActorRef] {
			out[i].Facilities, out[i].Households = []uuid.UUID{}, []uuid.UUID{}
		}
	}
	return out, rows.Err()
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
