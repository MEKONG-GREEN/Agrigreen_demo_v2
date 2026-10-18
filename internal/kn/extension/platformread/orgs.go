package platformread

import (
	"context"
	"errors"
	pp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Organizations struct{ reader }

func NewOrganizations(pool tenant.Beginner) *Organizations { return &Organizations{newReader(pool)} }

var (
	_ pp.Orgs         = (*Organizations)(nil)
	_ pp.OrgCodes     = (*Organizations)(nil)
	_ pp.OrgDirectory = (*Organizations)(nil)
)

// RepresentativeRef is intentionally absent from the publication and remains uuid.Nil.
const orgColumns = "org_ref,kind,code,name,coalesce(admin_unit_code,''),coalesce(parent_ref,'00000000-0000-0000-0000-000000000000'::uuid),active"

func scanOrg(row pgx.Row) (pp.Org, error) {
	var o pp.Org
	var kind string
	err := row.Scan(&o.Ref, &kind, &o.Code, &o.Name, &o.AdminUnitCode, &o.ParentRef, &o.Active)
	if err != nil {
		return pp.Org{}, err
	}
	o.Kind = pp.OrgKind(kind)
	return o, nil
}
func (r *Organizations) one(ctx context.Context, where string, arg any) (pp.Org, error) {
	var out pp.Org
	err := r.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = scanOrg(tx.QueryRow(ctx, "SELECT "+orgColumns+" FROM pf_party_pub.v1_org_tree WHERE "+where, arg))
		if errors.Is(e, pgx.ErrNoRows) {
			return pp.ErrOrgNotFound
		}
		return e
	})
	if err != nil {
		return pp.Org{}, err
	}
	return out, nil
}
func (r *Organizations) Get(ctx context.Context, ref uuid.UUID) (pp.Org, error) {
	return r.one(ctx, "org_ref=$1", ref)
}
func (r *Organizations) ByCode(ctx context.Context, code string) (pp.Org, error) {
	return r.one(ctx, "code=$1", code)
}
func (r *Organizations) OrgsByRefs(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]pp.Org, error) {
	out := make(map[uuid.UUID]pp.Org, len(refs))
	keys := make([]string, len(refs))
	for i, id := range refs {
		keys[i] = id.String()
	}
	err := r.read(ctx, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "SELECT "+orgColumns+" FROM pf_party_pub.v1_org_tree WHERE org_ref=ANY($1::text[]::uuid[])", keys)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			o, e := scanOrg(rows)
			if e != nil {
				return e
			}
			out[o.Ref] = o
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (r *Organizations) OrgsOfKind(ctx context.Context, kind pp.OrgKind, units []string, activeOnly bool) ([]pp.Org, error) {
	out := []pp.Org{}
	err := r.read(ctx, func(tx pgx.Tx) error {
		rows, e := tx.Query(ctx, "SELECT "+orgColumns+` FROM pf_party_pub.v1_org_tree
   WHERE kind=$1 AND ($2::boolean OR admin_unit_code=ANY($3::text[])) AND (NOT $4::boolean OR active) ORDER BY org_ref`, string(kind), units == nil, units, activeOnly)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			o, e := scanOrg(rows)
			if e != nil {
				return e
			}
			out = append(out, o)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
