package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LibraryItem là một tài liệu trong danh mục cá nhân của người đọc (US-LIB-05).
type LibraryItem struct {
	ActorRef    uuid.UUID
	DocumentRef uuid.UUID
	Saved       bool
	Note        *string
	EventTime   time.Time
	SyncSeq     int64
}

const libCols = `actor_ref, document_ref, saved, note, event_time, sync_seq`

func scanLib(row pgx.Row) (LibraryItem, error) {
	var l LibraryItem
	err := row.Scan(&l.ActorRef, &l.DocumentRef, &l.Saved, &l.Note, &l.EventTime, &l.SyncSeq)
	return l, err
}

// UpsertLibrary ghi trạng thái lưu/bỏ lưu và lấy sync_seq mới.
func UpsertLibrary(ctx context.Context, tx pgx.Tx, tid uuid.UUID, l *LibraryItem) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_content.library_item (tenant_id, actor_ref, document_ref, saved, note, event_time)
	  VALUES ($1, $2, $3, $4, $5, $6)
	  ON CONFLICT (tenant_id, actor_ref, document_ref) DO UPDATE SET saved = EXCLUDED.saved, note = EXCLUDED.note,
	    event_time = EXCLUDED.event_time, updated_at = now(), sync_seq = nextval('pf_content.library_sync_seq')
	  RETURNING sync_seq`, tid, l.ActorRef, l.DocumentRef, l.Saved, l.Note, l.EventTime.UTC()).Scan(&l.SyncSeq)
}

// LibraryByKey đọc một mục.
func LibraryByKey(ctx context.Context, tx pgx.Tx, actor, doc uuid.UUID) (*LibraryItem, error) {
	l, err := scanLib(tx.QueryRow(ctx, "SELECT "+libCols+" FROM pf_content.library_item WHERE actor_ref = $1 AND document_ref = $2", actor, doc))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// LibraryPage liệt kê mục đang lưu, mới nhất trước; beforeSeq > 0 là cursor.
func LibraryPage(ctx context.Context, tx pgx.Tx, actor uuid.UUID, beforeSeq int64, limit int) ([]LibraryItem, error) {
	rows, err := tx.Query(ctx, "SELECT "+libCols+` FROM pf_content.library_item
	  WHERE actor_ref = $1 AND saved AND ($2 = 0 OR sync_seq < $2) ORDER BY sync_seq DESC LIMIT $3`, actor, beforeSeq, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LibraryItem, error) { return scanLib(r) })
}

// LibraryChanges đọc thay đổi (lưu và bỏ lưu) của người đọc có sync_seq > after.
func LibraryChanges(ctx context.Context, tx pgx.Tx, actor uuid.UUID, after int64, limit int) ([]LibraryItem, error) {
	rows, err := tx.Query(ctx, "SELECT "+libCols+` FROM pf_content.library_item
	  WHERE actor_ref = $1 AND sync_seq > $2 ORDER BY sync_seq LIMIT $3`, actor, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LibraryItem, error) { return scanLib(r) })
}
