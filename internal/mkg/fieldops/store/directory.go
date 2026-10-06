package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ReplicaHTX là một dòng bản sao directory_htx_replica (mkg 000033).
type ReplicaHTX struct {
	Ref         uuid.UUID
	Present     bool   // false: nền tảng trả không có / chưa chia sẻ tại AsOf
	CommuneCode string // rỗng = nền tảng không có mã xã
	Active      bool
	AsOf        time.Time // thời điểm MKG nhận ảnh chụp từ nền tảng
}

// ReplicaPlot là một dòng bản sao directory_plot_replica (mkg 000033).
type ReplicaPlot struct {
	Ref            uuid.UUID
	Present        bool
	CooperativeRef *uuid.UUID
	CommuneCode    string
	Status         string
	AsOf           time.Time
}

func nullText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// PutReplicaHTX ghi bản sao một HTX. Ảnh chụp cũ hơn dòng đang có không đè (job và tra cứu
// dự phòng có thể ghi song song).
func PutReplicaHTX(ctx context.Context, tx pgx.Tx, tid uuid.UUID, h ReplicaHTX) error {
	commune := nullText(h.CommuneCode)
	if !h.Present {
		commune, h.Active = nil, false
	}
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.directory_htx_replica AS c
	    (tenant_id, htx_ref, present, commune_code, active, as_of)
	  VALUES ($1, $2, $3, $4, $5, $6)
	  ON CONFLICT (tenant_id, htx_ref) DO UPDATE SET
	    present = EXCLUDED.present, commune_code = EXCLUDED.commune_code, active = EXCLUDED.active,
	    as_of = EXCLUDED.as_of, updated_at = now()
	  WHERE c.as_of <= EXCLUDED.as_of`,
		tid, h.Ref, h.Present, commune, h.Active, h.AsOf.UTC())
	return err
}

// PutReplicaPlot ghi bản sao một thửa (như PutReplicaHTX).
func PutReplicaPlot(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p ReplicaPlot) error {
	coop, commune, status := p.CooperativeRef, nullText(p.CommuneCode), nullText(p.Status)
	if !p.Present {
		coop, commune, status = nil, nil, nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.directory_plot_replica AS c
	    (tenant_id, plot_ref, present, cooperative_ref, commune_code, status, as_of)
	  VALUES ($1, $2, $3, $4, $5, $6, $7)
	  ON CONFLICT (tenant_id, plot_ref) DO UPDATE SET
	    present = EXCLUDED.present, cooperative_ref = EXCLUDED.cooperative_ref, commune_code = EXCLUDED.commune_code,
	    status = EXCLUDED.status, as_of = EXCLUDED.as_of, updated_at = now()
	  WHERE c.as_of <= EXCLUDED.as_of`,
		tid, p.Ref, p.Present, coop, commune, status, p.AsOf.UTC())
	return err
}

// GetReplicaHTX đọc bản sao của một HTX; nil nếu chưa có.
func GetReplicaHTX(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*ReplicaHTX, error) {
	var h ReplicaHTX
	var commune *string
	err := tx.QueryRow(ctx, `SELECT htx_ref, present, commune_code, active, as_of
	    FROM mkg_fieldops.directory_htx_replica WHERE htx_ref = $1`, ref).Scan(&h.Ref, &h.Present, &commune, &h.Active, &h.AsOf)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if commune != nil {
		h.CommuneCode = *commune
	}
	return &h, nil
}

// ReplicaPlots đọc bản sao của các thửa refs (thửa chưa có bản sao vắng mặt trong map).
func ReplicaPlots(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) (map[uuid.UUID]ReplicaPlot, error) {
	out := map[uuid.UUID]ReplicaPlot{}
	if len(refs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT plot_ref, present, cooperative_ref, commune_code, status, as_of
	    FROM mkg_fieldops.directory_plot_replica WHERE plot_ref = ANY($1::uuid[])`, uuidStrings(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ReplicaPlot
		var commune, status *string
		if err := rows.Scan(&p.Ref, &p.Present, &p.CooperativeRef, &commune, &status, &p.AsOf); err != nil {
			return nil, err
		}
		if commune != nil {
			p.CommuneCode = *commune
		}
		if status != nil {
			p.Status = *status
		}
		out[p.Ref] = p
	}
	return out, rows.Err()
}

// ReplicaRefs trả các HTX / thửa cần làm mới: mọi mục đã có trong bản sao ∪ mục được lệnh gom
// chưa đóng/hủy tham chiếu (planned/approved/assigned) — job fieldops_directory.
func ReplicaRefs(ctx context.Context, tx pgx.Tx) (htx, plots []uuid.UUID, err error) {
	if htx, err = collectRefs(ctx, tx, `SELECT htx_ref FROM mkg_fieldops.directory_htx_replica
	  UNION SELECT htx_ref FROM mkg_fieldops.collection_order_current WHERE status IN ('planned', 'approved', 'assigned')
	  ORDER BY 1`); err != nil {
		return nil, nil, err
	}
	plots, err = collectRefs(ctx, tx, `SELECT plot_ref FROM mkg_fieldops.directory_plot_replica
	  UNION SELECT unnest(plot_refs) FROM mkg_fieldops.collection_order_current WHERE status IN ('planned', 'approved', 'assigned')
	  ORDER BY 1`)
	return htx, plots, err
}

func collectRefs(ctx context.Context, tx pgx.Tx, q string) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}
