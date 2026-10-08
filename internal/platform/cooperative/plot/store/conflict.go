package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// Conflict là một xung đột ranh giới chờ xử lý (mục 15, C-8).
type Conflict struct {
	ID             uuid.UUID
	Kind           string
	PlotRef        uuid.UUID
	BoundaryID     uuid.UUID
	OtherPlotRefs  []uuid.UUID
	MaxOverlapPct  *decimal.Decimal
	CommuneCode    string
	OwnerRef       uuid.UUID
	DueAt          time.Time
	Status         string
	DetectedAt     time.Time
	ResolvedBy     uuid.UUID
	ResolvedAt     *time.Time
	ResolutionNote string
}

// InsertConflict ghi xung đột mới (trạng thái open).
func InsertConflict(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, c Conflict) error {
	others := make([]string, 0, len(c.OtherPlotRefs)) // text[] → uuid[]: mã hóa ổn định ở chế độ PgBouncer
	for _, o := range c.OtherPlotRefs {
		others = append(others, o.String())
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.boundary_conflict
	    (tenant_id, id, kind, plot_ref, boundary_id, other_plot_refs, max_overlap_pct, commune_code, owner_ref, due_at, detected_at)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::text[]::uuid[],$7::numeric, $8::text, $9::uuid, $10::timestamptz, $11::timestamptz)`,
		tenantID, c.ID, c.Kind, c.PlotRef, c.BoundaryID, others, decPtr(c.MaxOverlapPct), c.CommuneCode, nilIfZero(c.OwnerRef),
		c.DueAt.UTC(), c.DetectedAt.UTC())
	return err
}

const conflictCols = `id, kind, plot_ref, boundary_id, other_plot_refs, max_overlap_pct::text, commune_code, owner_ref, due_at,
  status, detected_at, resolved_by, resolved_at, coalesce(resolution_note, '')`

func scanConflict(row pgx.Row) (Conflict, error) {
	var c Conflict
	var pct *string
	var owner, by *uuid.UUID
	if err := row.Scan(&c.ID, &c.Kind, &c.PlotRef, &c.BoundaryID, &c.OtherPlotRefs, &pct, &c.CommuneCode, &owner, &c.DueAt,
		&c.Status, &c.DetectedAt, &by, &c.ResolvedAt, &c.ResolutionNote); err != nil {
		return c, err
	}
	c.OwnerRef, c.ResolvedBy = deref(owner), deref(by)
	var err error
	c.MaxOverlapPct, err = parseDec(pct)
	return c, err
}

// ConflictByID đọc xung đột; forUpdate khóa dòng.
func ConflictByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Conflict, error) {
	q := "SELECT " + conflictCols + " FROM pf_coop.boundary_conflict WHERE id = $1::uuid"
	if forUpdate {
		q += " FOR UPDATE"
	}
	c, err := scanConflict(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ConflictFilter là điều kiện liệt kê xung đột theo phạm vi.
type ConflictFilter struct {
	All      bool
	Units    []string
	CoopOnly bool
	Coop     uuid.UUID
	Status   string // rỗng = mọi trạng thái
	PlotRef  uuid.UUID
	After    uuid.UUID
	Limit    int
}

// ListConflicts liệt kê xung đột, hạn gần nhất trước khi cùng trạng thái; cursor theo id.
func ListConflicts(ctx context.Context, tx pgx.Tx, f ConflictFilter) ([]Conflict, error) {
	rows, err := tx.Query(ctx, "SELECT "+conflictCols+` FROM pf_coop.boundary_conflict c
	  WHERE ($1::boolean OR c.commune_code = ANY($2::text[]))
	    AND (NOT $3::boolean OR c.owner_ref = $4::uuid OR c.owner_ref IS NULL)
	    AND ($5::text = '' OR c.status = $5::text)
	    AND ($6::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR c.plot_ref = $6::uuid)
	    AND c.id > $7::uuid
	  ORDER BY c.id LIMIT $8::int`, f.All, units(f.Units), f.CoopOnly, f.Coop, f.Status, f.PlotRef, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conflict
	for rows.Next() {
		c, err := scanConflict(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ResolveConflict đóng xung đột.
func ResolveConflict(ctx context.Context, tx pgx.Tx, id, by uuid.UUID, note string, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_coop.boundary_conflict SET status = 'resolved', resolved_by = $2::uuid,
	    resolved_at = $3::timestamptz, resolution_note = $4::text WHERE id = $1::uuid`, id, by, at.UTC(), note)
	return err
}

// OpenConflicts đếm xung đột còn mở của thửa.
func OpenConflicts(ctx context.Context, tx pgx.Tx, plotRef uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT count(*) FROM pf_coop.boundary_conflict WHERE plot_ref = $1::uuid AND status = 'open'", plotRef).Scan(&n)
	return n, err
}
