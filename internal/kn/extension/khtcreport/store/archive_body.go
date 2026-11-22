package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ArchiveStoredBody struct {
	Body      []byte
	BodyHash  string
	InputHash string
	FileName  string
	Version   int64
}

// ArchiveBody reads only stored bytes and integrity metadata, not report payloads.
func ArchiveBody(ctx context.Context, tx pgx.Tx, period, export uuid.UUID) (*ArchiveStoredBody, error) {
	var b ArchiveStoredBody
	err := tx.QueryRow(ctx, `SELECT body,body_hash,input_hash,file_name,seq FROM pf_extension.khtc_report_export WHERE period_id=$1 AND id=$2`, period, export).Scan(&b.Body, &b.BodyHash, &b.InputHash, &b.FileName, &b.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &b, err
}
