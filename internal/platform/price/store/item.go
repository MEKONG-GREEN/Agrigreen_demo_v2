package store

// SQL của mặt hàng theo thời kỳ (W11.73, pf 000141): sổ pf_price.item, item_assignment, item_proposal, item_set và ghi
// phiên bản nhóm hàng (pf_price.commodity_group của 000138). Đọc cả sổ của tenant rồi chiếu ở domain (danh mục cỡ trăm
// mặt hàng × vài phiên bản — BRD: 26 xã, 30–40 mặt hàng/xã).

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
)

// Meta là thông tin ghi của một phiên bản (lịch sử thay đổi).
type Meta struct {
	ID        uuid.UUID
	EventTime time.Time
	ActorRef  uuid.UUID
}

// ItemVersion là một phiên bản mặt hàng kèm thông tin ghi.
type ItemVersion struct {
	domain.Ver[domain.Item]
	Meta
	ProposalID *uuid.UUID
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func dateOnly(t time.Time) string { return t.Format(time.DateOnly) }

// LoadItems đọc mọi phiên bản mặt hàng của tenant (RLS), theo mã rồi seq.
func LoadItems(ctx context.Context, tx pgx.Tx) ([]ItemVersion, error) {
	rows, err := tx.Query(ctx, `SELECT id, code, seq, name, variety, group_code, unit_code, coalesce(spec, ''), coalesce(sale_place, ''),
	    kind, sort_order, status, valid_from, coalesce(replaces_code, ''), coalesce(note, ''), proposal_id, event_time, actor_ref
	  FROM pf_price.item ORDER BY code, seq`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ItemVersion, error) {
		var v ItemVersion
		it := &v.V
		err := r.Scan(&v.ID, &it.Code, &v.Seq, &it.Name, &it.Variety, &it.GroupCode, &it.UnitCode, &it.Spec, &it.SalePlace,
			&it.Kind, &it.SortOrder, &it.Status, &it.ValidFrom, &it.ReplacesCode, &it.Note, &v.ProposalID, &v.EventTime, &v.ActorRef)
		v.Key, v.Status, v.ValidFrom = it.Code, it.Status, it.ValidFrom
		return v, err
	})
}

// NextItemNumber khóa bộ sinh mã mặt hàng của tenant đến hết giao dịch và trả số kế tiếp (lớn nhất đã dùng + 1 — mã
// không tái sử dụng, BR-01; sổ chỉ ghi thêm nên số không giảm).
func NextItemNumber(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (int, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_price.item:' || $1::text, 0))`, tid); err != nil {
		return 0, err
	}
	var n int
	err := tx.QueryRow(ctx, `SELECT coalesce(max(substr(code, 3)::int), 0) + 1 FROM pf_price.item`).Scan(&n)
	return n, err
}

// InsertItem ghi một phiên bản mặt hàng; seq, chuỗi băm do trigger fn_chain đặt.
func InsertItem(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, it domain.Item, proposal *uuid.UUID, at time.Time,
	actor uuid.UUID, idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.item (id, tenant_id, stream_key, code, name, variety, group_code, unit_code, spec,
	    sale_place, kind, sort_order, status, valid_from, replaces_code, proposal_id, note, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'item:' || $3::text, $3::text, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::date, $14, $15, $16, $17, $18, $19, $20)
	  RETURNING seq`,
		id, tid, it.Code, it.Name, it.Variety, it.GroupCode, it.UnitCode, nullText(it.Spec), nullText(it.SalePlace), it.Kind,
		it.SortOrder, it.Status, dateOnly(it.ValidFrom), nullText(it.ReplacesCode), proposal, nullText(it.Note), at.UTC(), actor, idem,
		domain.RuleVersion).Scan(&seq)
	return seq, err
}

// LoadAssignments đọc mọi phiên bản gán mặt hàng cho xã của tenant.
func LoadAssignments(ctx context.Context, tx pgx.Tx) ([]domain.Ver[domain.Assignment], error) {
	rows, err := tx.Query(ctx, `SELECT point_code, item_code, seq, status, valid_from FROM pf_price.item_assignment ORDER BY point_code, item_code, seq`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Ver[domain.Assignment], error) {
		var v domain.Ver[domain.Assignment]
		err := r.Scan(&v.V.PointCode, &v.V.ItemCode, &v.Seq, &v.Status, &v.ValidFrom)
		v.Key = domain.AssignKey(v.V.PointCode, v.V.ItemCode)
		return v, err
	})
}

// InsertAssignment ghi một phiên bản gán (status active | retired).
func InsertAssignment(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, point, item, status string, validFrom, at time.Time,
	actor uuid.UUID, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.item_assignment (id, tenant_id, stream_key, point_code, item_code, status, valid_from,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'assign:' || $3::text || ':' || $4::text, $3::text, $4::text, $5, $6::date, $7, $8, $9, $10)`,
		id, tid, point, item, status, dateOnly(validFrom), at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

// ProposalVersion là một phiên bản đề xuất kèm thông tin ghi.
type ProposalVersion struct {
	ProposalID  uuid.UUID
	Seq         int64
	ProposerRef uuid.UUID
	domain.Proposal
	Meta
}

// LoadProposals đọc mọi phiên bản đề xuất của tenant, theo proposal_id rồi seq.
func LoadProposals(ctx context.Context, tx pgx.Tx) ([]ProposalVersion, error) {
	rows, err := tx.Query(ctx, `SELECT id, proposal_id, seq, point_code, name, variety, group_code, unit_code, coalesce(spec, ''), reason,
	    status, coalesce(item_code, ''), coalesce(decision_note, ''), proposer_ref, event_time, actor_ref
	  FROM pf_price.item_proposal ORDER BY proposal_id, seq`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ProposalVersion, error) {
		var v ProposalVersion
		p := &v.Proposal
		err := r.Scan(&v.ID, &v.ProposalID, &v.Seq, &p.PointCode, &p.Name, &p.Variety, &p.GroupCode, &p.UnitCode, &p.Spec, &p.Reason,
			&p.Status, &p.ItemCode, &p.DecisionNote, &v.ProposerRef, &v.EventTime, &v.ActorRef)
		return v, err
	})
}

// InsertProposal ghi một phiên bản đề xuất.
func InsertProposal(ctx context.Context, tx pgx.Tx, id, tid, proposalID uuid.UUID, p domain.Proposal, proposer uuid.UUID, at time.Time,
	actor uuid.UUID, idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.item_proposal (id, tenant_id, stream_key, proposal_id, point_code, name, variety,
	    group_code, unit_code, spec, reason, status, item_code, decision_note, proposer_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'proposal:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18) RETURNING seq`,
		id, tid, proposalID, p.PointCode, p.Name, p.Variety, p.GroupCode, p.UnitCode, nullText(p.Spec), p.Reason, p.Status,
		nullText(p.ItemCode), nullText(p.DecisionNote), proposer, at.UTC(), actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// LoadSets đọc mọi phiên bản bộ mặt hàng theo vụ của tenant.
func LoadSets(ctx context.Context, tx pgx.Tx) ([]domain.Ver[domain.ItemSet], error) {
	rows, err := tx.Query(ctx, `SELECT code, seq, name, season, item_codes, enabled, status, valid_from FROM pf_price.item_set ORDER BY code, seq`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Ver[domain.ItemSet], error) {
		var v domain.Ver[domain.ItemSet]
		err := r.Scan(&v.V.Code, &v.Seq, &v.V.Name, &v.V.Season, &v.V.ItemCodes, &v.V.Enabled, &v.Status, &v.ValidFrom)
		v.Key = v.V.Code
		return v, err
	})
}

// InsertSet ghi một phiên bản bộ mặt hàng theo vụ.
func InsertSet(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, s domain.ItemSet, status string, validFrom, at time.Time,
	actor uuid.UUID, idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.item_set (id, tenant_id, stream_key, code, name, season, item_codes, enabled, status,
	    valid_from, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'set:' || $3::text, $3::text, $4, $5, $6, $7, $8, $9::date, $10, $11, $12, $13) RETURNING seq`,
		id, tid, s.Code, s.Name, s.Season, s.ItemCodes, s.Enabled, status, dateOnly(validFrom), at.UTC(), actor, idem,
		domain.RuleVersion).Scan(&seq)
	return seq, err
}

// LoadGroups đọc mọi phiên bản nhóm hàng (pf 000138) của tenant.
func LoadGroups(ctx context.Context, tx pgx.Tx) ([]domain.Ver[domain.Entry], error) {
	rows, err := tx.Query(ctx, `SELECT code, seq, name, sort_order, status, valid_from FROM pf_price.commodity_group ORDER BY code, seq`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Ver[domain.Entry], error) {
		var v domain.Ver[domain.Entry]
		v.V.Kind = domain.KindGroup
		err := r.Scan(&v.V.Code, &v.Seq, &v.V.Name, &v.V.SortOrder, &v.Status, &v.ValidFrom)
		v.Key, v.V.ValidFrom = v.V.Code, v.ValidFrom
		return v, err
	})
}

// InsertGroupVersion ghi một phiên bản nhóm hàng với trạng thái cho trước (active: thêm/sửa tên; retired: xóa nhóm).
func InsertGroupVersion(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, e domain.Entry, status string, at time.Time, actor uuid.UUID,
	idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.commodity_group (id, tenant_id, stream_key, code, name, sort_order, status, valid_from,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'group:' || $3::text, $3::text, $4, $5, $6, $7::date, $8, $9, $10, $11) RETURNING seq`,
		id, tid, e.Code, e.Name, e.SortOrder, status, dateOnly(e.ValidFrom), at.UTC(), actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

// ItemExists: mã mặt hàng có trong sổ (mọi phiên bản, mọi trạng thái) — ngưỡng theo mặt hàng của W11.74 kiểm bằng hàm này.
func ItemExists(ctx context.Context, tx pgx.Tx, code string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_price.item WHERE code = $1)`, code).Scan(&ok)
	return ok, err
}
