package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// EgressRow là một dòng sổ xuất (tenant = chủ dữ liệu).
type EgressRow struct {
	ID               uuid.UUID
	StreamKey        string
	Seq              int64
	Channel          string
	SourceID         uuid.UUID
	CounterpartyKind string
	CounterpartyRef  uuid.UUID
	Product          string
	ContractVersion  int
	Records          int64
	Bytes            int64
	BusinessDate     time.Time
	Decision         string
	Reason           string
	Mode             string
	DailyRecords     int64
	DailyQuota       int64
	Anomaly          bool
	TraceID          *string
	EventTime        time.Time
	RecordedAt       time.Time
	ActorRef         uuid.UUID
	IdemKey          string
	RuleVersion      string
}

const egressCols = `id, stream_key, seq, channel, source_id, counterparty_kind, counterparty_ref, product, contract_version, records,
  bytes, business_date, decision, reason, mode, daily_records, daily_quota, anomaly, trace_id, event_time, recorded_at, actor_ref, idem_key, rule_version`

func scanEgress(r pgx.Row) (EgressRow, error) {
	var e EgressRow
	err := r.Scan(&e.ID, &e.StreamKey, &e.Seq, &e.Channel, &e.SourceID, &e.CounterpartyKind, &e.CounterpartyRef, &e.Product, &e.ContractVersion,
		&e.Records, &e.Bytes, &e.BusinessDate, &e.Decision, &e.Reason, &e.Mode, &e.DailyRecords, &e.DailyQuota, &e.Anomaly, &e.TraceID,
		&e.EventTime, &e.RecordedAt, &e.ActorRef, &e.IdemKey, &e.RuleVersion)
	return e, err
}

// EgressByIdem đọc dòng sổ xuất theo idem_key; nil khi chưa có.
func EgressByIdem(ctx context.Context, tx pgx.Tx, idem string) (*EgressRow, error) {
	e, err := scanEgress(tx.QueryRow(ctx, `SELECT `+egressCols+` FROM pf_metering.egress_event WHERE idem_key = $1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// InsertEgress ghi một dòng sổ xuất.
func InsertEgress(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *EgressRow) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_metering.egress_event (id, tenant_id, stream_key, channel, source_id, counterparty_kind,
	    counterparty_ref, product, contract_version, records, bytes, business_date, decision, reason, mode, daily_records, daily_quota,
	    anomaly, trace_id, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
	  RETURNING seq, recorded_at`,
		e.ID, tid, e.StreamKey, e.Channel, e.SourceID, e.CounterpartyKind, e.CounterpartyRef, e.Product, e.ContractVersion, e.Records, e.Bytes,
		e.BusinessDate, e.Decision, e.Reason, e.Mode, e.DailyRecords, e.DailyQuota, e.Anomaly, e.TraceID, e.EventTime, e.ActorRef, e.IdemKey,
		e.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// EgressKey là khóa bộ đếm khối lượng ngày.
type EgressKey struct {
	Channel          string
	CounterpartyKind string
	CounterpartyRef  uuid.UUID
	Day              time.Time
}

// LockEgressCounter tạo (nếu chưa có) và khóa dòng bộ đếm; trả số phần tử đã cho qua trong ngày.
func LockEgressCounter(ctx context.Context, tx pgx.Tx, tid uuid.UUID, k EgressKey) (int64, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO pf_metering.egress_counter_current (tenant_id, channel, counterparty_kind, counterparty_ref,
	    business_date, records, bytes, events) VALUES ($1,$2,$3,$4,$5,0,0,0) ON CONFLICT DO NOTHING`,
		tid, k.Channel, k.CounterpartyKind, k.CounterpartyRef, k.Day); err != nil {
		return 0, err
	}
	var n int64
	err := tx.QueryRow(ctx, `SELECT records FROM pf_metering.egress_counter_current
	  WHERE channel = $1 AND counterparty_kind = $2 AND counterparty_ref = $3 AND business_date = $4 FOR UPDATE`,
		k.Channel, k.CounterpartyKind, k.CounterpartyRef, k.Day).Scan(&n)
	return n, err
}

// BumpEgressCounter cộng phần tử/byte/lần của một lần được cho qua.
func BumpEgressCounter(ctx context.Context, tx pgx.Tx, k EgressKey, records, bytes int64) error {
	_, err := tx.Exec(ctx, `UPDATE pf_metering.egress_counter_current
	  SET records = records + $5, bytes = bytes + $6, events = events + 1, updated_at = now()
	  WHERE channel = $1 AND counterparty_kind = $2 AND counterparty_ref = $3 AND business_date = $4`,
		k.Channel, k.CounterpartyKind, k.CounterpartyRef, k.Day, records, bytes)
	return err
}

// EgressHistory trả khối lượng các ngày CÓ dữ liệu trong 28 ngày trước Day (không gồm Day).
func EgressHistory(ctx context.Context, tx pgx.Tx, k EgressKey, days int) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT records FROM pf_metering.egress_counter_current
	  WHERE channel = $1 AND counterparty_kind = $2 AND counterparty_ref = $3
	    AND business_date >= $4::date - $5::int AND business_date < $4::date AND records > 0`,
		k.Channel, k.CounterpartyKind, k.CounterpartyRef, k.Day, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// AnomalyRow là cảnh báo khối lượng xuất bất thường (một dòng mỗi ngày × kênh × bên nhận).
type AnomalyRow struct {
	ID               uuid.UUID
	BusinessDate     time.Time
	Channel          string
	CounterpartyKind string
	CounterpartyRef  uuid.UUID
	Product          string
	EgressEventID    uuid.UUID
	DailyRecords     int64
	Threshold        int64
	Basis            string
	Mode             string
	DetectedAt       time.Time
}

// InsertAnomalyOnce ghi cảnh báo nếu ngày này chưa có; true khi là lần đầu.
func InsertAnomalyOnce(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a AnomalyRow) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_metering.egress_anomaly (id, tenant_id, business_date, channel, counterparty_kind,
	    counterparty_ref, product, egress_event_id, daily_records, threshold, basis, mode, detected_at)
	  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	  ON CONFLICT (tenant_id, business_date, channel, counterparty_kind, counterparty_ref) DO NOTHING`,
		a.ID, tid, a.BusinessDate, a.Channel, a.CounterpartyKind, a.CounterpartyRef, a.Product, a.EgressEventID, a.DailyRecords,
		a.Threshold, a.Basis, a.Mode, a.DetectedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// EgressFilter lọc danh sách sổ xuất.
type EgressFilter struct {
	Channel string
	From    *time.Time // ngày giờ VN (bao gồm)
	To      *time.Time // ngày giờ VN (bao gồm)
	After   uuid.UUID
	Limit   int
}

// EgressEvents liệt kê sổ xuất của tenant (mới nhất trước).
func EgressEvents(ctx context.Context, tx pgx.Tx, f EgressFilter) ([]EgressRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+egressCols+` FROM pf_metering.egress_event
	  WHERE ($1::text = '' OR channel = $1::text) AND ($2::date IS NULL OR business_date >= $2) AND ($3::date IS NULL OR business_date <= $3)
	    AND ($4::uuid IS NULL OR id < $4)
	  ORDER BY id DESC LIMIT $5`, f.Channel, f.From, f.To, nullID(f.After), f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EgressRow
	for rows.Next() {
		e, err := scanEgress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Anomalies liệt kê cảnh báo của tenant (mới nhất trước).
func Anomalies(ctx context.Context, tx pgx.Tx, channel string, after uuid.UUID, limit int) ([]AnomalyRow, error) {
	rows, err := tx.Query(ctx, `SELECT id, business_date, channel, counterparty_kind, counterparty_ref, product, egress_event_id,
	    daily_records, threshold, basis, mode, detected_at
	  FROM pf_metering.egress_anomaly WHERE ($1::text = '' OR channel = $1::text) AND ($2::uuid IS NULL OR id < $2) ORDER BY id DESC LIMIT $3`,
		channel, nullID(after), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnomalyRow
	for rows.Next() {
		var a AnomalyRow
		if err := rows.Scan(&a.ID, &a.BusinessDate, &a.Channel, &a.CounterpartyKind, &a.CounterpartyRef, &a.Product, &a.EgressEventID,
			&a.DailyRecords, &a.Threshold, &a.Basis, &a.Mode, &a.DetectedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
