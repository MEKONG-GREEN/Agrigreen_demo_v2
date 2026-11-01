package store

// Ảnh thực địa Mẫu 03 (W11.53b — pf_extension.cnts_knqg_photo, sổ chỉ ghi thêm qua shared.apply_ledger).

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/domain"
)

// Sự việc trên ảnh.
const (
	PhotoAdded   = "added"
	PhotoRemoved = "removed"
)

// Photo là một dòng sổ ảnh (added: tệp mới; removed: gỡ ảnh PhotoRef — các cột tệp chép lại từ dòng added).
type Photo struct {
	Meta
	DocRef   uuid.UUID
	PhotoRef uuid.UUID
	Event    string
	Key      string
	Name     string
	Type     string
	Size     int64
	SHA256   string
	Caption  string
}

const photoCols = `id, seq, doc_ref, photo_ref, event, object_key, file_name, content_type, size_bytes, sha256, coalesce(caption, ''),
  event_time, recorded_at, actor_ref, on_behalf_of, support_reason, idem_key`

func scanPhoto(row pgx.Row) (Photo, error) {
	var p Photo
	err := row.Scan(&p.ID, &p.Seq, &p.DocRef, &p.PhotoRef, &p.Event, &p.Key, &p.Name, &p.Type, &p.Size, &p.SHA256, &p.Caption,
		&p.EventTime, &p.RecordedAt, &p.ActorRef, &p.OnBehalfOf, &p.SupportReason, &p.IdemKey)
	return p, err
}

// InsertPhoto ghi một dòng sổ ảnh; Seq do DB tính.
func InsertPhoto(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p *Photo) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_knqg_photo (id, tenant_id, stream_key, doc_ref, photo_ref, event, object_key,
	    file_name, content_type, size_bytes, sha256, caption, event_time, actor_ref, on_behalf_of, support_reason, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	  RETURNING seq, recorded_at`,
		p.ID, tid, "knqgphoto:"+p.DocRef.String(), p.DocRef, p.PhotoRef, p.Event, p.Key, p.Name, p.Type, p.Size, p.SHA256, optText(p.Caption),
		p.EventTime.UTC(), p.ActorRef, p.OnBehalfOf, p.SupportReason, p.IdemKey, domain.RuleVersion).Scan(&p.Seq, &p.RecordedAt)
}

// PhotoByIdem trả dòng sổ ảnh đã ghi với khóa idem (nil = chưa có).
func PhotoByIdem(ctx context.Context, tx pgx.Tx, key string) (*Photo, error) {
	p, err := scanPhoto(tx.QueryRow(ctx, "SELECT "+photoCols+" FROM pf_extension.cnts_knqg_photo WHERE idem_key = $1 LIMIT 1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Photos trả ảnh HIỆN HÀNH của biểu (added chưa gỡ), cũ trước.
func Photos(ctx context.Context, tx pgx.Tx, doc uuid.UUID) ([]Photo, error) {
	rows, err := tx.Query(ctx, "SELECT "+photoCols+` FROM pf_extension.cnts_knqg_photo a
	  WHERE a.doc_ref = $1 AND a.event = 'added'
	    AND NOT EXISTS (SELECT 1 FROM pf_extension.cnts_knqg_photo r WHERE r.doc_ref = a.doc_ref AND r.photo_ref = a.photo_ref AND r.event = 'removed')
	  ORDER BY a.seq`, doc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Photo
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PhotoAt trả dòng added của ảnh photo thuộc biểu doc (kể cả đã gỡ — gone=true), nil = không có.
func PhotoAt(ctx context.Context, tx pgx.Tx, doc, photo uuid.UUID) (p *Photo, gone bool, err error) {
	x, err := scanPhoto(tx.QueryRow(ctx, "SELECT "+photoCols+` FROM pf_extension.cnts_knqg_photo
	  WHERE doc_ref = $1 AND photo_ref = $2 AND event = 'added' LIMIT 1`, doc, photo))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_extension.cnts_knqg_photo WHERE doc_ref = $1 AND photo_ref = $2 AND event = 'removed')`,
		doc, photo).Scan(&gone)
	return &x, gone, err
}
