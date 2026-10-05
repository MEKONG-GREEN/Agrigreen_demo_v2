package party

// Nguồn của bản chiếu k_ref (pf/kref, W2.5, ADR-0029): cây Nút và cấp quyền chia sẻ đang hiệu lực, đọc bằng
// vai của module (party_app, R-05). Dùng cho lần dựng lại toàn bộ (worker -kref-rebuild, cmd/drill kref);
// dòng chảy thường ngày là sự kiện pf.party.org.* / pf.party.share_grant.*.

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	krefport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

type krefSource struct{ db tenant.Beginner }

var (
	_ krefport.OrgSource        = krefSource{}
	_ krefport.ShareGrantSource = krefSource{}
)

// KRefOrgs trả nguồn cây Nút của k_ref.
func (m *Module) KRefOrgs() krefport.OrgSource { return krefSource{db: m.db} }

// KRefShareGrants trả nguồn cấp quyền đang hiệu lực của k_ref.
func (m *Module) KRefShareGrants() krefport.ShareGrantSource { return krefSource{db: m.db} }

func (s krefSource) OrgNodes(ctx context.Context, tid uuid.UUID) ([]krefport.Org, error) {
	var rows []store.OrgNode
	if err := tenant.WithTenantTx(ctx, s.db, tid, func(tx pgx.Tx) error {
		var e error
		rows, e = store.OrgNodes(ctx, tx)
		return e
	}); err != nil {
		return nil, err
	}
	out := make([]krefport.Org, 0, len(rows))
	for _, o := range rows {
		out = append(out, krefport.Org{Ref: o.Ref, Kind: o.Kind, Code: o.Code, AdminUnitCode: o.AdminUnitCode, ParentRef: o.ParentRef,
			Active: o.Active, Seq: o.LastSeq})
	}
	return out, nil
}

func (s krefSource) ActiveShareGrants(ctx context.Context, tid uuid.UUID) ([]krefport.ShareGrant, error) {
	var rows []store.ActiveGrantRow
	if err := tenant.WithTenantTx(ctx, s.db, tid, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ActiveGrantRows(ctx, tx)
		return e
	}); err != nil {
		return nil, err
	}
	out := make([]krefport.ShareGrant, 0, len(rows))
	for _, g := range rows {
		kinds := slices.Clone(g.Kinds)
		slices.Sort(kinds)
		out = append(out, krefport.ShareGrant{GrantID: g.GrantID, Grantee: g.Grantee, Kinds: kinds, ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, Seq: g.LastSeq})
	}
	return out, nil
}

// KRefTenants liệt kê tenant có tổ chức hoặc cấp quyền (pool ag_outbox_publisher — BYPASSRLS, chỉ cột
// tenant_id, pf 000092), cho lần dựng lại k_ref mọi tenant.
func KRefTenants(pool *pgxpool.Pool) func(ctx context.Context) ([]uuid.UUID, error) {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT tenant_id FROM pf_party.org UNION SELECT tenant_id FROM pf_party.share_grant`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}
