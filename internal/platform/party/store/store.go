// Package store: truy cập DB của pf/party, chỉ qua pool của vai party_app (R-05).
// Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant (tenant.WithTenantTx).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

const consentCols = `id, tenant_id, subject_ref, purpose, text_version, status, channel, event_time,
  actor_ref, device_id, idem_key, seq, recorded_at`

func scanConsent(row pgx.Row) (domain.Consent, error) {
	var c domain.Consent
	var status, channel string
	err := row.Scan(&c.ID, &c.TenantID, &c.SubjectRef, &c.Purpose, &c.TextVersion, &status, &channel, &c.EventTime,
		&c.ActorRef, &c.DeviceID, &c.IdemKey, &c.Seq, &c.RecordedAt)
	c.Status, c.Channel = domain.Status(status), domain.Channel(channel)
	return c, err
}

// ByIdemKey tìm bản ghi đã ghi với idem_key (đường nhanh cho gọi lặp).
func ByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Consent, error) {
	c, err := scanConsent(tx.QueryRow(ctx, "SELECT "+consentCols+" FROM pf_party.consent WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ByID đọc một bản ghi.
func ByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.Consent, error) {
	c, err := scanConsent(tx.QueryRow(ctx, "SELECT "+consentCols+" FROM pf_party.consent WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Insert ghi sổ; DB tính seq, prev_hash, row_hash và trả về seq, recorded_at.
func Insert(ctx context.Context, tx pgx.Tx, c *domain.Consent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_party.consent
	    (id, tenant_id, stream_key, subject_ref, purpose, text_version, status, channel, event_time,
	     actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	  RETURNING seq, recorded_at`,
		c.ID, c.TenantID, domain.StreamKey(c.SubjectRef), c.SubjectRef, c.Purpose, c.TextVersion, string(c.Status), string(c.Channel),
		c.EventTime.UTC(), c.ActorRef, c.DeviceID, c.IdemKey, domain.RuleVersion).Scan(&c.Seq, &c.RecordedAt)
}

// CurrentFor đọc bảng chiếu và khóa dòng để cập nhật trong cùng giao dịch (R-21).
func CurrentFor(ctx context.Context, tx pgx.Tx, subject uuid.UUID, purpose string) (*domain.Current, error) {
	var cur domain.Current
	var status string
	err := tx.QueryRow(ctx, `SELECT status, text_version, consent_id, consent_seq, event_time
	    FROM pf_party.consent_current WHERE subject_ref = $1 AND purpose = $2 FOR UPDATE`, subject, purpose).
		Scan(&status, &cur.TextVersion, &cur.ConsentID, &cur.Seq, &cur.EventTime)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cur.Status = domain.Status(status)
	return &cur, nil
}

// UpsertCurrent ghi bảng chiếu.
func UpsertCurrent(ctx context.Context, tx pgx.Tx, c domain.Consent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.consent_current
	    (tenant_id, subject_ref, purpose, status, text_version, consent_id, consent_seq, event_time)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	  ON CONFLICT (tenant_id, subject_ref, purpose) DO UPDATE SET
	    status = EXCLUDED.status, text_version = EXCLUDED.text_version, consent_id = EXCLUDED.consent_id,
	    consent_seq = EXCLUDED.consent_seq, event_time = EXCLUDED.event_time, updated_at = now()`,
		c.TenantID, c.SubjectRef, c.Purpose, string(c.Status), c.TextVersion, c.ID, c.Seq, c.EventTime.UTC())
	return err
}

// ListBySubject phân trang bằng cursor (R-53): id > after, theo thứ tự id (UUIDv7).
func ListBySubject(ctx context.Context, tx pgx.Tx, subject uuid.UUID, after uuid.UUID, limit int) ([]domain.Consent, error) {
	rows, err := tx.Query(ctx, "SELECT "+consentCols+` FROM pf_party.consent
	    WHERE subject_ref = $1 AND id > $2 ORDER BY id LIMIT $3`, subject, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Consent
	for rows.Next() {
		c, err := scanConsent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// HasConsent đọc bảng chiếu.
func HasConsent(ctx context.Context, tx pgx.Tx, subject uuid.UUID, purpose string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_party.consent_current
	    WHERE subject_ref = $1 AND purpose = $2 AND status = 'granted')`, subject, purpose).Scan(&ok)
	return ok, err
}

// RebuildCurrent dựng lại bảng chiếu từ sổ cho tenant hiện tại (R-22); at là updated_at (đồng hồ chung của app).
func RebuildCurrent(ctx context.Context, tx pgx.Tx, at time.Time) (int64, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM pf_party.consent_current"); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO pf_party.consent_current
	    (tenant_id, subject_ref, purpose, status, text_version, consent_id, consent_seq, event_time, updated_at)
	  SELECT DISTINCT ON (tenant_id, subject_ref, purpose)
	    tenant_id, subject_ref, purpose, status, text_version, id, seq, event_time, $1::timestamptz
	  FROM pf_party.consent
	  ORDER BY tenant_id, subject_ref, purpose, event_time DESC, seq DESC`, at.UTC())
	return tag.RowsAffected(), err
}
