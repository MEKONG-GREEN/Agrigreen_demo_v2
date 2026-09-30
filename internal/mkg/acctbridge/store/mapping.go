// Package store: SQL của mkg/acctbridge (schema mkg_acctbridge, vai acctbridge_app). Mọi hàm chạy trong
// giao dịch tenant do app/ mở; RLS lọc theo tenant.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
)

// MappingEvent là một dòng sổ mapping_event.
type MappingEvent struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Seq           int64
	VersionID     uuid.UUID
	VersionSeq    int
	Kind          string // proposed | approved | rejected
	Rules         []domain.Rule
	RulesSHA256   string
	EffectiveFrom time.Time
	Provisional   bool
	ProposerRef   uuid.UUID
	Reason        *string
	EventTime     time.Time
	RecordedAt    time.Time
	ActorRef      uuid.UUID
	IdemKey       string
}

// InsertMappingEvent ghi sổ ánh xạ.
func InsertMappingEvent(ctx context.Context, tx pgx.Tx, e *MappingEvent) error {
	rules, err := json.Marshal(e.Rules)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO mkg_acctbridge.mapping_event
	    (id, tenant_id, stream_key, version_id, version_seq, kind, rules, rules_sha256, effective_from, provisional, proposer_ref, reason,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'mapping', $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, e.VersionID, e.VersionSeq, e.Kind, string(rules), e.RulesSHA256, e.EffectiveFrom, e.Provisional, e.ProposerRef, e.Reason,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.RuleMapping).Scan(&e.Seq, &e.RecordedAt)
}

const mappingEventCols = `id, tenant_id, seq, version_id, version_seq, kind, rules, rules_sha256, effective_from, provisional, proposer_ref,
  reason, event_time, recorded_at, actor_ref, idem_key`

func scanMappingEvent(row pgx.Row) (MappingEvent, error) {
	var e MappingEvent
	var rules []byte
	err := row.Scan(&e.ID, &e.TenantID, &e.Seq, &e.VersionID, &e.VersionSeq, &e.Kind, &rules, &e.RulesSHA256, &e.EffectiveFrom, &e.Provisional,
		&e.ProposerRef, &e.Reason, &e.EventTime, &e.RecordedAt, &e.ActorRef, &e.IdemKey)
	if err != nil {
		return e, err
	}
	err = json.Unmarshal(rules, &e.Rules)
	return e, err
}

// MappingEventByIdem tìm sự kiện ánh xạ theo idem_key (gọi lặp).
func MappingEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*MappingEvent, error) {
	e, err := scanMappingEvent(tx.QueryRow(ctx, "SELECT "+mappingEventCols+" FROM mkg_acctbridge.mapping_event WHERE idem_key = $1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// MappingEvents đọc toàn bộ sổ ánh xạ của tenant theo thứ tự ghi (dựng lại bảng chiếu).
func MappingEvents(ctx context.Context, tx pgx.Tx) ([]MappingEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+mappingEventCols+" FROM mkg_acctbridge.mapping_event ORDER BY seq")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MappingEvent
	for rows.Next() {
		e, err := scanMappingEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Mapping là bảng chiếu mapping_current.
type Mapping struct {
	VersionID      uuid.UUID
	VersionSeq     int
	Rules          []domain.Rule
	RulesSHA256    string
	EffectiveFrom  time.Time
	Provisional    bool
	Status         string // pending | approved | rejected
	ProposedBy     uuid.UUID
	ProposalReason string
	ProposedAt     time.Time
	DecidedBy      *uuid.UUID
	DecidedAt      *time.Time
	DecisionReason *string
}

const mappingCols = `version_id, version_seq, rules, rules_sha256, effective_from, provisional, status, proposed_by, proposal_reason,
  proposed_at, decided_by, decided_at, decision_reason`

func scanMapping(row pgx.Row) (Mapping, error) {
	var m Mapping
	var rules []byte
	err := row.Scan(&m.VersionID, &m.VersionSeq, &rules, &m.RulesSHA256, &m.EffectiveFrom, &m.Provisional, &m.Status, &m.ProposedBy,
		&m.ProposalReason, &m.ProposedAt, &m.DecidedBy, &m.DecidedAt, &m.DecisionReason)
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(rules, &m.Rules)
	return m, err
}

func oneMapping(row pgx.Row) (*Mapping, error) {
	m, err := scanMapping(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// LockMapping tuần tự hóa đề xuất ánh xạ trong tenant (cấp số phiên bản).
func LockMapping(ctx context.Context, tx pgx.Tx, tid uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "mkg_acctbridge.mapping:"+tid.String())
	return err
}

// NextMappingSeq trả số phiên bản kế tiếp.
func NextMappingSeq(ctx context.Context, tx pgx.Tx) (int, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT coalesce(max(version_seq), 0) + 1 FROM mkg_acctbridge.mapping_current").Scan(&n)
	return n, err
}

// GetMapping đọc một phiên bản; forUpdate khóa dòng.
func GetMapping(ctx context.Context, tx pgx.Tx, version uuid.UUID, forUpdate bool) (*Mapping, error) {
	q := "SELECT " + mappingCols + " FROM mkg_acctbridge.mapping_current WHERE version_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneMapping(tx.QueryRow(ctx, q, version))
}

// PendingMapping trả phiên bản đang chờ duyệt (tối đa một), nil nếu không có.
func PendingMapping(ctx context.Context, tx pgx.Tx) (*Mapping, error) {
	return oneMapping(tx.QueryRow(ctx, "SELECT "+mappingCols+" FROM mkg_acctbridge.mapping_current WHERE status = 'pending' LIMIT 1"))
}

// EffectiveMapping trả phiên bản ĐÃ DUYỆT hiệu lực vào ngày day (effective_from ≤ day, số phiên bản lớn
// nhất); nil = chưa có phiên bản nào được duyệt.
func EffectiveMapping(ctx context.Context, tx pgx.Tx, day time.Time) (*Mapping, error) {
	return oneMapping(tx.QueryRow(ctx, "SELECT "+mappingCols+` FROM mkg_acctbridge.mapping_current
	    WHERE status = 'approved' AND effective_from <= $1 ORDER BY effective_from DESC, version_seq DESC LIMIT 1`, day))
}

// Mappings liệt kê mọi phiên bản, mới nhất trước.
func Mappings(ctx context.Context, tx pgx.Tx) ([]Mapping, error) {
	rows, err := tx.Query(ctx, "SELECT "+mappingCols+" FROM mkg_acctbridge.mapping_current ORDER BY version_seq DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mapping
	for rows.Next() {
		m, err := scanMapping(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ApplyMappingEvent cập nhật bảng chiếu ánh xạ (cùng giao dịch với sổ — R-21).
func ApplyMappingEvent(ctx context.Context, tx pgx.Tx, e MappingEvent) error {
	if e.Kind == "proposed" {
		rules, err := json.Marshal(e.Rules)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO mkg_acctbridge.mapping_current
		    (tenant_id, version_id, version_seq, rules, rules_sha256, effective_from, provisional, status, proposed_by, proposal_reason, proposed_at)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', $8, $9, $10)`,
			e.TenantID, e.VersionID, e.VersionSeq, string(rules), e.RulesSHA256, e.EffectiveFrom, e.Provisional, e.ProposerRef, e.Reason, e.EventTime.UTC())
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE mkg_acctbridge.mapping_current SET status = $2, decided_by = $3, decided_at = $4, decision_reason = $5,
	    updated_at = now() WHERE version_id = $1`, e.VersionID, e.Kind, e.ActorRef, e.EventTime.UTC(), e.Reason)
	return err
}
