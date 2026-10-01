package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
)

const itemCols = `catalog, code, name, attrs::text, active, coalesce(deactivation_reason, ''), revision, updated_by, updated_at`

func scanItem(row pgx.Row) (domain.Item, error) {
	var it domain.Item
	var attrs string
	err := row.Scan(&it.Catalog, &it.Code, &it.Name, &attrs, &it.Active, &it.DeactivationReason, &it.Revision, &it.UpdatedBy, &it.UpdatedAt)
	if err != nil {
		return it, err
	}
	it.Attrs = map[string]string{}
	return it, json.Unmarshal([]byte(attrs), &it.Attrs)
}

// Item đọc một mục danh mục; forUpdate khóa dòng để cập nhật trong cùng giao dịch.
func Item(ctx context.Context, tx pgx.Tx, catalog, code string, forUpdate bool) (*domain.Item, error) {
	code = domain.CanonicalCode(catalog, code)
	q := "SELECT " + itemCols + " FROM pf_masterdata.catalog_item WHERE catalog = $1 AND code = $2"
	if forUpdate {
		q += " FOR UPDATE"
	}
	it, err := scanItem(tx.QueryRow(ctx, q, catalog, code))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &it, nil
}

// Items liệt kê mục của một danh mục theo mã, sau mã after (cursor).
func Items(ctx context.Context, tx pgx.Tx, catalog string, includeInactive bool, after string, limit int) ([]domain.Item, error) {
	rows, err := tx.Query(ctx, "SELECT "+itemCols+` FROM pf_masterdata.catalog_item
	    WHERE catalog = $1 AND ($2::boolean OR active) AND code > $3 ORDER BY code LIMIT $4`, catalog, includeInactive, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// UpsertItem ghi bảng hiện trạng sau khi đã ghi sổ catalog_change (R-21).
func UpsertItem(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, it domain.Item, changeID uuid.UUID) error {
	attrs, err := json.Marshal(it.Attrs)
	if err != nil {
		return err
	}
	var reason *string
	if !it.Active {
		reason = &it.DeactivationReason
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_masterdata.catalog_item
	    (tenant_id, catalog, code, name, attrs, active, deactivation_reason, revision, last_change_id, updated_by, updated_at)
	  VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11)
	  ON CONFLICT (tenant_id, catalog, code) DO UPDATE SET
	    name = EXCLUDED.name, attrs = EXCLUDED.attrs, active = EXCLUDED.active,
	    deactivation_reason = EXCLUDED.deactivation_reason, revision = EXCLUDED.revision,
	    last_change_id = EXCLUDED.last_change_id, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		tenantID, it.Catalog, it.Code, it.Name, string(attrs), it.Active, reason, it.Revision, changeID, it.UpdatedBy, it.UpdatedAt.UTC())
	return err
}

// InsertChange ghi sổ lịch sử danh mục; DB tính seq, chuỗi băm, recorded_at.
func InsertChange(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, c *domain.Change) error {
	var before *string
	if c.Before != nil {
		b, err := json.Marshal(c.Before)
		if err != nil {
			return err
		}
		s := string(b)
		before = &s
	}
	after, err := json.Marshal(c.After)
	if err != nil {
		return err
	}
	var reason *string
	if c.Reason != "" {
		reason = &c.Reason
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_masterdata.catalog_change
	    (id, tenant_id, stream_key, catalog, code, action, before, after, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10, $11, $12, $13)
	  RETURNING seq, recorded_at`,
		c.ID, tenantID, domain.CatalogStreamKey(c.Catalog, c.Code), c.Catalog, c.Code, c.Action, before, string(after), reason,
		c.EventTime.UTC(), c.ActorRef, c.IdemKey, domain.CatalogRuleVersion).Scan(&c.Seq, &c.RecordedAt)
}

// ChangeByIdemKey tìm dòng sổ đã ghi với idem_key (gọi lặp).
func ChangeByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Change, error) {
	rows, err := tx.Query(ctx, changeSelect+" WHERE idem_key = $1", key)
	cs, err := scanChanges(rows, err)
	if err != nil || len(cs) == 0 {
		return nil, err
	}
	return &cs[0], nil
}

const changeSelect = `SELECT id, catalog, code, action, before::text, after::text, coalesce(reason, ''), event_time,
  actor_ref, idem_key, seq, recorded_at FROM pf_masterdata.catalog_change`

func scanChanges(rows pgx.Rows, err error) ([]domain.Change, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Change
	for rows.Next() {
		var c domain.Change
		var before *string
		var after string
		if err := rows.Scan(&c.ID, &c.Catalog, &c.Code, &c.Action, &before, &after, &c.Reason, &c.EventTime,
			&c.ActorRef, &c.IdemKey, &c.Seq, &c.RecordedAt); err != nil {
			return nil, err
		}
		if before != nil {
			c.Before = &domain.Snapshot{}
			if err := json.Unmarshal([]byte(*before), c.Before); err != nil {
				return nil, err
			}
		}
		if err := json.Unmarshal([]byte(after), &c.After); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Changes liệt kê lịch sử của một mục theo seq, sau afterSeq (cursor).
func Changes(ctx context.Context, tx pgx.Tx, catalog, code string, afterSeq int64, limit int) ([]domain.Change, error) {
	return scanChanges(tx.Query(ctx, changeSelect+` WHERE stream_key = $1 AND seq > $2 ORDER BY seq LIMIT $3`,
		domain.CatalogStreamKey(catalog, domain.CanonicalCode(catalog, code)), afterSeq, limit))
}

// ItemNameTaken cho biết danh mục đã có mục khác (mã ≠ exceptCode) cùng tên, không phân biệt hoa thường
// (KN v7.7 FN-AD-04.05: trùng tên → chặn). So khớp chữ hoa/thường tiếng Việt ở Go (lower() của DB phụ
// thuộc LC_CTYPE, cụm "C" không hạ chữ có dấu); danh mục chỉ vài trăm mục.
func ItemNameTaken(ctx context.Context, tx pgx.Tx, catalog, name, exceptCode string) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT name FROM pf_masterdata.catalog_item WHERE catalog = $1 AND code <> $2`, catalog, exceptCode)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	want := strings.ToLower(strings.TrimSpace(name))
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return false, err
		}
		if strings.ToLower(strings.TrimSpace(n)) == want {
			return true, nil
		}
	}
	return false, rows.Err()
}
