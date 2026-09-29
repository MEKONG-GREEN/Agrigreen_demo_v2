package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CatalogImport là một lần nhập danh mục hàng loạt (HTX US-CAT-02). Rows là kết
// quả từng dòng dạng JSON (mảng) — danh mục không phải dữ liệu cá nhân.
type CatalogImport struct {
	ID                                uuid.UUID
	ActorRef                          uuid.UUID
	Format                            string
	FileSHA256                        []byte
	Total, Created, Skipped, Rejected int
	Rows                              json.RawMessage
	IdemKey                           string
	EventTime                         time.Time
}

// InsertCatalogImport ghi kết quả một lần nhập (chỉ ghi thêm).
func InsertCatalogImport(ctx context.Context, tx pgx.Tx, tid uuid.UUID, ci CatalogImport) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_masterdata.catalog_import
	    (tenant_id, id, actor_ref, format, file_sha256, total_rows, created_rows, skipped_rows, rejected_rows, rows, idem_key, event_time)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12)`,
		tid, ci.ID, ci.ActorRef, ci.Format, ci.FileSHA256, ci.Total, ci.Created, ci.Skipped, ci.Rejected, string(ci.Rows), ci.IdemKey, ci.EventTime.UTC())
	return err
}

const catalogImportCols = `id, actor_ref, format, file_sha256, total_rows, created_rows, skipped_rows, rejected_rows, rows::text, idem_key, event_time`

func scanCatalogImport(row pgx.Row) (*CatalogImport, error) {
	var ci CatalogImport
	var rows string
	err := row.Scan(&ci.ID, &ci.ActorRef, &ci.Format, &ci.FileSHA256, &ci.Total, &ci.Created, &ci.Skipped, &ci.Rejected, &rows, &ci.IdemKey, &ci.EventTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ci.Rows = json.RawMessage(rows)
	return &ci, nil
}

// CatalogImportByKey đọc lần nhập theo Idempotency-Key (gọi lặp).
func CatalogImportByKey(ctx context.Context, tx pgx.Tx, key string) (*CatalogImport, error) {
	return scanCatalogImport(tx.QueryRow(ctx, "SELECT "+catalogImportCols+" FROM pf_masterdata.catalog_import WHERE idem_key = $1", key))
}

// CatalogImportByID đọc một lần nhập.
func CatalogImportByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*CatalogImport, error) {
	return scanCatalogImport(tx.QueryRow(ctx, "SELECT "+catalogImportCols+" FROM pf_masterdata.catalog_import WHERE id = $1", id))
}
