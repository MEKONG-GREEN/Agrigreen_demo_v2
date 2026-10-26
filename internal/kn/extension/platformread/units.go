package platformread

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	pp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/jackc/pgx/v5"
)

type Units struct{ reader }

func NewUnits(pool tenant.Beginner) *Units { return &Units{newReader(pool)} }

var (
	_ pp.AdminUnits    = (*Units)(nil)
	_ pp.AdminUnitTree = (*Units)(nil)
	// Preserve the public error contract without calling the source implementation.
	errUnitNotFound = problem.New(http.StatusNotFound, "masterdata.admin_unit_not_found", "Không có đơn vị hành chính tại thời điểm tra").WithCause(pp.ErrUnitNotFound)
)

const unitColumns = "u.code,u.name,u.level,coalesce(u.parent_code,''),u.valid_from,u.valid_to,u.successor_codes"

func dayParam(at time.Time) string { return at.In(clock.Zone()).Format(time.DateOnly) }
func wireDay(d time.Time) time.Time {
	return clock.StartOfBusinessDate(clock.Date{Year: d.Year(), Month: d.Month(), Day: d.Day()})
}
func scanUnit(row pgx.Row) (pp.AdminUnit, error) {
	var u pp.AdminUnit
	var level string
	var from time.Time
	var to *time.Time
	err := row.Scan(&u.Code, &u.Name, &level, &u.ParentCode, &from, &to, &u.SuccessorCodes)
	if err != nil {
		return pp.AdminUnit{}, err
	}
	u.Level = pp.Level(level)
	u.ValidFrom = wireDay(from)
	u.SuccessorCodes = append([]string{}, u.SuccessorCodes...)
	if to != nil {
		v := wireDay(*to)
		u.ValidTo = &v
	}
	return u, nil
}
func unitAt(ctx context.Context, tx pgx.Tx, code string, at time.Time, latest bool) (*pp.AdminUnit, error) {
	suffix := " AND (u.valid_to IS NULL OR u.valid_to>$2::date)"
	if latest {
		suffix = " ORDER BY u.valid_from DESC LIMIT 1"
	}
	u, err := scanUnit(tx.QueryRow(ctx, "SELECT "+unitColumns+" FROM pf_masterdata_pub.v1_admin_units u WHERE u.code=$1 AND u.valid_from<=$2::date"+suffix, code, dayParam(at)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
func (r *Units) Get(ctx context.Context, code string, at time.Time) (pp.AdminUnit, error) {
	var out pp.AdminUnit
	err := r.read(ctx, func(tx pgx.Tx) error {
		u, e := unitAt(ctx, tx, code, at, false)
		if e != nil {
			return e
		}
		if u == nil {
			return errUnitNotFound
		}
		out = *u
		return nil
	})
	if err != nil {
		return pp.AdminUnit{}, err
	}
	return out, nil
}
func (r *Units) Children(ctx context.Context, parent string, at time.Time) ([]pp.AdminUnit, error) {
	var out []pp.AdminUnit
	err := r.read(ctx, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "SELECT "+unitColumns+` FROM pf_masterdata_pub.v1_admin_units u
   WHERE (($1::text='' AND u.level='province') OR ($1::text<>'' AND u.parent_code=$1))
   AND u.valid_from<=$2::date AND (u.valid_to IS NULL OR u.valid_to>$2::date) ORDER BY u.code`, parent, dayParam(at))
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			u, e := scanUnit(rows)
			if e != nil {
				return e
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (r *Units) Descendants(ctx context.Context, code string, at time.Time) ([]string, error) {
	var out []string
	err := r.read(ctx, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `WITH RECURSIVE t AS (
    SELECT u.tenant_id,u.code,0 AS depth FROM pf_masterdata_pub.v1_admin_units u
     WHERE u.code=$1 AND u.valid_from<=$2::date AND (u.valid_to IS NULL OR u.valid_to>$2::date)
    UNION
    SELECT c.tenant_id,c.code,t.depth+1 FROM pf_masterdata_pub.v1_admin_units c
     JOIN t ON t.tenant_id=c.tenant_id AND c.parent_code=t.code
     WHERE c.valid_from<=$2::date AND (c.valid_to IS NULL OR c.valid_to>$2::date) AND t.depth<8)
    SELECT u.code FROM pf_masterdata_pub.v1_admin_units u JOIN t ON t.tenant_id=u.tenant_id AND t.code=u.code
     WHERE u.valid_from<=$2::date AND (u.valid_to IS NULL OR u.valid_to>$2::date) ORDER BY t.depth>0,u.code`, code, dayParam(at))
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if e := rows.Scan(&c); e != nil {
				return e
			}
			out = append(out, c)
		}
		if e := rows.Err(); e != nil {
			return e
		}
		if len(out) == 0 {
			return errUnitNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (r *Units) Current(ctx context.Context, code string, at time.Time) ([]string, error) {
	var out []string
	err := r.read(ctx, func(tx pgx.Tx) error {
		day := clock.StartOfBusinessDate(clock.BusinessDate(at))
		u, e := unitAt(ctx, tx, code, at, false)
		if e != nil {
			return e
		}
		if u != nil {
			out = []string{u.Code}
			return nil
		}
		first, e := unitAt(ctx, tx, code, at, true)
		if e != nil {
			return e
		}
		if first == nil {
			return errUnitNotFound
		}
		type key struct {
			code string
			from time.Time
		}
		seen := map[key]bool{}
		result := map[string]bool{}
		queue := []pp.AdminUnit{*first}
		for hops := 0; len(queue) > 0; hops++ {
			if hops > 64 {
				return fmt.Errorf("masterdata: chuỗi kế thừa của %s quá dài hoặc có vòng", code)
			}
			u := queue[0]
			queue = queue[1:]
			k := key{u.Code, u.ValidFrom}
			if seen[k] {
				continue
			}
			seen[k] = true
			if !u.ValidFrom.After(day) && (u.ValidTo == nil || u.ValidTo.After(day)) {
				result[u.Code] = true
				continue
			}
			if u.ValidTo == nil || u.ValidTo.After(day) {
				continue
			}
			for _, c := range u.SuccessorCodes {
				next, e := unitAt(ctx, tx, c, *u.ValidTo, false)
				if e != nil {
					return e
				}
				if next != nil {
					queue = append(queue, *next)
				}
			}
		}
		if len(result) == 0 {
			return errUnitNotFound
		}
		for c := range result {
			out = append(out, c)
		}
		slices.Sort(out)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
