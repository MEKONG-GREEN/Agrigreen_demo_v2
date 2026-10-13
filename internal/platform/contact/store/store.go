// Package store là SQL của pf/contact (schema pf_contact, pf 20261013165159). Chạy trong giao dịch của module (vai
// r_contact, db.ForModule); RLS p_tenant lọc tenant. Sổ chỉ ghi thêm: không có UPDATE/DELETE. Không cột PII.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/contact/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// Version là phần chung của một phiên bản sổ.
type Version struct {
	ID            uuid.UUID
	Seq           int64
	Hidden        bool
	Change        string
	ChangedFields []string
	RecordedAt    time.Time
	Actor         uuid.UUID
}

// PersonRow là một phiên bản liên hệ ngoài tỉnh (không PII).
type PersonRow struct {
	SubjectRef uuid.UUID
	Meta       domain.PersonMeta
	Version
}

const personCols = `subject_ref, level, organization, title, id, seq, hidden, change, changed_fields, recorded_at, actor_ref`

func scanPerson(r pgx.CollectableRow) (PersonRow, error) {
	var x PersonRow
	err := r.Scan(&x.SubjectRef, &x.Meta.Level, &x.Meta.Organization, &x.Meta.Title, &x.ID, &x.Seq, &x.Hidden, &x.Change,
		&x.ChangedFields, &x.RecordedAt, &x.Actor)
	return x, err
}

// CurrentPersons trả phiên bản mới nhất của mọi liên hệ ngoài tỉnh.
func CurrentPersons(ctx context.Context, tx pgx.Tx) ([]PersonRow, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (subject_ref) `+personCols+` FROM pf_contact.person ORDER BY subject_ref, seq DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanPerson)
}

// PersonHistory trả mọi phiên bản của một liên hệ, cũ trước.
func PersonHistory(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]PersonRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+personCols+` FROM pf_contact.person WHERE subject_ref = $1 ORDER BY seq`, ref)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanPerson)
}

// PersonByIdem trả phiên bản đã ghi với idem_key (gọi lặp).
func PersonByIdem(ctx context.Context, tx pgx.Tx, idem string) (PersonRow, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+personCols+` FROM pf_contact.person WHERE idem_key = $1`+ledger.NewestByIdem(""), idem)
	if err != nil {
		return PersonRow{}, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanPerson)
	if errors.Is(err, pgx.ErrNoRows) {
		return PersonRow{}, false, nil
	}
	return x, err == nil, err
}

// InsertPerson ghi một phiên bản liên hệ ngoài tỉnh.
func InsertPerson(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x PersonRow, at time.Time, idem string) (int64, error) {
	if x.ChangedFields == nil {
		x.ChangedFields = []string{}
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_contact.person (id, tenant_id, stream_key, subject_ref, level, organization, title, hidden,
	    change, changed_fields, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'person:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING seq`,
		x.ID, tid, x.SubjectRef, x.Meta.Level, x.Meta.Organization, x.Meta.Title, x.Hidden, x.Change, x.ChangedFields, at.UTC(),
		x.Actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, ledger.Classify(err)
}

// UnitRow là một phiên bản thẻ Đơn vị.
type UnitRow struct {
	UnitID uuid.UUID
	OrgRef *uuid.UUID
	In     domain.UnitInput
	Version
}

const unitCols = `unit_id, org_ref, name, level, phone, email, address, id, seq, hidden, change, changed_fields, recorded_at, actor_ref`

func scanUnit(r pgx.CollectableRow) (UnitRow, error) {
	var x UnitRow
	err := r.Scan(&x.UnitID, &x.OrgRef, &x.In.Name, &x.In.Level, &x.In.Phone, &x.In.Email, &x.In.Address, &x.ID, &x.Seq, &x.Hidden,
		&x.Change, &x.ChangedFields, &x.RecordedAt, &x.Actor)
	return x, err
}

// CurrentUnits trả phiên bản mới nhất của mọi thẻ Đơn vị.
func CurrentUnits(ctx context.Context, tx pgx.Tx) ([]UnitRow, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (unit_id) `+unitCols+` FROM pf_contact.unit ORDER BY unit_id, seq DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanUnit)
}

// UnitHistory trả mọi phiên bản của một thẻ Đơn vị, cũ trước.
func UnitHistory(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]UnitRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+unitCols+` FROM pf_contact.unit WHERE unit_id = $1 ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanUnit)
}

// UnitByIdem trả phiên bản đã ghi với idem_key.
func UnitByIdem(ctx context.Context, tx pgx.Tx, idem string) (UnitRow, bool, error) {
	rows, err := tx.Query(ctx, `SELECT `+unitCols+` FROM pf_contact.unit WHERE idem_key = $1`+ledger.NewestByIdem(""), idem)
	if err != nil {
		return UnitRow{}, false, err
	}
	x, err := pgx.CollectExactlyOneRow(rows, scanUnit)
	if errors.Is(err, pgx.ErrNoRows) {
		return UnitRow{}, false, nil
	}
	return x, err == nil, err
}

// InsertUnit ghi một phiên bản thẻ Đơn vị.
func InsertUnit(ctx context.Context, tx pgx.Tx, tid uuid.UUID, x UnitRow, at time.Time, idem string) (int64, error) {
	if x.ChangedFields == nil {
		x.ChangedFields = []string{}
	}
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_contact.unit (id, tenant_id, stream_key, unit_id, org_ref, name, level, phone, email, address,
	    hidden, change, changed_fields, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'unit:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING seq`,
		x.ID, tid, x.UnitID, x.OrgRef, x.In.Name, x.In.Level, x.In.Phone, x.In.Email, x.In.Address, x.Hidden, x.Change, x.ChangedFields,
		at.UTC(), x.Actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, ledger.Classify(err)
}
