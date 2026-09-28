package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// SyncItem là một mục danh mục kèm sync_seq (con trỏ chiều xuống của pf/sync).
type SyncItem struct {
	Item domain.Item
	Seq  int64
}

// SyncItems liệt kê mục của các danh mục catalogs (kể cả đã ngừng dùng) có
// sync_seq > after, theo sync_seq tăng dần.
func SyncItems(ctx context.Context, tx pgx.Tx, catalogs []string, after int64, limit int) ([]SyncItem, error) {
	rows, err := tx.Query(ctx, "SELECT "+itemCols+`, sync_seq FROM pf_masterdata.catalog_item
	    WHERE catalog = ANY($1::text[]) AND sync_seq > $2 ORDER BY sync_seq LIMIT $3`, catalogs, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncItem
	for rows.Next() {
		var r SyncItem
		var attrs string
		it := &r.Item
		if err := rows.Scan(&it.Catalog, &it.Code, &it.Name, &attrs, &it.Active, &it.DeactivationReason, &it.Revision,
			&it.UpdatedBy, &it.UpdatedAt, &r.Seq); err != nil {
			return nil, err
		}
		it.Attrs = map[string]string{}
		if err := json.Unmarshal([]byte(attrs), &it.Attrs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SyncUnit là một phiên bản đơn vị hành chính kèm sync_seq.
type SyncUnit struct {
	Unit domain.Unit
	Seq  int64
}

// SyncUnits liệt kê phiên bản đơn vị hành chính có sync_seq > after, theo
// sync_seq tăng dần. all = false: chỉ đơn vị có mã thuộc units, hoặc đã đóng
// hiệu lực với đơn vị kế thừa thuộc units (để máy nhận tombstone của đơn vị cũ
// đã sáp nhập vào địa bàn).
func SyncUnits(ctx context.Context, tx pgx.Tx, all bool, units []string, after int64, limit int) ([]SyncUnit, error) {
	if units == nil {
		units = []string{}
	}
	rows, err := tx.Query(ctx, "SELECT "+unitCols+`, sync_seq FROM pf_masterdata.admin_unit
	    WHERE sync_seq > $1 AND ($2::boolean OR code = ANY($3::text[]) OR successor_codes && $3::text[])
	    ORDER BY sync_seq LIMIT $4`, after, all, units, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncUnit
	for rows.Next() {
		var r SyncUnit
		var level string
		u := &r.Unit
		if err := rows.Scan(&u.Code, &u.ValidFrom, &u.ValidTo, &u.Name, &level, &u.ParentCode, &u.Successors, &r.Seq); err != nil {
			return nil, err
		}
		u.Level = port.Level(level)
		out = append(out, r)
	}
	return out, rows.Err()
}
