package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OrgNode là một dòng pf_party.org cho bản chiếu k_ref (pf/kref, W2.5): mã, loại, đơn vị hành chính, cha,
// trạng thái, seq cuối — không tên, không người đại diện.
type OrgNode struct {
	Ref           uuid.UUID
	Kind          string
	Code          string
	AdminUnitCode string
	ParentRef     uuid.UUID
	Active        bool
	LastSeq       int64
}

// OrgNodes liệt kê mọi tổ chức của tenant trong giao dịch (kể cả đã vô hiệu hóa).
func OrgNodes(ctx context.Context, tx pgx.Tx) ([]OrgNode, error) {
	rows, err := tx.Query(ctx, `SELECT ref, kind, code, coalesce(admin_unit_code, ''),
	    coalesce(parent_ref, '00000000-0000-0000-0000-000000000000'::uuid), active, last_seq
	  FROM pf_party.org ORDER BY ref`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (OrgNode, error) {
		var o OrgNode
		return o, r.Scan(&o.Ref, &o.Kind, &o.Code, &o.AdminUnitCode, &o.ParentRef, &o.Active, &o.LastSeq)
	})
}

// ActiveGrantRow là một cấp quyền đang hiệu lực (status = active) của tenant bên cấp.
type ActiveGrantRow struct {
	GrantID   uuid.UUID
	Grantee   uuid.UUID
	Kinds     []string
	ValidFrom time.Time
	ValidTo   time.Time
	LastSeq   int64
	// Điều khoản v2 (W4.1; chiếu sang k_ref ở W4.2). Rỗng/Nil ở cấp phép trước v2 (partner kế thừa).
	Class        string
	ModuleTable  string
	ScopeNodeRef uuid.UUID
	PurposeCode  string
	BasisRef     string
}

// ActiveGrantRows liệt kê cấp quyền status = active mà tenant trong giao dịch là bên cấp.
func ActiveGrantRows(ctx context.Context, tx pgx.Tx) ([]ActiveGrantRow, error) {
	rows, err := tx.Query(ctx, `SELECT grant_id, grantee_tenant_id, kinds, valid_from, valid_to, last_seq,
	    coalesce(grant_class, ''), coalesce(module_table, ''), coalesce(scope_node_ref, '00000000-0000-0000-0000-000000000000'::uuid),
	    coalesce(purpose_code, ''), coalesce(basis_ref, '')
	  FROM pf_party.share_grant WHERE status = 'active' ORDER BY grant_id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ActiveGrantRow, error) {
		var g ActiveGrantRow
		return g, r.Scan(&g.GrantID, &g.Grantee, &g.Kinds, &g.ValidFrom, &g.ValidTo, &g.LastSeq, &g.Class, &g.ModuleTable, &g.ScopeNodeRef,
			&g.PurposeCode, &g.BasisRef)
	})
}
