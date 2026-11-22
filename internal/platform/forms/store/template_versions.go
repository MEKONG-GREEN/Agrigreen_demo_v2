package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// LockTemplateDefinition serializes the tenant's idempotency key first, then the
// profile/code stream. The first lock also prevents simultaneous key reuse across
// different streams; keys belong to the ledger, not merely to one template code.
// Call within the same transaction as replay lookup, base-version check and insert.
func LockTemplateDefinition(ctx context.Context, tx pgx.Tx, tid uuid.UUID, profile, code, idem string) error {
	for _, key := range []string{"pf_forms.template.idem:" + tid.String() + ":" + idem,
		"pf_forms.template.stream:" + tid.String() + ":" + profile + ":" + code} {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
			return err
		}
	}
	return nil
}

// TemplateByIdem reads a durable replay in the current tenant. Deliberately do not
// filter profile/code: the caller must reject reuse for a different definition.
func TemplateByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Template, error) {
	return optionalTemplate(tx.QueryRow(ctx, `SELECT `+tplCols+` FROM pf_forms.template WHERE idem_key = $1 ORDER BY recorded_at DESC LIMIT 1`, idem))
}

// LatestTemplateByCode reads the current version, or nil before first creation.
func LatestTemplateByCode(ctx context.Context, tx pgx.Tx, profile, code string) (*Template, error) {
	return optionalTemplate(tx.QueryRow(ctx, `SELECT `+tplCols+` FROM pf_forms.template WHERE profile = $1 AND code = $2 ORDER BY seq DESC LIMIT 1`, profile, code))
}

// TemplateByCodeVersion selects the immutable version requested by the consumer.
func TemplateByCodeVersion(ctx context.Context, tx pgx.Tx, profile, code string, version int64) (*Template, error) {
	return optionalTemplate(tx.QueryRow(ctx, `SELECT `+tplCols+` FROM pf_forms.template WHERE profile = $1 AND code = $2 AND seq = $3`, profile, code, version))
}

// TemplateVersions returns newest first, using the immutable sequence as cursor.
// The application validates the public limit; this bound also protects SQL callers.
func TemplateVersions(ctx context.Context, tx pgx.Tx, profile, code string, before int64, limit int) ([]Template, error) {
	if limit < 1 || limit > 201 {
		return nil, errors.New("forms: invalid template history limit")
	}
	rows, err := tx.Query(ctx, `SELECT `+tplCols+` FROM pf_forms.template WHERE profile = $1 AND code = $2 AND ($3::bigint = 0 OR seq < $3) ORDER BY seq DESC LIMIT $4`, profile, code, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Template, error) { return scanTemplate(r) })
}

func optionalTemplate(row pgx.Row) (*Template, error) {
	t, err := scanTemplate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
