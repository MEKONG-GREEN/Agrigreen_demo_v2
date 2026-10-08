package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// BoundaryWithGeom đọc một phiên bản ranh giới kèm hình (chi tiết xung đột).
func BoundaryWithGeom(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Boundary, error) {
	b, err := scanBoundary(tx.QueryRow(ctx, "SELECT "+boundaryCols+`, ST_AsGeoJSON(geom, 7)
	  FROM pf_coop.plot_boundary WHERE id = $1::uuid`, id), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}
